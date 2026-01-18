package keyring

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"github.com/eljojo/nara/identity"
)

func TestKeyring_Identity(t *testing.T) {
	// Generate a keypair
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	// Wrap in NaraKeypair
	keypair := identity.NaraKeypair{
		PrivateKey: priv,
		PublicKey:  pub,
	}

	kr := New(keypair, "test-nara-id")

	// Verify identity methods
	if kr.MyID() != "test-nara-id" {
		t.Errorf("MyID() = %q, want %q", kr.MyID(), "test-nara-id")
	}

	if string(kr.MyPublicKey()) != string(pub) {
		t.Error("MyPublicKey() doesn't match original public key")
	}

	// Verify base64 encoding works
	encoded := kr.MyPublicKeyBase64()
	if encoded == "" {
		t.Error("MyPublicKeyBase64() returned empty string")
	}
}

func TestKeyring_SignAndVerify(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	keypair := identity.NaraKeypair{
		PrivateKey: priv,
		PublicKey:  pub,
	}
	kr := New(keypair, "signer-id")

	// Register our own key for verification
	kr.Register("signer-id", pub)

	// Sign some data
	data := []byte("hello world")
	sig := kr.Sign(data)

	// Verify with own ID
	if !kr.Verify("signer-id", data, sig) {
		t.Error("Verify() failed for own signature")
	}

	// Verify wrong data fails
	if kr.Verify("signer-id", []byte("wrong data"), sig) {
		t.Error("Verify() should fail for wrong data")
	}

	// Verify unknown ID fails
	if kr.Verify("unknown-id", data, sig) {
		t.Error("Verify() should fail for unknown ID")
	}
}

func TestKeyring_RegisterAndLookup(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	keypair := identity.NaraKeypair{
		PrivateKey: priv,
		PublicKey:  pub,
	}
	kr := New(keypair, "my-id")

	// Generate another keypair for a peer
	peerPub, _, _ := ed25519.GenerateKey(rand.Reader)

	// Register the peer
	kr.Register("peer-id", peerPub)

	// Lookup should work
	if got := kr.Lookup("peer-id"); string(got) != string(peerPub) {
		t.Error("Lookup() didn't return registered key")
	}

	// Has should work
	if !kr.Has("peer-id") {
		t.Error("Has() should return true for registered peer")
	}

	// Unknown ID should return nil
	if kr.Lookup("unknown") != nil {
		t.Error("Lookup() should return nil for unknown ID")
	}

	// Count should be 1
	if kr.Count() != 1 {
		t.Errorf("Count() = %d, want 1", kr.Count())
	}

	// Looking up self should return our key
	if kr.Lookup("my-id") == nil {
		t.Error("Lookup() should return our own key")
	}
}

func TestKeyring_RegisterBase64(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	keypair := identity.NaraKeypair{
		PrivateKey: priv,
		PublicKey:  pub,
	}
	kr := New(keypair, "my-id")

	// Generate peer keypair and encode
	peerPub, _, _ := ed25519.GenerateKey(rand.Reader)
	encoded := identity.FormatPublicKey(peerPub)

	// Register via base64
	if err := kr.RegisterBase64("peer-id", encoded); err != nil {
		t.Fatalf("RegisterBase64() failed: %v", err)
	}

	// Lookup should work
	if got := kr.Lookup("peer-id"); string(got) != string(peerPub) {
		t.Error("Lookup() didn't return key registered via base64")
	}

	// Invalid base64 should error
	if err := kr.RegisterBase64("bad-peer", "not-valid-base64!!!"); err == nil {
		t.Error("RegisterBase64() should fail for invalid base64")
	}
}

// NOTE: Encryption is now handled by NaraKeypair.Seal/Open in the main package.
// See identity_crypto.go for encryption tests.

func TestParsePublicKey(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	encoded := identity.FormatPublicKey(pub)

	parsed, err := identity.ParsePublicKey(encoded)
	if err != nil {
		t.Fatalf("identity.ParsePublicKey() failed: %v", err)
	}

	if string(parsed) != string(pub) {
		t.Error("identity.ParsePublicKey() didn't return original key")
	}

	// Invalid input
	if _, err := identity.ParsePublicKey("invalid"); err == nil {
		t.Error("identity.ParsePublicKey() should fail for invalid input")
	}

	// Wrong size
	if _, err := identity.ParsePublicKey("YWJj"); err == nil { // "abc" in base64
		t.Error("identity.ParsePublicKey() should fail for wrong size")
	}
}
