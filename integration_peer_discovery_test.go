package nara

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// testPeerDiscoverySoul creates a unique soul for peer discovery tests
func testPeerDiscoverySoul(name string) string {
	hw := hashBytes([]byte("peer-discovery-test-" + name))
	soul := NativeSoulCustom(hw, name)
	return FormatSoul(soul)
}

// TestIntegration_DiscoverNarasFromEventStream verifies that naras mentioned in
// events are automatically added to the Neighbourhood, even without their public key.
func TestIntegration_DiscoverNarasFromEventStream(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	// Setup: alice and bob, connected via test servers
	alice := NewLocalNara("alice", testPeerDiscoverySoul("alice"), "", "", "", 50, 1000)
	alice.Network.TransportMode = TransportGossip

	bob := NewLocalNara("bob", testPeerDiscoverySoul("bob"), "", "", "", 50, 1000)
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

	// Set up test hooks and neighborhood
	sharedClient := &http.Client{Timeout: 5 * time.Second}
	alice.Network.testHTTPClient = sharedClient
	alice.Network.testMeshURLs = map[string]string{"bob": bobServer.URL}
	bob.Network.testHTTPClient = sharedClient
	bob.Network.testMeshURLs = map[string]string{"alice": aliceServer.URL}

	// Alice and Bob know each other (with public keys)
	aliceNara := NewNara("alice")
	aliceNara.Status.PublicKey = FormatPublicKey(alice.Keypair.PublicKey)
	bob.Network.importNara(aliceNara)

	bobNara := NewNara("bob")
	bobNara.Status.PublicKey = FormatPublicKey(bob.Keypair.PublicKey)
	alice.Network.importNara(bobNara)
	alice.setObservation("bob", NaraObservation{Online: "ONLINE"})

	// Bob doesn't know "ghost" yet
	if bob.Network.Neighbourhood["ghost"] != nil {
		t.Fatal("Bob should not know ghost initially")
	}

	// Alice creates an event mentioning "ghost" as the target
	event := NewTeaseEvent("alice", "ghost", "spooky behavior")
	event.Sign(alice.Keypair)
	alice.SyncLedger.AddSocialEvent(event)

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
	alice := NewLocalNara("alice", testPeerDiscoverySoul("alice-ht"), "", "", "", 50, 1000)
	alice.Network.TransportMode = TransportGossip

	bob := NewLocalNara("bob", testPeerDiscoverySoul("bob-ht"), "", "", "", 50, 1000)
	bob.Network.TransportMode = TransportGossip

	carol := NewLocalNara("carol", testPeerDiscoverySoul("carol-ht"), "", "", "", 50, 1000)
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
	alice.Network.testMeshURLs = map[string]string{"bob": bobServer.URL}
	bobNara := NewNara("bob")
	bobNara.Status.PublicKey = FormatPublicKey(bob.Keypair.PublicKey)
	alice.Network.importNara(bobNara)
	alice.setObservation("bob", NaraObservation{Online: "ONLINE"})

	// Bob knows alice and carol
	bob.Network.testHTTPClient = sharedClient
	bob.Network.testMeshURLs = map[string]string{
		"alice": aliceServer.URL,
		"carol": carolServer.URL,
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
	carol.Network.testMeshURLs = map[string]string{"bob": bobServer.URL}
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
	alice := NewLocalNara("alice", testPeerDiscoverySoul("alice-pr"), "", "", "", 50, 1000)
	alice.Network.TransportMode = TransportGossip

	bob := NewLocalNara("bob", testPeerDiscoverySoul("bob-pr"), "", "", "", 50, 1000)
	bob.Network.TransportMode = TransportGossip

	carol := NewLocalNara("carol", testPeerDiscoverySoul("carol-pr"), "", "", "", 50, 1000)
	carol.Network.TransportMode = TransportGossip

	ghost := NewLocalNara("ghost", testPeerDiscoverySoul("ghost-pr"), "", "", "", 50, 1000)
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
	alice.Network.testMeshURLs = map[string]string{
		"bob":   bobServer.URL,
		"carol": carolServer.URL, // URL only, no identity - needed for redirect following
	}
	bobNara := NewNara("bob")
	bobNara.Status.PublicKey = FormatPublicKey(bob.Keypair.PublicKey)
	alice.Network.importNara(bobNara)
	alice.setObservation("bob", NaraObservation{Online: "ONLINE"})

	// Bob knows alice and carol
	bob.Network.testHTTPClient = sharedClient
	bob.Network.testMeshURLs = map[string]string{
		"alice": aliceServer.URL,
		"carol": carolServer.URL,
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
	carol.Network.testMeshURLs = map[string]string{
		"bob":   bobServer.URL,
		"alice": aliceServer.URL, // For responding directly to alice
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
	alice := NewLocalNara("alice", testPeerDiscoverySoul("alice-auto"), "", "", "", 50, 1000)
	alice.Network.TransportMode = TransportGossip

	bob := NewLocalNara("bob", testPeerDiscoverySoul("bob-auto"), "", "", "", 50, 1000)
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
	alice.Network.testMeshURLs = map[string]string{"bob": bobServer.URL}
	bobNaraForAlice := NewNara("bob")
	// NO public key set - alice doesn't know bob's identity yet
	alice.Network.importNara(bobNaraForAlice)
	alice.setObservation("bob", NaraObservation{Online: "ONLINE"})

	// Bob knows alice's URL but NOT her public key initially
	bob.Network.testHTTPClient = sharedClient
	bob.Network.testMeshURLs = map[string]string{"alice": aliceServer.URL}
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
	alice := NewLocalNara("alice", testPeerDiscoverySoul("alice-go"), "", "", "", 50, 1000)
	alice.Network.TransportMode = TransportHybrid

	bob := NewLocalNara("bob", testPeerDiscoverySoul("bob-go"), "", "", "", 50, 1000)
	bob.Network.TransportMode = TransportHybrid

	carol := NewLocalNara("carol", testPeerDiscoverySoul("carol-go"), "", "", "", 50, 1000)
	carol.Network.TransportMode = TransportHybrid

	ghost := NewLocalNara("ghost", testPeerDiscoverySoul("ghost-go"), "", "", "", 50, 1000)
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
	alice.Network.testMeshURLs = map[string]string{"bob": bobServer.URL}
	bobNara := NewNara("bob")
	bobNara.Status.PublicKey = FormatPublicKey(bob.Keypair.PublicKey)
	alice.Network.importNara(bobNara)
	alice.setObservation("bob", NaraObservation{Online: "ONLINE"})

	// Bob knows alice and carol
	bob.Network.testHTTPClient = sharedClient
	bob.Network.testMeshURLs = map[string]string{
		"alice": aliceServer.URL,
		"carol": carolServer.URL,
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
	carol.Network.testMeshURLs = map[string]string{
		"bob":   bobServer.URL,
		"ghost": ghostServer.URL,
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
	ghost.Network.testMeshURLs = map[string]string{"carol": carolServer.URL}
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

	// Setup: alice and bob, both in gossip mode
	alice := NewLocalNara("alice", testPeerDiscoverySoul("alice-chau"), "", "", "", 50, 1000)
	alice.Network.TransportMode = TransportGossip

	bob := NewLocalNara("bob", testPeerDiscoverySoul("bob-chau"), "", "", "", 50, 1000)
	bob.Network.TransportMode = TransportGossip

	// Set up test servers for gossip
	aliceMux := http.NewServeMux()
	aliceMux.HandleFunc("/gossip/zine", alice.Network.httpGossipZineHandler)
	aliceServer := httptest.NewServer(aliceMux)
	defer aliceServer.Close()

	bobMux := http.NewServeMux()
	bobMux.HandleFunc("/gossip/zine", bob.Network.httpGossipZineHandler)
	bobServer := httptest.NewServer(bobMux)
	defer bobServer.Close()

	sharedClient := &http.Client{Timeout: 5 * time.Second}

	// Alice knows bob and has his public key
	alice.Network.testHTTPClient = sharedClient
	alice.Network.testMeshURLs = map[string]string{"bob": bobServer.URL}
	bobNaraForAlice := NewNara("bob")
	bobNaraForAlice.Status.PublicKey = FormatPublicKey(bob.Keypair.PublicKey)
	alice.Network.importNara(bobNaraForAlice)
	alice.setObservation("bob", NaraObservation{Online: "ONLINE", LastSeen: time.Now().Unix()})

	// Bob knows alice
	bob.Network.testHTTPClient = sharedClient
	bob.Network.testMeshURLs = map[string]string{"alice": aliceServer.URL}
	aliceNaraForBob := NewNara("alice")
	aliceNaraForBob.Status.PublicKey = FormatPublicKey(alice.Keypair.PublicKey)
	bob.Network.importNara(aliceNaraForBob)
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
