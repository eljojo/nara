package nara

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// Test hardware fingerprints for stash testing
var (
	stashHW1 = hashBytes([]byte("stash-test-hw-1"))
	stashHW2 = hashBytes([]byte("stash-test-hw-2"))
)

// Helper function to create StashData with test payload
func testStashData(timestamp int64, testValues []string) *StashData {
	data := map[string]interface{}{
		"values": testValues,
	}
	dataJSON, _ := json.Marshal(data)
	return &StashData{
		Timestamp: timestamp,
		Data:      dataJSON,
		Version:   1,
	}
}

// =====================================================
// Crypto Tests
// =====================================================

func TestDeriveEncryptionKeys(t *testing.T) {
	// Derive encryption keys from an Ed25519 private key
	soul := NativeSoulCustom(stashHW1, "alice")
	signingKeypair := DeriveKeypair(soul)

	encKeypair := DeriveEncryptionKeys(signingKeypair.PrivateKey)

	// Should have 32-byte symmetric key
	if len(encKeypair.SymmetricKey) != 32 {
		t.Errorf("Expected 32-byte symmetric key, got %d", len(encKeypair.SymmetricKey))
	}

	// Should be deterministic
	encKeypair2 := DeriveEncryptionKeys(signingKeypair.PrivateKey)
	if !bytes.Equal(encKeypair.SymmetricKey, encKeypair2.SymmetricKey) {
		t.Error("Encryption key derivation should be deterministic")
	}

	// Different souls should produce different keys
	otherSoul := NativeSoulCustom(stashHW2, "bob")
	otherSigningKeypair := DeriveKeypair(otherSoul)
	otherEncKeypair := DeriveEncryptionKeys(otherSigningKeypair.PrivateKey)

	if bytes.Equal(encKeypair.SymmetricKey, otherEncKeypair.SymmetricKey) {
		t.Error("Different souls should produce different encryption keys")
	}
}

func TestEncryptDecryptForSelf(t *testing.T) {
	// Create encryption keys
	soul := NativeSoulCustom(stashHW1, "alice")
	signingKeypair := DeriveKeypair(soul)
	encKeypair := DeriveEncryptionKeys(signingKeypair.PrivateKey)

	// Encrypt some data
	plaintext := []byte("hello, this is my secret stash data!")
	nonce, ciphertext, err := encKeypair.EncryptForSelf(plaintext)
	if err != nil {
		t.Fatalf("Encryption failed: %v", err)
	}

	// Nonce should be 24 bytes (XChaCha20)
	if len(nonce) != 24 {
		t.Errorf("Expected 24-byte nonce, got %d", len(nonce))
	}

	// Ciphertext should be larger than plaintext (includes auth tag)
	if len(ciphertext) <= len(plaintext) {
		t.Error("Ciphertext should be larger than plaintext")
	}

	// Decrypt should return original plaintext
	decrypted, err := encKeypair.DecryptForSelf(nonce, ciphertext)
	if err != nil {
		t.Fatalf("Decryption failed: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Error("Decrypted data should match original plaintext")
	}
}

func TestEncryptionDeterministic(t *testing.T) {
	// Same key should produce different ciphertext each time (random nonce)
	soul := NativeSoulCustom(stashHW1, "alice")
	signingKeypair := DeriveKeypair(soul)
	encKeypair := DeriveEncryptionKeys(signingKeypair.PrivateKey)

	plaintext := []byte("same data")

	nonce1, ciphertext1, _ := encKeypair.EncryptForSelf(plaintext)
	nonce2, ciphertext2, _ := encKeypair.EncryptForSelf(plaintext)

	// Nonces should be different
	if bytes.Equal(nonce1, nonce2) {
		t.Error("Each encryption should use a different random nonce")
	}

	// Ciphertexts should be different
	if bytes.Equal(ciphertext1, ciphertext2) {
		t.Error("Different nonces should produce different ciphertexts")
	}

	// But both should decrypt to the same plaintext
	decrypted1, _ := encKeypair.DecryptForSelf(nonce1, ciphertext1)
	decrypted2, _ := encKeypair.DecryptForSelf(nonce2, ciphertext2)

	if !bytes.Equal(decrypted1, decrypted2) {
		t.Error("Both should decrypt to same plaintext")
	}
}

func TestDecryptWithWrongKey(t *testing.T) {
	// Encrypt with one key, try to decrypt with another
	soul1 := NativeSoulCustom(stashHW1, "alice")
	soul2 := NativeSoulCustom(stashHW2, "bob")

	encKeypair1 := DeriveEncryptionKeys(DeriveKeypair(soul1).PrivateKey)
	encKeypair2 := DeriveEncryptionKeys(DeriveKeypair(soul2).PrivateKey)

	plaintext := []byte("secret data")
	nonce, ciphertext, _ := encKeypair1.EncryptForSelf(plaintext)

	// Decrypting with wrong key should fail
	_, err := encKeypair2.DecryptForSelf(nonce, ciphertext)
	if err == nil {
		t.Error("Decryption with wrong key should fail")
	}
}

// =====================================================
// Serialization Tests
// =====================================================

func TestStashDataSerialization(t *testing.T) {
	// Create StashData with JSON payload
	testValues := []string{"value1", "value2", "value3", "value4", "value5"}
	payload := map[string]interface{}{"values": testValues}
	dataJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to create JSON data: %v", err)
	}

	data := &StashData{
		Timestamp: time.Now().Unix(),
		Data:      dataJSON,
		Version:   1,
	}

	// Serialize
	serialized, err := data.Marshal()
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Deserialize
	parsed := &StashData{}
	err = parsed.Unmarshal(serialized)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Should match
	if parsed.Timestamp != data.Timestamp {
		t.Error("Timestamp mismatch after serialization")
	}
	if parsed.Version != data.Version {
		t.Error("Version mismatch after serialization")
	}

	// Extract values from parsed data
	var parsedPayload map[string]interface{}
	if err := json.Unmarshal(parsed.Data, &parsedPayload); err != nil {
		t.Fatalf("Failed to unmarshal parsed data: %v", err)
	}
	parsedValues := parsedPayload["values"].([]interface{})
	if len(parsedValues) != len(testValues) {
		t.Errorf("Values length mismatch: expected %d, got %d", len(testValues), len(parsedValues))
	}
	for i, e := range testValues {
		if parsedValues[i].(string) != e {
			t.Errorf("Value mismatch at %d: expected %s, got %s", i, e, parsedValues[i])
		}
	}
}

