package nara

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/btcsuite/btcutil/base58"
	"github.com/eljojo/nara/types"
	"golang.org/x/crypto/hkdf"
)

const (
	SeedLen = 32
	TagLen  = 8
	SoulLen = SeedLen + TagLen // 40 bytes total
)

type SoulV1 struct {
	Seed [SeedLen]byte
	Tag  [TagLen]byte
}

type IdentityResult struct {
	Name        types.NaraName // Changed from string
	Soul        SoulV1
	ID          types.NaraID // Nara ID: deterministic hash of soul+name
	IsValidBond bool
	IsNative    bool
}

// FormatSoul encodes a SoulV1 as a Base58 string
func FormatSoul(soul SoulV1) string {
	data := make([]byte, SoulLen)
	copy(data[:SeedLen], soul.Seed[:])
	copy(data[SeedLen:], soul.Tag[:])
	return base58.Encode(data)
}

// ParseSoul decodes a Base58 string into a SoulV1
func ParseSoul(s string) (SoulV1, error) {
	data := base58.Decode(s)
	if len(data) != SoulLen {
		return SoulV1{}, errors.New("invalid soul length")
	}

	var soul SoulV1
	copy(soul.Seed[:], data[:SeedLen])
	copy(soul.Tag[:], data[SeedLen:])
	return soul, nil
}

// ComputeTag computes the HMAC tag that bonds a seed to a name
func ComputeTag(seed [SeedLen]byte, name types.NaraName) [TagLen]byte {
	h := hmac.New(sha256.New, seed[:])
	h.Write([]byte("nara:name:v2:" + name.String()))
	sum := h.Sum(nil)

	var tag [TagLen]byte
	copy(tag[:], sum[:TagLen])
	return tag
}

// ValidateBond checks if a soul is validly bonded to a name
func ValidateBond(soul SoulV1, name types.NaraName) bool {
	if name == "" {
		return false
	}
	expectedTag := ComputeTag(soul.Seed, name)
	return hmac.Equal(soul.Tag[:], expectedTag[:])
}

// NativeSoulCustom generates a deterministic soul for a custom name on given hardware
func NativeSoulCustom(hwFingerprint []byte, name types.NaraName) SoulV1 {
	// Derive seed using HKDF
	hkdfReader := hkdf.New(sha256.New, hwFingerprint, []byte("nara:soul:v2"), []byte("seed:custom:"+name.String()))

	var seed [SeedLen]byte
	if _, err := io.ReadFull(hkdfReader, seed[:]); err != nil {
		panic("Failed to derive seed from HKDF: " + err.Error())
	}

	tag := ComputeTag(seed, name)

	return SoulV1{Seed: seed, Tag: tag}
}

// NativeSoulGenerated generates a deterministic soul for generated-name mode
func NativeSoulGenerated(hwFingerprint []byte) SoulV1 {
	// Derive seed using HKDF (without name in derivation)
	hkdfReader := hkdf.New(sha256.New, hwFingerprint, []byte("nara:soul:v2"), []byte("seed:generated"))

	var seed [SeedLen]byte
	if _, err := io.ReadFull(hkdfReader, seed[:]); err != nil {
		panic("Failed to derive seed from HKDF: " + err.Error())
	}

	// Compute the name this seed generates
	name := types.NaraName(GenerateName(hex.EncodeToString(seed[:])))

	// Compute tag for that name
	tag := ComputeTag(seed, name)

	return SoulV1{Seed: seed, Tag: tag}
}

// NameFromSoul derives the generated name from a soul's seed
func NameFromSoul(soul SoulV1) types.NaraName {
	return types.NaraName(GenerateName(hex.EncodeToString(soul.Seed[:])))
}

// DetermineIdentity resolves name and soul from arguments and hardware.
// hostname should be the short hostname (no domain suffix).
func DetermineIdentity(nameArg, soulArg, hostname string, hwFingerprint []byte) IdentityResult {
	// Determine if we're in generated-name mode (no name provided, generic hostname)
	effectiveName := nameArg
	if effectiveName == "" {
		effectiveName = hostname
	}
	generatedMode := IsGenericHostname(effectiveName)

	var soul SoulV1
	var name types.NaraName
	var err error

	if soulArg != "" {
		// Soul provided - parse it
		soul, err = ParseSoul(soulArg)
		if err != nil {
			// Invalid soul format - treat as invalid bond
			return IdentityResult{
				Name:        types.NaraName(effectiveName),
				Soul:        SoulV1{},
				ID:          "",
				IsValidBond: false,
				IsNative:    false,
			}
		}

		if generatedMode {
			// In generated mode with provided soul, derive name from soul
			name = NameFromSoul(soul)
		} else {
			name = types.NaraName(effectiveName)
		}
	} else {
		// No soul provided - generate native
		if generatedMode {
			soul = NativeSoulGenerated(hwFingerprint)
			name = NameFromSoul(soul)
		} else {
			name = types.NaraName(effectiveName)
			soul = NativeSoulCustom(hwFingerprint, name)
		}
	}

	// Validate bond - the HMAC tag must match
	isValidBond := ValidateBond(soul, name)

	// Check if native (matches what this hardware would generate)
	var expectedSoul SoulV1
	if generatedMode {
		expectedSoul = NativeSoulGenerated(hwFingerprint)
	} else {
		expectedSoul = NativeSoulCustom(hwFingerprint, name)
	}
	isNative := soul.Seed == expectedSoul.Seed && soul.Tag == expectedSoul.Tag

	// Compute nara ID from soul + name
	soulBase58 := FormatSoul(soul)
	id, err := ComputeNaraID(soulBase58, name)
	if err != nil {
		// This should never happen since we control soul format
		panic(fmt.Sprintf("Failed to compute nara ID: %v (soul=%s, name=%s)", err, soulBase58, name))
	}

	return IdentityResult{
		Name:        name,
		Soul:        soul,
		ID:          id,
		IsValidBond: isValidBond,
		IsNative:    isNative,
	}
}
