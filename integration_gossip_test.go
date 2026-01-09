package nara

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// TestIntegration_GossipOnlyMode validates that naras can operate in gossip-only mode
// without MQTT, spreading events purely via P2P zine exchanges using performGossipRound()
func TestIntegration_GossipOnlyMode(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	// Create 5 naras in gossip-only mode with real HTTP test servers
	type testNara struct {
		ln     *LocalNara
		server *httptest.Server
	}
	naras := make([]testNara, 5)

	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("gossip-nara-%c", 'a'+i)
		ln := NewLocalNara(name, testSoul(name), "", "", "", 50, 1000)
		ln.Network.TransportMode = TransportGossip

		// Mark as not booting
		me := ln.getMeObservation()
		me.LastRestart = time.Now().Unix() - 200
		me.LastSeen = time.Now().Unix()
		ln.setMeObservation(me)

		// Create HTTP test server for this nara's gossip endpoint
		mux := http.NewServeMux()
		mux.HandleFunc("/gossip/zine", ln.Network.httpGossipZineHandler)
		server := httptest.NewServer(mux)

		naras[i] = testNara{ln: ln, server: server}
	}
	defer func() {
		for _, n := range naras {
			n.server.Close()
		}
	}()

	// Shared HTTP client for all naras
	sharedClient := &http.Client{Timeout: 5 * time.Second}

	// Set up test hooks and neighborhood: each nara knows about the others
	for i := 0; i < 5; i++ {
		naras[i].ln.Network.testHTTPClient = sharedClient
		naras[i].ln.Network.testMeshURLs = make(map[string]string)

		for j := 0; j < 5; j++ {
			if i != j {
				neighborName := naras[j].ln.Me.Name
				neighbor := NewNara(neighborName)
				neighbor.Status.PublicKey = FormatPublicKey(naras[j].ln.Keypair.PublicKey)
				naras[i].ln.Network.importNara(neighbor)
				naras[i].ln.setObservation(neighborName, NaraObservation{Online: "ONLINE"})
				// Store test server URL for this neighbor
				naras[i].ln.Network.testMeshURLs[neighborName] = naras[j].server.URL
			}
		}
	}

	// Nara A creates a social event
	event := NewSocialSyncEvent("tease", naras[0].ln.Me.Name, "gossip-nara-b", "high restarts", "")
	naras[0].ln.SyncLedger.AddEvent(event)

	// Run gossip rounds using the REAL performGossipRound() production code
	for round := 0; round < 3; round++ {
		for i := 0; i < 5; i++ {
			naras[i].ln.Network.performGossipRound()
		}
		// Wait for async exchanges to complete
		time.Sleep(50 * time.Millisecond)
	}

	// Verify event propagated to most naras via gossip
	propagatedCount := 0
	for i := 0; i < 5; i++ {
		events := naras[i].ln.SyncLedger.GetEventsByService(ServiceSocial)
		for _, e := range events {
			if e.Social != nil && e.Social.Type == "tease" && e.Social.Target == "gossip-nara-b" {
				propagatedCount++
				break
			}
		}
	}

	// Should reach all 5 naras via gossip (epidemic spread)
	if propagatedCount < 5 {
		t.Errorf("Expected event to reach all 5 naras via gossip, reached %d", propagatedCount)
	}

	t.Logf("✅ Gossip-only mode: event reached %d/5 naras via performGossipRound()", propagatedCount)
}

