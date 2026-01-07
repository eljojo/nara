package nara

import (
	"encoding/json"
	"testing"
	"time"
)

// --- Basic Ledger Operations ---

func TestSyncLedger_AddAndDedup(t *testing.T) {
	ledger := NewSyncLedger(1000)

	// Add a social event
	event1 := NewSocialSyncEvent("tease", "alice", "bob", "high-restarts", "")
	if !ledger.AddEvent(event1) {
		t.Error("expected first event to be added")
	}

	// Adding same event again should be deduplicated
	if ledger.AddEvent(event1) {
		t.Error("expected duplicate event to be rejected")
	}

	// Count should be 1
	if ledger.EventCount() != 1 {
		t.Errorf("expected 1 event, got %d", ledger.EventCount())
	}

	// HasEvent should return true
	if !ledger.HasEvent(event1.ID) {
		t.Error("expected HasEvent to return true for added event")
	}
}

func TestSyncLedger_AddPingObservation(t *testing.T) {
	ledger := NewSyncLedger(1000)

	// Add a ping observation
	if !ledger.AddPingObservation("alice", "bob", 42.5) {
		t.Error("expected ping observation to be added")
	}

	// Check it's there
	pings := ledger.GetPingObservations()
	if len(pings) != 1 {
		t.Fatalf("expected 1 ping, got %d", len(pings))
	}

	if pings[0].Observer != "alice" || pings[0].Target != "bob" || pings[0].RTT != 42.5 {
		t.Errorf("ping data mismatch: %+v", pings[0])
	}
}

func TestSyncLedger_MergeEvents(t *testing.T) {
	ledger1 := NewSyncLedger(1000)
	ledger2 := NewSyncLedger(1000)

	// Add events to ledger1
	ledger1.AddPingObservation("alice", "bob", 10.0)
	ledger1.AddPingObservation("alice", "charlie", 20.0)
	ledger1.AddSocialEvent(SocialEvent{
		Timestamp: time.Now().Unix(),
		Type:      "tease",
		Actor:     "alice",
		Target:    "bob",
		Reason:    "high-restarts",
	})

	// Merge into ledger2
	events := ledger1.GetEventsForSync(nil, nil, 0, 0, 1, 0)
	added := ledger2.MergeEvents(events)

	if added != 3 {
		t.Errorf("expected 3 events merged, got %d", added)
	}

	if ledger2.EventCount() != 3 {
		t.Errorf("expected 3 events in ledger2, got %d", ledger2.EventCount())
	}

	// Merging again should add nothing (dedup)
	added = ledger2.MergeEvents(events)
	if added != 0 {
		t.Errorf("expected 0 events merged (dedup), got %d", added)
	}
}

func TestSyncLedger_InvalidEvents(t *testing.T) {
	ledger := NewSyncLedger(1000)

	// Event with no service
	invalid1 := SyncEvent{Timestamp: time.Now().Unix()}
	if ledger.AddEvent(invalid1) {
		t.Error("expected event with no service to be rejected")
	}

	// Ping with no observer
	invalid2 := SyncEvent{
		Timestamp: time.Now().Unix(),
		Service:   ServicePing,
		Ping:      &PingObservation{Target: "bob", RTT: 10.0},
	}
	if ledger.AddEvent(invalid2) {
		t.Error("expected ping with no observer to be rejected")
	}

	// Ping with zero RTT
	invalid3 := SyncEvent{
		Timestamp: time.Now().Unix(),
		Service:   ServicePing,
		Ping:      &PingObservation{Observer: "alice", Target: "bob", RTT: 0},
	}
	if ledger.AddEvent(invalid3) {
		t.Error("expected ping with zero RTT to be rejected")
	}

	// Social with invalid type
	invalid4 := SyncEvent{
		Timestamp: time.Now().Unix(),
		Service:   ServiceSocial,
		Social:    &SocialEventPayload{Type: "invalid", Actor: "alice", Target: "bob"},
	}
	if ledger.AddEvent(invalid4) {
		t.Error("expected social with invalid type to be rejected")
	}
}

