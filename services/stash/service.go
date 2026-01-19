package stash

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/eljojo/nara/messages"
	"github.com/eljojo/nara/runtime"
	"github.com/eljojo/nara/types"
)

// NOTE: Encryption is provided by runtime.Seal/Open which delegates to NaraKeypair.

// Service implements distributed encrypted storage (stash).
//
// Naras store their encrypted state with trusted peers (confidants) instead
// of on disk. Only the owner can decrypt, but confidants hold the ciphertext.
type Service struct {
	runtime.ServiceBase                    // Provides RT and Log (auto-populated by runtime)
	keypair             runtime.KeypairInterface // Cached from runtime for encryption

	// Stored stashes (we're a confidant for these owners)
	mu     sync.RWMutex
	stored map[types.NaraID]*EncryptedStash // ownerID -> stash

	// Our confidants (peers who hold our stash)
	confidants       []types.NaraID // List of nara IDs
	targetConfidants int            // Target number of confidants (default: 3)

	// Our own stash data (to be encrypted and distributed to confidants)
	myStashData      []byte // Arbitrary JSON payload
	myStashTimestamp int64  // When it was last updated
}

// EncryptedStash is what we store for other naras.
type EncryptedStash struct {
	OwnerID    types.NaraID
	Nonce      []byte
	Ciphertext []byte
	StoredAt   time.Time
}

// NewService creates a new stash service.
func NewService() *Service {
	return &Service{
		stored:           make(map[types.NaraID]*EncryptedStash),
		confidants:       make([]types.NaraID, 0),
		targetConfidants: 3, // Default: 3 confidants for redundancy
	}
}

// === Service interface ===

func (s *Service) Name() string {
	return "stash"
}

func (s *Service) Init() error {
	s.keypair = s.RT.Keypair() // Cache keypair reference
	s.Log.Info("stash service initialized successfully")
	return nil
}

func (s *Service) Start() error {
	s.Log.Info("stash service started")
	return nil
}

func (s *Service) Stop() error {
	if s.Log != nil {
		s.Log.Info("stash service stopped")
	}
	return nil
}

// === Public API ===

// StoreWith stores encrypted data with a confidant.
//
// This is a synchronous call that blocks until the confidant acknowledges
// receipt or the request times out.
func (s *Service) StoreWith(confidantID types.NaraID, data []byte) error {
	// Encrypt the data using keypair
	nonce, ciphertext, err := s.keypair.Seal(data)
	if err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}

	// Create store request
	msg := &runtime.Message{
		Kind:    "stash:store",
		Version: 1,
		ToID:    confidantID,
		Payload: &messages.StashStorePayload{
			OwnerID:    s.RT.MeID(),
			Owner:      s.RT.Me().Name,
			Nonce:      nonce,
			Ciphertext: ciphertext,
			Timestamp:  time.Now().Unix(),
		},
	}

	// Send and wait for ack (30 second timeout)
	result := <-s.RT.Call(msg, 30*time.Second)
	if result.Error != nil {
		return fmt.Errorf("store request: %w", result.Error)
	}

	// Extract payload from response
	ack, ok := result.Response.Payload.(*messages.StashStoreAck)
	if !ok {
		return fmt.Errorf("unexpected response type: %T", result.Response.Payload)
	}

	if !ack.Success {
		return fmt.Errorf("store failed: %s", ack.Reason)
	}

	s.Log.Info("stored stash with %s", confidantID)
	return nil
}

