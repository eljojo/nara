package nara

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eljojo/nara/types"
	"github.com/sirupsen/logrus"
)

// testPeerDiscoverySoul creates a unique soul for peer discovery tests
func testPeerDiscoverySoul(name string) string {
	hw := hashBytes([]byte("peer-discovery-test-" + name))
	soul := NativeSoulCustom(hw, types.NaraName(name))
	return FormatSoul(soul)
}

// TestIntegration_DiscoverNarasFromEventStream verifies that naras mentioned in
// events are automatically added to the Neighbourhood, even without their public key.
func TestIntegration_DiscoverNarasFromEventStream(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	// Setup: alice and bob in full mesh with gossip
	mesh := testCreateMeshNetwork(t, []string{"alice", "bob"}, 50, 1000)
	alice := mesh.GetByName("alice")
	bob := mesh.GetByName("bob")

	// Bob doesn't know "ghost" yet
	if bob.Network.Neighbourhood["ghost"] != nil {
		t.Fatal("Bob should not know ghost initially")
	}

	// Alice creates an event mentioning "ghost" as the target
	event := NewSocialSyncEvent("tease", "alice", "ghost", "spooky behavior", "")
	event.Sign("alice", alice.Keypair)
	alice.SyncLedger.AddEvent(event)

	// Alice sends zine to Bob (performGossipRound will send to bob via testMeshURLs)
	alice.Network.performGossipRound()
	time.Sleep(100 * time.Millisecond)

	// Bob should now have a Nara entry for "ghost" (even without public key)
	if bob.Network.Neighbourhood["ghost"] == nil {
		t.Error("Bob should have discovered ghost from event stream")
	}
}

// TestIntegration_HeyThereEventPropagation verifies that hey_there events
// (carrying public key and mesh IP) propagate through the gossip network.
func TestIntegration_HeyThereEventPropagation(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	// Setup: alice, bob, carol in a chain: alice <-> bob <-> carol
	alice := testLocalNaraWithParams(t, "alice", 50, 1000)
	alice.Network.TransportMode = TransportGossip

	bob := testLocalNaraWithParams(t, "bob", 50, 1000)
	bob.Network.TransportMode = TransportGossip

	carol := testLocalNaraWithParams(t, "carol", 50, 1000)
	carol.Network.TransportMode = TransportGossip

	// Set up test servers
	aliceMux := http.NewServeMux()
	aliceMux.HandleFunc("/gossip/zine", alice.Network.httpGossipZineHandler)
	aliceServer := httptest.NewServer(aliceMux)
	defer aliceServer.Close()

	bobMux := http.NewServeMux()
	bobMux.HandleFunc("/gossip/zine", bob.Network.httpGossipZineHandler)
	bobServer := httptest.NewServer(bobMux)
	defer bobServer.Close()

	carolMux := http.NewServeMux()
	carolMux.HandleFunc("/gossip/zine", carol.Network.httpGossipZineHandler)
	carolServer := httptest.NewServer(carolMux)
	defer carolServer.Close()

	sharedClient := &http.Client{Timeout: 5 * time.Second}

	// Alice knows bob
	alice.Network.testHTTPClient = sharedClient
	alice.Network.testMeshURLs = map[types.NaraName]string{types.NaraName("bob"): bobServer.URL}
	bobNara := NewNara("bob")
	bobNara.Status.PublicKey = FormatPublicKey(bob.Keypair.PublicKey)
	alice.Network.importNara(bobNara)
	alice.setObservation("bob", NaraObservation{Online: "ONLINE"})

	// Bob knows alice and carol
	bob.Network.testHTTPClient = sharedClient
	bob.Network.testMeshURLs = map[types.NaraName]string{
		types.NaraName("alice"): aliceServer.URL,
		types.NaraName("carol"): carolServer.URL,
	}
	aliceNara := NewNara("alice")
	aliceNara.Status.PublicKey = FormatPublicKey(alice.Keypair.PublicKey)
	bob.Network.importNara(aliceNara)
	bob.setObservation("alice", NaraObservation{Online: "ONLINE"})
	carolNaraForBob := NewNara("carol")
	carolNaraForBob.Status.PublicKey = FormatPublicKey(carol.Keypair.PublicKey)
	bob.Network.importNara(carolNaraForBob)
	bob.setObservation("carol", NaraObservation{Online: "ONLINE"})

	// Carol knows bob
	carol.Network.testHTTPClient = sharedClient
	carol.Network.testMeshURLs = map[types.NaraName]string{types.NaraName("bob"): bobServer.URL}
	bobNaraForCarol := NewNara("bob")
	bobNaraForCarol.Status.PublicKey = FormatPublicKey(bob.Keypair.PublicKey)
	carol.Network.importNara(bobNaraForCarol)
	carol.setObservation("bob", NaraObservation{Online: "ONLINE"})

	// Alice doesn't know carol's public key
	if alice.Network.getPublicKeyForNara("carol") != nil {
		t.Fatal("Alice should not know carol's public key initially")
	}

	// Carol's startup emits a hey_there sync event
	carol.Network.InitGossipIdentity()

	// Carol sends zine to bob
	carol.Network.performGossipRound()
	time.Sleep(100 * time.Millisecond)

	// Bob should now know carol's public key
	if bob.Network.getPublicKeyForNara("carol") == nil {
		t.Error("Bob should know carol's public key from hey_there event")
	}

	// Bob sends zine to alice (forwarding carol's hey_there)
	bob.Network.performGossipRound()
	time.Sleep(100 * time.Millisecond)

	// Alice should now know carol's public key too
	if alice.Network.Neighbourhood["carol"] == nil {
		t.Error("Alice should have discovered carol")
	}
	if alice.Network.getPublicKeyForNara("carol") == nil {
		t.Error("Alice should know carol's public key from propagated hey_there event")
	}
}

