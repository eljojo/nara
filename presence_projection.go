package nara

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// Default thresholds (in nanoseconds)
const (
	DefaultMissingThresholdNano       = int64(300) * int64(time.Second)  // 5 minutes
	DefaultMissingThresholdGossipNano = int64(3600) * int64(time.Second) // 1 hour
)

// OnlineState represents the derived online status for a nara.
type OnlineState struct {
	Status        string // "ONLINE", "OFFLINE", "MISSING", or "" (unknown)
	LastEventTime int64  // Timestamp of the determining event (nanoseconds)
	LastEventType string // Service type of the determining event
	Observer      string // Who reported this status (for observation events)
}

// MissingThresholdFunc returns the MISSING threshold (in nanoseconds) for a given nara.
// This allows the projection to account for gossip mode naras having longer thresholds.
type MissingThresholdFunc func(name NaraName) int64

// OnlineStatusProjection maintains per-nara online status derived from events.
// It implements "most recent event wins" semantics.
//
// For total uptime calculation, use GetTotalUptime() which incorporates checkpoint
// data when available, falls back to backfill records, then calculates from
// status-change events.
type OnlineStatusProjection struct {
	states     map[NaraName]*OnlineState
	ledger     *SyncLedger
	projection *Projection
	mu         sync.RWMutex

	// Threshold function for MISSING detection
	missingThresholdFunc MissingThresholdFunc

	// For triggering updates
	triggerCh chan struct{}
}

// NewOnlineStatusProjection creates a new online status projection.
func NewOnlineStatusProjection(ledger *SyncLedger) *OnlineStatusProjection {
	p := &OnlineStatusProjection{
		states:    make(map[NaraName]*OnlineState),
		ledger:    ledger,
		triggerCh: make(chan struct{}, 1),
		missingThresholdFunc: func(name NaraName) int64 {
			return DefaultMissingThresholdNano
		},
	}

	p.projection = NewProjection(ledger, p.handleEvent)

	// Register reset handler to clear state when ledger is restructured
	p.projection.SetOnReset(func() {
		p.mu.Lock()
		p.states = make(map[NaraName]*OnlineState)
		p.mu.Unlock()
	})

	return p
}

// SetMissingThresholdFunc sets the function used to determine MISSING threshold per nara.
func (p *OnlineStatusProjection) SetMissingThresholdFunc(f MissingThresholdFunc) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.missingThresholdFunc = f
}

// handleEvent processes a single event and updates the projection state.
func (p *OnlineStatusProjection) handleEvent(event SyncEvent) error {
	// Determine which nara this event affects and what status it implies
	var targetName NaraName
	var newStatus string
	var observer NaraName

	switch event.Service {
	case ServiceHeyThere:
		if event.HeyThere != nil {
			targetName = event.HeyThere.From
			newStatus = "ONLINE"
			observer = event.HeyThere.From
		}
	case ServiceChau:
		if event.Chau != nil {
			targetName = event.Chau.From
			newStatus = "OFFLINE"
			observer = event.Chau.From
		}
	case ServiceSeen:
		// NOTE: ServiceSeen may be redundant now that we handle ServicePing and ServiceSocial.
		// Consider removing if no longer used elsewhere. Keep for now as it's lightweight.
		if event.Seen != nil {
			targetName = event.Seen.Subject
			newStatus = "ONLINE"
			observer = event.Seen.Observer
		}
	case ServiceObservation:
		if event.Observation != nil {
			targetName = event.Observation.Subject
			observer = event.Observation.Observer
			switch event.Observation.Type {
			case "status-change":
				newStatus = event.Observation.OnlineState
			case "restart", "first-seen":
				newStatus = "ONLINE"
			}
		}
	case ServiceSocial:
		// Social events (tease, observed, gossip) prove the Actor is active/online
		if event.Social != nil {
			targetName = event.Social.Actor
			newStatus = "ONLINE"
			observer = event.Social.Actor
		}
	case ServicePing:
		// Ping events prove both Observer and Target are active
		if event.Ping != nil {
			// Handle Observer - they sent the ping, definitely online
			p.updateState(event.Ping.Observer, "ONLINE", event.Timestamp, event.Service, event.Ping.Observer)
			// Handle Target - they responded (RTT exists), also online
			targetName = event.Ping.Target
			newStatus = "ONLINE"
			observer = event.Ping.Observer
		}
	}

	if targetName == "" || newStatus == "" {
		return nil
	}

	p.updateState(targetName, newStatus, event.Timestamp, event.Service, observer)
	return nil
}

