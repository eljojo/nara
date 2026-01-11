package nara

import (
	"os"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v3/mem"
)

// MemoryMode describes coarse memory profiles for a nara.
type MemoryMode string

const (
	MemoryModeAuto   MemoryMode = "auto"
	MemoryModeShort  MemoryMode = "short"
	MemoryModeMedium MemoryMode = "medium"
	MemoryModeHog    MemoryMode = "hog"
	MemoryModeCustom MemoryMode = "custom"
)

// MemoryProfile captures memory behavior and limits.
type MemoryProfile struct {
	Mode                 MemoryMode
	BudgetMB             int
	MaxEvents            int
	EnableBackgroundSync bool
}

// ParseMemoryMode normalizes a mode string into a known MemoryMode.
func ParseMemoryMode(mode string) MemoryMode {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case string(MemoryModeAuto):
		return MemoryModeAuto
	case string(MemoryModeShort):
		return MemoryModeShort
	case string(MemoryModeHog):
		return MemoryModeHog
	case string(MemoryModeCustom):
		return MemoryModeCustom
	default:
		return MemoryModeMedium
	}
}

// MemoryModeForBudget chooses a mode based on available memory budget.
func MemoryModeForBudget(budgetMB int) MemoryMode {
	switch {
	case budgetMB <= 384:
		return MemoryModeShort
	case budgetMB <= 1024:
		return MemoryModeMedium
	default:
		return MemoryModeHog
	}
}

// AutoMemoryProfile determines the best profile based on available memory.
func AutoMemoryProfile() (MemoryProfile, string, error) {
	budgetMB, source, err := detectMemoryBudgetMB()
	if err != nil {
		return DefaultMemoryProfile(), "default", err
	}
	mode := MemoryModeForBudget(budgetMB)
	profile := MemoryProfileForMode(mode)
	return profile, source, nil
}

// MemoryProfileForMode returns the default profile for a given mode.
func MemoryProfileForMode(mode MemoryMode) MemoryProfile {
	switch mode {
	case MemoryModeShort:
		return MemoryProfile{
			Mode:     MemoryModeShort,
			BudgetMB: 256,
			// Memory math (rough, conservative):
			// Assume ~2.5 KB per stored event on average (struct + maps + payload + overhead).
			// 20k events * 2.5 KB ≈ 50 MB for ledger contents, leaving ample headroom
			// for indexes, projections, networking buffers, and Go heap growth in 256 MB.
			MaxEvents:            20000,
			EnableBackgroundSync: false,
		}
	case MemoryModeHog:
		return MemoryProfile{
			Mode:                 MemoryModeHog,
			BudgetMB:             2048,
			MaxEvents:            320000,
			EnableBackgroundSync: true,
		}
	case MemoryModeCustom:
		return MemoryProfile{
			Mode:                 MemoryModeCustom,
			BudgetMB:             0,
			MaxEvents:            0,
			EnableBackgroundSync: true,
		}
	default:
		return MemoryProfile{
			Mode:                 MemoryModeMedium,
			BudgetMB:             512,
			MaxEvents:            80000,
			EnableBackgroundSync: true,
		}
	}
}

// DefaultMemoryProfile returns the standard profile (medium).
func DefaultMemoryProfile() MemoryProfile {
	return MemoryProfileForMode(MemoryModeMedium)
}

func detectMemoryBudgetMB() (int, string, error) {
	vmem, err := mem.VirtualMemory()
	if err != nil {
		return 0, "", err
	}
	totalBytes := vmem.Total
	limitBytes, err := cgroupMemoryLimitBytes(totalBytes)
	if err == nil && limitBytes > 0 && limitBytes < totalBytes {
		return int(limitBytes / (1024 * 1024)), "cgroup", nil
	}
	return int(totalBytes / (1024 * 1024)), "system", nil
}

func cgroupMemoryLimitBytes(totalBytes uint64) (uint64, error) {
	paths := []string{
		"/sys/fs/cgroup/memory.max",                   // cgroup v2
		"/sys/fs/cgroup/memory/memory.limit_in_bytes", // cgroup v1
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		raw := strings.TrimSpace(string(data))
		if raw == "" || raw == "max" {
			return 0, nil
		}
		val, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return 0, err
		}
		if totalBytes > 0 && val > totalBytes*2 {
			return 0, nil
		}
		return val, nil
	}
	return 0, os.ErrNotExist
}
