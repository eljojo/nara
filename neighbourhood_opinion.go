package nara

import (
	"context"
	"sync"

	"github.com/sirupsen/logrus"
)

// neighbourhood_opinion.go
// Extracted from projection_opinion.go
// Contains opinion consensus projection and derivation logic

// ObservationRecord stores observation event data for consensus calculation.
type ObservationRecord struct {
	Observer       string // Who made the observation
	Subject        string // Who is being observed
	Type           string // "restart" or "first-seen"
	StartTime      int64
	RestartNum     int64
	LastRestart    int64
	ObserverUptime uint64
}

// OpinionConsensusProjection tracks restart and first-seen events for consensus derivation.
type OpinionConsensusProjection struct {
	// Observations indexed by subject
	observationsBySubject map[string][]ObservationRecord
	ledger                *SyncLedger
	projection            *Projection
	mu                    sync.RWMutex
}

// NewOpinionConsensusProjection creates a new opinion consensus projection.
func NewOpinionConsensusProjection(ledger *SyncLedger) *OpinionConsensusProjection {
	p := &OpinionConsensusProjection{
		observationsBySubject: make(map[string][]ObservationRecord),
		ledger:                ledger,
	}

	p.projection = NewProjection(ledger, p.handleEvent)

	// Register reset handler to clear state when ledger is restructured
	p.projection.SetOnReset(func() {
		p.mu.Lock()
		p.observationsBySubject = make(map[string][]ObservationRecord)
		p.mu.Unlock()
	})

	return p
}

