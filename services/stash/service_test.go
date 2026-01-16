package stash

import (
	"testing"
	"time"

	"github.com/eljojo/nara/messages"
	"github.com/eljojo/nara/runtime"
	"github.com/eljojo/nara/types"
)

// TestStashStoreAndAck tests the store request → ack flow.
func TestStashStoreAndAck(t *testing.T) {
	runtime.ClearBehaviors() // Clear global registry for test isolation

	// Create mock runtimes for Alice (owner) and Bob (confidant)
	aliceRT := runtime.NewMockRuntime(t, "alice", "alice-id-123")
	bobRT := runtime.NewMockRuntime(t, "bob", "bob-id-456")

	// Create stash services for both
	aliceStash := NewService()
	bobStash := NewService()

	// Register behaviors with runtimes (before Init)
	aliceStash.RegisterBehaviors(aliceRT)
	bobStash.RegisterBehaviors(bobRT)

	// Initialize services
	if err := aliceStash.Init(aliceRT, aliceRT.Log("stash")); err != nil {
		t.Fatalf("failed to init alice stash: %v", err)
	}
	if err := bobStash.Init(bobRT, bobRT.Log("stash")); err != nil {
		t.Fatalf("failed to init bob stash: %v", err)
	}

	// Start services
	if err := aliceStash.Start(); err != nil {
		t.Fatalf("failed to start alice stash: %v", err)
	}
	if err := bobStash.Start(); err != nil {
		t.Fatalf("failed to start bob stash: %v", err)
	}
	defer func() { _ = aliceStash.Stop() }()
	defer func() { _ = bobStash.Stop() }()

	// Alice stores data with Bob (in goroutine since it blocks)
	testData := []byte("secret data for bob to hold")
	done := make(chan error, 1)

	go func() {
		err := aliceStash.StoreWith("bob-id-456", testData)
		done <- err
	}()

	// Wait for Alice to emit the store request
	time.Sleep(50 * time.Millisecond)

	// Verify Alice emitted stash:store
	if aliceRT.EmittedCount() != 1 {
		t.Fatalf("expected 1 emitted message from alice, got %d", aliceRT.EmittedCount())
	}

	storeMsg := aliceRT.LastEmitted()
	if storeMsg.Kind != "stash:store" {
		t.Fatalf("expected stash:store, got %s", storeMsg.Kind)
	}
	if storeMsg.ToID != "bob-id-456" {
		t.Fatalf("expected ToID=bob-id-456, got %s", storeMsg.ToID)
	}

	// Deliver the store message to Bob
	bobRT.Deliver(storeMsg)

	// Bob should emit a stash:ack
	if bobRT.EmittedCount() != 1 {
		t.Fatalf("expected 1 emitted message from bob, got %d", bobRT.EmittedCount())
	}

	ackMsg := bobRT.LastEmitted()
	if ackMsg.Kind != "stash:ack" {
		t.Fatalf("expected stash:ack, got %s", ackMsg.Kind)
	}
	if ackMsg.ToID != "alice-id-123" {
		t.Fatalf("expected ToID=alice-id-123, got %s", ackMsg.ToID)
	}
	if ackMsg.InReplyTo != storeMsg.ID {
		t.Fatalf("expected InReplyTo=%s, got %s", storeMsg.ID, ackMsg.InReplyTo)
	}

	// Verify Bob stored the stash
	if !bobStash.HasStashFor("alice-id-123") {
		t.Fatal("bob should have stash for alice")
	}

	stored := bobStash.retrieve("alice-id-123")
	if stored == nil {
		t.Fatal("stored stash is nil")
	}
	if stored.OwnerID != "alice-id-123" {
		t.Fatalf("expected OwnerID=alice-id-123, got %s", stored.OwnerID)
	}
	if len(stored.Nonce) != 24 {
		t.Fatalf("expected 24-byte nonce, got %d", len(stored.Nonce))
	}
	if len(stored.Ciphertext) == 0 {
		t.Fatal("ciphertext is empty")
	}

	// Deliver the ack back to Alice
	aliceRT.Deliver(ackMsg)

	// Alice's StoreWith should complete
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("StoreWith failed: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("StoreWith timed out")
	}
}

