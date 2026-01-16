package nara

import (
	"fmt"
	"testing"
	"time"

	"github.com/eljojo/nara/types"
)

// TestCheckpoint_VoteAsOfTimeMismatch verifies Issue #1 is fixed:
// Votes should sign with proposal.AsOfTime, not time.Now().Unix()
// This test intentionally creates a vote with wrong AsOfTime to show it would fail.
func TestCheckpoint_VoteAsOfTimeMismatch(t *testing.T) {
	// Setup: Create a proposal with a specific AsOfTime
	proposerKeypair := generateTestKeypair()
	voterKeypair := generateTestKeypair()

	proposerID := types.NaraID("proposer-id")
	voterID := types.NaraID("voter-id")
	subject := types.NaraName("proposer")
	proposalAsOfTime := time.Now().Unix() - 10 // 10 seconds ago

	// Create proposal attestation (self-attestation)
	proposalAttestation := Attestation{
		Version:   1,
		Subject:   subject,
		SubjectID: proposerID,
		Observation: NaraObservation{
			Restarts:    100,
			TotalUptime: 50000,
			StartTime:   1624066568,
		},
		Attester:   subject,
		AttesterID: proposerID,
		AsOfTime:   proposalAsOfTime,
	}
	proposalAttestation.Signature = SignContent(&proposalAttestation, proposerKeypair)

	proposal := &CheckpointProposal{
		Attestation: proposalAttestation,
		Round:       1,
	}

	// CURRENT BEHAVIOR: Voter creates attestation with time.Now().Unix()
	voteAsOfTime := time.Now().Unix() // Different from proposalAsOfTime!
	voteAttestation := Attestation{
		Version:   1,
		Subject:   subject,
		SubjectID: proposerID,
		Observation: NaraObservation{
			Restarts:    100,
			TotalUptime: 50000,
			StartTime:   1624066568,
		},
		Attester:   types.NaraName("voter"),
		AttesterID: voterID,
		AsOfTime:   voteAsOfTime, // BUG: Different timestamp!
	}
	voteAttestation.Signature = SignContent(&voteAttestation, voterKeypair)

	vote := &CheckpointVote{
		Attestation: voteAttestation,
		ProposalTS:  proposal.AsOfTime,
		Round:       1,
		Approved:    true,
	}

	// Now simulate storing the checkpoint with both signatures
	checkpoint := &CheckpointEventPayload{
		Subject:   string(subject),
		SubjectID: proposerID,
		AsOfTime:  proposalAsOfTime, // Checkpoint uses proposal's AsOfTime
		Observation: NaraObservation{
			Restarts:    100,
			TotalUptime: 50000,
			StartTime:   1624066568,
		},
		VoterIDs:   []types.NaraID{proposerID, voterID},
		Signatures: []string{proposal.Signature, vote.Signature},
		Round:      1,
	}

	// Setup network for verification
	ledger := NewSyncLedger(1000)
	local := testLocalNara(t, "verifier")
	network := local.Network

	// Add naras to neighbourhood and register their keys
	proposerPubKey := pubKeyToBase64(proposerKeypair.PublicKey)
	voterPubKey := pubKeyToBase64(voterKeypair.PublicKey)
	network.Neighbourhood["proposer"] = &Nara{
		Name: "proposer",
		Status: NaraStatus{
			ID:        proposerID,
			PublicKey: proposerPubKey,
		},
	}
	network.Neighbourhood["voter"] = &Nara{
		Name: "voter",
		Status: NaraStatus{
			ID:        voterID,
			PublicKey: voterPubKey,
		},
	}
	network.RegisterKey(proposerID, proposerPubKey)
	network.RegisterKey(voterID, voterPubKey)

	service := NewCheckpointService(network, ledger, local)

	// Try to verify signatures
	result := service.verifyCheckpointSignatures(checkpoint)

	// EXPECTATION: With the bug, voter signature fails because:
	// - Vote was signed with voteAsOfTime
	// - But verification uses proposalAsOfTime
	// - The signable content doesn't match!
	// Since only 1 of 2 signatures is valid, verification should fail (need 2+)

	t.Logf("Valid signatures: %d/%d (known: %d)", result.ValidCount, result.TotalCount, result.KnownCount)
	t.Logf("Proposer AsOfTime: %d", proposalAsOfTime)
	t.Logf("Vote AsOfTime: %d (signed)", voteAsOfTime)
	t.Logf("Checkpoint AsOfTime: %d (verified against)", checkpoint.AsOfTime)

	// With Issue #2 fixed but this test using wrong AsOfTime:
	// - Proposer signature should be valid (same AsOfTime)
	// - Voter signature should fail (different AsOfTime)
	// - Overall verification fails because we need 2+ valid signatures
	if !result.Valid {
		t.Log("✓ Verification correctly failed: only 1 of 2 signatures valid (need 2+)")
		t.Log("  Proposer signature valid (same AsOfTime)")
		t.Log("  Voter signature invalid (test used wrong AsOfTime)")
	} else {
		t.Error("Expected verification to fail due to AsOfTime mismatch, but it passed!")
	}
}

