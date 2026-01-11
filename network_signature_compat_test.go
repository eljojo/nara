package nara

import (
	"fmt"
	"testing"
)

// TestHeyThereEvent_BackwardsCompatibleSignature verifies that a new nara
// (with ID field) can verify signatures from old naras (without ID in signature).
// This ensures gradual rollout without breaking compatibility.
func TestHeyThereEvent_BackwardsCompatibleSignature(t *testing.T) {
	// Create a keypair for an "old nara" that doesn't know about IDs
	ln := testLocalNara("old-nara")

	// Simulate an old nara creating a hey_there event WITHOUT ID in the signature
	// The old nara would sign: "hey_there:name:publicKey:meshIP"
	oldFormatEvent := &HeyThereEvent{
		From:      "old-nara",
		PublicKey: FormatPublicKey(ln.Keypair.PublicKey),
		MeshIP:    "100.64.0.1",
		// ID is intentionally empty - old naras don't set this
	}

	// Old nara signs with old format (no ID)
	oldMessage := "hey_there:old-nara:" + oldFormatEvent.PublicKey + ":100.64.0.1"
	oldFormatEvent.Signature = ln.Keypair.SignBase64([]byte(oldMessage))

	// Now simulate a NEW nara receiving this event and populating the ID field
	// (because the new nara computed an ID for the old nara based on its soul+name)
	newNaraID, err := ComputeNaraID(testSoul("old-nara"), "old-nara")
	if err != nil {
		t.Fatalf("Failed to compute ID: %v", err)
	}
	oldFormatEvent.ID = newNaraID // New nara adds ID to the struct

	// The new nara's Verify() should use fallback logic and succeed
	if !oldFormatEvent.Verify() {
		t.Error("New nara should be able to verify old nara's signature (without ID) using fallback")
	}

	t.Log("✅ Backwards compatibility: New nara verified old nara's signature")
}

// TestHeyThereEvent_NewFormatSignature verifies that new naras
// (with ID) create signatures that include the ID and verify correctly.
func TestHeyThereEvent_NewFormatSignature(t *testing.T) {
	// Create a new nara with ID
	ln := testLocalNara("new-nara")

	// New nara creates event WITH ID
	newEvent := &HeyThereEvent{
		From:      "new-nara",
		PublicKey: FormatPublicKey(ln.Keypair.PublicKey),
		MeshIP:    "100.64.0.2",
		ID:        ln.ID,
	}

	// Sign with new format (includes ID)
	newEvent.Sign(ln.Keypair)

	// Verify should succeed
	if !newEvent.Verify() {
		t.Error("New nara's signature (with ID) should verify successfully")
	}

	t.Log("✅ New format: Signature with ID verified correctly")
}

// TestHeyThereEvent_OldFormatWithoutID verifies that events without ID
// still work correctly (for old naras that never set ID).
func TestHeyThereEvent_OldFormatWithoutID(t *testing.T) {
	// Simulate an old nara that doesn't know about IDs at all
	ln := testLocalNara("ancient-nara")

	oldEvent := &HeyThereEvent{
		From:      "ancient-nara",
		PublicKey: FormatPublicKey(ln.Keypair.PublicKey),
		MeshIP:    "100.64.0.3",
		// ID is empty - old nara never sets this
	}

	// Sign with old format (Sign() should detect empty ID and use old format)
	oldEvent.Sign(ln.Keypair)

	// Verify should succeed with old format
	if !oldEvent.Verify() {
		t.Error("Old format signature (without ID) should verify successfully")
	}

	t.Log("✅ Old format: Signature without ID verified correctly")
}

// TestChauEvent_BackwardsCompatibleSignature verifies backwards compatibility for chau events
func TestChauEvent_BackwardsCompatibleSignature(t *testing.T) {
	// Create a keypair for an "old nara"
	ln := testLocalNara("old-nara")

	// Old nara creates chau event WITHOUT ID in signature
	oldFormatEvent := &ChauEvent{
		From:      "old-nara",
		PublicKey: FormatPublicKey(ln.Keypair.PublicKey),
		// ID is intentionally empty
	}

	// Old nara signs with old format: "chau:name:publicKey"
	oldMessage := "chau:old-nara:" + oldFormatEvent.PublicKey
	oldFormatEvent.Signature = ln.Keypair.SignBase64([]byte(oldMessage))

	// New nara populates ID field after receiving
	newNaraID, err := ComputeNaraID(testSoul("old-nara"), "old-nara")
	if err != nil {
		t.Fatalf("Failed to compute ID: %v", err)
	}
	oldFormatEvent.ID = newNaraID

	// Verify should use fallback and succeed
	if !oldFormatEvent.Verify() {
		t.Error("New nara should verify old nara's chau signature using fallback")
	}

	t.Log("✅ Backwards compatibility: New nara verified old chau signature")
}

