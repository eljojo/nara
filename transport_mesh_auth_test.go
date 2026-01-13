package nara

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestMeshAuthRequest_SignsCorrectly(t *testing.T) {
	// Create a keypair
	pub, priv, _ := ed25519.GenerateKey(nil)
	keypair := NaraKeypair{PrivateKey: priv, PublicKey: pub}

	headers := MeshAuthRequest("alice", keypair, "POST", "/events/sync")

	if headers.Get(HeaderNaraName) != "alice" {
		t.Errorf("expected name alice, got %s", headers.Get(HeaderNaraName))
	}

	if headers.Get(HeaderNaraTimestamp) == "" {
		t.Error("expected timestamp header")
	}

	if headers.Get(HeaderNaraSignature) == "" {
		t.Error("expected signature header")
	}
}

func TestMeshAuthResponse_SignsCorrectly(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	keypair := NaraKeypair{PrivateKey: priv, PublicKey: pub}

	body := []byte(`{"events": []}`)
	headers := MeshAuthResponse("bob", keypair, body)

	if headers.Get(HeaderNaraName) != "bob" {
		t.Errorf("expected name bob, got %s", headers.Get(HeaderNaraName))
	}

	if headers.Get(HeaderNaraSignature) == "" {
		t.Error("expected signature header")
	}
}

func TestVerifyMeshRequest_ValidSignature(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	keypair := NaraKeypair{PrivateKey: priv, PublicKey: pub}

	// Create signed request
	headers := MeshAuthRequest("alice", keypair, "POST", "/events/sync")

	req := httptest.NewRequest("POST", "/events/sync", nil)
	for k, v := range headers {
		req.Header[k] = v
	}

	// Mock public key lookup
	getPublicKey := func(name string) []byte {
		if name == "alice" {
			return pub
		}
		return nil
	}

	sender, err := VerifyMeshRequest(req, getPublicKey)
	if err != nil {
		t.Errorf("expected valid signature, got error: %v", err)
	}
	if sender != "alice" {
		t.Errorf("expected sender alice, got %s", sender)
	}
}

func TestVerifyMeshRequest_InvalidSignature(t *testing.T) {
	pub1, priv1, _ := ed25519.GenerateKey(nil)
	pub2, _, _ := ed25519.GenerateKey(nil)
	keypair := NaraKeypair{PrivateKey: priv1, PublicKey: pub1}

	// Sign with keypair1
	headers := MeshAuthRequest("alice", keypair, "POST", "/events/sync")

	req := httptest.NewRequest("POST", "/events/sync", nil)
	for k, v := range headers {
		req.Header[k] = v
	}

	// But verify against different public key
	getPublicKey := func(name string) []byte {
		if name == "alice" {
			return pub2 // Wrong key!
		}
		return nil
	}

	_, err := VerifyMeshRequest(req, getPublicKey)
	if err == nil {
		t.Error("expected signature verification to fail")
	}
}

func TestVerifyMeshRequest_MissingHeaders(t *testing.T) {
	req := httptest.NewRequest("POST", "/events/sync", nil)
	// No auth headers

	getPublicKey := func(name string) []byte { return nil }

	_, err := VerifyMeshRequest(req, getPublicKey)
	if err == nil {
		t.Error("expected error for missing headers")
	}
}

func TestVerifyMeshRequest_ExpiredTimestamp(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	keypair := NaraKeypair{PrivateKey: priv, PublicKey: pub}

	// Create request with old timestamp
	oldTimestamp := time.Now().Add(-1 * time.Minute).UnixMilli()
	message := "alice" + strconv.FormatInt(oldTimestamp, 10) + "POST" + "/events/sync"
	signature := keypair.Sign([]byte(message))

	req := httptest.NewRequest("POST", "/events/sync", nil)
	req.Header.Set(HeaderNaraName, "alice")
	req.Header.Set(HeaderNaraTimestamp, strconv.FormatInt(oldTimestamp, 10))
	req.Header.Set(HeaderNaraSignature, base64.StdEncoding.EncodeToString(signature))

	getPublicKey := func(name string) []byte {
		if name == "alice" {
			return pub
		}
		return nil
	}

	_, err := VerifyMeshRequest(req, getPublicKey)
	if err == nil {
		t.Error("expected error for expired timestamp")
	}
}

func TestVerifyMeshRequest_UnknownSender(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	keypair := NaraKeypair{PrivateKey: priv, PublicKey: pub}

	headers := MeshAuthRequest("stranger", keypair, "POST", "/events/sync")

	req := httptest.NewRequest("POST", "/events/sync", nil)
	for k, v := range headers {
		req.Header[k] = v
	}

	// Return nil for unknown naras
	getPublicKey := func(name string) []byte {
		return nil // Don't know this nara
	}

	_, err := VerifyMeshRequest(req, getPublicKey)
	if err == nil {
		t.Error("expected error for unknown sender")
	}
}