func TestStashDataTimestampPreserved(t *testing.T) {
	// The timestamp must survive full encrypt/decrypt cycle
	soul := NativeSoulCustom(stashHW1, "alice")
	encKeypair := DeriveEncryptionKeys(DeriveKeypair(soul).PrivateKey)

	originalTimestamp := int64(1234567890)
	dataJSON, _ := json.Marshal(map[string]interface{}{"test": "data"})
	data := &StashData{
		Timestamp: originalTimestamp,
		Data:      dataJSON,
		Version:   1,
	}

	// Serialize, encrypt, decrypt, deserialize
	serialized, _ := data.Marshal()
	nonce, ciphertext, _ := encKeypair.EncryptForSelf(serialized)
	decrypted, _ := encKeypair.DecryptForSelf(nonce, ciphertext)

	parsed := &StashData{}
	if err := parsed.Unmarshal(decrypted); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if parsed.Timestamp != originalTimestamp {
		t.Errorf("Timestamp not preserved: expected %d, got %d", originalTimestamp, parsed.Timestamp)
	}
}

// =====================================================
// Confident Management Tests
// =====================================================

func TestConfidantTrackerAddRemove(t *testing.T) {
	tracker := NewConfidantTracker(3)

	// Add some confidants
	tracker.Add("bob", time.Now().Unix())
	tracker.Add("carol", time.Now().Unix())

	if tracker.Count() != 2 {
		t.Errorf("Expected 2 confidants, got %d", tracker.Count())
	}

	// Check if bob is tracked
	if !tracker.Has("bob") {
		t.Error("Should have bob")
	}

	// Remove bob
	tracker.Remove("bob")
	if tracker.Has("bob") {
		t.Error("Should not have bob after removal")
	}
	if tracker.Count() != 1 {
		t.Errorf("Expected 1 confidant after removal, got %d", tracker.Count())
	}
}

func TestConfidantTrackerTargetCount(t *testing.T) {
	tracker := NewConfidantTracker(2)

	// Add up to target
	tracker.Add("bob", time.Now().Unix())
	tracker.Add("carol", time.Now().Unix())

	if !tracker.IsSatisfied() {
		t.Error("Should be satisfied at target count")
	}

	// Add one more (over target)
	tracker.Add("dave", time.Now().Unix())

	if tracker.NeedsMore() {
		t.Error("Should not need more when at target")
	}

	// Get excess confidants
	excess := tracker.GetExcess()
	if len(excess) != 1 {
		t.Errorf("Expected 1 excess confidant, got %d", len(excess))
	}
}

func TestConfidantTrackerFailureTracking(t *testing.T) {
	tracker := NewConfidantTracker(3)

	// Mark some naras as failed
	tracker.MarkFailed("bob")
	tracker.MarkFailed("carol")

	// Create peer list
	peers := []PeerInfo{
		{Name: "bob", MemoryMode: MemoryModeHog, UptimeSecs: 1000},
		{Name: "carol", MemoryMode: MemoryModeMedium, UptimeSecs: 500},
		{Name: "dave", MemoryMode: MemoryModeShort, UptimeSecs: 100},
		{Name: "eve", MemoryMode: MemoryModeHog, UptimeSecs: 2000},
	}

	// SelectBest should skip failed naras (bob and carol)
	selected := tracker.SelectBest("alice", peers)
	if selected == "bob" || selected == "carol" {
		t.Errorf("SelectBest should not select failed confidants, got %s", selected)
	}
	if selected != "eve" && selected != "dave" {
		t.Errorf("Expected eve or dave, got %s", selected)
	}

	// Add bob as confirmed - should clear failure
	tracker.Add("bob", time.Now().Unix())

	// Bob should now be available again if removed and re-added to peers
	tracker.Remove("bob")
	selected2 := tracker.SelectBest("alice", peers)
	// Bob should be selectable now (no longer failed)
	if selected2 != "bob" && selected2 != "eve" && selected2 != "dave" {
		t.Errorf("After Add() clears failure, bob should be selectable again")
	}

	// Test cleanup of expired failures
	tracker2 := NewConfidantTracker(3)

	// Mark failures with fake old timestamp
	tracker2.mu.Lock()
	tracker2.failed["oldnara"] = time.Now().Unix() - int64(FailureBackoffTime.Seconds()) - 10
	tracker2.failed["newnara"] = time.Now().Unix()
	tracker2.mu.Unlock()

	// Cleanup should only remove old failures
	recovered := tracker2.CleanupExpiredFailures()
	if len(recovered) != 1 || recovered[0] != "oldnara" {
		t.Errorf("Expected to recover oldnara, got %v", recovered)
	}

	// oldnara should be gone from failed map
	tracker2.mu.RLock()
	_, stillFailed := tracker2.failed["oldnara"]
	_, newStillFailed := tracker2.failed["newnara"]
	tracker2.mu.RUnlock()

	if stillFailed {
		t.Error("oldnara should have been removed from failed map")
	}
	if !newStillFailed {
		t.Error("newnara should still be in failed map")
	}
}