// --- Filtering and Slicing ---

func TestSyncLedger_GetEventsByService(t *testing.T) {
	ledger := NewSyncLedger(1000)

	// Add mixed events
	ledger.AddPingObservation("alice", "bob", 10.0)
	ledger.AddPingObservation("alice", "charlie", 20.0)
	ledger.AddSocialEvent(SocialEvent{
		Timestamp: time.Now().Unix(),
		Type:      "tease",
		Actor:     "alice",
		Target:    "bob",
		Reason:    "high-restarts",
	})

	// Get only pings
	pings := ledger.GetEventsByService(ServicePing)
	if len(pings) != 2 {
		t.Errorf("expected 2 ping events, got %d", len(pings))
	}

	// Get only social
	social := ledger.GetEventsByService(ServiceSocial)
	if len(social) != 1 {
		t.Errorf("expected 1 social event, got %d", len(social))
	}
}

func TestSyncLedger_GetEventsInvolving(t *testing.T) {
	ledger := NewSyncLedger(1000)

	ledger.AddPingObservation("alice", "bob", 10.0)
	ledger.AddPingObservation("charlie", "bob", 20.0)
	ledger.AddPingObservation("alice", "dave", 30.0)

	// Events involving bob (as observer or target)
	bobEvents := ledger.GetEventsInvolving("bob")
	if len(bobEvents) != 2 {
		t.Errorf("expected 2 events involving bob, got %d", len(bobEvents))
	}

	// Events involving alice
	aliceEvents := ledger.GetEventsInvolving("alice")
	if len(aliceEvents) != 2 {
		t.Errorf("expected 2 events involving alice, got %d", len(aliceEvents))
	}
}

func TestSyncLedger_InterleavedSlicing(t *testing.T) {
	ledger := NewSyncLedger(1000)

	// Add 9 events with different timestamps
	baseTime := time.Now().Unix()
	for i := 0; i < 9; i++ {
		e := SyncEvent{
			Timestamp: baseTime + int64(i),
			Service:   ServicePing,
			Ping:      &PingObservation{Observer: "alice", Target: "bob", RTT: float64(i + 1)},
		}
		e.ComputeID()
		ledger.AddEvent(e)
	}

	// Get slice 0 of 3 (events 0, 3, 6)
	slice0 := ledger.GetEventsForSync(nil, nil, 0, 0, 3, 0)
	if len(slice0) != 3 {
		t.Errorf("expected 3 events in slice 0, got %d", len(slice0))
	}

	// Get slice 1 of 3 (events 1, 4, 7)
	slice1 := ledger.GetEventsForSync(nil, nil, 0, 1, 3, 0)
	if len(slice1) != 3 {
		t.Errorf("expected 3 events in slice 1, got %d", len(slice1))
	}

	// Get slice 2 of 3 (events 2, 5, 8)
	slice2 := ledger.GetEventsForSync(nil, nil, 0, 2, 3, 0)
	if len(slice2) != 3 {
		t.Errorf("expected 3 events in slice 2, got %d", len(slice2))
	}

	// Verify no overlap - all 9 events covered
	allIDs := make(map[string]bool)
	for _, e := range slice0 {
		allIDs[e.ID] = true
	}
	for _, e := range slice1 {
		if allIDs[e.ID] {
			t.Error("slice 1 overlaps with slice 0")
		}
		allIDs[e.ID] = true
	}
	for _, e := range slice2 {
		if allIDs[e.ID] {
			t.Error("slice 2 overlaps with earlier slices")
		}
		allIDs[e.ID] = true
	}

	if len(allIDs) != 9 {
		t.Errorf("expected 9 unique events across slices, got %d", len(allIDs))
	}
}

