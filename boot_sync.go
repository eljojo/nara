package nara

import (
	"math/rand"
	"time"

	"github.com/sirupsen/logrus"
)

// backgroundSync performs lightweight periodic syncing to strengthen collective memory
// Runs every ~30 minutes (±5min jitter) to catch up on missed critical events
func (network *Network) backgroundSync() {
	// Initial random delay (0-5 minutes) to spread startup load
	initialDelay := time.Duration(rand.Intn(5)) * time.Minute

	select {
	case <-time.After(initialDelay):
		// continue
	case <-network.ctx.Done():
		logrus.Debugf("backgroundSync: shutting down before initial delay")
		return
	}

	// Main sync loop: every 30 minutes ±5min jitter
	for {
		baseInterval := 30 * time.Minute
		jitter := time.Duration(rand.Intn(10)-5) * time.Minute // -5 to +5 minutes
		interval := baseInterval + jitter

		select {
		case <-time.After(interval):
			network.performBackgroundSync()
		case <-network.ctx.Done():
			logrus.Debugf("backgroundSync: shutting down gracefully")
			return
		}
	}
}

// performBackgroundSync executes a single background sync cycle
func (network *Network) performBackgroundSync() {
	network.recoverSelfStartTimeFromMesh()

	// Get online neighbors
	online := network.NeighbourhoodOnlineNames()
	if len(online) == 0 {
		logrus.Debug("🔄 Background sync: no neighbors online")
		return
	}

	// Prefer neighbors that advertise medium/hog memory profiles
	eligible := make([]string, 0, len(online))
	for _, name := range online {
		if network.neighborSupportsBackgroundSync(name) {
			eligible = append(eligible, name)
		}
	}
	if len(eligible) == 0 {
		logrus.Debug("🔄 Background sync: no eligible neighbors (memory-limited)")
		return
	}

	// Pick 1-2 random neighbors to query
	numNeighbors := 1
	if len(eligible) > 1 && rand.Float64() > 0.5 {
		numNeighbors = 2
	}

	// Shuffle and pick neighbors
	rand.Shuffle(len(eligible), func(i, j int) {
		eligible[i], eligible[j] = eligible[j], eligible[i]
	})

	neighbors := eligible[:numNeighbors]

	// Query each neighbor via mesh if available
	for _, neighbor := range neighbors {
		if network.tsnetMesh != nil {
			ip := network.getMeshIPForNara(neighbor)
			if ip != "" {
				network.performBackgroundSyncViaMesh(neighbor, ip)
			} else {
				logrus.Debugf("🔄 Background sync: neighbor %s not mesh-enabled, skipping", neighbor)
			}
		} else {
			// Could add MQTT fallback here if needed
			logrus.Debug("🔄 Background sync: mesh not available, skipping")
		}
	}
}

func (network *Network) neighborSupportsBackgroundSync(name string) bool {
	nara := network.getNara(name)
	// Defensive: nara might not be found or might have been removed
	if nara == nil || nara.Name == "" {
		return true
	}
	if nara.Status.MemoryMode == string(MemoryModeShort) {
		return false
	}
	return true
}

// performBackgroundSyncViaMesh performs background sync with a specific neighbor via mesh
//
// IMPORTANT: This function determines which event types are continuously synced across the network.
// When adding new service types (in sync.go), consider whether they should be synced here.
// Current services synced:
//   - ServiceObservation: restart/first-seen/status-change events (24h window, personality filtered)
//   - ServicePing: RTT measurements (no time filter, used for Vivaldi coordinates)
//   - ServiceSocial: teases, observations, gossip (24h window, personality filtered)
//
// Boot recovery (bootRecoveryViaMesh) syncs ALL events without filtering.
// This background sync maintains eventual consistency for recent events.
func (network *Network) performBackgroundSyncViaMesh(neighbor, ip string) {
	logrus.Infof("🔄 background sync: requesting events from %s (%s)", neighbor, ip)

	// Fetch recent events from this neighbor using the new "recent" mode
	events, err := network.meshClient.FetchSyncEvents(network.ctx, ip, SyncRequest{
		Mode:  "recent",
		Limit: 100, // lightweight query
	})

	if err != nil {
		logrus.Infof("🔄 Background sync with %s failed: %v", neighbor, err)
		return
	}

	// Filter and merge events by type
	// Time cutoff for observation and social events (24h window)
	cutoff := time.Now().Add(-24 * time.Hour).UnixNano()
	added := 0
	hasPingEvents := false

	for _, event := range events {
		switch event.Service {
		case ServiceObservation:
			// Observation events: 24h window with personality filtering
			if event.Timestamp >= cutoff {
				if network.local.SyncLedger.AddEventFiltered(event, network.local.Me.Status.Personality) {
					added++
				}
			}

		case ServicePing:
			// Ping events: no time filtering (always useful for coordinate estimation)
			if network.local.SyncLedger.AddEvent(event) {
				added++
				hasPingEvents = true
			}

		case ServiceSocial:
			// Social events: 24h window with personality filtering
			// This ensures teases, gossip, and social observations propagate across the network
			if event.Timestamp >= cutoff {
				if network.local.SyncLedger.AddEventFiltered(event, network.local.Me.Status.Personality) {
					added++
				}
			}
		}
	}

	if added > 0 {
		logrus.Printf("🔄 background sync from %s: received %d events, merged %d", neighbor, len(events), added)

		// If we received ping events, update AvgPingRTT from history
		if hasPingEvents {
			network.seedAvgPingRTTFromHistory()
		}

		// Trigger projection update for new events
		if network.local.Projections != nil {
			network.local.Projections.Trigger()
		}
	} else if len(events) > 0 {
		logrus.Debugf("🔄 background sync from %s: received %d events (all duplicates)", neighbor, len(events))
	}
}
