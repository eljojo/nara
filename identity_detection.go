package nara

import (
	"crypto/sha256"
	"fmt"

	"github.com/btcsuite/btcutil/base58"
)

// ComputeNaraID computes a deterministic, stable ID from soul and name.
// This allows distinguishing naras with the same name but different souls.
//
// Computation: ID = Base58(SHA256(soul_bytes || name_bytes))
//
// Where:
//   - soul_bytes: Base58-decoded 40-byte soul (32-byte seed + 8-byte tag)
//   - name_bytes: UTF-8 encoded name string
//   - Result: Base58-encoded hash for human readability
//
// The ID is:
//   - Deterministic: Same soul+name always produces same ID
//   - Stable: Survives restarts (doesn't depend on ephemeral keypairs)
//   - Unique: Different souls with same name produce different IDs
func ComputeNaraID(soulBase58 string, name string) (NaraID, error) {
	// Decode soul from Base58 to get raw 40-byte soul
	soulBytes := base58.Decode(soulBase58)
	if len(soulBytes) == 0 {
		return "", fmt.Errorf("failed to decode soul: invalid base58 string")
	}

	// Validate soul length (should be 40 bytes: 32-byte seed + 8-byte tag)
	if len(soulBytes) != SoulLen {
		return "", fmt.Errorf("invalid soul length: got %d bytes, expected %d", len(soulBytes), SoulLen)
	}

	// Hash soul + name
	hasher := sha256.New()
	hasher.Write(soulBytes)
	hasher.Write([]byte(name))
	hash := hasher.Sum(nil)

	// Encode as Base58 for consistency with soul encoding
	return NaraID(base58.Encode(hash)), nil
}