func TestRandomConfidentSelection(t *testing.T) {
	tracker := NewConfidantTracker(2)
	tracker.Add("bob", time.Now().Unix())

	online := []string{"alice", "bob", "carol", "dave", "eve"}

	// Select should not return self or existing confidants
	selected := tracker.SelectRandom("alice", online)
	if selected == "" {
		t.Error("Should select a confident")
	}
	if selected == "alice" {
		t.Error("Should not select self")
	}
	if selected == "bob" {
		t.Error("Should not select existing confident")
	}

	// With repeated calls, should get distribution
	counts := make(map[string]int)
	for i := 0; i < 1000; i++ {
		s := tracker.SelectRandom("alice", online)
		counts[s]++
	}

	// carol, dave, eve should all be selected (not alice or bob)
	if counts["carol"] == 0 || counts["dave"] == 0 || counts["eve"] == 0 {
		t.Error("Selection should be distributed among eligible candidates")
	}
	if counts["alice"] > 0 || counts["bob"] > 0 {
		t.Error("Selection should exclude self and existing confidants")
	}
}

func TestConfidentSelectionExcludesExisting(t *testing.T) {
	tracker := NewConfidantTracker(3)
	tracker.Add("bob", time.Now().Unix())
	tracker.Add("carol", time.Now().Unix())

	online := []string{"alice", "bob", "carol", "dave"}

	// Only dave should be selectable
	selected := tracker.SelectRandom("alice", online)
	if selected != "dave" {
		t.Errorf("Expected dave (only eligible), got %s", selected)
	}

	// If no one is eligible, return empty
	tracker.Add("dave", time.Now().Unix())
	selected = tracker.SelectRandom("alice", online)
	if selected != "" {
		t.Errorf("Expected empty (no eligible), got %s", selected)
	}
}

func TestPendingConfidentTimeout(t *testing.T) {
	tracker := NewConfidantTracker(2)

	online := []string{"alice", "bob", "carol", "dave"}

	// Add bob as pending (sent stash, awaiting ack)
	tracker.AddPending("bob")

	// Verify bob is pending
	if !tracker.IsPending("bob") {
		t.Error("Bob should be pending")
	}

	// Pending should not count as confirmed
	if tracker.Has("bob") {
		t.Error("Pending confidant should not count as confirmed")
	}
	if tracker.Count() != 0 {
		t.Errorf("Expected 0 confirmed confidants, got %d", tracker.Count())
	}

	// SelectRandom should exclude pending confidants
	for i := 0; i < 100; i++ {
		selected := tracker.SelectRandom("alice", online)
		if selected == "bob" {
			t.Error("Should not select pending confident")
		}
		if selected == "alice" {
			t.Error("Should not select self")
		}
	}

	// When bob acks, move from pending to confirmed
	tracker.Add("bob", time.Now().Unix())
	if tracker.IsPending("bob") {
		t.Error("Bob should no longer be pending after ack")
	}
	if !tracker.Has("bob") {
		t.Error("Bob should be confirmed after ack")
	}
}

func TestPendingConfidentExpiry(t *testing.T) {
	tracker := NewConfidantTracker(2)

	// Manually set an expired pending entry (simulating old timestamp)
	tracker.mu.Lock()
	tracker.pending["bob"] = time.Now().Add(-2 * PendingTimeout).Unix()
	tracker.pending["carol"] = time.Now().Unix() // Not expired
	tracker.mu.Unlock()

	// Cleanup should remove expired but keep non-expired
	expired := tracker.CleanupExpiredPending()

	if len(expired) != 1 {
		t.Errorf("Expected 1 expired, got %d", len(expired))
	}
	if expired[0] != "bob" {
		t.Errorf("Expected bob to be expired, got %s", expired[0])
	}

	// Bob should be gone, carol should remain pending
	if tracker.IsPending("bob") {
		t.Error("Bob should be removed after expiry")
	}
	if !tracker.IsPending("carol") {
		t.Error("Carol should still be pending (not expired)")
	}
}

