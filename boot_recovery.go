package nara

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// BootRecoveryTargetEvents is the target number of events to fetch on boot
const BootRecoveryTargetEvents = 50000

// getNeighborsForBootRecovery returns online neighbors.
// Only re-discovers if no peers are known (peers are typically discovered at connect time in gossip mode).
func (network *Network) getNeighborsForBootRecovery() []string {
	online := network.NeighbourhoodOnlineNames()

	// In gossip-only mode, only re-discover if we have no peers
	// (peers are normally discovered immediately after tsnet connects)
	if len(online) == 0 && network.TransportMode == TransportGossip {
		logrus.Debug("📡 Boot recovery: no peers known, triggering mesh discovery...")
		if network.tsnetMesh != nil {
			network.discoverMeshPeers()
			online = network.NeighbourhoodOnlineNames()
		}
	}

	return online
}

// bootRecovery requests social events from neighbors after boot
func (network *Network) bootRecovery() {
	// Signal completion when done (allows formOpinion to proceed)
	defer func() {
		close(network.bootRecoveryDone)
		logrus.Debug("📦 boot recovery complete, signaling formOpinion to proceed")
	}()

	// In gossip mode, check if peers are already discovered (from immediate discovery at connect)
	// If so, skip the 30s wait and start syncing right away
	var online []string
	if network.TransportMode == TransportGossip {
		online = network.NeighbourhoodOnlineNames()
		if len(online) > 0 {
			logrus.Printf("📦 Gossip mode: %d peers already discovered, starting boot recovery immediately", len(online))
		}
	}

	// Wait for initial neighbor discovery (only if we don't have peers yet)
	if len(online) == 0 {
		select {
		case <-time.After(30 * time.Second):
			// continue
		case <-network.ctx.Done():
			return
		}

		// Retry up to 3 times with backoff if no neighbors found
		for attempt := 0; attempt < 3; attempt++ {
			online = network.getNeighborsForBootRecovery()
			if len(online) > 0 {
				break
			}
			select {
			case <-network.ctx.Done():
				return
			default:
			}
			if attempt < 2 {
				waitTime := time.Duration(30*(attempt+1)) * time.Second
				logrus.Printf("📦 no neighbors for boot recovery, retrying in %v...", waitTime)
				select {
				case <-time.After(waitTime):
					// continue
				case <-network.ctx.Done():
					return
				}
			}
		}
	}

	if len(online) == 0 {
		logrus.Printf("📦 no neighbors for boot recovery after retries")
		return
	}

	// Try mesh HTTP recovery first
	if network.tsnetMesh != nil {
		network.bootRecoveryViaMesh(online)
	} else {
		// Fall back to MQTT-based recovery
		network.bootRecoveryViaMQTT(online)
	}

	// After regular boot recovery, sync checkpoint timeline from network
	// This recovers the full historical record of the network
	if network.tsnetMesh != nil {
		network.syncCheckpointsFromNetwork(online)
	}
}

