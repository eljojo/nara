package nara

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"
)

// Test hardware fingerprints for stash testing
var (
	stashHW1 = hashBytes([]byte("stash-test-hw-1"))
	stashHW2 = hashBytes([]byte("stash-test-hw-2"))
	stashHW3 = hashBytes([]byte("stash-test-hw-3"))
)

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
	// Create StashData
	data := &StashData{
		Timestamp: time.Now().Unix(),
		Emojis:    []string{"🌸", "🦋", "🌊", "🔥", "⭐"},
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
	if len(parsed.Emojis) != len(data.Emojis) {
		t.Error("Emojis length mismatch after serialization")
	}
	for i, e := range data.Emojis {
		if parsed.Emojis[i] != e {
			t.Errorf("Emoji mismatch at %d: expected %s, got %s", i, e, parsed.Emojis[i])
		}
	}
}

func TestStashDataTimestampPreserved(t *testing.T) {
	// The timestamp must survive full encrypt/decrypt cycle
	soul := NativeSoulCustom(stashHW1, "alice")
	encKeypair := DeriveEncryptionKeys(DeriveKeypair(soul).PrivateKey)

	originalTimestamp := int64(1234567890)
	data := &StashData{
		Timestamp: originalTimestamp,
		Emojis:    []string{"🌸", "🦋", "🌊"},
	}

	// Serialize, encrypt, decrypt, deserialize
	serialized, _ := data.Marshal()
	nonce, ciphertext, _ := encKeypair.EncryptForSelf(serialized)
	decrypted, _ := encKeypair.DecryptForSelf(nonce, ciphertext)

	parsed := &StashData{}
	parsed.Unmarshal(decrypted)

	if parsed.Timestamp != originalTimestamp {
		t.Errorf("Timestamp not preserved: expected %d, got %d", originalTimestamp, parsed.Timestamp)
	}
}

func TestGenerateRandomEmojis(t *testing.T) {
	emojis := GenerateRandomEmojis(10)

	if len(emojis) != 10 {
		t.Errorf("Expected 10 emojis, got %d", len(emojis))
	}

	// All emojis should be from the pool
	poolSet := make(map[string]bool)
	for _, e := range EmojiPool {
		poolSet[e] = true
	}
	for _, e := range emojis {
		if !poolSet[e] {
			t.Errorf("Emoji %s not in pool", e)
		}
	}

	// Generate again - should be different (probabilistically)
	emojis2 := GenerateRandomEmojis(10)
	same := true
	for i := range emojis {
		if emojis[i] != emojis2[i] {
			same = false
			break
		}
	}
	// Very unlikely to be the same (1 in 50^10)
	if same {
		t.Log("Warning: two random emoji sequences were identical (extremely unlikely)")
	}
}

func TestEmojiPoolSize(t *testing.T) {
	// Emoji pool should have enough variety
	if len(EmojiPool) < 40 {
		t.Errorf("Emoji pool too small: %d (expected at least 40)", len(EmojiPool))
	}
}

// =====================================================
// Confident Management Tests
// =====================================================

func TestConfidentTrackerAddRemove(t *testing.T) {
	tracker := NewConfidentTracker(3)

	// Add some confidents
	tracker.Add("bob", time.Now().Unix())
	tracker.Add("carol", time.Now().Unix())

	if tracker.Count() != 2 {
		t.Errorf("Expected 2 confidents, got %d", tracker.Count())
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
		t.Errorf("Expected 1 confident after removal, got %d", tracker.Count())
	}
}

func TestConfidentTrackerTargetCount(t *testing.T) {
	tracker := NewConfidentTracker(2)

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

	// Get excess confidents
	excess := tracker.GetExcess()
	if len(excess) != 1 {
		t.Errorf("Expected 1 excess confident, got %d", len(excess))
	}
}

func TestRandomConfidentSelection(t *testing.T) {
	tracker := NewConfidentTracker(2)
	tracker.Add("bob", time.Now().Unix())

	online := []string{"alice", "bob", "carol", "dave", "eve"}

	// Select should not return self or existing confidents
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
		t.Error("Selection should exclude self and existing confidents")
	}
}

