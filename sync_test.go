package nara

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
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

	// Add 10 events - pruning happens automatically during AddEvent when over max
	baseTime := time.Now().UnixNano()
	for i := 0; i < 10; i++ {
		e := SyncEvent{
			Timestamp: baseTime + int64(i*1000), // Different nanoseconds
			Service:   ServicePing,
			Ping:      &PingObservation{Observer: "alice", Target: "bob", RTT: float64(i + 1)},
		}
		e.ComputeID()
		ledger.AddEvent(e)
	}

	// With automatic pruning (drops 10% when over max), we should be around max
	count := ledger.EventCount()
	if count > 6 { // Allow some slack
		t.Errorf("expected automatic pruning to keep events near max 5, got %d", count)
	}
	if count < 4 {
		t.Errorf("expected at least 4 events after pruning, got %d", count)
	}

	// Explicit prune (for time-based pruning) - verify it doesn't crash
	ledger.Prune()

	// Recent events should still be present
	events := ledger.GetEventsForSync(nil, nil, 0, 0, 1, 0)
	if len(events) == 0 {
		t.Error("expected some events after prune")
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

func TestSyncEvent_SignAndVerify(t *testing.T) {
	// Create a keypair
	pub, priv, _ := ed25519.GenerateKey(nil)
	keypair := NaraKeypair{PrivateKey: priv, PublicKey: pub}

	// Test signing a social event
	event := NewSocialSyncEvent("tease", "alice", "bob", "high-restarts", "")
	if event.IsSigned() {
		t.Error("event should not be signed initially")
	}

	event.Sign("alice", keypair)

	if !event.IsSigned() {
		t.Error("event should be signed after Sign()")
	}
	if event.Emitter != "alice" {
		t.Errorf("expected emitter alice, got %s", event.Emitter)
	}
	if event.Signature == "" {
		t.Error("expected signature to be set")
	}

	// Verify with correct key
	if !event.Verify(pub) {
		t.Error("expected signature to verify with correct key")
	}

	// Verify with wrong key
	pub2, _, _ := ed25519.GenerateKey(nil)
	if event.Verify(pub2) {
		t.Error("expected signature to fail with wrong key")
	}
}

func TestSyncEvent_SignedConstructors(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	keypair := NaraKeypair{PrivateKey: priv, PublicKey: pub}

	// Test signed social event constructor
	social := NewSignedSocialSyncEvent("tease", "alice", "bob", "reason", "", "alice", keypair)
	if !social.IsSigned() {
		t.Error("NewSignedSocialSyncEvent should create signed event")
	}
	if social.Emitter != "alice" {
		t.Errorf("expected emitter alice, got %s", social.Emitter)
	}
	if !social.Verify(pub) {
		t.Error("signed social event should verify")
	}

	// Test signed ping event constructor
	ping := NewSignedPingSyncEvent("alice", "bob", 42.5, "alice", keypair)
	if !ping.IsSigned() {
		t.Error("NewSignedPingSyncEvent should create signed event")
	}
	if ping.Emitter != "alice" {
		t.Errorf("expected emitter alice, got %s", ping.Emitter)
	}
	if !ping.Verify(pub) {
		t.Error("signed ping event should verify")
	}
}

func TestSyncEvent_VerifyUnsigned(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)

	// Unsigned event should return false for Verify (not panic)
	event := NewSocialSyncEvent("tease", "alice", "bob", "reason", "")
	if event.IsSigned() {
		t.Error("unsigned event should report IsSigned() = false")
	}
	if event.Verify(pub) {
		t.Error("unsigned event should return false for Verify")
	}
}

func TestSyncEvent_TamperedSignature(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	keypair := NaraKeypair{PrivateKey: priv, PublicKey: pub}

	// Create and sign an event
	event := NewSignedSocialSyncEvent("tease", "alice", "bob", "reason", "", "alice", keypair)

	// Tamper with the event content
	event.Social.Reason = "tampered"

	// Signature should no longer verify
	if event.Verify(pub) {
		t.Error("tampered event should fail verification")
	}
}