// RequestFrom requests stored data from a confidant.
//
// Returns the decrypted data if the confidant has it, or an error.
func (s *Service) RequestFrom(confidantID types.NaraID) ([]byte, error) {
	// Create request
	msg := &runtime.Message{
		Kind:    "stash:request",
		Version: 1,
		ToID:    confidantID,
		Payload: &messages.StashRequestPayload{
			OwnerID:   s.RT.MeID(),
			RequestID: "", // Runtime will use msg.ID
		},
	}

	// Send and wait for response (30 second timeout)
	result := <-s.RT.Call(msg, 30*time.Second)
	if result.Error != nil {
		return nil, fmt.Errorf("request: %w", result.Error)
	}

	// Extract payload from response
	resp, ok := result.Response.Payload.(*messages.StashResponsePayload)
	if !ok {
		return nil, fmt.Errorf("unexpected response type: %T", result.Response.Payload)
	}

	if !resp.Found {
		return nil, fmt.Errorf("confidant %s has no stash for us", confidantID)
	}

	// Decrypt using keypair
	plaintext, err := s.keypair.Open(resp.Nonce, resp.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	s.Log.Info("recovered stash from %s", confidantID)
	return plaintext, nil
}

// RecoverFromAny attempts to recover from any available confidant.
//
// Tries all configured confidants and returns the first successful recovery.
func (s *Service) RecoverFromAny() ([]byte, error) {
	if len(s.confidants) == 0 {
		return nil, fmt.Errorf("no confidants configured")
	}

	for _, confidantID := range s.confidants {
		data, err := s.RequestFrom(confidantID)
		if err == nil {
			return data, nil
		}
		s.Log.Warn("recovery from %s failed: %v", confidantID, err)
	}

	return nil, fmt.Errorf("no confidant had our stash")
}

// SetConfidants configures the list of confidants to use.
func (s *Service) SetConfidants(confidantIDs []types.NaraID) {
	s.confidants = confidantIDs
	s.Log.Info("configured %d confidants", len(confidantIDs))
}

// TargetConfidants returns the target number of confidants.
func (s *Service) TargetConfidants() int {
	return s.targetConfidants
}

// Confidants returns the list of current confidants.
// PushTo sends the stored stash to the owner.
// This is used for recovery - when we see a peer come online (hey-there),
// we proactively send them their stash if we have it.
func (s *Service) PushTo(ownerID types.NaraID) error {
	stash := s.GetStoredStash(ownerID)
	if stash == nil {
		return fmt.Errorf("no stash for %s", ownerID)
	}

	// Send the stash back via mesh
	msg := &runtime.Message{
		Kind: "stash:response",
		ToID: ownerID,
		Payload: &messages.StashResponsePayload{
			OwnerID:    ownerID,
			Found:      true,
			Nonce:      stash.Nonce,
			Ciphertext: stash.Ciphertext,
			StoredAt:   stash.StoredAt.Unix(),
		},
	}

	if err := s.RT.Emit(msg); err != nil {
		return fmt.Errorf("emit stash response: %w", err)
	}

	s.Log.Info("pushed stash to %s (%d bytes)", ownerID, len(stash.Ciphertext))
	return nil
}

func (s *Service) Confidants() []types.NaraID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]types.NaraID, len(s.confidants))
	copy(result, s.confidants)
	return result
}