func TestVerifyMeshResponse_ValidSignature(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	keypair := NaraKeypair{PrivateKey: priv, PublicKey: pub}

	body := []byte(`{"status": "ok"}`)
	headers := MeshAuthResponse("bob", keypair, body)

	resp := &http.Response{
		Header: headers,
		Body:   io.NopCloser(bytes.NewReader(body)),
	}

	getPublicKey := func(name string) []byte {
		if name == "bob" {
			return pub
		}
		return nil
	}

	sender, err := VerifyMeshResponse(resp, body, getPublicKey)
	if err != nil {
		t.Errorf("expected valid response signature, got error: %v", err)
	}
	if sender != "bob" {
		t.Errorf("expected sender bob, got %s", sender)
	}
}

func TestVerifyMeshResponse_TamperedBody(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	keypair := NaraKeypair{PrivateKey: priv, PublicKey: pub}

	originalBody := []byte(`{"status": "ok"}`)
	headers := MeshAuthResponse("bob", keypair, originalBody)

	// Tamper with the body
	tamperedBody := []byte(`{"status": "hacked"}`)

	resp := &http.Response{
		Header: headers,
		Body:   io.NopCloser(bytes.NewReader(tamperedBody)),
	}

	getPublicKey := func(name string) []byte {
		if name == "bob" {
			return pub
		}
		return nil
	}

	_, err := VerifyMeshResponse(resp, tamperedBody, getPublicKey)
	if err == nil {
		t.Error("expected signature verification to fail for tampered body")
	}
}

func TestMeshAuthMiddleware_SkipsPing(t *testing.T) {
	ln := testLocalNara("test")
	network := ln.Network
	defer network.Shutdown()

	called := false
	handler := func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Write([]byte("pong"))
	}

	wrapped := network.meshAuthMiddleware("/ping", handler)

	req := httptest.NewRequest("GET", "/ping", nil)
	// No auth headers - should still work for /ping
	w := httptest.NewRecorder()

	wrapped(w, req)

	if !called {
		t.Error("expected /ping handler to be called without auth")
	}
}

func TestMeshAuthMiddleware_RejectsUnauthenticated(t *testing.T) {
	// Ensure logrus is properly initialized to avoid nil pointer panics
	logrus.SetOutput(os.Stderr)

	ln := testLocalNara("test")
	network := ln.Network
	defer network.Shutdown()

	called := false
	handler := func(w http.ResponseWriter, r *http.Request) {
		called = true
	}

	wrapped := network.meshAuthMiddleware("/events/sync", handler)

	req := httptest.NewRequest("POST", "/events/sync", nil)
	// No auth headers
	w := httptest.NewRecorder()

	wrapped(w, req)

	if called {
		t.Error("expected handler NOT to be called without auth")
	}

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d", w.Code)
	}
}

func testMeshSoul(name string) string {
	hw := hashBytes([]byte("mesh-test-hardware-" + name))
	soul := NativeSoulCustom(hw, name)
	return FormatSoul(soul)
}

func TestMeshAuthMiddleware_AcceptsValidAuth(t *testing.T) {
	// Create two naras with valid souls
	aliceSoul := testMeshSoul("alice")
	bobSoul := testMeshSoul("bob")

	ln1 := testLocalNaraWithSoul("alice", aliceSoul)
	defer ln1.Network.Shutdown()
	ln2 := testLocalNaraWithSoul("bob", bobSoul)
	defer ln2.Network.Shutdown()

	// Bob imports Alice so he knows her public key
	alice := NewNara("alice")
	alice.Status.PublicKey = ln1.Me.Status.PublicKey
	ln2.Network.importNara(alice)

	called := false
	var verifiedSender string
	handler := func(w http.ResponseWriter, r *http.Request) {
		called = true
		verifiedSender = r.Header.Get("X-Nara-Verified")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok": true}`))
	}

	wrapped := ln2.Network.meshAuthMiddleware("/events/sync", handler)

	// Create authenticated request from Alice
	req := httptest.NewRequest("POST", "/events/sync", nil)
	headers := MeshAuthRequest("alice", ln1.Keypair, "POST", "/events/sync")
	for k, v := range headers {
		req.Header[k] = v
	}

	w := httptest.NewRecorder()
	wrapped(w, req)

	if !called {
		t.Error("expected handler to be called with valid auth")
	}

	if verifiedSender != "alice" {
		t.Errorf("expected verified sender alice, got %s", verifiedSender)
	}

	// Check response has auth headers
	if w.Header().Get(HeaderNaraName) != "bob" {
		t.Errorf("expected response signed by bob, got %s", w.Header().Get(HeaderNaraName))
	}

	if w.Header().Get(HeaderNaraSignature) == "" {
		t.Error("expected response to have signature")
	}
}
