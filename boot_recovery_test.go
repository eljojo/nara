package nara

import (
	"fmt"
	"sync"
	"testing"
)

// TestBootRecovery_RetryLogic tests the retry selection algorithm
func TestBootRecovery_RetryLogic(t *testing.T) {
	// Simulate the retry logic without actual HTTP calls
	type syncResult struct {
		name       string
		sliceIndex int
		success    bool
	}

	// Simulate initial results: slice 1 failed
	initialResults := []syncResult{
		{name: "neighbor-0", sliceIndex: 0, success: true},
		{name: "neighbor-1", sliceIndex: 1, success: false}, // Failed
		{name: "neighbor-2", sliceIndex: 2, success: true},
		{name: "neighbor-3", sliceIndex: 3, success: false}, // Failed
		{name: "neighbor-4", sliceIndex: 4, success: true},
	}

	// Track failed slices and available neighbors
	var failedSlices []int
	respondedNeighbors := make(map[string]bool)

	for _, result := range initialResults {
		respondedNeighbors[result.name] = result.success
		if !result.success {
			failedSlices = append(failedSlices, result.sliceIndex)
		}
	}

	// Verify failed slices are identified
	if len(failedSlices) != 2 {
		t.Errorf("expected 2 failed slices, got %d", len(failedSlices))
	}
	if failedSlices[0] != 1 || failedSlices[1] != 3 {
		t.Errorf("expected failed slices [1, 3], got %v", failedSlices)
	}

	// Find available neighbors for retry
	allNeighbors := []struct{ name, ip string }{
		{"neighbor-0", "ip0"},
		{"neighbor-1", "ip1"},
		{"neighbor-2", "ip2"},
		{"neighbor-3", "ip3"},
		{"neighbor-4", "ip4"},
	}

	var availableNeighbors []struct{ name, ip string }
	for _, n := range allNeighbors {
		if respondedNeighbors[n.name] {
			availableNeighbors = append(availableNeighbors, n)
		}
	}

	// Verify available neighbors
	if len(availableNeighbors) != 3 {
		t.Errorf("expected 3 available neighbors, got %d", len(availableNeighbors))
	}

	// Verify the round-robin selection for retry
	for i, sliceIdx := range failedSlices {
		selectedNeighbor := availableNeighbors[i%len(availableNeighbors)]
		t.Logf("Retry slice %d with neighbor %s", sliceIdx, selectedNeighbor.name)

		// Verify we're not selecting the failed neighbor
		if sliceIdx == 1 && selectedNeighbor.name == "neighbor-1" {
			t.Error("should not retry slice 1 with neighbor-1 (it failed)")
		}
		if sliceIdx == 3 && selectedNeighbor.name == "neighbor-3" {
			t.Error("should not retry slice 3 with neighbor-3 (it failed)")
		}
	}
}

// TestBootRecovery_AllNeighborsFail tests behavior when all neighbors fail
func TestBootRecovery_AllNeighborsFail(t *testing.T) {
	// Simulate all failures
	type syncResult struct {
		name       string
		sliceIndex int
		success    bool
	}

	results := []syncResult{
		{name: "neighbor-0", sliceIndex: 0, success: false},
		{name: "neighbor-1", sliceIndex: 1, success: false},
		{name: "neighbor-2", sliceIndex: 2, success: false},
	}

	var failedSlices []int
	respondedNeighbors := make(map[string]bool)

	for _, result := range results {
		respondedNeighbors[result.name] = result.success
		if !result.success {
			failedSlices = append(failedSlices, result.sliceIndex)
		}
	}

	// All slices failed
	if len(failedSlices) != 3 {
		t.Errorf("expected 3 failed slices, got %d", len(failedSlices))
	}

	// No neighbors available for retry
	allNeighbors := []struct{ name, ip string }{
		{"neighbor-0", "ip0"},
		{"neighbor-1", "ip1"},
		{"neighbor-2", "ip2"},
	}

	var availableNeighbors []struct{ name, ip string }
	for _, n := range allNeighbors {
		if respondedNeighbors[n.name] {
			availableNeighbors = append(availableNeighbors, n)
		}
	}

	// Should have no available neighbors
	if len(availableNeighbors) != 0 {
		t.Errorf("expected 0 available neighbors when all fail, got %d", len(availableNeighbors))
	}
}