func TestPendingToConfirmedFlow(t *testing.T) {
	// Simulate the full flow: send -> pending -> ack -> confirmed
	tracker := NewConfidantTracker(2)

	online := []string{"alice", "bob", "carol", "dave"}

	// Step 1: Select and mark as pending (simulating sending stash)
	selected := tracker.SelectRandom("alice", online)
	if selected == "" {
		t.Fatal("Should select someone")
	}
	tracker.AddPending(selected)
	t.Logf("Selected %s, marked as pending", selected)

	// Step 2: Verify needs more (pending doesn't count)
	if !tracker.NeedsMore() {
		t.Error("Should still need more (pending doesn't count)")
	}
	if tracker.NeedsCount() != 2 {
		t.Errorf("Should need 2 more, got %d", tracker.NeedsCount())
	}

	// Step 3: Simulate ack received
	tracker.Add(selected, time.Now().Unix())

	// Step 4: Now we have 1 confirmed, need 1 more
	if tracker.Count() != 1 {
		t.Errorf("Expected 1 confirmed, got %d", tracker.Count())
	}
	if tracker.NeedsCount() != 1 {
		t.Errorf("Should need 1 more, got %d", tracker.NeedsCount())
	}

	// Step 5: Select another
	selected2 := tracker.SelectRandom("alice", online)
	if selected2 == "" || selected2 == selected {
		t.Errorf("Should select different confidant, got %s", selected2)
	}
	tracker.AddPending(selected2)
	tracker.Add(selected2, time.Now().Unix())

	// Step 6: Now satisfied
	if !tracker.IsSatisfied() {
		t.Error("Should be satisfied with 2 confirmed confidants")
	}
}

// =====================================================
// Message Tests
// =====================================================

func TestStashPayloadTimestamp(t *testing.T) {
	// The timestamp is inside the encrypted payload
	soul := NativeSoulCustom(stashHW1, "alice")
	signingKeypair := DeriveKeypair(soul)
	encKeypair := DeriveEncryptionKeys(signingKeypair.PrivateKey)

	timestamp := time.Now().Unix()
	stashData := testStashData(timestamp, []string{"🌸", "🦋", "🌊"})

	// Create payload
	payload, err := CreateStashPayload("alice", stashData, encKeypair)
	if err != nil {
		t.Fatalf("CreateStashPayload failed: %v", err)
	}

	// Extract timestamp (requires decryption)
	extractedTimestamp, err := ExtractStashTimestamp(payload, encKeypair)
	if err != nil {
		t.Fatalf("ExtractStashTimestamp failed: %v", err)
	}

	if extractedTimestamp != timestamp {
		t.Errorf("Timestamp mismatch: expected %d, got %d", timestamp, extractedTimestamp)
	}
}

func TestSignatureVerification(t *testing.T) {
	soul := NativeSoulCustom(stashHW1, "alice")
	keypair := DeriveKeypair(soul)
	encKeypair := DeriveEncryptionKeys(keypair.PrivateKey)

	stashData := testStashData(time.Now().Unix(), []string{"🔥", "⭐"})
	payload, _ := CreateStashPayload("alice", stashData, encKeypair)

	// Create signed StashStore message
	msg := &StashStore{
		From:    "alice",
		Payload: *payload,
	}
	msg.Sign(keypair)

	// Should verify with correct public key
	if !msg.Verify(keypair.PublicKey) {
		t.Error("Valid signature should verify")
	}

	// Should not verify with wrong public key
	otherKeypair := DeriveKeypair(NativeSoulCustom(stashHW2, "bob"))
	if msg.Verify(otherKeypair.PublicKey) {
		t.Error("Should not verify with wrong public key")
	}

	// Tampered message should not verify
	msg.From = "mallory"
	if msg.Verify(keypair.PublicKey) {
		t.Error("Tampered message should not verify")
	}
}

// =====================================================
// Bootstrap Workflow Tests (Mock Network)
// =====================================================

func TestBootstrapNoStash(t *testing.T) {
	// Nara boots, no one has their stash, should start fresh
	network := NewMockMeshNetwork()

	// Set up owner
	ownerSoul := NativeSoulCustom(stashHW1, "alice")
	ownerKeypair := DeriveKeypair(ownerSoul)

	// Create stash manager for owner
	manager := NewStashManager("alice", ownerKeypair, 2)

	// Request stash (broadcast) - no one responds
	responses := make(chan *StashResponse, 10)
	go manager.RequestStash(network, responses)

	// Wait a bit and close (simulating timeout)
	time.Sleep(100 * time.Millisecond)
	close(responses)

	// Should have no recovered stash
	if manager.HasRecoveredStash() {
		t.Error("Should not have recovered stash when no one responds")
	}

	// Should be ready for fresh start
	if !manager.ShouldStartFresh() {
		t.Error("Should be ready for fresh start")
	}
}

