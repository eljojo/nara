package nara

import (
	"encoding/hex"
	"testing"
)

// Test hardware fingerprints for deterministic testing
var (
	hw1 = hashBytes([]byte("hardware-1"))
	hw2 = hashBytes([]byte("hardware-2"))
)

func TestSoulV1Format(t *testing.T) {
	t.Parallel()
	// Test that a soul can be created, formatted, and parsed back
	soul := NativeSoulCustom(hw1, "jojo")

	formatted := FormatSoul(soul)
	t.Logf("Formatted soul: %s (len=%d)", formatted, len(formatted))

	// Base58 encoding of 40 bytes should be roughly 54 chars
	if len(formatted) < 50 || len(formatted) > 60 {
		t.Errorf("Expected soul length ~54 chars, got %d", len(formatted))
	}

	// Parse it back
	parsed, err := ParseSoul(formatted)
	if err != nil {
		t.Fatalf("Failed to parse soul: %v", err)
	}

	if parsed.Seed != soul.Seed {
		t.Errorf("Seed mismatch after round-trip")
	}
	if parsed.Tag != soul.Tag {
		t.Errorf("Tag mismatch after round-trip")
	}
}

func TestSoulV1InvalidFormat(t *testing.T) {
	t.Parallel()
	// Invalid Base58 should fail
	_, err := ParseSoul("invalid!@#$")
	if err == nil {
		t.Error("Expected error for invalid Base58")
	}

	// Too short should fail
	_, err = ParseSoul("abc")
	if err == nil {
		t.Error("Expected error for too-short soul")
	}
}

func TestBondValidation(t *testing.T) {
	t.Parallel()
	// Create a soul for "jojo"
	soul := NativeSoulCustom(hw1, "jojo")

	// Should be valid for "jojo"
	if !ValidateBond(soul, "jojo") {
		t.Error("Expected valid bond for correct name")
	}

	// Should be invalid for different name
	if ValidateBond(soul, "other") {
		t.Error("Expected invalid bond for wrong name")
	}

	// Should be invalid for empty name
	if ValidateBond(soul, "") {
		t.Error("Expected invalid bond for empty name")
	}
}

func TestNativeSoulDeterminism(t *testing.T) {
	t.Parallel()
	// Same hw + name should always produce same soul
	soul1 := NativeSoulCustom(hw1, "jojo")
	soul2 := NativeSoulCustom(hw1, "jojo")

	if soul1.Seed != soul2.Seed || soul1.Tag != soul2.Tag {
		t.Error("NativeSoulCustom should be deterministic")
	}

	// Different hw should produce different soul
	soul3 := NativeSoulCustom(hw2, "jojo")
	if soul1.Seed == soul3.Seed {
		t.Error("Different hardware should produce different seed")
	}

	// Different name should produce different soul
	soul4 := NativeSoulCustom(hw1, "other")
	if soul1.Seed == soul4.Seed {
		t.Error("Different name should produce different seed")
	}
}

func TestNativeSoulGenerated(t *testing.T) {
	t.Parallel()
	// Generated soul mode (no name provided)
	soul1 := NativeSoulGenerated(hw1)
	soul2 := NativeSoulGenerated(hw1)

	// Should be deterministic
	if soul1.Seed != soul2.Seed || soul1.Tag != soul2.Tag {
		t.Error("NativeSoulGenerated should be deterministic")
	}

	// Different hw should produce different soul
	soul3 := NativeSoulGenerated(hw2)
	if soul1.Seed == soul3.Seed {
		t.Error("Different hardware should produce different generated soul")
	}

	// The generated name should be derivable from the seed
	name1 := GenerateName(hex.EncodeToString(soul1.Seed[:]))
	name2 := GenerateName(hex.EncodeToString(soul3.Seed[:]))

	if name1 == name2 {
		t.Error("Different hw should produce different generated names")
	}

	// The soul should be valid for its generated name
	if !ValidateBond(soul1, name1) {
		t.Error("Generated soul should be valid for its derived name")
	}
}