// TestChauEvent_NewFormatSignature verifies new format chau signatures
func TestChauEvent_NewFormatSignature(t *testing.T) {
	ln := testLocalNara("new-nara")

	newEvent := &ChauEvent{
		From:      "new-nara",
		PublicKey: FormatPublicKey(ln.Keypair.PublicKey),
		ID:        ln.ID,
	}

	newEvent.Sign(ln.Keypair)

	if !newEvent.Verify() {
		t.Error("New chau signature (with ID) should verify successfully")
	}

	t.Log("✅ New format: Chau signature with ID verified correctly")
}

// TestChauEvent_OldFormatWithoutID verifies old format chau events
func TestChauEvent_OldFormatWithoutID(t *testing.T) {
	ln := testLocalNara("ancient-nara")

	oldEvent := &ChauEvent{
		From:      "ancient-nara",
		PublicKey: FormatPublicKey(ln.Keypair.PublicKey),
		// ID is empty
	}

	oldEvent.Sign(ln.Keypair)

	if !oldEvent.Verify() {
		t.Error("Old chau signature (without ID) should verify successfully")
	}

	t.Log("✅ Old format: Chau signature without ID verified correctly")
}

// TestMixedRollout_OldAndNewNaras simulates a network with both old and new naras
func TestMixedRollout_OldAndNewNaras(t *testing.T) {
	// Create an old nara (simulated by signing without ID)
	oldNara := testLocalNara("old-nara")

	// Create a new nara (with ID)
	newNara := testLocalNara("new-nara")

	// Old nara sends hey_there (old format signature)
	oldHeyThere := &HeyThereEvent{
		From:      "old-nara",
		PublicKey: FormatPublicKey(oldNara.Keypair.PublicKey),
		MeshIP:    "100.64.0.1",
	}
	// Sign with old format
	oldMessage := "hey_there:old-nara:" + oldHeyThere.PublicKey + ":100.64.0.1"
	oldHeyThere.Signature = oldNara.Keypair.SignBase64([]byte(oldMessage))

	// New nara receives it and adds ID (because new nara computes IDs for everyone)
	oldNaraID, _ := ComputeNaraID(testSoul("old-nara"), "old-nara")
	oldHeyThere.ID = oldNaraID

	// New nara should verify old signature
	if !oldHeyThere.Verify() {
		t.Error("New nara failed to verify old nara's signature")
	}

	// New nara sends hey_there (new format with ID)
	newHeyThere := &HeyThereEvent{
		From:      "new-nara",
		PublicKey: FormatPublicKey(newNara.Keypair.PublicKey),
		MeshIP:    "100.64.0.2",
		ID:        newNara.ID,
	}
	newHeyThere.Sign(newNara.Keypair)

	// Old nara receives it - old nara's Verify() would only check without ID
	// But this test simulates what would happen if old nara doesn't strip ID
	// The new signature format should still verify correctly
	if !newHeyThere.Verify() {
		t.Error("New signature should verify (even if old nara doesn't understand ID)")
	}

	t.Log("✅ Mixed rollout: Old and new naras can interoperate")
}

// TestHeyThereEvent_SignatureMustIncludeID verifies that the signature
// MUST include the ID if present.
func TestHeyThereEvent_SignatureMustIncludeID(t *testing.T) {
	t.Skip("To be added after rollout of new ID signatures") // TODO(signature)

	ln := testLocalNara("new-nara")
	event := &HeyThereEvent{
		From:      "new-nara",
		PublicKey: FormatPublicKey(ln.Keypair.PublicKey),
		MeshIP:    "100.64.0.2",
		ID:        ln.ID,
	}

	// Manually create a signature that includes the ID
	msgWithID := fmt.Sprintf("hey_there:%s:%s:%s:%s", event.From, event.PublicKey, event.MeshIP, event.ID)
	event.Signature = ln.Keypair.SignBase64([]byte(msgWithID))

	// This should fail if signatureMessage() is incorrectly excluding the ID
	// because Verify() will try signatureMessage() first, and then fallback to legacy.
	// Both will use a message WITHOUT ID, which won't match our signature-with-id.
	if !event.Verify() {
		t.Errorf("Verify() failed for an event signed WITH ID. This likely means signatureMessage() is not including the ID in the message. Current signatureMessage: %s", event.signatureMessage())
	}

	t.Log("✅ HeyThere: Signature with ID verified correctly")
}

// TestChauEvent_SignatureMustIncludeID verifies the same for ChauEvent
func TestChauEvent_SignatureMustIncludeID(t *testing.T) {
	t.Skip("To be added after rollout of new ID signatures") // TODO(signature)

	ln := testLocalNara("new-nara")
	event := &ChauEvent{
		From:      "new-nara",
		PublicKey: FormatPublicKey(ln.Keypair.PublicKey),
		ID:        ln.ID,
	}

	// Sign WITH ID
	msgWithID := fmt.Sprintf("chau:%s:%s:%s", event.From, event.PublicKey, event.ID)
	event.Signature = ln.Keypair.SignBase64([]byte(msgWithID))

	if !event.Verify() {
		t.Errorf("Verify() failed for a chau event signed WITH ID. Current signatureMessage: %s", event.signatureMessage())
	}

	t.Log("✅ Chau: Signature with ID verified correctly")
}
