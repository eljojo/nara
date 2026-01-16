package nara

import (
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/eljojo/nara/types"
)

// BootRecoveryTargetEvents is the target number of events to fetch on boot
const BootRecoveryTargetEvents = 50000

// getNeighborsForBootRecovery returns online neighbors.
// Only re-discovers if no peers are known (peers are typically discovered at connect time in gossip mode).
func (network *Network) getNeighborsForBootRecovery() []types.NaraName {
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
	var online []types.NaraName
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

// meshNeighbor holds info needed to communicate with a neighbor via mesh
type meshNeighbor struct {
	name types.NaraName
	id   types.NaraID
	ip   string
}

// bootRecoveryViaMesh uses direct HTTP to sync events from neighbors (parallelized)
func (network *Network) bootRecoveryViaMesh(online []types.NaraName) {
	// Collect ALL mesh-enabled neighbors
	var meshNeighbors []meshNeighbor

	maxMeshNeighbors := len(online)
	if network.isShortMemoryMode() {
		maxMeshNeighbors = 4
	}
	for _, name := range online {
		ip, naraID := network.getMeshInfoForNara(name)
		if ip != "" && naraID != "" {
			meshNeighbors = append(meshNeighbors, meshNeighbor{name, naraID, ip})
			// Register peer for mesh client lookups
			if network.meshClient != nil {
				network.meshClient.RegisterPeerIP(naraID, ip)
			}
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

	// Try new sample mode API first (capacity-based organic memory)
	success := network.bootRecoveryViaMeshSampleMode(online, meshNeighbors)
	if success {
		return
	}

	// TODO: Remove this fallback after all naras have upgraded to support Mode: "sample"
	// Fallback period: ~6 months from 2026-01
	logrus.Warn("📦 Sample mode failed, falling back to legacy slicing API")
	network.bootRecoveryViaMeshLegacy(online, meshNeighbors)
}

// bootRecoveryViaMeshSampleMode uses the new Mode: "sample" API for organic hazy memory reconstruction
func (network *Network) bootRecoveryViaMeshSampleMode(online []types.NaraName, meshNeighbors []meshNeighbor) bool {
	// Determine boot recovery target based on memory mode
	// These targets represent the "capacity" we want to fill during boot recovery
	var capacity, pageSize int
	switch network.local.MemoryProfile.Mode {
	case MemoryModeShort:
		capacity = 5000 // ~5k events
		pageSize = 1000 // 1k per call
	case MemoryModeHog:
		capacity = 80000 // ~80k events
		pageSize = 5000  // 5k per call
	default: // MemoryModeMedium
		capacity = 50000 // ~50k events
		pageSize = 5000  // 5k per call
	}

	// Calculate number of API calls needed
	callsNeeded := capacity / pageSize

	logrus.Printf("📦 boot recovery via mesh (sample mode): capacity=%d, page=%d, calls=%d across %d neighbors",
		capacity, pageSize, callsNeeded, len(meshNeighbors))

	// Distribute calls across ALL available neighbors (round-robin)
	type callTask struct {
		neighbor meshNeighbor
		callNum  int
	}
	var tasks []callTask
	for i := 0; i < callsNeeded; i++ {
		neighborIdx := i % len(meshNeighbors)
		tasks = append(tasks, callTask{
			neighbor: meshNeighbors[neighborIdx],
			callNum:  i,
		})
	}

	// Execute calls in parallel with concurrency limit
	type syncResult struct {
		neighbor types.NaraName
		callNum  int
		events   []SyncEvent
		success  bool
		err      error
	}
	results := make(chan syncResult, len(tasks))
	var wg sync.WaitGroup

	maxConcurrent := 10
	if network.isShortMemoryMode() {
		maxConcurrent = 3
	}
	sem := make(chan struct{}, maxConcurrent)

	for _, task := range tasks {
		wg.Add(1)
		sem <- struct{}{} // Acquire semaphore

		go func(t callTask) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore

			events, err := network.meshClient.FetchSyncEvents(network.ctx, t.neighbor.id, SyncRequest{
				Mode:       "sample",
				SampleSize: pageSize,
			})

			results <- syncResult{
				neighbor: t.neighbor.name,
				callNum:  t.callNum,
				events:   events,
				success:  err == nil,
				err:      err,
			}
		}(task)
	}

	// Close results channel when all fetches complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Process results as they arrive
	var totalMerged int
	var failedCalls int
	respondedNeighbors := make(map[types.NaraName]bool)

	for result := range results {
		if !result.success {
			failedCalls++
			logrus.Warnf("📦 Sample call %d to %s failed: %v", result.callNum, result.neighbor, result.err)
			continue
		}

		respondedNeighbors[result.neighbor] = true

		// Merge events with verification
		added, _ := network.MergeSyncEventsWithVerification(result.events)
		totalMerged += added

		logrus.Debugf("📦 Sample call %d from %s: received %d events, merged %d",
			result.callNum, result.neighbor, len(result.events), added)
	}

	// If too many calls failed, consider it a failure (fallback to legacy)
	if failedCalls > callsNeeded/2 {
		logrus.Warnf("📦 Sample mode: %d/%d calls failed, falling back to legacy API", failedCalls, callsNeeded)
		return false
	}

	logrus.Printf("📦 boot recovery (sample mode) complete: merged %d events from %d neighbors",
		totalMerged, len(respondedNeighbors))

	// Trigger projections
	if network.local.Projections != nil {
		network.local.Projections.Trigger()
	}

	return true
}

// bootRecoveryViaMeshLegacy uses the old slicing API for backward compatibility
// TODO: Remove after ~6 months (2026-07) when all naras support Mode: "sample"
func (network *Network) bootRecoveryViaMeshLegacy(online []types.NaraName, meshNeighbors []meshNeighbor) {
	// Get all known subjects (naras)
	subjects := append(online, network.meName())

	totalSlices := len(meshNeighbors)
	// Divide target across neighbors
	eventsPerNeighbor := BootRecoveryTargetEvents / totalSlices
	if eventsPerNeighbor < 100 {
		eventsPerNeighbor = 100 // minimum events per neighbor
	}

	logrus.Printf("📦 boot recovery via mesh: syncing from %d neighbors in parallel (~%d events each)", totalSlices, eventsPerNeighbor)

	// Fetch from all neighbors in parallel
	type syncResult struct {
		name         types.NaraName
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

		go func(idx int, n meshNeighbor) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore

			events, respVerified := network.fetchSyncEventsFromMesh(n.id, n.name, subjects, idx, totalSlices, eventsPerNeighbor)
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
	respondedNeighbors := make(map[types.NaraName]bool)

	for result := range results {
		respondedNeighbors[result.name] = result.success
		if len(result.events) > 0 {
			added, warned := network.MergeSyncEventsWithVerification(result.events)
			totalMerged += added
			if added > 0 && network.logService != nil {
				network.logService.BatchMeshSync(result.name, added)
			}
			if warned > 0 {
				logrus.Debugf("📦 mesh sync from %s: %d events with %d verification warnings", result.name.String(), added, warned)
			}
		} else if !result.success {
			// Track failed slices for retry
			failedSlices = append(failedSlices, result.sliceIndex)
			logrus.Printf("📦 mesh sync from %s failed (slice %d), will retry with another neighbor", result.name.String(), result.sliceIndex)
		}
	}

	// Retry failed slices with different neighbors
	if len(failedSlices) > 0 {
		// Find neighbors that succeeded (they're available for retry)
		var availableNeighbors []meshNeighbor
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

				go func(idx int, n meshNeighbor) {
					defer retryWg.Done()
					defer func() { <-sem }()

					logrus.Printf("📦 retry: asking %s for slice %d", n.name, idx)
					events, respVerified := network.fetchSyncEventsFromMesh(n.id, n.name, subjects, idx, totalSlices, eventsPerNeighbor)
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
						logrus.Debugf("📦 retry mesh sync from %s: %d events with %d verification warnings", result.name.String(), added, warned)
					}
				} else {
					logrus.Debugf("📦 retry mesh sync from %s (slice %d) failed", result.name.String(), result.sliceIndex)
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
func (network *Network) fetchSyncEventsFromMesh(naraID types.NaraID, name types.NaraName, subjects []types.NaraName, sliceIndex, sliceTotal, maxEvents int) ([]SyncEvent, bool) {
	// Use MeshClient for clean, reusable mesh HTTP communication
	events, err := network.meshClient.FetchSyncEvents(network.ctx, naraID, SyncRequest{
		Subjects:   subjects,
		SinceTime:  0,
		SliceIndex: sliceIndex,
		SliceTotal: sliceTotal,
		MaxEvents:  maxEvents,
	})

	if err != nil {
		logrus.Warnf("📦 mesh sync from %s failed: %v", name.String(), err)
		return nil, false
	}

	// Parse response for additional verification
	// TODO: Move this logic into MeshClient once we refactor response verification
	response := SyncResponse{Events: events}
	verified := false

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
func (network *Network) bootRecoveryViaMQTT(online []types.NaraName) {
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

		topic := "nara/ledger/" + neighbor.String() + "/request"
		network.postEvent(topic, req)
		logrus.Infof("📦 requested events about %d subjects from %s", len(partition), neighbor)
	}
}

// RequestLedgerSync manually triggers a sync request to a specific neighbor
func (network *Network) RequestLedgerSync(neighbor types.NaraName, subjects []types.NaraName) {
	if network.ReadOnly {
		return
	}

	req := LedgerRequest{
		From:     network.meName(),
		Subjects: subjects,
	}

	topic := "nara/ledger/" + neighbor.String() + "/request"
	network.postEvent(topic, req)
}
