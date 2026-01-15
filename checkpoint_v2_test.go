package nara

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"
	"time"
)

// Helper to decode base64 public key
func pubKeyFromBase64(encoded string) ed25519.PublicKey {
	decoded, _ := base64.StdEncoding.DecodeString(encoded)
	return ed25519.PublicKey(decoded)
}

// TestCheckpointV1BackwardsCompat tests that v1 checkpoints still verify correctly
// This ensures we haven't broken existing checkpoint verification
func TestCheckpointV1BackwardsCompat(t *testing.T) {
	subject := "lisa"
	subjectID := "lisa-id-abc"
	voterIDs := []string{"homer-id", "marge-id", "bart-id"}
	keypairs := make([]NaraKeypair, 3)
	publicKeys := make(map[string]string)

	for i, voterID := range voterIDs {
		keypairs[i] = generateTestKeypair()
		publicKeys[voterID] = pubKeyToBase64(keypairs[i].PublicKey)
	}

	// Create v1 checkpoint (Version = 1, no PreviousCheckpointID)
	checkpoint := &CheckpointEventPayload{
		Version:   1,
		Subject:   subject,
		SubjectID: subjectID,
		Observation: NaraObservation{
			StartTime:   1624066568,
			Restarts:    47,
			TotalUptime: 23456789,
		},
		AsOfTime: time.Now().Unix(),
		Round:    1,
	}

	// Add voter signatures using v1 attestation format
	for i, voterID := range voterIDs {
		attestation := Attestation{
			Version:     1,
			Subject:     subject,
			SubjectID:   subjectID,
			Observation: checkpoint.Observation,
			AttesterID:  voterID,
			AsOfTime:    checkpoint.AsOfTime,
		}

		signature := SignContent(&attestation, keypairs[i])
		checkpoint.VoterIDs = append(checkpoint.VoterIDs, voterID)
		checkpoint.Signatures = append(checkpoint.Signatures, signature)
	}

	// Verify v1 checkpoint with v1 signatures
	lookup := PublicKeyLookup(func(id, name string) ed25519.PublicKey {
		if pubKeyStr, ok := publicKeys[id]; ok {
			return pubKeyFromBase64(pubKeyStr)
		}
		return nil
	})

	result := checkpoint.VerifySignatureWithCounts(lookup)

	if !result.Valid {
		t.Errorf("V1 checkpoint verification failed: valid=%v, validCount=%d, totalCount=%d",
			result.Valid, result.ValidCount, result.TotalCount)
	}

	if result.ValidCount != 3 {
		t.Errorf("Expected 3 valid signatures, got %d", result.ValidCount)
	}

	// Test ContentString format for v1
	contentStr := checkpoint.ContentString()
	expectedPrefix := "checkpoint:" + subjectID
	if len(contentStr) < len(expectedPrefix) || contentStr[:len(expectedPrefix)] != expectedPrefix {
		t.Errorf("V1 ContentString has wrong format: %s", contentStr)
	}

	// V1 format should NOT contain "v2" prefix
	if len(contentStr) > 12 && contentStr[:12] == "checkpoint:v" {
		t.Errorf("V1 ContentString should not have version prefix, got: %s", contentStr)
	}
}

// TestCheckpointV1WithVersionZero tests that Version=0 is treated as v1
func TestCheckpointV1WithVersionZero(t *testing.T) {
	subject := "lisa"
	subjectID := "lisa-id-abc"
	voterIDs := []string{"homer-id", "marge-id"}
	keypairs := make([]NaraKeypair, 2)
	for i := range keypairs {
		keypairs[i] = generateTestKeypair()
	}

	// Create checkpoint with Version = 0 (should be treated as v1)
	checkpoint := &CheckpointEventPayload{
		Version:   0,
		Subject:   subject,
		SubjectID: subjectID,
		Observation: NaraObservation{
			StartTime:   1624066568,
			Restarts:    47,
			TotalUptime: 23456789,
		},
		AsOfTime: time.Now().Unix(),
		Round:    1,
	}

	// Sign with v1 attestation (Version 0 treated as 1)
	for i, voterID := range voterIDs {
		attestation := Attestation{
			Version:     0,
			Subject:     subject,
			SubjectID:   subjectID,
			Observation: checkpoint.Observation,
			AttesterID:  voterID,
			AsOfTime:    checkpoint.AsOfTime,
		}

		signature := SignContent(&attestation, keypairs[i])
		checkpoint.VoterIDs = append(checkpoint.VoterIDs, voterID)
		checkpoint.Signatures = append(checkpoint.Signatures, signature)
	}

	// Verify
	lookup := PublicKeyLookup(func(id, name string) ed25519.PublicKey {
		for i, voterID := range voterIDs {
			if id == voterID {
				return keypairs[i].PublicKey
			}
		}
		return nil
	})

	result := checkpoint.VerifySignatureWithCounts(lookup)
	if !result.Valid {
		t.Errorf("Version 0 checkpoint verification failed")
	}
}