// TestIntegration_PeerResolutionProtocol verifies that when a nara doesn't know
// another nara's identity, it can query neighbors to discover them.
func TestIntegration_PeerResolutionProtocol(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	// Setup: alice <-> bob <-> carol, carol knows ghost's identity
	alice := testLocalNaraWithParams(t, "alice", 50, 1000)
	alice.Network.TransportMode = TransportGossip

	bob := testLocalNaraWithParams(t, "bob", 50, 1000)
	bob.Network.TransportMode = TransportGossip

	carol := testLocalNaraWithParams(t, "carol", 50, 1000)
	carol.Network.TransportMode = TransportGossip

	ghost := testLocalNaraWithParams(t, "ghost", 50, 1000)
	ghost.Network.TransportMode = TransportGossip

	// Set up test servers with peer query handlers
	aliceMux := http.NewServeMux()
	aliceMux.HandleFunc("/gossip/zine", alice.Network.httpGossipZineHandler)
	aliceMux.HandleFunc("/peer/query", alice.Network.httpPeerQueryHandler)
	aliceServer := httptest.NewServer(aliceMux)
	defer aliceServer.Close()

	bobMux := http.NewServeMux()
	bobMux.HandleFunc("/gossip/zine", bob.Network.httpGossipZineHandler)
	bobMux.HandleFunc("/peer/query", bob.Network.httpPeerQueryHandler)
	bobServer := httptest.NewServer(bobMux)
	defer bobServer.Close()

	carolMux := http.NewServeMux()
	carolMux.HandleFunc("/gossip/zine", carol.Network.httpGossipZineHandler)
	carolMux.HandleFunc("/peer/query", carol.Network.httpPeerQueryHandler)
	carolServer := httptest.NewServer(carolMux)
	defer carolServer.Close()

	sharedClient := &http.Client{Timeout: 5 * time.Second}

	// Alice knows bob (and has URLs for carol for redirect following)
	// In a real mesh network, alice could reach any mesh IP even if she doesn't know their identity
	alice.Network.testHTTPClient = sharedClient
	alice.Network.testMeshURLs = map[types.NaraName]string{
		types.NaraName("bob"):   bobServer.URL,
		types.NaraName("carol"): carolServer.URL, // URL only, no identity - needed for redirect following
	}
	bobNara := NewNara("bob")
	bobNara.Status.PublicKey = FormatPublicKey(bob.Keypair.PublicKey)
	alice.Network.importNara(bobNara)
	alice.setObservation("bob", NaraObservation{Online: "ONLINE"})

	// Bob knows alice and carol
	bob.Network.testHTTPClient = sharedClient
	bob.Network.testMeshURLs = map[types.NaraName]string{
		types.NaraName("alice"): aliceServer.URL,
		types.NaraName("carol"): carolServer.URL,
	}
	aliceNara := NewNara("alice")
	aliceNara.Status.PublicKey = FormatPublicKey(alice.Keypair.PublicKey)
	bob.Network.importNara(aliceNara)
	bob.setObservation("alice", NaraObservation{Online: "ONLINE"})
	carolNaraForBob := NewNara("carol")
	carolNaraForBob.Status.PublicKey = FormatPublicKey(carol.Keypair.PublicKey)
	bob.Network.importNara(carolNaraForBob)
	bob.setObservation("carol", NaraObservation{Online: "ONLINE"})

	// Carol knows bob and ghost (has ghost's public key!)
	carol.Network.testHTTPClient = sharedClient
	carol.Network.testMeshURLs = map[types.NaraName]string{
		types.NaraName("bob"):   bobServer.URL,
		types.NaraName("alice"): aliceServer.URL, // For responding directly to alice
	}
	bobNaraForCarol := NewNara("bob")
	bobNaraForCarol.Status.PublicKey = FormatPublicKey(bob.Keypair.PublicKey)
	carol.Network.importNara(bobNaraForCarol)
	carol.setObservation("bob", NaraObservation{Online: "ONLINE"})
	// Carol knows ghost's identity
	ghostNara := NewNara("ghost")
	ghostNara.Status.PublicKey = FormatPublicKey(ghost.Keypair.PublicKey)
	ghostNara.Status.MeshIP = "100.64.0.99"
	carol.Network.importNara(ghostNara)

	// Alice doesn't know ghost
	if alice.Network.getPublicKeyForNara("ghost") != nil {
		t.Fatal("Alice should not know ghost's public key initially")
	}

	// Alice resolves ghost's identity by querying neighbors
	response := alice.Network.resolvePeer("ghost")

	// Alice should have received ghost's identity
	if response == nil {
		t.Fatal("Alice should have received a response for ghost")
	}
	if response.PublicKey == "" {
		t.Error("Response should contain ghost's public key")
	}
	if response.PublicKey != FormatPublicKey(ghost.Keypair.PublicKey) {
		t.Error("Response public key should match ghost's actual public key")
	}

	// Alice should now know ghost's public key
	if alice.Network.getPublicKeyForNara("ghost") == nil {
		t.Error("Alice should now know ghost's public key after resolution")
	}
}

