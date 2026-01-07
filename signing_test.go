package nara

import (
	"encoding/json"
	"testing"
)

func TestHeyThereEvent_SignAndVerify(t *testing.T) {
	// Create a keypair from a test soul
	soul := NativeSoulCustom([]byte("test-hw-heythere-1"), "alice")
	keypair := DeriveKeypair(soul)

	// Create and sign a hey_there event
	event := &HeyThereEvent{
		From:      "alice",
		PublicKey: FormatPublicKey(keypair.PublicKey),
		MeshIP:    "100.64.0.1",
	}
	event.Sign(keypair)

	if event.Signature == "" {
		t.Error("Expected signature to be set after signing")
	}

	// Verify should succeed
	if !event.Verify() {
		t.Error("Expected signature to verify")
	}

	// Tamper with the event - should fail
	tamperedEvent := *event
	tamperedEvent.MeshIP = "100.64.0.2"
	if tamperedEvent.Verify() {
		t.Error("Expected tampered event to fail verification")
	}

	// Wrong signature - should fail
	wrongEvent := &HeyThereEvent{
		From:      "alice",
		PublicKey: FormatPublicKey(keypair.PublicKey),
		MeshIP:    "100.64.0.1",
		Signature: "invalid-signature",
	}
	if wrongEvent.Verify() {
		t.Error("Expected invalid signature to fail verification")
	}
}

func TestHeyThereEvent_VerifyUnsigned(t *testing.T) {
	event := &HeyThereEvent{
		From:      "alice",
		PublicKey: "",
		MeshIP:    "100.64.0.1",
	}

	// Should return false for unsigned event
	if event.Verify() {
		t.Error("Expected Verify to return false for unsigned event")
	}
}

func TestNewspaperEvent_SignAndVerify(t *testing.T) {
	// Create a keypair from a test soul
	soul := NativeSoulCustom([]byte("test-hw-newspaper-1"), "alice")
	keypair := DeriveKeypair(soul)

	// Create a status
	status := NaraStatus{
		Flair:      "test-flair",
		Chattiness: 50,
		PublicKey:  FormatPublicKey(keypair.PublicKey),
		MeshIP:     "100.64.0.1",
	}

	// Create and sign a newspaper event manually (simulating what SignNewspaper does)
	event := NewspaperEvent{
		From:   "alice",
		Status: status,
	}

	// Sign the JSON-serialized status
	statusJSON, _ := json.Marshal(status)
	event.Signature = keypair.SignBase64(statusJSON)

	// Verify should succeed
	if !event.Verify(keypair.PublicKey) {
		t.Error("Expected signature to verify")
	}

	// Tamper with status - should fail
	tamperedEvent := event
	tamperedEvent.Status.Chattiness = 100
	if tamperedEvent.Verify(keypair.PublicKey) {
		t.Error("Expected tampered event to fail verification")
	}

	// Wrong public key - should fail
	wrongSoul := NativeSoulCustom([]byte("different-hw-newspaper"), "bob")
	wrongKeypair := DeriveKeypair(wrongSoul)
	if event.Verify(wrongKeypair.PublicKey) {
		t.Error("Expected wrong public key to fail verification")
	}
}

func TestNewspaperEvent_VerifyUnsigned(t *testing.T) {
	event := NewspaperEvent{
		From:   "alice",
		Status: NaraStatus{Flair: "test"},
	}

	soul := NativeSoulCustom([]byte("test-hw-newspaper-2"), "alice")
	keypair := DeriveKeypair(soul)

	// Should return false for unsigned event
	if event.Verify(keypair.PublicKey) {
		t.Error("Expected Verify to return false for unsigned event")
	}
}
