package nara

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"
)

// CheckpointEventPayload records a historical state snapshot for a nara
// Used to preserve historical data (restart counts, uptime) that predates
// the event-based tracking system. Multiple high-uptime naras can attest
// to the checkpoint data, making it a trusted anchor for historical state.
//
// This solves the "historians' note" problem: old data was tracked differently,
// we snapshot what we knew then, and track properly going forward.
type CheckpointEventPayload struct {
	Subject     string `json:"subject"`      // Who this checkpoint is about (name)
	SubjectID   string `json:"subject_id"`   // Nara ID (for indexing)
	AsOfTime    int64  `json:"as_of_time"`   // Unix timestamp (SECONDS) when snapshot was taken
	FirstSeen   int64  `json:"first_seen"`   // Unix timestamp (SECONDS) when network first saw this nara
	Restarts    int64  `json:"restarts"`     // Historical restart count at checkpoint time
	TotalUptime int64  `json:"total_uptime"` // Total verified online seconds at checkpoint time
	Importance  int    `json:"importance"`   // Always Critical (3) - never pruned

	// Community consensus - voters who participated in checkpoint creation
	VoterIDs   []string `json:"voter_ids,omitempty"`  // Nara IDs who voted for these values
	Signatures []string `json:"signatures,omitempty"` // Base64 Ed25519 signatures (each verifies the values)
}

// ContentString returns canonical string for hashing/signing
// Checkpoints are unique per (subject_id, as_of_time) pair
func (p *CheckpointEventPayload) ContentString() string {
	// Use SubjectID if available, fall back to Subject for backward compatibility
	id := p.SubjectID
	if id == "" {
		id = p.Subject
	}
	return fmt.Sprintf("checkpoint:%s:%d:%d:%d:%d",
		id, p.AsOfTime, p.FirstSeen, p.Restarts, p.TotalUptime)
}

// SignableContent returns the canonical string for signature verification
// All voters sign the same content, making signatures verifiable
func (p *CheckpointEventPayload) SignableContent() string {
	return p.ContentString()
}

// IsValid checks if the checkpoint payload is well-formed
func (p *CheckpointEventPayload) IsValid() bool {
	if p.Subject == "" {
		return false
	}
	if p.AsOfTime <= 0 {
		return false
	}
	if p.Restarts < 0 {
		return false
	}
	if p.TotalUptime < 0 {
		return false
	}
	// VoterIDs and Signatures must match in length if present
	if len(p.VoterIDs) != len(p.Signatures) {
		return false
	}
	return true
}

// GetActor implements Payload (first voter is the primary actor)
func (p *CheckpointEventPayload) GetActor() string {
	if len(p.VoterIDs) > 0 {
		return p.VoterIDs[0]
	}
	return ""
}

// GetTarget implements Payload (Subject is the target)
func (p *CheckpointEventPayload) GetTarget() string { return p.Subject }

// LogFormat returns technical log description
func (p *CheckpointEventPayload) LogFormat() string {
	return fmt.Sprintf("checkpoint: %s as-of %d (restarts: %d, uptime: %ds, voters: %d)",
		p.Subject, p.AsOfTime, p.Restarts, p.TotalUptime, len(p.VoterIDs))
}

// ToLogEvent returns a structured log event for checkpoint creation
func (p *CheckpointEventPayload) ToLogEvent() *LogEvent {
	return &LogEvent{
		Category: CategoryPresence,
		Type:     "checkpoint",
		Actor:    p.GetActor(),
		Target:   p.Subject,
		Detail:   fmt.Sprintf("📸 checkpoint for %s (restarts: %d)", p.Subject, p.Restarts),
	}
}

// signableData returns the canonical bytes for checkpoint attestation signing
func (p *CheckpointEventPayload) signableData() []byte {
	hasher := sha256.New()
	hasher.Write([]byte(p.ContentString()))
	return hasher.Sum(nil)
}

// NewCheckpointEvent creates a checkpoint event for snapshotting historical state
// This captures what the network knew about a nara at a specific point in time,
// allowing historical data to be preserved as we transition to event-based tracking.
//
// Parameters:
//   - subject: The nara this checkpoint is about
//   - asOfTime: Unix timestamp (seconds) when this snapshot was taken
//   - firstSeen: Unix timestamp (seconds) when the network first saw this nara
//   - restarts: Total restart count known at checkpoint time
//   - totalUptime: Total verified online seconds at checkpoint time
func NewCheckpointEvent(subject string, asOfTime, firstSeen, restarts, totalUptime int64) SyncEvent {
	e := SyncEvent{
		Timestamp: time.Now().UnixNano(),
		Service:   ServiceCheckpoint,
		Checkpoint: &CheckpointEventPayload{
			Subject:     subject,
			AsOfTime:    asOfTime,
			FirstSeen:   firstSeen,
			Restarts:    restarts,
			TotalUptime: totalUptime,
			Importance:  ImportanceCritical,
			VoterIDs:    []string{},
			Signatures:  []string{},
		},
	}
	e.ComputeID()
	return e
}

// AddCheckpointVoter adds a voter's signature to a checkpoint event
// Multiple naras can vote on the same checkpoint data,
// making it a trusted anchor for historical state.
// voterID is the nara's unique ID (not name).
func (e *SyncEvent) AddCheckpointVoter(voterID string, keypair NaraKeypair) {
	if e.Service != ServiceCheckpoint || e.Checkpoint == nil {
		return
	}

	// Sign the checkpoint content
	sig := keypair.SignBase64(e.Checkpoint.signableData())

	e.Checkpoint.VoterIDs = append(e.Checkpoint.VoterIDs, voterID)
	e.Checkpoint.Signatures = append(e.Checkpoint.Signatures, sig)

	// Recompute ID since content changed
	e.ComputeID()
}

// VerifyCheckpointSignatures verifies all signatures on a checkpoint event
// Returns the number of valid signatures found
// publicKeys maps voterID -> base64 public key string
func (e *SyncEvent) VerifyCheckpointSignatures(publicKeys map[string]string) int {
	if e.Service != ServiceCheckpoint || e.Checkpoint == nil {
		return 0
	}

	validCount := 0
	data := e.Checkpoint.signableData()

	for i, voterID := range e.Checkpoint.VoterIDs {
		if i >= len(e.Checkpoint.Signatures) {
			break
		}

		pubKeyStr, ok := publicKeys[voterID]
		if !ok {
			continue
		}

		pubKeyBytes, err := base64.StdEncoding.DecodeString(pubKeyStr)
		if err != nil || len(pubKeyBytes) != ed25519.PublicKeySize {
			continue
		}

		sigBytes, err := base64.StdEncoding.DecodeString(e.Checkpoint.Signatures[i])
		if err != nil {
			continue
		}

		if VerifySignature(ed25519.PublicKey(pubKeyBytes), data, sigBytes) {
			validCount++
		}
	}

	return validCount
}

// GetCheckpoint returns the most recent checkpoint event for a subject, or nil if none exists
func (l *SyncLedger) GetCheckpoint(subject string) *CheckpointEventPayload {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var latest *CheckpointEventPayload
	var latestAsOfTime int64 = 0

	for _, e := range l.Events {
		if e.Service == ServiceCheckpoint && e.Checkpoint != nil && e.Checkpoint.Subject == subject {
			if e.Checkpoint.AsOfTime > latestAsOfTime {
				latest = e.Checkpoint
				latestAsOfTime = e.Checkpoint.AsOfTime
			}
		}
	}
	return latest
}
