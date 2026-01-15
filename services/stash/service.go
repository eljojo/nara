package stash

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/eljojo/nara/messages"
	"github.com/eljojo/nara/runtime"
	"github.com/eljojo/nara/utilities"

	"github.com/sirupsen/logrus"
)

// Service implements distributed encrypted storage (stash).
//
// Naras store their encrypted state with trusted peers (confidants) instead
// of on disk. Only the owner can decrypt, but confidants hold the ciphertext.
type Service struct {
	rt  runtime.RuntimeInterface
	log *runtime.ServiceLog

	// Encryption
	encryptor *utilities.Encryptor

	// Stored stashes (we're a confidant for these owners)
	mu     sync.RWMutex
	stored map[string]*EncryptedStash // ownerID -> stash

	// Our confidants (peers who hold our stash)
	confidants       []string // List of nara IDs
	targetConfidants int      // Target number of confidants (default: 3)

	// Our own stash data (to be encrypted and distributed to confidants)
	myStashData      []byte // Arbitrary JSON payload
	myStashTimestamp int64  // When it was last updated

	// Request/response correlation
	storeCorrelator   *utilities.Correlator[messages.StashStoreAck]
	requestCorrelator *utilities.Correlator[messages.StashResponsePayload]

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
}

// EncryptedStash is what we store for other naras.
type EncryptedStash struct {
	OwnerID    string
	Nonce      []byte
	Ciphertext []byte
	StoredAt   time.Time
}

// NewService creates a new stash service.
func NewService() *Service {
	return &Service{
		stored:            make(map[string]*EncryptedStash),
		confidants:        make([]string, 0),
		targetConfidants:  3, // Default: 3 confidants for redundancy
		storeCorrelator:   utilities.NewCorrelator[messages.StashStoreAck](30 * time.Second),
		requestCorrelator: utilities.NewCorrelator[messages.StashResponsePayload](30 * time.Second),
	}
}

// === Service interface ===

func (s *Service) Name() string {
	return "stash"
}

func (s *Service) Init(rt runtime.RuntimeInterface) error {
	s.rt = rt
	s.log = rt.Log("stash")

	// Get seed from keypair for encryption
	seed := make([]byte, 32) // TODO: Get from keypair in Phase 4
	s.encryptor = utilities.NewEncryptor(seed)

	s.ctx, s.cancel = context.WithCancel(context.Background())

	logrus.Info("📦 Stash service initialized successfully")
	return nil
}

func (s *Service) Start() error {
	s.log.Info("stash service started")
	return nil
}

func (s *Service) Stop() error {
	if s.cancel != nil {
		s.cancel()
	}
	if s.log != nil {
		s.log.Info("stash service stopped")
	}
	return nil
}

// === Public API ===

// StoreWith stores encrypted data with a confidant.
//
// This is a synchronous call that blocks until the confidant acknowledges
// receipt or the request times out.
func (s *Service) StoreWith(confidantID string, data []byte) error {
	// Encrypt the data
	nonce, ciphertext, err := s.encryptor.Seal(data)
	if err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}

	// Create store request
	msg := &runtime.Message{
		Kind:    "stash:store",
		Version: 1,
		ToID:    confidantID,
		Payload: &messages.StashStorePayload{
			OwnerID:    s.rt.MeID(),
			Owner:      s.rt.Me().Name,
			Nonce:      nonce,
			Ciphertext: ciphertext,
			Timestamp:  time.Now().Unix(),
		},
	}

	// Send and wait for ack
	result := <-s.storeCorrelator.Send(s.rt, msg)
	if result.Err != nil {
		return fmt.Errorf("store request: %w", result.Err)
	}

	if !result.Response.Success {
		return fmt.Errorf("store failed: %s", result.Response.Reason)
	}

	s.log.Info("stored stash with %s", confidantID)
	return nil
}

// RequestFrom requests stored data from a confidant.
//
// Returns the decrypted data if the confidant has it, or an error.
func (s *Service) RequestFrom(confidantID string) ([]byte, error) {
	// Create request
	msg := &runtime.Message{
		Kind:    "stash:request",
		Version: 1,
		ToID:    confidantID,
		Payload: &messages.StashRequestPayload{
			OwnerID:   s.rt.MeID(),
			RequestID: "", // Correlator will use msg.ID
		},
	}

	// Send and wait for response
	result := <-s.requestCorrelator.Send(s.rt, msg)
	if result.Err != nil {
		return nil, fmt.Errorf("request: %w", result.Err)
	}

	if !result.Response.Found {
		return nil, fmt.Errorf("confidant %s has no stash for us", confidantID)
	}

	// Decrypt
	plaintext, err := s.encryptor.Open(result.Response.Nonce, result.Response.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	s.log.Info("recovered stash from %s", confidantID)
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
		s.log.Warn("recovery from %s failed: %v", confidantID, err)
	}

	return nil, fmt.Errorf("no confidant had our stash")
}