// TestStashRequestAndResponse tests the request → response flow.
func TestStashRequestAndResponse(t *testing.T) {
	runtime.ClearBehaviors() // Clear global registry for test isolation

	// Create mock runtimes
	aliceRT := runtime.NewMockRuntime(t, "alice", "alice-id-123")
	bobRT := runtime.NewMockRuntime(t, "bob", "bob-id-456")

	// Create stash services
	aliceStash := NewService()
	bobStash := NewService()

	// Register behaviors with runtimes (before Init)
	aliceStash.RegisterBehaviors(aliceRT)
	bobStash.RegisterBehaviors(bobRT)

	// Initialize
	if err := aliceStash.Init(aliceRT, aliceRT.Log("stash")); err != nil {
		t.Fatalf("failed to init alice stash: %v", err)
	}
	if err := bobStash.Init(bobRT, bobRT.Log("stash")); err != nil {
		t.Fatalf("failed to init bob stash: %v", err)
	}

	// Start
	if err := aliceStash.Start(); err != nil {
		t.Fatalf("failed to start alice stash: %v", err)
	}
	if err := bobStash.Start(); err != nil {
		t.Fatalf("failed to start bob stash: %v", err)
	}
	defer func() { _ = aliceStash.Stop() }()
	defer func() { _ = bobStash.Stop() }()

	// Bob manually stores a stash for Alice (simulating previous store)
	// Use Alice's runtime for encryption (since only Alice can decrypt)
	testData := []byte("alice's secret data")
	nonce, ciphertext, err := aliceRT.Seal(testData)
	if err != nil {
		t.Fatalf("failed to encrypt: %v", err)
	}

	bobStash.store("alice-id-123", nonce, ciphertext)

	// Alice requests her stash from Bob (in goroutine)
	done := make(chan []byte, 1)
	errChan := make(chan error, 1)

	go func() {
		data, err := aliceStash.RequestFrom("bob-id-456")
		if err != nil {
			errChan <- err
			return
		}
		done <- data
	}()

	// Wait for Alice to emit the request
	time.Sleep(50 * time.Millisecond)

	// Verify Alice emitted stash:request
	if aliceRT.EmittedCount() != 1 {
		t.Fatalf("expected 1 emitted message from alice, got %d", aliceRT.EmittedCount())
	}

	requestMsg := aliceRT.LastEmitted()
	if requestMsg.Kind != "stash:request" {
		t.Fatalf("expected stash:request, got %s", requestMsg.Kind)
	}
	if requestMsg.ToID != "bob-id-456" {
		t.Fatalf("expected ToID=bob-id-456, got %s", requestMsg.ToID)
	}

	// Deliver the request to Bob
	bobRT.Deliver(requestMsg)

	// Bob should emit a stash:response
	if bobRT.EmittedCount() != 1 {
		t.Fatalf("expected 1 emitted message from bob, got %d", bobRT.EmittedCount())
	}

	responseMsg := bobRT.LastEmitted()
	if responseMsg.Kind != "stash:response" {
		t.Fatalf("expected stash:response, got %s", responseMsg.Kind)
	}
	if responseMsg.ToID != "alice-id-123" {
		t.Fatalf("expected ToID=alice-id-123, got %s", responseMsg.ToID)
	}
	if responseMsg.InReplyTo != requestMsg.ID {
		t.Fatalf("expected InReplyTo=%s, got %s", requestMsg.ID, responseMsg.InReplyTo)
	}

	// Verify response payload
	respPayload, ok := responseMsg.Payload.(*messages.StashResponsePayload)
	if !ok {
		t.Fatalf("expected StashResponsePayload, got %T", responseMsg.Payload)
	}
	if !respPayload.Found {
		t.Fatal("expected Found=true")
	}
	if respPayload.OwnerID != "alice-id-123" {
		t.Fatalf("expected OwnerID=alice-id-123, got %s", respPayload.OwnerID)
	}

	// Deliver the response back to Alice
	aliceRT.Deliver(responseMsg)

	// Alice's RequestFrom should complete with decrypted data
	select {
	case data := <-done:
		if string(data) != string(testData) {
			t.Fatalf("expected data=%q, got %q", testData, data)
		}
	case err := <-errChan:
		t.Fatalf("RequestFrom failed: %v", err)
	case <-time.After(1 * time.Second):
		t.Fatal("RequestFrom timed out")
	}
}

