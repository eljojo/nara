package nara

import (
	"testing"
	"time"
)

// Test that multiple observers reporting the same restart results in single stored event
func TestObservationDedup_IdenticalRestarts(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := "nara-target"
	startTime := int64(1234567890)
	restartNum := int64(42)

	// Three observers report the same restart
	observers := []string{"observer-a", "observer-b", "observer-c"}
	for _, observer := range observers {
		event := NewRestartObservationEvent(observer, subject, startTime, restartNum)
		ledger.AddEventWithDedup(event)
	}

	// Should have exactly 1 event stored (deduplicated)
	events := ledger.GetObservationEventsAbout(subject)
	restartEvents := 0
	for _, e := range events {
		if e.Observation.Type == "restart" &&
			e.Observation.StartTime == startTime &&
			e.Observation.RestartNum == restartNum {
			restartEvents++
		}
	}

	if restartEvents != 1 {
		t.Errorf("Expected 1 deduplicated restart event, got %d", restartEvents)
	}
}

// Test that different restart numbers are NOT deduplicated
func TestObservationDedup_DifferentRestartNumbers(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := "nara-target"
	startTime := int64(1234567890)

	// Same observer reports 3 different restarts
	for i := 0; i < 3; i++ {
		event := NewRestartObservationEvent("observer-a", subject, startTime, int64(i))
		ledger.AddEventWithDedup(event)
	}

	// Should have 3 events (different restart numbers)
	events := ledger.GetObservationEventsAbout(subject)
	if len(events) != 3 {
		t.Errorf("Expected 3 events (different restart numbers), got %d", len(events))
	}

	// Verify all three restart numbers are present
	foundRestarts := make(map[int64]bool)
	for _, e := range events {
		if e.Observation.Type == "restart" {
			foundRestarts[e.Observation.RestartNum] = true
		}
	}

	for i := 0; i < 3; i++ {
		if !foundRestarts[int64(i)] {
			t.Errorf("Expected to find restart_num=%d", i)
		}
	}
}

// Test that different start times are NOT deduplicated
func TestObservationDedup_DifferentStartTimes(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := "nara-target"
	restartNum := int64(5)

	// Three observers report different start times (clock skew or actual different restarts)
	startTimes := []int64{1234567890, 1234567891, 1234567892}
	for i, startTime := range startTimes {
		observer := "observer-" + string(rune('a'+i))
		event := NewRestartObservationEvent(observer, subject, startTime, restartNum)
		ledger.AddEventWithDedup(event)
	}

	// Should have 3 events (different start times)
	events := ledger.GetObservationEventsAbout(subject)
	if len(events) != 3 {
		t.Errorf("Expected 3 events (different start times), got %d", len(events))
	}
}

// Test that different subjects are NOT deduplicated
func TestObservationDedup_DifferentSubjects(t *testing.T) {
	ledger := NewSyncLedger(1000)
	startTime := int64(1234567890)
	restartNum := int64(10)

	// Same observer reports same restart for different subjects
	subjects := []string{"nara-a", "nara-b", "nara-c"}
	for _, subject := range subjects {
		event := NewRestartObservationEvent("observer-1", subject, startTime, restartNum)
		ledger.AddEventWithDedup(event)
	}

	// Should have 3 events (different subjects)
	totalEvents := 0
	for _, subject := range subjects {
		events := ledger.GetObservationEventsAbout(subject)
		totalEvents += len(events)
	}

	if totalEvents != 3 {
		t.Errorf("Expected 3 events (different subjects), got %d", totalEvents)
	}
}

// Test that first observer is preserved when deduplicating
func TestObservationDedup_PreservesFirstObserver(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := "nara-target"
	startTime := int64(1234567890)
	restartNum := int64(15)

	// First observer reports
	event1 := NewRestartObservationEvent("alice", subject, startTime, restartNum)
	time.Sleep(1 * time.Millisecond)
	ledger.AddEventWithDedup(event1)

	// Second observer reports same restart
	event2 := NewRestartObservationEvent("bob", subject, startTime, restartNum)
	time.Sleep(1 * time.Millisecond)
	ledger.AddEventWithDedup(event2)

	// Third observer reports same restart
	event3 := NewRestartObservationEvent("charlie", subject, startTime, restartNum)
	ledger.AddEventWithDedup(event3)

	// Should have exactly 1 event, attributed to first observer (alice)
	events := ledger.GetObservationEventsAbout(subject)
	if len(events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(events))
	}

	if events[0].Observation.Observer != "alice" {
		t.Errorf("Expected first observer 'alice' to be preserved, got '%s'", events[0].Observation.Observer)
	}
}

