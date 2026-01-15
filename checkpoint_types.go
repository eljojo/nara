package nara

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"sort"
	"time"
)

// MinCheckpointSignatures is the minimum number of valid signatures required
// for a checkpoint to be considered verified.
const MinCheckpointSignatures = 2

// CheckpointEventPayload records a historical state snapshot for a nara
// Used to preserve historical data (restart counts, uptime) that predates
// the event-based tracking system. Multiple high-uptime naras can attest
// to the checkpoint data, making it a trusted anchor for historical state.
//
// This solves the "historians' note" problem: old data was tracked differently,
// we snapshot what we knew then, and track properly going forward.
type CheckpointEventPayload struct {
	// Version for backwards compatibility
	Version int `json:"version"` // Checkpoint format version (default: 1)

	// Identity (who this checkpoint is about)
	Subject   string `json:"subject"`    // Nara name
	SubjectID NaraID `json:"subject_id"` // Nara ID (for indexing)

	// Chain of trust (v2+)
	PreviousCheckpointID string `json:"previous_checkpoint_id,omitempty"` // ID of previous checkpoint for this subject (empty for first checkpoint or v1)

	// The agreed-upon state (embedded, pure data)
	Observation NaraObservation `json:"observation"`

	// Checkpoint metadata
	AsOfTime int64 `json:"as_of_time"` // Unix timestamp (SECONDS) when snapshot was taken
	Round    int   `json:"round"`      // Consensus round (1 or 2) - needed for signature verification

	// Multi-party attestation - voters who participated in checkpoint creation
	VoterIDs   []NaraID `json:"voter_ids,omitempty"`  // Nara IDs who voted for these values
	Signatures []string `json:"signatures,omitempty"` // Base64 Ed25519 signatures (each verifies the values)
}

// Note: Importance is always Critical - no need to store it

// ContentString returns canonical string for hashing/signing
// Checkpoints are unique per (subject_id, as_of_time) pair
// Version-aware: v1 uses original format, v2 includes previous checkpoint ID
func (p *CheckpointEventPayload) ContentString() string {
	// Use SubjectID if available, fall back to Subject for backward compatibility
	id := p.SubjectID
	if id == "" {
		id = p.Subject
	}

	// v1 format (unchanged for backwards compatibility)
	if p.Version == 0 || p.Version == 1 {
		return fmt.Sprintf("checkpoint:%s:%d:%d:%d:%d",
			id, p.AsOfTime, p.Observation.StartTime, p.Observation.Restarts, p.Observation.TotalUptime)
	}

	// v2 format: includes previous checkpoint ID for chain of trust
	return fmt.Sprintf("checkpoint:v2:%s:%d:%d:%d:%d:%s",
		id, p.AsOfTime, p.Observation.StartTime, p.Observation.Restarts, p.Observation.TotalUptime, p.PreviousCheckpointID)
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
	if p.Observation.Restarts < 0 {
		return false
	}
	if p.Observation.TotalUptime < 0 {
		return false
	}
	// VoterIDs and Signatures must match in length if present
	if len(p.VoterIDs) != len(p.Signatures) {
		return false
	}
	return true
}

// GetActor implements Payload
// Returns empty string because checkpoint voters are identified by IDs, not names.
// VoterIDs contain nara IDs (public key hashes), not names, so we cannot use them
// for name-based operations like discovery or logging.
func (p *CheckpointEventPayload) GetActor() string {
	return "" // Don't return VoterIDs - they're IDs, not names
}

// GetTarget implements Payload (Subject is the target)
func (p *CheckpointEventPayload) GetTarget() string { return p.Subject }

// CheckpointVerificationResult contains detailed verification information
type CheckpointVerificationResult struct {
	Valid      bool // Whether verification passed (validCount >= MinCheckpointSignatures)
	ValidCount int  // Number of signatures that verified successfully
	KnownCount int  // Number of voters whose public keys we could look up
	TotalCount int  // Total number of voters/signatures
}