// TestIntegration_HeyThereSyncEventEmittedOnStartup verifies that naras automatically
// emit hey_there sync events on startup via InitGossipIdentity().
// This tests the same code path that Start() uses in production.
func TestIntegration_HeyThereSyncEventEmittedOnStartup(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	// Setup: alice and bob, both in gossip mode
	alice := testLocalNaraWithParams(t, "alice", 50, 1000)
	alice.Network.TransportMode = TransportGossip

	bob := testLocalNaraWithParams(t, "bob", 50, 1000)
	bob.Network.TransportMode = TransportGossip

	// Set up test servers
	aliceMux := http.NewServeMux()
	aliceMux.HandleFunc("/gossip/zine", alice.Network.httpGossipZineHandler)
	aliceServer := httptest.NewServer(aliceMux)
	defer aliceServer.Close()

	bobMux := http.NewServeMux()
	bobMux.HandleFunc("/gossip/zine", bob.Network.httpGossipZineHandler)
	bobServer := httptest.NewServer(bobMux)
	defer bobServer.Close()

	sharedClient := &http.Client{Timeout: 5 * time.Second}

	// Alice knows bob's URL but NOT his public key initially
	alice.Network.testHTTPClient = sharedClient
	alice.Network.testMeshURLs = map[types.NaraName]string{types.NaraName("bob"): bobServer.URL}
	bobNaraForAlice := NewNara("bob")
	// NO public key set - alice doesn't know bob's identity yet
	alice.Network.importNara(bobNaraForAlice)
	alice.setObservation("bob", NaraObservation{Online: "ONLINE"})

	// Bob knows alice's URL but NOT her public key initially
	bob.Network.testHTTPClient = sharedClient
	bob.Network.testMeshURLs = map[types.NaraName]string{types.NaraName("alice"): aliceServer.URL}
	aliceNaraForBob := NewNara("alice")
	// NO public key set - bob doesn't know alice's identity yet
	bob.Network.importNara(aliceNaraForBob)
	bob.setObservation("alice", NaraObservation{Online: "ONLINE"})

	// Verify neither knows the other's public key
	if alice.Network.getPublicKeyForNara("bob") != nil {
		t.Fatal("Alice should not know bob's public key initially")
	}
	if bob.Network.getPublicKeyForNara("alice") != nil {
		t.Fatal("Bob should not know alice's public key initially")
	}

	// Simulate startup - call InitGossipIdentity() which is also called by Start()
	// This tests the same code path that production uses
	alice.Network.InitGossipIdentity()
	bob.Network.InitGossipIdentity()

	// After startup, each nara's SyncLedger should contain their hey_there event
	aliceHasHeyThere := false
	for _, e := range alice.SyncLedger.GetAllEvents() {
		if e.Service == ServiceHeyThere && e.HeyThere != nil && e.HeyThere.From == "alice" {
			aliceHasHeyThere = true
			break
		}
	}
	if !aliceHasHeyThere {
		t.Error("Alice should have automatically emitted a hey_there sync event on startup")
	}

	bobHasHeyThere := false
	for _, e := range bob.SyncLedger.GetAllEvents() {
		if e.Service == ServiceHeyThere && e.HeyThere != nil && e.HeyThere.From == "bob" {
			bobHasHeyThere = true
			break
		}
	}
	if !bobHasHeyThere {
		t.Error("Bob should have automatically emitted a hey_there sync event on startup")
	}

	// Now gossip - alice sends to bob
	alice.Network.performGossipRound()
	time.Sleep(100 * time.Millisecond)

	// Bob should now know alice's public key from the hey_there event
	if bob.Network.getPublicKeyForNara("alice") == nil {
		t.Error("Bob should know alice's public key after receiving her hey_there event")
	}

	// Bob sends to alice
	bob.Network.performGossipRound()
	time.Sleep(100 * time.Millisecond)

	// Alice should now know bob's public key
	if alice.Network.getPublicKeyForNara("bob") == nil {
		t.Error("Alice should know bob's public key after receiving his hey_there event")
	}
}

