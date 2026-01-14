package nara

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func TestPruneInactiveNaras_Zombies(t *testing.T) {
	ln := testLocalNaraWithParams(t, "test-nara", 50, 1000)
	now := time.Now().Unix()

	// Add a zombie nara (never seen, no activity, malformed entry)
	zombieNara := NewNara("zombie-nara")
	zombieNara.Status.Flair = "" // No flair
	zombieNara.Status.Buzz = 0   // No buzz
	ln.Network.importNara(zombieNara)
	ln.setObservation("zombie-nara", NaraObservation{
		LastSeen:  0,           // Never seen (malformed)
		StartTime: now - 86400, // "First seen" 1 day ago but never actually active
		Online:    "OFFLINE",
	})

	// Run pruning
	ln.Network.pruneInactiveNaras()

	// Zombie should be removed immediately
	if ln.Network.Neighbourhood["zombie-nara"] != nil {
		t.Error("zombie-nara should be pruned (never seen, malformed entry)")
	}
}

func TestPruneInactiveNaras_Newcomers(t *testing.T) {
	ln := testLocalNaraWithParams(t, "test-nara", 50, 1000)
	now := time.Now().Unix()

	// Add a newcomer that's been offline for 25 hours (should be pruned)
	newcomerOffline := NewNara("newcomer-offline")
	newcomerOffline.Status.Buzz = 5
	ln.Network.importNara(newcomerOffline)
	ln.setObservation("newcomer-offline", NaraObservation{
		LastSeen:  now - (25 * 3600), // 25 hours offline
		StartTime: now - (30 * 3600), // First seen 30 hours ago (newcomer)
		Online:    "OFFLINE",
	})

	// Add a newcomer that's been offline for 12 hours (should NOT be pruned)
	newcomerRecent := NewNara("newcomer-recent")
	newcomerRecent.Status.Buzz = 5
	ln.Network.importNara(newcomerRecent)
	ln.setObservation("newcomer-recent", NaraObservation{
		LastSeen:  now - (12 * 3600), // 12 hours offline
		StartTime: now - (20 * 3600), // First seen 20 hours ago (newcomer)
		Online:    "OFFLINE",
	})

	ln.Network.pruneInactiveNaras()

	// Newcomer offline for 25h should be pruned
	if ln.Network.Neighbourhood["newcomer-offline"] != nil {
		t.Error("newcomer-offline should be pruned (24h+ offline, < 24h old)")
	}

	// Newcomer offline for 12h should remain
	if ln.Network.Neighbourhood["newcomer-recent"] == nil {
		t.Error("newcomer-recent should not be pruned (only 12h offline)")
	}
}

func TestPruneInactiveNaras_Established(t *testing.T) {
	ln := testLocalNaraWithParams(t, "test-nara", 50, 1000)
	now := time.Now().Unix()

	// Add established nara offline for 8 days (should be pruned)
	establishedOld := NewNara("established-old")
	establishedOld.Status.Flair = "🍕"
	establishedOld.Status.Buzz = 10
	ln.Network.importNara(establishedOld)
	ln.setObservation("established-old", NaraObservation{
		LastSeen:  now - (8 * 86400),  // 8 days offline
		StartTime: now - (10 * 86400), // First seen 10 days ago (established)
		Online:    "OFFLINE",
	})

	// Add established nara offline for 3 days (should NOT be pruned - 7d grace period)
	establishedRecent := NewNara("established-recent")
	establishedRecent.Status.Flair = "🌟"
	establishedRecent.Status.Buzz = 15
	ln.Network.importNara(establishedRecent)
	ln.setObservation("established-recent", NaraObservation{
		LastSeen:  now - (3 * 86400), // 3 days offline
		StartTime: now - (5 * 86400), // First seen 5 days ago (established)
		Online:    "OFFLINE",
	})

	ln.Network.pruneInactiveNaras()

	// Established offline for 8d should be pruned
	if ln.Network.Neighbourhood["established-old"] != nil {
		t.Error("established-old should be pruned (8d offline, 7d threshold)")
	}

	// Established offline for 3d should remain (within 7d grace period)
	if ln.Network.Neighbourhood["established-recent"] == nil {
		t.Error("established-recent should not be pruned (only 3d offline, 7d grace period)")
	}
}