// TestIntegration_HybridMode validates that naras can use both MQTT and gossip simultaneously
// In hybrid mode, events should propagate via gossip using performGossipRound()
func TestIntegration_HybridMode(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	// Create 3 naras in hybrid mode with HTTP servers for gossip
	type testNara struct {
		ln     *LocalNara
		server *httptest.Server
	}
	naras := make([]testNara, 3)

	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("hybrid-nara-%c", 'a'+i)
		ln := NewLocalNara(name, testSoul(name), "", "", "", 50, 1000)
		ln.Network.TransportMode = TransportHybrid

		me := ln.getMeObservation()
		me.LastRestart = time.Now().Unix() - 200
		me.LastSeen = time.Now().Unix()
		ln.setMeObservation(me)

		mux := http.NewServeMux()
		mux.HandleFunc("/gossip/zine", ln.Network.httpGossipZineHandler)
		server := httptest.NewServer(mux)

		naras[i] = testNara{ln: ln, server: server}
	}
	defer func() {
		for _, n := range naras {
			n.server.Close()
		}
	}()

	// Shared HTTP client for all naras
	sharedClient := &http.Client{Timeout: 5 * time.Second}

	// Set up test hooks and neighborhood
	for i := 0; i < 3; i++ {
		naras[i].ln.Network.testHTTPClient = sharedClient
		naras[i].ln.Network.testMeshURLs = make(map[string]string)

		for j := 0; j < 3; j++ {
			if i != j {
				neighbor := NewNara(naras[j].ln.Me.Name)
				neighbor.Status.PublicKey = FormatPublicKey(naras[j].ln.Keypair.PublicKey)
				naras[i].ln.Network.importNara(neighbor)
				naras[i].ln.setObservation(naras[j].ln.Me.Name, NaraObservation{Online: "ONLINE"})
				naras[i].ln.Network.testMeshURLs[naras[j].ln.Me.Name] = naras[j].server.URL
			}
		}
	}

	// Nara A creates an event
	event := NewSocialSyncEvent("tease", "hybrid-nara-a", "hybrid-nara-b", "came back", "")
	naras[0].ln.SyncLedger.AddEvent(event)

	// Run gossip rounds using performGossipRound() production code
	for round := 0; round < 2; round++ {
		for i := 0; i < 3; i++ {
			naras[i].ln.Network.performGossipRound()
		}
		time.Sleep(50 * time.Millisecond)
	}

	// All naras should see the event via gossip transport
	for i := 0; i < 3; i++ {
		events := naras[i].ln.SyncLedger.GetEventsByService(ServiceSocial)
		if len(events) < 1 {
			t.Errorf("Nara %d expected to see at least 1 event, got %d", i, len(events))
		}
	}

	t.Logf("✅ Hybrid mode: event propagated to all naras via performGossipRound()")
}

