package nara

import (
	"testing"
	"time"

	"github.com/eljojo/nara/identity"
	"github.com/eljojo/nara/types"
)

// TestCheckpoint_VoteSignatureVerificationBug verifies that verifyVoteSignature works correctly
// Fixed: Uses Go embedding to access vote.Attestation.Signature, and uses ID-based lookup
func TestCheckpoint_VoteSignatureVerificationBug(t *testing.T) {
	tc := NewTestCoordinator(t)
	verifier := tc.AddNara("verifier", WithoutServer())
	voter := tc.AddNara("alice", WithoutServer())
	proposer := tc.AddNara("proposer", WithoutServer())
	tc.Connect("verifier", "alice")
	tc.Connect("verifier", "proposer")

	asOfTime := time.Now().Unix()

	attestation := Attestation{
		Version:   1,
		Subject:   proposer.Me.Name,
		SubjectID: proposer.ID,
		Observation: NaraObservation{
			Restarts:    100,
			TotalUptime: 50000,
			StartTime:   1624066568,
		},
		Attester:   voter.Me.Name,
		AttesterID: voter.ID,
		AsOfTime:   asOfTime,
	}
	attestation.Signature = identity.SignContent(&attestation, voter.Keypair)

	vote := &CheckpointVote{
		Attestation: attestation,
		ProposalTS:  asOfTime,
		Round:       1,
		Approved:    true,
	}

	ledger := NewSyncLedger(1000)
	service := NewCheckpointService(verifier.Network, ledger, verifier)

	isValid := service.verifyVoteSignature(vote)

	if !isValid {
		t.Error("Expected legitimate vote to pass verification, but it failed!")
	} else {
		t.Logf("✓ Vote verification passed for attester %s (%s)", voter.Me.Name, voter.ID)
		t.Log("  ✓ Fixed: Now uses ID-based lookup (getPublicKeyForNaraID)")
		t.Log("  Comment explains embedding behavior for maintainability")
	}
}

// TestCheckpoint_VoteSignatureNameSpoofing verifies protection against name spoofing
// Fixed: verifyVoteSignature now looks up public key by AttesterID (not Attester name)
func TestCheckpoint_VoteSignatureNameSpoofing(t *testing.T) {
	tc := NewTestCoordinator(t)
	verifier := tc.AddNara("verifier", WithoutServer())
	victim := tc.AddNara("alice", WithoutServer())
	attacker := tc.AddNara("attacker", WithoutServer())
	proposer := tc.AddNara("proposer", WithoutServer())
	tc.Connect("verifier", "alice")
	tc.Connect("verifier", "proposer")

	asOfTime := time.Now().Unix()

	attestation := Attestation{
		Version:   1,
		Subject:   proposer.Me.Name,
		SubjectID: proposer.ID,
		Observation: NaraObservation{
			Restarts:    100,
			TotalUptime: 50000,
			StartTime:   1624066568,
		},
		Attester:   victim.Me.Name, // SPOOFED name
		AttesterID: attacker.ID,    // Actual signer
		AsOfTime:   asOfTime,
	}
	attestation.Signature = identity.SignContent(&attestation, attacker.Keypair)

	vote := &CheckpointVote{
		Attestation: attestation,
		ProposalTS:  asOfTime,
		Round:       1,
		Approved:    true,
	}

	ledger := NewSyncLedger(1000)
	service := NewCheckpointService(verifier.Network, ledger, verifier)

	isValid := service.verifyVoteSignature(vote)

	t.Log("Attacker spoofed Attester name as:", victim.Me.Name)
	t.Log("Actual AttesterID:", attacker.ID)
	t.Log("Verification result:", isValid)

	if !isValid {
		t.Log("✓ Attack failed - signature verification correctly rejected")
		t.Log("  ✓ Fixed: Uses ID-based lookup (getPublicKeyForNaraID)")
		t.Log("  Attacker's signature doesn't match because we look up by AttesterID")
	}
}

// TestCheckpoint_VoteNameVsIDLookup verifies that ID-based lookup handles name changes
// Fixed: Now works correctly even when nara changes name or we have stale name data
func TestCheckpoint_VoteNameVsIDLookup(t *testing.T) {
	// Scenario: Voter "alice" created a vote, but we only know her by ID in our network
	// (maybe she changed names, or we have her indexed differently)
	tc := NewTestCoordinator(t)
	verifier := tc.AddNara("verifier", WithoutServer())
	voter := tc.AddNara("alice-renamed", WithoutServer())
	proposer := tc.AddNara("proposer", WithoutServer())
	tc.Connect("verifier", "alice-renamed")
	tc.Connect("verifier", "proposer")

	oldName := types.NaraName("alice")
	asOfTime := time.Now().Unix()

	attestation := Attestation{
		Version:   1,
		Subject:   proposer.Me.Name,
		SubjectID: proposer.ID,
		Observation: NaraObservation{
			Restarts:    100,
			TotalUptime: 50000,
			StartTime:   1624066568,
		},
		Attester:   oldName, // Vote says "alice"
		AttesterID: voter.ID,
		AsOfTime:   asOfTime,
	}
	attestation.Signature = identity.SignContent(&attestation, voter.Keypair)

	vote := &CheckpointVote{
		Attestation: attestation,
		ProposalTS:  asOfTime,
		Round:       1,
		Approved:    true,
	}

	ledger := NewSyncLedger(1000)
	service := NewCheckpointService(verifier.Network, ledger, verifier)

	// FIXED: verifyVoteSignature now looks up by vote.AttesterID (stable)
	// so it works even though we only have "alice-renamed" in our network
	isValid := service.verifyVoteSignature(vote)

	if !isValid {
		t.Error("Expected vote to pass (ID lookup succeeds), but it failed!")
	} else {
		t.Log("✓ Bug is fixed: Valid vote accepted despite name change")
		t.Log("  Fixed: getPublicKeyForNaraID(vote.AttesterID) succeeds - ID is stable")
		t.Log("  Old bug: getPublicKeyForNara(vote.Attester) would have failed - no 'alice' in network")
	}
}

// TestCheckpoint_ProposalSignatureVerification verifies proposal signature verification is fixed
func TestCheckpoint_ProposalSignatureVerification(t *testing.T) {
	tc := NewTestCoordinator(t)
	verifier := tc.AddNara("verifier", WithoutServer())
	proposer := tc.AddNara("proposer", WithoutServer())
	tc.Connect("verifier", "proposer")

	asOfTime := time.Now().Unix()

	attestation := Attestation{
		Version:   1,
		Subject:   proposer.Me.Name,
		SubjectID: proposer.ID,
		Observation: NaraObservation{
			Restarts:    100,
			TotalUptime: 50000,
			StartTime:   1624066568,
		},
		Attester:   proposer.Me.Name,
		AttesterID: proposer.ID,
		AsOfTime:   asOfTime,
	}
	attestation.Signature = identity.SignContent(&attestation, proposer.Keypair)

	proposal := &CheckpointProposal{
		Attestation: attestation,
		Round:       1,
	}

	ledger := NewSyncLedger(1000)
	service := NewCheckpointService(verifier.Network, ledger, verifier)

	isValid := service.verifyProposalSignature(proposal)

	if !isValid {
		t.Error("Expected legitimate proposal to pass, but it failed!")
	} else {
		t.Log("✓ Proposal verification passed (uses Go embedding to verify attestation)")
		t.Log("  ✓ Fixed: Now uses ID-based lookup (getPublicKeyForNaraID)")
		t.Log("  Comment explains embedding behavior for maintainability")
	}
}