// updateState updates the state for a nara if this event is newer than the current state.
func (p *OnlineStatusProjection) updateState(name NaraName, status string, timestamp int64, service string, observer NaraName) {
	if name == "" {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	current := p.states[name]
	if current == nil || timestamp > current.LastEventTime {
		p.states[name] = &OnlineState{
			Status:        status,
			LastEventTime: timestamp,
			LastEventType: service,
			Observer:      observer,
		}
	}
}

// GetStatus returns the current derived status for a nara.
// Returns "ONLINE", "OFFLINE", "MISSING", or "" if unknown.
// Note: Call Trigger() or RunOnce() before this if you need up-to-date data.
func (p *OnlineStatusProjection) GetStatus(name NaraName) string {
	p.mu.RLock()
	state := p.states[name]
	thresholdFunc := p.missingThresholdFunc
	p.mu.RUnlock()

	if state == nil {
		return "" // Unknown
	}

	// If status is ONLINE, check if event is too old (should be MISSING)
	if state.Status == "ONLINE" {
		now := time.Now().UnixNano()
		age := now - state.LastEventTime
		threshold := thresholdFunc(name)

		if age > threshold {
			return "MISSING"
		}
	}

	return state.Status
}

// GetState returns the full state for a nara (for debugging/testing).
func (p *OnlineStatusProjection) GetState(name NaraName) *OnlineState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.states[name]
}

// GetTotalUptime returns the total uptime in seconds for a nara.
// Uses checkpoint data if available, falls back to backfill record,
// then calculates from status-change events.
func (p *OnlineStatusProjection) GetTotalUptime(name NaraName) int64 {
	return p.ledger.DeriveTotalUptime(name)
}

// GetAllStatuses returns all known nara statuses.
func (p *OnlineStatusProjection) GetAllStatuses() map[NaraName]string {
	p.mu.RLock()
	thresholdFunc := p.missingThresholdFunc
	states := make(map[NaraName]*OnlineState, len(p.states))
	for k, v := range p.states {
		states[k] = v
	}
	p.mu.RUnlock()

	result := make(map[NaraName]string, len(states))
	now := time.Now().UnixNano()

	for name, state := range states {
		status := state.Status
		if status == "ONLINE" {
			age := now - state.LastEventTime
			threshold := thresholdFunc(name)
			if age > threshold {
				status = "MISSING"
			}
		}
		result[name] = status
	}

	return result
}

// RunToEnd catches up the projection to the current state.
func (p *OnlineStatusProjection) RunToEnd(ctx context.Context) error {
	return p.projection.RunToEnd(ctx)
}

// RunOnce processes any new events since the last run.
func (p *OnlineStatusProjection) RunOnce() (bool, error) {
	return p.projection.RunOnce()
}

// Trigger signals the projection to process new events.
func (p *OnlineStatusProjection) Trigger() {
	select {
	case p.triggerCh <- struct{}{}:
	default:
		// Already triggered, skip
	}
}

// RunContinuous runs the projection continuously, processing new events as they arrive.
// Primary updates come via Trigger() when events arrive; the ticker is a safety net.
func (p *OnlineStatusProjection) RunContinuous(ctx context.Context) {
	// Initial catch-up
	if err := p.RunToEnd(ctx); err != nil {
		logrus.WithError(err).Warn("Failed initial catch-up for online status projection")
	}

	// Ticker is just a safety net - main updates come via triggerCh
	// 30s is sufficient since event ingestion calls Trigger() immediately
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := p.RunOnce(); err != nil {
				logrus.WithError(err).Warn("Failed to run online status projection (ticker)")
			}
		case <-p.triggerCh:
			if _, err := p.RunOnce(); err != nil {
				logrus.WithError(err).Warn("Failed to run online status projection (trigger)")
			}
		}
	}
}

// Reset clears all state and resets the projection to reprocess from the beginning.
func (p *OnlineStatusProjection) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.states = make(map[NaraName]*OnlineState)
	p.projection.Reset()
}