func TestBootstrapWithStash(t *testing.T) {
	// Nara boots, requests stash, receives from confidant, recovers data
	network := NewMockMeshNetwork()

	// Set up owner
	ownerSoul := NativeSoulCustom(stashHW1, "alice")
	ownerKeypair := DeriveKeypair(ownerSoul)
	ownerEncKeypair := DeriveEncryptionKeys(ownerKeypair.PrivateKey)

	// Create stash data with test values
	originalValues := []string{"val1", "val2", "val3", "val4", "val5"}
	originalTimestamp := time.Now().Unix() - 3600 // 1 hour ago
	stashData := testStashData(originalTimestamp, originalValues)
	payload, _ := CreateStashPayload("alice", stashData, ownerEncKeypair)

	// Set up confidant (bob) who has alice's stash
	bobTransport := NewMockMeshTransport()
	network.Register("bob", bobTransport)

	// Create manager and simulate recovery
	manager := NewStashManager("alice", ownerKeypair, 2)

	// Simulate receiving response from bob
	responses := make(chan *StashResponse, 10)
	responses <- &StashResponse{
		From:    "bob",
		Owner:   "alice",
		Payload: *payload,
	}
	close(responses)

	// Process responses
	err := manager.ProcessResponses(responses)
	if err != nil {
		t.Fatalf("ProcessResponses failed: %v", err)
	}

	// Should have recovered stash
	if !manager.HasRecoveredStash() {
		t.Error("Should have recovered stash")
	}

	// Verify recovered values (extract from JSON data)
	recoveredData := manager.GetRecoveredData()
	var recoveredPayload map[string]interface{}
	if err := json.Unmarshal(recoveredData, &recoveredPayload); err != nil {
		t.Fatalf("Failed to unmarshal recovered data: %v", err)
	}
	recoveredValuesRaw := recoveredPayload["values"].([]interface{})
	recoveredValues := make([]string, len(recoveredValuesRaw))
	for i, e := range recoveredValuesRaw {
		recoveredValues[i] = e.(string)
	}
	if len(recoveredValues) != len(originalValues) {
		t.Errorf("Values count mismatch: expected %d, got %d", len(originalValues), len(recoveredValues))
	}
	for i, e := range originalValues {
		if recoveredValues[i] != e {
			t.Errorf("Value mismatch at %d: expected %s, got %s", i, e, recoveredValues[i])
		}
	}

	// Verify timestamp
	if manager.GetRecoveredTimestamp() != originalTimestamp {
		t.Errorf("Timestamp mismatch: expected %d, got %d", originalTimestamp, manager.GetRecoveredTimestamp())
	}
}

func TestBootstrapMultipleResponses(t *testing.T) {
	// Multiple confidants respond, pick newest timestamp
	ownerSoul := NativeSoulCustom(stashHW1, "alice")
	ownerKeypair := DeriveKeypair(ownerSoul)
	ownerEncKeypair := DeriveEncryptionKeys(ownerKeypair.PrivateKey)

	now := time.Now().Unix()

	// Create older stash (from bob)
	olderData := testStashData(now-3600, []string{"🌳", "🌲"}) // 1 hour ago
	olderPayload, _ := CreateStashPayload("alice", olderData, ownerEncKeypair)

	// Create newer stash (from carol)
	newerData := testStashData(now-60, []string{"🌴", "🌵", "🌾"}) // 1 minute ago
	newerPayload, _ := CreateStashPayload("alice", newerData, ownerEncKeypair)

	// Create manager
	manager := NewStashManager("alice", ownerKeypair, 2)

	// Simulate receiving both responses (older first)
	responses := make(chan *StashResponse, 10)
	responses <- &StashResponse{From: "bob", Owner: "alice", Payload: *olderPayload}
	responses <- &StashResponse{From: "carol", Owner: "alice", Payload: *newerPayload}
	close(responses)

	// Process - should pick newest
	if err := manager.ProcessResponses(responses); err != nil {
		t.Fatalf("Failed to process responses: %v", err)
	}

	// The recovered data should be the newer one
	recoveredTimestamp := manager.GetRecoveredTimestamp()
	if recoveredTimestamp != now-60 {
		t.Errorf("Should pick newest stash, expected %d, got %d", now-60, recoveredTimestamp)
	}
}

func TestBootstrapStaleStash(t *testing.T) {
	// Reject stash with timestamp older than what we already have
	ownerSoul := NativeSoulCustom(stashHW1, "alice")
	ownerKeypair := DeriveKeypair(ownerSoul)
	ownerEncKeypair := DeriveEncryptionKeys(ownerKeypair.PrivateKey)

	now := time.Now().Unix()

	// Manager already has a stash with a recent timestamp
	manager := NewStashManager("alice", ownerKeypair, 2)
	manager.SetCurrentTimestamp(now - 60) // 1 minute ago

	// Receive older stash
	staleData := testStashData(now-3600, []string{"🍄", "🪨"}) // 1 hour ago (stale!)
	stalePayload, _ := CreateStashPayload("alice", staleData, ownerEncKeypair)

	responses := make(chan *StashResponse, 10)
	responses <- &StashResponse{From: "bob", Owner: "alice", Payload: *stalePayload}
	close(responses)

	// Process - should reject stale
	if err := manager.ProcessResponses(responses); err != nil {
		t.Fatalf("Failed to process responses: %v", err)
	}

	// Should NOT have updated with stale data
	if manager.GetRecoveredTimestamp() == now-3600 {
		t.Error("Should reject stale stash")
	}
}

// =====================================================
// Runtime Workflow Tests (Mock Network)
// =====================================================