func TestSyncLedger_AddSignedPingObservation(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	keypair := NaraKeypair{PrivateKey: priv, PublicKey: pub}

	ledger := NewSyncLedger(100)

	// Add signed ping observation
	if !ledger.AddSignedPingObservation("alice", "bob", 42.5, "alice", keypair) {
		t.Error("expected AddSignedPingObservation to succeed")
	}

	// Verify the event was added and is signed
	events := ledger.GetEventsByService(ServicePing)
	if len(events) != 1 {
		t.Fatalf("expected 1 ping event, got %d", len(events))
	}

	if !events[0].IsSigned() {
		t.Error("added event should be signed")
	}
	if !events[0].Verify(pub) {
		t.Error("added event should verify")
	}
}

func TestSyncLedger_AddSignedPingObservationWithReplace(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	keypair := NaraKeypair{PrivateKey: priv, PublicKey: pub}

	ledger := NewSyncLedger(100)

	// Add MaxPingsPerPair + 1 signed pings
	for i := 0; i < MaxPingsPerPair+1; i++ {
		if !ledger.AddSignedPingObservationWithReplace("alice", "bob", float64(10+i), "alice", keypair) {
			t.Errorf("expected AddSignedPingObservationWithReplace to succeed for ping %d", i)
		}
	}

	// Should still have only MaxPingsPerPair pings
	pings := ledger.GetPingObservations()
	if len(pings) != MaxPingsPerPair {
		t.Errorf("expected %d pings, got %d", MaxPingsPerPair, len(pings))
	}

	// All should be signed
	events := ledger.GetEventsByService(ServicePing)
	for _, e := range events {
		if !e.IsSigned() {
			t.Error("all ping events should be signed")
		}
		if !e.Verify(pub) {
			t.Error("all ping events should verify")
		}
	}

	// Latest ping should be the newest value (10 + MaxPingsPerPair)
	latest := ledger.GetLatestPingTo("bob")
	expectedRTT := float64(10 + MaxPingsPerPair)
	if latest.RTT != expectedRTT {
		t.Errorf("expected latest RTT %.1f, got %.1f", expectedRTT, latest.RTT)
	}
}

// --- Personality-Aware Methods (for unified ledger) ---