// handleEvent processes a single event and stores it if it's a relevant observation.
func (p *OpinionConsensusProjection) handleEvent(event SyncEvent) error {
	// Only process observation events
	if event.Service != ServiceObservation || event.Observation == nil {
		return nil
	}

	obs := event.Observation

	// Only track restart and first-seen for consensus
	if obs.Type != "restart" && obs.Type != "first-seen" {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Use observer's uptime for weighting (default to 1 if not set)
	uptime := obs.ObserverUptime
	if uptime == 0 {
		uptime = 1
	}

	record := ObservationRecord{
		Observer:       obs.Observer,
		Subject:        obs.Subject,
		Type:           obs.Type,
		StartTime:      obs.StartTime,
		RestartNum:     obs.RestartNum,
		LastRestart:    obs.LastRestart,
		ObserverUptime: uptime,
	}

	p.observationsBySubject[obs.Subject] = append(p.observationsBySubject[obs.Subject], record)

	return nil
}

// DeriveOpinion returns consensus opinion about a subject.
// Uses the same uptime-weighted clustering algorithm as the legacy implementation.
// Note: Call Trigger() or RunOnce() before this if you need up-to-date data.
func (p *OpinionConsensusProjection) DeriveOpinion(subject string) OpinionData {
	p.mu.RLock()
	observations := p.observationsBySubject[subject]
	// Make a copy to avoid holding lock during computation
	obsCopy := make([]ObservationRecord, len(observations))
	copy(obsCopy, observations)
	p.mu.RUnlock()

	if len(obsCopy) == 0 {
		return OpinionData{} // No consensus data available
	}

	const tolerance int64 = 60 // seconds - handles clock drift

	// Derive StartTime using clustering
	startTime := deriveProjectionStartTimeConsensus(obsCopy, tolerance)

	// Derive Restarts (use highest restart count)
	restarts := deriveProjectionRestartsConsensus(obsCopy, subject)

	// Derive LastRestart (most recent)
	lastRestart := deriveProjectionLastRestartConsensus(obsCopy)

	// Derive TotalUptime from ledger (uses status-change events)
	totalUptime := p.ledger.DeriveTotalUptime(subject)

	return OpinionData{
		StartTime:   startTime,
		Restarts:    restarts,
		LastRestart: lastRestart,
		TotalUptime: totalUptime,
	}
}

// deriveProjectionStartTimeConsensus uses trimmed mean for startTime consensus.
func deriveProjectionStartTimeConsensus(observations []ObservationRecord, tolerance int64) int64 {
	if len(observations) == 0 {
		return 0
	}

	subject := observations[0].Subject // All observations are for same subject
	var values []int64
	maxStartTime := int64(0)

	for _, obs := range observations {
		if obs.StartTime > 0 {
			values = append(values, obs.StartTime)
			if obs.StartTime > maxStartTime {
				maxStartTime = obs.StartTime
			}
		}
	}

	if len(values) == 0 {
		return 0
	}

	trimmed := trimmedMeanConsensus(values)

	// Only log significant differences (>1 hour)
	if trimmed != maxStartTime && trimmed > 0 {
		diff := maxStartTime - trimmed
		if diff > 3600 {
			logrus.Debugf("starttime consensus for %s: max=%d, trimmed=%d (diff=%ds)",
				subject, maxStartTime, trimmed, diff)
		}
	}

	return trimmed
}

// deriveProjectionRestartsConsensus returns the restart count using trimmed mean consensus.
// Filters out outliers and averages the remaining observations for a robust consensus.
func deriveProjectionRestartsConsensus(observations []ObservationRecord, subject string) int64 {
	maxRestarts := int64(0)
	uniqueObservers := make(map[string]bool)
	var values []int64

	for _, obs := range observations {
		if obs.RestartNum > maxRestarts {
			maxRestarts = obs.RestartNum
		}
		// Track unique observers and collect non-zero values
		if obs.RestartNum > 0 && obs.Type == "restart" {
			uniqueObservers[obs.Observer] = true
			values = append(values, obs.RestartNum)
		}
	}

	// If only one observer is reporting restarts, use max (most recent from that observer)
	if len(uniqueObservers) <= 1 {
		return maxRestarts
	}

	// Use trimmed mean: remove outliers and average
	trimmedRestarts := trimmedMeanConsensus(values)

	// Fallback to max if trimmed mean returns 0
	if trimmedRestarts == 0 {
		return maxRestarts
	}

	// Only warn about very significant differences (>20% and >10 absolute)
	diff := maxRestarts - trimmedRestarts
	if diff > 10 {
		percentDiff := float64(diff) / float64(maxRestarts) * 100
		if percentDiff >= 20.0 {
			logrus.Warnf("🔍 restarts consensus diff for %s: max=%d, trimmed=%d (diff=%d, %.1f%%)",
				subject, maxRestarts, trimmedRestarts, diff, percentDiff)
		}
	}

	return trimmedRestarts
}

// deriveProjectionLastRestartConsensus returns the most recent LastRestart timestamp.
func deriveProjectionLastRestartConsensus(observations []ObservationRecord) int64 {
	maxLastRestart := int64(0)
	for _, obs := range observations {
		if obs.LastRestart > maxLastRestart {
			maxLastRestart = obs.LastRestart
		}
	}
	return maxLastRestart
}

// trimmedMeanConsensus calculates a robust average by removing outliers.
// Ignores zeros, calculates median, removes values >5x or <0.2x median, returns average.
// This is a wrapper around TrimmedMeanPositive for backward compatibility.
func trimmedMeanConsensus(values []int64) int64 {
	return TrimmedMeanPositive(values)
}

// GetObservationsFor returns the observation records for a subject.
func (p *OpinionConsensusProjection) GetObservationsFor(subject string) []ObservationRecord {
	p.mu.RLock()
	defer p.mu.RUnlock()
	observations := p.observationsBySubject[subject]
	result := make([]ObservationRecord, len(observations))
	copy(result, observations)
	return result
}

// RunToEnd catches up the projection to the current state.
func (p *OpinionConsensusProjection) RunToEnd(ctx context.Context) error {
	return p.projection.RunToEnd(ctx)
}

// RunOnce processes any new events since last run.
func (p *OpinionConsensusProjection) RunOnce() (bool, error) {
	return p.projection.RunOnce()
}

// Trigger processes new events immediately.
// Use this when events have been added and you need updated state.
func (p *OpinionConsensusProjection) Trigger() {
	p.projection.RunOnce()
}

// SubjectCount returns the number of subjects with observations.
func (p *OpinionConsensusProjection) SubjectCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.observationsBySubject)
}

// Reset clears all state and resets the projection.
func (p *OpinionConsensusProjection) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.observationsBySubject = make(map[string][]ObservationRecord)
	p.projection.Reset()
}