func TestPruneInactiveNaras_Veterans(t *testing.T) {
	ln := testLocalNaraWithParams(t, "test-nara", 50, 1000)
	now := time.Now().Unix()

	// Add veteran nara (known for 100 days) that's been offline for 30 days
	// Veterans should NEVER be auto-pruned - they're community members
	bart := NewNara("bart")
	bart.Status.Flair = "🗿"
	bart.Status.Buzz = 20
	ln.Network.importNara(bart)
	ln.setObservation("bart", NaraObservation{
		LastSeen:  now - (30 * 86400),  // 30 days offline
		StartTime: now - (100 * 86400), // Known for 100 days (veteran!)
		Online:    "OFFLINE",
	})

	// Add another veteran (r2d2) offline for 60 days
	r2d2 := NewNara("r2d2")
	r2d2.Status.Flair = "🗿"
	r2d2.Status.Buzz = 15
	ln.Network.importNara(r2d2)
	ln.setObservation("r2d2", NaraObservation{
		LastSeen:  now - (60 * 86400),  // 60 days offline!
		StartTime: now - (200 * 86400), // Known for 200 days (ancient veteran)
		Online:    "OFFLINE",
	})

	ln.Network.pruneInactiveNaras()

	// Veterans should NEVER be pruned, even after months offline
	if ln.Network.Neighbourhood["bart"] == nil {
		t.Error("bart (veteran) should not be pruned even after 30 days offline")
	}

	if ln.Network.Neighbourhood["r2d2"] == nil {
		t.Error("r2d2 (veteran) should not be pruned even after 60 days offline")
	}
}

func TestPruneInactiveNaras_OnlineNeverPruned(t *testing.T) {
	ln := testLocalNaraWithParams(t, "test-nara", 50, 1000)
	now := time.Now().Unix()

	// Add an ONLINE nara (should never be pruned regardless of age)
	onlineNara := NewNara("online-nara")
	onlineNara.Status.Flair = "🚀"
	onlineNara.Status.Buzz = 10
	ln.Network.importNara(onlineNara)
	ln.setObservation("online-nara", NaraObservation{
		LastSeen:  now - 60,   // Seen 1 minute ago
		StartTime: now - 3600, // First seen 1 hour ago
		Online:    "ONLINE",
	})

	ln.Network.pruneInactiveNaras()

	// ONLINE naras are never pruned
	if ln.Network.Neighbourhood["online-nara"] == nil {
		t.Error("online-nara should never be pruned while ONLINE")
	}
}

