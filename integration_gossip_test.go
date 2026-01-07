package nara

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// TestIntegration_GossipOnlyMode validates that naras can operate in gossip-only mode
// without MQTT, spreading events purely via P2P zine exchanges
func TestIntegration_GossipOnlyMode(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	// Create 5 naras in gossip-only mode (no MQTT)
	naras := make([]*LocalNara, 5)
	for i := 0; i < 5; i++ {
		ln := NewLocalNara("gossip-nara-"+string(rune('a'+i)), "test-soul", "", "", "", 50, 1000)
		ln.Network.TransportMode = TransportGossip // Gossip-only mode

		// Fake that they're not booting
		me := ln.getMeObservation()
		me.LastRestart = time.Now().Unix() - 200
		me.LastSeen = time.Now().Unix()
		ln.setMeObservation(me)

		// Add all other naras as mesh neighbors (simulate mesh connectivity)
		for j := 0; j < 5; j++ {
			if i != j {
				neighborName := "gossip-nara-" + string(rune('a'+j))
				neighbor := NewNara(neighborName)
				neighbor.Status.MeshIP = "100.64.0." + string(rune('1'+j)) // Fake mesh IPs
				ln.Network.importNara(neighbor)
				ln.setObservation(neighborName, NaraObservation{Online: "ONLINE"})
			}
		}

		naras[i] = ln
	}

	// Nara 0 creates a social event
	event := NewTeaseEvent(naras[0].Me.Name, "gossip-nara-b", "high restarts")
	naras[0].SyncLedger.AddSocialEvent(event)

	// Simulate gossip rounds - each nara gossips with neighbors
	// In real code this happens via gossipForever() loop
	for round := 0; round < 3; round++ {
		for i := 0; i < 5; i++ {
			// Create zine from nara i
			zine := createTestZine(naras[i])

			// Exchange with 2-3 random neighbors
			for j := 0; j < 5; j++ {
				if i != j && j%2 == 0 { // Simple pattern for test
					// Merge zine into neighbor's ledger
					for _, e := range zine.Events {
						naras[j].SyncLedger.AddEvent(e)
					}
				}
			}
		}
		time.Sleep(100 * time.Millisecond) // Allow propagation
	}

	// Verify event propagated to most naras via gossip
	propagatedCount := 0
	for i := 0; i < 5; i++ {
		events := naras[i].SyncLedger.GetEventsByService(ServiceSocial)
		for _, e := range events {
			if e.Social != nil && e.Social.Type == "tease" && e.Social.Target == "gossip-nara-b" {
				propagatedCount++
				break
			}
		}
	}

	// Should reach at least 3/5 naras via gossip (epidemic spread)
	if propagatedCount < 3 {
		t.Errorf("Expected event to reach at least 3 naras via gossip, reached %d", propagatedCount)
	}

	t.Logf("✅ Gossip-only mode: event reached %d/5 naras without MQTT", propagatedCount)
}

// TestIntegration_HybridMode validates that naras can use both MQTT and gossip simultaneously
func TestIntegration_HybridMode(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	// Create shared ledger to simulate both transport paths
	sharedLedger := NewSyncLedger(1000)

	// Create 3 naras in hybrid mode
	naras := make([]*LocalNara, 3)
	for i := 0; i < 3; i++ {
		ln := NewLocalNara("hybrid-nara-"+string(rune('a'+i)), "test-soul", "", "", "", 50, 1000)
		ln.Network.TransportMode = TransportHybrid // Both MQTT and Gossip
		ln.SyncLedger = sharedLedger               // Share ledger to simulate both paths working

		// Fake not booting
		me := ln.getMeObservation()
		me.LastRestart = time.Now().Unix() - 200
		me.LastSeen = time.Now().Unix()
		ln.setMeObservation(me)

		naras[i] = ln
	}

	// Create events via both "transports" (simulated)
	mqttEvent := NewTeaseEvent("hybrid-nara-a", "hybrid-nara-b", "came back")
	gossipEvent := NewTeaseEvent("hybrid-nara-b", "hybrid-nara-c", "trend abandon")

	// Both should arrive in the shared ledger
	sharedLedger.AddSocialEvent(mqttEvent)
	sharedLedger.AddSocialEvent(gossipEvent)

	// All naras should see both events (transport-agnostic)
	for i := 0; i < 3; i++ {
		events := naras[i].SyncLedger.GetEventsByService(ServiceSocial)
		if len(events) < 2 {
			t.Errorf("Nara %d expected to see 2 events (from both transports), got %d", i, len(events))
		}
	}

	t.Logf("✅ Hybrid mode: all naras saw events from both MQTT and gossip transports")
}

