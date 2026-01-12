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

// EncryptionKeypair holds a symmetric key derived from an Ed25519 private key
// Used for self-encryption (encrypt data that only the owner can decrypt)
type EncryptionKeypair struct {
	SymmetricKey []byte // 32-byte key for XChaCha20-Poly1305
}

// DeriveEncryptionKeys derives a symmetric encryption key from an Ed25519 private key
// Uses HKDF with SHA-256 to derive a 32-byte key for XChaCha20-Poly1305
func DeriveEncryptionKeys(privateKey ed25519.PrivateKey) EncryptionKeypair {
	// Extract the seed from the private key (first 32 bytes)
	seed := privateKey.Seed()

	// Use HKDF to derive the encryption key
	// Salt: "nara:stash:v1" (domain separation)
	// Info: "symmetric" (key purpose)
	hkdfReader := hkdf.New(sha256.New, seed, []byte("nara:stash:v1"), []byte("symmetric"))

	symmetricKey := make([]byte, 32)
	if _, err := io.ReadFull(hkdfReader, symmetricKey); err != nil {
		// This should never happen with HKDF
		panic("hkdf failed: " + err.Error())
	}

	return EncryptionKeypair{
		SymmetricKey: symmetricKey,
	}
}

// EncryptForSelf encrypts plaintext using XChaCha20-Poly1305 with a random nonce
// Returns the nonce and ciphertext separately so the nonce can be stored with the payload
func (kp EncryptionKeypair) EncryptForSelf(plaintext []byte) (nonce, ciphertext []byte, err error) {
	aead, err := chacha20poly1305.NewX(kp.SymmetricKey)
	if err != nil {
		return nil, nil, err
	}

	// Generate random nonce (24 bytes for XChaCha20)
	nonce = make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}

	// Encrypt (ciphertext includes auth tag)
	ciphertext = aead.Seal(nil, nonce, plaintext, nil)

	return nonce, ciphertext, nil
}

// DecryptForSelf decrypts ciphertext using XChaCha20-Poly1305
func (kp EncryptionKeypair) DecryptForSelf(nonce, ciphertext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(kp.SymmetricKey)
	if err != nil {
		return nil, err
	}

	if len(nonce) != aead.NonceSize() {
		return nil, errors.New("invalid nonce size")
	}

	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errors.New("decryption failed: invalid ciphertext or wrong key")
	}

	return plaintext, nil
}