func TestPruneInactiveNaras_Integration(t *testing.T) {
	// Comprehensive test with realistic mix of naras
	ln := testLocalNaraWithParams(t, "test-nara", 50, 1000)
	now := time.Now().Unix()

	// Setup: mix of zombies, newcomers, established, and veterans
	testCases := []struct {
		name         string
		flair        string
		buzz         int
		lastSeen     int64 // seconds ago
		startTime    int64 // seconds ago
		online       string
		shouldRemove bool
		reason       string
	}{
		// Zombies (should be removed) - use -1 for lastSeen to indicate "never seen"
		{"zombie-1", "", 0, -1, 86400, "OFFLINE", true, "zombie: never seen"},
		{"zombie-2", "", 0, -1, -1, "OFFLINE", true, "zombie: no timestamps, no activity"},

		// Newcomers
		{"newcomer-gone", "", 5, 25 * 3600, 30 * 3600, "OFFLINE", true, "newcomer: 25h offline"},
		{"newcomer-active", "🌱", 5, 3600, 12 * 3600, "OFFLINE", false, "newcomer: only 1h offline"},

		// Established
		{"established-gone", "🍕", 10, 8 * 86400, 10 * 86400, "OFFLINE", true, "established: 8d offline (>7d)"},
		{"established-away", "🥪", 10, 3 * 86400, 5 * 86400, "OFFLINE", false, "established: 3d offline (<7d)"},

		// Veterans (NEVER pruned)
		{"bart", "🗿", 20, 30 * 86400, 284 * 86400, "OFFLINE", false, "veteran: 30d offline but kept"},
		{"lisa", "🗿", 15, 60 * 86400, 1665 * 86400, "OFFLINE", false, "veteran: 60d offline but kept"},
		{"r2d2", "🗿", 18, 7 * 86400, 100 * 86400, "OFFLINE", false, "veteran: 7d offline but kept"},

		// Online (never pruned)
		{"condorito", "🏄", 14, 300, 34 * 3600, "ONLINE", false, "online: currently active"},
		{"nelly", "🥪", 5, 120, 1481 * 86400, "ONLINE", false, "online: veteran active"},
	}

	for _, tc := range testCases {
		nara := NewNara(tc.name)
		nara.Status.Flair = tc.flair
		nara.Status.Buzz = tc.buzz
		ln.Network.importNara(nara)

		// Use -1 as sentinel for "never seen" (LastSeen=0) or "no start time" (StartTime=0)
		lastSeen := now - tc.lastSeen
		if tc.lastSeen < 0 {
			lastSeen = 0
		}
		startTime := now - tc.startTime
		if tc.startTime < 0 {
			startTime = 0
		}

		ln.setObservation(tc.name, NaraObservation{
			LastSeen:  lastSeen,
			StartTime: startTime,
			Online:    tc.online,
		})
	}

	initialCount := len(ln.Network.Neighbourhood)
	t.Logf("Initial nara count: %d", initialCount)

	// Run pruning
	ln.Network.pruneInactiveNaras()

	// Verify each test case
	for _, tc := range testCases {
		exists := ln.Network.Neighbourhood[tc.name] != nil
		if tc.shouldRemove && exists {
			t.Errorf("%s should be pruned (%s) but still exists", tc.name, tc.reason)
		}
		if !tc.shouldRemove && !exists {
			t.Errorf("%s should NOT be pruned (%s) but was removed", tc.name, tc.reason)
		}
	}

	// Count removals
	expectedRemoved := 0
	for _, tc := range testCases {
		if tc.shouldRemove {
			expectedRemoved++
		}
	}

	finalCount := len(ln.Network.Neighbourhood)
	actualRemoved := initialCount - finalCount

	if actualRemoved != expectedRemoved {
		t.Errorf("Expected to remove %d naras, actually removed %d", expectedRemoved, actualRemoved)
	}

	t.Logf("✅ Pruned %d/%d naras as expected", actualRemoved, expectedRemoved)
}

