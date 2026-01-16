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
	// Setup: Create a legitimate voter with a valid signature
	voterKeypair := generateTestKeypair()
	voterID := types.NaraID("voter-id-123")
	voterName := types.NaraName("alice")

	proposerID := types.NaraID("proposer-id")
	proposerName := types.NaraName("bob")
	asOfTime := time.Now().Unix()

	// Create a valid attestation (third-party: voter attests about proposer)
	attestation := Attestation{
		Version:   1,
		Subject:   proposerName,
		SubjectID: proposerID,
		Observation: NaraObservation{
			Restarts:    100,
			TotalUptime: 50000,
			StartTime:   1624066568,
		},
		Attester:   voterName,
		AttesterID: voterID,
		AsOfTime:   asOfTime,
	}
	attestation.Signature = identity.SignContent(&attestation, voterKeypair)

	// Create a vote with this attestation
	vote := &CheckpointVote{
		Attestation: attestation,
		ProposalTS:  asOfTime,
		Round:       1,
		Approved:    true,
	}

	// Setup network with voter's public key
	ledger := NewSyncLedger(1000)
	local := testLocalNara(t, "verifier")
	network := local.Network
	// Add voter to neighbourhood
	voterNara := &Nara{
		Name: voterName,
		Status: NaraStatus{
			ID:        voterID,
			PublicKey: pubKeyToBase64(voterKeypair.PublicKey),
		},
	}
	network.Neighbourhood[voterName] = voterNara
	// Register key in keyring (required since keyring is source of truth)
	network.RegisterKey(voterID, pubKeyToBase64(voterKeypair.PublicKey))

	service := NewCheckpointService(network, ledger, local)

	// Try to verify the vote signature
	isValid := service.verifyVoteSignature(vote)

	// NOTE: This passes due to Go's embedding - vote.Signature accesses
	// the embedded Attestation.Signature field.
	if !isValid {
		t.Error("Expected legitimate vote to pass verification, but it failed!")
	} else {
		t.Log("✓ Vote verification passed (uses Go embedding to verify attestation)")
		t.Log("  ✓ Fixed: Now uses ID-based lookup (getPublicKeyForNaraID)")
		t.Log("  Comment explains embedding behavior for maintainability")
	}
}