// TestIntegration_GossipOnlyPeerDiscovery is the full end-to-end test:
// a nara in gossip-only mode should be discovered by the network.
func TestIntegration_GossipOnlyPeerDiscovery(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	// Setup: alice, bob, carol in hybrid mode, ghost in gossip-only mode
	// Network topology: alice <-> bob <-> carol <-> ghost
	alice := testLocalNaraWithParams(t, "alice", 50, 1000)
	alice.Network.TransportMode = TransportHybrid

	bob := testLocalNaraWithParams(t, "bob", 50, 1000)
	bob.Network.TransportMode = TransportHybrid

	carol := testLocalNaraWithParams(t, "carol", 50, 1000)
	carol.Network.TransportMode = TransportHybrid

	ghost := testLocalNaraWithParams(t, "ghost", 50, 1000)
	ghost.Network.TransportMode = TransportGossip // Gossip-only!

	// Set up test servers
	aliceMux := http.NewServeMux()
	aliceMux.HandleFunc("/gossip/zine", alice.Network.httpGossipZineHandler)
	aliceServer := httptest.NewServer(aliceMux)
	defer aliceServer.Close()

	bobMux := http.NewServeMux()
	bobMux.HandleFunc("/gossip/zine", bob.Network.httpGossipZineHandler)
	bobServer := httptest.NewServer(bobMux)
	defer bobServer.Close()

	carolMux := http.NewServeMux()
	carolMux.HandleFunc("/gossip/zine", carol.Network.httpGossipZineHandler)
	carolServer := httptest.NewServer(carolMux)
	defer carolServer.Close()

	ghostMux := http.NewServeMux()
	ghostMux.HandleFunc("/gossip/zine", ghost.Network.httpGossipZineHandler)
	ghostServer := httptest.NewServer(ghostMux)
	defer ghostServer.Close()

	sharedClient := &http.Client{Timeout: 5 * time.Second}

	// Alice knows bob
	alice.Network.testHTTPClient = sharedClient
	alice.Network.testMeshURLs = map[types.NaraName]string{types.NaraName("bob"): bobServer.URL}
	bobNara := NewNara("bob")
	bobNara.Status.PublicKey = FormatPublicKey(bob.Keypair.PublicKey)
	alice.Network.importNara(bobNara)
	alice.setObservation("bob", NaraObservation{Online: "ONLINE"})

	// Bob knows alice and carol
	bob.Network.testHTTPClient = sharedClient
	bob.Network.testMeshURLs = map[types.NaraName]string{
		types.NaraName("alice"): aliceServer.URL,
		types.NaraName("carol"): carolServer.URL,
	}
	aliceNara := NewNara("alice")
	aliceNara.Status.PublicKey = FormatPublicKey(alice.Keypair.PublicKey)
	bob.Network.importNara(aliceNara)
	bob.setObservation("alice", NaraObservation{Online: "ONLINE"})
	carolNaraForBob := NewNara("carol")
	carolNaraForBob.Status.PublicKey = FormatPublicKey(carol.Keypair.PublicKey)
	bob.Network.importNara(carolNaraForBob)
	bob.setObservation("carol", NaraObservation{Online: "ONLINE"})

	// Carol knows bob and ghost
	carol.Network.testHTTPClient = sharedClient
	carol.Network.testMeshURLs = map[types.NaraName]string{
		types.NaraName("bob"):   bobServer.URL,
		types.NaraName("ghost"): ghostServer.URL,
	}
	bobNaraForCarol := NewNara("bob")
	bobNaraForCarol.Status.PublicKey = FormatPublicKey(bob.Keypair.PublicKey)
	carol.Network.importNara(bobNaraForCarol)
	carol.setObservation("bob", NaraObservation{Online: "ONLINE"})
	ghostNaraForCarol := NewNara("ghost")
	ghostNaraForCarol.Status.PublicKey = FormatPublicKey(ghost.Keypair.PublicKey)
	carol.Network.importNara(ghostNaraForCarol)
	carol.setObservation("ghost", NaraObservation{Online: "ONLINE"})

	// Ghost knows carol (gossip-only mode)
	ghost.Network.testHTTPClient = sharedClient
	ghost.Network.testMeshURLs = map[types.NaraName]string{types.NaraName("carol"): carolServer.URL}
	carolNaraForGhost := NewNara("carol")
	carolNaraForGhost.Status.PublicKey = FormatPublicKey(carol.Keypair.PublicKey)
	ghost.Network.importNara(carolNaraForGhost)
	ghost.setObservation("carol", NaraObservation{Online: "ONLINE"})

	// Alice doesn't know ghost
	if alice.Network.Neighbourhood["ghost"] != nil {
		t.Fatal("Alice should not know ghost initially")
	}

	// Ghost's startup emits hey_there sync event
	ghost.Network.InitGossipIdentity()

	// Simulate gossip rounds: ghost -> carol -> bob -> alice
	ghost.Network.performGossipRound()
	time.Sleep(100 * time.Millisecond)

	carol.Network.performGossipRound()
	time.Sleep(100 * time.Millisecond)

	bob.Network.performGossipRound()
	time.Sleep(100 * time.Millisecond)

	// All naras should now know about ghost
	for _, test := range []struct {
		name string
		nara *LocalNara
	}{
		{"alice", alice},
		{"bob", bob},
		{"carol", carol},
	} {
		if test.nara.Network.Neighbourhood["ghost"] == nil {
			t.Errorf("%s should know about ghost", test.name)
		}
		if test.nara.Network.getPublicKeyForNara("ghost") == nil {
			t.Errorf("%s should know ghost's public key", test.name)
		}
	}
}