// SelectConfidantsAutomatically picks 3 confidants automatically:
// - First: peer with highest uptime
// - Second and third: random peers
// Returns error if unable to find 3 willing peers.
func (s *Service) SelectConfidantsAutomatically() error {
	// Check if runtime has network info (might not be configured yet)
	if s.RT == nil {
		s.Log.Error("CRITICAL: Stash service runtime is nil! Init() was never called or failed silently.")
		s.Log.Error("This usually means initRuntime() or startRuntime() failed during startup.")
		s.Log.Error("Check logs for 'Failed to initialize runtime' or 'Failed to start runtime'.")
		return fmt.Errorf("runtime not initialized - check startup logs for initialization errors")
	}

	s.Log.Info("selecting confidants automatically...")

	// Get list of online peers from runtime
	peers := s.RT.OnlinePeers()

	s.Log.Info("auto-selecting confidants: found %d online peers", len(peers))
	for i, peer := range peers {
		s.Log.Info("  peer %d: %s (%s) uptime=%v", i+1, peer.Name, peer.ID, peer.Uptime)
	}

	if len(peers) < s.targetConfidants {
		return fmt.Errorf("need at least %d online peers, only found %d", s.targetConfidants, len(peers))
	}

	s.Log.Info("found %d online peers", len(peers))

	// Sort peers by uptime (highest first)
	sortedPeers := make([]*runtime.PeerInfo, len(peers))
	copy(sortedPeers, peers)

	// Simple bubble sort by uptime
	for i := 0; i < len(sortedPeers)-1; i++ {
		for j := 0; j < len(sortedPeers)-i-1; j++ {
			if sortedPeers[j].Uptime < sortedPeers[j+1].Uptime {
				sortedPeers[j], sortedPeers[j+1] = sortedPeers[j+1], sortedPeers[j]
			}
		}
	}

	selected := make([]types.NaraID, 0, 3)
	used := make(map[types.NaraID]bool)

	// Try to get first confidant (highest uptime)
	for _, peer := range sortedPeers {
		if len(selected) >= 1 {
			break
		}

		// Skip if already used or is ourselves
		if used[peer.ID] || peer.ID == s.RT.MeID() {
			continue
		}

		// Try to store with this peer
		testData := []byte(fmt.Sprintf(`{"test":"probe","timestamp":%d}`, time.Now().Unix()))
		s.Log.Info("trying peer %s (%s) as first confidant (uptime: %v)...", peer.Name, peer.ID, peer.Uptime)
		if err := s.StoreWith(peer.ID, testData); err != nil {
			s.Log.Warn("peer %s declined: %v", peer.Name, err)
			continue
		}

		s.Log.Info("peer %s accepted! selected confidant 1/3 (uptime: %s)", peer.Name, peer.Uptime)
		selected = append(selected, peer.ID)
		used[peer.ID] = true
	}

	if len(selected) == 0 {
		return fmt.Errorf("no peer accepted to be first confidant")
	}

	// Shuffle remaining peers for random selection
	remainingPeers := make([]*runtime.PeerInfo, 0, len(peers)-1)
	for _, peer := range peers {
		if !used[peer.ID] && peer.ID != s.RT.MeID() {
			remainingPeers = append(remainingPeers, peer)
		}
	}

	// Try random peers for remaining confidant slots
	for len(selected) < s.targetConfidants && len(remainingPeers) > 0 {
		// Pick random index
		idx := time.Now().UnixNano() % int64(len(remainingPeers))
		peer := remainingPeers[idx]

		// Try to store with this peer
		testData := []byte(fmt.Sprintf(`{"test":"probe","timestamp":%d}`, time.Now().Unix()))
		if err := s.StoreWith(peer.ID, testData); err != nil {
			s.Log.Warn("peer %s declined: %v", peer.ID, err)
			// Remove from candidates and try next
			remainingPeers = append(remainingPeers[:idx], remainingPeers[idx+1:]...)
			continue
		}

		s.Log.Info("selected confidant %d/%d: %s", len(selected)+1, s.targetConfidants, peer.ID)
		selected = append(selected, peer.ID)
		used[peer.ID] = true

		// Remove from candidates
		remainingPeers = append(remainingPeers[:idx], remainingPeers[idx+1:]...)
	}

	if len(selected) < s.targetConfidants {
		return fmt.Errorf("only found %d willing confidants, need %d", len(selected), s.targetConfidants)
	}

	// Store the selected confidants
	s.SetConfidants(selected)
	s.Log.Info("automatically selected %d confidants", s.targetConfidants)

	return nil
}

// SetStashData updates the stash data and distributes it to all confidants.
// If fewer than targetConfidants are configured, it automatically selects peers.
func (s *Service) SetStashData(data []byte) error {
	s.mu.Lock()
	s.myStashData = data
	s.myStashTimestamp = time.Now().Unix()
	s.mu.Unlock()

	s.Log.Info("stash data updated (%d bytes)", len(data))

	// If fewer than target confidants, try to auto-select
	if len(s.confidants) < s.targetConfidants {
		s.Log.Info("only %d confidants configured (need %d), selecting automatically...", len(s.confidants), s.targetConfidants)
		// Clear old confidants before auto-selecting
		s.confidants = []types.NaraID{}
		if err := s.SelectConfidantsAutomatically(); err != nil {
			return fmt.Errorf("failed to auto-select confidants: %w", err)
		}
	}

	// Distribute to all configured confidants
	return s.DistributeToConfidants()
}