func TestStashStoreAndAck(t *testing.T) {
	// Owner sends StashStore, confidant stores and acks
	network := NewMockMeshNetwork()

	// Set up confidant (bob)
	bobTransport := NewMockMeshTransport()
	network.Register("bob", bobTransport)
	bobManager := NewConfidantStashStore() // in-memory store for confidants

	// Set up owner (alice)
	aliceSoul := NativeSoulCustom(stashHW1, "alice")
	aliceKeypair := DeriveKeypair(aliceSoul)
	aliceEncKeypair := DeriveEncryptionKeys(aliceKeypair.PrivateKey)

	// Create stash
	stashData := testStashData(time.Now().Unix(), []string{"🦋", "🐝", "🐛"})
	payload, _ := CreateStashPayload("alice", stashData, aliceEncKeypair)

	// Create signed store message
	storeMsg := &StashStore{
		From:    "alice",
		Payload: *payload,
	}
	storeMsg.Sign(aliceKeypair)

	// Bob receives and processes
	err := bobManager.HandleStashStore(storeMsg, func(name string) []byte {
		if name == "alice" {
			return aliceKeypair.PublicKey
		}
		return nil
	})
	if err != nil {
		t.Fatalf("HandleStashStore failed: %v", err)
	}

	// Bob should have stored it
	if !bobManager.HasStashFor("alice") {
		t.Error("Bob should have stored alice's stash")
	}

	// Bob should send ack
	ack := bobManager.CreateAck("alice")
	if ack.Owner != "alice" {
		t.Error("Ack should be for alice")
	}
}

func TestStashRequestTriggersResponse(t *testing.T) {
	// Broadcast request, confidant sends payload
	bobManager := NewConfidantStashStore()

	// Pre-store alice's stash in bob
	aliceSoul := NativeSoulCustom(stashHW1, "alice")
	aliceKeypair := DeriveKeypair(aliceSoul)
	aliceEncKeypair := DeriveEncryptionKeys(aliceKeypair.PrivateKey)

	stashData := testStashData(time.Now().Unix(), []string{"🦎", "🐢"})
	payload, _ := CreateStashPayload("alice", stashData, aliceEncKeypair)
	bobManager.Store("alice", payload)

	// Alice sends request
	request := &StashRequest{
		From: "alice",
	}
	request.Sign(aliceKeypair)

	// Bob handles request
	response := bobManager.HandleStashRequest(request, func(name string) []byte {
		if name == "alice" {
			return aliceKeypair.PublicKey
		}
		return nil
	})

	// Should return the stored stash
	if response == nil {
		t.Fatal("Bob should respond with stored stash")
	}
	if response.Owner != "alice" {
		t.Error("Response should be for alice")
	}
}

func TestStashDelete(t *testing.T) {
	// Test Delete method on ConfidantStashStore
	bobManager := NewConfidantStashStore()

	// Pre-store alice's stash
	aliceSoul := NativeSoulCustom(stashHW1, "alice")
	aliceKeypair := DeriveKeypair(aliceSoul)
	aliceEncKeypair := DeriveEncryptionKeys(aliceKeypair.PrivateKey)

	payload, _ := CreateStashPayload("alice", testStashData(time.Now().Unix(), []string{"🔥", "⭐"}), aliceEncKeypair)
	bobManager.Store("alice", payload)

	if !bobManager.HasStashFor("alice") {
		t.Fatal("Setup: bob should have alice's stash")
	}

	// Delete alice's stash
	deleted := bobManager.Delete("alice")
	if !deleted {
		t.Fatal("Delete should return true when stash existed")
	}

	// Bob should no longer have alice's stash
	if bobManager.HasStashFor("alice") {
		t.Error("Bob should have deleted alice's stash")
	}

	// Deleting again should return false
	deleted = bobManager.Delete("alice")
	if deleted {
		t.Error("Delete should return false when stash doesn't exist")
	}
}

func TestConfidentGoesOffline(t *testing.T) {
	// Confident disappears, owner picks new random one
	tracker := NewConfidantTracker(2)
	tracker.Add("bob", time.Now().Unix())
	tracker.Add("carol", time.Now().Unix())

	online := []string{"alice", "carol", "dave"} // bob is offline

	// Mark bob as gone
	tracker.MarkOffline("bob", online)

	// Bob should be removed
	if tracker.Has("bob") {
		t.Error("Offline confidant should be removed")
	}

	// Should need to pick a replacement
	if !tracker.NeedsMore() {
		t.Error("Should need replacement confident")
	}

	// Pick new one
	selected := tracker.SelectRandom("alice", online)
	if selected != "dave" {
		t.Errorf("Should select dave as replacement, got %s", selected)
	}
}

func TestTooManyConfidents(t *testing.T) {
	// >3 confidants, owner asks extras to delete
	tracker := NewConfidantTracker(2) // target: 2

	tracker.Add("bob", time.Now().Unix())
	tracker.Add("carol", time.Now().Unix())
	tracker.Add("dave", time.Now().Unix())
	tracker.Add("eve", time.Now().Unix())

	// Should have excess
	excess := tracker.GetExcess()
	if len(excess) != 2 {
		t.Errorf("Expected 2 excess confidants, got %d", len(excess))
	}
}

