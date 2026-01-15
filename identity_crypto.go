package nara

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"

	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
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
		logrus.Warnf("❌ Invalid public key size: got %d, want %d", len(publicKey), ed25519.PublicKeySize)
		return false
	}
	if len(signature) != ed25519.SignatureSize {
		logrus.Warnf("❌ Invalid signature size: got %d, want %d", len(signature), ed25519.SignatureSize)
		return false
	}
	success := ed25519.Verify(publicKey, message, signature)
	if !success {
		logrus.Debugf("❌ Signature verification failed using public key %s for message: %s", FormatPublicKey(publicKey), string(message))
	}
	return success
}

// VerifySignatureBase64 verifies a base64-encoded signature
func VerifySignatureBase64(publicKey []byte, message []byte, signatureBase64 string) bool {
	signature, err := base64.StdEncoding.DecodeString(signatureBase64)
	if err != nil {
		logrus.Warnf("❌ Failed to decode signature: %v", err)
		return false
	}
	return VerifySignature(publicKey, message, signature)
}

// Signable is implemented by types that can produce canonical content for signing.
// This provides a unified interface for cryptographic signing across different message types.
type Signable interface {
	// SignableContent returns the canonical string representation for signing.
	// The string should be deterministic and include all fields that need authentication.
	SignableContent() string
}

// SignContent signs a Signable's content directly (no pre-hashing).
// This matches the existing signing pattern used throughout the codebase.
// Returns a base64-encoded Ed25519 signature.
func SignContent(s Signable, kp NaraKeypair) string {
	return kp.SignBase64([]byte(s.SignableContent()))
}

// VerifyContent verifies a signature against a Signable's content.
// The signature should have been created with SignContent.
func VerifyContent(s Signable, publicKey []byte, signature string) bool {
	return VerifySignatureBase64(publicKey, []byte(s.SignableContent()), signature)
}

// NOTE: Self-encryption is provided by NaraKeypair.Seal/Open.
// This uses XChaCha20-Poly1305 with a symmetric key derived from the keypair's seed.
