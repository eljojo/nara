package nara

import (
	"testing"
	"time"

	"github.com/eljojo/nara/types"
)

func TestObservations_Record(t *testing.T) {
	ln := testLocalNara(t, "me")
	obs := ln.getMeObservation()
	obs.LastSeen = 100
	ln.setMeObservation(obs)

	savedObs := ln.getMeObservation()
	if savedObs.LastSeen != 100 {
		t.Errorf("expected LastSeen 100, got %d", savedObs.LastSeen)
	}
}

func TestObservations_Online(t *testing.T) {
	ln := testLocalNara(t, "me")
	network := ln.Network
	name := types.NaraName("other")
	network.importNara(NewNara(name))

	// 1. Initial observation via recording online
	network.recordObservationOnlineNara(name, 0)
	obs := network.local.getObservation(name)
	if obs.Online != "ONLINE" {
		t.Errorf("expected state ONLINE, got %s", obs.Online)
	}
	if obs.LastSeen == 0 {
		t.Error("expected LastSeen to be set")
	}

	// 2. Transition to OFFLINE
	obs.Online = "OFFLINE"
	obs.LastSeen = time.Now().Unix()
	network.local.setObservation(name, obs)

	obs = network.local.getObservation(name)
	if obs.isOnline() {
		t.Error("expected isOnline() to be false")
	}

	// 3. Transition back to ONLINE (should increment restarts)
	network.recordObservationOnlineNara(name, 0)
	obs = network.local.getObservation(name)
	if obs.Online != "ONLINE" {
		t.Errorf("expected back to ONLINE, got %s", obs.Online)
	}
	if obs.Restarts != 1 {
		t.Errorf("expected 1 restart, got %d", obs.Restarts)
	}
}

func TestIsGhostNara_TrueWhenNoData(t *testing.T) {
	ln := testLocalNara(t, "me")
	network := ln.Network

	// Add some neighbors that have observations but no data about the ghost
	for i := 0; i < 5; i++ {
		neighbor := NewNara(types.NaraName("neighbor-" + string(rune('a'+i))))
		network.importNara(neighbor)
		// Neighbor has empty observation about ghost (all zeros)
		neighbor.setObservation("ghost-nara", NaraObservation{})
	}

	// Add the ghost to neighbourhood
	network.importNara(NewNara(types.NaraName("ghost-nara")))

	// Ghost nara with no data from anyone
	if !network.isGhostNara("ghost-nara") {
		t.Error("expected ghost-nara to be detected as ghost when no one has data")
	}
}

func TestIsGhostNara_FalseWhenNeighborHasStartTime(t *testing.T) {
	ln := testLocalNara(t, "me")
	network := ln.Network

	// Add neighbor with StartTime data
	neighbor := NewNara("neighbor-a")
	network.importNara(neighbor)
	neighbor.setObservation("real-nara", NaraObservation{StartTime: 1234567890})

	network.importNara(NewNara("real-nara"))

	if network.isGhostNara("real-nara") {
		t.Error("expected real-nara NOT to be ghost when neighbor has StartTime")
	}
}

func TestIsGhostNara_FalseWhenNeighborHasRestarts(t *testing.T) {
	ln := testLocalNara(t, "me")
	network := ln.Network

	neighbor := NewNara("neighbor-a")
	network.importNara(neighbor)
	neighbor.setObservation("real-nara", NaraObservation{Restarts: 5})

	network.importNara(NewNara("real-nara"))

	if network.isGhostNara("real-nara") {
		t.Error("expected real-nara NOT to be ghost when neighbor has Restarts")
	}
}

func TestIsGhostNara_FalseWhenWeHaveData(t *testing.T) {
	ln := testLocalNara(t, "me")
	network := ln.Network

	// We have data, even if neighbors don't
	network.local.setObservation("known-nara", NaraObservation{StartTime: 1234567890})
	network.importNara(NewNara("known-nara"))

	if network.isGhostNara("known-nara") {
		t.Error("expected known-nara NOT to be ghost when we have StartTime")
	}
}

func TestIsGhostNaraSafeToDelete_NeverDeleteSelf(t *testing.T) {
	ln := testLocalNara(t, "me")
	network := ln.Network

	if network.isGhostNaraSafeToDelete("me") {
		t.Error("should never delete ourselves")
	}
}

func TestIsGhostNaraSafeToDelete_FalseWhenOnline(t *testing.T) {
	ln := testLocalNara(t, "me")
	network := ln.Network

	// Add enough neighbors for the check
	for i := 0; i < 5; i++ {
		neighbor := NewNara(types.NaraName("neighbor-" + string(rune('a'+i))))
		network.importNara(neighbor)
	}

	// Ghost is marked ONLINE
	network.local.setObservation("ghost", NaraObservation{Online: "ONLINE"})
	network.importNara(NewNara("ghost"))

	if network.isGhostNaraSafeToDelete("ghost") {
		t.Error("should not delete nara marked ONLINE")
	}
}

