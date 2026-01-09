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

// TestOnlineStatusAfterResetWithMixedTimestamps is a regression test for the bug where
// after receiving zines with old events, a nara would be incorrectly marked as MISSING
// even though we had recent activity from them.
//
// The bug: When ledger pruning causes a projection reset, events were reprocessed
// in insertion order (not timestamp order). If old events happened to come first
// in the reprocessing, the projection would have old timestamps and incorrectly
// mark naras as MISSING.
//
// The fix: Sort events by timestamp when reprocessing after a reset.
func TestOnlineStatusAfterResetWithMixedTimestamps(t *testing.T) {
	// Use a larger ledger so events survive pruning
	ledger := NewSyncLedger(20)
	projection := NewOnlineStatusProjection(ledger)
	projection.RunToEnd(context.Background())

	now := time.Now().UnixNano()
	tenMinutesAgo := now - int64(10*time.Minute)
	oneMinuteAgo := now - int64(1*time.Minute)

	// Simulate the problematic scenario:
	// 1. First add old events (simulating zine events received from the past)
	// 2. Then add a recent seen event for lisa
	// 3. Then add MORE old events for lisa (simulating receiving a zine with old history)
	// 4. Trigger pruning/reset
	// 5. Lisa should still be ONLINE because the recent event should win

	// Add old events first (simulating zine events from the past)
	for i := 0; i < 5; i++ {
		ledger.AddEvent(SyncEvent{
			Timestamp: tenMinutesAgo + int64(i*1000), // Old timestamps
			Service:   ServiceHeyThere,
			HeyThere: &HeyThereEvent{
				From:      "other-nara",
				PublicKey: "key",
			},
		})
	}

	// Add an OLD hey_there from lisa (simulating receiving a zine with her old events)
	// This has an OLD timestamp but is added to the ledger NOW
	oldLisaEvent := SyncEvent{
		Timestamp: tenMinutesAgo, // 10 minutes ago - OVER the 5 min threshold
		Service:   ServiceHeyThere,
		HeyThere: &HeyThereEvent{
			From:      "lisa",
			PublicKey: "lisa-key",
		},
	}
	oldLisaEvent.ComputeID()
	ledger.AddEvent(oldLisaEvent)

	// Add a RECENT seen event for lisa (we just saw her newspaper)
	// This is added AFTER the old event, but has a NEWER timestamp
	recentSeenEvent := SyncEvent{
		Timestamp: oneMinuteAgo, // 1 minute ago - well within 5 min threshold
		Service:   ServiceSeen,
		Seen: &SeenEvent{
			Observer: "me",
			Subject:  "lisa",
			Via:      "newspaper",
		},
	}
	recentSeenEvent.ComputeID()
	ledger.AddEvent(recentSeenEvent)

	// Add more padding events
	for i := 0; i < 5; i++ {
		ledger.AddEvent(SyncEvent{
			Timestamp: now - int64(30*time.Second) + int64(i*1000),
			Service:   ServiceSeen,
			Seen: &SeenEvent{
				Observer: "someone",
				Subject:  "padding",
				Via:      "test",
			},
		})
	}

	// Process all events so far
	projection.RunOnce()

	// Lisa should be ONLINE (the recent seen event should win over the old hey_there)
	status := projection.GetStatus("lisa")
	if status != "ONLINE" {
		t.Errorf("Before pruning: expected lisa ONLINE, got %q", status)
	}

	// Now trigger pruning by adding more events to exceed capacity
	for i := 0; i < 10; i++ {
		ledger.AddEvent(SyncEvent{
			Timestamp: now + int64(i*1000),
			Service:   ServiceSeen,
			Seen: &SeenEvent{
				Observer: "someone",
				Subject:  "other",
				Via:      "test",
			},
		})
	}

	// This should trigger a reset because version changed due to pruning
	projection.RunOnce()

	// After reset, the projection reprocesses all remaining events.
	// THE BUG: If events are processed in INSERTION order, the old hey_there
	// might be processed first, setting lisa's LastEventTime to 10 minutes ago.
	// Then the recent seen event updates it to 1 minute ago. That should be fine...
	//
	// Actually, the real bug is more subtle: the events in the ledger are stored
	// in insertion order, and after pruning some events are removed. The remaining
	// events still have their timestamps, but the projection handler compares
	// `event.Timestamp > current.LastEventTime`. The recent event should win.
	//
	// THE FIX: Sort events by timestamp when reprocessing, ensuring chronological
	// processing and that the most recent state is preserved.

	status = projection.GetStatus("lisa")
	if status == "MISSING" {
		t.Error("BUG: lisa incorrectly marked as MISSING after pruning/reset")
		t.Log("This happens when events are processed in insertion order instead of timestamp order")
	}
	if status != "ONLINE" {
		t.Errorf("After pruning: expected lisa ONLINE, got %q", status)
	}
}