func TestPruneInactiveNaras_RemovesFromObservations(t *testing.T) {
	// This test verifies that pruning removes naras from BOTH the Neighbourhood
	// AND from our own Observations. The bug was that we only removed from
	// Neighbourhood but forgot to properly lock and remove from Observations.
	ln := testLocalNaraWithParams(t, "test-nara", 50, 1000)
	now := time.Now().Unix()

	// Add a newcomer that should be pruned (offline for 25 hours)
	newcomerOffline := NewNara("newcomer-offline")
	newcomerOffline.Status.Buzz = 5
	ln.Network.importNara(newcomerOffline)
	ln.setObservation("newcomer-offline", NaraObservation{
		LastSeen:  now - (25 * 3600), // 25 hours offline
		StartTime: now - (30 * 3600), // First seen 30 hours ago (newcomer)
		Online:    "OFFLINE",
	})

	// Add a zombie that should be pruned immediately
	zombieNara := NewNara("zombie-nara")
	ln.Network.importNara(zombieNara)
	ln.setObservation("zombie-nara", NaraObservation{
		LastSeen:  0,           // Never seen (malformed)
		StartTime: now - 86400, // "First seen" 1 day ago but never actually active
		Online:    "OFFLINE",
	})

	// Add an established nara that should NOT be pruned (only 3 days offline)
	establishedOk := NewNara("established-ok")
	establishedOk.Status.Flair = "🌟"
	ln.Network.importNara(establishedOk)
	ln.setObservation("established-ok", NaraObservation{
		LastSeen:  now - (3 * 86400), // 3 days offline
		StartTime: now - (5 * 86400), // First seen 5 days ago (established)
		Online:    "OFFLINE",
	})

	// Verify observations exist before pruning
	ln.Me.mu.Lock()
	if _, exists := ln.Me.Status.Observations["newcomer-offline"]; !exists {
		t.Error("newcomer-offline observation should exist before pruning")
	}
	if _, exists := ln.Me.Status.Observations["zombie-nara"]; !exists {
		t.Error("zombie-nara observation should exist before pruning")
	}
	if _, exists := ln.Me.Status.Observations["established-ok"]; !exists {
		t.Error("established-ok observation should exist before pruning")
	}
	ln.Me.mu.Unlock()

	// Run pruning
	ln.Network.pruneInactiveNaras()

	// Verify naras are removed from Neighbourhood
	if ln.Network.Neighbourhood["newcomer-offline"] != nil {
		t.Error("newcomer-offline should be removed from Neighbourhood")
	}
	if ln.Network.Neighbourhood["zombie-nara"] != nil {
		t.Error("zombie-nara should be removed from Neighbourhood")
	}
	if ln.Network.Neighbourhood["established-ok"] == nil {
		t.Error("established-ok should NOT be removed from Neighbourhood")
	}

	// THE KEY TEST: Verify observations are also removed
	ln.Me.mu.Lock()
	if _, exists := ln.Me.Status.Observations["newcomer-offline"]; exists {
		t.Error("newcomer-offline observation should be removed after pruning")
	}
	if _, exists := ln.Me.Status.Observations["zombie-nara"]; exists {
		t.Error("zombie-nara observation should be removed after pruning")
	}
	if _, exists := ln.Me.Status.Observations["established-ok"]; !exists {
		t.Error("established-ok observation should NOT be removed (nara kept)")
	}
	ln.Me.mu.Unlock()
}

func TestPruneInactiveNaras_RaceCondition(t *testing.T) {
	// This test verifies that pruning properly locks when removing from observations.
	// Without proper locking, concurrent access to observations would cause a race.
	ln := testLocalNaraWithParams(t, "test-nara", 50, 1000)
	now := time.Now().Unix()

	// Add multiple naras that should be pruned
	for i := 0; i < 10; i++ {
		name := "zombie-" + string(rune('a'+i))
		zombieNara := NewNara(name)
		ln.Network.importNara(zombieNara)
		ln.setObservation(name, NaraObservation{
			LastSeen:  0,           // Never seen
			StartTime: now - 86400, // Old zombie
			Online:    "OFFLINE",
		})
	}

	// Concurrent access: one goroutine pruning, another reading observations
	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: Prune naras
	go func() {
		defer wg.Done()
		ln.Network.pruneInactiveNaras()
	}()

	// Goroutine 2: Concurrently iterate observations (this would race without proper locking)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			ln.Me.mu.Lock()
			for k, v := range ln.Me.Status.Observations {
				_ = k
				_ = v.Online
			}
			ln.Me.mu.Unlock()
			time.Sleep(1 * time.Microsecond)
		}
	}()

	wg.Wait()

	// Verify all zombies were removed
	for i := 0; i < 10; i++ {
		name := "zombie-" + string(rune('a'+i))
		if ln.Network.Neighbourhood[name] != nil {
			t.Errorf("%s should be removed from Neighbourhood", name)
		}
		ln.Me.mu.Lock()
		if _, exists := ln.Me.Status.Observations[name]; exists {
			t.Errorf("%s observation should be removed", name)
		}
		ln.Me.mu.Unlock()
	}
}

