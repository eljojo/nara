package nara

import (
	"testing"

	"github.com/eljojo/nara/types"
)

// Test hardware fingerprints for deterministic testing
var (
	worldHW1 = hashBytes([]byte("world-test-hw-1"))
	worldHW2 = hashBytes([]byte("world-test-hw-2"))
	worldHW3 = hashBytes([]byte("world-test-hw-3"))
	worldHW4 = hashBytes([]byte("world-test-hw-4"))
)

func TestDeriveKeypair(t *testing.T) {
	// Create a soul and derive a keypair
	soul := NativeSoulCustom(worldHW1, "alice")
	keypair := DeriveKeypair(soul)

	// Keypair should have valid keys
	if keypair.PrivateKey == nil {
		t.Error("Expected non-nil private key")
	}
	if keypair.PublicKey == nil {
		t.Error("Expected non-nil public key")
	}

	// Public key should be 32 bytes (Ed25519)
	if len(keypair.PublicKey) != 32 {
		t.Errorf("Expected 32-byte public key, got %d", len(keypair.PublicKey))
	}
}

func TestKeypairDeterminism(t *testing.T) {
	// Same soul should always produce same keypair
	soul := NativeSoulCustom(worldHW1, "alice")

	keypair1 := DeriveKeypair(soul)
	keypair2 := DeriveKeypair(soul)

	if FormatPublicKey(keypair1.PublicKey) != FormatPublicKey(keypair2.PublicKey) {
		t.Error("Same soul should produce same keypair")
	}
}

func TestKeypairSignVerify(t *testing.T) {
	soul := NativeSoulCustom(worldHW1, "alice")
	keypair := DeriveKeypair(soul)

	message := []byte("hello world")
	signature := keypair.Sign(message)

	// Should verify with correct public key
	if !VerifySignature(keypair.PublicKey, message, signature) {
		t.Error("Signature should verify with correct key")
	}

	// Should not verify with wrong message
	if VerifySignature(keypair.PublicKey, []byte("wrong message"), signature) {
		t.Error("Signature should not verify with wrong message")
	}

	// Should not verify with wrong public key
	otherSoul := NativeSoulCustom(worldHW2, "bob")
	otherKeypair := DeriveKeypair(otherSoul)
	if VerifySignature(otherKeypair.PublicKey, message, signature) {
		t.Error("Signature should not verify with wrong key")
	}
}

func TestPublicKeyFormatParse(t *testing.T) {
	soul := NativeSoulCustom(worldHW1, "alice")
	keypair := DeriveKeypair(soul)

	// Format and parse should round-trip
	formatted := FormatPublicKey(keypair.PublicKey)
	parsed, err := ParsePublicKey(formatted)
	if err != nil {
		t.Fatalf("Failed to parse public key: %v", err)
	}

	if FormatPublicKey(parsed) != formatted {
		t.Error("Public key should round-trip through format/parse")
	}
}

func TestWorldMessage_Creation(t *testing.T) {
	wm := NewWorldMessage("Hello from Alice!", "alice")

	if wm.ID == "" {
		t.Error("WorldMessage should have an ID")
	}
	if wm.OriginalMessage != "Hello from Alice!" {
		t.Error("OriginalMessage should match")
	}
	if wm.Originator != "alice" {
		t.Error("Originator should be alice")
	}
	if len(wm.Hops) != 0 {
		t.Error("New message should have no hops")
	}
}

func TestWorldMessage_AddHop(t *testing.T) {
	wm := NewWorldMessage("Hello!", "alice")

	// Bob receives and adds a hop
	bobSoul := NativeSoulCustom(worldHW2, "bob")
	bobKeypair := DeriveKeypair(bobSoul)

	err := wm.AddHop("bob", bobKeypair, "🌟")
	if err != nil {
		t.Fatalf("AddHop failed: %v", err)
	}

	if len(wm.Hops) != 1 {
		t.Errorf("Expected 1 hop, got %d", len(wm.Hops))
	}
	if wm.Hops[0].Nara != "bob" {
		t.Error("Hop nara should be bob")
	}
	if wm.Hops[0].Stamp != "🌟" {
		t.Error("Hop stamp should be 🌟")
	}
	if wm.Hops[0].Signature == "" {
		t.Error("Hop should have signature")
	}
	if wm.Hops[0].Timestamp == 0 {
		t.Error("Hop should have timestamp")
	}
}