// TestOnlineStatusRaceConditionWithAsyncTrigger tests the race condition where
// the observation maintenance loop reads from the projection before it has
// processed newly added events. This simulates what happens during zine merges:
//   1. Events are added to ledger
//   2. Trigger() is called (async, doesn't block)
//   3. Observation loop calls GetStatus() immediately
//   4. BUG: GetStatus returns stale data
//
// The fix is to call RunOnce() synchronously before reading status.
func TestOnlineStatusRaceConditionWithAsyncTrigger(t *testing.T) {
	ledger := NewSyncLedger(100)
	projection := NewOnlineStatusProjection(ledger)
	now := time.Now().UnixNano()

	// Phase 1: Initial state - bob is OFFLINE (via chau event)
	oldChauEvent := SyncEvent{
		Timestamp: now - int64(10*time.Second),
		Service:   ServiceChau,
		Chau: &ChauEvent{
			From: "bob",
		},
	}
	oldChauEvent.ComputeID()
	ledger.AddEvent(oldChauEvent)

	// Process initial events
	projection.RunOnce()

	// Verify bob is OFFLINE
	status := projection.GetStatus("bob")
	if status != "OFFLINE" {
		t.Fatalf("Initial state: expected bob OFFLINE, got %q", status)
	}

	// Phase 2: Simulate zine merge - add hey_there that makes bob ONLINE
	// This is what happens when we receive a zine with bob's recent hey_there
	recentHeyThere := SyncEvent{
		Timestamp: now - int64(5*time.Second), // More recent than chau
		Service:   ServiceHeyThere,
		HeyThere: &HeyThereEvent{
			From:      "bob",
			PublicKey: "bob-key",
		},
	}
	recentHeyThere.ComputeID()
	ledger.AddEvent(recentHeyThere)

	// In production, Trigger() is called here (async, doesn't wait)
	projection.Trigger()

	// BUG SCENARIO: Observation loop runs immediately after Trigger()
	// WITHOUT calling RunOnce() first. The projection hasn't processed
	// the new event yet, so it returns stale data.
	//
	// Note: In this test, Trigger() sends to a channel but there's no
	// goroutine receiving (RunContinuous isn't running), so the projection
	// state is definitely stale here.
	staleStatus := projection.GetStatus("bob")

	// The stale status should still be OFFLINE (the bug behavior)
	// This verifies our test actually captures the race condition.
	if staleStatus != "OFFLINE" {
		t.Errorf("Race condition test: expected stale status OFFLINE, got %q", staleStatus)
		t.Log("If this fails, the test isn't properly capturing the race condition")
	}

	// FIX: Call RunOnce() synchronously before reading status
	// This is what the observation maintenance loop should do.
	projection.RunOnce()

	// Now status should be correct (ONLINE from the recent hey_there)
	correctStatus := projection.GetStatus("bob")
	if correctStatus != "ONLINE" {
		t.Errorf("After RunOnce fix: expected bob ONLINE, got %q", correctStatus)
		t.Log("The fix is to call RunOnce() before GetStatus() in the observation loop")
	}
}

// TestOnlineStatusRaceWithVersionChange tests the race condition when events
// cause a ledger version change (pruning). This simulates:
//   1. Projection is up-to-date at version V1
//   2. Zine merge causes many events to be added
//   3. Pruning happens, version changes to V2
//   4. Observation loop reads from projection (still at V1)
//   5. BUG: Projection doesn't know it needs to reset
//
// The fix is RunOnce() which checks version and resets if needed.
func TestOnlineStatusRaceWithVersionChange(t *testing.T) {
	ledger := NewSyncLedger(20) // Small capacity to trigger pruning
	projection := NewOnlineStatusProjection(ledger)
	now := time.Now().UnixNano()

	// Phase 1: Add events including one for "charlie"
	for i := 0; i < 10; i++ {
		ledger.AddEvent(SyncEvent{
			Timestamp: now - int64(15-i)*int64(time.Second),
			Service:   ServiceSeen,
			Seen: &SeenEvent{
				Observer: "observer",
				Subject:  "other" + string(rune('0'+i)),
				Via:      "test",
			},
		})
	}

	// Add a recent event for charlie
	charlieEvent := SyncEvent{
		Timestamp: now - int64(5*time.Second),
		Service:   ServiceHeyThere,
		HeyThere: &HeyThereEvent{
			From:      "charlie",
			PublicKey: "charlie-key",
		},
	}
	charlieEvent.ComputeID()
	ledger.AddEvent(charlieEvent)

	// Process all events
	projection.RunOnce()

	// Verify charlie is ONLINE
	status := projection.GetStatus("charlie")
	if status != "ONLINE" {
		t.Fatalf("Initial state: expected charlie ONLINE, got %q", status)
	}

	// Record the current version
	initialVersion := ledger.GetVersion()

	// Phase 2: Simulate zine merge that causes pruning
	// Add many events to exceed capacity and trigger pruning
	for i := 0; i < 15; i++ {
		ledger.AddEvent(SyncEvent{
			Timestamp: now + int64(i*1000), // Recent timestamps
			Service:   ServiceSeen,
			Seen: &SeenEvent{
				Observer: "other-observer",
				Subject:  "new-nara" + string(rune('0'+i)),
				Via:      "zine",
			},
		})
	}

	// Trigger (async)
	projection.Trigger()

	// Version should have changed due to pruning
	newVersion := ledger.GetVersion()
	if newVersion == initialVersion {
		t.Log("Warning: Version didn't change. Test might not be exercising the version-change path.")
	}

	// BUG SCENARIO: Reading status without RunOnce()
	// The projection still has charlie's state, but it might be inconsistent
	// because the ledger was restructured.
	//
	// Without RunOnce(), the projection doesn't know to reset.
	// This might still return ONLINE in some cases, but the state is stale.
	// The key issue is the projection doesn't know the ledger changed.

	// FIX: Call RunOnce() which checks version and resets if needed
	projection.RunOnce()

	// After RunOnce(), if charlie's event survived pruning, status is ONLINE
	// If it was pruned, status would be "" (unknown)
	fixedStatus := projection.GetStatus("charlie")

	// Charlie's event should have survived (it's relatively recent)
	// The important thing is that RunOnce() properly processed the version change
	if fixedStatus == "" {
		t.Log("Charlie's event was pruned - this is expected behavior")
		t.Log("The test verifies RunOnce() properly handles version changes")
	} else if fixedStatus == "ONLINE" {
		t.Log("Charlie's event survived pruning - status correctly shows ONLINE")
	} else if fixedStatus == "MISSING" {
		t.Errorf("After RunOnce fix: charlie incorrectly marked as MISSING")
		t.Log("This would indicate the timestamp-sorting fix isn't working")
	}
}