// TestIntegration_ChauSyncEventPropagation verifies that chau (graceful shutdown) events
// propagate through gossip and mark naras as OFFLINE (not MISSING).
func TestIntegration_ChauSyncEventPropagation(t *testing.T) {
	logrus.SetLevel(logrus.DebugLevel)

	// Setup: alice and bob in full mesh with gossip
	mesh := testCreateMeshNetwork(t, []string{"alice", "bob"}, 50, 1000)
	alice := mesh.GetByName("alice")
	bob := mesh.GetByName("bob")

	// Update observations with LastSeen (needed for this test)
	alice.setObservation("bob", NaraObservation{Online: "ONLINE", LastSeen: time.Now().Unix()})
	bob.setObservation("alice", NaraObservation{Online: "ONLINE", LastSeen: time.Now().Unix()})

	// Verify bob is ONLINE from alice's perspective
	obs := alice.getObservation("bob")
	if obs.Online != "ONLINE" {
		t.Fatalf("Bob should be ONLINE initially, got %s", obs.Online)
	}

	// Bob gracefully shuts down (emits chau sync event)
	bob.Network.emitChauSyncEvent()

	// Bob sends zine to alice (with the chau event)
	bob.Network.performGossipRound()
	time.Sleep(100 * time.Millisecond)

	// Alice should now see bob as OFFLINE (not MISSING)
	obs = alice.getObservation("bob")
	if obs.Online != "OFFLINE" {
		t.Errorf("Bob should be OFFLINE after chau, got %s", obs.Online)
	}

	t.Logf("✅ Chau sync event propagation working:")
	t.Logf("   - Bob emitted chau sync event")
	t.Logf("   - Alice received it via gossip")
	t.Logf("   - Alice marked Bob as OFFLINE (graceful shutdown)")
}