// GetStashData returns the current stash data.
func (s *Service) GetStashData() (data []byte, timestamp int64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.myStashData, s.myStashTimestamp
}

// HasStashData returns true if we have stash data configured.
func (s *Service) HasStashData() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.myStashData) > 0
}

// ClearMyStash clears the local stash data (used for testing restart scenarios).
func (s *Service) ClearMyStash() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.myStashData = nil
	s.myStashTimestamp = 0
}

// DistributeToConfidants distributes the current stash to all configured confidants.
func (s *Service) DistributeToConfidants() error {
	s.mu.RLock()
	data := s.myStashData
	confidants := make([]types.NaraID, len(s.confidants))
	copy(confidants, s.confidants)
	s.mu.RUnlock()

	if len(data) == 0 {
		return fmt.Errorf("no stash data to distribute")
	}

	if len(confidants) == 0 {
		return fmt.Errorf("no confidants configured")
	}

	if len(confidants) < s.targetConfidants {
		return fmt.Errorf("minimum %d confidants required, only have %d", s.targetConfidants, len(confidants))
	}

	// Distribute to each confidant
	var errors []string
	successCount := 0
	for _, confidantID := range confidants {
		if err := s.StoreWith(confidantID, data); err != nil {
			s.Log.Warn("failed to store with %s: %v", confidantID, err)
			errors = append(errors, fmt.Sprintf("%s: %v", confidantID, err))
		} else {
			successCount++
		}
	}

	if successCount == 0 {
		return fmt.Errorf("failed to distribute to any confidants: %v", errors)
	}

	s.Log.Info("distributed stash to %d/%d confidants", successCount, len(confidants))
	if len(errors) > 0 {
		s.Log.Warn("distribution errors: %v", errors)
	}

	return nil
}

// HasStashFor returns true if we're storing a stash for the given owner.
func (s *Service) HasStashFor(ownerID types.NaraID) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.stored[ownerID]
	return ok
}

// === Confidant API (for storing others' stashes) ===

// canStore checks if we can store a stash for the given owner.
// Returns true if:
// - We already have a stash for this owner (update is allowed)
// - We haven't hit the storage limit yet
func (s *Service) canStore(ownerID types.NaraID) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Always allow updates for existing owners
	if _, exists := s.stored[ownerID]; exists {
		return true
	}

	// Check if we have room for a new owner
	return len(s.stored) < s.StorageLimit()
}

func (s *Service) store(ownerID types.NaraID, nonce, ciphertext []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.stored[ownerID] = &EncryptedStash{
		OwnerID:    ownerID,
		Nonce:      nonce,
		Ciphertext: ciphertext,
		StoredAt:   time.Now(),
	}

	s.Log.Info("stored stash for %s (%d bytes)", ownerID, len(ciphertext))
}

func (s *Service) GetStoredStash(ownerID types.NaraID) *EncryptedStash {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stored[ownerID]
}

// StorageLimit returns the maximum number of stashes this nara can store for others.
// Based on memory mode: low=5, medium=20, high=50.
func (s *Service) StorageLimit() int {
	mode := s.RT.MemoryMode()
	switch mode {
	case "low":
		return 5
	case "medium":
		return 20
	case "high":
		return 50
	default:
		return 5 // Default to low
	}
}

// StoredCount returns the number of stashes currently stored for other naras.
func (s *Service) StoredCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.stored)
}

// MarshalState returns the service's state as JSON for debugging.
func (s *Service) MarshalState() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state := struct {
		Confidants       []types.NaraID                   `json:"confidants"`
		Stored           map[types.NaraID]*EncryptedStash `json:"stored"`
		MyStashData      []byte                           `json:"my_stash_data,omitempty"`
		MyStashTimestamp int64                            `json:"my_stash_timestamp,omitempty"`
	}{
		Confidants:       s.confidants,
		Stored:           s.stored,
		MyStashData:      s.myStashData,
		MyStashTimestamp: s.myStashTimestamp,
	}

	return json.Marshal(state)
}