// bootRecoveryViaMesh uses direct HTTP to sync events from neighbors (parallelized)
func (network *Network) bootRecoveryViaMesh(online []string) {
	// Get all known subjects (naras)
	subjects := append(online, network.meName())

	// Collect ALL mesh-enabled neighbors (no limit)
	var meshNeighbors []struct {
		name string
		ip   string
	}

	maxMeshNeighbors := len(online)
	if network.isShortMemoryMode() {
		maxMeshNeighbors = 4
	}
	for _, name := range online {
		ip := network.getMeshIPForNara(name)
		if ip != "" {
			meshNeighbors = append(meshNeighbors, struct {
				name string
				ip   string
			}{name, ip})
		}
	}
	if len(meshNeighbors) > maxMeshNeighbors {
		meshNeighbors = meshNeighbors[:maxMeshNeighbors]
	}

	if len(meshNeighbors) == 0 {
		logrus.Printf("📦 no mesh-enabled neighbors for boot recovery, falling back to MQTT")
		network.bootRecoveryViaMQTT(online)
		return
	}

	totalSlices := len(meshNeighbors)
	// Divide target across neighbors
	eventsPerNeighbor := BootRecoveryTargetEvents / totalSlices
	if eventsPerNeighbor < 100 {
		eventsPerNeighbor = 100 // minimum events per neighbor
	}

	logrus.Printf("📦 boot recovery via mesh: syncing from %d neighbors in parallel (~%d events each)", totalSlices, eventsPerNeighbor)

	// Use tsnet HTTP client to route through Tailscale
	client := network.getMeshHTTPClient()
	if client == nil {
		logrus.Warnf("📦 mesh HTTP client unavailable for boot recovery")
		return
	}

	// Fetch from all neighbors in parallel
	type syncResult struct {
		name         string
		sliceIndex   int
		events       []SyncEvent
		respVerified bool
		success      bool
	}
	results := make(chan syncResult, len(meshNeighbors))
	var wg sync.WaitGroup

	// Limit concurrent requests to avoid overwhelming the network
	maxConcurrent := 10
	if network.isShortMemoryMode() {
		maxConcurrent = 3
	}
	sem := make(chan struct{}, maxConcurrent)

	for i, neighbor := range meshNeighbors {
		wg.Add(1)
		sem <- struct{}{} // Acquire semaphore

		go func(idx int, n struct{ name, ip string }) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore

			events, respVerified := network.fetchSyncEventsFromMesh(client, n.ip, n.name, subjects, idx, totalSlices, eventsPerNeighbor)
			results <- syncResult{
				name:         n.name,
				sliceIndex:   idx,
				events:       events,
				respVerified: respVerified,
				success:      len(events) > 0 || respVerified, // success if we got events or at least verified (empty slice is OK)
			}
		}(i, neighbor)
	}

	// Close results channel when all fetches complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Process results as they arrive (merging is thread-safe)
	var totalMerged int
	var failedSlices []int
	respondedNeighbors := make(map[string]bool)

	for result := range results {
		respondedNeighbors[result.name] = result.success
		if len(result.events) > 0 {
			added, warned := network.MergeSyncEventsWithVerification(result.events)
			totalMerged += added
			if added > 0 && network.logService != nil {
				network.logService.BatchMeshSync(result.name, added)
			}
			if warned > 0 {
				logrus.Debugf("📦 mesh sync from %s: %d events with %d verification warnings", result.name, added, warned)
			}
		} else if !result.success {
			// Track failed slices for retry
			failedSlices = append(failedSlices, result.sliceIndex)
			logrus.Printf("📦 mesh sync from %s failed (slice %d), will retry with another neighbor", result.name, result.sliceIndex)
		}
	}

	// Retry failed slices with different neighbors
	if len(failedSlices) > 0 {
		// Find neighbors that succeeded (they're available for retry)
		var availableNeighbors []struct{ name, ip string }
		for _, n := range meshNeighbors {
			if respondedNeighbors[n.name] {
				availableNeighbors = append(availableNeighbors, n)
			}
		}

		if len(availableNeighbors) > 0 {
			logrus.Printf("📦 retrying %d failed slices with %d available neighbors", len(failedSlices), len(availableNeighbors))

			retryResults := make(chan syncResult, len(failedSlices))
			var retryWg sync.WaitGroup

			for i, sliceIdx := range failedSlices {
				// Pick a different neighbor for each failed slice (round-robin)
				neighbor := availableNeighbors[i%len(availableNeighbors)]

				retryWg.Add(1)
				sem <- struct{}{}

				go func(idx int, n struct{ name, ip string }) {
					defer retryWg.Done()
					defer func() { <-sem }()

					logrus.Printf("📦 retry: asking %s for slice %d", n.name, idx)
					events, respVerified := network.fetchSyncEventsFromMesh(client, n.ip, n.name, subjects, idx, totalSlices, eventsPerNeighbor)
					retryResults <- syncResult{
						name:         n.name,
						sliceIndex:   idx,
						events:       events,
						respVerified: respVerified,
						success:      len(events) > 0,
					}
				}(sliceIdx, neighbor)
			}

			go func() {
				retryWg.Wait()
				close(retryResults)
			}()

			for result := range retryResults {
				if len(result.events) > 0 {
					added, warned := network.MergeSyncEventsWithVerification(result.events)
					totalMerged += added
					if added > 0 && network.logService != nil {
						network.logService.BatchMeshSync(result.name, added)
					}
					if warned > 0 {
						logrus.Debugf("📦 retry mesh sync from %s: %d events with %d verification warnings", result.name, added, warned)
					}
				} else {
					logrus.Debugf("📦 retry mesh sync from %s (slice %d) failed", result.name, result.sliceIndex)
				}
			}
		} else {
			logrus.Debugf("📦 no available neighbors for retry, %d slices remain unsynced", len(failedSlices))
		}
	}

	if network.logService != nil {
		network.logService.Info(CategoryMesh, "boot recovery complete: %d events total", totalMerged)
	}

	// Seed AvgPingRTT from recovered ping observations
	network.seedAvgPingRTTFromHistory()
}

