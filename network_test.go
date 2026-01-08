package nara

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNetwork_ImportNara(t *testing.T) {
	ln := NewLocalNara("me", testSoul("me"), "host", "user", "pass", -1, 0)
	network := ln.Network

	other := NewNara("other")
	other.Status.Flair = "🌈"

	network.importNara(other)

	if len(network.Neighbourhood) != 1 {
		t.Errorf("expected 1 nara in neighbourhood, got %d", len(network.Neighbourhood))
	}

	imported := network.getNara("other")
	if imported.Name != "other" {
		t.Errorf("expected imported nara name to be 'other', got %s", imported.Name)
	}
	if imported.Status.Flair != "🌈" {
		t.Errorf("expected imported nara flair to be '🌈', got %s", imported.Status.Flair)
	}
}

func TestNetwork_NaraOrdering(t *testing.T) {
	ln := NewLocalNara("me", testSoul("me"), "host", "user", "pass", -1, 0)
	network := ln.Network

	// Set me observation
	obsMe := network.local.getMeObservation()
	obsMe.StartTime = 1000
	obsMe.Online = "ONLINE"
	network.local.setMeObservation(obsMe)

	// Add an older nara
	older := NewNara("older")
	network.importNara(older)
	obsOlder := NaraObservation{StartTime: 500, Online: "ONLINE"}
	network.local.setObservation("older", obsOlder)

	// Add a younger nara
	younger := NewNara("younger")
	network.importNara(younger)
	obsYounger := NaraObservation{StartTime: 1500, Online: "ONLINE"}
	network.local.setObservation("younger", obsYounger)

	oldest := network.oldestNara()
	if oldest.Name != "older" {
		t.Errorf("expected oldest nara to be 'older', got %s", oldest.Name)
	}

	youngest := network.youngestNara()
	if youngest.Name != "younger" {
		t.Errorf("expected youngest nara to be 'younger', got %s", youngest.Name)
	}
}

func TestNetwork_NeighbourhoodNames(t *testing.T) {
	ln := NewLocalNara("me", testSoul("me"), "host", "user", "pass", -1, 0)
	network := ln.Network

	network.importNara(NewNara("a"))
	network.importNara(NewNara("b"))

	names := network.NeighbourhoodNames()
	if len(names) != 2 {
		t.Errorf("expected 2 names, got %d", len(names))
	}

	foundA := false
	foundB := false
	for _, n := range names {
		if n == "a" {
			foundA = true
		}
		if n == "b" {
			foundB = true
		}
	}

	if !foundA || !foundB {
		t.Errorf("did not find all expected names: foundA=%v, foundB=%v", foundA, foundB)
	}
}

func TestNara_SoulNotLeakedInJSON(t *testing.T) {
	// Create a nara with a real soul (the kind that gets serialized over MQTT/HTTP)
	soul := "8Qv9xR3kM7nL2pY5wJ4hT6fD1gS0aZ8cB3vN9mK7qE5rU2yX4iO6lP"
	ln := NewLocalNara("testnara", soul, "host", "user", "pass", -1, 0)

	// Serialize the Nara (this is what selfie() sends over MQTT)
	naraJSON, err := json.Marshal(ln.Me)
	if err != nil {
		t.Fatalf("Failed to marshal Nara: %v", err)
	}

	// The soul should NOT appear in the serialized JSON
	if strings.Contains(string(naraJSON), soul) {
		t.Errorf("SECURITY: Soul leaked in Nara JSON serialization!\nJSON: %s", string(naraJSON))
	}

	// Also check NaraStatus directly (this is what HTTP API returns)
	statusJSON, err := json.Marshal(ln.Me.Status)
	if err != nil {
		t.Fatalf("Failed to marshal NaraStatus: %v", err)
	}

	if strings.Contains(string(statusJSON), soul) {
		t.Errorf("SECURITY: Soul leaked in NaraStatus JSON serialization!\nJSON: %s", string(statusJSON))
	}

	// Verify the JSON doesn't even have a "Soul" field
	if strings.Contains(string(naraJSON), `"Soul"`) {
		t.Errorf("SECURITY: Nara JSON contains Soul field!\nJSON: %s", string(naraJSON))
	}
	if strings.Contains(string(statusJSON), `"Soul"`) {
		t.Errorf("SECURITY: NaraStatus JSON contains Soul field!\nJSON: %s", string(statusJSON))
	}
}