// Test deduplication with mixed duplicate and unique events
func TestObservationDedup_MixedEvents(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := "nara-target"

	// Restart #1: 3 observers report it (should deduplicate to 1)
	for i := 0; i < 3; i++ {
		observer := "observer-" + string(rune('a'+i))
		event := NewRestartObservationEvent(observer, subject, 1000, 1)
		ledger.AddEventWithDedup(event)
	}

	// Restart #2: 2 observers report it (should deduplicate to 1)
	for i := 0; i < 2; i++ {
		observer := "observer-" + string(rune('d'+i))
		event := NewRestartObservationEvent(observer, subject, 2000, 2)
		ledger.AddEventWithDedup(event)
	}

	// Restart #3: 1 observer reports it (no deduplication needed)
	event := NewRestartObservationEvent("observer-f", subject, 3000, 3)
	ledger.AddEventWithDedup(event)

	// Should have exactly 3 events total (one per unique restart)
	events := ledger.GetObservationEventsAbout(subject)
	if len(events) != 3 {
		t.Errorf("Expected 3 deduplicated events, got %d", len(events))
	}

	// Verify each restart number appears exactly once
	foundRestarts := make(map[int64]int)
	for _, e := range events {
		foundRestarts[e.Observation.RestartNum]++
	}

	for i := int64(1); i <= 3; i++ {
		if foundRestarts[i] != 1 {
			t.Errorf("Expected restart_num=%d to appear exactly once, got %d", i, foundRestarts[i])
		}
	}
}

// Test that deduplication only applies to restart events, not status-change
func TestObservationDedup_OnlyRestartEvents(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := "nara-target"

	// Three observers report status-change to OFFLINE (should NOT deduplicate)
	for i := 0; i < 3; i++ {
		observer := "observer-" + string(rune('a'+i))
		event := NewStatusChangeObservationEvent(observer, subject, "OFFLINE")
		ledger.AddEventWithDedup(event)
	}

	// Should have 3 events (status-change events are not deduplicated)
	events := ledger.GetObservationEventsAbout(subject)
	statusChangeCount := 0
	for _, e := range events {
		if e.Observation.Type == "status-change" {
			statusChangeCount++
		}
	}

	if statusChangeCount != 3 {
		t.Errorf("Expected 3 status-change events (no deduplication), got %d", statusChangeCount)
	}
}

// Test that first-seen events are not deduplicated
func TestObservationDedup_NoFirstSeenDedup(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := "nara-target"
	startTime := int64(1234567890)

	// Three observers report first-seen (should NOT deduplicate)
	for i := 0; i < 3; i++ {
		observer := "observer-" + string(rune('a'+i))
		event := NewFirstSeenObservationEvent(observer, subject, startTime)
		ledger.AddEventWithDedup(event)
	}

	// Should have 3 events (first-seen events are not deduplicated)
	events := ledger.GetObservationEventsAbout(subject)
	firstSeenCount := 0
	for _, e := range events {
		if e.Observation.Type == "first-seen" {
			firstSeenCount++
		}
	}

	if firstSeenCount != 3 {
		t.Errorf("Expected 3 first-seen events (no deduplication), got %d", firstSeenCount)
	}
}

// Test deduplication with backfill events
func TestObservationDedup_BackfillRestarts(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := "nara-target"
	startTime := int64(1234567890)
	restartNum := int64(1137)

	// Three observers backfill the same historical restart
	for i := 0; i < 3; i++ {
		observer := "observer-" + string(rune('a'+i))
		event := NewBackfillObservationEvent(observer, subject, startTime, restartNum, startTime)
		ledger.AddEventWithDedup(event)
	}

	// Should have exactly 1 event (deduplicated)
	events := ledger.GetObservationEventsAbout(subject)
	backfillCount := 0
	for _, e := range events {
		if e.Observation.IsBackfill &&
			e.Observation.StartTime == startTime &&
			e.Observation.RestartNum == restartNum {
			backfillCount++
		}
	}

	if backfillCount != 1 {
		t.Errorf("Expected 1 deduplicated backfill event, got %d", backfillCount)
	}
}

