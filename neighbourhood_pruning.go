package nara

import (
	"fmt"
	"runtime"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/eljojo/nara/types"
)

// neighbourhood_pruning.go
// Extracted from network.go
// Contains neighbourhood pruning and maintenance methods

// pruneInactiveNaras removes naras that have been offline for too long.
// Uses tiered pruning: newcomers (24h), established (7d), veterans (never pruned).
func (network *Network) pruneInactiveNaras() {
	network.local.mu.Lock()

	now := time.Now().Unix()

	const (
		newcomerAge        = int64(2 * 86400)  // 2 days - proving period
		establishedAge     = int64(30 * 86400) // 30 days - veteran threshold
		newcomerTimeout    = int64(24 * 3600)  // Prune newcomers after 24h offline
		establishedTimeout = int64(7 * 86400)  // Prune established after 7d offline
		// Veterans (30d+) are never auto-pruned - they're part of the community
	)

	var toRemove []types.NaraName
	var established, veterans, zombies int
	var prunedNewcomers, prunedEstablished int

	for name, nara := range network.Neighbourhood {
		obs := network.local.getObservationLocked(name)

		// Never remove ourselves
		if name == network.meName() {
			continue
		}

		// Count naras by age category (regardless of online status)
		naraAge := int64(0)
		if obs.StartTime > 0 {
			naraAge = now - obs.StartTime
		}
		if naraAge >= establishedAge {
			veterans++
		} else if naraAge >= newcomerAge {
			established++
		}

		// Never remove currently ONLINE naras
		if obs.Online == "ONLINE" {
			continue
		}

		// Calculate how long since we last saw them
		timeSinceLastSeen := int64(0)
		if obs.LastSeen > 0 {
			timeSinceLastSeen = now - obs.LastSeen
		}

		nara.mu.Lock()
		hasActivity := nara.Status.Flair != "" || nara.Status.Buzz > 0 || nara.Status.Chattiness > 0
		nara.mu.Unlock()

		// Zombie detection: never seen but "first seen" is old
		// These are malformed entries that should be cleaned up immediately
		if obs.LastSeen == 0 && obs.StartTime > 0 && (now-obs.StartTime) > 3600 {
			toRemove = append(toRemove, name)
			zombies++
			continue
		}

		// Also catch zombies with no timestamps and no activity
		if obs.LastSeen == 0 && obs.StartTime == 0 && !hasActivity {
			toRemove = append(toRemove, name)
			zombies++
			continue
		}

		// Skip if we don't have LastSeen (but they're not a zombie)
		if obs.LastSeen == 0 {
			logrus.Debugf("⏭️  Skipping %s: no LastSeen timestamp (but has activity, not a zombie)", name)
			continue
		}

		// Tiered pruning based on nara age
		shouldPrune := false

		if naraAge < newcomerAge {
			// Newcomer: prune if offline for 24h
			if timeSinceLastSeen > newcomerTimeout {
				shouldPrune = true
				prunedNewcomers++
			}
		} else if naraAge < establishedAge {
			// Established: prune if offline for 7 days
			if timeSinceLastSeen > establishedTimeout {
				shouldPrune = true
				prunedEstablished++
			}
			// Veteran (30d+): keep indefinitely, they're part of the community
			// Even if they're offline for months, we remember them (bart, r2d2, lisa, etc.)
		}

		if shouldPrune {
			toRemove = append(toRemove, name)
		}
	}

	// Remove inactive naras
	if len(toRemove) > 0 {
		// Lock ordering: We release local.mu before taking Me.mu to avoid holding both simultaneously
		// This is safe because we're only deleting (not checking existence then acting on it)
		// Other code that needs both locks must follow: local.mu → Me.mu (never the reverse)

		// First remove from Neighbourhood (protected by network.local.mu)
		for _, name := range toRemove {
			delete(network.Neighbourhood, name)
			network.howdyCoordinators.Delete(name)
		}

		// Also remove their events from the sync ledger to prevent re-discovery
		// This removes ALL events involving the ghost nara, including:
		// - Events they emitted (hey-there, chau, social, etc.)
		// - Events about them (observations, pings, social events where they're target)
		// - Checkpoints WHERE THEY ARE THE SUBJECT (checkpoints about them)
		//   Note: Checkpoints where they were only a voter/emitter are kept
		// See sync.go:eventInvolvesNara() for the full pruning logic
		if network.local.SyncLedger != nil {
			for _, name := range toRemove {
				network.local.SyncLedger.RemoveEventsFor(name)
			}
		}

		// Clean up verification caches and trigger projection updates
		network.pruneVerifyPingCache(toRemove)
		if network.local.Projections != nil {
			network.local.Projections.Trigger()
		}
		network.local.mu.Unlock()

		// Then remove from our observations (protected by network.local.Me.mu)
		network.local.Me.mu.Lock()
		for _, name := range toRemove {
			delete(network.local.Me.Status.Observations, name)
		}
		network.local.Me.mu.Unlock()

		logrus.Printf("🧹 Pruned %d inactive naras: %d zombies, %d newcomers (24h), %d established (7d) | kept %d established, %d veterans (30d+)",
			len(toRemove), zombies, prunedNewcomers, prunedEstablished, established-prunedEstablished, veterans)
		logrus.Printf("🕯️ 🪦 🕯️  In memory of: %v", toRemove)
	} else {
		network.local.mu.Unlock()
	}
}