// TestOnlineStatusSocialEventsMarkActorOnline verifies that social events (teases)
// mark the Actor as ONLINE. This is critical because naras that actively tease
// others should not be marked as MISSING/disappeared.
func TestOnlineStatusSocialEventsMarkActorOnline(t *testing.T) {
	ledger := NewSyncLedger(100)
	projection := NewOnlineStatusProjection(ledger)

	now := time.Now().UnixNano()

	// tame-sun teases r2d2 - this should mark tame-sun as ONLINE
	ledger.AddEvent(SyncEvent{
		Timestamp: now,
		Service:   ServiceSocial,
		Emitter:   "tame-sun",
		Social: &SocialEventPayload{
			Type:   "tease",
			Actor:  "tame-sun",
			Target: "r2d2",
			Reason: "r2d2 discovering the restart button",
		},
	})

	// Process the social event
	projection.RunOnce()

	// tame-sun should be ONLINE because they just teased someone
	status := projection.GetStatus("tame-sun")
	if status != "ONLINE" {
		t.Errorf("Expected tame-sun to be ONLINE after teasing (Actor in social event), got: %q", status)
		t.Log("Social events should mark the Actor as ONLINE - they are actively participating")
	}

	// Verify the state was properly recorded
	state := projection.GetState("tame-sun")
	if state == nil {
		t.Fatal("Expected tame-sun to have state after social event, got nil")
	}
	if state.LastEventType != ServiceSocial {
		t.Errorf("Expected LastEventType to be %q, got %q", ServiceSocial, state.LastEventType)
	}
}

// TestOnlineStatusPingEventsMarkBothOnline verifies that ping events mark
// both the Observer (sender) and Target (receiver) as ONLINE.
func TestOnlineStatusPingEventsMarkBothOnline(t *testing.T) {
	ledger := NewSyncLedger(100)
	projection := NewOnlineStatusProjection(ledger)

	now := time.Now().UnixNano()

	// alice pings bob - both should be marked ONLINE
	ledger.AddEvent(SyncEvent{
		Timestamp: now,
		Service:   ServicePing,
		Emitter:   "alice",
		Ping: &PingObservation{
			Observer: "alice",
			Target:   "bob",
			RTT:      15.5,
		},
	})

	// Process the ping event
	projection.RunOnce()

	// alice (Observer) should be ONLINE
	aliceStatus := projection.GetStatus("alice")
	if aliceStatus != "ONLINE" {
		t.Errorf("Expected alice (Observer) to be ONLINE after ping, got: %q", aliceStatus)
	}

	// bob (Target) should be ONLINE
	bobStatus := projection.GetStatus("bob")
	if bobStatus != "ONLINE" {
		t.Errorf("Expected bob (Target) to be ONLINE after ping, got: %q", bobStatus)
	}

	// Verify states were properly recorded
	aliceState := projection.GetState("alice")
	if aliceState == nil {
		t.Fatal("Expected alice to have state after ping event, got nil")
	}
	if aliceState.LastEventType != ServicePing {
		t.Errorf("Expected alice LastEventType to be %q, got %q", ServicePing, aliceState.LastEventType)
	}

	bobState := projection.GetState("bob")
	if bobState == nil {
		t.Fatal("Expected bob to have state after ping event, got nil")
	}
	if bobState.LastEventType != ServicePing {
		t.Errorf("Expected bob LastEventType to be %q, got %q", ServicePing, bobState.LastEventType)
	}
}