// VerifySignatureWithCounts verifies checkpoint signatures and returns detailed counts.
// This is useful for debugging and inspector UIs.
func (p *CheckpointEventPayload) VerifySignatureWithCounts(lookup PublicKeyLookup) CheckpointVerificationResult {
	result := CheckpointVerificationResult{
		TotalCount: len(p.VoterIDs),
	}

	if lookup == nil || len(p.VoterIDs) != len(p.Signatures) {
		return result
	}

	for i, voterID := range p.VoterIDs {
		pubKey := lookup(voterID, "")
		if pubKey == nil {
			continue
		}
		result.KnownCount++

		// Build attestation for verification - signatures use attestation format
		version := p.Version
		if version == 0 {
			version = 1
		}
		attestation := Attestation{
			Version:     version,
			Subject:     p.Subject,
			SubjectID:   p.SubjectID,
			Observation: p.Observation,
			AttesterID:  voterID,
			AsOfTime:    p.AsOfTime,
		}

		// For v2, include the previous checkpoint ID as the reference point
		if version >= 2 {
			attestation.LastSeenCheckpointID = p.PreviousCheckpointID
		}

		signableContent := attestation.SignableContent()
		if VerifySignatureBase64(pubKey, []byte(signableContent), p.Signatures[i]) {
			result.ValidCount++
		}
	}

	result.Valid = result.ValidCount >= MinCheckpointSignatures
	return result
}

// VerifySignature implements Payload for checkpoint multi-sig verification.
// Requires at least MinCheckpointSignatures valid voter signatures.
func (p *CheckpointEventPayload) VerifySignature(event *SyncEvent, lookup PublicKeyLookup) bool {
	return p.VerifySignatureWithCounts(lookup).Valid
}

// LogFormat returns technical log description
func (p *CheckpointEventPayload) LogFormat() string {
	return fmt.Sprintf("checkpoint: %s as-of %d (restarts: %d, uptime: %ds, voters: %d)",
		p.Subject, p.AsOfTime, p.Observation.Restarts, p.Observation.TotalUptime, len(p.VoterIDs))
}

// ToLogEvent returns a structured log event for checkpoint creation
func (p *CheckpointEventPayload) ToLogEvent() *LogEvent {
	subject := p.Subject
	restarts := p.Observation.Restarts
	return &LogEvent{
		Category: CategoryPresence,
		Type:     "checkpoint",
		Actor:    p.GetActor(), // Returns "" since VoterIDs are IDs not names
		Target:   p.Subject,
		Detail:   fmt.Sprintf("restarts: %d", p.Observation.Restarts),
		GroupFormat: func(actors string) string {
			// Note: actors will usually be empty since GetActor() returns ""
			// but provide format anyway for consistency
			if actors == "" {
				return fmt.Sprintf("📸 checkpoint for %s (restarts: %d)", subject, restarts)
			}
			return fmt.Sprintf("📸 %s created checkpoint for %s (restarts: %d)", actors, subject, restarts)
		},
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
			Subject:   subject,
			SubjectID: "", // Will be populated by caller if available
			AsOfTime:  asOfTime,
			Observation: NaraObservation{
				Restarts:    restarts,
				TotalUptime: totalUptime,
				StartTime:   firstSeen,
			},
			VoterIDs:   []NaraID{},
			Signatures: []string{},
		},
	}
	e.ComputeID()
	return e
}