func TestSyncLedger_SinceTimeFilter(t *testing.T) {
	ledger := NewSyncLedger(1000)

	baseTime := time.Now().Unix()

	// Add events at different times: baseTime+0, +60, +120, +180, +240
	for i := 0; i < 5; i++ {
		e := SyncEvent{
			Timestamp: baseTime + int64(i*60), // 1 minute apart
			Service:   ServicePing,
			Ping:      &PingObservation{Observer: "alice", Target: "bob", RTT: float64(i + 1)},
		}
		e.ComputeID()
		ledger.AddEvent(e)
	}

	// Get events since baseTime+60 (filter is > not >=, so excludes 0 and 60)
	// Events at 120, 180, 240 should pass = 3 events
	recent := ledger.GetEventsForSync(nil, nil, baseTime+60, 0, 1, 0)
	if len(recent) != 3 {
		t.Errorf("expected 3 events after sinceTime, got %d", len(recent))
	}
}

func TestSyncLedger_MaxEventsLimit(t *testing.T) {
	ledger := NewSyncLedger(1000)

	// Add 10 events
	baseTime := time.Now().Unix()
	for i := 0; i < 10; i++ {
		e := SyncEvent{
			Timestamp: baseTime + int64(i),
			Service:   ServicePing,
			Ping:      &PingObservation{Observer: "alice", Target: "bob", RTT: float64(i + 1)},
		}
		e.ComputeID()
		ledger.AddEvent(e)
	}

	// Request max 3 events
	limited := ledger.GetEventsForSync(nil, nil, 0, 0, 1, 3)
	if len(limited) != 3 {
		t.Errorf("expected 3 events with maxEvents=3, got %d", len(limited))
	}

	// Should be the most recent 3 (timestamps 7, 8, 9)
	for _, e := range limited {
		if e.Timestamp < baseTime+7 {
			t.Errorf("expected only most recent events, got timestamp %d", e.Timestamp)
		}
	}
}

// --- Ping Diversity ---

func TestSyncLedger_PingDiversity_KeepsRecentHistory(t *testing.T) {
	ledger := NewSyncLedger(1000)

	// Add several pings A→B - should keep up to MaxPingsPerPair
	for i := 0; i < MaxPingsPerPair; i++ {
		if !ledger.AddPingObservationWithReplace("alice", "bob", float64(10+i)) {
			t.Errorf("expected ping %d to be added", i)
		}
	}

	// Should have exactly MaxPingsPerPair pings
	pings := ledger.GetPingsBetween("alice", "bob")
	if len(pings) != MaxPingsPerPair {
		t.Errorf("expected %d pings between alice→bob, got %d", MaxPingsPerPair, len(pings))
	}

	// Add one more - should evict the oldest
	ledger.AddPingObservationWithReplace("alice", "bob", 99.0)

	pings = ledger.GetPingsBetween("alice", "bob")
	if len(pings) != MaxPingsPerPair {
		t.Errorf("expected still %d pings after overflow, got %d", MaxPingsPerPair, len(pings))
	}

	// The oldest (RTT=10.0) should have been evicted
	for _, p := range pings {
		if p.RTT == 10.0 {
			t.Error("oldest ping (RTT=10.0) should have been evicted")
		}
	}

	// The newest (RTT=99.0) should be present
	found := false
	for _, p := range pings {
		if p.RTT == 99.0 {
			found = true
			break
		}
	}
	if !found {
		t.Error("newest ping (RTT=99.0) should be present")
	}

	// Add ping B→A (different direction) - should be separate
	ledger.AddPingObservationWithReplace("bob", "alice", 15.0)

	// Should have MaxPingsPerPair + 1 total pings now
	allPings := ledger.GetPingsBetween("alice", "bob")
	if len(allPings) != MaxPingsPerPair+1 {
		t.Errorf("expected %d pings (both directions), got %d", MaxPingsPerPair+1, len(allPings))
	}
}

