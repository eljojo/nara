// Package keyring provides unified cryptographic identity management.
//
// Keyring is the central place for:
//   - Signing (our identity signs things)
//   - Verification (verify others' signatures)
//   - Key storage (register and lookup public keys)
//
// Architecture:
//   - NaraKeypair (in main package): Signing + Encryption/Decryption
//   - Keyring: Key management (register, lookup, verify) + signing for convenience
//
// Encryption is handled by NaraKeypair.Seal/Open, not Keyring.
// Services depend on Keyring for key lookups.
package keyring

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"github.com/eljojo/nara/types"
	"sync"
)

// Keyring manages cryptographic identity and keys.
type Keyring struct {
	// Our identity
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
	myID       types.NaraID

	// Others' keys (cached, already parsed)
	keys map[types.NaraID]ed25519.PublicKey // id -> pubkey
	mu   sync.RWMutex
}

// New creates a new Keyring from an Ed25519 private key and our ID.
func New(privateKey ed25519.PrivateKey, myID types.NaraID) *Keyring {
	publicKey := privateKey.Public().(ed25519.PublicKey)

	return &Keyring{
		privateKey: privateKey,
		publicKey:  publicKey,
		myID:       myID,
		keys:       make(map[types.NaraID]ed25519.PublicKey),
	}
}

// === Our Identity ===

// MyID returns our nara ID.
func (k *Keyring) MyID() types.NaraID {
	return k.myID
}

// MyPublicKey returns our public key.
func (k *Keyring) MyPublicKey() ed25519.PublicKey {
	return k.publicKey
}

// MyPublicKeyBase64 returns our public key as a base64 string.
func (k *Keyring) MyPublicKeyBase64() string {
	return base64.StdEncoding.EncodeToString(k.publicKey)
}

// Sign signs data with our private key.
func (k *Keyring) Sign(data []byte) []byte {
	return ed25519.Sign(k.privateKey, data)
}

// SignBase64 signs data and returns a base64-encoded signature.
func (k *Keyring) SignBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(k.Sign(data))
}

// === Others' Keys ===

// Register stores a public key for a nara ID.
// If the key is already registered, this is a no-op.
func (k *Keyring) Register(id types.NaraID, pubkey ed25519.PublicKey) {
	if id == "" || len(pubkey) == 0 {
		return
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.keys[id] = pubkey
}

// RegisterBase64 parses and stores a base64-encoded public key.
func (k *Keyring) RegisterBase64(id types.NaraID, pubkeyBase64 string) error {
	if id == "" || pubkeyBase64 == "" {
		return nil
	}
	pubkey, err := base64.StdEncoding.DecodeString(pubkeyBase64)
	if err != nil {
		return err
	}
	if len(pubkey) != ed25519.PublicKeySize {
		return errors.New("invalid public key size")
	}
	k.Register(id, pubkey)
	return nil
}

// Lookup returns the public key for a nara ID, or nil if unknown.
// Also checks if the ID is our own.
func (k *Keyring) Lookup(id types.NaraID) ed25519.PublicKey {
	if id == "" {
		return nil
	}
	if id == k.myID {
		return k.publicKey
	}
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.keys[id]
}

// Has returns true if we have a public key for the given ID.
func (k *Keyring) Has(id types.NaraID) bool {
	return k.Lookup(id) != nil
}

// Count returns the number of registered keys (excluding our own).
func (k *Keyring) Count() int {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return len(k.keys)
}

// === Verification ===

// Verify verifies a signature from a nara ID.
// Returns false if the ID is unknown or the signature is invalid.
func (k *Keyring) Verify(id types.NaraID, data, signature []byte) bool {
	pubkey := k.Lookup(id)
	if pubkey == nil {
		return false
	}
	if len(signature) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(pubkey, data, signature)
}

// VerifyBase64 verifies a base64-encoded signature from a nara ID.
func (k *Keyring) VerifyBase64(id types.NaraID, data []byte, signatureBase64 string) bool {
	signature, err := base64.StdEncoding.DecodeString(signatureBase64)
	if err != nil {
		return false
	}
	return k.Verify(id, data, signature)
}

// VerifyWithKey verifies a signature directly with a public key (no lookup).
func (k *Keyring) VerifyWithKey(pubkey ed25519.PublicKey, data, signature []byte) bool {
	if len(pubkey) != ed25519.PublicKeySize || len(signature) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(pubkey, data, signature)
}

// === Utility ===

// ParsePublicKey decodes a base64 public key string.
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

// FormatPublicKey encodes a public key as base64.
func FormatPublicKey(pub ed25519.PublicKey) string {
	return base64.StdEncoding.EncodeToString(pub)
}