// TestIntegration_ZineVerificationTriggersResolution verifies that receiving a zine
// from an unknown nara triggers active resolution of their public key.
func TestIntegration_ZineVerificationTriggersResolution(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	// Setup: alice <-> bob <-> carol
	alice := testLocalNaraWithSoulAndParams(t, "alice", testPeerDiscoverySoul("alice-zv"), 50, 1000)
	alice.Network.TransportMode = TransportGossip

	bob := testLocalNaraWithSoulAndParams(t, "bob", testPeerDiscoverySoul("bob-zv"), 50, 1000)
	bob.Network.TransportMode = TransportGossip

	carol := testLocalNaraWithSoulAndParams(t, "carol", testPeerDiscoverySoul("carol-zv"), 50, 1000)
	carol.Network.TransportMode = TransportGossip

	// Set up test servers
	aliceMux := http.NewServeMux()
	aliceMux.HandleFunc("/gossip/zine", alice.Network.httpGossipZineHandler)
	aliceMux.HandleFunc("/peer/query", alice.Network.httpPeerQueryHandler)
	aliceServer := httptest.NewServer(aliceMux)
	defer aliceServer.Close()

	bobMux := http.NewServeMux()
	bobMux.HandleFunc("/gossip/zine", bob.Network.httpGossipZineHandler)
	bobMux.HandleFunc("/peer/query", bob.Network.httpPeerQueryHandler)
	bobServer := httptest.NewServer(bobMux)
	defer bobServer.Close()

	carolMux := http.NewServeMux()
	carolMux.HandleFunc("/gossip/zine", carol.Network.httpGossipZineHandler)
	carolMux.HandleFunc("/peer/query", carol.Network.httpPeerQueryHandler)
	carolServer := httptest.NewServer(carolMux)
	defer carolServer.Close()

	sharedClient := &http.Client{Timeout: 5 * time.Second}

	// Alice knows bob (full identity)
	alice.Network.testHTTPClient = sharedClient
	alice.Network.testMeshURLs = map[types.NaraName]string{
		types.NaraName("bob"):   bobServer.URL,
		types.NaraName("carol"): carolServer.URL,
	}
	bobNara := NewNara("bob")
	bobNara.Status.PublicKey = FormatPublicKey(bob.Keypair.PublicKey)
	alice.Network.importNara(bobNara)
	alice.setObservation("bob", NaraObservation{Online: "ONLINE"})

	// Bob knows carol (full identity)
	bob.Network.testHTTPClient = sharedClient
	bob.Network.testMeshURLs = map[types.NaraName]string{
		types.NaraName("carol"): carolServer.URL,
	}
	carolNaraForBob := NewNara("carol")
	carolNaraForBob.Status.PublicKey = FormatPublicKey(carol.Keypair.PublicKey)
	bob.Network.importNara(carolNaraForBob)
	bob.setObservation("carol", NaraObservation{Online: "ONLINE"})

	// Alice DOES NOT know carol's public key
	if alice.Network.getPublicKeyForNara("carol") != nil {
		t.Fatal("Alice should not know carol's public key initially")
	}

	// Carol sends a signed zine to Alice
	zine := Zine{
		From:      "carol",
		CreatedAt: time.Now().Unix(),
		Events:    make([]SyncEvent, 0),
	}
	sig, err := SignZine(&zine, carol.Keypair)
	if err != nil {
		t.Fatalf("Failed to sign zine: %v", err)
	}
	zine.Signature = sig

	// Send it via HTTP to Alice's handler
	zineBytes, _ := json.Marshal(zine)
	req, _ := http.NewRequest("POST", aliceServer.URL+"/gossip/zine", bytes.NewBuffer(zineBytes))
	req.Header.Set("Content-Type", "application/json")
	resp, err := sharedClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to send zine to Alice: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status OK, got %d", resp.StatusCode)
	}

	// VERIFICATION: Alice should now know carol's public key
	// because httpGossipZineHandler called resolvePublicKeyForNara("carol")
	// which called resolvePeer("carol") which queried bob!
	if alice.Network.getPublicKeyForNara("carol") == nil {
		t.Error("Alice should have resolved carol's public key after receiving her zine")
	}
}