// TestIntegration_MixedNetworkTopology validates that gossip-enabled naras (gossip-only and hybrid)
// can propagate events via performGossipRound(), while MQTT-only naras are excluded from gossip
func TestIntegration_MixedNetworkTopology(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	// Create mixed network with HTTP servers for gossip-enabled naras
	// - 2 MQTT-only naras (no gossip server)
	// - 2 Gossip-only naras (gossip server)
	// - 2 Hybrid naras (gossip server)
	type testNara struct {
		ln     *LocalNara
		server *httptest.Server // nil for MQTT-only
		mode   TransportMode
	}
	modes := []TransportMode{TransportMQTT, TransportMQTT, TransportGossip, TransportGossip, TransportHybrid, TransportHybrid}
	naras := make([]testNara, 6)

	for i := 0; i < 6; i++ {
		name := fmt.Sprintf("mixed-nara-%c", 'a'+i)
		ln := NewLocalNara(name, testSoul(name), "", "", "", 50, 1000)
		ln.Network.TransportMode = modes[i]

		me := ln.getMeObservation()
		me.LastRestart = time.Now().Unix() - 200
		me.LastSeen = time.Now().Unix()
		ln.setMeObservation(me)

		var server *httptest.Server
		if modes[i] != TransportMQTT {
			// Only gossip-enabled naras get HTTP servers
			mux := http.NewServeMux()
			mux.HandleFunc("/gossip/zine", ln.Network.httpGossipZineHandler)
			server = httptest.NewServer(mux)
		}

		naras[i] = testNara{ln: ln, server: server, mode: modes[i]}
	}
	defer func() {
		for _, n := range naras {
			if n.server != nil {
				n.server.Close()
			}
		}
	}()

	// Shared HTTP client for all naras
	sharedClient := &http.Client{Timeout: 5 * time.Second}

	// Set up test hooks and neighborhood
	for i := 0; i < 6; i++ {
		naras[i].ln.Network.testHTTPClient = sharedClient
		naras[i].ln.Network.testMeshURLs = make(map[string]string)

		for j := 0; j < 6; j++ {
			if i != j {
				neighbor := NewNara(naras[j].ln.Me.Name)
				neighbor.Status.PublicKey = FormatPublicKey(naras[j].ln.Keypair.PublicKey)
				naras[i].ln.Network.importNara(neighbor)
				naras[i].ln.setObservation(naras[j].ln.Me.Name, NaraObservation{Online: "ONLINE"})
				// Only add URL for gossip-enabled neighbors
				if naras[j].server != nil {
					naras[i].ln.Network.testMeshURLs[naras[j].ln.Me.Name] = naras[j].server.URL
				}
			}
		}
	}

	// Gossip-only nara creates event
	event := NewSocialSyncEvent("tease", "mixed-nara-c", "mixed-nara-a", "high restarts", "")
	naras[2].ln.SyncLedger.AddEvent(event) // mixed-nara-c is gossip-only

	// Run gossip rounds using performGossipRound() - only gossip-enabled naras participate
	for round := 0; round < 2; round++ {
		for i := 0; i < 6; i++ {
			if naras[i].server != nil { // Only gossip-enabled naras run gossip rounds
				naras[i].ln.Network.performGossipRound()
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Verify: gossip-enabled naras (indices 2-5) should have the event
	gossipEnabledWithEvent := 0
	for i := 2; i < 6; i++ {
		events := naras[i].ln.SyncLedger.GetEventsByService(ServiceSocial)
		if len(events) >= 1 {
			gossipEnabledWithEvent++
		}
	}
	if gossipEnabledWithEvent < 4 {
		t.Errorf("Expected all 4 gossip-enabled naras to have event, got %d", gossipEnabledWithEvent)
	}

	// Verify: MQTT-only naras (indices 0-1) should NOT have the event via gossip
	// (they would need MQTT or a hybrid bridge in real deployment)
	for i := 0; i < 2; i++ {
		events := naras[i].ln.SyncLedger.GetEventsByService(ServiceSocial)
		if len(events) > 0 {
			t.Errorf("MQTT-only nara %d should not receive events via gossip, got %d", i, len(events))
		}
	}

	t.Logf("✅ Mixed topology: gossip-enabled naras propagated event via performGossipRound()")
}

// TestIntegration_ZineCreationAndExchange validates zine structure and bidirectional exchange
// Uses real HTTP servers and the production performGossipRound() code path
func TestIntegration_ZineCreationAndExchange(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	// Create 2 naras with valid souls for keypair generation
	alice := NewLocalNara("alice", testSoul("alice"), "", "", "", 50, 1000)
	bob := NewLocalNara("bob", testSoul("bob"), "", "", "", 50, 1000)

	// Verify keypairs were generated
	if len(alice.Keypair.PrivateKey) == 0 {
		t.Fatal("Alice keypair not generated - soul invalid")
	}
	if len(bob.Keypair.PrivateKey) == 0 {
		t.Fatal("Bob keypair not generated - soul invalid")
	}

	alice.Network.TransportMode = TransportGossip
	bob.Network.TransportMode = TransportGossip

	// Create HTTP servers for both naras (these handle /gossip/zine requests)
	aliceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		alice.Network.httpGossipZineHandler(w, r)
	}))
	defer aliceServer.Close()

	bobServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bob.Network.httpGossipZineHandler(w, r)
	}))
	defer bobServer.Close()

	// Set up test hooks so performGossipRound() can reach the test servers
	sharedClient := &http.Client{Timeout: 5 * time.Second}

	// Alice knows about bob
	alice.Network.testHTTPClient = sharedClient
	alice.Network.testMeshURLs = map[string]string{"bob": bobServer.URL}
	bobNara := NewNara("bob")
	bobNara.Status.PublicKey = FormatPublicKey(bob.Keypair.PublicKey)
	alice.Network.importNara(bobNara)
	alice.setObservation("bob", NaraObservation{Online: "ONLINE"})

	// Bob knows about alice
	bob.Network.testHTTPClient = sharedClient
	bob.Network.testMeshURLs = map[string]string{"alice": aliceServer.URL}
	aliceNara := NewNara("alice")
	aliceNara.Status.PublicKey = FormatPublicKey(alice.Keypair.PublicKey)
	bob.Network.importNara(aliceNara)
	bob.setObservation("alice", NaraObservation{Online: "ONLINE"})

	// Alice creates some events
	for i := 0; i < 5; i++ {
		event := NewSocialSyncEvent("tease", "alice", "bob", "test", "")
		alice.SyncLedger.AddEvent(event)
	}

	// Verify alice has a zine to send
	aliceZine := alice.Network.createZine()
	if aliceZine == nil {
		t.Fatal("Alice createZine returned nil")
	}
	if len(aliceZine.Events) != 5 {
		t.Errorf("Expected alice's zine to have 5 events, got %d", len(aliceZine.Events))
	}
	if aliceZine.Signature == "" {
		t.Error("Expected zine to be signed")
	}

	// Alice performs a gossip round - this triggers the REAL HTTP exchange
	alice.Network.performGossipRound()

	// Wait for async exchange to complete
	time.Sleep(100 * time.Millisecond)

	// Verify bob received alice's events via the gossip round
	bobEvents := bob.SyncLedger.GetEventsByService(ServiceSocial)
	if len(bobEvents) < 5 {
		t.Errorf("Bob should have received alice's 5 events via gossip, got %d", len(bobEvents))
	}

	// Bob creates his own event
	event := NewSocialSyncEvent("tease", "bob", "alice", "response", "")
	bob.SyncLedger.AddEvent(event)

	// Bob performs a gossip round back to alice
	bob.Network.performGossipRound()

	// Wait for async exchange
	time.Sleep(100 * time.Millisecond)

	// Verify alice received bob's event via gossip
	aliceEvents := alice.SyncLedger.GetEventsByService(ServiceSocial)
	if len(aliceEvents) < 6 { // original 5 + bob's 1
		t.Errorf("Alice should have 6 events total (5 original + 1 from bob via gossip), got %d", len(aliceEvents))
	}

	t.Logf("✅ Zine exchange: bidirectional propagation via performGossipRound() working correctly")
}

