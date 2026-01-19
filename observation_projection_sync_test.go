package nara

import (
	"fmt"
	"testing"
	"time"

	"github.com/eljojo/nara/identity"
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
	if _, err := projection.RunOnce(); err != nil {
		t.Fatalf("Failed to run projection: %v", err)
	}

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
	if _, err := projection.RunOnce(); err != nil {
		t.Fatalf("Failed to run projection: %v", err)
	}

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
	ln := testLocalNara(t, "jojo-m1")
	network := ln.Network
	network.local.Projections = NewProjectionStore(network.local.SyncLedger)

	// Add multiple old ping events for raccoon to trigger replacement logic
	// This ensures AddSignedPingObservationWithReplace will remove an old ping
	for i := 0; i < MaxPingsPerTarget; i++ {
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
	if _, err := network.local.Projections.OnlineStatus().RunOnce(); err != nil {
		t.Fatalf("Failed to run projection: %v", err)
	}

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

// TestBootTimeChauDoesNotClobberOnlineStatus reproduces the boot-time race condition where:
// 1. Nara is booting (uptime < 120s)
// 2. Live hey_there marks a neighbor as ONLINE (observation state)
// 3. Backfill brings in an old chau event for that neighbor
// 4. processChauSyncEvents checks hasMoreRecentHeyThere, but the hey_there may not be in ledger yet
// 5. BUG: Neighbor gets marked OFFLINE despite being actually online
// 6. User sees naras flip from ONLINE to OFFLINE during backfill, then back to ONLINE after boot
//
// Long-term direction: processChauSyncEvents should NOT directly update observations.
// Instead, projections should be the source of truth for online status, and
// observationMaintenanceOnce should sync from projections.
// For now, we fix by skipping processChauSyncEvents during boot.
func TestBootTimeChauDoesNotClobberOnlineStatus(t *testing.T) {
	// Create a LocalNara that is booting (default state after creation)
	observer := testLocalNara(t, "observer")
	network := observer.Network
	network.local.Projections = NewProjectionStore(network.local.SyncLedger)

	// Create a neighbor nara with valid keypair for signing
	neighbor := testLocalNara(t, "raccoon")

	// Observer knows about neighbor (has their public key)
	neighborNara := NewNara("raccoon")
	neighborNara.Status.PublicKey = identity.FormatPublicKey(neighbor.Keypair.PublicKey)
	network.importNara(neighborNara)

	// Simulate: neighbor's hey_there arrived via live MQTT and marked them ONLINE
	// This happens before their hey_there SyncEvent is in our ledger
	observation := observer.getObservation("raccoon")
	observation.Online = "ONLINE"
	observation.LastSeen = time.Now().Unix()
	observer.setObservation("raccoon", observation)

	// Verify neighbor is ONLINE before chau processing
	obsBefore := observer.getObservation("raccoon")
	if obsBefore.Online != "ONLINE" {
		t.Fatalf("Expected neighbor to be ONLINE before chau, got %s", obsBefore.Online)
	}

	// Simulate: backfill brings in an OLD chau event from the neighbor
	// This chau is from before the neighbor came back online
	oldChauTime := time.Now().Add(-5 * time.Minute).UnixNano()

	// Create a signed chau event with the old timestamp
	// (processChauSyncEvents verifies signatures, so we must sign after setting timestamp)
	chauEvent := SyncEvent{
		Timestamp: oldChauTime,
		Service:   ServiceChau,
		Chau: &ChauEvent{
			From:      "raccoon",
			PublicKey: identity.FormatPublicKey(neighbor.Keypair.PublicKey),
			ID:        neighbor.ID,
		},
	}
	chauEvent.ComputeID()
	chauEvent.Sign("raccoon", neighbor.Keypair)

	// Add to ledger (simulating backfill)
	observer.SyncLedger.MergeEvents([]SyncEvent{chauEvent})

	// NOTE: No hey_there in ledger yet (the live one updated observation directly
	// but the SyncEvent hasn't been added/synced yet - this is the race condition)

	// Process chau events from ledger
	events := observer.SyncLedger.GetAllEvents()
	network.processChauSyncEvents(events)

	// BUG: The neighbor should still be ONLINE, but processChauSyncEvents marked them OFFLINE
	// because hasMoreRecentHeyThere found no hey_there in the ledger
	obsAfter := observer.getObservation("raccoon")

	// This is the failing assertion that demonstrates the bug:
	// During boot, processChauSyncEvents should NOT clobber ONLINE status
	if obsAfter.Online != "ONLINE" {
		t.Errorf("BUG REPRODUCED: Neighbor was ONLINE but got marked %s during boot chau processing", obsAfter.Online)
		t.Errorf("This causes the boot-time status flapping: naras appear offline during backfill")
		t.Errorf("Fix: Skip processChauSyncEvents during boot (isBooting == true)")
	}
}