// TestBootRecovery_SuccessNoRetryNeeded tests that no retry happens when all succeed
func TestBootRecovery_SuccessNoRetryNeeded(t *testing.T) {
	type syncResult struct {
		name       string
		sliceIndex int
		success    bool
	}

	results := []syncResult{
		{name: "neighbor-0", sliceIndex: 0, success: true},
		{name: "neighbor-1", sliceIndex: 1, success: true},
		{name: "neighbor-2", sliceIndex: 2, success: true},
	}

	var failedSlices []int
	for _, result := range results {
		if !result.success {
			failedSlices = append(failedSlices, result.sliceIndex)
		}
	}

	// No slices failed - no retry needed
	if len(failedSlices) != 0 {
		t.Errorf("expected 0 failed slices, got %d", len(failedSlices))
	}
}

// TestBootRecovery_ConcurrentRetryLogic tests that concurrent retry logic works correctly
func TestBootRecovery_ConcurrentRetryLogic(t *testing.T) {
	// Test that concurrent retries don't interfere with each other
	failedSlices := []int{1, 3, 5, 7, 9}
	availableNeighbors := []struct{ name, ip string }{
		{"neighbor-0", "ip0"},
		{"neighbor-2", "ip2"},
		{"neighbor-4", "ip4"},
	}

	// Track which neighbor handles each slice (simulated)
	assignments := make(map[int]string)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i, sliceIdx := range failedSlices {
		wg.Add(1)
		go func(idx int, slice int) {
			defer wg.Done()
			// Round-robin assignment
			neighbor := availableNeighbors[idx%len(availableNeighbors)]
			mu.Lock()
			assignments[slice] = neighbor.name
			mu.Unlock()
		}(i, sliceIdx)
	}

	wg.Wait()

	// Verify all slices got assigned
	if len(assignments) != len(failedSlices) {
		t.Errorf("expected %d assignments, got %d", len(failedSlices), len(assignments))
	}

	// Verify round-robin distribution
	expectedAssignments := map[int]string{
		1: "neighbor-0",
		3: "neighbor-2",
		5: "neighbor-4",
		7: "neighbor-0",
		9: "neighbor-2",
	}

	for slice, expected := range expectedAssignments {
		if assignments[slice] != expected {
			t.Errorf("slice %d: expected neighbor %s, got %s", slice, expected, assignments[slice])
		}
	}
}

// TestBootRecovery_MergeAfterRetry tests that events from retry are properly merged
func TestBootRecovery_MergeAfterRetry(t *testing.T) {
	ledger := NewSyncLedger(1000)

	// Simulate first round: got events from slices 0 and 2
	round1Events := []SyncEvent{
		NewPingSyncEvent("observer", "target-0", 10.0),
		NewPingSyncEvent("observer", "target-2", 20.0),
	}
	added1 := ledger.MergeEvents(round1Events)
	if added1 != 2 {
		t.Errorf("expected 2 events merged in round 1, got %d", added1)
	}

	// Simulate retry: got events from slice 1 (that failed initially)
	retryEvents := []SyncEvent{
		NewPingSyncEvent("observer", "target-1", 15.0),
	}
	added2 := ledger.MergeEvents(retryEvents)
	if added2 != 1 {
		t.Errorf("expected 1 event merged in retry, got %d", added2)
	}

	// Verify total events
	total := ledger.EventCount()
	if total != 3 {
		t.Errorf("expected 3 total events after retry merge, got %d", total)
	}

	// Verify deduplication: merging retry events again should add nothing
	added3 := ledger.MergeEvents(retryEvents)
	if added3 != 0 {
		t.Errorf("expected 0 events merged (dedup), got %d", added3)
	}
}