// TestSyncLedger_AddSocialEventFiltered tests personality-based filtering on add
func TestSyncLedger_AddSocialEventFiltered(t *testing.T) {
	tests := []struct {
		name        string
		personality NaraPersonality
		event       SyncEvent
		shouldAdd   bool
		reason      string
	}{
		{
			name:        "high_chill_ignores_random_jabs",
			personality: NaraPersonality{Chill: 75, Sociability: 50, Agreeableness: 50},
			event:       NewSocialSyncEvent("tease", "alice", "bob", ReasonRandom, ""),
			shouldAdd:   false,
			reason:      "high chill (>70) should ignore random jabs",
		},
		{
			name:        "low_chill_keeps_random_jabs",
			personality: NaraPersonality{Chill: 50, Sociability: 50, Agreeableness: 50},
			event:       NewSocialSyncEvent("tease", "alice", "bob", ReasonRandom, ""),
			shouldAdd:   true,
			reason:      "low chill should keep random jabs",
		},
		{
			name:        "very_high_chill_only_significant_events",
			personality: NaraPersonality{Chill: 90, Sociability: 50, Agreeableness: 50},
			event:       NewSocialSyncEvent("tease", "alice", "bob", ReasonTrendAbandon, ""),
			shouldAdd:   false,
			reason:      "very high chill (>85) only keeps comebacks and high-restarts",
		},
		{
			name:        "very_high_chill_keeps_comebacks",
			personality: NaraPersonality{Chill: 90, Sociability: 50, Agreeableness: 50},
			event:       NewSocialSyncEvent("tease", "alice", "bob", ReasonComeback, ""),
			shouldAdd:   true,
			reason:      "very high chill should keep comebacks",
		},
		{
			name:        "high_agreeableness_filters_trend_abandon",
			personality: NaraPersonality{Chill: 50, Sociability: 50, Agreeableness: 85},
			event:       NewSocialSyncEvent("tease", "alice", "bob", ReasonTrendAbandon, ""),
			shouldAdd:   false,
			reason:      "high agreeableness (>80) filters trend-abandon drama",
		},
		{
			name:        "low_sociability_ignores_random",
			personality: NaraPersonality{Chill: 50, Sociability: 15, Agreeableness: 50},
			event:       NewSocialSyncEvent("tease", "alice", "bob", ReasonRandom, ""),
			shouldAdd:   false,
			reason:      "low sociability (<20) ignores random teases",
		},
		{
			name:        "everyone_keeps_journey_timeout",
			personality: NaraPersonality{Chill: 90, Sociability: 10, Agreeableness: 90},
			event:       NewSocialSyncEvent("observation", "system", "bob", ReasonJourneyTimeout, ""),
			shouldAdd:   true,
			reason:      "everyone keeps journey-timeout (reliability matters)",
		},
		{
			name:        "very_high_chill_skips_routine_online_offline",
			personality: NaraPersonality{Chill: 90, Sociability: 50, Agreeableness: 50},
			event:       NewSocialSyncEvent("observation", "system", "bob", ReasonOnline, ""),
			shouldAdd:   false,
			reason:      "very high chill (>85) skips routine online/offline",
		},
		{
			name:        "low_sociability_skips_journey_pass",
			personality: NaraPersonality{Chill: 50, Sociability: 25, Agreeableness: 50},
			event:       NewSocialSyncEvent("observation", "system", "bob", ReasonJourneyPass, ""),
			shouldAdd:   false,
			reason:      "low sociability (<30) skips journey-pass",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ledger := NewSyncLedger(1000)
			added := ledger.AddSocialEventFiltered(tt.event, tt.personality)
			if added != tt.shouldAdd {
				t.Errorf("%s: expected added=%v, got %v", tt.reason, tt.shouldAdd, added)
			}
		})
	}
}

// TestSyncLedger_GetTeaseCounts tests objective tease counting (no personality influence)
func TestSyncLedger_GetTeaseCounts(t *testing.T) {
	ledger := NewSyncLedger(1000)

	// Add various teases from different actors
	ledger.AddEvent(NewSocialSyncEvent("tease", "alice", "bob", ReasonRandom, ""))
	ledger.AddEvent(NewSocialSyncEvent("tease", "alice", "charlie", ReasonComeback, ""))
	ledger.AddEvent(NewSocialSyncEvent("tease", "alice", "dave", ReasonHighRestarts, ""))
	ledger.AddEvent(NewSocialSyncEvent("tease", "bob", "alice", ReasonRandom, ""))
	ledger.AddEvent(NewSocialSyncEvent("tease", "bob", "charlie", ReasonNiceNumber, ""))
	ledger.AddEvent(NewSocialSyncEvent("tease", "charlie", "alice", ReasonRandom, ""))

	// Add non-tease events (should not be counted)
	ledger.AddEvent(NewSocialSyncEvent("observation", "system", "alice", ReasonOnline, ""))
	ledger.AddEvent(NewSocialSyncEvent("gossip", "dave", "bob", ReasonRandom, ""))
	ledger.AddPingObservation("alice", "bob", 42.5)

	counts := ledger.GetTeaseCounts()

	if counts["alice"] != 3 {
		t.Errorf("expected alice to have 3 teases, got %d", counts["alice"])
	}
	if counts["bob"] != 2 {
		t.Errorf("expected bob to have 2 teases, got %d", counts["bob"])
	}
	if counts["charlie"] != 1 {
		t.Errorf("expected charlie to have 1 tease, got %d", counts["charlie"])
	}
	if counts["dave"] != 0 {
		t.Errorf("expected dave to have 0 teases (gossip doesn't count), got %d", counts["dave"])
	}
	if counts["system"] != 0 {
		t.Errorf("expected system to have 0 teases (observation doesn't count), got %d", counts["system"])
	}
}