func TestConfidentSelectionExcludesExisting(t *testing.T) {
	tracker := NewConfidentTracker(3)
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

// =====================================================
// Message Tests
// =====================================================

func TestStashPayloadTimestamp(t *testing.T) {
	// The timestamp is inside the encrypted payload
	soul := NativeSoulCustom(stashHW1, "alice")
	signingKeypair := DeriveKeypair(soul)
	encKeypair := DeriveEncryptionKeys(signingKeypair.PrivateKey)

	timestamp := time.Now().Unix()
	stashData := &StashData{
		Timestamp: timestamp,
		Emojis:    []string{"🌸", "🦋", "🌊"},
	}

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

	stashData := &StashData{
		Timestamp: time.Now().Unix(),
		Emojis:    []string{"🔥", "⭐"},
	}
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
	// Nara boots, requests stash, receives from confident, recovers emojis
	network := NewMockMeshNetwork()

	// Set up owner
	ownerSoul := NativeSoulCustom(stashHW1, "alice")
	ownerKeypair := DeriveKeypair(ownerSoul)
	ownerEncKeypair := DeriveEncryptionKeys(ownerKeypair.PrivateKey)

	// Create stash data with emojis
	originalEmojis := []string{"🌸", "🦋", "🌊", "🔥", "⭐"}
	originalTimestamp := time.Now().Unix() - 3600 // 1 hour ago
	stashData := &StashData{
		Timestamp: originalTimestamp,
		Emojis:    originalEmojis,
	}
	payload, _ := CreateStashPayload("alice", stashData, ownerEncKeypair)

	// Set up confident (bob) who has alice's stash
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

	// Verify recovered emojis
	recoveredEmojis := manager.GetRecoveredEmojis()
	if len(recoveredEmojis) != len(originalEmojis) {
		t.Errorf("Emoji count mismatch: expected %d, got %d", len(originalEmojis), len(recoveredEmojis))
	}
	for i, e := range originalEmojis {
		if recoveredEmojis[i] != e {
			t.Errorf("Emoji mismatch at %d: expected %s, got %s", i, e, recoveredEmojis[i])
		}
	}

	// Verify timestamp
	if manager.GetRecoveredTimestamp() != originalTimestamp {
		t.Errorf("Timestamp mismatch: expected %d, got %d", originalTimestamp, manager.GetRecoveredTimestamp())
	}
}

func TestBootstrapMultipleResponses(t *testing.T) {
	// Multiple confidents respond, pick newest timestamp
	ownerSoul := NativeSoulCustom(stashHW1, "alice")
	ownerKeypair := DeriveKeypair(ownerSoul)
	ownerEncKeypair := DeriveEncryptionKeys(ownerKeypair.PrivateKey)

	now := time.Now().Unix()

	// Create older stash (from bob)
	olderData := &StashData{
		Timestamp: now - 3600, // 1 hour ago
		Emojis:    []string{"🌳", "🌲"},
	}
	olderPayload, _ := CreateStashPayload("alice", olderData, ownerEncKeypair)

	// Create newer stash (from carol)
	newerData := &StashData{
		Timestamp: now - 60, // 1 minute ago
		Emojis:    []string{"🌴", "🌵", "🌾"},
	}
	newerPayload, _ := CreateStashPayload("alice", newerData, ownerEncKeypair)

	// Create manager
	manager := NewStashManager("alice", ownerKeypair, 2)

	// Simulate receiving both responses (older first)
	responses := make(chan *StashResponse, 10)
	responses <- &StashResponse{From: "bob", Owner: "alice", Payload: *olderPayload}
	responses <- &StashResponse{From: "carol", Owner: "alice", Payload: *newerPayload}
	close(responses)

	// Process - should pick newest
	manager.ProcessResponses(responses)

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
	staleData := &StashData{
		Timestamp: now - 3600, // 1 hour ago (stale!)
		Emojis:    []string{"🍄", "🪨"},
	}
	stalePayload, _ := CreateStashPayload("alice", staleData, ownerEncKeypair)

	responses := make(chan *StashResponse, 10)
	responses <- &StashResponse{From: "bob", Owner: "alice", Payload: *stalePayload}
	close(responses)

	// Process - should reject stale
	manager.ProcessResponses(responses)

	// Should NOT have updated with stale data
	if manager.GetRecoveredTimestamp() == now-3600 {
		t.Error("Should reject stale stash")
	}
}

// =====================================================
// Runtime Workflow Tests (Mock Network)
// =====================================================

func TestStashStoreAndAck(t *testing.T) {
	// Owner sends StashStore, confident stores and acks
	network := NewMockMeshNetwork()

	// Set up confident (bob)
	bobTransport := NewMockMeshTransport()
	network.Register("bob", bobTransport)
	bobManager := NewConfidentStashStore() // in-memory store for confidents

	// Set up owner (alice)
	aliceSoul := NativeSoulCustom(stashHW1, "alice")
	aliceKeypair := DeriveKeypair(aliceSoul)
	aliceEncKeypair := DeriveEncryptionKeys(aliceKeypair.PrivateKey)

	// Create stash
	stashData := &StashData{
		Timestamp: time.Now().Unix(),
		Emojis:    []string{"🦋", "🐝", "🐛"},
	}
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
	// Broadcast request, confident sends payload
	bobManager := NewConfidentStashStore()

	// Pre-store alice's stash in bob
	aliceSoul := NativeSoulCustom(stashHW1, "alice")
	aliceKeypair := DeriveKeypair(aliceSoul)
	aliceEncKeypair := DeriveEncryptionKeys(aliceKeypair.PrivateKey)

	stashData := &StashData{
		Timestamp: time.Now().Unix(),
		Emojis:    []string{"🦎", "🐢"},
	}
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
	// Owner asks confident to delete, confident removes from memory
	bobManager := NewConfidentStashStore()

	// Pre-store alice's stash
	aliceSoul := NativeSoulCustom(stashHW1, "alice")
	aliceKeypair := DeriveKeypair(aliceSoul)
	aliceEncKeypair := DeriveEncryptionKeys(aliceKeypair.PrivateKey)

	payload, _ := CreateStashPayload("alice", &StashData{
		Timestamp: time.Now().Unix(),
		Emojis:    []string{"🔥", "⭐"},
	}, aliceEncKeypair)
	bobManager.Store("alice", payload)

	if !bobManager.HasStashFor("alice") {
		t.Fatal("Setup: bob should have alice's stash")
	}

	// Alice sends delete request
	deleteMsg := &StashDelete{
		From: "alice",
	}
	deleteMsg.Sign(aliceKeypair)

	// Bob handles delete
	err := bobManager.HandleStashDelete(deleteMsg, func(name string) []byte {
		if name == "alice" {
			return aliceKeypair.PublicKey
		}
		return nil
	})
	if err != nil {
		t.Fatalf("HandleStashDelete failed: %v", err)
	}

	// Bob should no longer have alice's stash
	if bobManager.HasStashFor("alice") {
		t.Error("Bob should have deleted alice's stash")
	}
}

func TestConfidentGoesOffline(t *testing.T) {
	// Confident disappears, owner picks new random one
	tracker := NewConfidentTracker(2)
	tracker.Add("bob", time.Now().Unix())
	tracker.Add("carol", time.Now().Unix())

	online := []string{"alice", "carol", "dave"} // bob is offline

	// Mark bob as gone
	tracker.MarkOffline("bob", online)

	// Bob should be removed
	if tracker.Has("bob") {
		t.Error("Offline confident should be removed")
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
	// >3 confidents, owner asks extras to delete
	tracker := NewConfidentTracker(2) // target: 2

	tracker.Add("bob", time.Now().Unix())
	tracker.Add("carol", time.Now().Unix())
	tracker.Add("dave", time.Now().Unix())
	tracker.Add("eve", time.Now().Unix())

	// Should have excess
	excess := tracker.GetExcess()
	if len(excess) != 2 {
		t.Errorf("Expected 2 excess confidents, got %d", len(excess))
	}
}

func TestCloutRewardForStoring(t *testing.T) {
	// Confident gets clout when storing stash
	personality := NaraPersonality{Sociability: 50, Agreeableness: 50, Chill: 50}
	ledger := NewSyncLedger(1000)
	cloutProjection := NewCloutProjection(ledger)

	// Record stash stored event
	event := SyncEvent{
		Timestamp: time.Now().Unix(),
		Service:   ServiceSocial,
		Social: &SocialEventPayload{
			Type:   "service", // service events always give positive clout
			Actor:  "bob",
			Target: "alice",
			Reason: ReasonStashStored,
		},
	}
	event.ComputeID()

	added := ledger.AddEvent(event)
	if !added {
		t.Error("Should add stash stored event")
	}

	// Process events before querying
	cloutProjection.RunToEnd(context.Background())

	// Bob should gain clout
	clout := cloutProjection.DeriveClout("observer-soul", personality)
	if clout["bob"] <= 0 {
		t.Error("Bob should gain clout for storing stash")
	}
}

// =====================================================
// Size Limit Tests
// =====================================================

func TestStashSizeLimit(t *testing.T) {
	// Stash should be limited to 10KB
	manager := NewConfidentStashStore()

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
	// Full end-to-end flow: create emojis, stash to confidents, recover from scratch

	// Setup: Create owner (alice) with emoji identity
	aliceSoul := NativeSoulCustom(stashHW1, "alice")
	aliceKeypair := DeriveKeypair(aliceSoul)
	aliceEncKeypair := DeriveEncryptionKeys(aliceKeypair.PrivateKey)

	// Create stash data with emojis
	originalEmojis := []string{"🌸", "🦋", "🌊", "🔥", "⭐", "🌙", "🌈", "⚡", "❄️", "🍄"}
	timestamp := time.Now().Unix() - 86400 // 1 day ago
	stashData := &StashData{
		Timestamp: timestamp,
		Emojis:    originalEmojis,
	}
	payload, err := CreateStashPayload("alice", stashData, aliceEncKeypair)
	if err != nil {
		t.Fatalf("CreateStashPayload failed: %v", err)
	}

	// Setup: Two confidents (bob and carol)
	bobStore := NewConfidentStashStore()
	carolStore := NewConfidentStashStore()

	getPublicKey := func(name string) []byte {
		if name == "alice" {
			return aliceKeypair.PublicKey
		}
		return nil
	}

	// Phase 1: Alice stores stash with confidents
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

	// Verify both confidents have the stash
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

	// Verify recovered emojis match original
	recoveredEmojis := recoveryManager.GetRecoveredEmojis()
	if len(recoveredEmojis) != len(originalEmojis) {
		t.Errorf("Emoji count mismatch: expected %d, got %d", len(originalEmojis), len(recoveredEmojis))
	}
	for i, e := range originalEmojis {
		if recoveredEmojis[i] != e {
			t.Errorf("Emoji mismatch at %d: expected %s, got %s", i, e, recoveredEmojis[i])
		}
	}

	// Verify timestamp matches
	if recoveryManager.GetRecoveredTimestamp() != timestamp {
		t.Errorf("Timestamp mismatch: expected %d, got %d", timestamp, recoveryManager.GetRecoveredTimestamp())
	}

	t.Log("End-to-end stash flow completed successfully")
}

func TestConfidentTrackerWithMockNetwork(t *testing.T) {
	// Test confident management with simulated network churn

	tracker := NewConfidentTracker(2)
	online := []string{"alice", "bob", "carol", "dave", "eve"}

	// Alice picks initial confidents
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
		t.Error("Should be satisfied with 2 confidents")
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
		// Need to pick new confidents
		newConf := tracker.SelectRandom("alice", newOnline)
		if newConf != "" {
			tracker.Add(newConf, time.Now().Unix())
		}
	}

	t.Logf("Final confident count: %d", tracker.Count())
}

func TestConcurrentStashRequests(t *testing.T) {
	// Test handling multiple simultaneous stash requests

	// Setup one confident with alice's stash
	confidentStore := NewConfidentStashStore()

	aliceSoul := NativeSoulCustom(stashHW1, "alice")
	aliceKeypair := DeriveKeypair(aliceSoul)
	aliceEncKeypair := DeriveEncryptionKeys(aliceKeypair.PrivateKey)

	stashData := &StashData{
		Timestamp: time.Now().Unix(),
		Emojis:    []string{"🐙", "🦑", "🦀"},
	}
	payload, _ := CreateStashPayload("alice", stashData, aliceEncKeypair)

	storeMsg := &StashStore{From: "alice", Payload: *payload}
	storeMsg.Sign(aliceKeypair)
	confidentStore.HandleStashStore(storeMsg, func(name string) []byte {
		if name == "alice" {
			return aliceKeypair.PublicKey
		}
		return nil
	})

	// Simulate concurrent requests
	results := make(chan *StashResponse, 10)
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			request := &StashRequest{From: "alice"}
			request.Sign(aliceKeypair)
			response := confidentStore.HandleStashRequest(request, func(name string) []byte {
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
		emojis    []string
	}{
		{"bob", now - 3600, []string{"🌸", "🦋"}},  // 1 hour ago
		{"carol", now - 60, []string{"🌊", "🔥"}},  // 1 minute ago
		{"dave", now - 1800, []string{"⭐", "🌙"}}, // 30 minutes ago
	}

	responses := make(chan *StashResponse, 10)
	for _, s := range stashes {
		data := &StashData{
			Timestamp: s.timestamp,
			Emojis:    s.emojis,
		}
		payload, _ := CreateStashPayload("alice", data, aliceEncKeypair)
		responses <- &StashResponse{
			From:    s.from,
			Owner:   "alice",
			Payload: *payload,
		}
	}
	close(responses)

	manager := NewStashManager("alice", aliceKeypair, 2)
	manager.ProcessResponses(responses)

	// Should have picked carol's (newest)
	recoveredTimestamp := manager.GetRecoveredTimestamp()
	if recoveredTimestamp != now-60 {
		t.Errorf("Should pick newest timestamp (carol's), got timestamp from %ds ago", now-recoveredTimestamp)
	}
}