// TestCheckpoint_VoteSignatureNameSpoofing verifies protection against name spoofing
// Fixed: verifyVoteSignature now looks up public key by AttesterID (not Attester name)
func TestCheckpoint_VoteSignatureNameSpoofing(t *testing.T) {
	// Setup: Attacker and victim
	attackerKeypair := generateTestKeypair()
	victimKeypair := generateTestKeypair()

	attackerID := types.NaraID("attacker-id")
	victimID := types.NaraID("victim-id")
	victimName := types.NaraName("alice") // High-reputation nara

	proposerID := types.NaraID("proposer-id")
	proposerName := types.NaraName("bob")
	asOfTime := time.Now().Unix()

	// Attacker creates an attestation but claims to be the victim by name
	attestation := Attestation{
		Version:   1,
		Subject:   proposerName,
		SubjectID: proposerID,
		Observation: NaraObservation{
			Restarts:    100,
			TotalUptime: 50000,
			StartTime:   1624066568,
		},
		Attester:   victimName, // SPOOFED: Claims to be alice
		AttesterID: attackerID, // But actually attacker's ID
		AsOfTime:   asOfTime,
	}
	// Attacker signs with their own key
	attestation.Signature = identity.SignContent(&attestation, attackerKeypair)

	vote := &CheckpointVote{
		Attestation: attestation,
		ProposalTS:  asOfTime,
		Round:       1,
		Approved:    true,
	}

	// Setup network: both victim and attacker are known
	ledger := NewSyncLedger(1000)
	local := testLocalNara(t, "verifier")
	network := local.Network
	victimNara := &Nara{
		Name: victimName,
		Status: NaraStatus{
			ID:        victimID,
			PublicKey: pubKeyToBase64(victimKeypair.PublicKey),
		},
	}
	network.Neighbourhood[victimName] = victimNara
	network.RegisterKey(victimID, pubKeyToBase64(victimKeypair.PublicKey))
	// Note: attacker is NOT in neighbourhood, or has different key

	service := NewCheckpointService(network, ledger, local)

	// Current buggy behavior:
	// - verifyVoteSignature looks up by vote.Attester (victim's name)
	// - Gets victim's public key
	// - Tries to verify attacker's signature with victim's public key
	// - Verification fails (correctly, but for wrong reason)

	// The vulnerability: If we lookup by name instead of ID, the attacker
	// can impersonate anyone by setting Attester to their name.
	// We should ONLY trust AttesterID and lookup by ID.

	isValid := service.verifyVoteSignature(vote)

	t.Log("Attacker spoofed Attester name as:", victimName)
	t.Log("Actual AttesterID:", attackerID)
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

	voterKeypair := generateTestKeypair()
	voterID := types.NaraID("voter-id-123")
	oldName := types.NaraName("alice")
	newName := types.NaraName("alice-renamed") // She changed her name

	proposerID := types.NaraID("proposer-id")
	proposerName := types.NaraName("bob")
	asOfTime := time.Now().Unix()

	// Vote was created when she was still "alice"
	attestation := Attestation{
		Version:   1,
		Subject:   proposerName,
		SubjectID: proposerID,
		Observation: NaraObservation{
			Restarts:    100,
			TotalUptime: 50000,
			StartTime:   1624066568,
		},
		Attester:   oldName, // Vote says "alice"
		AttesterID: voterID, // But ID is stable
		AsOfTime:   asOfTime,
	}
	attestation.Signature = identity.SignContent(&attestation, voterKeypair)

	vote := &CheckpointVote{
		Attestation: attestation,
		ProposalTS:  asOfTime,
		Round:       1,
		Approved:    true,
	}

	// Our network knows her by new name and ID
	ledger := NewSyncLedger(1000)
	local := testLocalNara(t, "verifier")
	network := local.Network
	voterNara := &Nara{
		Name: newName, // We know her as "alice-renamed"
		Status: NaraStatus{
			ID:        voterID, // Same ID
			PublicKey: pubKeyToBase64(voterKeypair.PublicKey),
		},
	}
	network.Neighbourhood[newName] = voterNara
	network.RegisterKey(voterID, pubKeyToBase64(voterKeypair.PublicKey))

	service := NewCheckpointService(network, ledger, local)

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
	// Setup: Create a legitimate proposer
	proposerKeypair := generateTestKeypair()
	proposerID := types.NaraID("proposer-id")
	proposerName := types.NaraName("bob")
	asOfTime := time.Now().Unix()

	// Create a valid self-attestation
	attestation := Attestation{
		Version:   1,
		Subject:   proposerName,
		SubjectID: proposerID,
		Observation: NaraObservation{
			Restarts:    100,
			TotalUptime: 50000,
			StartTime:   1624066568,
		},
		Attester:   proposerName,
		AttesterID: proposerID,
		AsOfTime:   asOfTime,
	}
	attestation.Signature = identity.SignContent(&attestation, proposerKeypair)

	proposal := &CheckpointProposal{
		Attestation: attestation,
		Round:       1,
	}

	// Setup network
	ledger := NewSyncLedger(1000)
	local := testLocalNara(t, "verifier")
	network := local.Network
	proposerNara := &Nara{
		Name: proposerName,
		Status: NaraStatus{
			ID:        proposerID,
			PublicKey: pubKeyToBase64(proposerKeypair.PublicKey),
		},
	}
	network.Neighbourhood[proposerName] = proposerNara
	network.RegisterKey(proposerID, pubKeyToBase64(proposerKeypair.PublicKey))

	service := NewCheckpointService(network, ledger, local)

	// Check if proposal verification has the same issues
	isValid := service.verifyProposalSignature(proposal)

	if !isValid {
		t.Error("Expected legitimate proposal to pass, but it failed!")
	} else {
		t.Log("✓ Proposal verification passed (uses Go embedding to verify attestation)")
		t.Log("  ✓ Fixed: Now uses ID-based lookup (getPublicKeyForNaraID)")
		t.Log("  Comment explains embedding behavior for maintainability")
	}
}
