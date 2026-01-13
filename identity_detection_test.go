package nara

import (
	"testing"
)

// TestComputeNaraID_Deterministic verifies that the same soul+name always produces the same ID
func TestComputeNaraID_Deterministic(t *testing.T) {
	soul := testSoul("test-nara")
	name := "test-nara"

	id1, err1 := ComputeNaraID(soul, name)
	if err1 != nil {
		t.Fatalf("Failed to compute ID (attempt 1): %v", err1)
	}

	id2, err2 := ComputeNaraID(soul, name)
	if err2 != nil {
		t.Fatalf("Failed to compute ID (attempt 2): %v", err2)
	}

	if id1 != id2 {
		t.Errorf("ID should be deterministic: got %s and %s", id1, id2)
	}

	// Verify ID is non-empty
	if id1 == "" {
		t.Error("ID should not be empty")
	}
}

// TestComputeNaraID_DifferentSouls verifies that different souls with the same name produce different IDs
func TestComputeNaraID_DifferentSouls(t *testing.T) {
	name := "same-name"
	soul1 := testSoul("soul1")
	soul2 := testSoul("soul2")

	id1, err1 := ComputeNaraID(soul1, name)
	if err1 != nil {
		t.Fatalf("Failed to compute ID for soul1: %v", err1)
	}

	id2, err2 := ComputeNaraID(soul2, name)
	if err2 != nil {
		t.Fatalf("Failed to compute ID for soul2: %v", err2)
	}

	if id1 == id2 {
		t.Errorf("Different souls should produce different IDs: both got %s", id1)
	}
}

// TestComputeNaraID_DifferentNames verifies that the same soul with different names produces different IDs
func TestComputeNaraID_DifferentNames(t *testing.T) {
	soul := testSoul("test-soul")
	name1 := "nara1"
	name2 := "nara2"

	id1, err1 := ComputeNaraID(soul, name1)
	if err1 != nil {
		t.Fatalf("Failed to compute ID for name1: %v", err1)
	}

	id2, err2 := ComputeNaraID(soul, name2)
	if err2 != nil {
		t.Fatalf("Failed to compute ID for name2: %v", err2)
	}

	if id1 == id2 {
		t.Errorf("Different names should produce different IDs: both got %s", id1)
	}
}

// TestComputeNaraID_InvalidSoul verifies that invalid soul encoding returns an error
func TestComputeNaraID_InvalidSoul(t *testing.T) {
	invalidSoul := "not-valid-base58!!!"
	name := "test-nara"

	_, err := ComputeNaraID(invalidSoul, name)
	if err == nil {
		t.Error("Expected error for invalid soul encoding, got nil")
	}
}

// TestComputeNaraID_WrongSoulLength verifies that a soul with wrong length returns an error
func TestComputeNaraID_WrongSoulLength(t *testing.T) {
	// Create a valid Base58 string but with wrong length (not 40 bytes)
	shortSoul := "3vQB7B6MrGQZaxCuFg4oh" // This is valid Base58 but too short
	name := "test-nara"

	_, err := ComputeNaraID(shortSoul, name)
	if err == nil {
		t.Error("Expected error for wrong soul length, got nil")
	}
}

// TestLocalNara_IDInitialization verifies that LocalNara initializes with correct ID
func TestLocalNara_IDInitialization(t *testing.T) {
	name := "test-nara"
	soul := testSoul(name)
	identity := DetermineIdentity(name, soul, name, nil)

	profile := DefaultMemoryProfile()
	ln, err := NewLocalNara(identity, "", "", "", -1, profile)
	if err != nil {
		t.Fatalf("Failed to create LocalNara: %v", err)
	}
	// Verify ID is computed
	if ln.ID == "" {
		t.Error("LocalNara ID should not be empty")
	}

	// Verify ID matches identity
	if ln.ID != identity.ID {
		t.Errorf("LocalNara ID mismatch: got %s, expected %s", ln.ID, identity.ID)
	}

	// Verify ID is also set in Status
	if ln.Me.Status.ID != identity.ID {
		t.Errorf("LocalNara Status.ID mismatch: got %s, expected %s", ln.Me.Status.ID, identity.ID)
	}
}

// TestLocalNara_IDUniqueness verifies that two LocalNaras with same name but different souls have different IDs
func TestLocalNara_IDUniqueness(t *testing.T) {
	name := "same-name"
	soul1 := testSoul("soul1")
	soul2 := testSoul("soul2")

	identity1 := DetermineIdentity(name, soul1, name, nil)
	identity2 := DetermineIdentity(name, soul2, name, nil)

	profile := DefaultMemoryProfile()
	ln1, err := NewLocalNara(identity1, "", "", "", -1, profile)
	if err != nil {
		t.Fatalf("Failed to create LocalNara: %v", err)
	}
	profile = DefaultMemoryProfile()
	ln2, err := NewLocalNara(identity2, "", "", "", -1, profile)
	if err != nil {
		t.Fatalf("Failed to create LocalNara: %v", err)
	}
	if ln1.ID == ln2.ID {
		t.Errorf("Two naras with same name but different souls should have different IDs: both got %s", ln1.ID)
	}
}