// TestIntegration_WorldJourneyTriggersResolution verifies that receiving a world journey
// from an unknown nara triggers active resolution of their public key for verification.
func TestIntegration_WorldJourneyTriggersResolution(t *testing.T) {
	logrus.SetLevel(logrus.DebugLevel)

	// Setup: alice <-> bob <-> carol
	alice := testLocalNaraWithSoulAndParams(t, "alice", testPeerDiscoverySoul("alice-wj"), 50, 1000)
	bob := testLocalNaraWithSoulAndParams(t, "bob", testPeerDiscoverySoul("bob-wj"), 50, 1000)
	carol := testLocalNaraWithSoulAndParams(t, "carol", testPeerDiscoverySoul("carol-wj"), 50, 1000)

	// Set up test servers
	aliceMux := http.NewServeMux()
	aliceMux.HandleFunc("/peer/query", alice.Network.httpPeerQueryHandler)
	aliceMux.HandleFunc("/world/relay", alice.Network.httpWorldRelayHandler)
	aliceServer := httptest.NewServer(aliceMux)
	defer aliceServer.Close()

	bobMux := http.NewServeMux()
	bobMux.HandleFunc("/peer/query", bob.Network.httpPeerQueryHandler)
	bobServer := httptest.NewServer(bobMux)
	defer bobServer.Close()

	carolMux := http.NewServeMux()
	carolServer := httptest.NewServer(carolMux)
	defer carolServer.Close()

	sharedClient := &http.Client{Timeout: 5 * time.Second}

	// Alice knows bob
	alice.Network.testHTTPClient = sharedClient
	alice.Network.testMeshURLs = map[types.NaraName]string{
		types.NaraName("bob"): bobServer.URL,
	}
	bobNara := NewNara("bob")
	bobNara.Status.PublicKey = FormatPublicKey(bob.Keypair.PublicKey)
	alice.Network.importNara(bobNara)
	alice.setObservation("bob", NaraObservation{Online: "ONLINE"})

	// Bob knows carol
	bob.Network.testHTTPClient = sharedClient
	bob.Network.testMeshURLs = map[types.NaraName]string{
		types.NaraName("carol"): carolServer.URL,
	}
	carolNaraForBob := NewNara("carol")
	carolNaraForBob.Status.PublicKey = FormatPublicKey(carol.Keypair.PublicKey)
	bob.Network.importNara(carolNaraForBob)
	bob.setObservation("carol", NaraObservation{Online: "ONLINE"})

	// Set up mock network for world journey
	mockNet := NewMockMeshNetwork()
	aliceTransport := NewMockMeshTransport()
	bobTransport := NewMockMeshTransport()

	mockNet.Register("alice", aliceTransport)
	mockNet.Register("bob", bobTransport)

	alice.Network.InitWorldJourney(aliceTransport)
	bob.Network.InitWorldJourney(bobTransport)

	// We need bob to be reachable for peer queries
	// Alice's resolvePeer will use testHTTPClient (sharedClient) and testMeshURLs
	alice.Network.testHTTPClient = sharedClient
	alice.Network.testMeshURLs = map[types.NaraName]string{
		types.NaraName("bob"): bobServer.URL,
	}

	// Bob needs to know carol to answer the query
	carolNara := NewNara("carol")
	carolNara.Status.PublicKey = FormatPublicKey(carol.Keypair.PublicKey)
	bob.Network.importNara(carolNara)

	// Alice DOES NOT know carol
	if alice.Network.getPublicKeyForNara("carol") != nil {
		t.Fatal("Alice should not know carol's public key initially")
	}

	// Carol starts a journey
	wm := NewWorldMessage("hello from carol", "carol")
	if err := wm.AddHop("carol", carol.Keypair, "🌸"); err != nil {
		t.Fatalf("Failed to add hop: %v", err)
	}

	// Carol sends it to Alice (directly via relay handler for simplicity)
	wmBytes, _ := json.Marshal(wm)
	req, _ := http.NewRequest("POST", aliceServer.URL+"/world/relay", bytes.NewBuffer(wmBytes))
	req.Header.Set("Content-Type", "application/json")

	// We need to bypass meshAuthMiddleware for this test or set up auth headers
	// For now, let's just use the internal HandleIncoming directly to test the resolver integration
	err := alice.Network.worldHandler.HandleIncoming(wm)
	if err != nil {
		t.Fatalf("HandleIncoming failed: %v", err)
	}

	// VERIFICATION: Alice should now know carol's public key
	// because HandleIncoming -> VerifySignature -> getPublicKey -> resolvePublicKeyForNara
	if alice.Network.getPublicKeyForNara("carol") == nil {
		t.Logf("Neighbourhood names: %v", alice.Network.NeighbourhoodNames())
		t.Logf("Online names: %v", alice.Network.NeighbourhoodOnlineNames())
		t.Error("Alice should have resolved carol's public key after receiving her world journey")
	}
}

