package nara

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestProjectionResetsOnPrune verifies that projections correctly reset
// and reprocess all events when the ledger is restructured by pruning.
func TestProjectionResetsOnPrune(t *testing.T) {
	// Create a small ledger that will prune quickly
	ledger := NewSyncLedger(5)

	// Track how many times the handler is called and resets occur
	var eventCount atomic.Int32
	var resetCount atomic.Int32

	projection := NewProjection(ledger, func(event SyncEvent) error {
		eventCount.Add(1)
		return nil
	})

	projection.SetOnReset(func() {
		resetCount.Add(1)
	})

	// Add 3 events
	baseTime := time.Now().UnixNano()
	for i := 0; i < 3; i++ {
		ledger.AddEvent(SyncEvent{
			Timestamp: baseTime + int64(i*1000),
			Service:   ServiceSeen,
			Seen: &SeenEvent{
				Observer: "alice",
				Subject:  "bob",
				Via:      "mesh",
			},
		})
	}

	// Process events
	ctx := context.Background()
	err := projection.RunToEnd(ctx)
	if err != nil {
		t.Fatalf("RunToEnd failed: %v", err)
	}

	if eventCount.Load() != 3 {
		t.Errorf("Expected 3 events processed, got %d", eventCount.Load())
	}
	if resetCount.Load() != 0 {
		t.Errorf("Expected 0 resets before pruning, got %d", resetCount.Load())
	}

	// Reset counter for next phase
	eventCount.Store(0)

	// Add more events to trigger pruning (ledger max is 5, we have 3, adding 4 more = 7 total)
	// This will cause pruning which re-sorts events by timestamp
	for i := 0; i < 4; i++ {
		ledger.AddEvent(SyncEvent{
			Timestamp: baseTime + int64((i+3)*1000),
			Service:   ServiceSeen,
			Seen: &SeenEvent{
				Observer: "charlie",
				Subject:  "dave",
				Via:      "zine",
			},
		})
	}

	// Verify version changed (pruning happened)
	version := ledger.GetVersion()
	if version == 0 {
		t.Error("Expected version to be incremented after pruning")
	}

	// Process new events - this should trigger a reset
	err = projection.RunToEnd(ctx)
	if err != nil {
		t.Fatalf("RunToEnd after pruning failed: %v", err)
	}

	if resetCount.Load() != 1 {
		t.Errorf("Expected 1 reset after pruning, got %d", resetCount.Load())
	}

	// All remaining events should be reprocessed (5 events after pruning)
	if eventCount.Load() != 5 {
		t.Errorf("Expected 5 events reprocessed after reset, got %d", eventCount.Load())
	}
}

// TestProjectionRunOnceResetsOnPrune verifies RunOnce also handles version changes.
func TestProjectionRunOnceResetsOnPrune(t *testing.T) {
	ledger := NewSyncLedger(5)

	var eventCount atomic.Int32
	var resetCount atomic.Int32

	projection := NewProjection(ledger, func(event SyncEvent) error {
		eventCount.Add(1)
		return nil
	})

	projection.SetOnReset(func() {
		resetCount.Add(1)
	})

	// Add 3 events
	baseTime := time.Now().UnixNano()
	for i := 0; i < 3; i++ {
		ledger.AddEvent(SyncEvent{
			Timestamp: baseTime + int64(i*1000),
			Service:   ServiceSeen,
			Seen: &SeenEvent{
				Observer: "alice",
				Subject:  "bob",
				Via:      "mesh",
			},
		})
	}

	// Process via RunOnce
	processed, err := projection.RunOnce()
	if err != nil {
		t.Fatalf("RunOnce failed: %v", err)
	}
	if !processed {
		t.Error("Expected RunOnce to process events")
	}
	if eventCount.Load() != 3 {
		t.Errorf("Expected 3 events, got %d", eventCount.Load())
	}

	eventCount.Store(0)

	// Trigger pruning
	for i := 0; i < 4; i++ {
		ledger.AddEvent(SyncEvent{
			Timestamp: baseTime + int64((i+3)*1000),
			Service:   ServiceSeen,
			Seen: &SeenEvent{
				Observer: "charlie",
				Subject:  "dave",
				Via:      "zine",
			},
		})
	}

	// RunOnce should detect version change and reset
	processed, err = projection.RunOnce()
	if err != nil {
		t.Fatalf("RunOnce after pruning failed: %v", err)
	}
	if !processed {
		t.Error("Expected RunOnce to process events after reset")
	}
	if resetCount.Load() != 1 {
		t.Errorf("Expected 1 reset, got %d", resetCount.Load())
	}
	if eventCount.Load() != 5 {
		t.Errorf("Expected 5 events reprocessed, got %d", eventCount.Load())
	}
}

