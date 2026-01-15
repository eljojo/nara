package nara

import "fmt"

// Attestation represents a signed claim about a nara's state
// "I (Attester) claim that Subject has this state"
//
// Attestations are the foundation of checkpoint consensus:
// - Self-attestation: Attester == Subject (proposal - "I claim this about myself")
// - Third-party attestation: Attester != Subject (vote - "I claim this about someone else")
//
// The signature cryptographically binds the Attester to the claimed state,
// making attestations verifiable and unforgeable.
type Attestation struct {
	// Version for backwards compatibility
	Version int `json:"version"` // Attestation format version (default: 1)

	// About whom
	Subject   string `json:"subject"`    // Nara name being attested about
	SubjectID string `json:"subject_id"` // Nara ID (public key hash)

	// What state is being claimed
	Observation NaraObservation `json:"observation"`

	// Who is making the claim
	Attester   string `json:"attester"`    // Nara name making the attestation
	AttesterID string `json:"attester_id"` // Nara ID (public key hash)

	// Reference point (v2+) - what checkpoint attester had seen when making this attestation
	LastSeenCheckpointID string `json:"last_seen_checkpoint_id,omitempty"` // ID of last checkpoint attester has seen for subject (empty for v1)

	// When & Proof
	AsOfTime  int64  `json:"as_of_time"` // Unix timestamp of the checkpoint snapshot (all attesters sign the same time)
	Signature string `json:"signature"`  // Ed25519 signature of the attestation
}

// IsSelfAttestation returns true if this is a self-attestation (attester == subject)
func (a *Attestation) IsSelfAttestation() bool {
	return a.AttesterID == a.SubjectID
}

// SignableContent returns the canonical string representation for signing/verification
// Version-aware: v1 uses original format, v2 includes reference point (last seen checkpoint)
// v1 format: "attestation:v1:{attester_id}:{subject_id}:{as_of_time}:{restarts}:{total_uptime}:{first_seen}"
// v2 format: "attestation:v2:{attester_id}:{subject_id}:{as_of_time}:{restarts}:{total_uptime}:{first_seen}:{last_seen_checkpoint_id}"
func (a *Attestation) SignableContent() string {
	version := a.Version
	if version == 0 {
		version = 1 // Default to v1 for backwards compatibility
	}

	// v1 format (unchanged for backwards compatibility)
	if version == 1 {
		return fmt.Sprintf("attestation:v%d:%s:%s:%d:%d:%d:%d",
			version,
			a.AttesterID,
			a.SubjectID,
			a.AsOfTime,
			a.Observation.Restarts,
			a.Observation.TotalUptime,
			a.Observation.StartTime)
	}

	// v2 format: includes last seen checkpoint ID for consensus on reference point
	return fmt.Sprintf("attestation:v%d:%s:%s:%d:%d:%d:%d:%s",
		version,
		a.AttesterID,
		a.SubjectID,
		a.AsOfTime,
		a.Observation.Restarts,
		a.Observation.TotalUptime,
		a.Observation.StartTime,
		a.LastSeenCheckpointID)
}