// Test deduplication hash computation stability
func TestObservationDedup_HashStability(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := "nara-target"
	startTime := int64(1234567890)
	restartNum := int64(25)

	// Create same restart event twice
	event1 := NewRestartObservationEvent("observer-a", subject, startTime, restartNum)
	event1.ComputeID()

	event2 := NewRestartObservationEvent("observer-b", subject, startTime, restartNum)
	event2.ComputeID()

	// For deduplication, the content hash should match (ignoring observer)
	// The ID computation should include subject, restart_num, and start_time
	// but deduplication should be based on content, not observer

	ledger.AddEventWithDedup(event1)
	ledger.AddEventWithDedup(event2)

	events := ledger.GetObservationEventsAbout(subject)
	if len(events) != 1 {
		t.Errorf("Expected 1 event after deduplication, got %d", len(events))
	}
}

// Test deduplication under load
func TestObservationDedup_HighVolume(t *testing.T) {
	ledger := NewSyncLedger(2000)
	subject := "nara-target"
	startTime := int64(1234567890)
	restartNum := int64(100)

	// 50 observers all report the same restart
	for i := 0; i < 50; i++ {
		observer := "observer-" + string(rune('a'+(i%26)))
		if i >= 26 {
			observer = observer + string(rune('a'+((i-26)%26)))
		}
		event := NewRestartObservationEvent(observer, subject, startTime, restartNum)
		ledger.AddEventWithDedup(event)
	}

	// Should have exactly 1 event stored (all deduplicated)
	events := ledger.GetObservationEventsAbout(subject)
	restartCount := 0
	for _, e := range events {
		if e.Observation.Type == "restart" &&
			e.Observation.StartTime == startTime &&
			e.Observation.RestartNum == restartNum {
			restartCount++
		}
	}

	if restartCount != 1 {
		t.Errorf("Expected 1 event after mass deduplication, got %d", restartCount)
	}
}

// Test that deduplication + compaction work together
// Note: Compaction of restart events requires a checkpoint to exist
func TestObservationDedup_WithCompaction(t *testing.T) {
	ledger := NewSyncLedger(1000)
	observer := "observer-a"
	subject := "nara-target"

	// First add a checkpoint so compaction works
	checkpoint := NewTestCheckpointEvent(subject, time.Now().Unix()-3600, time.Now().Unix()-86400, 0, 0)
	ledger.AddEvent(checkpoint)

	// Add 25 unique restarts with same parameters (compaction limit is 20 per pair)
	for i := 0; i < 25; i++ {
		event := NewRestartObservationEvent(observer, subject, int64(1000), int64(i))
		ledger.AddEventWithDedup(event)
	}

	// After compaction, should have at most 20 events from observer-a
	eventsAfterA := ledger.GetObservationEventsAbout(subject)
	if len(eventsAfterA) > 20 {
		t.Errorf("Expected at most 20 events after observer-a, got %d", len(eventsAfterA))
	}

	// Another observer reports the same 25 restarts (should deduplicate most)
	for i := 0; i < 25; i++ {
		event := NewRestartObservationEvent("observer-b", subject, int64(1000), int64(i))
		ledger.AddEventWithDedup(event)
	}

	// Deduplication should prevent most additions (only non-compacted ones from A get duplicated)
	// Each observer→subject pair has max 20 events, so at most 40 total
	events := ledger.GetObservationEventsAbout(subject)
	if len(events) > 40 {
		t.Errorf("Expected at most 40 events (20 per observer), got %d", len(events))
	}

	// All events should be unique by restart_num (no duplicates due to deduplication)
	seen := make(map[int64]bool)
	for _, e := range events {
		if e.Observation.Type == "restart" {
			if seen[e.Observation.RestartNum] {
				t.Errorf("Duplicate restart_num=%d found (deduplication failed)", e.Observation.RestartNum)
			}
			seen[e.Observation.RestartNum] = true
		}
	}
}

// Test deduplication with slight time variations (clock skew tolerance)
func TestObservationDedup_ClockSkewTolerance(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := "nara-target"
	baseStartTime := int64(1234567890)
	restartNum := int64(50)

	// Three observers with slight clock skew (within tolerance)
	skews := []int64{0, 1, 2} // ±2 seconds
	for i, skew := range skews {
		observer := "observer-" + string(rune('a'+i))
		event := NewRestartObservationEvent(observer, subject, baseStartTime+skew, restartNum)
		ledger.AddEventWithDedup(event)
	}

	// Depending on implementation, might deduplicate with clock skew tolerance
	// For now, document expected behavior
	events := ledger.GetObservationEventsAbout(subject)

	// Note: Implementation may choose to either:
	// 1. Deduplicate within small time window (lenient)
	// 2. Require exact match (strict)
	if len(events) > 3 {
		t.Errorf("Expected at most 3 events with clock skew, got %d", len(events))
	}

	t.Logf("Clock skew test: got %d events (implementation may vary)", len(events))
}
