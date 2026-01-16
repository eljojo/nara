package nara

import (
	"bytes"
	"context"
	"encoding/json"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
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
		go func(name NaraName) {
			defer wg.Done()
			network.exchangeZine(name, zine)
		}(targetName)
	}
	wg.Wait()
}

// exchangeZine sends our zine to a neighbor and receives theirs back
// TODO: Migrate to MeshClient.PostGossipZine() method to reduce code duplication and improve maintainability
func (network *Network) exchangeZine(targetName NaraName, myZine *Zine) {
	// Determine URL
	url := network.buildMeshURL(targetName, "/gossip/zine")
	if url == "" {
		return
	}

	// Encode our zine
	zineBytes, err := json.Marshal(myZine)
	if err != nil {
		logrus.Warnf("📰 Failed to encode zine for %s: %v", targetName, err)
		return
	}

	// Create request with 30s timeout to prevent goroutine leaks
	ctx, cancel := context.WithTimeout(network.ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(zineBytes))
	if err != nil {
		logrus.Warnf("📰 Failed to create zine request for %s: %v", targetName, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	// Add mesh authentication headers (Ed25519 signature)
	network.AddMeshAuthHeaders(req)

	client := network.getMeshHTTPClient()
	if client == nil {
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		logrus.Infof("📰 Failed to exchange zine with %s: %v", targetName, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logrus.Infof("📰 Zine exchange with %s failed: status %d", targetName, resp.StatusCode)
		return
	}

	// Decode their zine
	var theirZine Zine
	if err := json.NewDecoder(resp.Body).Decode(&theirZine); err != nil {
		logrus.Warnf("📰 Failed to decode zine from %s: %v", targetName, err)
		return
	}

	// Verify signature
	pubKey := network.resolvePublicKeyForNara(targetName)
	if len(pubKey) > 0 && !VerifyZine(&theirZine, pubKey) {
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
func (network *Network) selectGossipTargets() []NaraName {
	online := network.NeighbourhoodOnlineNames()

	// Filter to mesh-enabled only
	var meshEnabled []NaraName
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
	shuffled := make([]NaraName, len(meshEnabled))
	copy(shuffled, meshEnabled)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	return shuffled[:targetCount]
}
