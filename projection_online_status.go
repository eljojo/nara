package nara

import (
	"context"
	"sync"
	"time"
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
}

// MissingThresholdFunc returns the MISSING threshold (in nanoseconds) for a given nara.
// This allows the projection to account for gossip mode naras having longer thresholds.
type MissingThresholdFunc func(name string) int64

// OnlineStatusProjection maintains per-nara online status derived from events.
// It implements "most recent event wins" semantics.
type OnlineStatusProjection struct {
	states     map[string]*OnlineState
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
		states:    make(map[string]*OnlineState),
		ledger:    ledger,
		triggerCh: make(chan struct{}, 1),
		missingThresholdFunc: func(name string) int64 {
			return DefaultMissingThresholdNano
		},
	}

	p.projection = NewProjection(ledger, p.handleEvent)

	// Register reset handler to clear state when ledger is restructured
	p.projection.SetOnReset(func() {
		p.mu.Lock()
		p.states = make(map[string]*OnlineState)
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
	var targetName string
	var newStatus string

	switch event.Service {
	case ServiceHeyThere:
		if event.HeyThere != nil {
			targetName = event.HeyThere.From
			newStatus = "ONLINE"
		}
	case ServiceChau:
		if event.Chau != nil {
			targetName = event.Chau.From
			newStatus = "OFFLINE"
		}
	case ServiceSeen:
		if event.Seen != nil {
			targetName = event.Seen.Subject
			newStatus = "ONLINE"
		}
	case ServiceObservation:
		if event.Observation != nil {
			targetName = event.Observation.Subject
			switch event.Observation.Type {
			case "status-change":
				newStatus = event.Observation.OnlineState
			case "restart", "first-seen":
				newStatus = "ONLINE"
			}
		}
	}

	if targetName == "" || newStatus == "" {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Update state if this event is newer than the current state
	current := p.states[targetName]
	if current == nil || event.Timestamp > current.LastEventTime {
		p.states[targetName] = &OnlineState{
			Status:        newStatus,
			LastEventTime: event.Timestamp,
			LastEventType: event.Service,
		}
	}

	return nil
}

// GetStatus returns the current derived status for a nara.
// Returns "ONLINE", "OFFLINE", "MISSING", or "" if unknown.
func (p *OnlineStatusProjection) GetStatus(name string) string {
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
func (p *OnlineStatusProjection) GetState(name string) *OnlineState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.states[name]
}

// GetAllStatuses returns all known nara statuses.
func (p *OnlineStatusProjection) GetAllStatuses() map[string]string {
	p.mu.RLock()
	thresholdFunc := p.missingThresholdFunc
	states := make(map[string]*OnlineState, len(p.states))
	for k, v := range p.states {
		states[k] = v
	}
	p.mu.RUnlock()

	result := make(map[string]string, len(states))
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
	p.RunToEnd(ctx)

	// Ticker is just a safety net - main updates come via triggerCh
	// 30s is sufficient since event ingestion calls Trigger() immediately
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.RunOnce()
		case <-p.triggerCh:
			p.RunOnce()
		}
	}
}

// Reset clears all state and resets the projection to reprocess from the beginning.
func (p *OnlineStatusProjection) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.states = make(map[string]*OnlineState)
	p.projection.Reset()
}