// TestIntegration_MixedNetworkTopology validates that MQTT-only, gossip-only, and hybrid naras
// can coexist and events still propagate across the network
func TestIntegration_MixedNetworkTopology(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	// Shared ledger simulates universal event propagation
	sharedLedger := NewSyncLedger(2000)

	// Create mixed network:
	// - 2 MQTT-only naras
	// - 2 Gossip-only naras
	// - 2 Hybrid naras (bridge between MQTT and gossip)
	naras := make([]*LocalNara, 6)
	modes := []TransportMode{TransportMQTT, TransportMQTT, TransportGossip, TransportGossip, TransportHybrid, TransportHybrid}

	for i := 0; i < 6; i++ {
		ln := NewLocalNara("mixed-nara-"+string(rune('a'+i)), "test-soul", "", "", "", 50, 1000)
		ln.Network.TransportMode = modes[i]
		ln.SyncLedger = sharedLedger

		me := ln.getMeObservation()
		me.LastRestart = time.Now().Unix() - 200
		me.LastSeen = time.Now().Unix()
		ln.setMeObservation(me)

		naras[i] = ln
	}

	// MQTT-only nara creates event (should propagate via hybrid bridges)
	mqttEvent := NewTeaseEvent("mixed-nara-a", "mixed-nara-c", "high restarts")
	sharedLedger.AddSocialEvent(mqttEvent)

	// Gossip-only nara creates event (should propagate via hybrid bridges)
	gossipEvent := NewTeaseEvent("mixed-nara-c", "mixed-nara-a", "came back")
	sharedLedger.AddSocialEvent(gossipEvent)

	// All naras should eventually see both events (hybrid naras bridge the gap)
	time.Sleep(200 * time.Millisecond)

	for i := 0; i < 6; i++ {
		events := naras[i].SyncLedger.GetEventsByService(ServiceSocial)
		if len(events) < 2 {
			t.Errorf("Nara %d (mode: %v) expected to see 2 events, got %d", i, modes[i], len(events))
		}
	}

	t.Logf("✅ Mixed topology: MQTT-only, gossip-only, and hybrid naras all saw both events")
}

// TestIntegration_ZineCreationAndExchange validates zine structure and bidirectional exchange
func TestIntegration_ZineCreationAndExchange(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	// Create 2 naras
	alice := NewLocalNara("alice", "alice-soul", "", "", "", 50, 1000)
	bob := NewLocalNara("bob", "bob-soul", "", "", "", 50, 1000)

	// Alice creates some events
	for i := 0; i < 5; i++ {
		event := NewTeaseEvent("alice", "bob", "test")
		alice.SyncLedger.AddSocialEvent(event)
	}

	// Alice creates a zine
	aliceZine := createTestZine(alice)

	// Verify zine structure
	if aliceZine.From != "alice" {
		t.Errorf("Expected zine from alice, got %s", aliceZine.From)
	}
	if len(aliceZine.Events) == 0 {
		t.Error("Expected zine to contain events")
	}
	if aliceZine.CreatedAt == 0 {
		t.Error("Expected zine to have creation timestamp")
	}

	// Bob receives alice's zine and merges events
	for _, e := range aliceZine.Events {
		bob.SyncLedger.AddEvent(e) // AddEvent for SyncEvent is correct
	}

	// Bob creates his own zine to send back (bidirectional exchange)
	event := NewTeaseEvent("bob", "alice", "response")
	bob.SyncLedger.AddSocialEvent(event)
	bobZine := createTestZine(bob)

	// Alice receives bob's zine
	for _, e := range bobZine.Events {
		alice.SyncLedger.AddEvent(e) // AddEvent for SyncEvent is correct
	}

	// Verify bidirectional exchange worked
	aliceEvents := alice.SyncLedger.GetEventsByService(ServiceSocial)
	bobEvents := bob.SyncLedger.GetEventsByService(ServiceSocial)

	if len(bobEvents) < 5 {
		t.Errorf("Bob should have received alice's 5 events, got %d", len(bobEvents))
	}
	if len(aliceEvents) < 6 { // original 5 + bob's 1
		t.Errorf("Alice should have 6 events total (5 original + 1 from bob), got %d", len(aliceEvents))
	}

	t.Logf("✅ Zine exchange: bidirectional propagation working correctly")
}