func TestCloutRewardForStoring(t *testing.T) {
	// Confident gets clout when storing stash
	personality := NaraPersonality{Sociability: 50, Agreeableness: 50, Chill: 50}
	ledger := NewSyncLedger(1000)
	cloutProjection := NewCloutProjection(ledger)

	// Record stash stored event
	event := SocialEventPayload{
		Type:   "service", // service events always give positive clout
		Actor:  "bob",
		Target: "alice",
		Reason: ReasonStashStored,
	}

	syncEvent := SyncEvent{
		Service:   ServiceSocial,
		Timestamp: time.Now().UnixNano(),
		Emitter:   "bob",
		Social:    &event,
	}
	syncEvent.ComputeID()

	added := ledger.AddEvent(syncEvent)
	if !added {
		t.Error("Should add stash stored event")
	}

	// Process events before querying
	if err := cloutProjection.RunToEnd(context.Background()); err != nil {
		t.Fatalf("Failed to run projection: %v", err)
	}

	// Bob should gain clout
	clout := cloutProjection.DeriveClout("observer-soul", personality)
	if clout["bob"] <= 0 {
		t.Errorf("Bob should gain clout for storing stash, got %f", clout["bob"])
	}
}

// =====================================================
// Size Limit Tests
// =====================================================

func TestStashSizeLimit(t *testing.T) {
	// Stash should be limited to 10KB
	manager := NewConfidantStashStore()

	// Create oversized payload
	largePayload := &StashPayload{
		Owner:      "alice",
		Nonce:      make([]byte, 24),
		Ciphertext: make([]byte, 20*1024), // 20KB - too large
	}

	aliceSoul := NativeSoulCustom(stashHW1, "alice")
	aliceKeypair := DeriveKeypair(aliceSoul)

	storeMsg := &StashStore{
		From:    "alice",
		Payload: *largePayload,
	}
	storeMsg.Sign(aliceKeypair)

	err := manager.HandleStashStore(storeMsg, func(name string) []byte {
		return aliceKeypair.PublicKey
	})

	if err == nil {
		t.Error("Should reject oversized stash")
	}
}

// =====================================================
// Integration Tests
// =====================================================

func TestEndToEndStashFlow(t *testing.T) {
	// Full end-to-end flow: create data, stash to confidants, recover from scratch

	// Setup: Create owner (alice)
	aliceSoul := NativeSoulCustom(stashHW1, "alice")
	aliceKeypair := DeriveKeypair(aliceSoul)
	aliceEncKeypair := DeriveEncryptionKeys(aliceKeypair.PrivateKey)

	// Create stash data with test values
	originalValues := []string{"val1", "val2", "val3", "val4", "val5", "val6", "val7", "val8", "val9", "val10"}
	timestamp := time.Now().Unix() - 86400 // 1 day ago
	stashData := testStashData(timestamp, originalValues)
	payload, err := CreateStashPayload("alice", stashData, aliceEncKeypair)
	if err != nil {
		t.Fatalf("CreateStashPayload failed: %v", err)
	}

	// Setup: Two confidants (bob and carol)
	bobStore := NewConfidantStashStore()
	carolStore := NewConfidantStashStore()

	getPublicKey := func(name string) []byte {
		if name == "alice" {
			return aliceKeypair.PublicKey
		}
		return nil
	}

	// Phase 1: Alice stores stash with confidants
	storeMsg := &StashStore{
		From:    "alice",
		Payload: *payload,
	}
	storeMsg.Sign(aliceKeypair)

	err = bobStore.HandleStashStore(storeMsg, getPublicKey)
	if err != nil {
		t.Fatalf("Bob store failed: %v", err)
	}
	err = carolStore.HandleStashStore(storeMsg, getPublicKey)
	if err != nil {
		t.Fatalf("Carol store failed: %v", err)
	}

	// Verify both confidants have the stash
	if !bobStore.HasStashFor("alice") || !carolStore.HasStashFor("alice") {
		t.Error("Confidents should have alice's stash")
	}

	// Phase 2: Alice "reboots" (loses state) and requests stash
	request := &StashRequest{From: "alice"}
	request.Sign(aliceKeypair)

	// Confidents respond
	bobResponse := bobStore.HandleStashRequest(request, getPublicKey)
	carolResponse := carolStore.HandleStashRequest(request, getPublicKey)

	if bobResponse == nil || carolResponse == nil {
		t.Fatal("Confidents should respond to request")
	}

	// Phase 3: Alice recovers state
	recoveryManager := NewStashManager("alice", aliceKeypair, 2)

	responses := make(chan *StashResponse, 10)
	responses <- bobResponse
	responses <- carolResponse
	close(responses)

	err = recoveryManager.ProcessResponses(responses)
	if err != nil {
		t.Fatalf("ProcessResponses failed: %v", err)
	}

	if !recoveryManager.HasRecoveredStash() {
		t.Fatal("Should have recovered stash")
	}

	// Verify recovered values match original (extract from JSON data)
	recoveredData := recoveryManager.GetRecoveredData()
	var recoveredPayload map[string]interface{}
	if err := json.Unmarshal(recoveredData, &recoveredPayload); err != nil {
		t.Fatalf("Failed to unmarshal recovered data: %v", err)
	}
	recoveredValuesRaw := recoveredPayload["values"].([]interface{})
	recoveredValues := make([]string, len(recoveredValuesRaw))
	for i, e := range recoveredValuesRaw {
		recoveredValues[i] = e.(string)
	}
	if len(recoveredValues) != len(originalValues) {
		t.Errorf("Values count mismatch: expected %d, got %d", len(originalValues), len(recoveredValues))
	}
	for i, e := range originalValues {
		if recoveredValues[i] != e {
			t.Errorf("Value mismatch at %d: expected %s, got %s", i, e, recoveredValues[i])
		}
	}

	// Verify timestamp matches
	if recoveryManager.GetRecoveredTimestamp() != timestamp {
		t.Errorf("Timestamp mismatch: expected %d, got %d", timestamp, recoveryManager.GetRecoveredTimestamp())
	}

	t.Log("End-to-end stash flow completed successfully")
}