// TestSyncLedger_DeriveClout tests subjective clout calculation
func TestSyncLedger_DeriveClout(t *testing.T) {
	ledger := NewSyncLedger(1000)

	// Add events with recent timestamps
	now := time.Now().Unix()

	// Add a tease from alice to bob
	teaseEvent := SyncEvent{
		Timestamp: now,
		Service:   ServiceSocial,
		Social: &SocialEventPayload{
			Type:   "tease",
			Actor:  "alice",
			Target: "bob",
			Reason: ReasonHighRestarts,
		},
	}
	teaseEvent.ComputeID()
	ledger.AddEvent(teaseEvent)

	// Different personalities should derive different clout
	socialPersonality := NaraPersonality{Chill: 30, Sociability: 80, Agreeableness: 50}
	chillPersonality := NaraPersonality{Chill: 80, Sociability: 30, Agreeableness: 50}

	// Use deterministic souls for reproducibility
	soul1 := "soul-observer-1"
	soul2 := "soul-observer-2"

	// Get clout from both perspectives
	clout1 := ledger.DeriveClout(soul1, socialPersonality)
	clout2 := ledger.DeriveClout(soul2, chillPersonality)

	// Alice should have some clout (positive or negative depending on resonance)
	// The exact values depend on TeaseResonates, but we can verify structure
	if _, exists := clout1["alice"]; !exists {
		// Tease actor should appear in clout map
		t.Error("expected alice to have clout entry from social observer")
	}

	// The clout values should potentially differ due to different personalities
	// (TeaseResonates uses personality to determine if tease lands)
	t.Logf("Social observer clout for alice: %.2f", clout1["alice"])
	t.Logf("Chill observer clout for alice: %.2f", clout2["alice"])
}

// TestSyncLedger_DeriveClout_Observations tests clout from observation events
func TestSyncLedger_DeriveClout_Observations(t *testing.T) {
	ledger := NewSyncLedger(1000)
	now := time.Now().Unix()

	// Add observation events
	events := []struct {
		target string
		reason string
	}{
		{"reliable-nara", ReasonJourneyComplete},  // positive
		{"reliable-nara", ReasonJourneyPass},      // positive
		{"unreliable-nara", ReasonJourneyTimeout}, // negative
		{"random-nara", ReasonOnline},             // slightly positive
		{"random-nara", ReasonOffline},            // slightly negative
	}

	for _, e := range events {
		event := SyncEvent{
			Timestamp: now,
			Service:   ServiceSocial,
			Social: &SocialEventPayload{
				Type:   "observation",
				Actor:  "observer",
				Target: e.target,
				Reason: e.reason,
			},
		}
		event.ComputeID()
		ledger.AddEvent(event)
	}

	personality := NaraPersonality{Chill: 50, Sociability: 50, Agreeableness: 50}
	clout := ledger.DeriveClout("observer-soul", personality)

	// reliable-nara should have positive clout (journey-complete + journey-pass)
	if clout["reliable-nara"] <= 0 {
		t.Errorf("expected reliable-nara to have positive clout, got %.2f", clout["reliable-nara"])
	}

	// unreliable-nara should have negative clout (journey-timeout)
	if clout["unreliable-nara"] >= 0 {
		t.Errorf("expected unreliable-nara to have negative clout, got %.2f", clout["unreliable-nara"])
	}
}