// socialMaintenance periodically cleans up social data and prunes inactive naras.
func (network *Network) socialMaintenance() {
	// Run every 5 minutes
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// continue
		case <-network.ctx.Done():
			return
		}

		// Prune inactive naras from neighbourhood
		network.pruneInactiveNaras()

		// Prune the sync ledger
		if network.local.SyncLedger != nil {
			beforeCount := network.local.SyncLedger.EventCount()
			network.local.SyncLedger.Prune()
			afterCount := network.local.SyncLedger.EventCount()

			// Log event store stats
			serviceCounts := network.local.SyncLedger.GetEventCountsByService()
			criticalCount := network.local.SyncLedger.GetCriticalEventCount()
			var statsStr string
			for service, count := range serviceCounts {
				if statsStr != "" {
					statsStr += ", "
				}
				statsStr += fmt.Sprintf("%s=%d", service, count)
			}

			// Get memory stats
			var memStats runtime.MemStats
			runtime.ReadMemStats(&memStats)
			memAllocMB := memStats.Alloc / 1024 / 1024
			memSysMB := memStats.Sys / 1024 / 1024
			memHeapMB := memStats.HeapSys / 1024 / 1024
			memStackMB := memStats.StackSys / 1024 / 1024
			numGoroutines := runtime.NumGoroutine()
			// Calculate overhead: Sys - HeapSys - StackSys = other runtime structures
			memOtherMB := memSysMB - memHeapMB - memStackMB

			if beforeCount != afterCount {
				logrus.Printf("📊 event store: %d events (%s, critical=%d) - pruned %d | mem: %dMB alloc, %dMB sys (heap:%dMB stack:%dMB other:%dMB) | goroutines:%d",
					afterCount, statsStr, criticalCount, beforeCount-afterCount, memAllocMB, memSysMB, memHeapMB, memStackMB, memOtherMB, numGoroutines)
			} else {
				logrus.Printf("📊 event store: %d events (%s, critical=%d) | mem: %dMB alloc, %dMB sys (heap:%dMB stack:%dMB other:%dMB) | goroutines:%d",
					afterCount, statsStr, criticalCount, memAllocMB, memSysMB, memHeapMB, memStackMB, memOtherMB, numGoroutines)
			}

			// Cleanup rate limiter to prevent unbounded map growth
			network.local.SyncLedger.observationRL.Cleanup()
		}

		// Cleanup tease cooldowns
		network.TeaseState.Cleanup()
	}
}
