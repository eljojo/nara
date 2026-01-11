package nara

import (
	"os"
	"runtime"
	"time"

	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/process"
	"github.com/sirupsen/logrus"
)

type HostStats struct {
	Uptime         uint64
	LoadAvg        float64
	MemAllocMB     uint64  // Current heap allocation in MB
	MemSysMB       uint64  // Total memory obtained from OS in MB
	MemHeapMB      uint64  // Heap memory (in use + free) in MB
	MemStackMB     uint64  // Stack memory in MB
	NumGoroutines  int     // Number of active goroutines
	NumGC          uint32  // Number of completed GC cycles
	ProcCPUPercent float64 // CPU usage of this process (percent)
}

func (ln *LocalNara) updateHostStatsForever() {
	for {
		ln.updateHostStats()

		select {
		case <-time.After(5 * time.Second):
			// Continue to next iteration
		case <-ln.Network.ctx.Done():
			logrus.Debugf("updateHostStatsForever: shutting down gracefully")
			return
		}
	}
}

func (ln *LocalNara) updateHostStats() {
	uptime, _ := host.Uptime()
	ln.Me.Status.HostStats.Uptime = uptime

	load, _ := load.Avg()
	loadavg := load.Load1 / float64(runtime.NumCPU())
	ln.Me.Status.HostStats.LoadAvg = float64(int64(loadavg*100)) / 100 // truncate to 2 digits

	// Collect memory statistics from Go runtime
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	ln.Me.Status.HostStats.MemAllocMB = memStats.Alloc / 1024 / 1024    // Current heap allocation
	ln.Me.Status.HostStats.MemSysMB = memStats.Sys / 1024 / 1024        // Total memory from OS
	ln.Me.Status.HostStats.MemHeapMB = memStats.HeapSys / 1024 / 1024   // Heap (in use + free)
	ln.Me.Status.HostStats.MemStackMB = memStats.StackSys / 1024 / 1024 // Stack memory
	ln.Me.Status.HostStats.NumGoroutines = runtime.NumGoroutine()       // Active goroutines
	ln.Me.Status.HostStats.NumGC = memStats.NumGC                       // GC cycles

	if proc, err := process.NewProcess(int32(os.Getpid())); err == nil {
		if cpuPct, err := proc.Percent(0); err == nil {
			ln.Me.Status.HostStats.ProcCPUPercent = float64(int64(cpuPct*100)) / 100
		}
	}

	chattiness := int64(ln.Network.weightedBuzz())

	if ln.forceChattiness >= 0 && ln.forceChattiness <= 100 {
		chattiness = int64(ln.forceChattiness)
	} else {
		if loadavg < 1 {
			chattiness = chattiness + int64((1-loadavg)*20)
		} else {
			ln.Me.Status.Chattiness = 0
		}
	}

	if chattiness > 100 {
		chattiness = 100
	}

	if ln.Me.Status.HostStats.LoadAvg > 0.8 && ln.Network.skippingEvents == false && !ln.isBooting() {
		logrus.Println("[warning] busy cpu, newspaper events may be dropped")
		ln.Network.skippingEvents = true
	} else if ln.Me.Status.HostStats.LoadAvg < 0.8 && ln.Network.skippingEvents == true {
		logrus.Println("[recovered] cpu is healthy again, not dropping events anymore")
		ln.Network.skippingEvents = false
	}

	if ln.isBooting() {
		chattiness = 100
		ln.Network.skippingEvents = false
	}

	ln.Me.Status.Chattiness = chattiness
}