func TestWorldMessage_HasVisited(t *testing.T) {
	wm := NewWorldMessage("Hello!", "alice")

	// Initially no one has visited
	if wm.HasVisited("bob") {
		t.Error("Bob should not have visited yet")
	}

	// Add Bob's hop
	bobSoul := NativeSoulCustom(worldHW2, "bob")
	bobKeypair := DeriveKeypair(bobSoul)
	if err := wm.AddHop("bob", bobKeypair, "🌟"); err != nil {
		t.Fatalf("Failed to add hop: %v", err)
	}

	// Now Bob has visited
	if !wm.HasVisited("bob") {
		t.Error("Bob should have visited")
	}
	if wm.HasVisited("carol") {
		t.Error("Carol should not have visited")
	}
}

func TestWorldMessage_VerifyChain(t *testing.T) {
	wm := NewWorldMessage("Hello!", "alice")

	// Create keypairs
	bobSoul := NativeSoulCustom(worldHW2, "bob")
	bobKeypair := DeriveKeypair(bobSoul)
	carolSoul := NativeSoulCustom(worldHW3, "carol")
	carolKeypair := DeriveKeypair(carolSoul)

	// Build the chain
	if err := wm.AddHop("bob", bobKeypair, "🌟"); err != nil {
		t.Fatalf("Failed to add bob's hop: %v", err)
	}
	if err := wm.AddHop("carol", carolKeypair, "🎉"); err != nil {
		t.Fatalf("Failed to add carol's hop: %v", err)
	}

	// Create a public key lookup function
	getPublicKey := func(name types.NaraName) []byte {
		switch name {
		case "bob":
			return bobKeypair.PublicKey
		case "carol":
			return carolKeypair.PublicKey
		default:
			return nil
		}
	}

	// Chain should verify
	err := wm.VerifyChain(getPublicKey)
	if err != nil {
		t.Errorf("Chain should verify: %v", err)
	}
}

func TestWorldMessage_VerifyChain_TamperedMessage(t *testing.T) {
	wm := NewWorldMessage("Hello!", "alice")

	bobSoul := NativeSoulCustom(worldHW2, "bob")
	bobKeypair := DeriveKeypair(bobSoul)
	if err := wm.AddHop("bob", bobKeypair, "🌟"); err != nil {
		t.Fatalf("Failed to add hop: %v", err)
	}

	// Tamper with the message
	wm.OriginalMessage = "Tampered!"

	getPublicKey := func(name types.NaraName) []byte {
		if name == "bob" {
			return bobKeypair.PublicKey
		}
		return nil
	}

	// Chain should NOT verify
	err := wm.VerifyChain(getPublicKey)
	if err == nil {
		t.Error("Tampered message should fail verification")
	}
}

func TestWorldMessage_IsComplete(t *testing.T) {
	wm := NewWorldMessage("Hello!", "alice")

	// Not complete initially
	if wm.IsComplete() {
		t.Error("New message should not be complete")
	}

	// Add some hops
	bobSoul := NativeSoulCustom(worldHW2, "bob")
	bobKeypair := DeriveKeypair(bobSoul)
	if err := wm.AddHop("bob", bobKeypair, "🌟"); err != nil {
		t.Fatalf("Failed to add hop: %v", err)
	}

	// Still not complete (hasn't returned to alice)
	if wm.IsComplete() {
		t.Error("Message should not be complete without returning to originator")
	}

	// Add alice's final hop
	aliceSoul := NativeSoulCustom(worldHW1, "alice")
	aliceKeypair := DeriveKeypair(aliceSoul)
	if err := wm.AddHop("alice", aliceKeypair, "🏠"); err != nil {
		t.Fatalf("Failed to add hop: %v", err)
	}

	// Now complete
	if !wm.IsComplete() {
		t.Error("Message should be complete after returning to originator")
	}
}