func TestPruneInactiveNaras_HttpApi(t *testing.T) {
	// This test verifies that pruned naras don't show up in the HTTP API response.
	// This replicates the user's bug report where pruned naras still appeared in api.json
	ln := testLocalNaraWithParams(t, "test-nara", 50, 1000)
	now := time.Now().Unix()

	// Add a newcomer that should be pruned (offline for 25 hours)
	newcomerOffline := NewNara("newcomer-offline")
	newcomerOffline.Status.Buzz = 5
	ln.Network.importNara(newcomerOffline)
	ln.setObservation("newcomer-offline", NaraObservation{
		LastSeen:  now - (25 * 3600), // 25 hours offline
		StartTime: now - (30 * 3600), // First seen 30 hours ago (newcomer)
		Online:    "OFFLINE",
	})

	// Add a zombie that should be pruned immediately
	zombieNara := NewNara("zombie-nara")
	ln.Network.importNara(zombieNara)
	ln.setObservation("zombie-nara", NaraObservation{
		LastSeen:  0,           // Never seen (malformed)
		StartTime: now - 86400, // "First seen" 1 day ago but never actually active
		Online:    "OFFLINE",
	})

	// Add an established nara that should NOT be pruned (only 3 days offline)
	establishedOk := NewNara("established-ok")
	establishedOk.Status.Flair = "🌟"
	establishedOk.Status.Buzz = 10
	ln.Network.importNara(establishedOk)
	ln.setObservation("established-ok", NaraObservation{
		LastSeen:  now - (3 * 86400), // 3 days offline
		StartTime: now - (5 * 86400), // First seen 5 days ago (established)
		Online:    "OFFLINE",
	})

	// Run pruning - this should remove newcomer-offline and zombie-nara
	ln.Network.pruneInactiveNaras()

	// Now simulate what the HTTP API does: marshal Me.Status to JSON and check observations
	ln.Me.mu.Lock()
	statusJSON, err := json.Marshal(ln.Me.Status)
	ln.Me.mu.Unlock()

	if err != nil {
		t.Fatalf("Failed to marshal status: %v", err)
	}

	var parsedStatus map[string]interface{}
	if err := json.Unmarshal(statusJSON, &parsedStatus); err != nil {
		t.Fatalf("Failed to unmarshal status: %v", err)
	}

	observations, ok := parsedStatus["Observations"].(map[string]interface{})
	if !ok {
		t.Fatal("Observations not found in status JSON")
	}

	t.Logf("Observations in API response: %d entries", len(observations))
	for name := range observations {
		t.Logf("  - %s", name)
	}

	// THE KEY TEST: Verify pruned naras don't appear in the API response
	if _, exists := observations["newcomer-offline"]; exists {
		t.Error("newcomer-offline should NOT appear in API response after pruning")
	}
	if _, exists := observations["zombie-nara"]; exists {
		t.Error("zombie-nara should NOT appear in API response after pruning")
	}
	if _, exists := observations["established-ok"]; !exists {
		t.Error("established-ok SHOULD appear in API response (not pruned)")
	}
}

func TestPruneInactiveNaras_ObservationMaintenanceRace(t *testing.T) {
	// This test verifies the race condition where observationMaintenance()
	// could re-add observations that were just pruned.
	//
	// The bug: observationMaintenance copies observations, then pruning runs
	// and removes naras, then observationMaintenance writes back the old copy,
	// re-adding the pruned naras.
	//
	// The fix: observationMaintenance now holds network.local.mu during both
	// the existence check AND the observation write (atomic check-and-write).
	ln := testLocalNaraWithParams(t, "test-nara", 50, 1000)
	now := time.Now().Unix()

	// Add a zombie nara that should be pruned
	zombieNara := NewNara("zombie-nara")
	ln.Network.importNara(zombieNara)
	ln.setObservation("zombie-nara", NaraObservation{
		LastSeen:  0,           // Never seen (malformed)
		StartTime: now - 86400, // "First seen" 1 day ago but never actually active
		Online:    "OFFLINE",
	})

	// Simulate the race: run observationMaintenanceOnce in background
	// It will snapshot observations (including zombie), then we'll prune,
	// then it will try to process its stale snapshot
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		// Brief pause to let pruning happen first
		time.Sleep(50 * time.Millisecond)
		// Call the real observationMaintenance function
		ln.Network.observationMaintenanceOnce()
	}()

	// Give goroutine time to start
	time.Sleep(10 * time.Millisecond)

	// Now pruning runs and removes the zombie
	ln.Network.pruneInactiveNaras()

	// Verify zombie was removed
	if ln.Network.Neighbourhood["zombie-nara"] != nil {
		t.Error("zombie-nara should be removed from Neighbourhood")
	}
	ln.Me.mu.Lock()
	if _, exists := ln.Me.Status.Observations["zombie-nara"]; exists {
		t.Error("zombie-nara observation should be removed after pruning")
	}
	ln.Me.mu.Unlock()

	// Wait for observationMaintenance goroutine to finish
	wg.Wait()

	// Verify zombie is still not in observations (the fix prevents re-adding)
	ln.Me.mu.Lock()
	if _, exists := ln.Me.Status.Observations["zombie-nara"]; exists {
		t.Error("zombie-nara observation should NOT be re-added by observationMaintenance after pruning")
	}
	ln.Me.mu.Unlock()
}

