package utilities

import (
	"errors"
	"sync"
	"time"

	"github.com/eljojo/nara/runtime"
	"github.com/sirupsen/logrus"
)

// Correlator tracks pending requests and matches responses.
//
// This is a generic utility for request/response patterns. Services
// that need request/response semantics (like stash) can use this.
//
// Example:
//
//	type StashService struct {
//	    storeReqs *utilities.Correlator[messages.StashStoreAck]
//	}
//
//	func (s *StashService) StoreWith(confidant string, data []byte) error {
//	    msg := &runtime.Message{Kind: "stash:store", ...}
//	    result := <-s.storeReqs.Send(s.rt, msg)
//	    return result.Err
//	}
type Correlator[Resp any] struct {
	pending map[string]*pendingRequest[Resp]
	mu      sync.Mutex
	timeout time.Duration
}

type pendingRequest[Resp any] struct {
	ch      chan Result[Resp]
	sentAt  time.Time
	expires time.Time
}

// Result is returned by Send.
type Result[Resp any] struct {
	Response Resp
	Err      error // ErrTimeout if no response in time
}

// ErrTimeout is returned when a request times out.
var ErrTimeout = errors.New("request timed out")

// NewCorrelator creates a correlator with the given timeout.
func NewCorrelator[Resp any](timeout time.Duration) *Correlator[Resp] {
	c := &Correlator[Resp]{
		pending: make(map[string]*pendingRequest[Resp]),
		timeout: timeout,
	}
	go c.reapLoop() // Clean up timed-out requests
	return c
}

// Send emits a request and returns a channel for the response.
//
// The message is emitted via runtime.Emit(), and a channel is returned
// that will receive either the response or a timeout error.
func (c *Correlator[Resp]) Send(rt runtime.RuntimeInterface, msg *runtime.Message) <-chan Result[Resp] {
	ch := make(chan Result[Resp], 1)

	// Ensure timestamp is set (required for ID computation)
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}

	// Ensure message has an ID before storing in pending map
	// (Emit() will compute one if empty, but we need it NOW for correlation)
	if msg.ID == "" {
		msg.ID = runtime.ComputeID(msg)
	}

	c.mu.Lock()
	c.pending[msg.ID] = &pendingRequest[Resp]{
		ch:      ch,
		sentAt:  time.Now(),
		expires: time.Now().Add(c.timeout),
	}
	c.mu.Unlock()

	// Emit the request
	if err := rt.Emit(msg); err != nil {
		// Failed to emit - clean up and return error
		c.mu.Lock()
		delete(c.pending, msg.ID)
		c.mu.Unlock()
		ch <- Result[Resp]{Err: err}
		return ch
	}

	return ch
}

// Receive is called when a response arrives - matches it to pending request.
//
// Returns true if the response matched a pending request, false otherwise.
// Services call this from their response handlers.
func (c *Correlator[Resp]) Receive(requestID string, resp Resp) bool {
	c.mu.Lock()
	pending, ok := c.pending[requestID]
	if ok {
		delete(c.pending, requestID)
	}
	// Debug: log pending requests
	if !ok {
		pendingIDs := make([]string, 0, len(c.pending))
		for id := range c.pending {
			pendingIDs = append(pendingIDs, id)
		}
		logrus.Debugf("[correlator] Response for %s not found. Pending: %v", requestID, pendingIDs)
	}
	c.mu.Unlock()

	if ok {
		pending.ch <- Result[Resp]{Response: resp}
		return true
	}

	return false // No pending request (late response, already timed out)
}

// reapLoop runs in the background and cleans up timed-out requests.
func (c *Correlator[Resp]) reapLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for id, req := range c.pending {
			if now.After(req.expires) {
				req.ch <- Result[Resp]{Err: ErrTimeout}
				delete(c.pending, id)
			}
		}
		c.mu.Unlock()
	}
}
