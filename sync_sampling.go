package nara

import (
	"math"
	"math/rand"
	"sort"
	"time"

	"github.com/eljojo/nara/types"
)

// SampleEvents returns a decay-weighted sample of events (mode: "sample")
// This implements "organic hazy memory" for boot recovery:
// - Recent events more likely (clearer memory)
// - Old events less likely but not zero (fading memory)
// - Critical events always included
// - Events emitted/observed by myName have higher weight
func (l *SyncLedger) SampleEvents(sampleSize int, myName types.NaraName, services []string, subjects []types.NaraName) []SyncEvent {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if sampleSize <= 0 {
		return []SyncEvent{}
	}

	// Build filter sets
	serviceSet := make(map[string]bool)
	filterByService := len(services) > 0
	for _, s := range services {
		serviceSet[s] = true
	}

	subjectSet := make(map[types.NaraName]bool)
	filterBySubject := len(subjects) > 0
	for _, s := range subjects {
		subjectSet[s] = true
	}

	// 1. Always include critical events (checkpoints, hey_there, chau)
	var critical []SyncEvent
	var candidates []weightedEvent

	now := time.Now()

	for _, e := range l.Events {
		// Apply filters
		if filterByService && !serviceSet[e.Service] {
			continue
		}
		if filterBySubject && !subjectSet[e.GetActor()] && !subjectSet[e.GetTarget()] {
			continue
		}

		// Critical events always included
		if isCriticalEvent(e) {
			critical = append(critical, e)
			continue
		}

		// Calculate weight for non-critical events
		weight := calculateEventWeight(e, now, myName)
		candidates = append(candidates, weightedEvent{
			Event:  e,
			Weight: weight,
		})
	}

	// If critical events alone exceed sample size, return them sorted by timestamp
	if len(critical) >= sampleSize {
		sort.Slice(critical, func(i, j int) bool {
			return critical[i].Timestamp < critical[j].Timestamp
		})
		return critical[:sampleSize]
	}

	// 2. Sample from candidates using weighted random selection
	remaining := sampleSize - len(critical)
	sampled := weightedRandomSample(candidates, remaining)

	// 3. Combine critical + sampled, sort by timestamp
	result := append(critical, sampled...)
	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp < result[j].Timestamp
	})

	return result
}

// weightedEvent pairs an event with its inclusion weight
type weightedEvent struct {
	Event  SyncEvent
	Weight float64
}

// isCriticalEvent returns true for events that should always be included
func isCriticalEvent(e SyncEvent) bool {
	switch e.Service {
	case ServiceCheckpoint, ServiceHeyThere, ServiceChau:
		return true
	case ServiceObservation:
		// Observation events with critical importance
		if e.Observation != nil && e.Observation.Importance == ImportanceCritical {
			return true
		}
	}
	return false
}

// calculateEventWeight computes the inclusion probability weight
// Factors:
// - Age decay: exponential decay with ~30-day half-life
// - Importance boost: critical events weighted higher
// - Self-relevance: events we emitted or are about us
func calculateEventWeight(e SyncEvent, now time.Time, myName types.NaraName) float64 {
	// Age decay (exponential with 30-day half-life)
	eventTime := time.Unix(0, e.Timestamp)
	age := now.Sub(eventTime)
	halfLifeHours := 30.0 * 24.0 // 30 days in hours
	decay := math.Exp(-age.Hours() / (halfLifeHours * 0.693))

	// Importance boost (check observation events)
	importance := 1.0
	if e.Service == ServiceObservation && e.Observation != nil {
		switch e.Observation.Importance {
		case ImportanceCritical:
			importance = 5.0
		case ImportanceNormal:
			importance = 2.0
		case ImportanceCasual:
			importance = 1.0
		}
	}

	// Self-relevance boost (events we emitted or are about us)
	selfBoost := 1.0
	if e.Emitter == myName || e.GetActor() == myName || e.GetTarget() == myName {
		selfBoost = 2.0
	}

	return decay * importance * selfBoost
}

// weightedRandomSample selects n events using weighted random sampling
// Uses the "reservoir sampling with weights" algorithm
func weightedRandomSample(candidates []weightedEvent, n int) []SyncEvent {
	if n <= 0 || len(candidates) == 0 {
		return []SyncEvent{}
	}

	// If we want more than we have, return all
	if n >= len(candidates) {
		result := make([]SyncEvent, len(candidates))
		for i, c := range candidates {
			result[i] = c.Event
		}
		return result
	}

	// Weighted random sampling using "A-Res" algorithm
	// Each candidate gets a random key = random^(1/weight)
	// Select top n by key
	type keyedEvent struct {
		Event SyncEvent
		Key   float64
	}

	keyed := make([]keyedEvent, len(candidates))
	for i, c := range candidates {
		// Avoid division by zero or negative weights
		weight := c.Weight
		if weight <= 0 {
			weight = 0.001
		}
		// Key = random^(1/weight) for weighted reservoir sampling
		key := math.Pow(rand.Float64(), 1.0/weight)
		keyed[i] = keyedEvent{Event: c.Event, Key: key}
	}

	// Sort by key descending (highest keys = selected)
	sort.Slice(keyed, func(i, j int) bool {
		return keyed[i].Key > keyed[j].Key
	})

	// Take top n
	result := make([]SyncEvent, n)
	for i := 0; i < n; i++ {
		result[i] = keyed[i].Event
	}

	return result
}
