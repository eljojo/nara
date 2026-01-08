package nara

import (
	"context"
	"sort"
	"sync"
)

// ObservationRecord stores observation event data for consensus calculation.
type ObservationRecord struct {
	Subject        string
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
func (p *OpinionConsensusProjection) DeriveOpinion(subject string) OpinionData {
	// First, catch up on any new events
	p.RunToEnd(context.Background())

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
	restarts := deriveProjectionRestartsConsensus(obsCopy)

	// Derive LastRestart (most recent)
	lastRestart := deriveProjectionLastRestartConsensus(obsCopy)

	return OpinionData{
		StartTime:   startTime,
		Restarts:    restarts,
		LastRestart: lastRestart,
	}
}

// projectionConsensusValue for clustering algorithm
type projectionConsensusValue struct {
	value  int64
	weight uint64
}

// projectionConsensusCluster groups values within tolerance
type projectionConsensusCluster struct {
	values      []int64
	totalWeight uint64
}

// deriveProjectionStartTimeConsensus uses uptime-weighted clustering for startTime.
func deriveProjectionStartTimeConsensus(observations []ObservationRecord, tolerance int64) int64 {
	var values []projectionConsensusValue
	for _, obs := range observations {
		if obs.StartTime > 0 {
			values = append(values, projectionConsensusValue{value: obs.StartTime, weight: obs.ObserverUptime})
		}
	}
	return projectionConsensusByUptime(values, tolerance)
}

// deriveProjectionRestartsConsensus returns the highest restart count seen.
func deriveProjectionRestartsConsensus(observations []ObservationRecord) int64 {
	maxRestarts := int64(0)
	for _, obs := range observations {
		if obs.RestartNum > maxRestarts {
			maxRestarts = obs.RestartNum
		}
	}
	return maxRestarts
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

// projectionConsensusByUptime implements uptime-weighted clustering for consensus.
// Uses a strategy hierarchy:
// 1. Prefer clusters with ≥2 observers (sorted by total weight)
// 2. Fall back to highest-weight single-observer cluster
func projectionConsensusByUptime(values []projectionConsensusValue, tolerance int64) int64 {
	if len(values) == 0 {
		return 0
	}

	// Sort by value
	sort.Slice(values, func(i, j int) bool {
		return values[i].value < values[j].value
	})

	// Build clusters
	var clusters []projectionConsensusCluster
	current := projectionConsensusCluster{
		values:      []int64{values[0].value},
		totalWeight: values[0].weight,
	}
	clusterStart := values[0].value

	for i := 1; i < len(values); i++ {
		if values[i].value-clusterStart <= tolerance {
			current.values = append(current.values, values[i].value)
			current.totalWeight += values[i].weight
		} else {
			clusters = append(clusters, current)
			current = projectionConsensusCluster{
				values:      []int64{values[i].value},
				totalWeight: values[i].weight,
			}
			clusterStart = values[i].value
		}
	}
	clusters = append(clusters, current)

	// Sort clusters by total weight (highest first)
	sort.Slice(clusters, func(i, j int) bool {
		return clusters[i].totalWeight > clusters[j].totalWeight
	})

	// Strategy 1: Prefer clusters with ≥2 observers
	for _, cluster := range clusters {
		if len(cluster.values) >= 2 {
			return cluster.values[len(cluster.values)/2]
		}
	}

	// Strategy 2: Fall back to highest-weight cluster
	if len(clusters) == 0 || len(clusters[0].values) == 0 {
		return 0
	}
	return clusters[0].values[len(clusters[0].values)/2]
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
