package nara

import (
	"fmt"
	"sort"
	"time"
)

// Checkpoint creation and coordination logic
//
// High-uptime naras can create checkpoints to snapshot historical state.
// This allows the network to maintain accurate restart counts and uptime
// even as individual events are pruned over time.
//
// Checkpoint creation flow:
// 1. High-uptime nara identifies a subject needing a checkpoint
// 2. Nara prepares a proposal from their local data
// 3. Nara requests signatures from other high-uptime naras via mesh HTTP
// 4. If enough signatures gathered, nara finalizes and broadcasts checkpoint

// DefaultMinCheckpointUptime is the minimum uptime (7 days) to participate in checkpointing
const DefaultMinCheckpointUptime = 7 * 24 * 60 * 60 // 7 days in seconds

// DefaultCheckpointTopPercentile defines the top % of naras by uptime eligible for checkpointing
const DefaultCheckpointTopPercentile = 0.20 // top 20%

// CheckpointSignatureRequest is sent to request a signature from another high-uptime nara
type CheckpointSignatureRequest struct {
	Proposal  *CheckpointEventPayload `json:"proposal"`
	Requester string                  `json:"requester"`
}

// CheckpointSignatureResponse is returned after evaluating a signature request
type CheckpointSignatureResponse struct {
	Attester  string `json:"attester"`
	Signature string `json:"signature,omitempty"`
	Approved  bool   `json:"approved"`
	Reason    string `json:"reason,omitempty"` // Set when declining
}

// IsHighUptime checks if a nara observation qualifies as high-uptime
// for checkpoint creation/attestation purposes.
//
// Criteria:
// - Must be ONLINE
// - Must have been running for at least minUptimeSeconds
func IsHighUptime(obs NaraObservation, minUptimeSeconds int64) bool {
	// Must be online
	if obs.Online != "ONLINE" {
		return false
	}

	// Must have valid start time
	if obs.StartTime <= 0 {
		return false
	}

	// Calculate current uptime
	currentUptime := time.Now().Unix() - obs.StartTime

	return currentUptime >= minUptimeSeconds
}

// GetHighUptimeNaras returns the names of naras in the top percentile by uptime.
// Only ONLINE naras are considered.
//
// Example: GetHighUptimeNaras(observations, 0.2) returns top 20%
func GetHighUptimeNaras(observations map[string]NaraObservation, topPercentile float64) []string {
	// Collect online naras with their uptime
	type naraUptime struct {
		name   string
		uptime int64
	}

	var naras []naraUptime
	now := time.Now().Unix()

	for name, obs := range observations {
		if obs.Online != "ONLINE" || obs.StartTime <= 0 {
			continue
		}
		uptime := now - obs.StartTime
		naras = append(naras, naraUptime{name: name, uptime: uptime})
	}

	if len(naras) == 0 {
		return nil
	}

	// Sort by uptime descending
	sort.Slice(naras, func(i, j int) bool {
		return naras[i].uptime > naras[j].uptime
	})

	// Calculate how many to return
	count := int(float64(len(naras)) * topPercentile)
	if count < 1 {
		count = 1 // At least return 1
	}
	if count > len(naras) {
		count = len(naras)
	}

	// Extract names
	result := make([]string, count)
	for i := 0; i < count; i++ {
		result[i] = naras[i].name
	}

	return result
}

// ShouldProposeCheckpoint determines if a checkpoint should be proposed for a subject.
//
// Conditions:
// - Subject has backfill data (historical knowledge exists)
// - No valid checkpoint exists yet for this subject
func ShouldProposeCheckpoint(ledger *SyncLedger, subject string) bool {
	// Check if we have backfill data (indicates historical knowledge)
	backfill := ledger.GetBackfillEvent(subject)
	if backfill == nil {
		// No historical data to checkpoint
		return false
	}

	// Check if a checkpoint already exists
	checkpoint := ledger.GetCheckpoint(subject)
	if checkpoint != nil {
		// Checkpoint already exists - don't propose another
		// In the future, we might want to propose updated checkpoints periodically
		return false
	}

	return true
}

// PrepareCheckpointProposal creates a checkpoint proposal from local ledger data.
// Returns nil if insufficient data exists.
func PrepareCheckpointProposal(ledger *SyncLedger, subject string) *CheckpointEventPayload {
	// Get backfill for first_seen
	backfill := ledger.GetBackfillEvent(subject)

	// Get derived values
	restarts := ledger.DeriveRestartCount(subject)
	totalUptime := ledger.DeriveTotalUptime(subject)

	// Determine first_seen time
	var firstSeen int64
	if backfill != nil {
		firstSeen = backfill.StartTime
	}

	// Must have some data
	if firstSeen == 0 && restarts == 0 {
		return nil
	}

	return &CheckpointEventPayload{
		Subject:     subject,
		AsOfTime:    time.Now().Unix(),
		FirstSeen:   firstSeen,
		Restarts:    restarts,
		TotalUptime: totalUptime,
		Importance:  ImportanceCritical,
		Attesters:   []string{},
		Signatures:  []string{},
	}
}

