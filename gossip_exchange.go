package nara

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/eljojo/nara/types"
)

// gossipForever periodically exchanges zines with random mesh neighbors
// Runs in background, spreading events organically through the network
func (network *Network) gossipForever() {
	// Wait for mesh to be ready
	select {
	case <-time.After(30 * time.Second):
		// continue
	case <-network.ctx.Done():
		return
	}

	for {
		interval := network.gossipInterval()
		select {
		case <-network.ctx.Done():
			return
		case <-time.After(interval):
			if network.ReadOnly || network.tsnetMesh == nil {
				continue
			}

			// Skip if in MQTT-only mode
			if network.TransportMode == TransportMQTT {
				continue
			}

			network.performGossipRound()
		}
	}
}

// gossipInterval returns the time to wait between gossip rounds
// Personality-based: 30-300 seconds, similar to chattiness
func (network *Network) gossipInterval() time.Duration {
	baseInterval := network.local.chattinessRate(30, 300)
	return time.Duration(baseInterval) * time.Second
}

// performGossipRound creates a zine and exchanges it with random neighbors
func (network *Network) performGossipRound() {
	// Create our zine
	zine := network.createZine()
	if zine == nil {
		logrus.Infof("📰 No events to gossip")
		return
	}

	// Select targets
	targets := network.selectGossipTargets()
	if len(targets) == 0 {
		logrus.Infof("📰 No gossip targets available")
		return
	}

	// Count events by type for diagnostics
	typeCounts := make(map[string]int)
	for _, e := range zine.Events {
		typeCounts[e.Service]++
	}
	// TODO: fix casting of targets to strings
	// logrus.Infof("📰 Gossiping with %d neighbors [%s] (zine has %d events: %v)", len(targets), strings.Join(targets, ", "), len(zine.Events), typeCounts)

	// Exchange zines with each target
	var wg sync.WaitGroup
	for _, targetName := range targets {
		wg.Add(1)
		go func(name types.NaraName) {
			defer wg.Done()
			network.exchangeZine(name, zine)
		}(targetName)
	}
	wg.Wait()
}

// exchangeZine sends our zine to a neighbor and receives theirs back
func (network *Network) exchangeZine(targetName types.NaraName, myZine *Zine) {
	// Resolve nara name to ID
	naraID := network.getNaraIDByName(targetName)
	if naraID == "" {
		logrus.Debugf("📰 Cannot exchange zine with %s: could not resolve nara ID", targetName)
		return
	}

	// Send our zine and receive theirs via mesh client
	ctx, cancel := context.WithTimeout(network.ctx, 15*time.Second)
	defer cancel()

	theirZine, err := network.meshClient.PostGossipZine(ctx, naraID, myZine)
	if err != nil {
		logrus.Infof("📰 Failed to exchange zine with %s: %v", targetName, err)
		return
	}

	// Verify signature
	pubKey := network.resolvePublicKeyForNara(targetName)
	if len(pubKey) > 0 && !VerifyZine(theirZine, pubKey) {
		logrus.Warnf("📰 Invalid zine signature from %s, rejecting", targetName)
		return
	}

	// Merge their events into our ledger
	added, _ := network.MergeSyncEventsWithVerification(theirZine.Events)
	if added > 0 && network.logService != nil {
		network.logService.BatchGossipMerge(targetName, added)
	}

	// Mark peer as online - successful zine exchange proves they're reachable
	// Their events in the zine already prove they're active, no need for seen event
	network.recordObservationOnlineNara(targetName, theirZine.CreatedAt)
}

// selectGossipTargets selects random mesh-enabled neighbors for gossip
// Returns 3-5 random online naras with mesh connectivity
func (network *Network) selectGossipTargets() []types.NaraName {
	online := network.NeighbourhoodOnlineNames()

	// Filter to mesh-enabled only
	var meshEnabled []types.NaraName
	for _, name := range online {
		if network.hasMeshConnectivity(name) {
			meshEnabled = append(meshEnabled, name)
		}
	}

	if len(meshEnabled) == 0 {
		return nil
	}

	// Select 3-5 random targets (short memory: 1-2)
	targetCount := 3 + rand.Intn(3) // Random between 3-5
	if network.isShortMemoryMode() {
		targetCount = 1 + rand.Intn(2)
	}
	if targetCount > len(meshEnabled) {
		targetCount = len(meshEnabled)
	}

	// Shuffle and take first N
	shuffled := make([]types.NaraName, len(meshEnabled))
	copy(shuffled, meshEnabled)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	return shuffled[:targetCount]
}