// TestIntegration_GossipTargetSelection validates that gossip picks random mesh neighbors
// Uses the production selectGossipTargets() method instead of test helper
func TestIntegration_GossipTargetSelection(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	ln := NewLocalNara("test-nara", testSoul("test-nara"), "", "", "", 50, 1000)
	ln.Network.TransportMode = TransportGossip

	// Add 10 mesh-enabled neighbors
	for i := 0; i < 10; i++ {
		neighborName := fmt.Sprintf("neighbor-%c", 'a'+i)
		neighbor := NewNara(neighborName)
		neighbor.Status.MeshIP = fmt.Sprintf("100.64.0.%d", 1+i)
		ln.Network.importNara(neighbor)
		ln.setObservation(neighborName, NaraObservation{Online: "ONLINE"})
	}

	// Select gossip targets multiple times using production code
	selections := make(map[string]int)
	for i := 0; i < 50; i++ {
		targets := ln.Network.selectGossipTargets()
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
	event := NewSocialSyncEvent("tease", "alice", "bob", "high restarts", "")

	// Add same event multiple times (simulating arrival via MQTT and gossip)
	added1 := ledger.AddEvent(event)
	added2 := ledger.AddEvent(event) // Duplicate
	added3 := ledger.AddEvent(event) // Duplicate

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

// MockPeerDiscovery returns a predefined list of peers for testing
type MockPeerDiscovery struct {
	peers []DiscoveredPeer
}

func (m *MockPeerDiscovery) ScanForPeers(myIP string) []DiscoveredPeer {
	return m.peers
}

// TestIntegration_MeshDiscovery validates the discovery mechanism using a mock strategy
// This tests the actual discoverMeshPeers() production code path:
//  1. Strategy pattern scans for peers (mocked: returns predefined list)
//  2. Production code adds discovered peers to neighborhood
//  3. Production code marks them as ONLINE
//  4. Production code stores mesh IPs
//  5. Bootstrap is skipped in test (requires real HTTP mesh)
func TestIntegration_MeshDiscovery(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	// Create a nara that will discover peers (use testSoul for valid keypair)
	discoverer := NewLocalNara("discovery-nara-a", testSoul("discovery-nara-a"), "", "", "", 50, 1000)
	discoverer.Network.TransportMode = TransportGossip

	// Verify no neighbors initially
	if len(discoverer.Network.NeighbourhoodOnlineNames()) != 0 {
		t.Fatalf("Expected 0 initial neighbors, got %d", len(discoverer.Network.NeighbourhoodOnlineNames()))
	}

	// Set up mock discovery that returns 2 peers
	// This replaces the real TailscalePeerDiscovery which would scan 100.64.0.1-254
	mockDiscovery := &MockPeerDiscovery{
		peers: []DiscoveredPeer{
			{Name: "discovery-nara-b", MeshIP: "100.64.0.2"},
			{Name: "discovery-nara-c", MeshIP: "100.64.0.3"},
		},
	}
	discoverer.Network.peerDiscovery = mockDiscovery

	// Mock tsnetMesh so discoverMeshPeers doesn't bail early
	// (In real code, this would be the actual tsnet mesh)
	// Server() returns nil, so bootstrap will be skipped
	discoverer.Network.tsnetMesh = &TsnetMesh{}

	// Run discovery - this is the ACTUAL production code path
	// The only mock is the strategy that returns peers
	discoverer.Network.discoverMeshPeers()

	// Verify neighbors were discovered
	neighborNames := discoverer.Network.NeighbourhoodOnlineNames()
	if len(neighborNames) != 2 {
		t.Errorf("Expected 2 neighbors after discovery, got %d: %v", len(neighborNames), neighborNames)
	}

	// Verify the specific naras were discovered
	expectedNeighbors := map[string]bool{
		"discovery-nara-b": false,
		"discovery-nara-c": false,
	}
	for _, name := range neighborNames {
		if _, ok := expectedNeighbors[name]; ok {
			expectedNeighbors[name] = true
		}
	}
	for name, found := range expectedNeighbors {
		if !found {
			t.Errorf("Expected to discover %s but didn't", name)
		}
	}

	// Verify mesh IPs were stored correctly
	meshIPb := discoverer.Network.getMeshIPForNara("discovery-nara-b")
	if meshIPb != "100.64.0.2" {
		t.Errorf("Expected mesh IP 100.64.0.2 for discovery-nara-b, got %s", meshIPb)
	}

	meshIPc := discoverer.Network.getMeshIPForNara("discovery-nara-c")
	if meshIPc != "100.64.0.3" {
		t.Errorf("Expected mesh IP 100.64.0.3 for discovery-nara-c, got %s", meshIPc)
	}

	// Verify observations were created
	obsB := discoverer.getObservation("discovery-nara-b")
	if obsB.Online != "ONLINE" {
		t.Errorf("Expected discovery-nara-b to be marked ONLINE, got %s", obsB.Online)
	}

	obsC := discoverer.getObservation("discovery-nara-c")
	if obsC.Online != "ONLINE" {
		t.Errorf("Expected discovery-nara-c to be marked ONLINE, got %s", obsC.Online)
	}

	t.Logf("✅ Mesh discovery: successfully discovered 2 peers using strategy pattern")
	t.Logf("   - discovery-nara-b at %s", meshIPb)
	t.Logf("   - discovery-nara-c at %s", meshIPc)
}

// TestIntegration_GossipOnlyBootRecovery validates that in gossip-only mode,
// boot recovery discovers peers via mesh before trying to sync.
// BUG: Currently boot recovery runs before mesh discovery, so it finds no neighbors.
func TestIntegration_GossipOnlyBootRecovery(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	// Create a nara in gossip-only mode
	nara := NewLocalNara("boot-recovery-nara", testSoul("boot-recovery-nara"), "", "", "", 50, 1000)
	nara.Network.TransportMode = TransportGossip

	// Set up mock peer discovery that returns peers
	mockDiscovery := &MockPeerDiscovery{
		peers: []DiscoveredPeer{
			{Name: "peer-alpha", MeshIP: "100.64.0.10"},
			{Name: "peer-beta", MeshIP: "100.64.0.11"},
		},
	}
	nara.Network.peerDiscovery = mockDiscovery
	nara.Network.tsnetMesh = &TsnetMesh{} // Mock mesh so discovery doesn't bail

	// Verify no neighbors initially (simulates fresh boot)
	if len(nara.Network.NeighbourhoodOnlineNames()) != 0 {
		t.Fatalf("Expected 0 initial neighbors, got %d", len(nara.Network.NeighbourhoodOnlineNames()))
	}

	// THE BUG: In gossip-only mode, there are no MQTT-discovered neighbors.
	// Boot recovery should trigger mesh discovery automatically before checking for neighbors.
	// We test this by calling getNeighborsForBootRecovery() which should return
	// discovered peers in gossip-only mode.

	online := nara.Network.getNeighborsForBootRecovery()

	// In gossip-only mode, this MUST return discovered peers (after triggering mesh discovery)
	if len(online) != 2 {
		t.Errorf("BUG: In gossip-only mode, getNeighborsForBootRecovery() should discover peers via mesh. Expected 2 neighbors, got %d", len(online))
		t.Log("Boot recovery needs to call discoverMeshPeers() before checking for neighbors in gossip-only mode")
	} else {
		t.Logf("✅ Gossip-only boot recovery: found %d peers via mesh discovery", len(online))
	}
}