// TestIntegration_MeshAuthTriggersResolution verifies that mesh authentication
// triggers resolution for unknown senders.
func TestIntegration_MeshAuthTriggersResolution(t *testing.T) {
	logrus.SetLevel(logrus.DebugLevel)

	// Setup: alice <-> bob <-> carol
	alice := testLocalNaraWithSoulAndParams(t, "alice", testPeerDiscoverySoul("alice-ma"), 50, 1000)
	bob := testLocalNaraWithSoulAndParams(t, "bob", testPeerDiscoverySoul("bob-ma"), 50, 1000)
	carol := testLocalNaraWithSoulAndParams(t, "carol", testPeerDiscoverySoul("carol-ma"), 50, 1000)

	// Set up test servers
	// Alice's server has meshAuthMiddleware
	aliceMux := http.NewServeMux()
	aliceMux.HandleFunc("/coordinates", alice.Network.meshAuthMiddleware("/coordinates", alice.Network.httpCoordinatesHandler))
	aliceMux.HandleFunc("/peer/query", alice.Network.httpPeerQueryHandler)
	aliceServer := httptest.NewServer(aliceMux)
	defer aliceServer.Close()

	bobMux := http.NewServeMux()
	bobMux.HandleFunc("/peer/query", bob.Network.httpPeerQueryHandler)
	bobServer := httptest.NewServer(bobMux)
	defer bobServer.Close()

	sharedClient := &http.Client{Timeout: 5 * time.Second}

	// Alice knows bob
	alice.Network.testHTTPClient = sharedClient
	alice.Network.testMeshURLs = map[types.NaraName]string{
		types.NaraName("bob"): bobServer.URL,
	}
	bobNara := NewNara("bob")
	bobNara.Status.PublicKey = FormatPublicKey(bob.Keypair.PublicKey)
	alice.Network.importNara(bobNara)
	alice.setObservation("bob", NaraObservation{Online: "ONLINE"})

	// Bob knows carol
	carolNaraForBob := NewNara("carol")
	carolNaraForBob.Status.PublicKey = FormatPublicKey(carol.Keypair.PublicKey)
	bob.Network.importNara(carolNaraForBob)
	bob.setObservation("carol", NaraObservation{Online: "ONLINE"})

	// Bob also needs to know Alice so his HandleIncoming doesn't log errors
	aliceNaraForBob := NewNara("alice")
	aliceNaraForBob.Status.PublicKey = FormatPublicKey(alice.Keypair.PublicKey)
	bob.Network.importNara(aliceNaraForBob)

	// Bob needs to be ONLINE for Alice to query him
	alice.setObservation("bob", NaraObservation{Online: "ONLINE"})

	// Alice DOES NOT know carol
	if alice.Network.getPublicKeyForNara("carol") != nil {
		t.Fatal("Alice should not know carol's public key initially")
	}

	// Carol sends a Mesh-Authenticated request to Alice
	req, _ := http.NewRequest("GET", aliceServer.URL+"/coordinates", nil)

	// Manual Mesh Auth headers for Carol
	req.Header.Set("X-Nara-Name", "carol")
	timestamp := fmt.Sprintf("%d", time.Now().UnixMilli()) // MeshAuth uses UnixMilli
	req.Header.Set("X-Nara-Timestamp", timestamp)

	// Signature of "name + timestamp + method + path"
	msg := fmt.Sprintf("carol%sGET/coordinates", timestamp)
	sig := carol.Keypair.SignBase64([]byte(msg))
	req.Header.Set("X-Nara-Signature", sig)

	resp, err := sharedClient.Do(req)
	if err == nil {
		defer resp.Body.Close()
	}

	if err != nil {
		t.Fatalf("Failed to send mesh request to Alice: %v", err)
	}

	// VERIFICATION: Alice should now know carol's public key
	// because meshAuthMiddleware called resolvePublicKeyForNara("carol")
	if alice.Network.getPublicKeyForNara("carol") == nil {
		t.Error("Alice should have resolved carol's public key via mesh auth")
	}
}