// TODO: Add integration test for fetchSyncEventsFromMesh using httptest.NewServer pattern
// See TestIntegration_CheckpointSync for reference on how to inject HTTP mux/client
// This should test the full HTTP request/response flow for sync event fetching,
// including signature verification, slice-based fetching, and error handling
//
// fetchSyncEventsFromMesh fetches unified SyncEvents with signature verification
func (network *Network) fetchSyncEventsFromMesh(client *http.Client, meshIP, name string, subjects []string, sliceIndex, sliceTotal, maxEvents int) ([]SyncEvent, bool) {
	// Build request
	reqBody := SyncRequest{
		From:       network.meName(),
		Subjects:   subjects,
		SinceTime:  0, // get all events
		SliceIndex: sliceIndex,
		SliceTotal: sliceTotal,
		MaxEvents:  maxEvents,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		logrus.Warnf("📦 failed to marshal mesh sync request: %v", err)
		return nil, false
	}

	// Make HTTP request to neighbor's mesh endpoint
	url := network.buildMeshURLFromIP(meshIP, "/events/sync")
	// Boot sync requests are batched via the summary log, not individual lines
	req, err := http.NewRequest("POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		logrus.Warnf("📦 failed to create mesh sync request: %v", err)
		return nil, false
	}
	req.Header.Set("Content-Type", "application/json")

	// Add mesh authentication headers (Ed25519 signature)
	network.AddMeshAuthHeaders(req)

	resp, err := client.Do(req)
	if err != nil {
		logrus.Warnf("📦 mesh sync from %s failed: %v", name, err)
		return nil, false
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		logrus.Warnf("📦 mesh sync from %s rejected our auth (they may not know us yet)", name)
		return nil, false
	}

	if resp.StatusCode != http.StatusOK {
		logrus.Warnf("📦 mesh sync from %s returned status %d", name, resp.StatusCode)
		return nil, false
	}

	// Read and verify response body
	body, verified := network.VerifyMeshResponseBody(resp)
	if !verified {
		logrus.Warnf("📦 mesh response from %s failed signature verification", name)
		// Continue anyway - response might be valid but from a nara we don't know yet
	}

	// Parse response
	var response SyncResponse
	if err := json.Unmarshal(body, &response); err != nil {
		logrus.Warnf("📦 failed to decode mesh sync response from %s: %v", name, err)
		return nil, false
	}

	// Also verify the inner signature for extra assurance
	if response.Signature != "" && !verified {
		// Look up sender's public key from our neighborhood
		// Note: Short-circuit evaluation ensures nara.Status is only accessed if nara != nil
		network.local.mu.Lock()
		nara := network.Neighbourhood[name]
		network.local.mu.Unlock()
		if nara != nil {
			nara.mu.Lock()
			publicKey := nara.Status.PublicKey
			nara.mu.Unlock()
			if publicKey != "" {
				pubKey, err := ParsePublicKey(publicKey)
				if err == nil {
					if response.VerifySignature(pubKey) {
						verified = true
					} else {
						logrus.Warnf("📦 inner signature verification failed for %s", name)
					}
				}
			}
		}
	}

	return response.Events, verified
}

// bootRecoveryViaMQTT uses MQTT ledger requests to sync events (fallback)
func (network *Network) bootRecoveryViaMQTT(online []string) {
	// Get all known subjects (naras)
	subjects := append(online, network.meName())

	// Pick up to 5 neighbors to query
	maxNeighbors := 5
	if network.isShortMemoryMode() {
		maxNeighbors = 2
	}
	if len(online) < maxNeighbors {
		maxNeighbors = len(online)
	}

	// Partition subjects across neighbors
	partitions := PartitionSubjects(subjects, maxNeighbors)

	logrus.Printf("📦 boot recovery via MQTT: requesting events from %d neighbors", maxNeighbors)

	for i := 0; i < maxNeighbors; i++ {
		neighbor := online[i]
		partition := partitions[i]

		if len(partition) == 0 {
			continue
		}

		req := LedgerRequest{
			From:     network.meName(),
			Subjects: partition,
		}

		topic := "nara/ledger/" + neighbor + "/request"
		network.postEvent(topic, req)
		logrus.Infof("📦 requested events about %d subjects from %s", len(partition), neighbor)
	}
}

// RequestLedgerSync manually triggers a sync request to a specific neighbor
func (network *Network) RequestLedgerSync(neighbor string, subjects []string) {
	if network.ReadOnly {
		return
	}

	req := LedgerRequest{
		From:     network.meName(),
		Subjects: subjects,
	}

	topic := "nara/ledger/" + neighbor + "/request"
	network.postEvent(topic, req)
}