func TestPruneInactiveNaras_FullCleanup(t *testing.T) {
	// This test verifies that pruning removes all traces of a nara,
	// preventing re-discovery from any event type.
	ln := testLocalNaraWithParams(t, "test-nara", 50, 1000)
	now := time.Now().Unix()

	zombieName := "ghost-nara"
	zombieNara := NewNara(zombieName)
	ln.Network.importNara(zombieNara)
	ln.setObservation(zombieName, NaraObservation{
		LastSeen:  0,
		StartTime: now - 86400,
		Online:    "OFFLINE",
	})

	// Add various events where ghost is involved
	events := []SyncEvent{
		NewPingSyncEvent("test-nara", zombieName, 42.0),
		NewSocialSyncEvent("tease", zombieName, "test-nara", "random", ""),
		NewHeyThereSyncEvent(zombieName, "pubkey", "1.2.3.4", "id", ln.Keypair),
	}

	for _, e := range events {
		ln.Network.local.SyncLedger.AddEvent(e)
		if !ln.Network.local.SyncLedger.HasEvent(e.ID) {
			t.Errorf("Event %s should exist before pruning", e.Service)
		}
	}

	// Run pruning
	ln.Network.pruneInactiveNaras()

	// 1. Verify nara is gone from Neighbourhood
	if ln.Network.Neighbourhood[zombieName] != nil {
		t.Error("ghost-nara should be removed from Neighbourhood")
	}

	// 2. Verify all events are gone
	for _, e := range events {
		if ln.Network.local.SyncLedger.HasEvent(e.ID) {
			t.Errorf("Event %s should be removed from SyncLedger", e.Service)
		}
	}

	// 3. Verify re-discovery doesn't happen when processing the ledger
	allEvents := ln.Network.local.SyncLedger.GetAllEvents()
	ln.Network.discoverNarasFromEvents(allEvents)

	if ln.Network.Neighbourhood[zombieName] != nil {
		t.Error("ghost-nara should NOT be re-discovered after pruning events")
	}

	// 4. Verify no ping events for the ghost nara
	pingEvents := ln.Network.local.SyncLedger.GetPingObservations()
	for _, ping := range pingEvents {
		if ping.Observer == zombieName || ping.Target == zombieName {
			t.Errorf("Ping event for ghost nara (%s -> %s) found after pruning", ping.Observer, ping.Target)
		}
	}
}

func TestPruneInactiveNaras_PrunesEvents(t *testing.T) {
	// This test verifies that pruning removes events from the SyncLedger.
	// This prevents "ghost naras" from being re-discovered by discoverNarasFromEvents.
	ln := testLocalNaraWithParams(t, "test-nara", 50, 1000)
	now := time.Now().Unix()

	// Add a zombie nara that should be pruned
	zombieName := "ghost-nara"
	zombieNara := NewNara(zombieName)
	ln.Network.importNara(zombieNara)
	ln.setObservation(zombieName, NaraObservation{
		LastSeen:  0,           // Never seen
		StartTime: now - 86400, // Old zombie
		Online:    "OFFLINE",
	})

	// Add some events for this ghost nara
	event := NewPingSyncEvent("test-nara", zombieName, 42.0)
	ln.Network.local.SyncLedger.AddEvent(event)

	if !ln.Network.local.SyncLedger.HasEvent(event.ID) {
		t.Fatal("Event should exist before pruning")
	}

	// Run pruning
	ln.Network.pruneInactiveNaras()

	// Verify nara is gone from Neighbourhood
	if ln.Network.Neighbourhood[zombieName] != nil {
		t.Error("ghost-nara should be removed from Neighbourhood")
	}

	// THE KEY TEST: Verify events are gone
	if ln.Network.local.SyncLedger.HasEvent(event.ID) {
		t.Error("Events for ghost-nara should be removed from SyncLedger")
	}

	// Verify re-discovery doesn't happen when processing the ledger
	allEvents := ln.Network.local.SyncLedger.GetAllEvents()
	ln.Network.discoverNarasFromEvents(allEvents)

	if ln.Network.Neighbourhood[zombieName] != nil {
		t.Error("ghost-nara should NOT be re-discovered after pruning events")
	}
}

