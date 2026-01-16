package nara

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// SocialEventRecord stores social event data needed for clout calculation.
type SocialEventRecord struct {
	ID        string
	Timestamp int64 // nanoseconds
	Type      string
	Actor     NaraName
	Target    NaraName
	Reason    string
	Witness   NaraName
}

// CloutProjection accumulates social events for clout calculation.
// Clout is observer-dependent (TeaseResonates + personality), so we store raw events
// and compute clout on demand.
type CloutProjection struct {
	events     []SocialEventRecord
	ledger     *SyncLedger
	projection *Projection
	mu         sync.RWMutex
}

// NewCloutProjection creates a new clout projection.
func NewCloutProjection(ledger *SyncLedger) *CloutProjection {
	p := &CloutProjection{
		events: make([]SocialEventRecord, 0),
		ledger: ledger,
	}

	p.projection = NewProjection(ledger, p.handleEvent)

	// Register reset handler to clear state when ledger is restructured
	p.projection.SetOnReset(func() {
		p.mu.Lock()
		p.events = make([]SocialEventRecord, 0)
		p.mu.Unlock()
	})

	return p
}

// handleEvent processes a single event and stores it if it's a social event.
func (p *CloutProjection) handleEvent(event SyncEvent) error {
	// Only store social events
	if event.Service != ServiceSocial || event.Social == nil {
		return nil
	}

	payload := event.Social

	p.mu.Lock()
	defer p.mu.Unlock()

	p.events = append(p.events, SocialEventRecord{
		ID:        event.ID,
		Timestamp: event.Timestamp,
		Type:      payload.Type,
		Actor:     payload.Actor,
		Target:    payload.Target,
		Reason:    payload.Reason,
		Witness:   payload.Witness,
	})

	return nil
}

// DeriveClout computes subjective clout scores based on stored events.
// Each observer derives their own opinion based on their soul and personality.
// Note: Call Trigger() or RunOnce() before this if you need up-to-date data.
func (p *CloutProjection) DeriveClout(observerSoul string, personality NaraPersonality) map[NaraName]float64 {
	p.mu.RLock()
	events := make([]SocialEventRecord, len(p.events))
	copy(events, p.events)
	p.mu.RUnlock()

	clout := make(map[NaraName]float64)
	now := time.Now().Unix()

	for _, record := range events {
		weight := computeCloutEventWeight(record, personality, now)

		switch record.Type {
		case "tease":
			// Check if tease resonates with observer
			if TeaseResonates(record.ID, observerSoul, personality) {
				clout[record.Actor] += weight * 1.0 // good tease = clout
			} else {
				clout[record.Actor] -= weight * 0.3 // bad tease = cringe
			}
		case "observed":
			// Third-party observation, smaller weight
			if TeaseResonates(record.ID, observerSoul, personality) {
				clout[record.Actor] += weight * 0.5
			}
		case "gossip":
			// Gossip has minimal direct clout impact
			clout[record.Actor] += weight * 0.1
		case "observation":
			// System observations affect the TARGET's clout
			applyProjectionObservationClout(clout, record, weight)
		case "service":
			// Service events (like stash) give clout to the ACTOR (the helper)
			if record.Reason == ReasonStashStored {
				clout[record.Actor] += weight * 2.0 // generous reward for being helpful
			} else {
				clout[record.Actor] += weight * 0.5
			}
		}
	}

	return clout
}

// applyProjectionObservationClout applies clout changes for observation events.
func applyProjectionObservationClout(clout map[NaraName]float64, record SocialEventRecord, weight float64) {
	switch record.Reason {
	case ReasonOnline:
		// Coming online is slightly positive (reliable, available)
		clout[record.Target] += weight * 0.1
	case ReasonOffline:
		// Going offline is slightly negative (less available)
		clout[record.Target] -= weight * 0.05
	case ReasonJourneyPass:
		// Participating in journeys is positive (engaged citizen)
		clout[record.Target] += weight * 0.2
	case ReasonJourneyComplete:
		// Completing journeys is very positive (success!)
		clout[record.Target] += weight * 0.5
	case ReasonJourneyTimeout:
		// Journey timeout is negative (unreliable)
		clout[record.Target] -= weight * 0.3
	}
}

// computeCloutEventWeight calculates the personality-adjusted weight for an event.
func computeCloutEventWeight(record SocialEventRecord, personality NaraPersonality, now int64) float64 {
	weight := 1.0

	// Base personality modifiers
	weight += float64(personality.Sociability) / 200.0
	weight -= float64(personality.Chill) / 400.0

	// Reason-based adjustments
	switch record.Reason {
	case ReasonHighRestarts:
		if personality.Sociability > 70 {
			weight *= 0.7
		}
	case ReasonComeback:
		if personality.Sociability > 60 {
			weight *= 1.4
		}
	case ReasonTrendAbandon:
		if personality.Agreeableness > 70 {
			weight *= 0.5
		}
		if personality.Sociability > 60 {
			weight *= 1.2
		}
	case ReasonRandom:
		if personality.Chill > 60 {
			weight *= 0.3
		}
		if personality.Agreeableness > 70 {
			weight *= 0.6
		}
	case ReasonNiceNumber:
		weight *= 1.1
	case ReasonOnline, ReasonOffline:
		weight *= 0.5
	case ReasonJourneyPass:
		weight *= 0.8
	case ReasonJourneyComplete:
		weight *= 1.3
	case ReasonJourneyTimeout:
		weight *= 1.2
	case ReasonStashStored:
		weight *= 1.5
	}

	// Type-based adjustments
	switch record.Type {
	case "observed":
		weight *= 0.7
	case "gossip":
		weight *= 0.4
	}

	// Time decay
	// Note: record.Timestamp is in nanoseconds, now is in seconds
	age := now - record.Timestamp/1e9
	if age < 0 {
		age = 7 * 24 * 60 * 60 // Cap at 7 days for future timestamps
	}
	if age > 0 {
		baseHalfLife := float64(24 * 60 * 60) // 24 hours
		chillModifier := 1.0 + float64(50-personality.Chill)/100.0
		socModifier := 1.0 + float64(personality.Sociability)/333.0
		halfLife := baseHalfLife * chillModifier * socModifier
		decayFactor := 1.0 / (1.0 + float64(age)/halfLife)
		weight *= decayFactor
	}

	// Minimum weight
	if weight < 0.1 {
		weight = 0.1
	}

	return weight
}

// RunToEnd catches up the projection to the current state.
func (p *CloutProjection) RunToEnd(ctx context.Context) error {
	return p.projection.RunToEnd(ctx)
}

// RunOnce processes any new events since last run.
func (p *CloutProjection) RunOnce() (bool, error) {
	return p.projection.RunOnce()
}

// Trigger processes new events immediately.
// Use this when events have been added and you need updated state.
func (p *CloutProjection) Trigger() {
	if _, err := p.projection.RunOnce(); err != nil {
		logrus.WithError(err).Warn("Failed to run clout projection")
	}
}

// EventCount returns the number of social events stored.
func (p *CloutProjection) EventCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.events)
}

// Reset clears all state and resets the projection.
func (p *CloutProjection) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = make([]SocialEventRecord, 0)
	p.projection.Reset()
}