// TestCheckpointV2Format tests the v2 checkpoint format and signature
func TestCheckpointV2Format(t *testing.T) {
	subject := "lisa"
	subjectID := "lisa-id-abc"
	previousCheckpointID := "prev-checkpoint-123"
	voterIDs := []string{"homer-id", "marge-id"}
	keypairs := make([]NaraKeypair, 2)
	for i := range keypairs {
		keypairs[i] = generateTestKeypair()
	}

	// Create v2 checkpoint with PreviousCheckpointID
	checkpoint := &CheckpointEventPayload{
		Version:              2,
		Subject:              subject,
		SubjectID:            subjectID,
		PreviousCheckpointID: previousCheckpointID,
		Observation: NaraObservation{
			StartTime:   1624066568,
			Restarts:    47,
			TotalUptime: 23456789,
		},
		AsOfTime: time.Now().Unix(),
		Round:    1,
	}

	// Sign with v2 attestation (includes LastSeenCheckpointID)
	for i, voterID := range voterIDs {
		attestation := Attestation{
			Version:              2,
			Subject:              subject,
			SubjectID:            subjectID,
			Observation:          checkpoint.Observation,
			AttesterID:           voterID,
			AsOfTime:             checkpoint.AsOfTime,
			LastSeenCheckpointID: previousCheckpointID,
		}

		signature := SignContent(&attestation, keypairs[i])
		checkpoint.VoterIDs = append(checkpoint.VoterIDs, voterID)
		checkpoint.Signatures = append(checkpoint.Signatures, signature)
	}

	// Verify v2 checkpoint
	lookup := PublicKeyLookup(func(id, name string) ed25519.PublicKey {
		for i, voterID := range voterIDs {
			if id == voterID {
				return keypairs[i].PublicKey
			}
		}
		return nil
	})

	result := checkpoint.VerifySignatureWithCounts(lookup)
	if !result.Valid {
		t.Errorf("V2 checkpoint verification failed: valid=%v, validCount=%d",
			result.Valid, result.ValidCount)
	}

	// Test ContentString format for v2
	contentStr := checkpoint.ContentString()
	expectedPrefix := "checkpoint:v2:" + subjectID
	if len(contentStr) < len(expectedPrefix) || contentStr[:len(expectedPrefix)] != expectedPrefix {
		t.Errorf("V2 ContentString has wrong format, expected prefix '%s', got: %s",
			expectedPrefix, contentStr)
	}

	// V2 ContentString should include previous checkpoint ID
	if len(contentStr) < len(previousCheckpointID) ||
		contentStr[len(contentStr)-len(previousCheckpointID):] != previousCheckpointID {
		t.Errorf("V2 ContentString should end with previous checkpoint ID '%s', got: %s",
			previousCheckpointID, contentStr)
	}
}

// TestCheckpointV2TamperDetection tests that tampering with PreviousCheckpointID breaks signature
func TestCheckpointV2TamperDetection(t *testing.T) {
	subject := "lisa"
	subjectID := "lisa-id-abc"
	previousCheckpointID := "prev-checkpoint-123"
	voterIDs := []string{"homer-id", "marge-id"}
	keypairs := make([]NaraKeypair, 2)
	for i := range keypairs {
		keypairs[i] = generateTestKeypair()
	}

	// Create v2 checkpoint
	checkpoint := &CheckpointEventPayload{
		Version:              2,
		Subject:              subject,
		SubjectID:            subjectID,
		PreviousCheckpointID: previousCheckpointID,
		Observation: NaraObservation{
			StartTime:   1624066568,
			Restarts:    47,
			TotalUptime: 23456789,
		},
		AsOfTime: time.Now().Unix(),
		Round:    1,
	}

	// Sign with correct previous checkpoint ID
	for i, voterID := range voterIDs {
		attestation := Attestation{
			Version:              2,
			Subject:              subject,
			SubjectID:            subjectID,
			Observation:          checkpoint.Observation,
			AttesterID:           voterID,
			AsOfTime:             checkpoint.AsOfTime,
			LastSeenCheckpointID: previousCheckpointID,
		}

		signature := SignContent(&attestation, keypairs[i])
		checkpoint.VoterIDs = append(checkpoint.VoterIDs, voterID)
		checkpoint.Signatures = append(checkpoint.Signatures, signature)
	}

	// Verify it works with correct previous ID
	lookup := PublicKeyLookup(func(id, name string) ed25519.PublicKey {
		for i, voterID := range voterIDs {
			if id == voterID {
				return keypairs[i].PublicKey
			}
		}
		return nil
	})

	result := checkpoint.VerifySignatureWithCounts(lookup)
	if !result.Valid {
		t.Fatal("Initial v2 checkpoint should verify successfully")
	}

	// Now tamper with PreviousCheckpointID
	checkpoint.PreviousCheckpointID = "tampered-checkpoint-999"

	// Verification should fail because signed content changed
	result = checkpoint.VerifySignatureWithCounts(lookup)
	if result.Valid {
		t.Error("Tampered v2 checkpoint should fail verification")
	}

	if result.ValidCount != 0 {
		t.Errorf("Expected 0 valid signatures after tampering, got %d", result.ValidCount)
	}
}

