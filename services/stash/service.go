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
	confidants []string // List of nara IDs

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

	return nil
}

func (s *Service) Start() error {
	s.log.Info("stash service started")
	return nil
}

func (s *Service) Stop() error {
	s.cancel()
	s.log.Info("stash service stopped")
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
		Confidants []string                   `json:"confidants"`
		Stored     map[string]*EncryptedStash `json:"stored"`
	}{
		Confidants: s.confidants,
		Stored:     s.stored,
	}

	return json.Marshal(state)
}

// UnmarshalState loads the service's state from JSON.
func (s *Service) UnmarshalState(data []byte) error {
	var state struct {
		Confidants []string                   `json:"confidants"`
		Stored     map[string]*EncryptedStash `json:"stored"`
	}

	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.confidants = state.Confidants
	s.stored = state.Stored

	logrus.Infof("[stash] loaded state: %d confidants, %d stored stashes",
		len(s.confidants), len(s.stored))

	return nil
}