func TestIsGhostNaraSafeToDelete_FalseWhenRecentLastSeen(t *testing.T) {
	ln := testLocalNara(t, "me")
	network := ln.Network

	for i := 0; i < 5; i++ {
		neighbor := NewNara(types.NaraName("neighbor-" + string(rune('a'+i))))
		network.importNara(neighbor)
	}

	// LastSeen is recent (1 hour ago - within 24 hour window)
	oneHourAgo := time.Now().Unix() - 3600
	network.local.setObservation("recent-ghost", NaraObservation{LastSeen: oneHourAgo})
	network.importNara(NewNara("recent-ghost"))

	if network.isGhostNaraSafeToDelete("recent-ghost") {
		t.Error("should not delete nara seen within last 24 hours")
	}
}

func TestIsGhostNaraSafeToDelete_FalseWhenNeighborHasData(t *testing.T) {
	ln := testLocalNara(t, "me")
	network := ln.Network

	// One neighbor has data
	neighborWithData := NewNara("neighbor-a")
	network.importNara(neighborWithData)
	neighborWithData.setObservation("not-ghost", NaraObservation{StartTime: 12345})

	// Other neighbors don't
	for i := 1; i < 5; i++ {
		neighbor := NewNara(types.NaraName("neighbor-" + string(rune('a'+i))))
		network.importNara(neighbor)
	}

	network.importNara(NewNara("not-ghost"))

	if network.isGhostNaraSafeToDelete("not-ghost") {
		t.Error("should not delete when ANY neighbor has data")
	}
}

func TestIsGhostNaraSafeToDelete_FalseWhenNeighborThinksOnline(t *testing.T) {
	ln := testLocalNara(t, "me")
	network := ln.Network

	// Neighbor thinks it's online (even with no other data)
	neighborOnline := NewNara("neighbor-a")
	network.importNara(neighborOnline)
	neighborOnline.setObservation("maybe-ghost", NaraObservation{Online: "ONLINE"})

	for i := 1; i < 5; i++ {
		neighbor := NewNara(types.NaraName("neighbor-" + string(rune('a'+i))))
		network.importNara(neighbor)
	}

	network.importNara(NewNara("maybe-ghost"))

	if network.isGhostNaraSafeToDelete("maybe-ghost") {
		t.Error("should not delete when any neighbor thinks it's ONLINE")
	}
}

func TestIsGhostNaraSafeToDelete_FalseWhenTooFewNeighbors(t *testing.T) {
	ln := testLocalNara(t, "me")
	network := ln.Network

	// Only 2 neighbors (need at least 3)
	for i := 0; i < 2; i++ {
		neighbor := NewNara(types.NaraName("neighbor-" + string(rune('a'+i))))
		network.importNara(neighbor)
	}

	network.importNara(NewNara("ghost"))

	if network.isGhostNaraSafeToDelete("ghost") {
		t.Error("should not delete when fewer than 3 neighbors checked")
	}
}

func TestIsGhostNaraSafeToDelete_TrueWhenAllCriteriaMet(t *testing.T) {
	ln := testLocalNara(t, "me")
	network := ln.Network

	// Add enough neighbors with no data about ghost
	for i := 0; i < 5; i++ {
		neighbor := NewNara(types.NaraName("neighbor-" + string(rune('a'+i))))
		network.importNara(neighbor)
		neighbor.setObservation("true-ghost", NaraObservation{}) // all zeros
	}

	// Our observation: old LastSeen (> 24 hours), no data
	twoDaysAgo := time.Now().Unix() - 172800
	network.local.setObservation("true-ghost", NaraObservation{LastSeen: twoDaysAgo})
	network.importNara(NewNara("true-ghost"))

	if !network.isGhostNaraSafeToDelete("true-ghost") {
		t.Error("expected true-ghost to be safe to delete when all criteria met")
	}
}

func TestIsGhostNaraSafeToDelete_TrueWhenNeverSeen(t *testing.T) {
	ln := testLocalNara(t, "me")
	network := ln.Network

	for i := 0; i < 5; i++ {
		neighbor := NewNara(types.NaraName("neighbor-" + string(rune('a'+i))))
		network.importNara(neighbor)
	}

	// LastSeen = 0 (never seen)
	network.local.setObservation("never-seen", NaraObservation{LastSeen: 0})
	network.importNara(NewNara("never-seen"))

	if !network.isGhostNaraSafeToDelete("never-seen") {
		t.Error("expected never-seen ghost to be safe to delete")
	}
}