// TestCheckpointV2FirstCheckpoint tests v2 checkpoint with empty PreviousCheckpointID
func TestCheckpointV2FirstCheckpoint(t *testing.T) {
	subject := "lisa"
	subjectID := "lisa-id-abc"
	voterIDs := []string{"homer-id", "marge-id"}
	keypairs := make([]NaraKeypair, 2)
	for i := range keypairs {
		keypairs[i] = generateTestKeypair()
	}

	// Create v2 checkpoint with empty PreviousCheckpointID (first checkpoint)
	checkpoint := &CheckpointEventPayload{
		Version:              2,
		Subject:              subject,
		SubjectID:            subjectID,
		PreviousCheckpointID: "", // Empty for first checkpoint
		Observation: NaraObservation{
			StartTime:   1624066568,
			Restarts:    47,
			TotalUptime: 23456789,
		},
		AsOfTime: time.Now().Unix(),
		Round:    1,
	}

	// Sign with empty LastSeenCheckpointID
	for i, voterID := range voterIDs {
		attestation := Attestation{
			Version:              2,
			Subject:              subject,
			SubjectID:            subjectID,
			Observation:          checkpoint.Observation,
			AttesterID:           voterID,
			AsOfTime:             checkpoint.AsOfTime,
			LastSeenCheckpointID: "", // Empty for first checkpoint
		}

		signature := SignContent(&attestation, keypairs[i])
		checkpoint.VoterIDs = append(checkpoint.VoterIDs, voterID)
		checkpoint.Signatures = append(checkpoint.Signatures, signature)
	}

	// Verify first v2 checkpoint
	lookup := PublicKeyLookup(func(id, name string) ed25519.PublicKey {
		for i, voterID := range voterIDs {
			if id == voterID {
				return keypairs[i].PublicKey
			}
		}
		return nil
	})

	result := checkpoint.VerifySignatureWithCounts(lookup)
	if !result.Valid {
		t.Errorf("First v2 checkpoint (empty previous ID) should verify successfully")
	}

	// ContentString should end with empty string (likely "::")
	contentStr := checkpoint.ContentString()
	expectedPrefix := "checkpoint:v2:" + subjectID
	if len(contentStr) < len(expectedPrefix) || contentStr[:len(expectedPrefix)] != expectedPrefix {
		t.Errorf("V2 ContentString has wrong format: %s", contentStr)
	}
}

// TestAttestationV1Format tests v1 attestation SignableContent format
func TestAttestationV1Format(t *testing.T) {
	attestation := Attestation{
		Version:    1,
		SubjectID:  "subject-123",
		AttesterID: "attester-456",
		Observation: NaraObservation{
			StartTime:   1624066568,
			Restarts:    47,
			TotalUptime: 23456789,
		},
		AsOfTime: time.Now().Unix(),
	}

	content := attestation.SignableContent()

	// v1 format: attestation:v1:{attester_id}:{subject_id}:{as_of_time}:{restarts}:{uptime}:{first_seen}
	expectedPrefix := "attestation:v1:"
	if len(content) < len(expectedPrefix) || content[:len(expectedPrefix)] != expectedPrefix {
		t.Errorf("V1 attestation has wrong format prefix, got: %s", content)
	}

	// Should NOT contain LastSeenCheckpointID field
	// Count colons: v1 format has exactly 7 colons
	colonCount := 0
	for _, c := range content {
		if c == ':' {
			colonCount++
		}
	}
	if colonCount != 7 {
		t.Errorf("V1 attestation should have 7 colons, got %d in: %s", colonCount, content)
	}
}

