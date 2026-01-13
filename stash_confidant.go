package nara

import (
	"errors"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ConfidantStashStore stores stash payloads in memory (confidant side)
type ConfidantStashStore struct {
	stashes       map[string]*StashPayload // owner -> payload
	commitments   map[string]int64         // owner -> when we committed (unix timestamp)
	maxStashes    int                      // Memory-based limit (0 = unlimited)
	totalBytes    int64                    // Total bytes of stash data stored (for metrics)
	evictionCount int                      // Number of ghost prunings (only evict when owner offline 7+ days)
	mu            sync.RWMutex
}

// NewConfidantStashStore creates a new in-memory stash store
func NewConfidantStashStore() *ConfidantStashStore {
	return &ConfidantStashStore{
		stashes:     make(map[string]*StashPayload),
		commitments: make(map[string]int64),
		maxStashes:  0, // Unlimited by default, set via SetMaxStashes
	}
}

// HandleStashStore handles an incoming StashStore message
func (s *ConfidantStashStore) HandleStashStore(msg *StashStore, getPublicKey func(string) []byte) error {
	// Verify signature
	pubKey := getPublicKey(msg.From)
	if pubKey == nil {
		return errors.New("unknown sender")
	}
	if !msg.Verify(pubKey) {
		return errors.New("invalid signature")
	}

	// Check size limit
	if len(msg.Payload.Ciphertext) > MaxStashSize {
		return errors.New("stash too large")
	}

	// Store it
	s.Store(msg.From, &msg.Payload)
	return nil
}

// HasCapacity returns true if we can accept a new stash commitment
func (s *ConfidantStashStore) HasCapacity() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.maxStashes == 0 || len(s.stashes) < s.maxStashes
}

// Store stores a payload for the given owner. Returns true if accepted, false if at capacity.
// This creates a commitment - we promise to keep this stash unless the owner goes ghost (offline 7+ days).
func (s *ConfidantStashStore) Store(owner string, payload *StashPayload) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if this is an update to existing stash (always allowed)
	_, isUpdate := s.stashes[owner]

	// If not an update, check capacity
	if !isUpdate && s.maxStashes > 0 && len(s.stashes) >= s.maxStashes {
		return false // At capacity, reject
	}

	// Remove old size from totalBytes if replacing
	if oldPayload, exists := s.stashes[owner]; exists {
		s.totalBytes -= int64(oldPayload.Size())
	}

	// Store and create commitment
	s.stashes[owner] = payload
	s.commitments[owner] = time.Now().Unix()
	s.totalBytes += int64(payload.Size())

	return true // Accepted
}

// HasStashFor returns true if we have a stash for the given owner
func (s *ConfidantStashStore) HasStashFor(owner string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.stashes[owner]
	return ok
}

// Delete removes a stash for the given owner, returns true if it existed
func (s *ConfidantStashStore) Delete(owner string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	payload, exists := s.stashes[owner]
	if !exists {
		return false
	}

	// Update metrics
	s.totalBytes -= int64(payload.Size())

	// Remove stash and commitment
	delete(s.stashes, owner)
	delete(s.commitments, owner)

	return true
}

// CreateAck creates an ack message for the given owner
func (s *ConfidantStashStore) CreateAck(owner string) *StashStoreAck {
	return &StashStoreAck{
		Owner: owner,
	}
}

// HandleStashRequest handles a stash request and returns a response if we have the stash
func (s *ConfidantStashStore) HandleStashRequest(req *StashRequest, getPublicKey func(string) []byte) *StashResponse {
	// Verify signature
	pubKey := getPublicKey(req.From)
	if pubKey == nil {
		return nil
	}
	if !req.Verify(pubKey) {
		return nil
	}

	s.mu.RLock()
	payload, ok := s.stashes[req.From]
	s.mu.RUnlock()

	if !ok {
		return nil
	}

	return &StashResponse{
		Owner:   req.From,
		Payload: *payload,
	}
}

// GetAllOwners returns a list of all owners we're storing stashes for
func (s *ConfidantStashStore) GetAllOwners() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	owners := make([]string, 0, len(s.stashes))
	for owner := range s.stashes {
		owners = append(owners, owner)
	}
	return owners
}

// EvictGhost evicts the stash for a specific owner (used when they're confirmed dead/ghost)
func (s *ConfidantStashStore) EvictGhost(owner string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	payload, ok := s.stashes[owner]
	if !ok {
		return false
	}

	s.totalBytes -= int64(payload.Size())
	delete(s.stashes, owner)
	delete(s.commitments, owner)
	s.evictionCount++
	logrus.Infof("📦 Evicted ghost stash for %s (commitment broken - owner offline 7+ days)", owner)
	return true
}

// EvictGhosts evicts stashes for naras that have been offline for more than 7 days
// This is the only way stashes are evicted - we keep our commitments unless the owner is ghost
func (s *ConfidantStashStore) EvictGhosts(isGhost func(string) bool) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	evicted := 0
	for owner := range s.stashes {
		if isGhost(owner) {
			payload := s.stashes[owner]
			s.totalBytes -= int64(payload.Size())
			delete(s.stashes, owner)
			delete(s.commitments, owner)
			s.evictionCount++
			evicted++
			logrus.Infof("📦 Evicted ghost stash for %s (offline 7+ days)", owner)
		}
	}

	return evicted
}

// GetMetrics returns current storage metrics
func (s *ConfidantStashStore) GetMetrics() StashMetrics {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return StashMetrics{
		StashesStored:   len(s.stashes),
		TotalStashBytes: s.totalBytes,
		EvictionCount:   s.evictionCount,
		StorageLimit:    s.maxStashes,
	}
}

// SetMaxStashes sets the maximum number of stashes to store (memory limit)
func (s *ConfidantStashStore) SetMaxStashes(max int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxStashes = max
}
