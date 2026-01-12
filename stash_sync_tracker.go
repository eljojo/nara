package nara

import (
	"sync"
	"time"
)

// StashSyncTracker tracks when we last synced stashes with each peer (memory-only)
// This prevents over-syncing while keeping Nara's no-persistence principle
type StashSyncTracker struct {
	lastSyncPerPeer map[string]int64 // peer -> last sync timestamp (unix seconds)
	mu              sync.RWMutex
}

// NewStashSyncTracker creates a new memory-only sync tracker
func NewStashSyncTracker() *StashSyncTracker {
	return &StashSyncTracker{
		lastSyncPerPeer: make(map[string]int64),
	}
}

// UpdateLastSync records that we synced with a peer at the given time
func (t *StashSyncTracker) UpdateLastSync(peer string, timestamp int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lastSyncPerPeer[peer] = timestamp
}

// GetLastSync returns when we last synced with a peer (0 if never)
func (t *StashSyncTracker) GetLastSync(peer string) int64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.lastSyncPerPeer[peer]
}

// ShouldSync returns true if we should sync with this peer based on minimum interval
func (t *StashSyncTracker) ShouldSync(peer string, minInterval time.Duration) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	lastSync := t.lastSyncPerPeer[peer]
	if lastSync == 0 {
		return true // Never synced
	}

	elapsed := time.Now().Unix() - lastSync
	return elapsed >= int64(minInterval.Seconds())
}

// MarkSyncNow marks that we're syncing with a peer right now
func (t *StashSyncTracker) MarkSyncNow(peer string) {
	t.UpdateLastSync(peer, time.Now().Unix())
}