func TestCrossHardwareValidity(t *testing.T) {
	t.Parallel()
	// Core requirement: soul from HW1 is VALID on HW2 (foreign but valid bond)

	// Create soul for "jojo" on HW1
	soulHW1 := NativeSoulCustom(hw1, "jojo")

	// Create soul for "jojo" on HW2
	soulHW2 := NativeSoulCustom(hw2, "jojo")

	// Both should be valid bonds for "jojo"
	if !ValidateBond(soulHW1, "jojo") {
		t.Error("HW1 soul should be valid for jojo")
	}
	if !ValidateBond(soulHW2, "jojo") {
		t.Error("HW2 soul should be valid for jojo")
	}

	// They should be different souls (different seeds)
	if soulHW1.Seed == soulHW2.Seed {
		t.Error("Different hardware should produce different seeds")
	}

	// A random/wrong soul should NOT be valid for "jojo"
	wrongSoul := NativeSoulCustom(hw1, "wrongname")
	if ValidateBond(wrongSoul, "jojo") {
		t.Error("Soul for 'wrongname' should not be valid for 'jojo'")
	}
}

func TestDetermineIdentityCustomName(t *testing.T) {
	t.Parallel()
	// ./nara -name jojo on HW1
	result := DetermineIdentity("jojo", "", "nixos", hw1)

	if result.Name != "jojo" {
		t.Errorf("Expected name 'jojo', got '%s'", result.Name)
	}
	if !result.IsValidBond {
		t.Error("Expected valid bond")
	}
	if !result.IsNative {
		t.Error("Expected native soul")
	}

	// Same soul should work on HW2 (foreign but valid)
	soulStr := FormatSoul(result.Soul)
	result2 := DetermineIdentity("jojo", soulStr, "nixos", hw2)

	if result2.Name != "jojo" {
		t.Errorf("Expected name 'jojo', got '%s'", result2.Name)
	}
	if !result2.IsValidBond {
		t.Error("Expected valid bond on HW2")
	}
	if result2.IsNative {
		t.Error("Expected foreign soul on HW2")
	}
}

func TestDetermineIdentityGeneratedName(t *testing.T) {
	t.Parallel()
	// ./nara (no name, generic hostname) on HW1
	result1 := DetermineIdentity("", "", "nixos", hw1)

	// Should have generated a name
	if result1.Name == "" || result1.Name == "nixos" {
		t.Errorf("Expected generated name, got '%s'", result1.Name)
	}

	// Should be valid and native
	if !result1.IsValidBond {
		t.Error("Expected valid bond for generated name")
	}
	if !result1.IsNative {
		t.Error("Expected native soul")
	}

	// Same on HW2 should get different name
	result2 := DetermineIdentity("", "", "nixos", hw2)
	if result1.Name == result2.Name {
		t.Error("Different hardware should produce different generated names")
	}

	// Passing HW1's soul to HW2 should preserve the name
	soulStr := FormatSoul(result1.Soul)
	result3 := DetermineIdentity("", soulStr, "nixos", hw2)

	if result3.Name != result1.Name {
		t.Errorf("Expected preserved name '%s', got '%s'", result1.Name, result3.Name)
	}
	if !result3.IsValidBond {
		t.Error("Expected valid bond for traveling generated soul")
	}
	if result3.IsNative {
		t.Error("Expected foreign soul")
	}
}

func TestDetermineIdentityInvalidSoul(t *testing.T) {
	t.Parallel()
	// ./nara -name jojo -soul <soul-for-other-name>
	wrongSoul := NativeSoulCustom(hw1, "wrongname")
	wrongSoulStr := FormatSoul(wrongSoul)

	result := DetermineIdentity("jojo", wrongSoulStr, "nixos", hw1)

	if result.Name != "jojo" {
		t.Errorf("Name should still be 'jojo', got '%s'", result.Name)
	}
	if result.IsValidBond {
		t.Error("Expected INVALID bond (soul was minted for different name)")
	}
}

func TestDetermineIdentityHostnameAsName(t *testing.T) {
	t.Parallel()
	// ./nara (no -name flag, but hostname is "myserver")
	result := DetermineIdentity("", "", "myserver", hw1)

	if result.Name != "myserver" {
		t.Errorf("Expected hostname 'myserver', got '%s'", result.Name)
	}
	if !result.IsValidBond {
		t.Error("Expected valid bond")
	}
	if !result.IsNative {
		t.Error("Expected native soul")
	}
}

func TestGeneratedNameRegexProtection(t *testing.T) {
	t.Parallel()
	// Someone tries to claim a generated-style name with wrong soul
	// Name: "fuzzy-cat-123" (looks generated)
	// Soul: random soul not minted for this name

	randomSoul := NativeSoulCustom(hw1, "attacker")
	result := DetermineIdentity("fuzzy-cat-123", FormatSoul(randomSoul), "nixos", hw1)

	if result.IsValidBond {
		t.Error("Generated-style name with wrong soul should be invalid")
	}
}