// SetConfidants configures the list of confidants to use.
func (s *Service) SetConfidants(confidantIDs []string) {
	s.confidants = confidantIDs
	s.log.Info("configured %d confidants", len(confidantIDs))
}

// SelectConfidantsAutomatically picks 3 confidants automatically:
// - First: peer with highest uptime
// - Second and third: random peers
// Returns error if unable to find 3 willing peers.
func (s *Service) SelectConfidantsAutomatically() error {
	// Check if runtime has network info (might not be configured yet)
	if s.rt == nil {
		logrus.Error("📦 CRITICAL: Stash service runtime is nil! Init() was never called or failed silently.")
		logrus.Error("📦 This usually means initRuntime() or startRuntime() failed during startup.")
		logrus.Error("📦 Check logs for 'Failed to initialize runtime' or 'Failed to start runtime'.")
		return fmt.Errorf("runtime not initialized - check startup logs for initialization errors")
	}

	s.log.Info("selecting confidants automatically...")

	// Get list of online peers from runtime
	peers := s.rt.OnlinePeers()

	logrus.Infof("📦 Auto-selecting confidants: found %d online peers", len(peers))
	for i, peer := range peers {
		logrus.Infof("📦   Peer %d: %s (%s) uptime=%v", i+1, peer.Name, peer.ID, peer.Uptime)
	}

	if len(peers) < 3 {
		return fmt.Errorf("need at least 3 online peers, only found %d", len(peers))
	}

	s.log.Info("found %d online peers", len(peers))

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

	selected := make([]string, 0, 3)
	used := make(map[string]bool)

	// Try to get first confidant (highest uptime)
	for _, peer := range sortedPeers {
		if len(selected) >= 1 {
			break
		}

		// Skip if already used or is ourselves
		if used[peer.ID] || peer.ID == s.rt.MeID() {
			continue
		}

		// Try to store with this peer
		testData := []byte(fmt.Sprintf(`{"test":"probe","timestamp":%d}`, time.Now().Unix()))
		logrus.Infof("📦 Trying peer %s (%s) as first confidant (uptime: %v)...", peer.Name, peer.ID, peer.Uptime)
		if err := s.StoreWith(peer.ID, testData); err != nil {
			logrus.Warnf("📦   ❌ Peer %s declined: %v", peer.Name, err)
			s.log.Warn("peer %s (uptime: %s) declined: %v", peer.ID, peer.Uptime, err)
			continue
		}

		logrus.Infof("📦   ✅ Peer %s accepted!", peer.Name)
		s.log.Info("selected confidant 1/3: %s (uptime: %s)", peer.ID, peer.Uptime)
		selected = append(selected, peer.ID)
		used[peer.ID] = true
	}

	if len(selected) == 0 {
		return fmt.Errorf("no peer accepted to be first confidant")
	}

	// Shuffle remaining peers for random selection
	remainingPeers := make([]*runtime.PeerInfo, 0, len(peers)-1)
	for _, peer := range peers {
		if !used[peer.ID] && peer.ID != s.rt.MeID() {
			remainingPeers = append(remainingPeers, peer)
		}
	}

	// Try random peers for second and third confidants
	for len(selected) < 3 && len(remainingPeers) > 0 {
		// Pick random index
		idx := time.Now().UnixNano() % int64(len(remainingPeers))
		peer := remainingPeers[idx]

		// Try to store with this peer
		testData := []byte(fmt.Sprintf(`{"test":"probe","timestamp":%d}`, time.Now().Unix()))
		if err := s.StoreWith(peer.ID, testData); err != nil {
			s.log.Warn("peer %s declined: %v", peer.ID, err)
			// Remove from candidates and try next
			remainingPeers = append(remainingPeers[:idx], remainingPeers[idx+1:]...)
			continue
		}

		s.log.Info("selected confidant %d/3: %s", len(selected)+1, peer.ID)
		selected = append(selected, peer.ID)
		used[peer.ID] = true

		// Remove from candidates
		remainingPeers = append(remainingPeers[:idx], remainingPeers[idx+1:]...)
	}

	if len(selected) < 3 {
		return fmt.Errorf("only found %d willing confidants, need 3", len(selected))
	}

	// Store the selected confidants
	s.SetConfidants(selected)
	s.log.Info("automatically selected 3 confidants")

	return nil
}