func TestPruneInactiveNaras_PrunesCheckpointEventsAboutSubject(t *testing.T) {
	// This test verifies that when a ghost nara is pruned, checkpoint events
	// WHERE THEY ARE THE SUBJECT are also removed from the ledger.
	// However, checkpoints where they were only a voter/emitter should be kept.
	ln := testLocalNara(t, "test-nara")
	now := time.Now().Unix()

	// Create a ghost nara that will be pruned
	ghostName := "ghost-nara"
	ghostNara := NewNara(ghostName)
	ln.Network.importNara(ghostNara)
	ln.setObservation(ghostName, NaraObservation{
		LastSeen:  now - (9 * 86400),  // 9 days offline
		StartTime: now - (10 * 86400), // Known for 10 days (established)
		Online:    "OFFLINE",
	})

	// Create another nara that will NOT be pruned
	activeName := "active-nara"
	activeNara := NewNara(activeName)
	ln.Network.importNara(activeNara)
	ln.setObservation(activeName, NaraObservation{
		LastSeen:  now - 60,
		StartTime: now - (10 * 86400),
		Online:    "ONLINE",
	})

	// Add checkpoint ABOUT the ghost (should be pruned)
	obs1 := NaraObservation{
		Restarts:    5,
		TotalUptime: 3600,
		StartTime:   now - (10 * 86400),
	}
	event1 := testAddCheckpointToLedger(ln.SyncLedger, ghostName, ln.Me.Name, ln.Keypair, obs1)

	// Add checkpoint ABOUT the active nara (should NOT be pruned)
	// Note: Ghost was a voter, but that doesn't matter - only subject matters for pruning
	obs2 := NaraObservation{
		Restarts:    3,
		TotalUptime: 7200,
		StartTime:   now - (10 * 86400),
	}
	event2 := testAddCheckpointToLedger(ln.SyncLedger, activeName, ln.Me.Name, ln.Keypair, obs2)

	// Verify both checkpoints exist before pruning
	if !ln.SyncLedger.HasEvent(event1.ID) {
		t.Fatal("Checkpoint about ghost should exist before pruning")
	}
	if !ln.SyncLedger.HasEvent(event2.ID) {
		t.Fatal("Checkpoint about active should exist before pruning")
	}

	// Run pruning
	ln.Network.pruneInactiveNaras()

	// Verify ghost nara is removed
	if ln.Network.Neighbourhood[ghostName] != nil {
		t.Error("ghost-nara should be removed from Neighbourhood")
	}

	// THE KEY ASSERTION: Checkpoint ABOUT the ghost should be removed
	if ln.SyncLedger.HasEvent(event1.ID) {
		t.Error("Checkpoint ABOUT ghost-nara (where they're the subject) should be removed")
	}

	// Checkpoint about the active nara should be KEPT (even though ghost was a voter)
	if !ln.SyncLedger.HasEvent(event2.ID) {
		t.Error("Checkpoint about active-nara should be kept (ghost was only a voter, not subject)")
	}

	t.Log("✅ Checkpoint pruning works correctly:")
	t.Log("   • Checkpoints ABOUT ghost naras are removed")
	t.Log("   • Checkpoints where ghost was only voter/emitter are kept")
}