// SignCheckpointProposal signs a checkpoint proposal and returns the base64 signature.
// This is used when creating or attesting to a checkpoint.
func SignCheckpointProposal(proposal *CheckpointEventPayload, keypair NaraKeypair) string {
	data := proposal.signableData()
	return keypair.SignBase64(data)
}

// ValidateCheckpointProposal checks if a proposal matches our local data.
// Used before signing someone else's checkpoint proposal.
//
// Returns nil if proposal is acceptable, error describing mismatch otherwise.
func ValidateCheckpointProposal(ledger *SyncLedger, proposal *CheckpointEventPayload) error {
	// Basic validation
	if proposal == nil {
		return fmt.Errorf("proposal is nil")
	}
	if proposal.Subject == "" {
		return fmt.Errorf("missing subject")
	}
	if proposal.AsOfTime <= 0 {
		return fmt.Errorf("invalid as_of_time")
	}

	// Compare against our local data
	ourRestarts := ledger.DeriveRestartCount(proposal.Subject)

	// Allow some tolerance for restart count (network may have different views)
	// Accept if within ±5 restarts
	restartDiff := proposal.Restarts - ourRestarts
	if restartDiff < 0 {
		restartDiff = -restartDiff
	}
	if restartDiff > 5 {
		return fmt.Errorf("restart count mismatch: proposal has %d, we have %d", proposal.Restarts, ourRestarts)
	}

	// Check first_seen matches backfill (if we have one)
	backfill := ledger.GetBackfillEvent(proposal.Subject)
	if backfill != nil && proposal.FirstSeen != 0 {
		// Allow some tolerance for first_seen (60 seconds)
		firstSeenDiff := proposal.FirstSeen - backfill.StartTime
		if firstSeenDiff < 0 {
			firstSeenDiff = -firstSeenDiff
		}
		if firstSeenDiff > 60 {
			return fmt.Errorf("first_seen mismatch: proposal has %d, backfill has %d", proposal.FirstSeen, backfill.StartTime)
		}
	}

	return nil
}

// Note: signableData() is defined in sync.go

// GetSubjectsNeedingCheckpoint returns subjects that have backfill data but no checkpoint.
// Used to identify which naras need checkpoints during maintenance.
func (l *SyncLedger) GetSubjectsNeedingCheckpoint() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	// Find all subjects with backfill events
	backfillSubjects := make(map[string]bool)
	for _, e := range l.Events {
		if e.Service == ServiceObservation && e.Observation != nil && e.Observation.IsBackfill {
			backfillSubjects[e.Observation.Subject] = true
		}
	}

	// Find all subjects with checkpoints
	checkpointSubjects := make(map[string]bool)
	for _, e := range l.Events {
		if e.Service == ServiceCheckpoint && e.Checkpoint != nil {
			checkpointSubjects[e.Checkpoint.Subject] = true
		}
	}

	// Return subjects with backfill but no checkpoint
	var result []string
	for subject := range backfillSubjects {
		if !checkpointSubjects[subject] {
			result = append(result, subject)
		}
	}

	// Sort for deterministic ordering
	sort.Strings(result)
	return result
}

// GetFirstSeenFromEvents returns the earliest known StartTime for a subject from events.
// Used when preparing checkpoint proposals.
func (l *SyncLedger) GetFirstSeenFromEvents(subject string) int64 {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var earliest int64 = 0

	for _, e := range l.Events {
		// Check backfill events
		if e.Service == ServiceObservation && e.Observation != nil {
			if e.Observation.Subject == subject && e.Observation.StartTime > 0 {
				if earliest == 0 || e.Observation.StartTime < earliest {
					earliest = e.Observation.StartTime
				}
			}
		}

		// Check existing checkpoints
		if e.Service == ServiceCheckpoint && e.Checkpoint != nil {
			if e.Checkpoint.Subject == subject && e.Checkpoint.FirstSeen > 0 {
				if earliest == 0 || e.Checkpoint.FirstSeen < earliest {
					earliest = e.Checkpoint.FirstSeen
				}
			}
		}
	}

	return earliest
}