// TestSyncLedger_EventWeight_TimeDecay tests that old events have less weight
func TestSyncLedger_EventWeight_TimeDecay(t *testing.T) {
	ledger := NewSyncLedger(1000)
	personality := NaraPersonality{Chill: 50, Sociability: 50, Agreeableness: 50}

	now := time.Now().Unix()

	// Recent event (just now)
	recentEvent := SyncEvent{
		Timestamp: now,
		Service:   ServiceSocial,
		Social: &SocialEventPayload{
			Type:   "tease",
			Actor:  "alice",
			Target: "bob",
			Reason: ReasonHighRestarts,
		},
	}
	recentEvent.ComputeID()

	// Old event (3 days ago)
	oldEvent := SyncEvent{
		Timestamp: now - 3*24*60*60,
		Service:   ServiceSocial,
		Social: &SocialEventPayload{
			Type:   "tease",
			Actor:  "charlie",
			Target: "dave",
			Reason: ReasonHighRestarts,
		},
	}
	oldEvent.ComputeID()

	recentWeight := ledger.EventWeight(recentEvent, personality)
	oldWeight := ledger.EventWeight(oldEvent, personality)

	// Recent events should have more weight
	if recentWeight <= oldWeight {
		t.Errorf("expected recent event weight (%.2f) > old event weight (%.2f)", recentWeight, oldWeight)
	}
}

// TestSyncLedger_EventWeight_PersonalityModifiers tests personality affects weight
func TestSyncLedger_EventWeight_PersonalityModifiers(t *testing.T) {
	ledger := NewSyncLedger(1000)
	now := time.Now().Unix()

	event := SyncEvent{
		Timestamp: now,
		Service:   ServiceSocial,
		Social: &SocialEventPayload{
			Type:   "tease",
			Actor:  "alice",
			Target: "bob",
			Reason: ReasonComeback,
		},
	}
	event.ComputeID()

	// Social personality (high sociability)
	socialPersonality := NaraPersonality{Chill: 30, Sociability: 90, Agreeableness: 50}
	socialWeight := ledger.EventWeight(event, socialPersonality)

	// Chill personality (high chill, low sociability)
	chillPersonality := NaraPersonality{Chill: 90, Sociability: 30, Agreeableness: 50}
	chillWeight := ledger.EventWeight(event, chillPersonality)

	// Social naras should weight comeback events higher
	if socialWeight <= chillWeight {
		t.Errorf("expected social personality to weight comebacks higher (%.2f vs %.2f)", socialWeight, chillWeight)
	}
}

// TestSyncLedger_EventWeight_ReasonModifiers tests reason affects weight
func TestSyncLedger_EventWeight_ReasonModifiers(t *testing.T) {
	ledger := NewSyncLedger(1000)
	now := time.Now().Unix()
	personality := NaraPersonality{Chill: 90, Sociability: 50, Agreeableness: 50}

	// Random event
	randomEvent := SyncEvent{
		Timestamp: now,
		Service:   ServiceSocial,
		Social: &SocialEventPayload{
			Type:   "tease",
			Actor:  "alice",
			Target: "bob",
			Reason: ReasonRandom,
		},
	}
	randomEvent.ComputeID()

	// High restarts event
	highRestartsEvent := SyncEvent{
		Timestamp: now,
		Service:   ServiceSocial,
		Social: &SocialEventPayload{
			Type:   "tease",
			Actor:  "alice",
			Target: "bob",
			Reason: ReasonHighRestarts,
		},
	}
	highRestartsEvent.ComputeID()

	randomWeight := ledger.EventWeight(randomEvent, personality)
	highRestartsWeight := ledger.EventWeight(highRestartsEvent, personality)

	// High chill should diminish random events more than high-restarts
	if randomWeight >= highRestartsWeight {
		t.Errorf("expected random event to have less weight for chill personality (%.2f vs %.2f)", randomWeight, highRestartsWeight)
	}
}

