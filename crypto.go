package nara

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
)

// NaraKeypair holds an Ed25519 keypair derived from a soul
type NaraKeypair struct {
	PrivateKey ed25519.PrivateKey
	PublicKey  ed25519.PublicKey
}

// DeriveKeypair deterministically derives an Ed25519 keypair from a soul's seed.
// The soul's 32-byte seed is exactly Ed25519's SeedSize, so same soul = same keypair.
func DeriveKeypair(soul SoulV1) NaraKeypair {
	privateKey := ed25519.NewKeyFromSeed(soul.Seed[:])
	publicKey := privateKey.Public().(ed25519.PublicKey)

	return NaraKeypair{
		PrivateKey: privateKey,
		PublicKey:  publicKey,
	}
}

// Sign signs a message with the keypair's private key
func (kp NaraKeypair) Sign(message []byte) []byte {
	return ed25519.Sign(kp.PrivateKey, message)
}

// SignBase64 signs a message and returns the signature as a base64 string
func (kp NaraKeypair) SignBase64(message []byte) string {
	return base64.StdEncoding.EncodeToString(kp.Sign(message))
}

// FormatPublicKey encodes a public key as Base64 for transmission
func FormatPublicKey(pub ed25519.PublicKey) string {
	return base64.StdEncoding.EncodeToString(pub)
}

// ParsePublicKey decodes a Base64 public key
func ParsePublicKey(s string) (ed25519.PublicKey, error) {
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	if len(data) != ed25519.PublicKeySize {
		return nil, errors.New("invalid public key size")
	}
	return ed25519.PublicKey(data), nil
}

// VerifySignature verifies a signature against a public key and message
func VerifySignature(publicKey ed25519.PublicKey, message, signature []byte) bool {
	if len(publicKey) != ed25519.PublicKeySize {
		return false
	}
	if len(signature) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(publicKey, message, signature)
}