func TestGarbageCollectGhostNaras(t *testing.T) {
	ln := testLocalNara(t, "me")
	network := ln.Network

	// Add neighbors - give them real data so they're not detected as ghosts
	for i := 0; i < 5; i++ {
		name := types.NaraName("neighbor-" + string(rune('a'+i)))
		neighbor := NewNara(name)
		network.importNara(neighbor)
		// Mark them as having StartTime so they're "real" naras
		network.local.setObservation(name, NaraObservation{StartTime: 1234567890})
	}

	// Add a true ghost (should be deleted)
	twoDaysAgo := time.Now().Unix() - 172800
	network.local.setObservation(types.NaraName("ghost-to-delete"), NaraObservation{LastSeen: twoDaysAgo})
	network.importNara(NewNara(types.NaraName("ghost-to-delete")))

	// Add a real nara (should NOT be deleted)
	network.local.setObservation("real-nara", NaraObservation{StartTime: 1234567890, Restarts: 5})
	network.importNara(NewNara("real-nara"))

	// Add a recent ghost (should NOT be deleted - seen recently)
	oneHourAgo := time.Now().Unix() - 3600
	network.local.setObservation("recent-ghost", NaraObservation{LastSeen: oneHourAgo})
	network.importNara(NewNara("recent-ghost"))

	// Add events for different naras to test event deletion
	now := time.Now().UnixNano()

	// Events FROM the ghost nara (should be deleted)
	ghostHeyThere := SyncEvent{
		Timestamp: now,
		Service:   ServiceHeyThere,
		HeyThere:  &HeyThereEvent{From: "ghost-to-delete", PublicKey: "ghost-key"},
	}
	ghostHeyThere.ComputeID()
	ln.SyncLedger.AddEvent(ghostHeyThere)

	// Events ABOUT the ghost nara (should be deleted)
	ghostObservation := SyncEvent{
		Timestamp:   now + 1,
		Service:     ServiceObservation,
		Observation: &ObservationEventPayload{Observer: "me", Subject: "ghost-to-delete", Type: "first-seen", StartTime: 12345, Importance: 1},
	}
	ghostObservation.ComputeID()
	ln.SyncLedger.AddEvent(ghostObservation)

	// Events FROM real-nara (should NOT be deleted)
	realHeyThere := SyncEvent{
		Timestamp: now + 2,
		Service:   ServiceHeyThere,
		HeyThere:  &HeyThereEvent{From: "real-nara", PublicKey: "real-key"},
	}
	realHeyThere.ComputeID()
	ln.SyncLedger.AddEvent(realHeyThere)

	// Events ABOUT real-nara (should NOT be deleted)
	realObservation := SyncEvent{
		Timestamp:   now + 3,
		Service:     ServiceObservation,
		Observation: &ObservationEventPayload{Observer: "me", Subject: "real-nara", Type: "first-seen", StartTime: 12345, Importance: 1},
	}
	realObservation.ComputeID()
	ln.SyncLedger.AddEvent(realObservation)

	// Events between other naras (should NOT be deleted)
	otherEvent := SyncEvent{
		Timestamp:   now + 4,
		Service:     ServiceObservation,
		Observation: &ObservationEventPayload{Observer: "neighbor-a", Subject: "neighbor-b", Type: "first-seen", StartTime: 12345, Importance: 1},
	}
	otherEvent.ComputeID()
	ln.SyncLedger.AddEvent(otherEvent)

	// Verify initial event counts
	initialEventCount := ln.SyncLedger.EventCount()
	if initialEventCount != 5 {
		t.Errorf("expected 5 initial events, got %d", initialEventCount)
	}

	// Run GC
	deleted := network.garbageCollectGhostNaras()

	if deleted != 1 {
		t.Errorf("expected 1 ghost deleted, got %d", deleted)
	}

	// Verify ghost-to-delete is gone from neighbourhood
	network.local.mu.Lock()
	_, ghostExists := network.Neighbourhood["ghost-to-delete"]
	_, realExists := network.Neighbourhood["real-nara"]
	_, recentExists := network.Neighbourhood["recent-ghost"]
	network.local.mu.Unlock()

	if ghostExists {
		t.Error("ghost-to-delete should have been removed from Neighbourhood")
	}
	if !realExists {
		t.Error("real-nara should still exist in Neighbourhood")
	}
	if !recentExists {
		t.Error("recent-ghost should still exist (seen recently)")
	}

	// Verify ghost-to-delete is gone from observations
	ghostObs := network.local.getObservation("ghost-to-delete")
	if ghostObs.LastSeen != 0 {
		t.Error("ghost-to-delete should have been removed from Observations")
	}

	// Verify real-nara observation still exists
	realObs := network.local.getObservation("real-nara")
	if realObs.StartTime != 1234567890 {
		t.Error("real-nara observation should still exist")
	}

	// Verify event deletion - ghost events should be removed, others should remain
	finalEventCount := ln.SyncLedger.EventCount()
	expectedEvents := 3 // real-nara hey_there, real-nara observation, neighbor-a→neighbor-b observation
	if finalEventCount != expectedEvents {
		t.Errorf("expected %d events after GC, got %d", expectedEvents, finalEventCount)
	}

	// Verify specific events
	allEvents := ln.SyncLedger.GetAllEvents()

	// Check that ghost events are gone
	for _, e := range allEvents {
		if e.HeyThere != nil && e.HeyThere.From == "ghost-to-delete" {
			t.Error("ghost-to-delete hey_there event should have been deleted")
		}
		if e.Observation != nil && e.Observation.Subject == "ghost-to-delete" {
			t.Error("ghost-to-delete observation event should have been deleted")
		}
	}

	// Check that real-nara events are still present
	foundRealHeyThere := false
	foundRealObservation := false
	foundOtherEvent := false
	for _, e := range allEvents {
		if e.HeyThere != nil && e.HeyThere.From == "real-nara" {
			foundRealHeyThere = true
		}
		if e.Observation != nil && e.Observation.Subject == "real-nara" {
			foundRealObservation = true
		}
		if e.Observation != nil && e.Observation.Observer == "neighbor-a" && e.Observation.Subject == "neighbor-b" {
			foundOtherEvent = true
		}
	}

	if !foundRealHeyThere {
		t.Error("real-nara hey_there event should still exist")
	}
	if !foundRealObservation {
		t.Error("real-nara observation event should still exist")
	}
	if !foundOtherEvent {
		t.Error("neighbor-a→neighbor-b observation event should still exist")
	}
}