// SetStashData updates the stash data and distributes it to all confidants.
// If no confidants are configured, it automatically selects 3 peers.
func (s *Service) SetStashData(data []byte) error {
	s.mu.Lock()
	s.myStashData = data
	s.myStashTimestamp = time.Now().Unix()
	s.mu.Unlock()

	s.log.Info("stash data updated (%d bytes)", len(data))

	// If fewer than 3 confidants, try to auto-select
	if len(s.confidants) < 3 {
		s.log.Info("only %d confidants configured (need 3), selecting automatically...", len(s.confidants))
		// Clear old confidants before auto-selecting
		s.confidants = []string{}
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

// DistributeToConfidants distributes the current stash to all configured confidants.
func (s *Service) DistributeToConfidants() error {
	s.mu.RLock()
	data := s.myStashData
	confidants := make([]string, len(s.confidants))
	copy(confidants, s.confidants)
	s.mu.RUnlock()

	if len(data) == 0 {
		return fmt.Errorf("no stash data to distribute")
	}

	if len(confidants) == 0 {
		return fmt.Errorf("no confidants configured")
	}

	if len(confidants) < 3 {
		return fmt.Errorf("minimum 3 confidants required, only have %d", len(confidants))
	}

	// Distribute to each confidant
	var errors []string
	successCount := 0
	for _, confidantID := range confidants {
		if err := s.StoreWith(confidantID, data); err != nil {
			s.log.Warn("failed to store with %s: %v", confidantID, err)
			errors = append(errors, fmt.Sprintf("%s: %v", confidantID, err))
		} else {
			successCount++
		}
	}

	if successCount == 0 {
		return fmt.Errorf("failed to distribute to any confidants: %v", errors)
	}

	s.log.Info("distributed stash to %d/%d confidants", successCount, len(confidants))
	if len(errors) > 0 {
		s.log.Warn("distribution errors: %v", errors)
	}

	return nil
}

// HasStashFor returns true if we're storing a stash for the given owner.
func (s *Service) HasStashFor(ownerID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.stored[ownerID]
	return ok
}

// === Confidant API (for storing others' stashes) ===

func (s *Service) store(ownerID string, nonce, ciphertext []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.stored[ownerID] = &EncryptedStash{
		OwnerID:    ownerID,
		Nonce:      nonce,
		Ciphertext: ciphertext,
		StoredAt:   time.Now(),
	}

	s.log.Info("stored stash for %s (%d bytes)", ownerID, len(ciphertext))
}

func (s *Service) retrieve(ownerID string) *EncryptedStash {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stored[ownerID]
}

// === State persistence ===

// MarshalState returns the service's state as JSON for persistence.
func (s *Service) MarshalState() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state := struct {
		Confidants       []string                   `json:"confidants"`
		Stored           map[string]*EncryptedStash `json:"stored"`
		MyStashData      []byte                     `json:"my_stash_data,omitempty"`
		MyStashTimestamp int64                      `json:"my_stash_timestamp,omitempty"`
	}{
		Confidants:       s.confidants,
		Stored:           s.stored,
		MyStashData:      s.myStashData,
		MyStashTimestamp: s.myStashTimestamp,
	}

	return json.Marshal(state)
}

// UnmarshalState loads the service's state from JSON.
func (s *Service) UnmarshalState(data []byte) error {
	var state struct {
		Confidants       []string                   `json:"confidants"`
		Stored           map[string]*EncryptedStash `json:"stored"`
		MyStashData      []byte                     `json:"my_stash_data,omitempty"`
		MyStashTimestamp int64                      `json:"my_stash_timestamp,omitempty"`
	}

	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.confidants = state.Confidants
	s.stored = state.Stored
	s.myStashData = state.MyStashData
	s.myStashTimestamp = state.MyStashTimestamp

	logrus.Infof("[stash] loaded state: %d confidants, %d stored stashes, my stash: %d bytes",
		len(s.confidants), len(s.stored), len(s.myStashData))

	return nil
}