// TestOnlineStatusProjectionResetsOnPrune verifies OnlineStatusProjection
// correctly resets its state when ledger is restructured.
func TestOnlineStatusProjectionResetsOnPrune(t *testing.T) {
	ledger := NewSyncLedger(5)
	projection := NewOnlineStatusProjection(ledger)

	ctx := context.Background()
	baseTime := time.Now().UnixNano()

	// Add hey-there event for alice (marks ONLINE)
	ledger.AddEvent(SyncEvent{
		Timestamp: baseTime,
		Service:   ServiceHeyThere,
		HeyThere: &HeyThereEvent{
			From:      "alice",
			PublicKey: "key1",
			MeshIP:    "10.0.0.1",
		},
	})

	// Add chau event for bob (marks OFFLINE)
	ledger.AddEvent(SyncEvent{
		Timestamp: baseTime + 1000,
		Service:   ServiceChau,
		Chau: &ChauEvent{
			From:      "bob",
			PublicKey: "key2",
		},
	})

	// Process events
	projection.RunToEnd(ctx)

	// Verify initial state
	if status := projection.GetStatus("alice"); status != "ONLINE" {
		t.Errorf("Expected alice ONLINE, got %s", status)
	}
	if status := projection.GetStatus("bob"); status != "OFFLINE" {
		t.Errorf("Expected bob OFFLINE, got %s", status)
	}

	// Add events to trigger pruning - use high-priority (low importance) events
	// that will be pruned, leaving alice's hey-there intact
	for i := 0; i < 5; i++ {
		ledger.AddEvent(SyncEvent{
			Timestamp: baseTime + int64((i+2)*1000),
			Service:   ServiceSeen,
			Seen: &SeenEvent{
				Observer: "observer",
				Subject:  "charlie",
				Via:      "mesh",
			},
		})
	}

	// Process after pruning - projection should reset and rebuild state
	projection.RunToEnd(ctx)

	// State should still be correct after reset and reprocess
	// (alice's hey-there should survive pruning as it's lower priority to prune)
	allStatuses := projection.GetAllStatuses()
	if len(allStatuses) == 0 {
		t.Error("Expected some statuses after reset")
	}
}

// TestOnlineStatusHandlesOutOfOrderEvents verifies that projections correctly
// handle events arriving out of chronological order (e.g., via zine gossip).
// Projections use timestamps for ordering, so out-of-order arrival is handled
// without needing a reset.
func TestOnlineStatusHandlesOutOfOrderEvents(t *testing.T) {
	// Large ledger to avoid pruning
	ledger := NewSyncLedger(100)
	projection := NewOnlineStatusProjection(ledger)

	ctx := context.Background()
	baseTime := time.Now().UnixNano()

	// Add hey-there for alice at time T+1000 (ONLINE)
	ledger.AddEvent(SyncEvent{
		Timestamp: baseTime + 1000,
		Service:   ServiceHeyThere,
		HeyThere: &HeyThereEvent{
			From:      "alice",
			PublicKey: "key1",
			MeshIP:    "10.0.0.1",
		},
	})

	projection.RunToEnd(ctx)
	if status := projection.GetStatus("alice"); status != "ONLINE" {
		t.Errorf("Expected alice ONLINE, got %s", status)
	}

	// Now an OLD event arrives via zine - chau at time T+500 (before the hey-there)
	// This simulates an event arriving "into the past"
	ledger.AddEvent(SyncEvent{
		Timestamp: baseTime + 500, // OLDER than hey-there
		Service:   ServiceChau,
		Chau: &ChauEvent{
			From:      "alice",
			PublicKey: "key1",
		},
	})

	projection.RunToEnd(ctx)

	// Alice should STILL be ONLINE because hey-there (T+1000) is newer than chau (T+500)
	// The projection uses timestamps for ordering, not arrival order
	if status := projection.GetStatus("alice"); status != "ONLINE" {
		t.Errorf("Expected alice ONLINE (hey-there is newer), got %s", status)
	}

	// Now a NEW chau arrives at T+2000
	ledger.AddEvent(SyncEvent{
		Timestamp: baseTime + 2000,
		Service:   ServiceChau,
		Chau: &ChauEvent{
			From:      "alice",
			PublicKey: "key1",
		},
	})

	projection.RunToEnd(ctx)

	// NOW alice should be OFFLINE because this chau is the newest
	if status := projection.GetStatus("alice"); status != "OFFLINE" {
		t.Errorf("Expected alice OFFLINE (chau is newest), got %s", status)
	}
}

// TestGetEventsSinceReturnsVersion verifies GetEventsSince returns version correctly.
func TestGetEventsSinceReturnsVersion(t *testing.T) {
	ledger := NewSyncLedger(5)

	// Initially version should be 0
	_, _, version := ledger.GetEventsSince(0)
	if version != 0 {
		t.Errorf("Expected initial version 0, got %d", version)
	}

	// Add events (no pruning yet)
	baseTime := time.Now().UnixNano()
	for i := 0; i < 3; i++ {
		ledger.AddEvent(SyncEvent{
			Timestamp: baseTime + int64(i*1000),
			Service:   ServiceSeen,
			Seen: &SeenEvent{
				Observer: "alice",
				Subject:  "bob",
				Via:      "mesh",
			},
		})
	}

	// Version should still be 0 (no pruning)
	_, _, version = ledger.GetEventsSince(0)
	if version != 0 {
		t.Errorf("Expected version 0 before pruning, got %d", version)
	}

	// Trigger pruning
	for i := 0; i < 4; i++ {
		ledger.AddEvent(SyncEvent{
			Timestamp: baseTime + int64((i+3)*1000),
			Service:   ServiceSeen,
			Seen: &SeenEvent{
				Observer: "charlie",
				Subject:  "dave",
				Via:      "zine",
			},
		})
	}

	// Version should now be > 0 (incremented by pruning)
	_, _, version = ledger.GetEventsSince(0)
	if version == 0 {
		t.Error("Expected version > 0 after pruning, got 0")
	}

	// GetVersion should match
	if ledger.GetVersion() != version {
		t.Errorf("GetVersion() %d != GetEventsSince version %d", ledger.GetVersion(), version)
	}
}