// TestBootRecovery_PartialSuccessLogic tests mixed success/failure scenarios
func TestBootRecovery_PartialSuccessLogic(t *testing.T) {
	// Simulate a scenario where some slices succeed and some fail
	type syncResult struct {
		name       string
		sliceIndex int
		events     int // number of events received
		success    bool
	}

	// Initial results: slices 0, 2 succeed; slices 1, 3 fail
	initialResults := []syncResult{
		{name: "neighbor-0", sliceIndex: 0, events: 100, success: true},
		{name: "neighbor-1", sliceIndex: 1, events: 0, success: false},
		{name: "neighbor-2", sliceIndex: 2, events: 100, success: true},
		{name: "neighbor-3", sliceIndex: 3, events: 0, success: false},
	}

	// Track results
	var totalEvents int
	var failedSlices []int
	respondedNeighbors := make(map[string]bool)

	for _, result := range initialResults {
		respondedNeighbors[result.name] = result.success
		totalEvents += result.events
		if !result.success {
			failedSlices = append(failedSlices, result.sliceIndex)
		}
	}

	// Verify initial state
	if totalEvents != 200 {
		t.Errorf("expected 200 events from successful slices, got %d", totalEvents)
	}
	if len(failedSlices) != 2 {
		t.Errorf("expected 2 failed slices, got %d", len(failedSlices))
	}

	// Find available neighbors for retry
	allNeighbors := []struct{ name, ip string }{
		{"neighbor-0", "ip0"},
		{"neighbor-1", "ip1"},
		{"neighbor-2", "ip2"},
		{"neighbor-3", "ip3"},
	}

	var availableNeighbors []struct{ name, ip string }
	for _, n := range allNeighbors {
		if respondedNeighbors[n.name] {
			availableNeighbors = append(availableNeighbors, n)
		}
	}

	if len(availableNeighbors) != 2 {
		t.Errorf("expected 2 available neighbors, got %d", len(availableNeighbors))
	}

	// Simulate retry: each failed slice gets 100 events from an available neighbor
	retryEvents := 0
	for i := range failedSlices {
		// Round-robin selection
		_ = availableNeighbors[i%len(availableNeighbors)]
		retryEvents += 100 // Simulated successful retry
	}

	totalEvents += retryEvents

	// After retry, we should have all events
	if totalEvents != 400 {
		t.Errorf("expected 400 total events after retry, got %d", totalEvents)
	}
}

// TestBootRecovery_RetryWithEmptySlice tests that empty but verified responses are OK
func TestBootRecovery_RetryWithEmptySlice(t *testing.T) {
	// A slice might be empty but verified (nara has no events for that slice)
	// This should count as success (not trigger retry)

	type syncResult struct {
		name         string
		sliceIndex   int
		events       int
		respVerified bool
	}

	results := []syncResult{
		{name: "neighbor-0", sliceIndex: 0, events: 50, respVerified: true},
		{name: "neighbor-1", sliceIndex: 1, events: 0, respVerified: true}, // Empty but verified = OK
		{name: "neighbor-2", sliceIndex: 2, events: 0, respVerified: false}, // Empty and unverified = FAIL
	}

	var failedSlices []int
	for _, r := range results {
		// Success if we got events OR response was verified
		success := r.events > 0 || r.respVerified
		if !success {
			failedSlices = append(failedSlices, r.sliceIndex)
		}
	}

	// Only slice 2 should be considered failed
	if len(failedSlices) != 1 {
		t.Errorf("expected 1 failed slice, got %d", len(failedSlices))
	}
	if len(failedSlices) > 0 && failedSlices[0] != 2 {
		t.Errorf("expected slice 2 to fail, got slice %d", failedSlices[0])
	}
}

// TestBootRecovery_LargeScaleRetry tests retry logic with many slices
func TestBootRecovery_LargeScaleRetry(t *testing.T) {
	// Simulate 20 neighbors, 5 fail
	numNeighbors := 20
	numFailures := 5

	var failedSlices []int
	respondedNeighbors := make(map[string]bool)

	for i := 0; i < numNeighbors; i++ {
		name := fmt.Sprintf("neighbor-%d", i)
		// First 5 neighbors fail
		if i < numFailures {
			respondedNeighbors[name] = false
			failedSlices = append(failedSlices, i)
		} else {
			respondedNeighbors[name] = true
		}
	}

	// Should have 5 failed slices
	if len(failedSlices) != numFailures {
		t.Errorf("expected %d failed slices, got %d", numFailures, len(failedSlices))
	}

	// Find available neighbors
	var availableCount int
	for _, success := range respondedNeighbors {
		if success {
			availableCount++
		}
	}

	// Should have 15 available neighbors
	expectedAvailable := numNeighbors - numFailures
	if availableCount != expectedAvailable {
		t.Errorf("expected %d available neighbors, got %d", expectedAvailable, availableCount)
	}

	// With 15 available neighbors, we can easily retry 5 failed slices
	// Each failed slice can be assigned to a different neighbor
	if availableCount < len(failedSlices) {
		t.Error("not enough available neighbors for retry")
	}
}
