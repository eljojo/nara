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

// TestOnlineStatusChauThenOldHeyThereViaZine verifies the exact scenario:
// 1. nara1 and nara2 are online
// 2. nara3 joins
// 3. nara1 says chau, they all see it
// 4. nara2 sends zine to nara3 including hey_there from nara1 (older timestamp)
// 5. nara3 should still know nara1 is offline (chau has newer timestamp)
//
// This tests that when an old hey_there arrives after a chau via zine gossip,
// the chau wins because it has the later timestamp.
func TestOnlineStatusChauThenOldHeyThereViaZine(t *testing.T) {
	// Simulate nara3's perspective
	ledger := NewSyncLedger(100)
	projection := NewOnlineStatusProjection(ledger)
	ctx := context.Background()

	// Timeline:
	// T=1000: nara1 sends hey_there (nara2 sees it, nara3 doesn't exist yet)
	// T=2000: nara3 joins
	// T=3000: nara1 sends chau (everyone sees it)
	// T=4000: nara2 sends zine to nara3 containing the hey_there from T=1000

	baseTime := time.Now().UnixNano()
	heyThereTime := baseTime + 1000*int64(time.Millisecond)
	chauTime := baseTime + 3000*int64(time.Millisecond)

	// Step 1: nara3 receives chau directly (this is what nara3 sees first)
	ledger.AddEvent(SyncEvent{
		Timestamp: chauTime,
		Service:   ServiceChau,
		Emitter:   "nara1",
		Chau: &ChauEvent{
			From:      "nara1",
			PublicKey: "key1",
		},
	})

	projection.RunToEnd(ctx)

	// nara1 should be OFFLINE
	if status := projection.GetStatus("nara1"); status != "OFFLINE" {
		t.Errorf("After chau: expected nara1 OFFLINE, got %s", status)
	}

	// Step 2: nara3 receives zine from nara2 containing old hey_there
	// This hey_there has an OLDER timestamp than the chau
	ledger.AddEvent(SyncEvent{
		Timestamp: heyThereTime, // T=1000, older than chau at T=3000
		Service:   ServiceHeyThere,
		Emitter:   "nara1",
		HeyThere: &HeyThereEvent{
			From:      "nara1",
			PublicKey: "key1",
			MeshIP:    "10.0.0.1",
		},
	})

	projection.RunToEnd(ctx)

	// nara1 should STILL be OFFLINE because the chau (T=3000) is newer than hey_there (T=1000)
	if status := projection.GetStatus("nara1"); status != "OFFLINE" {
		t.Errorf("After old hey_there via zine: expected nara1 OFFLINE, got %s", status)
	}

	// Verify the state internals
	state := projection.GetState("nara1")
	if state == nil {
		t.Fatal("Expected state for nara1")
	}
	if state.LastEventTime != chauTime {
		t.Errorf("Expected LastEventTime=%d (chau), got %d", chauTime, state.LastEventTime)
	}
	if state.LastEventType != ServiceChau {
		t.Errorf("Expected LastEventType=%s, got %s", ServiceChau, state.LastEventType)
	}

	// Step 3: If nara1 comes back online with a NEW hey_there, it should work
	newHeyThereTime := baseTime + 5000*int64(time.Millisecond)
	ledger.AddEvent(SyncEvent{
		Timestamp: newHeyThereTime, // T=5000, newer than chau at T=3000
		Service:   ServiceHeyThere,
		Emitter:   "nara1",
		HeyThere: &HeyThereEvent{
			From:      "nara1",
			PublicKey: "key1",
			MeshIP:    "10.0.0.1",
		},
	})

	projection.RunToEnd(ctx)

	// NOW nara1 should be ONLINE
	if status := projection.GetStatus("nara1"); status != "ONLINE" {
		t.Errorf("After new hey_there: expected nara1 ONLINE, got %s", status)
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

// TestOnlineStatusOwnHeyThereInLedger verifies that when a nara sends hey_there,
// it must be added to their own ledger so the projection knows they're online.
// The fix: network.go HeyThere() now adds event to SyncLedger and triggers projection.
func TestOnlineStatusOwnHeyThereInLedger(t *testing.T) {
	// Create a local nara that will send hey_there
	ledger := NewSyncLedger(100)
	projection := NewOnlineStatusProjection(ledger)

	// Initial catchup
	projection.RunToEnd(context.Background())

	// Simulate what HeyThere() does: create and "send" a hey_there event
	myName := "test-nara"
	heyThereEvent := SyncEvent{
		Timestamp: time.Now().UnixNano(),
		Service:   ServiceHeyThere,
		HeyThere: &HeyThereEvent{
			From:      myName,
			PublicKey: "test-key",
		},
	}
	heyThereEvent.ComputeID()

	// The fix: add hey_there to local ledger (simulates what HeyThere() does after fix)
	ledger.AddEvent(heyThereEvent)

	// The fix also triggers the projection immediately
	projection.RunOnce()

	// Check our own status - should be ONLINE
	status := projection.GetStatus(myName)
	if status != "ONLINE" {
		t.Errorf("Expected ONLINE status for ourselves after sending hey_there, got %q", status)
	}
}

// TestOnlineStatusWithoutOwnHeyThere documents the bug behavior when hey_there isn't in ledger.
func TestOnlineStatusWithoutOwnHeyThere(t *testing.T) {
	// This test documents what happens WITHOUT the fix (bug behavior)
	ledger := NewSyncLedger(100)
	projection := NewOnlineStatusProjection(ledger)
	projection.RunToEnd(context.Background())

	myName := "test-nara"
	// Simulate OLD bug: hey_there was sent to MQTT but NOT added to local ledger
	// (no event added)

	projection.RunOnce()

	// Without any events, projection returns "" (unknown)
	status := projection.GetStatus(myName)
	if status != "" {
		t.Errorf("Expected empty status when no events in ledger, got %q", status)
	}
}
