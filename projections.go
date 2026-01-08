// Projections provide event-sourced read models for nara.
// They maintain pre-computed state that updates incrementally as events arrive.

package nara

import (
	"context"
	"sync"
)

// EventHandler is a callback function that processes a single event.
type EventHandler func(event SyncEvent) error

// ResetHandler is called when the projection needs to reset its state.
type ResetHandler func()

// Projection processes events from a SyncLedger incrementally.
type Projection struct {
	ledger      *SyncLedger
	handler     EventHandler
	onReset     ResetHandler // Called when projection needs to clear state
	position    int          // Index of next event to process
	lastVersion int64        // Last seen ledger version
	mu          sync.Mutex
}

// NewProjection creates a new projection with the given event handler.
func NewProjection(ledger *SyncLedger, handler EventHandler) *Projection {
	return &Projection{
		ledger:      ledger,
		handler:     handler,
		position:    0,
		lastVersion: -1, // -1 indicates "never processed" - don't reset on first run
	}
}

// SetOnReset sets a callback that's invoked when the projection resets.
// This allows projections to clear their derived state on reset.
func (p *Projection) SetOnReset(handler ResetHandler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onReset = handler
}

// RunToEnd processes all events from the current position to the end of the ledger.
func (p *Projection) RunToEnd(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if ledger structure changed (pruning, etc.) - if so, reset and reprocess
	events, total, version := p.ledger.GetEventsSince(p.position)
	if version != p.lastVersion && p.lastVersion >= 0 {
		// Ledger was restructured, need to reset and reprocess from beginning
		p.position = 0
		if p.onReset != nil {
			p.onReset()
		}
		events, total, version = p.ledger.GetEventsSince(0)
	}

	for i, event := range events {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if err := p.handler(event); err != nil {
				return err
			}
			p.position = p.position + i + 1
		}
	}
	// Ensure position is at the end even if no events processed
	p.position = total
	p.lastVersion = version

	return nil
}

// RunOnce processes new events since last run (if any).
// Returns true if any events were processed.
func (p *Projection) RunOnce() (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if ledger structure changed (pruning, etc.) - if so, reset and reprocess
	events, total, version := p.ledger.GetEventsSince(p.position)
	if version != p.lastVersion && p.lastVersion >= 0 {
		// Ledger was restructured, need to reset and reprocess from beginning
		p.position = 0
		if p.onReset != nil {
			p.onReset()
		}
		events, total, version = p.ledger.GetEventsSince(0)
	}

	if len(events) == 0 {
		p.lastVersion = version
		return false, nil
	}

	for i, event := range events {
		if err := p.handler(event); err != nil {
			return true, err
		}
		p.position = p.position + i + 1
	}
	// Ensure position is at the end
	p.position = total
	p.lastVersion = version

	return true, nil
}

// Reset resets the projection to process from the beginning.
func (p *Projection) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.position = 0
}

// Position returns the current position in the event stream.
func (p *Projection) Position() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.position
}

// ProjectionStore manages all projections for a nara.
type ProjectionStore struct {
	ledger *SyncLedger

	onlineStatus *OnlineStatusProjection
	clout        *CloutProjection
	opinion      *OpinionConsensusProjection

	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.RWMutex
}

// NewProjectionStore creates a new projection store for the given ledger.
func NewProjectionStore(ledger *SyncLedger) *ProjectionStore {
	ctx, cancel := context.WithCancel(context.Background())

	return &ProjectionStore{
		ledger:       ledger,
		onlineStatus: NewOnlineStatusProjection(ledger),
		clout:        NewCloutProjection(ledger),
		opinion:      NewOpinionConsensusProjection(ledger),
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Start begins continuous projection updates.
func (s *ProjectionStore) Start() {
	// OnlineStatus runs continuously
	go s.onlineStatus.RunContinuous(s.ctx)
}

// Trigger forces an immediate projection update for all projections.
func (s *ProjectionStore) Trigger() {
	s.onlineStatus.Trigger()
	// Clout and opinion are on-demand, no need to trigger
}

// Shutdown gracefully stops all projections.
func (s *ProjectionStore) Shutdown() {
	s.cancel()
}

// OnlineStatus returns the online status projection.
func (s *ProjectionStore) OnlineStatus() *OnlineStatusProjection {
	return s.onlineStatus
}

// Clout returns the clout projection.
func (s *ProjectionStore) Clout() *CloutProjection {
	return s.clout
}

// Opinion returns the opinion consensus projection.
func (s *ProjectionStore) Opinion() *OpinionConsensusProjection {
	return s.opinion
}
