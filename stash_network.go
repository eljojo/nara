package nara

import (
	"time"

	"github.com/sirupsen/logrus"
)

// stashMaintenance manages confidant selection and inventory
func (network *Network) stashMaintenance() {
	if network.stashService != nil {
		network.stashService.RunMaintenanceLoop(network.stashDistributeTrigger)
	}
}

// pushStashToOwner pushes a stash back to its owner via HTTP POST to /stash/push.
// This is called during hey-there recovery or stash-refresh requests.
// Runs the push in a background goroutine with optional delay.
func (network *Network) pushStashToOwner(targetName string, delay time.Duration) {
	if network.stashService != nil {
		network.stashService.PushStashToOwner(targetName, delay)
	}
}

// getPeerInfo gathers metadata about peers for confidant selection
func (network *Network) getPeerInfo(names []string) []PeerInfo {
	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	peers := make([]PeerInfo, 0, len(names))
	for _, name := range names {
		nara, ok := network.Neighbourhood[name]
		if !ok {
			continue
		}

		nara.mu.Lock()
		memoryMode := MemoryModeMedium // Default
		if modeStr := nara.Status.MemoryMode; modeStr != "" {
			memoryMode = MemoryMode(modeStr)
		}

		// Calculate uptime approximation
		// Use the time since last event as a proxy for reliability
		uptimeSecs := int64(0)
		if state := network.local.Projections.OnlineStatus().GetState(name); state != nil {
			if state.Status == "ONLINE" {
				// Longer time online (in this session) = more reliable
				elapsedNanos := time.Now().UnixNano() - state.LastEventTime
				uptimeSecs = elapsedNanos / 1e9 // Convert to seconds
				if uptimeSecs < 0 {
					uptimeSecs = 0
				}
			}
		}
		nara.mu.Unlock()

		peers = append(peers, PeerInfo{
			Name:       name,
			MemoryMode: memoryMode,
			UptimeSecs: uptimeSecs,
		})
	}

	return peers
}

// initStashService initializes the unified stash service.
// This can be called by tests after manually setting up stashManager.
func (network *Network) initStashService() {
	network.stashService = NewStashService(
		network.stashManager,
		network.confidantStore,
		network.stashSyncTracker,
		newStashServiceContext(network),
	)
}

// reactToConfidantOffline is called immediately when we detect a nara went offline
// If they're one of our confidants, remove them and trigger immediate replacement search
func (network *Network) reactToConfidantOffline(name string) {
	if network.stashService != nil {
		network.stashService.ReactToConfidantOffline(name)
	}
}

// broadcastStashRefresh broadcasts an MQTT event asking confidants to push stash back
// This is used during boot if hey-there didn't trigger recovery
func (network *Network) broadcastStashRefresh() {
	if network.Mqtt == nil || !network.Mqtt.IsConnected() {
		logrus.Debugf("📦 Cannot broadcast stash-refresh: MQTT not connected")
		return
	}

	if network.stashService == nil {
		return
	}

	// Create a stash-refresh event (similar to hey-there)
	event := map[string]interface{}{
		"from":      network.meName(),
		"timestamp": time.Now().Unix(),
	}

	// Broadcast on MQTT
	topic := "nara/plaza/stash_refresh"
	network.postEvent(topic, event)
	logrus.Infof("📦 Broadcast stash-refresh request")
}
