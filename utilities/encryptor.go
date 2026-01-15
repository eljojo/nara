package utilities

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

// Encryptor provides self-encryption using XChaCha20-Poly1305.
//
// Self-encryption means encrypting data that only the owner can decrypt.
// The encryption key is derived deterministically from a seed, so the same
// seed always produces the same encryption key.
//
// This is used by stash to encrypt data before storing it with confidants.
// Only the owner can decrypt because only the owner has the seed.
type Encryptor struct {
	symmetricKey []byte // 32-byte key for XChaCha20-Poly1305
}

// NewEncryptor creates an encryptor from a 32-byte seed.
//
// The seed is typically the Ed25519 private key seed. It's deterministically
// expanded into a symmetric encryption key using HKDF with domain separation.
func NewEncryptor(seed []byte) *Encryptor {
	if len(seed) != 32 {
		panic("encryptor seed must be 32 bytes")
	}

	// Use HKDF to derive the encryption key
	// Salt: "nara:stash:v1" (domain separation)
	// Info: "symmetric" (key purpose)
	hkdfReader := hkdf.New(sha256.New, seed, []byte("nara:stash:v1"), []byte("symmetric"))

	symmetricKey := make([]byte, 32)
	if _, err := io.ReadFull(hkdfReader, symmetricKey); err != nil {
		// This should never happen with HKDF
		panic("hkdf failed: " + err.Error())
	}

	return &Encryptor{
		symmetricKey: symmetricKey,
	}
}

// Seal encrypts plaintext using XChaCha20-Poly1305 with a random nonce.
//
// Returns the nonce and ciphertext separately so the nonce can be stored
// alongside the ciphertext. The nonce is 24 bytes, and the ciphertext
// includes a 16-byte authentication tag.
func (e *Encryptor) Seal(plaintext []byte) (nonce, ciphertext []byte, err error) {
	aead, err := chacha20poly1305.NewX(e.symmetricKey)
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

// Open decrypts ciphertext using XChaCha20-Poly1305.
//
// The nonce must be the same nonce used during encryption. Returns an error
// if the ciphertext is invalid or if it was encrypted with a different key.
func (e *Encryptor) Open(nonce, ciphertext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(e.symmetricKey)
	if err != nil {
		return nil, err
	}

	if len(nonce) != aead.NonceSize() {
		return nil, errors.New("invalid nonce size (expected 24 bytes)")
	}

	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errors.New("decryption failed: invalid ciphertext or wrong key")
	}

	return plaintext, nil
}