// TestStashRequestNotFound tests requesting a stash that doesn't exist.
func TestStashRequestNotFound(t *testing.T) {
	runtime.ClearBehaviors() // Clear global registry for test isolation

	// Create mock runtimes
	aliceRT := runtime.NewMockRuntime(t, "alice", "alice-id-123")
	bobRT := runtime.NewMockRuntime(t, "bob", "bob-id-456")

	// Create stash services
	aliceStash := NewService()
	bobStash := NewService()

	// Register behaviors with runtimes (before Init)
	aliceStash.RegisterBehaviors(aliceRT)
	bobStash.RegisterBehaviors(bobRT)

	// Initialize
	if err := aliceStash.Init(aliceRT, aliceRT.Log("stash")); err != nil {
		t.Fatalf("failed to init alice stash: %v", err)
	}
	if err := bobStash.Init(bobRT, bobRT.Log("stash")); err != nil {
		t.Fatalf("failed to init bob stash: %v", err)
	}

	// Start
	if err := aliceStash.Start(); err != nil {
		t.Fatalf("failed to start alice stash: %v", err)
	}
	if err := bobStash.Start(); err != nil {
		t.Fatalf("failed to start bob stash: %v", err)
	}
	defer func() { _ = aliceStash.Stop() }()
	defer func() { _ = bobStash.Stop() }()

	// Alice requests her stash from Bob (but Bob doesn't have it)
	done := make(chan error, 1)

	go func() {
		_, err := aliceStash.RequestFrom("bob-id-456")
		done <- err
	}()

	// Wait for Alice to emit the request
	time.Sleep(50 * time.Millisecond)

	requestMsg := aliceRT.LastEmitted()

	// Deliver the request to Bob
	bobRT.Deliver(requestMsg)

	// Bob should emit a stash:response with Found=false
	responseMsg := bobRT.LastEmitted()
	respPayload, ok := responseMsg.Payload.(*messages.StashResponsePayload)
	if !ok {
		t.Fatalf("expected StashResponsePayload, got %T", responseMsg.Payload)
	}
	if respPayload.Found {
		t.Fatal("expected Found=false")
	}

	// Deliver the response back to Alice
	aliceRT.Deliver(responseMsg)

	// Alice's RequestFrom should return an error
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if err.Error() != "confidant bob-id-456 has no stash for us" {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("RequestFrom timed out")
	}
}

// TestStashStateMarshaling tests MarshalState and UnmarshalState.
func TestStashStateMarshaling(t *testing.T) {
	rt := runtime.NewMockRuntime(t, "test", "test-id-123")

	svc := NewService()
	if err := svc.Init(rt, rt.Log("stash")); err != nil {
		t.Fatalf("failed to init: %v", err)
	}

	// Set some state
	svc.SetConfidants([]types.NaraID{"conf-1", "conf-2", "conf-3"})
	svc.store("owner-1", []byte("nonce1"), []byte("cipher1"))
	svc.store("owner-2", []byte("nonce2"), []byte("cipher2"))

	// Marshal state
	data, err := svc.MarshalState()
	if err != nil {
		t.Fatalf("MarshalState failed: %v", err)
	}

	// Create new service and unmarshal
	svc2 := NewService()
	if err := svc2.Init(rt, rt.Log("stash")); err != nil {
		t.Fatalf("failed to init svc2: %v", err)
	}

	if err := svc2.UnmarshalState(data); err != nil {
		t.Fatalf("UnmarshalState failed: %v", err)
	}

	// Verify confidants
	if len(svc2.confidants) != 3 {
		t.Fatalf("expected 3 confidants, got %d", len(svc2.confidants))
	}
	if svc2.confidants[0] != "conf-1" || svc2.confidants[1] != "conf-2" || svc2.confidants[2] != "conf-3" {
		t.Fatalf("confidants mismatch: %v", svc2.confidants)
	}

	// Verify stored stashes
	if !svc2.HasStashFor("owner-1") {
		t.Fatal("expected stash for owner-1")
	}
	if !svc2.HasStashFor("owner-2") {
		t.Fatal("expected stash for owner-2")
	}

	stash1 := svc2.retrieve("owner-1")
	if string(stash1.Ciphertext) != "cipher1" {
		t.Fatalf("expected cipher1, got %s", stash1.Ciphertext)
	}

	stash2 := svc2.retrieve("owner-2")
	if string(stash2.Ciphertext) != "cipher2" {
		t.Fatalf("expected cipher2, got %s", stash2.Ciphertext)
	}
}

// TestStashEncryptionDecryption tests the encryption/decryption flow via runtime.
func TestStashEncryptionDecryption(t *testing.T) {
	// Create two runtimes with different keypairs
	aliceRT := runtime.NewMockRuntime(t, "alice", "alice-id-123")
	bobRT := runtime.NewMockRuntime(t, "bob", "bob-id-456")

	plaintext := []byte("this is a secret message for encryption")

	// Encrypt with Alice's keypair
	nonce, ciphertext, err := aliceRT.Seal(plaintext)
	if err != nil {
		t.Fatalf("Seal failed: %v", err)
	}

	if len(nonce) != 24 {
		t.Fatalf("expected 24-byte nonce, got %d", len(nonce))
	}

	if len(ciphertext) < len(plaintext) {
		t.Fatalf("ciphertext too short (should include auth tag)")
	}

	// Decrypt with Alice's keypair (owner can decrypt)
	decrypted, err := aliceRT.Open(nonce, ciphertext)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Fatalf("decrypted mismatch: expected %q, got %q", plaintext, decrypted)
	}

	// Test wrong nonce
	wrongNonce := make([]byte, 24)
	_, err = aliceRT.Open(wrongNonce, ciphertext)
	if err == nil {
		t.Fatal("expected decryption to fail with wrong nonce")
	}

	// Test wrong key (Bob can't decrypt Alice's data)
	_, err = bobRT.Open(nonce, ciphertext)
	if err == nil {
		t.Fatal("expected decryption to fail with wrong key")
	}
}