func TestWorldJourney_CloutRouting(t *testing.T) {
	// Create mock naras with clout relationships
	// Alice likes: Bob (10), Carol (5), Dave (2)
	// Should route Alice -> Bob -> Carol -> Dave -> Alice

	clout := map[string]map[types.NaraName]float64{
		"alice": {types.NaraName("bob"): 10, types.NaraName("carol"): 5, types.NaraName("dave"): 2},
		"bob":   {types.NaraName("carol"): 8, types.NaraName("dave"): 3, types.NaraName("alice"): 5},
		"carol": {types.NaraName("dave"): 7, types.NaraName("alice"): 4, types.NaraName("bob"): 2},
		"dave":  {types.NaraName("alice"): 9, types.NaraName("bob"): 1, types.NaraName("carol"): 3},
	}

	wm := NewWorldMessage("Hello!", "alice")

	// Alice chooses next (should be Bob - highest clout)
	next := ChooseNextNara("alice", wm, clout["alice"], []types.NaraName{types.NaraName("alice"), types.NaraName("bob"), types.NaraName("carol"), types.NaraName("dave")})
	if next != "bob" {
		t.Errorf("Alice should choose bob, got %s", next)
	}

	// Add Bob's hop
	bobSoul := NativeSoulCustom(worldHW2, "bob")
	if err := wm.AddHop("bob", DeriveKeypair(bobSoul), "🌟"); err != nil {
		t.Fatalf("Failed to add Bob's hop: %v", err)
	}

	// Bob chooses next (should be Carol - highest unvisited)
	next = ChooseNextNara("bob", wm, clout["bob"], []types.NaraName{types.NaraName("alice"), types.NaraName("bob"), types.NaraName("carol"), types.NaraName("dave")})
	if next != "carol" {
		t.Errorf("Bob should choose carol, got %s", next)
	}

	// Add Carol's hop
	carolSoul := NativeSoulCustom(worldHW3, "carol")
	if err := wm.AddHop("carol", DeriveKeypair(carolSoul), "🎉"); err != nil {
		t.Fatalf("Failed to add Carol's hop: %v", err)
	}

	// Carol chooses next (should be Dave - only unvisited non-originator)
	next = ChooseNextNara("carol", wm, clout["carol"], []types.NaraName{types.NaraName("alice"), types.NaraName("bob"), types.NaraName("carol"), types.NaraName("dave")})
	if next != "dave" {
		t.Errorf("Carol should choose dave, got %s", next)
	}

	// Add Dave's hop
	daveSoul := NativeSoulCustom(worldHW4, "dave")
	if err := wm.AddHop("dave", DeriveKeypair(daveSoul), "🚀"); err != nil {
		t.Fatalf("Failed to add Dave's hop: %v", err)
	}

	// Dave chooses next (should be Alice - only option is to return home)
	next = ChooseNextNara("dave", wm, clout["dave"], []types.NaraName{types.NaraName("alice"), types.NaraName("bob"), types.NaraName("carol"), types.NaraName("dave")})
	if next != "alice" {
		t.Errorf("Dave should choose alice (return home), got %s", next)
	}
}