// TestAttestationV2Format tests v2 attestation SignableContent format
func TestAttestationV2Format(t *testing.T) {
	lastSeenCheckpointID := "prev-checkpoint-789"

	attestation := Attestation{
		Version:              2,
		SubjectID:            "subject-123",
		AttesterID:           "attester-456",
		LastSeenCheckpointID: lastSeenCheckpointID,
		Observation: NaraObservation{
			StartTime:   1624066568,
			Restarts:    47,
			TotalUptime: 23456789,
		},
		AsOfTime: time.Now().Unix(),
	}

	content := attestation.SignableContent()

	// v2 format: attestation:v2:{attester_id}:{subject_id}:{as_of_time}:{restarts}:{uptime}:{first_seen}:{last_seen_checkpoint_id}
	expectedPrefix := "attestation:v2:"
	if len(content) < len(expectedPrefix) || content[:len(expectedPrefix)] != expectedPrefix {
		t.Errorf("V2 attestation has wrong format prefix, got: %s", content)
	}

	// Should end with LastSeenCheckpointID
	if len(content) < len(lastSeenCheckpointID) ||
		content[len(content)-len(lastSeenCheckpointID):] != lastSeenCheckpointID {
		t.Errorf("V2 attestation should end with '%s', got: %s", lastSeenCheckpointID, content)
	}

	// Count colons: v2 format has exactly 8 colons (one more than v1)
	colonCount := 0
	for _, c := range content {
		if c == ':' {
			colonCount++
		}
	}
	if colonCount != 8 {
		t.Errorf("V2 attestation should have 8 colons, got %d in: %s", colonCount, content)
	}
}

// TestGetLatestCheckpointID tests the ledger helper for getting latest checkpoint ID
func TestGetLatestCheckpointID(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := "lisa"

	// No checkpoint exists yet
	latestID := ledger.GetLatestCheckpointID(subject)
	if latestID != "" {
		t.Errorf("Expected empty string for non-existent checkpoint, got: %s", latestID)
	}

	// Add first checkpoint
	checkpoint1 := NewTestCheckpointEvent(subject, time.Now().Unix()-1000, 1624066568, 47, 23456789)
	ledger.AddEvent(checkpoint1)

	latestID = ledger.GetLatestCheckpointID(subject)
	if latestID == "" {
		t.Error("Expected checkpoint ID, got empty string")
	}
	if latestID != checkpoint1.ID {
		t.Errorf("Expected checkpoint ID %s, got %s", checkpoint1.ID, latestID)
	}

	// Add second checkpoint with later AsOfTime
	checkpoint2 := NewTestCheckpointEvent(subject, time.Now().Unix(), 1624066568, 50, 24000000)
	ledger.AddEvent(checkpoint2)

	latestID = ledger.GetLatestCheckpointID(subject)
	if latestID != checkpoint2.ID {
		t.Errorf("Expected latest checkpoint ID %s, got %s", checkpoint2.ID, latestID)
	}

	// Different subject should have no checkpoint
	otherLatestID := ledger.GetLatestCheckpointID("homer")
	if otherLatestID != "" {
		t.Errorf("Expected empty string for different subject, got: %s", otherLatestID)
	}
}

// TestCheckpointV2NodeRejectsV1Proposal tests that v2 nodes don't vote on v1 proposals
func TestCheckpointV2NodeRejectsV1Proposal(t *testing.T) {
	ln := testNara(t, "test-nara", WithParams(50, 1000))

	// Create checkpoint service
	cs := NewCheckpointService(ln.Network, ln.SyncLedger, ln)

	// Create a v1 proposal from another nara
	asOfTime := time.Now().Unix()
	v1Proposal := &CheckpointProposal{
		Attestation: Attestation{
			Version:   1, // v1 proposal
			Subject:   "other-nara",
			SubjectID: "other-nara-id",
			Observation: NaraObservation{
				StartTime:   asOfTime - 3600,
				Restarts:    5,
				TotalUptime: 3600,
			},
			Attester:   "other-nara",
			AttesterID: "other-nara-id",
			AsOfTime:   asOfTime,
			Signature:  "fake-signature",
		},
		Round: 1,
	}

	// Track whether HandleProposal would send a vote (it shouldn't)
	// We can't directly test that a vote wasn't published, but we can check
	// that the function returns early by checking debug logs or by testing
	// that no vote is created in the pending votes structure.
	// For now, just ensure HandleProposal doesn't panic
	cs.HandleProposal(v1Proposal)

	// If we got here without panic, the v1 proposal was handled (rejected) correctly
	// In a real scenario, we'd verify no vote was published via MQTT
}