// NewTestCheckpointEvent creates a checkpoint event for tests, ensuring the AsOfTime
// is after the checkpoint cutoff so it won't be filtered during ingestion.
// Preserves relative ordering of timestamps by adding an offset when needed.
// Use this in tests that need checkpoints to exist in the ledger.
// Use NewCheckpointEvent() for tests that need precise timestamp control.
func NewTestCheckpointEvent(subject string, asOfTime, firstSeen, restarts, totalUptime int64) SyncEvent {
	// Ensure test checkpoints aren't filtered by the cutoff time
	// Preserve relative ordering by adding the same offset to all old timestamps
	if asOfTime <= CheckpointCutoffTime {
		offset := CheckpointCutoffTime + 1000
		asOfTime = offset + asOfTime // Preserve relative differences
	}
	return NewCheckpointEvent(subject, asOfTime, firstSeen, restarts, totalUptime)
}

// AddCheckpointVoter adds a voter's signature to a checkpoint event
// Multiple naras can vote on the same checkpoint data,
// making it a trusted anchor for historical state.
// voterID is the nara's unique ID (not name).
func (e *SyncEvent) AddCheckpointVoter(voterID NaraID, keypair NaraKeypair) {
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
func (e *SyncEvent) VerifyCheckpointSignatures(publicKeys map[NaraID]string) int {
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
	event := l.GetCheckpointEvent(subject)
	if event != nil {
		return event.Checkpoint
	}
	return nil
}

// GetCheckpointEvent returns the full SyncEvent for the most recent checkpoint, or nil if none exists
func (l *SyncLedger) GetCheckpointEvent(subject string) *SyncEvent {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var latest *SyncEvent
	var latestAsOfTime int64 = 0

	for i := range l.Events {
		e := &l.Events[i]
		if e.Service == ServiceCheckpoint && e.Checkpoint != nil && e.Checkpoint.Subject == subject {
			if e.Checkpoint.AsOfTime > latestAsOfTime {
				latest = e
				latestAsOfTime = e.Checkpoint.AsOfTime
			}
		}
	}
	return latest
}

// DeriveRestartCount derives the total restart count for a subject from checkpoints + events
// Priority order:
//  1. If checkpoint exists: checkpoint.Restarts + count(unique StartTimes after checkpoint)
//  2. If backfill exists: backfill.RestartNum + count(unique StartTimes after backfill timestamp)
//  3. Otherwise: count(unique StartTimes from all restart events)
func (l *SyncLedger) DeriveRestartCount(subject string) int64 {
	l.mu.RLock()
	defer l.mu.RUnlock()

	// Check for checkpoint first (highest priority)
	var checkpoint *CheckpointEventPayload
	var checkpointAsOfTime int64 = 0
	for _, e := range l.Events {
		if e.Service == ServiceCheckpoint && e.Checkpoint != nil && e.Checkpoint.Subject == subject {
			if e.Checkpoint.AsOfTime > checkpointAsOfTime {
				checkpoint = e.Checkpoint
				checkpointAsOfTime = e.Checkpoint.AsOfTime
			}
		}
	}

	if checkpoint != nil {
		// Count unique start times after checkpoint
		newRestarts := l.countUniqueStartTimesAfterLocked(subject, checkpoint.AsOfTime)
		return checkpoint.Observation.Restarts + newRestarts
	}

	// Check for backfill event (second priority)
	var backfill *ObservationEventPayload
	for _, e := range l.Events {
		if e.Service == ServiceObservation && e.Observation != nil &&
			e.Observation.Subject == subject && e.Observation.IsBackfill {
			backfill = e.Observation
			break
		}
	}

	if backfill != nil {
		// Count unique start times that are DIFFERENT from the backfill's StartTime
		// New restarts are identified by having a different StartTime than what backfill captured
		newRestarts := l.countUniqueStartTimesExcludingLocked(subject, backfill.StartTime)
		return backfill.RestartNum + newRestarts
	}

	// No checkpoint or backfill - just count all unique start times
	return l.countUniqueStartTimesAfterLocked(subject, 0)
}

// countUniqueStartTimesAfterLocked counts unique StartTime values for restart events after a given time
// Caller must hold the read lock
func (l *SyncLedger) countUniqueStartTimesAfterLocked(subject string, afterTime int64) int64 {
	startTimes := make(map[int64]bool)

	for _, e := range l.Events {
		if e.Service != ServiceObservation || e.Observation == nil {
			continue
		}
		if e.Observation.Subject != subject {
			continue
		}
		if e.Observation.Type != "restart" {
			continue
		}
		// Skip backfill events when counting new restarts
		if e.Observation.IsBackfill {
			continue
		}
		// Only count restart events whose StartTime is after the cutoff
		// This means the restart happened AFTER the checkpoint/reference point
		if afterTime > 0 && e.Observation.StartTime <= afterTime {
			continue
		}
		startTimes[e.Observation.StartTime] = true
	}

	return int64(len(startTimes))
}

// countUniqueStartTimesExcludingLocked counts unique StartTime values excluding a specific one
// Used for backfill: count all unique start times except the one the backfill already captured
// Caller must hold the read lock
func (l *SyncLedger) countUniqueStartTimesExcludingLocked(subject string, excludeStartTime int64) int64 {
	startTimes := make(map[int64]bool)

	for _, e := range l.Events {
		if e.Service != ServiceObservation || e.Observation == nil {
			continue
		}
		if e.Observation.Subject != subject {
			continue
		}
		if e.Observation.Type != "restart" {
			continue
		}
		// Skip backfill events when counting new restarts
		if e.Observation.IsBackfill {
			continue
		}
		// Skip the specific StartTime that the backfill already counted
		if e.Observation.StartTime == excludeStartTime {
			continue
		}
		startTimes[e.Observation.StartTime] = true
	}

	return int64(len(startTimes))
}

// DeriveTotalUptime derives the total uptime for a subject in seconds
// Priority:
//  1. Checkpoint: checkpoint.TotalUptime + uptime from status events after checkpoint
//  2. Backfill: assume online since StartTime, adjusted by status-change events
//  3. No baseline: just sum up online periods from status-change events
//
// For backfill events, we assume the nara has been online since their StartTime
// (first seen) unless status-change events indicate otherwise.
func (l *SyncLedger) DeriveTotalUptime(subject string) int64 {
	l.mu.RLock()
	defer l.mu.RUnlock()

	// Check for checkpoint first (highest priority)
	var checkpoint *CheckpointEventPayload
	var checkpointAsOfTime int64 = 0
	for _, e := range l.Events {
		if e.Service == ServiceCheckpoint && e.Checkpoint != nil && e.Checkpoint.Subject == subject {
			if e.Checkpoint.AsOfTime > checkpointAsOfTime {
				checkpoint = e.Checkpoint
				checkpointAsOfTime = e.Checkpoint.AsOfTime
			}
		}
	}

	baseUptime := int64(0)
	afterTime := int64(0)
	assumeOnlineSince := int64(0) // For backfill: assume online since this time

	if checkpoint != nil {
		baseUptime = checkpoint.Observation.TotalUptime
		afterTime = checkpoint.AsOfTime
	} else {
		// No checkpoint - check for backfill event
		for _, e := range l.Events {
			if e.Service == ServiceObservation && e.Observation != nil &&
				e.Observation.Subject == subject && e.Observation.IsBackfill {
				// For backfill: assume online since StartTime
				assumeOnlineSince = e.Observation.StartTime
				break
			}
		}
	}

	// Collect status change events after checkpoint/backfill time
	type statusEvent struct {
		timestamp int64
		state     string
	}
	var statusEvents []statusEvent

	for _, e := range l.Events {
		if e.Service != ServiceObservation || e.Observation == nil {
			continue
		}
		if e.Observation.Subject != subject {
			continue
		}
		if e.Observation.Type != "status-change" {
			continue
		}
		eventTimeSec := e.Timestamp / 1e9
		if afterTime > 0 && eventTimeSec <= afterTime {
			continue
		}
		statusEvents = append(statusEvents, statusEvent{
			timestamp: eventTimeSec,
			state:     e.Observation.OnlineState,
		})
	}

	// Sort by timestamp
	sort.Slice(statusEvents, func(i, j int) bool {
		return statusEvents[i].timestamp < statusEvents[j].timestamp
	})

	// For backfill without checkpoint: assume online since StartTime
	// until first OFFLINE event or until now
	var onlineStart int64 = 0
	if assumeOnlineSince > 0 && len(statusEvents) == 0 {
		// No status events - assume online since StartTime until now
		baseUptime = time.Now().Unix() - assumeOnlineSince
		return baseUptime
	} else if assumeOnlineSince > 0 {
		// Have status events - start counting from assumeOnlineSince
		onlineStart = assumeOnlineSince
	}

	// Calculate online periods from status events
	for _, se := range statusEvents {
		if se.state == "ONLINE" && onlineStart == 0 {
			onlineStart = se.timestamp
		} else if (se.state == "OFFLINE" || se.state == "MISSING") && onlineStart > 0 {
			baseUptime += se.timestamp - onlineStart
			onlineStart = 0
		}
	}

	// If still online, count up to now
	if onlineStart > 0 {
		baseUptime += time.Now().Unix() - onlineStart
	}

	return baseUptime
}

// DeriveUptimeAfter calculates uptime from status-change events after a given timestamp.
// This is used for checkpoint-based uptime derivation where we need to add
// new uptime on top of a checkpoint baseline.
func (l *SyncLedger) DeriveUptimeAfter(subject string, afterTime int64) int64 {
	l.mu.RLock()
	defer l.mu.RUnlock()

	// Collect status change events after the given time
	type statusEvent struct {
		timestamp int64
		state     string
	}
	var statusEvents []statusEvent

	for _, e := range l.Events {
		if e.Service != ServiceObservation || e.Observation == nil {
			continue
		}
		if e.Observation.Subject != subject {
			continue
		}
		if e.Observation.Type != "status-change" {
			continue
		}
		eventTimeSec := e.Timestamp / 1e9
		if afterTime > 0 && eventTimeSec <= afterTime {
			continue
		}
		statusEvents = append(statusEvents, statusEvent{
			timestamp: eventTimeSec,
			state:     e.Observation.OnlineState,
		})
	}

	// Sort by timestamp
	sort.Slice(statusEvents, func(i, j int) bool {
		return statusEvents[i].timestamp < statusEvents[j].timestamp
	})

	// Calculate online periods
	var uptime int64 = 0
	var onlineStart int64 = 0
	for _, se := range statusEvents {
		if se.state == "ONLINE" && onlineStart == 0 {
			onlineStart = se.timestamp
		} else if (se.state == "OFFLINE" || se.state == "MISSING") && onlineStart > 0 {
			uptime += se.timestamp - onlineStart
			onlineStart = 0
		}
	}

	// If still online, count up to now
	if onlineStart > 0 {
		uptime += time.Now().Unix() - onlineStart
	}

	return uptime
}

// GetFirstSeenFromEvents returns the earliest known StartTime for a subject from events
func (l *SyncLedger) GetFirstSeenFromEvents(subject string) int64 {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var earliest int64 = 0

	for _, e := range l.Events {
		// Check observation events (including backfill)
		if e.Service == ServiceObservation && e.Observation != nil {
			if e.Observation.Subject == subject && e.Observation.StartTime > 0 {
				if earliest == 0 || e.Observation.StartTime < earliest {
					earliest = e.Observation.StartTime
				}
			}
		}

		// Check existing checkpoints
		if e.Service == ServiceCheckpoint && e.Checkpoint != nil {
			if e.Checkpoint.Subject == subject && e.Checkpoint.Observation.StartTime > 0 {
				if earliest == 0 || e.Checkpoint.Observation.StartTime < earliest {
					earliest = e.Checkpoint.Observation.StartTime
				}
			}
		}
	}

	return earliest
}