func TestWorldJourney_EndToEnd(t *testing.T) {
	// Create 4 test naras with souls and keypairs
	type testNara struct {
		name    string
		soul    SoulV1
		keypair NaraKeypair
	}

	naras := []testNara{
		{"alice", NativeSoulCustom(worldHW1, "alice"), DeriveKeypair(NativeSoulCustom(worldHW1, "alice"))},
		{"bob", NativeSoulCustom(worldHW2, "bob"), DeriveKeypair(NativeSoulCustom(worldHW2, "bob"))},
		{"carol", NativeSoulCustom(worldHW3, "carol"), DeriveKeypair(NativeSoulCustom(worldHW3, "carol"))},
		{"dave", NativeSoulCustom(worldHW4, "dave"), DeriveKeypair(NativeSoulCustom(worldHW4, "dave"))},
	}

	// Public key lookup
	getPublicKey := func(name types.NaraName) []byte {
		for _, n := range naras {
			if n.name == name.String() {
				return n.keypair.PublicKey
			}
		}
		return nil
	}

	// Mock clout - creates path: alice -> bob -> carol -> dave -> alice
	clout := map[string]map[types.NaraName]float64{
		"alice": {types.NaraName("bob"): 10, types.NaraName("carol"): 5, types.NaraName("dave"): 2},
		"bob":   {types.NaraName("carol"): 8, types.NaraName("dave"): 3, types.NaraName("alice"): 5},
		"carol": {types.NaraName("dave"): 7, types.NaraName("alice"): 4, types.NaraName("bob"): 2},
		"dave":  {types.NaraName("alice"): 9, types.NaraName("bob"): 1, types.NaraName("carol"): 3},
	}
	onlineNaras := []types.NaraName{types.NaraName("alice"), types.NaraName("bob"), types.NaraName("carol"), types.NaraName("dave")}

	// Alice starts the journey
	wm := NewWorldMessage("Going around the world!", "alice")

	// Simulate the journey
	currentNara := types.NaraName("alice")
	for !wm.IsComplete() {
		next := ChooseNextNara(currentNara, wm, clout[currentNara.String()], onlineNaras)
		if next == "" {
			t.Fatal("Journey stuck - no next nara")
		}

		// Find the nara and add hop
		for _, n := range naras {
			if n.name == next.String() {
				stamps := map[string]string{"alice": "🏠", "bob": "🌟", "carol": "🎉", "dave": "🚀"}
				err := wm.AddHop(next, n.keypair, stamps[next.String()])
				if err != nil {
					t.Fatalf("Failed to add hop for %s: %v", next, err)
				}
				break
			}
		}

		currentNara = next

		// Safety: max 10 hops to prevent infinite loop
		if len(wm.Hops) > 10 {
			t.Fatal("Journey exceeded max hops")
		}
	}

	// Verify the complete chain
	err := wm.VerifyChain(getPublicKey)
	if err != nil {
		t.Errorf("Complete journey should verify: %v", err)
	}

	// Check the path
	expectedPath := []types.NaraName{types.NaraName("bob"), types.NaraName("carol"), types.NaraName("dave"), types.NaraName("alice")}
	if len(wm.Hops) != len(expectedPath) {
		t.Errorf("Expected %d hops, got %d", len(expectedPath), len(wm.Hops))
	}
	for i, hop := range wm.Hops {
		if hop.Nara != expectedPath[i] {
			t.Errorf("Hop %d: expected %s, got %s", i, expectedPath[i], hop.Nara)
		}
	}

	t.Logf("Journey complete! Path: alice -> %s", formatPath(wm.Hops))
}

func TestWorldJourney_CloutRewards(t *testing.T) {
	// After a successful journey, clout should be awarded
	// Originator: +10, Participants: +2 each

	wm := NewWorldMessage("Hello!", "alice")

	// Build a complete journey
	bobKeypair := DeriveKeypair(NativeSoulCustom(worldHW2, "bob"))
	carolKeypair := DeriveKeypair(NativeSoulCustom(worldHW3, "carol"))
	aliceKeypair := DeriveKeypair(NativeSoulCustom(worldHW1, "alice"))

	if err := wm.AddHop("bob", bobKeypair, "🌟"); err != nil {
		t.Fatalf("Failed to add bob's hop: %v", err)
	}
	if err := wm.AddHop("carol", carolKeypair, "🎉"); err != nil {
		t.Fatalf("Failed to add carol's hop: %v", err)
	}
	if err := wm.AddHop("alice", aliceKeypair, "🏠"); err != nil {
		t.Fatalf("Failed to add alice's hop: %v", err)
	}

	// Calculate rewards
	rewards := CalculateWorldRewards(wm)

	// Alice (originator) should get 10
	if rewards["alice"] != 10 {
		t.Errorf("Originator should get 10 clout, got %v", rewards["alice"])
	}

	// Bob and Carol (participants) should get 2 each
	if rewards["bob"] != 2 {
		t.Errorf("Bob should get 2 clout, got %v", rewards["bob"])
	}
	if rewards["carol"] != 2 {
		t.Errorf("Carol should get 2 clout, got %v", rewards["carol"])
	}
}

// Helper to format path for logging
func formatPath(hops []WorldHop) string {
	result := ""
	for i, hop := range hops {
		if i > 0 {
			result += " -> "
		}
		result += hop.Nara.String() + hop.Stamp
	}
	return result
}
