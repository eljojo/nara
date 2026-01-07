package nara

import (
	"math/rand"
	"sort"
	"time"

	"github.com/sirupsen/logrus"
)

// coordinateMaintenance runs periodic pings and coordinate updates
// Self-throttles to max 2 pings per minute regardless of network size
func (network *Network) coordinateMaintenance() {
	// Wait for mesh to be ready
	time.Sleep(30 * time.Second)

	config := DefaultVivaldiConfig()
	ticker := time.NewTicker(30 * time.Second) // 2 pings per minute
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if network.ReadOnly || network.tsnetMesh == nil {
				continue
			}

			// Select one target to ping (self-throttling: 1 ping per tick)
			target := network.selectPingTarget()
			if target == "" {
				continue
			}

			network.pingAndUpdateCoordinates(target, config)
		}
	}
}

// pingTarget holds info about a potential ping target
type pingTarget struct {
	name     string
	meshIP   string
	priority float64 // higher = more important to ping
}

// selectPingTarget chooses which nara to ping based on priority
// Priority order:
// 1. New naras (never pinged) - priority 1000
// 2. High-error naras (uncertain position) - priority based on error
// 3. Stale measurements (haven't pinged recently) - priority based on staleness
// 4. Random sampling for coverage
func (network *Network) selectPingTarget() string {
	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	now := time.Now().Unix()
	var targets []pingTarget

	for name, nara := range network.Neighbourhood {
		obs := nara.getObservation(name)
		if obs.Online != "ONLINE" {
			continue
		}

		nara.mu.Lock()
		meshIP := nara.Status.MeshIP
		coords := nara.Status.Coordinates
		nara.mu.Unlock()

		if meshIP == "" {
			continue // Can't ping without mesh IP
		}

		// Get our observation of this peer
		myObs := network.local.getObservationLocked(name)

		// Calculate priority
		var priority float64

		// Never pinged = highest priority (1000)
		if myObs.LastPingTime == 0 {
			priority = 1000 + rand.Float64()*10 // Add jitter
		} else {
			// Calculate staleness (seconds since last ping)
			staleness := float64(now - myObs.LastPingTime)

			// High error = needs more measurements
			errorBonus := 0.0
			if coords != nil && coords.Error > 0.5 {
				errorBonus = coords.Error * 100
			}

			// Base priority from staleness + error bonus
			priority = staleness + errorBonus + rand.Float64()*10
		}

		targets = append(targets, pingTarget{
			name:     name,
			meshIP:   meshIP,
			priority: priority,
		})
	}

	if len(targets) == 0 {
		return ""
	}

	// Sort by priority (highest first)
	sort.Slice(targets, func(i, j int) bool {
		return targets[i].priority > targets[j].priority
	})

	return targets[0].name
}

// pingAndUpdateCoordinates pings a peer and updates our coordinates
func (network *Network) pingAndUpdateCoordinates(targetName string, config VivaldiConfig) {
	network.local.mu.Lock()
	nara, exists := network.Neighbourhood[targetName]
	network.local.mu.Unlock()

	if !exists {
		return
	}

	nara.mu.Lock()
	meshIP := nara.Status.MeshIP
	peerCoords := nara.Status.Coordinates
	nara.mu.Unlock()

	if meshIP == "" {
		return
	}

	// Ping the peer
	rtt, err := network.tsnetMesh.Ping(meshIP, 5*time.Second)
	if err != nil {
		logrus.Debugf("📍 Ping to %s failed: %v", targetName, err)
		return
	}

	rttMs := float64(rtt.Milliseconds())
	logrus.Debugf("📍 Ping to %s: %.2fms", targetName, rttMs)

	// Update our coordinates if peer has coordinates
	network.local.Me.mu.Lock()
	myCoords := network.local.Me.Status.Coordinates
	if myCoords != nil && peerCoords != nil && peerCoords.IsValid() {
		myCoords.Update(peerCoords, rttMs, config)
	}
	network.local.Me.mu.Unlock()

	// Update observation with RTT data
	obs := network.local.getObservation(targetName)
	obs.LastPingRTT = rttMs
	obs.LastPingTime = time.Now().Unix()

	// Update exponential moving average (alpha = 0.3)
	if obs.AvgPingRTT == 0 {
		obs.AvgPingRTT = rttMs
	} else {
		obs.AvgPingRTT = 0.3*rttMs + 0.7*obs.AvgPingRTT
	}

	network.local.setObservation(targetName, obs)

	// Record to unified sync ledger for network-wide propagation
	// Uses replace strategy: one ping per observer→target pair
	// Sign the event so others can verify it came from us
	if network.local.SyncLedger != nil {
		network.local.SyncLedger.AddSignedPingObservationWithReplace(
			network.meName(), targetName, rttMs,
			network.meName(), network.local.Keypair,
		)
	}
}

// getCoordinatesForPeer returns the coordinates for a peer by name
// Used by ApplyProximityToClout
func (network *Network) getCoordinatesForPeer(name string) *NetworkCoordinate {
	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	nara, exists := network.Neighbourhood[name]
	if !exists {
		return nil
	}

	nara.mu.Lock()
	defer nara.mu.Unlock()

	return nara.Status.Coordinates
}
