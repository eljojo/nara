package nara

import (
	"fmt"
	"testing"
	"time"
)

// TestPingVerificationUpdatesProjectionImmediately reproduces the production bug where:
// 1. Old ONLINE event (>5min ago) causes GetStatus() to return MISSING
// 2. Ping verification succeeds and adds new ping event
// 3. BUG: Projection hasn't processed new event yet, so GetStatus() still returns MISSING
// 4. This creates a log spam loop where every second we see the same disagreement
func TestPingVerificationUpdatesProjectionImmediately(t *testing.T) {
	ledger := NewSyncLedger(100)
	projection := NewOnlineStatusProjection(ledger)

	// Simulate an old ONLINE event from 6 minutes ago (beyond 5min threshold)
	oldEventTime := time.Now().Add(-6 * time.Minute).UnixNano()
	oldEvent := SyncEvent{
		ID:        "old-ping-1",
		Service:   ServicePing,
		Timestamp: oldEventTime,
		Ping: &PingObservation{
			Observer: "jojo-m1",
			Target:   "raccoon",
			RTT:      42.0,
		},
	}
	ledger.AddEvent(oldEvent)

	// Process the old event
	projection.RunOnce()

	// GetStatus should return MISSING because event is >5min old
	status := projection.GetStatus("raccoon")
	if status != "MISSING" {
		t.Errorf("Expected MISSING for old event, got %s", status)
	}

	// Simulate ping verification succeeding: add a fresh ping event
	freshPingTime := time.Now().UnixNano()
	freshEvent := SyncEvent{
		ID:        "fresh-ping-1",
		Service:   ServicePing,
		Timestamp: freshPingTime,
		Ping: &PingObservation{
			Observer: "jojo-m1",
			Target:   "raccoon",
			RTT:      42.0,
		},
	}
	ledger.AddEvent(freshEvent)

	// BUG: Without processing, GetStatus() still uses old event
	// This simulates what happens in the maintenance loop - we add the ping event
	// but don't process it before the next maintenance cycle checks status again
	statusBeforeProcessing := projection.GetStatus("raccoon")
	if statusBeforeProcessing != "MISSING" {
		t.Logf("Status before processing: %s (expected MISSING due to stale projection)", statusBeforeProcessing)
	}

	// Process the new event
	projection.RunOnce()

	// NOW GetStatus should return ONLINE
	statusAfterProcessing := projection.GetStatus("raccoon")
	if statusAfterProcessing != "ONLINE" {
		t.Errorf("Expected ONLINE after processing fresh ping event, got %s", statusAfterProcessing)
	}

	// The test demonstrates the bug: between adding the event and processing it,
	// GetStatus() returns stale MISSING status, causing log spam every second
}

// TestMarkOnlineFromPingUpdatesProjectionSynchronously tests that when we verify
// a nara is online via ping, the projection is updated immediately so the next
// maintenance cycle doesn't see stale MISSING status.
//
// This reproduces the production bug where:
// 1. Old ONLINE event (>5min ago) causes GetStatus() to return MISSING
// 2. Ping verification succeeds and calls AddSignedPingObservationWithReplace
// 3. BUG: AddSignedPingObservationWithReplace removes old ping WITHOUT incrementing version
// 4. Projection's position tracking breaks (points past end of array)
// 5. RunOnce() returns empty, projection never sees new ping event
// 6. GetStatus() still returns MISSING, creating log spam every second for 60 seconds
func TestMarkOnlineFromPingUpdatesProjectionSynchronously(t *testing.T) {
	ln := testLocalNara("jojo-m1")
	network := ln.Network
	network.local.Projections = NewProjectionStore(network.local.SyncLedger)

	// Add multiple old ping events for raccoon to trigger replacement logic
	// This ensures AddSignedPingObservationWithReplace will remove an old ping
	for i := 0; i < MaxPingsPerPair; i++ {
		oldEventTime := time.Now().Add(-time.Duration(6+i) * time.Minute).UnixNano()
		oldEvent := SyncEvent{
			ID:        fmt.Sprintf("old-ping-%d", i),
			Service:   ServicePing,
			Timestamp: oldEventTime,
			Ping: &PingObservation{
				Observer: "jojo-m1",
				Target:   "raccoon",
				RTT:      42.0,
			},
		}
		network.local.SyncLedger.AddEvent(oldEvent)
	}
	network.local.Projections.OnlineStatus().RunOnce()

	// Verify GetStatus returns MISSING due to old events
	statusBefore := network.local.Projections.OnlineStatus().GetStatus("raccoon")
	if statusBefore != "MISSING" {
		t.Fatalf("Expected MISSING before ping (all events old), got %s", statusBefore)
	}

	// Simulate successful ping verification - this will remove oldest ping and add new one
	network.markOnlineFromPing("raccoon", 42.0)

	// CRITICAL: GetStatus should return ONLINE immediately after markOnlineFromPing
	// This FAILS without the version increment fix in AddSignedPingObservationWithReplace
	statusAfter := network.local.Projections.OnlineStatus().GetStatus("raccoon")
	if statusAfter != "ONLINE" {
		t.Errorf("Expected ONLINE immediately after markOnlineFromPing, got %s", statusAfter)
		t.Errorf("This is the production bug: projection didn't process new ping event due to broken position tracking")
	}
}
