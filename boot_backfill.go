package nara

import (
	"github.com/sirupsen/logrus"

	"github.com/eljojo/nara/types"
)

// backfillObservations migrates existing observations to observation events
// This enables smooth transition from newspaper-based consensus to event-based
// Called directly from formOpinion() after opinions are formed
func (network *Network) backfillObservations() {
	myName := network.meName()
	backfillCount := 0

	// Lock Me.mu to safely read Me.Status.Observations
	network.local.Me.mu.Lock()
	observations := make(map[types.NaraName]NaraObservation)
	for name, obs := range network.local.Me.Status.Observations {
		observations[name] = obs
	}
	network.local.Me.mu.Unlock()

	logrus.Printf("📦 Checking if backfill needed for %d observations...", len(observations))

	for naraName, obs := range observations {
		if naraName == myName {
			continue // Skip self
		}

		// Check if we already have a backfill event for this nara
		existingEvents := network.local.SyncLedger.GetObservationEventsAbout(naraName)
		hasBackfill := false
		for _, e := range existingEvents {
			if e.Observation != nil && e.Observation.IsBackfill {
				hasBackfill = true
				break
			}
		}
		if hasBackfill {
			// Already have backfill baseline, skip
			continue
		}

		// No backfill exists yet, but we have newspaper-based knowledge
		// Create backfill event if data is meaningful
		if obs.StartTime > 0 && obs.Restarts >= 0 {
			event := NewBackfillObservationEvent(
				myName,
				naraName,
				obs.StartTime,
				obs.Restarts,
				obs.LastRestart,
			)

			// Add with full anti-abuse protections
			added := network.local.SyncLedger.AddEventWithDedup(event)
			if added {
				backfillCount++
				logrus.Infof("📦 Backfilled observation for %s (start:%d, restarts:%d)",
					naraName, obs.StartTime, obs.Restarts)
			}
		}
	}

	if backfillCount > 0 {
		logrus.Printf("📦 Backfilled %d historical observations into event system", backfillCount)
		if network.local.Projections != nil {
			network.local.Projections.Trigger()
		}
	} else {
		logrus.Printf("📦 No backfill needed (events already present or no meaningful data)")
	}
}

// seedAvgPingRTTFromHistory initializes AvgPingRTT from historical ping observations
// Called after boot recovery to seed exponential moving average with recovered data
func (network *Network) seedAvgPingRTTFromHistory() {
	if network.local.SyncLedger == nil {
		return
	}

	// Get all ping observations from the ledger
	allPings := network.local.SyncLedger.GetPingObservations()

	// Group pings by target to seed our observations
	// We use ALL pings (from any observer) to get initial RTT estimates
	// This means we learn from the network: if B→C has 50ms RTT, we seed our C observation with that
	myName := network.meName()
	pingsByTarget := make(map[types.NaraName][]float64)

	for _, ping := range allPings {
		// TODO: ping.Target should be migrated to types.NaraID instead of string
		// Currently using string comparison but this should be ID-based
		// Skip pings TO us (we care about targets we might ping, not pings to us)
		targetName := types.NaraName(ping.Target)
		if targetName != myName {
			pingsByTarget[targetName] = append(pingsByTarget[targetName], ping.RTT)
		}
	}

	// Seed AvgPingRTT for each target
	seededCount := 0
	for naraName, rtts := range pingsByTarget {
		obs := network.local.getObservation(naraName)

		// Only seed if AvgPingRTT is not already set (0 means uninitialized)
		if obs.AvgPingRTT == 0 && len(rtts) > 0 {
			// Calculate simple average from historical pings
			sum := 0.0
			for _, rtt := range rtts {
				sum += rtt
			}
			avg := sum / float64(len(rtts))

			obs.AvgPingRTT = avg
			network.local.setObservation(naraName, obs)
			seededCount++
		}
	}

	if seededCount > 0 {
		logrus.Printf("📍 Seeded AvgPingRTT for %d targets from historical ping observations", seededCount)
	}
}