// TestStashRefreshHandler tests handleRefreshV1.
// NOTE: Temporarily disabled - needs proper MQTT integration for refresh broadcasts
// func TestStashRefreshHandler(t *testing.T) {
// 	aliceRT := runtime.NewMockRuntime(t, "alice", "alice-id-123")
// 	bobRT := runtime.NewMockRuntime(t, "bob", "bob-id-456")
//
// 	aliceStash := NewService()
// 	if err := aliceStash.Init(aliceRT); err != nil {
// 		t.Fatalf("failed to init alice stash: %v", err)
// 	}
// 	if err := aliceStash.Start(); err != nil {
// 		t.Fatalf("failed to start alice stash: %v", err)
// 	}
// 	defer aliceStash.Stop()
//
// 	// Alice stores a stash for Bob
// 	aliceStash.store("bob-id-456", []byte("nonce123"), []byte("ciphertext123"))
//
// 	// Bob broadcasts a refresh request
// 	refreshMsg := &runtime.Message{
// 		Kind:   "stash-refresh",
// 		FromID: "bob-id-456",
// 		Payload: &messages.StashRefreshPayload{
// 			OwnerID: "bob-id-456",
// 		},
// 	}
//
// 	// Deliver refresh to Alice
// 	aliceRT.Deliver(refreshMsg)
//
// 	// Alice should emit a stash:response with the stored data
// 	if aliceRT.EmittedCount() != 1 {
// 		t.Fatalf("expected 1 emitted message, got %d", aliceRT.EmittedCount())
// 	}
//
// 	responseMsg := aliceRT.LastEmitted()
// 	if responseMsg.Kind != "stash:response" {
// 		t.Fatalf("expected stash:response, got %s", responseMsg.Kind)
// 	}
// 	if responseMsg.ToID != "bob-id-456" {
// 		t.Fatalf("expected ToID=bob-id-456, got %s", responseMsg.ToID)
// 	}
//
// 	respPayload, ok := responseMsg.Payload.(*messages.StashResponsePayload)
// 	if !ok {
// 		t.Fatalf("expected StashResponsePayload, got %T", responseMsg.Payload)
// 	}
// 	if !respPayload.Found {
// 		t.Fatal("expected Found=true")
// 	}
// 	if string(respPayload.Ciphertext) != "ciphertext123" {
// 		t.Fatalf("expected ciphertext123, got %s", respPayload.Ciphertext)
// 	}
// }

// TestStashInvalidPayloads tests validation of payloads.
func TestStashInvalidPayloads(t *testing.T) {
	rt := runtime.NewMockRuntime(t, "alice", "alice-id-123")

	svc := NewService()
	svc.RegisterBehaviors(rt) // Register behaviors before Init
	if err := svc.Init(rt, rt.Log("stash")); err != nil {
		t.Fatalf("failed to init: %v", err)
	}
	if err := svc.Start(); err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	defer func() { _ = svc.Stop() }()

	// Test store with empty OwnerID
	storeMsg := &runtime.Message{
		Kind:    "stash:store",
		Version: 1,
		FromID:  "bob-id-456",
		ToID:    "alice-id-123",
		Payload: &messages.StashStorePayload{
			OwnerID:    "", // Invalid
			Nonce:      []byte("nonce"),
			Ciphertext: []byte("cipher"),
			Timestamp:  time.Now().Unix(),
		},
	}

	rt.Deliver(storeMsg)

	// Service should emit a failure ack
	if rt.EmittedCount() != 1 {
		t.Fatalf("expected 1 emitted message, got %d", rt.EmittedCount())
	}

	ackMsg := rt.LastEmitted()
	if ackMsg.Kind != "stash:ack" {
		t.Fatalf("expected stash:ack, got %s", ackMsg.Kind)
	}

	ackPayload, ok := ackMsg.Payload.(*messages.StashStoreAck)
	if !ok {
		t.Fatalf("expected StashStoreAck, got %T", ackMsg.Payload)
	}
	if ackPayload.Success {
		t.Fatal("expected Success=false for invalid payload")
	}
	if ackPayload.Reason == "" {
		t.Fatal("expected Reason to be set")
	}
}