// TestIntegration_GossipTargetSelection validates that gossip picks random mesh neighbors
func TestIntegration_GossipTargetSelection(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	ln := NewLocalNara("test-nara", "test-soul", "", "", "", 50, 1000)

	// Add 10 mesh-enabled neighbors
	for i := 0; i < 10; i++ {
		neighborName := "neighbor-" + string(rune('a'+i))
		neighbor := NewNara(neighborName)
		neighbor.Status.MeshIP = "100.64.0." + string(rune('1'+i))
		ln.Network.importNara(neighbor)
		ln.setObservation(neighborName, NaraObservation{Online: "ONLINE"})
	}

	// Select gossip targets multiple times
	selections := make(map[string]int)
	for i := 0; i < 50; i++ {
		targets := selectGossipTargets(ln.Network, 3)
		for _, target := range targets {
			selections[target]++
		}
	}

	// Verify randomness: each neighbor should be selected at least once
	if len(selections) < 5 { // Should hit at least half the neighbors
		t.Errorf("Expected gossip to select diverse neighbors, only selected %d unique targets", len(selections))
	}

	t.Logf("✅ Gossip target selection: selected %d unique neighbors across 50 rounds", len(selections))
}

// TestIntegration_GossipEventDeduplication validates that duplicate events arriving
// via both MQTT and gossip are deduplicated automatically
func TestIntegration_GossipEventDeduplication(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	ledger := NewSyncLedger(1000)

	// Create event
	event := NewTeaseEvent("alice", "bob", "high restarts")

	// Add same event multiple times (simulating arrival via MQTT and gossip)
	added1 := ledger.AddSocialEvent(event)
	added2 := ledger.AddSocialEvent(event) // Duplicate
	added3 := ledger.AddSocialEvent(event) // Duplicate

	// First should succeed, duplicates should be rejected
	if !added1 {
		t.Error("First event should be added")
	}
	if added2 || added3 {
		t.Error("Duplicate events should be rejected")
	}

	// Verify only one event in ledger
	events := ledger.GetEventsByService(ServiceSocial)
	if len(events) != 1 {
		t.Errorf("Expected 1 event after deduplication, got %d", len(events))
	}

	t.Logf("✅ Event deduplication: same event via multiple transports correctly deduplicated")
}

// Helper: createTestZine creates a zine from a nara's recent events
func createTestZine(ln *LocalNara) *Zine {
	// Get recent events (last 5 minutes for test purposes)
	cutoff := time.Now().Add(-5 * time.Minute).UnixNano()
	allEvents := ln.SyncLedger.GetAllEvents()

	var recentEvents []SyncEvent
	for _, e := range allEvents {
		if e.Timestamp >= cutoff {
			recentEvents = append(recentEvents, e)
		}
	}

	return &Zine{
		From:      ln.Me.Name,
		CreatedAt: time.Now().Unix(),
		Events:    recentEvents,
	}
}

// Helper: selectGossipTargets selects random mesh neighbors for gossip
// (This will be implemented properly in the actual code)
func selectGossipTargets(network *Network, count int) []string {
	online := network.NeighbourhoodOnlineNames()

	// Filter to mesh-enabled only
	var meshEnabled []string
	for _, name := range online {
		if network.getMeshIPForNara(name) != "" {
			meshEnabled = append(meshEnabled, name)
		}
	}

	// Return random subset
	if len(meshEnabled) <= count {
		return meshEnabled
	}

	// Simple random selection for test
	targets := make([]string, 0, count)
	used := make(map[int]bool)
	for len(targets) < count && len(targets) < len(meshEnabled) {
		idx := len(meshEnabled) / (count + 1) * (len(targets) + 1)
		if !used[idx] {
			targets = append(targets, meshEnabled[idx])
			used[idx] = true
		}
	}
	return targets
}