// TestHeyThere_TriggersAnnounce verifies that receiving a hey_there event
// causes the nara to announce itself (post its newspaper).
// This is a regression test for the selfie removal - without this behavior,
// naras would take much longer to discover each other after boot.
func TestHeyThere_TriggersAnnounce(t *testing.T) {
	ln := NewLocalNara("me", testSoul("me"), "host", "user", "pass", -1, 0)
	network := ln.Network

	// Configure for testing:
	// - NOT ReadOnly so announce() actually runs
	// - TransportGossip so postEvent skips MQTT (no real network needed)
	// - testSkipHeyThereSleep to avoid 1s delay in tests
	network.ReadOnly = false
	network.TransportMode = TransportGossip
	network.testSkipHeyThereSleep = true

	// Verify initial state
	if network.testAnnounceCount != 0 {
		t.Fatalf("expected initial announce count to be 0, got %d", network.testAnnounceCount)
	}

	// Simulate receiving a hey_there from another nara
	network.handleHeyThereEvent(HeyThereEvent{From: "newcomer"})

	// Verify that announce() was called
	if network.testAnnounceCount != 1 {
		t.Errorf("expected announce count to be 1 after hey_there, got %d", network.testAnnounceCount)
	}

	// Verify the newcomer was recorded as online
	obs := network.local.getObservation("newcomer")
	if obs.Online != "ONLINE" {
		t.Errorf("expected newcomer to be ONLINE, got %s", obs.Online)
	}
}

// TestHeyThere_ReadOnlySkipsAnnounce verifies that ReadOnly mode
// prevents announcement (as expected for read-only naras).
func TestHeyThere_ReadOnlySkipsAnnounce(t *testing.T) {
	ln := NewLocalNara("me", testSoul("me"), "host", "user", "pass", -1, 0)
	network := ln.Network

	// ReadOnly mode should skip announce
	network.ReadOnly = true
	network.testSkipHeyThereSleep = true

	network.handleHeyThereEvent(HeyThereEvent{From: "newcomer"})

	// announce() should NOT have been called
	if network.testAnnounceCount != 0 {
		t.Errorf("expected announce count to be 0 in ReadOnly mode, got %d", network.testAnnounceCount)
	}

	// But the newcomer should still be recorded as online
	obs := network.local.getObservation("newcomer")
	if obs.Online != "ONLINE" {
		t.Errorf("expected newcomer to be ONLINE even in ReadOnly mode, got %s", obs.Online)
	}
}