func TestConfidantTrackerWithMockNetwork(t *testing.T) {
	// Test confidant management with simulated network churn

	tracker := NewConfidantTracker(2)
	online := []string{"alice", "bob", "carol", "dave", "eve"}

	// Alice picks initial confidants
	conf1 := tracker.SelectRandom("alice", online)
	if conf1 == "" {
		t.Fatal("Should select first confident")
	}
	tracker.Add(conf1, time.Now().Unix())

	conf2 := tracker.SelectRandom("alice", online)
	if conf2 == "" || conf2 == conf1 {
		t.Fatal("Should select different second confident")
	}
	tracker.Add(conf2, time.Now().Unix())

	// Should be satisfied
	if !tracker.IsSatisfied() {
		t.Error("Should be satisfied with 2 confidants")
	}

	// Simulate one going offline
	newOnline := []string{"alice", "bob", "dave"} // carol and eve offline
	tracker.MarkOffline(conf1, newOnline)
	tracker.MarkOffline(conf2, newOnline)

	// Check if we need replacements
	if tracker.IsSatisfied() {
		// Might still be satisfied if both conf1 and conf2 are in newOnline
		t.Log("Still satisfied after network churn")
	} else {
		// Need to pick new confidants
		newConf := tracker.SelectRandom("alice", newOnline)
		if newConf != "" {
			tracker.Add(newConf, time.Now().Unix())
		}
	}

	t.Logf("Final confidant count: %d", tracker.Count())
}

func TestConcurrentStashRequests(t *testing.T) {
	// Test handling multiple simultaneous stash requests

	// Setup one confidant with alice's stash
	confidantStore := NewConfidantStashStore()

	aliceSoul := NativeSoulCustom(stashHW1, "alice")
	aliceKeypair := DeriveKeypair(aliceSoul)
	aliceEncKeypair := DeriveEncryptionKeys(aliceKeypair.PrivateKey)

	stashData := testStashData(time.Now().Unix(), []string{"🐙", "🦑", "🦀"})
	payload, _ := CreateStashPayload("alice", stashData, aliceEncKeypair)

	storeMsg := &StashStore{From: "alice", Payload: *payload}
	storeMsg.Sign(aliceKeypair)
	if err := confidantStore.HandleStashStore(storeMsg, func(name string) []byte {
		if name == "alice" {
			return aliceKeypair.PublicKey
		}
		return nil
	}); err != nil {
		t.Fatalf("Failed to handle stash store: %v", err)
	}

	// Simulate concurrent requests
	results := make(chan *StashResponse, 10)
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			request := &StashRequest{From: "alice"}
			request.Sign(aliceKeypair)
			response := confidantStore.HandleStashRequest(request, func(name string) []byte {
				if name == "alice" {
					return aliceKeypair.PublicKey
				}
				return nil
			})
			if response != nil {
				results <- response
			}
		}()
	}

	wg.Wait()
	close(results)

	// All requests should have gotten responses
	responseCount := 0
	for range results {
		responseCount++
	}

	if responseCount != 10 {
		t.Errorf("Expected 10 responses, got %d", responseCount)
	}
}

func TestStashTimestampConflictResolution(t *testing.T) {
	// Test that newest timestamp wins when multiple responses arrive

	aliceSoul := NativeSoulCustom(stashHW1, "alice")
	aliceKeypair := DeriveKeypair(aliceSoul)
	aliceEncKeypair := DeriveEncryptionKeys(aliceKeypair.PrivateKey)

	now := time.Now().Unix()

	// Create three stashes with different timestamps
	stashes := []struct {
		from      string
		timestamp int64
		values    []string
	}{
		{"bob", now - 3600, []string{"a", "b"}},  // 1 hour ago
		{"carol", now - 60, []string{"c", "d"}},  // 1 minute ago
		{"dave", now - 1800, []string{"e", "f"}}, // 30 minutes ago
	}

	responses := make(chan *StashResponse, 10)
	for _, s := range stashes {
		data := testStashData(s.timestamp, s.values)
		payload, _ := CreateStashPayload("alice", data, aliceEncKeypair)
		responses <- &StashResponse{
			From:    s.from,
			Owner:   "alice",
			Payload: *payload,
		}
	}
	close(responses)

	manager := NewStashManager("alice", aliceKeypair, 2)
	if err := manager.ProcessResponses(responses); err != nil {
		t.Fatalf("Failed to process responses: %v", err)
	}

	// Should have picked carol's (newest)
	recoveredTimestamp := manager.GetRecoveredTimestamp()
	if recoveredTimestamp != now-60 {
		t.Errorf("Should pick newest timestamp (carol's), got timestamp from %ds ago", now-recoveredTimestamp)
	}
}