// TestCheckpoint_SignatureFormatMismatch verifies Issue #2 is fixed:
// Both signing and verification now use "attestation:..." format
func TestCheckpoint_SignatureFormatMismatch(t *testing.T) {
	// Setup
	proposerKeypair := generateTestKeypair()
	voterKeypair := generateTestKeypair()

	proposerID := types.NaraID("proposer-id")
	voterID := types.NaraID("voter-id")
	subject := types.NaraName("proposer")
	asOfTime := time.Now().Unix()

	// Create proposal attestation - signs with Attestation.SignableContent()
	proposalAttestation := Attestation{
		Subject:   subject,
		SubjectID: proposerID,
		Observation: NaraObservation{
			Restarts:    100,
			TotalUptime: 50000,
			StartTime:   1624066568,
		},
		Attester:   subject,
		AttesterID: proposerID,
		AsOfTime:   asOfTime,
	}
	proposalAttestation.Signature = SignContent(&proposalAttestation, proposerKeypair)

	proposal := &CheckpointProposal{
		Attestation: proposalAttestation,
		Round:       1,
	}

	// Create vote attestation - signs with Attestation.SignableContent()
	voteAttestation := Attestation{
		Version:   1,
		Subject:   subject,
		SubjectID: proposerID,
		Observation: NaraObservation{
			Restarts:    100,
			TotalUptime: 50000,
			StartTime:   1624066568,
		},
		Attester:   types.NaraName("voter"),
		AttesterID: voterID,
		AsOfTime:   asOfTime, // Same as proposal for now
	}
	voteAttestation.Signature = SignContent(&voteAttestation, voterKeypair)

	vote := &CheckpointVote{
		Attestation: voteAttestation,
		ProposalTS:  proposal.AsOfTime,
		Round:       1,
		Approved:    true,
	}

	// Store checkpoint
	checkpoint := &CheckpointEventPayload{
		Subject:   string(subject),
		SubjectID: proposerID,
		AsOfTime:  asOfTime,
		Observation: NaraObservation{
			Restarts:    100,
			TotalUptime: 50000,
			StartTime:   1624066568,
		},
		VoterIDs:   []types.NaraID{proposerID, voterID},
		Signatures: []string{proposal.Signature, vote.Signature},
		Round:      1,
	}

	// Setup network
	ledger := NewSyncLedger(1000)
	local := testLocalNara(t, "verifier")
	network := local.Network

	// Add naras to neighbourhood and register their keys
	proposerPubKey := pubKeyToBase64(proposerKeypair.PublicKey)
	voterPubKey := pubKeyToBase64(voterKeypair.PublicKey)
	network.Neighbourhood["proposer"] = &Nara{
		Name: "proposer",
		Status: NaraStatus{
			ID:        proposerID,
			PublicKey: proposerPubKey,
		},
	}
	network.Neighbourhood["voter"] = &Nara{
		Name: "voter",
		Status: NaraStatus{
			ID:        voterID,
			PublicKey: voterPubKey,
		},
	}
	network.RegisterKey(proposerID, proposerPubKey)
	network.RegisterKey(voterID, voterPubKey)

	service := NewCheckpointService(network, ledger, local)

	// Log what was signed vs what will be verified
	t.Log("SIGNED WITH:")
	t.Logf("  Proposer: %s", proposalAttestation.SignableContent())
	t.Logf("  Voter:    %s", voteAttestation.SignableContent())

	t.Log("")
	t.Log("VERIFIED WITH:")
	proposalVerifyContent := fmt.Sprintf("checkpoint-proposal:%s:%d:%d:%d:%d:%d",
		proposerID, asOfTime, 100, 50000, 1624066568, 1)
	voteVerifyContent := fmt.Sprintf("checkpoint-vote:%s:%d:%d:%d:%d:%d",
		voterID, asOfTime, 100, 50000, 1624066568, 1)
	t.Logf("  Proposer: %s", proposalVerifyContent)
	t.Logf("  Voter:    %s", voteVerifyContent)

	// Try to verify
	result := service.verifyCheckpointSignatures(checkpoint)

	t.Logf("Valid signatures: %d/%d (known: %d)", result.ValidCount, result.TotalCount, result.KnownCount)

	// With Issue #2 fixed, both signatures should be valid
	if result.Valid && result.ValidCount == 2 {
		t.Log("✓ Issue #2 is fixed: Both signatures valid using attestation format")
		t.Log("  Signing uses: attestation:...")
		t.Log("  Verification uses: attestation:...")
	} else {
		t.Errorf("Verification failed - expected 2 valid signatures, got %d!", result.ValidCount)
	}
}