// TestSyncLedger_GetRecentSocialEvents tests getting recent social events
func TestSyncLedger_GetRecentSocialEvents(t *testing.T) {
	ledger := NewSyncLedger(1000)

	// Add events with different timestamps
	baseTime := time.Now().UnixNano()
	for i := 0; i < 10; i++ {
		e := SyncEvent{
			Timestamp: baseTime + int64(i*1000),
			Service:   ServiceSocial,
			Social: &SocialEventPayload{
				Type:   "tease",
				Actor:  "alice",
				Target: fmt.Sprintf("target-%d", i),
				Reason: ReasonRandom,
			},
		}
		e.ComputeID()
		ledger.AddEvent(e)
	}

	// Add a ping event (should not be included)
	ledger.AddPingObservation("alice", "bob", 42.5)

	// Get 5 most recent
	recent := ledger.GetRecentSocialEvents(5)
	if len(recent) != 5 {
		t.Errorf("expected 5 recent events, got %d", len(recent))
	}

	// Should be sorted by timestamp descending (most recent first)
	for i := 1; i < len(recent); i++ {
		if recent[i].Timestamp > recent[i-1].Timestamp {
			t.Error("expected events sorted by timestamp descending")
		}
	}

	// All should be social events
	for _, e := range recent {
		if e.Service != ServiceSocial {
			t.Errorf("expected only social events, got %s", e.Service)
		}
	}
}

// TestSyncLedger_GetSocialEventsAbout tests getting events about a specific nara
func TestSyncLedger_GetSocialEventsAbout(t *testing.T) {
	ledger := NewSyncLedger(1000)

	// Add events targeting different naras
	ledger.AddEvent(NewSocialSyncEvent("tease", "alice", "bob", ReasonRandom, ""))
	ledger.AddEvent(NewSocialSyncEvent("tease", "charlie", "bob", ReasonComeback, ""))
	ledger.AddEvent(NewSocialSyncEvent("tease", "alice", "dave", ReasonRandom, ""))
	ledger.AddEvent(NewSocialSyncEvent("observation", "system", "bob", ReasonOnline, ""))

	// Get events about bob
	bobEvents := ledger.GetSocialEventsAbout("bob")
	if len(bobEvents) != 3 {
		t.Errorf("expected 3 events about bob, got %d", len(bobEvents))
	}

	// Verify all are about bob
	for _, e := range bobEvents {
		if e.Social.Target != "bob" {
			t.Errorf("expected event to be about bob, got target %s", e.Social.Target)
		}
	}

	// Get events about dave
	daveEvents := ledger.GetSocialEventsAbout("dave")
	if len(daveEvents) != 1 {
		t.Errorf("expected 1 event about dave, got %d", len(daveEvents))
	}
}

// TestSyncLedger_ChillMemoryDecay tests that chill naras forget faster
func TestSyncLedger_ChillMemoryDecay(t *testing.T) {
	ledger := NewSyncLedger(1000)
	now := time.Now().Unix()

	// Event from 2 days ago
	oldEvent := SyncEvent{
		Timestamp: now - 2*24*60*60,
		Service:   ServiceSocial,
		Social: &SocialEventPayload{
			Type:   "tease",
			Actor:  "alice",
			Target: "bob",
			Reason: ReasonComeback,
		},
	}
	oldEvent.ComputeID()

	// Very chill personality (should forget faster)
	chillPersonality := NaraPersonality{Chill: 90, Sociability: 50, Agreeableness: 50}

	// Not chill personality (should remember longer)
	notChillPersonality := NaraPersonality{Chill: 10, Sociability: 50, Agreeableness: 50}

	chillWeight := ledger.EventWeight(oldEvent, chillPersonality)
	notChillWeight := ledger.EventWeight(oldEvent, notChillPersonality)

	// Not-chill nara should remember the event longer (higher weight)
	if notChillWeight <= chillWeight {
		t.Errorf("expected not-chill personality to have higher weight for old events (%.3f vs %.3f)", notChillWeight, chillWeight)
	}
}