// --- Signed Responses ---

func TestSyncResponse_Signing(t *testing.T) {
	// Create a keypair from a valid soul (use GenerateSoul or create bytes directly)
	soul1 := SoulV1{}
	for i := 0; i < 32; i++ {
		soul1.Seed[i] = byte(i) // deterministic seed
	}
	keypair := DeriveKeypair(soul1)

	// Create some events
	ledger := NewSyncLedger(1000)
	ledger.AddPingObservation("alice", "bob", 42.5)
	ledger.AddSocialEvent(SocialEvent{
		Timestamp: time.Now().Unix(),
		Type:      "tease",
		Actor:     "alice",
		Target:    "bob",
		Reason:    "high-restarts",
	})

	events := ledger.GetEventsForSync(nil, nil, 0, 0, 1, 0)

	// Create and sign response
	response := NewSignedSyncResponse("test-nara", events, keypair)

	// Verify signature should pass with correct public key
	if !response.VerifySignature(keypair.PublicKey) {
		t.Error("expected signature verification to pass")
	}

	// Verify should fail with wrong public key
	soul2 := SoulV1{}
	for i := 0; i < 32; i++ {
		soul2.Seed[i] = byte(255 - i) // different seed
	}
	otherKeypair := DeriveKeypair(soul2)
	if response.VerifySignature(otherKeypair.PublicKey) {
		t.Error("expected signature verification to fail with wrong key")
	}

	// Tamper with response - verification should fail
	response.Events = append(response.Events, NewPingSyncEvent("attacker", "victim", 1.0))
	if response.VerifySignature(keypair.PublicKey) {
		t.Error("expected signature verification to fail after tampering")
	}
}

// --- Boot Recovery Simulation ---

func TestBootRecovery_DistributedSync(t *testing.T) {
	// Simulate 3 neighbors, each with different events
	neighbor1 := NewSyncLedger(1000)
	neighbor2 := NewSyncLedger(1000)
	neighbor3 := NewSyncLedger(1000)

	// Each neighbor has some unique events and some shared
	baseTime := time.Now().Unix()

	// Shared events (all neighbors have these)
	for i := 0; i < 10; i++ {
		e := SyncEvent{
			Timestamp: baseTime + int64(i),
			Service:   ServicePing,
			Ping:      &PingObservation{Observer: "shared", Target: "target", RTT: float64(i)},
		}
		e.ComputeID()
		neighbor1.AddEvent(e)
		neighbor2.AddEvent(e)
		neighbor3.AddEvent(e)
	}

	// Unique events per neighbor
	for i := 0; i < 5; i++ {
		e1 := NewPingSyncEvent("neighbor1", "unique", float64(i))
		e2 := NewPingSyncEvent("neighbor2", "unique", float64(i))
		e3 := NewPingSyncEvent("neighbor3", "unique", float64(i))
		neighbor1.AddEvent(e1)
		neighbor2.AddEvent(e2)
		neighbor3.AddEvent(e3)
	}

	// Boot recovery: query each with interleaved slicing
	bootingNara := NewSyncLedger(1000)
	totalNeighbors := 3

	slice0 := neighbor1.GetEventsForSync(nil, nil, 0, 0, totalNeighbors, 0)
	slice1 := neighbor2.GetEventsForSync(nil, nil, 0, 1, totalNeighbors, 0)
	slice2 := neighbor3.GetEventsForSync(nil, nil, 0, 2, totalNeighbors, 0)

	bootingNara.MergeEvents(slice0)
	bootingNara.MergeEvents(slice1)
	bootingNara.MergeEvents(slice2)

	// Should have collected events from all neighbors
	// 10 shared + 5 unique each = 10 + 15 = 25 total (but slicing means we get subset)
	// With interleaved slicing, we get different portions from each
	if bootingNara.EventCount() < 10 {
		t.Errorf("expected at least 10 events from boot recovery, got %d", bootingNara.EventCount())
	}
}