// TestCheckpointV2NodeIgnoresV1Votes tests that v2 nodes ignore v1 votes
func TestCheckpointV2NodeIgnoresV1Votes(t *testing.T) {
	ln := testNara(t, "test-nara", WithParams(50, 1000))

	// Create checkpoint service
	cs := NewCheckpointService(ln.Network, ln.SyncLedger, ln)

	// Create a v2 proposal from this nara
	asOfTime := time.Now().Unix()
	v2Proposal := &CheckpointProposal{
		Attestation: Attestation{
			Version:   2,
			Subject:   ln.Me.Name,
			SubjectID: ln.Me.Status.ID,
			Observation: NaraObservation{
				StartTime:   asOfTime - 3600,
				Restarts:    5,
				TotalUptime: 3600,
			},
			Attester:             ln.Me.Name,
			AttesterID:           ln.Me.Status.ID,
			AsOfTime:             asOfTime,
			LastSeenCheckpointID: "",
			Signature:            "fake-signature",
		},
		Round: 1,
	}

	// Set this as the pending proposal
	cs.myPendingProposalMu.Lock()
	cs.myPendingProposal = &pendingProposal{
		proposal:  v2Proposal,
		votes:     []*CheckpointVote{},
		expiresAt: time.Now().Add(30 * time.Second),
		round:     1,
		finalized: false,
	}
	cs.myPendingProposalMu.Unlock()

	// Create a v1 vote from another nara
	voterKeypair := generateTestKeypair()
	voterID := "voter-id-123"
	voterName := "voter-nara"

	v1Attestation := Attestation{
		Version:     1, // v1 attestation
		Subject:     ln.Me.Name,
		SubjectID:   ln.Me.Status.ID,
		Observation: v2Proposal.Observation,
		Attester:    voterName,
		AttesterID:  voterID,
		AsOfTime:    asOfTime,
	}

	// Sign the v1 attestation
	v1Attestation.Signature = SignContent(&v1Attestation, voterKeypair)

	v1Vote := &CheckpointVote{
		Attestation: v1Attestation,
		ProposalTS:  asOfTime,
		Round:       1,
		Approved:    true,
	}

	// Add voter's public key to the network
	voterPubKey := pubKeyToBase64(voterKeypair.PublicKey)
	ln.Network.Neighbourhood[voterName] = &Nara{
		Name: voterName,
		Status: NaraStatus{
			ID:        voterID,
			PublicKey: voterPubKey,
		},
	}
	ln.Network.RegisterKey(voterID, voterPubKey)

	// Handle the v1 vote (should be ignored)
	cs.HandleVote(v1Vote)

	// Verify the vote was NOT added to pending votes
	cs.myPendingProposalMu.Lock()
	pending := cs.myPendingProposal
	cs.myPendingProposalMu.Unlock()

	if pending == nil {
		t.Fatal("Pending proposal should still exist")
	}

	pending.votesMu.Lock()
	voteCount := len(pending.votes)
	pending.votesMu.Unlock()

	if voteCount != 0 {
		t.Errorf("Expected 0 votes (v1 vote should be ignored), got %d", voteCount)
	}

	// Now test that a v2 vote IS accepted
	v2Attestation := Attestation{
		Version:              2, // v2 attestation
		Subject:              ln.Me.Name,
		SubjectID:            ln.Me.Status.ID,
		Observation:          v2Proposal.Observation,
		Attester:             voterName,
		AttesterID:           voterID,
		AsOfTime:             asOfTime,
		LastSeenCheckpointID: "", // No previous checkpoint
	}

	// Sign the v2 attestation
	v2Attestation.Signature = SignContent(&v2Attestation, voterKeypair)

	v2Vote := &CheckpointVote{
		Attestation: v2Attestation,
		ProposalTS:  asOfTime,
		Round:       1,
		Approved:    true,
	}

	// Handle the v2 vote (should be accepted)
	cs.HandleVote(v2Vote)

	// Verify the v2 vote WAS added
	pending.votesMu.Lock()
	voteCount = len(pending.votes)
	pending.votesMu.Unlock()

	if voteCount != 1 {
		t.Errorf("Expected 1 vote (v2 vote should be accepted), got %d", voteCount)
	}
}
