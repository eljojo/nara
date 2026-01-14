package nara

import (
	"encoding/json"
	"sync"
	"time"
)

// StashManager manages stash operations for the owner
type StashManager struct {
	ownerName        string
	keypair          NaraKeypair
	encKeypair       EncryptionKeypair
	confidantTracker *ConfidantTracker

	currentStash      *StashData // Current stash data to be stored on confidants
	recoveredStash    *StashData
	currentTimestamp  int64
	hasRecoveredStash bool
	mu                sync.RWMutex
}

// NewStashManager creates a new stash manager for the owner
func NewStashManager(ownerName string, keypair NaraKeypair, targetConfidents int) *StashManager {
	return &StashManager{
		ownerName:        ownerName,
		keypair:          keypair,
		encKeypair:       DeriveEncryptionKeys(keypair.PrivateKey),
		confidantTracker: NewConfidantTracker(targetConfidents),
	}
}

// RequestStash broadcasts a stash request (implementation will be in network.go)
func (m *StashManager) RequestStash(network *MockMeshNetwork, responses chan *StashResponse) {
	// This is a placeholder - actual implementation will use MQTT
	// For testing, responses come through the channel
}

// ProcessResponses processes stash responses and picks the newest
func (m *StashManager) ProcessResponses(responses <-chan *StashResponse) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var newestData *StashData
	var newestTimestamp int64

	for resp := range responses {
		// Decrypt and decompress the payload
		data, err := DecryptStashPayload(&resp.Payload, m.encKeypair)
		if err != nil {
			continue // Skip invalid responses
		}

		// Skip if older than what we have
		if data.Timestamp <= m.currentTimestamp {
			continue
		}

		// Pick newest
		if data.Timestamp > newestTimestamp {
			newestTimestamp = data.Timestamp
			newestData = data
		}

		// Track the confident
		m.confidantTracker.Add(resp.From, time.Now().Unix())
	}

	if newestData != nil {
		m.recoveredStash = newestData
		m.hasRecoveredStash = true
		m.currentTimestamp = newestTimestamp
	}

	return nil
}

// HasRecoveredStash returns true if we recovered a stash
func (m *StashManager) HasRecoveredStash() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.hasRecoveredStash
}

// ShouldStartFresh returns true if we should start fresh (no stash recovered)
func (m *StashManager) ShouldStartFresh() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return !m.hasRecoveredStash
}

// GetRecoveredTimestamp returns the timestamp of the recovered stash
func (m *StashManager) GetRecoveredTimestamp() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.recoveredStash != nil {
		return m.recoveredStash.Timestamp
	}
	return 0
}

// SetCurrentTimestamp sets the current timestamp (for rejecting stale stash)
func (m *StashManager) SetCurrentTimestamp(ts int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentTimestamp = ts
}

// GetRecoveredData returns the recovered stash data, or nil if none
func (m *StashManager) GetRecoveredData() json.RawMessage {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.recoveredStash != nil {
		return m.recoveredStash.Data
	}
	return nil
}

// SetCurrentStash sets the current stash data to be stored on confidants
func (m *StashManager) SetCurrentStash(data json.RawMessage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentStash = &StashData{
		Timestamp: time.Now().Unix(),
		Data:      data,
		Version:   1,
	}
	m.currentTimestamp = m.currentStash.Timestamp
}

// GetCurrentStash returns the current stash data, or nil if none
func (m *StashManager) GetCurrentStash() *StashData {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentStash
}

// HasStashData returns true if we have stash data to store
func (m *StashManager) HasStashData() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentStash != nil && len(m.currentStash.Data) > 0
}