// TestPingVerificationRateLimitBug reproduces a production bug where:
// 1. Maintenance cycle detects MISSING, pings successfully, keeps ONLINE
// 2. The reportMissingWithDelay goroutine fires (from before the ping)
// 3. It calls verifyOnlineWithPing again within 60s (rate-limited)
// 4. BUG: Rate-limit returns false, treated as failed ping, emits MISSING event
//
// Expected: Rate-limited ping should return cached result (true), not false
//
// Production logs showed:
//
//	🔍 Verification ping to racoon succeeded (4.00ms) - still online!
//	🔍 Disagreement resolved: racoon reported MISSING but ping succeeded - keeping ONLINE
//	observation: racoon has disappeared (verified)
//	📊 Status-change observation event: racoon → MISSING (after 9s delay, verified)
func TestPingVerificationRateLimitBug(t *testing.T) {
	// Setup
	ln := testLocalNara(t, "me")
	network := ln.Network
	other := NewNara("other")
	network.importNara(other)

	// Ensure nara is not in booting state (set LastRestart to 200 seconds ago)
	meObs := ln.getMeObservation()
	meObs.LastRestart = time.Now().Unix() - 200
	meObs.LastSeen = time.Now().Unix()
	ln.setMeObservation(meObs)

	// Reset rate limit state (in case previous tests ran)
	resetVerifyPingRateLimit()

	// Configure test ping that succeeds
	pingCallCount := 0
	network.testPingFunc = func(name types.NaraName) (bool, error) {
		pingCallCount++
		t.Logf("Test ping called (call #%d) for %s", pingCallCount, name)
		return true, nil // ping succeeds
	}

	// 1. Mark nara as ONLINE initially
	network.recordObservationOnlineNara("other", 0)
	obs := network.local.getObservation("other")
	if obs.Online != "ONLINE" {
		t.Fatalf("expected ONLINE, got %s", obs.Online)
	}

	// 2. First ping verification (simulating maintenance cycle or delayed report)
	result1 := network.verifyOnlineWithPing("other")
	if !result1 {
		t.Fatal("First ping should succeed")
	}
	if pingCallCount != 1 {
		t.Errorf("First call: expected 1 ping, got %d", pingCallCount)
	}
	t.Logf("First verification: success=%v, pings=%d", result1, pingCallCount)

	// 3. Second ping verification within 60s (simulating reportMissingWithDelay)
	// This should be rate-limited and return the cached result
	result2 := network.verifyOnlineWithPing("other")

	// BUG: Without the fix, this returns false (treating rate-limit as failed ping)
	// EXPECTED: Should return true (cached result from successful ping)
	if !result2 {
		t.Errorf("Second verification (rate-limited): expected true (cached result), got false - THIS IS THE BUG")
	}

	// Verify ping wasn't called again (rate-limited)
	if pingCallCount != 1 {
		t.Errorf("Second call: expected no new ping (rate-limited), but got %d total calls", pingCallCount)
	}
	t.Logf("Second verification (rate-limited): success=%v, pings=%d", result2, pingCallCount)
}