// --- Pruning ---

func TestSyncLedger_Prune(t *testing.T) {
	ledger := NewSyncLedger(5) // Max 5 events

	// Add 10 events
	baseTime := time.Now().Unix()
	for i := 0; i < 10; i++ {
		e := SyncEvent{
			Timestamp: baseTime + int64(i),
			Service:   ServicePing,
			Ping:      &PingObservation{Observer: "alice", Target: "bob", RTT: float64(i + 1)},
		}
		e.ComputeID()
		ledger.AddEvent(e)
	}

	if ledger.EventCount() != 10 {
		t.Errorf("expected 10 events before prune, got %d", ledger.EventCount())
	}

	// Prune
	ledger.Prune()

	if ledger.EventCount() != 5 {
		t.Errorf("expected 5 events after prune, got %d", ledger.EventCount())
	}

	// Oldest events should be removed - check that remaining are recent
	events := ledger.GetEventsForSync(nil, nil, 0, 0, 1, 0)
	for _, e := range events {
		if e.Timestamp < baseTime+5 {
			t.Errorf("expected only recent events after prune, got timestamp %d", e.Timestamp)
		}
	}
}

// --- JSON Serialization ---

func TestSyncEvent_JSONRoundtrip(t *testing.T) {
	// Test social event
	social := NewSocialSyncEvent("tease", "alice", "bob", "high-restarts", "charlie")

	jsonBytes, err := json.Marshal(social)
	if err != nil {
		t.Fatalf("failed to marshal social event: %v", err)
	}

	var decoded SyncEvent
	if err := json.Unmarshal(jsonBytes, &decoded); err != nil {
		t.Fatalf("failed to unmarshal social event: %v", err)
	}

	if decoded.ID != social.ID || decoded.Service != social.Service {
		t.Error("social event roundtrip mismatch")
	}
	if decoded.Social.Actor != "alice" || decoded.Social.Target != "bob" {
		t.Error("social event payload mismatch")
	}

	// Test ping event
	ping := NewPingSyncEvent("alice", "bob", 42.5)

	jsonBytes, err = json.Marshal(ping)
	if err != nil {
		t.Fatalf("failed to marshal ping event: %v", err)
	}

	if err := json.Unmarshal(jsonBytes, &decoded); err != nil {
		t.Fatalf("failed to unmarshal ping event: %v", err)
	}

	if decoded.Service != ServicePing {
		t.Error("ping event service mismatch")
	}
	if decoded.Ping.RTT != 42.5 {
		t.Errorf("ping RTT mismatch: got %.1f", decoded.Ping.RTT)
	}
}

// --- Legacy Conversion ---

func TestSyncEvent_LegacyConversion(t *testing.T) {
	// Create legacy social event
	legacy := SocialEvent{
		Timestamp: time.Now().Unix(),
		Type:      "tease",
		Actor:     "alice",
		Target:    "bob",
		Reason:    "high-restarts",
		Witness:   "charlie",
	}
	legacy.ComputeID()

	// Convert to SyncEvent
	syncEvent := SyncEventFromSocialEvent(legacy)

	if syncEvent.Service != ServiceSocial {
		t.Error("expected social service")
	}
	if syncEvent.Social.Actor != "alice" {
		t.Error("actor mismatch")
	}

	// Convert back
	backToLegacy := syncEvent.ToSocialEvent()
	if backToLegacy == nil {
		t.Fatal("ToSocialEvent returned nil")
	}
	if backToLegacy.Actor != legacy.Actor || backToLegacy.Target != legacy.Target {
		t.Error("roundtrip conversion mismatch")
	}

	// Ping event should return nil for ToSocialEvent
	pingEvent := NewPingSyncEvent("alice", "bob", 10.0)
	if pingEvent.ToSocialEvent() != nil {
		t.Error("expected nil for ping event ToSocialEvent")
	}
}