// DeriveOpinionFromCheckpoint derives opinion using checkpoints as baseline.
// This is the "new way" that uses checkpoint.Restarts + new observations after checkpoint.
// Returns the opinion and whether a checkpoint was used.
func (p *OpinionConsensusProjection) DeriveOpinionFromCheckpoint(subject string) (OpinionData, bool) {
	// Get checkpoint from ledger (if any)
	checkpoint := p.ledger.GetCheckpoint(subject)
	if checkpoint == nil {
		// No checkpoint - fall back to observation-only method
		return p.DeriveOpinion(subject), false
	}

	p.mu.RLock()
	observations := p.observationsBySubject[subject]
	obsCopy := make([]ObservationRecord, len(observations))
	copy(obsCopy, observations)
	p.mu.RUnlock()

	// Filter to only observations AFTER the checkpoint
	var postCheckpointObs []ObservationRecord
	for _, obs := range obsCopy {
		// For restart events, check if StartTime is after checkpoint
		if obs.Type == "restart" && obs.StartTime > checkpoint.AsOfTime {
			postCheckpointObs = append(postCheckpointObs, obs)
		}
		// For first-seen, we use the checkpoint's StartTime as baseline
	}

	// Use checkpoint as baseline for restarts
	restarts := checkpoint.Observation.Restarts

	// Add new restarts observed after checkpoint (count unique StartTimes)
	if len(postCheckpointObs) > 0 {
		uniqueStartTimes := make(map[int64]bool)
		for _, obs := range postCheckpointObs {
			if obs.StartTime > 0 {
				uniqueStartTimes[obs.StartTime] = true
			}
		}
		restarts += int64(len(uniqueStartTimes))
	}

	// StartTime: use checkpoint's value (it's the consensus at checkpoint time)
	startTime := checkpoint.Observation.StartTime

	// LastRestart: check if we have newer observations, otherwise use checkpoint-derived value
	lastRestart := int64(0)
	for _, obs := range postCheckpointObs {
		if obs.LastRestart > lastRestart {
			lastRestart = obs.LastRestart
		}
	}
	// If no post-checkpoint observations with LastRestart, we can't know from checkpoint alone
	// (checkpoint doesn't store LastRestart directly, but we could derive it from the most recent StartTime)
	if lastRestart == 0 && len(postCheckpointObs) > 0 {
		// Use latest StartTime from post-checkpoint observations as LastRestart
		for _, obs := range postCheckpointObs {
			if obs.StartTime > lastRestart {
				lastRestart = obs.StartTime
			}
		}
	}

	// TotalUptime: checkpoint baseline + uptime from status-change events after checkpoint
	totalUptime := checkpoint.Observation.TotalUptime
	totalUptime += p.ledger.DeriveUptimeAfter(subject, checkpoint.AsOfTime)

	return OpinionData{
		StartTime:   startTime,
		Restarts:    restarts,
		LastRestart: lastRestart,
		TotalUptime: totalUptime,
	}, true
}

// DeriveOpinionWithValidation derives opinion and compares observation-based vs checkpoint-based.
// Logs a warning if the two methods produce significantly different results.
// Returns the observation-based opinion (current source of truth).
func (p *OpinionConsensusProjection) DeriveOpinionWithValidation(subject string) OpinionData {
	// Current method: observation-based (source of truth)
	obsOpinion := p.DeriveOpinion(subject)

	// New method: checkpoint-based
	checkpointOpinion, usedCheckpoint := p.DeriveOpinionFromCheckpoint(subject)

	if !usedCheckpoint {
		// No checkpoint exists, nothing to compare
		return obsOpinion
	}

	// Compare the two methods
	const restartTolerance int64 = 5      // Allow 5 restart difference
	const startTimeTolerance int64 = 3600 // Allow 1 hour difference
	const uptimeTolerance int64 = 300     // Allow 5 minutes uptime difference

	restartDiff := obsOpinion.Restarts - checkpointOpinion.Restarts
	if restartDiff < 0 {
		restartDiff = -restartDiff
	}

	startTimeDiff := obsOpinion.StartTime - checkpointOpinion.StartTime
	if startTimeDiff < 0 {
		startTimeDiff = -startTimeDiff
	}

	uptimeDiff := obsOpinion.TotalUptime - checkpointOpinion.TotalUptime
	if uptimeDiff < 0 {
		uptimeDiff = -uptimeDiff
	}

	if restartDiff > restartTolerance || startTimeDiff > startTimeTolerance || uptimeDiff > uptimeTolerance {
		logrus.Warnf("⚠️ Opinion method divergence for %s: "+
			"obs={restarts:%d, start:%d, uptime:%d} vs checkpoint={restarts:%d, start:%d, uptime:%d} "+
			"(restart_diff=%d, start_diff=%ds, uptime_diff=%ds)",
			subject,
			obsOpinion.Restarts, obsOpinion.StartTime, obsOpinion.TotalUptime,
			checkpointOpinion.Restarts, checkpointOpinion.StartTime, checkpointOpinion.TotalUptime,
			restartDiff, startTimeDiff, uptimeDiff)
	}

	// Return observation-based opinion (current source of truth)
	return obsOpinion
}
