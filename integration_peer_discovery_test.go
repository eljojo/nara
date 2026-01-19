package nara

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eljojo/nara/identity"
	"github.com/eljojo/nara/types"
)

// testPeerDiscoverySoul creates a unique soul for peer discovery tests
func testPeerDiscoverySoul(name string) string {
	hw := identity.HashBytes([]byte("peer-discovery-test-" + name))
	soul := identity.NativeSoulCustom(hw, types.NaraName(name))
	return identity.FormatSoul(soul)
}

// TestIntegration_DiscoverNarasFromEventStream verifies that naras mentioned in
// events are automatically added to the Neighbourhood, even without their public key.
func TestIntegration_DiscoverNarasFromEventStream(t *testing.T) {
	t.Parallel()

	// Setup: alice and bob in full mesh with gossip
	mesh := testCreateMeshNetwork(t, []string{"alice", "bob"}, 50, 1000, 0)
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
	t.Parallel()

	tc := NewTestCoordinator(t)
	alice := tc.AddNara("alice", WithHandlers("gossip"))
	bob := tc.AddNara("bob", WithHandlers("gossip"))
	carol := tc.AddNara("carol", WithHandlers("gossip"))

	// Setup chain: alice <-> bob <-> carol
	tc.Connect("alice", "bob")
	tc.Connect("bob", "carol")
	tc.SetOnline("alice", "bob")
	tc.SetOnline("bob", "alice")
	tc.SetOnline("bob", "carol")
	tc.SetOnline("carol", "bob")

	// Set transport mode to gossip
	alice.Network.TransportMode = TransportGossip
	bob.Network.TransportMode = TransportGossip
	carol.Network.TransportMode = TransportGossip

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
	t.Parallel()

	// Setup: alice <-> bob <-> carol, carol knows ghost's identity
	alice := testLocalNaraWithParams(t, "alice", 50, 1000)
	alice.Network.TransportMode = TransportGossip

	bob := testLocalNaraWithParams(t, "bob", 50, 1000)
	bob.Network.TransportMode = TransportGossip

	carol := testLocalNaraWithParams(t, "carol", 50, 1000)
	carol.Network.TransportMode = TransportGossip

	ghost := testLocalNaraWithParams(t, "ghost", 50, 1000)
	ghost.Network.TransportMode = TransportGossip

	// Set up test servers using production route registration
	// This ensures tests fail if we forget to register handlers in production
	aliceServer := httptest.NewServer(alice.Network.createHTTPMux(false))
	defer aliceServer.Close()

	bobServer := httptest.NewServer(bob.Network.createHTTPMux(false))
	defer bobServer.Close()

	carolServer := httptest.NewServer(carol.Network.createHTTPMux(false))
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
	bobNara.ID = bob.ID // Set ID so nameToID mapping is created
	bobNara.Status.PublicKey = identity.FormatPublicKey(bob.Keypair.PublicKey)
	alice.Network.importNara(bobNara)
	alice.setObservation("bob", NaraObservation{Online: "ONLINE"})

	// Bob knows alice and carol
	bob.Network.testHTTPClient = sharedClient
	bob.Network.testMeshURLs = map[types.NaraName]string{
		types.NaraName("alice"): aliceServer.URL,
		types.NaraName("carol"): carolServer.URL,
	}
	aliceNara := NewNara("alice")
	aliceNara.Status.PublicKey = identity.FormatPublicKey(alice.Keypair.PublicKey)
	bob.Network.importNara(aliceNara)
	bob.setObservation("alice", NaraObservation{Online: "ONLINE"})
	carolNaraForBob := NewNara("carol")
	carolNaraForBob.ID = carol.ID // Set ID so nameToID mapping is created
	carolNaraForBob.Status.PublicKey = identity.FormatPublicKey(carol.Keypair.PublicKey)
	bob.Network.importNara(carolNaraForBob)
	bob.setObservation("carol", NaraObservation{Online: "ONLINE"})

	// Carol knows bob and ghost (has ghost's public key!)
	carol.Network.testHTTPClient = sharedClient
	carol.Network.testMeshURLs = map[types.NaraName]string{
		types.NaraName("bob"):   bobServer.URL,
		types.NaraName("alice"): aliceServer.URL, // For responding directly to alice
	}
	bobNaraForCarol := NewNara("bob")
	bobNaraForCarol.Status.PublicKey = identity.FormatPublicKey(bob.Keypair.PublicKey)
	carol.Network.importNara(bobNaraForCarol)
	carol.setObservation("bob", NaraObservation{Online: "ONLINE"})
	// Carol knows ghost's identity
	ghostNara := NewNara("ghost")
	ghostNara.Status.PublicKey = identity.FormatPublicKey(ghost.Keypair.PublicKey)
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
	if response.PublicKey != identity.FormatPublicKey(ghost.Keypair.PublicKey) {
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
	t.Parallel()

	tc := NewTestCoordinator(t)

	// Setup: alice and bob, both in gossip mode
	alice := tc.AddNara("alice", WithHandlers("gossip"), NotBooting())
	alice.Network.TransportMode = TransportGossip

	bob := tc.AddNara("bob", WithHandlers("gossip"), NotBooting())
	bob.Network.TransportMode = TransportGossip

	// Connect but WITHOUT importing public keys initially
	// This tests that hey_there events propagate public keys
	tc.connectOneWayWithoutPublicKey("alice", "bob")
	tc.connectOneWayWithoutPublicKey("bob", "alice")

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
	t.Parallel()

	tc := NewTestCoordinator(t)

	// Setup: alice, bob, carol in hybrid mode, ghost in gossip-only mode
	// Network topology: alice <-> bob <-> carol <-> ghost (chain)
	alice := tc.AddNara("alice", WithHandlers("gossip"), NotBooting())
	alice.Network.TransportMode = TransportHybrid

	bob := tc.AddNara("bob", WithHandlers("gossip"), NotBooting())
	bob.Network.TransportMode = TransportHybrid

	carol := tc.AddNara("carol", WithHandlers("gossip"), NotBooting())
	carol.Network.TransportMode = TransportHybrid

	ghost := tc.AddNara("ghost", WithHandlers("gossip"), NotBooting())
	ghost.Network.TransportMode = TransportGossip // Gossip-only!

	// Create chain topology: alice <-> bob <-> carol <-> ghost
	tc.Connect("alice", "bob")
	tc.Connect("bob", "carol")
	tc.Connect("carol", "ghost")

	// Alice doesn't know ghost initially
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
	t.Parallel()

	// Setup: alice and bob in full mesh with gossip
	mesh := testCreateMeshNetwork(t, []string{"alice", "bob"}, 50, 1000, 0)
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
	t.Parallel()

	// Setup: alice <-> bob <-> carol
	alice := testLocalNaraWithSoulAndParams(t, "alice", testPeerDiscoverySoul("alice-zv"), 50, 1000)
	alice.Network.TransportMode = TransportGossip

	bob := testLocalNaraWithSoulAndParams(t, "bob", testPeerDiscoverySoul("bob-zv"), 50, 1000)
	bob.Network.TransportMode = TransportGossip

	carol := testLocalNaraWithSoulAndParams(t, "carol", testPeerDiscoverySoul("carol-zv"), 50, 1000)
	carol.Network.TransportMode = TransportGossip

	// Set up test servers using production route registration
	aliceServer := httptest.NewServer(alice.Network.createHTTPMux(false))
	defer aliceServer.Close()

	bobServer := httptest.NewServer(bob.Network.createHTTPMux(false))
	defer bobServer.Close()

	carolServer := httptest.NewServer(carol.Network.createHTTPMux(false))
	defer carolServer.Close()

	sharedClient := &http.Client{Timeout: 5 * time.Second}

	// Alice knows bob (full identity)
	alice.Network.testHTTPClient = sharedClient
	alice.Network.testMeshURLs = map[types.NaraName]string{
		types.NaraName("bob"):   bobServer.URL,
		types.NaraName("carol"): carolServer.URL,
	}
	bobNara := NewNara("bob")
	bobNara.ID = bob.ID // Set ID so nameToID mapping is created
	bobNara.Status.PublicKey = identity.FormatPublicKey(bob.Keypair.PublicKey)
	alice.Network.importNara(bobNara)
	alice.setObservation("bob", NaraObservation{Online: "ONLINE"})

	// Bob knows carol (full identity)
	bob.Network.testHTTPClient = sharedClient
	bob.Network.testMeshURLs = map[types.NaraName]string{
		types.NaraName("carol"): carolServer.URL,
	}
	carolNaraForBob := NewNara("carol")
	carolNaraForBob.ID = carol.ID // Set ID so nameToID mapping is created
	carolNaraForBob.Status.PublicKey = identity.FormatPublicKey(carol.Keypair.PublicKey)
	bob.Network.importNara(carolNaraForBob)
	bob.setObservation("carol", NaraObservation{Online: "ONLINE"})

	// Set up carol's mesh client to send to alice
	carol.Network.meshClient.UpdateHTTPClient(sharedClient)
	carol.Network.meshClient.RegisterPeer(alice.ID, aliceServer.URL)

	// Alice DOES NOT know carol's public key
	if alice.Network.getPublicKeyForNara("carol") != nil {
		t.Fatal("Alice should not know carol's public key initially")
	}

	// Carol sends a signed zine to Alice using MeshClient
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

	// Send via MeshClient (handles authentication)
	ctx := context.Background()
	_, err = carol.Network.meshClient.PostGossipZine(ctx, alice.ID, zine)
	if err != nil {
		t.Fatalf("Failed to send zine to Alice: %v", err)
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
	t.Parallel()

	// Setup: alice <-> bob <-> carol
	alice := testLocalNaraWithSoulAndParams(t, "alice", testPeerDiscoverySoul("alice-wj"), 50, 1000)
	bob := testLocalNaraWithSoulAndParams(t, "bob", testPeerDiscoverySoul("bob-wj"), 50, 1000)
	carol := testLocalNaraWithSoulAndParams(t, "carol", testPeerDiscoverySoul("carol-wj"), 50, 1000)

	// Set up test servers using production route registration
	aliceServer := httptest.NewServer(alice.Network.createHTTPMux(false))
	defer aliceServer.Close()

	bobServer := httptest.NewServer(bob.Network.createHTTPMux(false))
	defer bobServer.Close()

	carolServer := httptest.NewServer(carol.Network.createHTTPMux(false))
	defer carolServer.Close()

	sharedClient := &http.Client{Timeout: 5 * time.Second}

	// Alice knows bob
	alice.Network.testHTTPClient = sharedClient
	alice.Network.testMeshURLs = map[types.NaraName]string{
		types.NaraName("bob"): bobServer.URL,
	}
	bobNara := NewNara("bob")
	bobNara.ID = bob.ID // Set ID so nameToID mapping is created
	bobNara.Status.PublicKey = identity.FormatPublicKey(bob.Keypair.PublicKey)
	alice.Network.importNara(bobNara)
	alice.setObservation("bob", NaraObservation{Online: "ONLINE"})

	// Bob knows alice and carol
	bob.Network.testHTTPClient = sharedClient
	bob.Network.testMeshURLs = map[types.NaraName]string{
		types.NaraName("alice"): aliceServer.URL,
		types.NaraName("carol"): carolServer.URL,
	}
	aliceNaraForBob := NewNara("alice")
	aliceNaraForBob.ID = alice.ID // Set ID so nameToID mapping is created
	aliceNaraForBob.Status.PublicKey = identity.FormatPublicKey(alice.Keypair.PublicKey)
	bob.Network.importNara(aliceNaraForBob)
	bob.setObservation("alice", NaraObservation{Online: "ONLINE"})

	carolNaraForBob := NewNara("carol")
	carolNaraForBob.ID = carol.ID // Set ID so nameToID mapping is created
	carolNaraForBob.Status.PublicKey = identity.FormatPublicKey(carol.Keypair.PublicKey)
	bob.Network.importNara(carolNaraForBob)
	bob.setObservation("carol", NaraObservation{Online: "ONLINE"})

	// Carol knows alice and bob
	carol.Network.testHTTPClient = sharedClient
	carol.Network.testMeshURLs = map[types.NaraName]string{
		types.NaraName("alice"): aliceServer.URL,
		types.NaraName("bob"):   bobServer.URL,
	}
	aliceNaraForCarol := NewNara("alice")
	aliceNaraForCarol.ID = alice.ID // Set ID so nameToID mapping is created
	aliceNaraForCarol.Status.PublicKey = identity.FormatPublicKey(alice.Keypair.PublicKey)
	carol.Network.importNara(aliceNaraForCarol)

	bobNaraForCarol := NewNara("bob")
	bobNaraForCarol.ID = bob.ID // Set ID so nameToID mapping is created
	bobNaraForCarol.Status.PublicKey = identity.FormatPublicKey(bob.Keypair.PublicKey)
	carol.Network.importNara(bobNaraForCarol)

	// Initialize world journey for alice, bob, and carol
	// (meshClient is already set up in NewNetwork)
	alice.Network.InitWorldJourney()
	bob.Network.InitWorldJourney()
	carol.Network.InitWorldJourney()

	// Configure alice's meshClient to route to bob by ID
	alice.Network.meshClient.EnableTestMode(map[types.NaraID]string{
		bob.ID: bobServer.URL,
	})

	// Configure bob's meshClient to route to carol by ID
	bob.Network.meshClient.EnableTestMode(map[types.NaraID]string{
		carol.ID: carolServer.URL,
	})

	// We need bob to be reachable for peer queries
	// Alice's resolvePeer will use testHTTPClient (sharedClient) and testMeshURLs
	alice.Network.testHTTPClient = sharedClient
	alice.Network.testMeshURLs = map[types.NaraName]string{
		types.NaraName("bob"): bobServer.URL,
	}

	// Bob needs to know carol to answer the query
	carolNara := NewNara("carol")
	carolNara.ID = carol.ID // Set ID so nameToID mapping is created
	carolNara.Status.PublicKey = identity.FormatPublicKey(carol.Keypair.PublicKey)
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
	t.Parallel()

	// Setup: alice <-> bob <-> carol
	alice := testLocalNaraWithSoulAndParams(t, "alice", testPeerDiscoverySoul("alice-ma"), 50, 1000)
	bob := testLocalNaraWithSoulAndParams(t, "bob", testPeerDiscoverySoul("bob-ma"), 50, 1000)
	carol := testLocalNaraWithSoulAndParams(t, "carol", testPeerDiscoverySoul("carol-ma"), 50, 1000)

	// Set up test servers using production route registration
	// Production mux includes meshAuthMiddleware on /coordinates
	aliceServer := httptest.NewServer(alice.Network.createHTTPMux(false))
	defer aliceServer.Close()

	bobServer := httptest.NewServer(bob.Network.createHTTPMux(false))
	defer bobServer.Close()

	sharedClient := &http.Client{Timeout: 5 * time.Second}

	// Alice knows bob
	alice.Network.testHTTPClient = sharedClient
	alice.Network.testMeshURLs = map[types.NaraName]string{
		types.NaraName("bob"): bobServer.URL,
	}
	bobNara := NewNara("bob")
	bobNara.ID = bob.ID // Set ID so nameToID mapping is created
	bobNara.Status.PublicKey = identity.FormatPublicKey(bob.Keypair.PublicKey)
	alice.Network.importNara(bobNara)
	alice.setObservation("bob", NaraObservation{Online: "ONLINE"})

	// Bob knows carol
	carolNaraForBob := NewNara("carol")
	carolNaraForBob.ID = carol.ID // Set ID so nameToID mapping is created
	carolNaraForBob.Status.PublicKey = identity.FormatPublicKey(carol.Keypair.PublicKey)
	bob.Network.importNara(carolNaraForBob)
	bob.setObservation("carol", NaraObservation{Online: "ONLINE"})

	// Bob also needs to know Alice so his HandleIncoming doesn't log errors
	aliceNaraForBob := NewNara("alice")
	aliceNaraForBob.ID = alice.ID
	aliceNaraForBob.Status.PublicKey = identity.FormatPublicKey(alice.Keypair.PublicKey)
	bob.Network.importNara(aliceNaraForBob)

	// Bob needs to be ONLINE for Alice to query him
	alice.setObservation("bob", NaraObservation{Online: "ONLINE"})

	// Set up carol's mesh client to send to alice
	carol.Network.meshClient.UpdateHTTPClient(sharedClient)
	carol.Network.meshClient.RegisterPeer(alice.ID, aliceServer.URL)

	// Alice DOES NOT know carol
	if alice.Network.getPublicKeyForNara("carol") != nil {
		t.Fatal("Alice should not know carol's public key initially")
	}

	// Carol sends a Mesh-Authenticated request to Alice via SendDM
	// This triggers meshAuthMiddleware which should resolve carol's public key
	// Note: The DM may fail validation (400) but auth happens first, triggering resolution
	ctx := context.Background()
	testEvent := SyncEvent{
		Service:   "test",
		Timestamp: time.Now().UnixNano(),
		Emitter:   "carol",
		EmitterID: carol.ID,
	}
	_ = carol.Network.meshClient.SendDM(ctx, alice.ID, testEvent)
	// Ignore error - we're testing auth middleware resolution, not DM handling

	// VERIFICATION: Alice should now know carol's public key
	// because meshAuthMiddleware called resolvePublicKeyForNara("carol")
	if alice.Network.getPublicKeyForNara("carol") == nil {
		t.Error("Alice should have resolved carol's public key via mesh auth")
	}
}