// TestNewspaperEvent_JSONParsing is a regression test for the bug where
// newspaper events sent over MQTT were being parsed incorrectly.
// The bug: We were unmarshalling the JSON into NaraStatus directly,
// but the JSON structure is NewspaperEvent{From, Status, Signature}.
// This caused all status fields to be empty because they're nested under "Status".
func TestNewspaperEvent_JSONParsing(t *testing.T) {
	// 1. Setup sender and create a signed event using real code
	senderName := "blue-jay"
	// Generate a valid native soul for the sender
	senderSoulV1 := NativeSoulCustom([]byte("test-hw-fingerprint"), senderName)
	senderSoul := FormatSoul(senderSoulV1)

	sender := NewLocalNara(senderName, senderSoul, "host", "user", "pass", -1, 0)

	sender.Me.Status.Flair = "🐦"
	sender.Me.Status.Chattiness = 75
	sender.Me.Status.Buzz = 42
	sender.Me.Status.Trend = "coffee"
	sender.Me.Status.TrendEmoji = "☕"
	sender.Me.Status.MeshEnabled = true
	sender.Me.Status.MeshIP = "100.64.0.1"
	sender.Me.Status.Personality = NaraPersonality{
		Agreeableness: 71,
		Sociability:   87,
		Chill:         41,
	}

	// Create the signed event - this uses the real signing logic
	event := sender.Network.SignNewspaper(sender.Me.Status)

	// 2. Serialize to JSON (this is what gets sent over MQTT)
	eventJSON, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Failed to marshal NewspaperEvent: %v", err)
	}

	// 3. Setup receiver and process the event using real code
	receiver := NewLocalNara("receiver", testSoul("receiver"), "host", "user", "pass", -1, 0)

	// Parse the JSON the way the MQTT handler does (newspaperHandler in mqtt.go)
	var parsedEvent NewspaperEvent
	if err := json.Unmarshal(eventJSON, &parsedEvent); err != nil {
		t.Fatalf("Failed to unmarshal NewspaperEvent: %v", err)
	}

	// In real life, 'from' comes from the MQTT topic: nara/newspaper/blue-jay
	parsedEvent.From = "blue-jay"

	// Process the event using the real handler
	receiver.Network.handleNewspaperEvent(parsedEvent)

	// 4. Verify the receiver's neighborhood was updated correctly
	imported := receiver.Network.getNara("blue-jay")
	if imported.Name == "" {
		t.Fatal("blue-jay not found in neighborhood")
	}

	if imported.Status.Flair != "🐦" {
		t.Errorf("Flair: expected '🐦', got '%s'", imported.Status.Flair)
	}
	if imported.Status.Chattiness != 75 {
		t.Errorf("Chattiness: expected 75, got %d", imported.Status.Chattiness)
	}
	if imported.Status.Buzz != 42 {
		t.Errorf("Buzz: expected 42, got %d", imported.Status.Buzz)
	}
	if imported.Status.Trend != "coffee" {
		t.Errorf("Trend: expected 'coffee', got '%s'", imported.Status.Trend)
	}
	if imported.Status.TrendEmoji != "☕" {
		t.Errorf("TrendEmoji: expected '☕', got '%s'", imported.Status.TrendEmoji)
	}
	if imported.Status.Personality.Agreeableness != 71 {
		t.Errorf("Personality.Agreeableness: expected 71, got %d", imported.Status.Personality.Agreeableness)
	}
	if imported.Status.MeshIP != "100.64.0.1" {
		t.Errorf("MeshIP: expected '100.64.0.1', got '%s'", imported.Status.MeshIP)
	}
	if !imported.Status.MeshEnabled {
		t.Errorf("MeshEnabled: expected true, got false")
	}
	if imported.Status.PublicKey != sender.Me.Status.PublicKey {
		t.Errorf("PublicKey mismatch: expected %s, got %s", sender.Me.Status.PublicKey, imported.Status.PublicKey)
	}

	// Verify the signature verification actually worked inside handleNewspaperEvent
	// If it had failed, the neighborhood would not have been updated with these values.

	// 5. Demonstrate the bug regression test
	// If we parse into NaraStatus directly, all fields are empty because they are nested under "Status" in the JSON
	var wrongParsed NaraStatus
	if err := json.Unmarshal(eventJSON, &wrongParsed); err != nil {
		t.Fatalf("Failed to unmarshal as NaraStatus: %v", err)
	}

	if wrongParsed.Flair != "" {
		t.Errorf("BUG CHECK: Parsing NewspaperEvent JSON as NaraStatus should result in empty Flair, got '%s'", wrongParsed.Flair)
	}
	if wrongParsed.Personality.Agreeableness != 0 {
		t.Errorf("BUG CHECK: Parsing NewspaperEvent JSON as NaraStatus should result in zero Agreeableness, got %d", wrongParsed.Personality.Agreeableness)
	}
}
