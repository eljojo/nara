package nara

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	// MaxStashSize is the maximum size of a stash payload (10KB)
	MaxStashSize = 10 * 1024
)

// StashData is what gets encrypted (timestamp inside for tamper-proofing)
// This stores arbitrary JSON data. Naras can use this to store any JSON they want,
// distributed to confidants for recovery after restarts.
type StashData struct {
	Timestamp int64           `json:"timestamp"` // When this stash was created
	Data      json.RawMessage `json:"data"`      // Arbitrary JSON payload (any valid JSON)
	Version   int             `json:"version"`   // Schema version for future compatibility
}

// Marshal serializes StashData to JSON
func (s *StashData) Marshal() ([]byte, error) {
	return json.Marshal(s)
}

// Unmarshal deserializes StashData from JSON
func (s *StashData) Unmarshal(data []byte) error {
	return json.Unmarshal(data, s)
}

// StashPayload is the encrypted blob stored by confidants
type StashPayload struct {
	Owner      string `json:"owner"`
	Nonce      []byte `json:"nonce"`      // 24-byte XChaCha20 nonce
	Ciphertext []byte `json:"ciphertext"` // Encrypted StashData
}

// Size returns the total size of the payload
func (p *StashPayload) Size() int {
	return len(p.Owner) + len(p.Nonce) + len(p.Ciphertext)
}

// StashStore is sent from owner to confidant to store stash
type StashStore struct {
	From      string       `json:"from"`
	Signature string       `json:"signature"` // Base64-encoded signature
	Payload   StashPayload `json:"payload"`
}

// Sign signs the message with the given keypair
func (s *StashStore) Sign(keypair NaraKeypair) {
	// Sign the payload owner + ciphertext
	data := s.dataToSign()
	sig := keypair.Sign(data)
	s.Signature = base64.StdEncoding.EncodeToString(sig)
}

// Verify verifies the message signature
func (s *StashStore) Verify(publicKey []byte) bool {
	sig, err := base64.StdEncoding.DecodeString(s.Signature)
	if err != nil {
		return false
	}
	data := s.dataToSign()
	return VerifySignature(publicKey, data, sig)
}

func (s *StashStore) dataToSign() []byte {
	// Include from + payload owner + ciphertext in signature
	return append([]byte(s.From+":"+s.Payload.Owner+":"), s.Payload.Ciphertext...)
}

// StashStoreAck is sent from confidant to owner to confirm storage
type StashStoreAck struct {
	From  string `json:"from"`
	Owner string `json:"owner"`
}

// StashRequest is broadcast by owner to ask "who has my stash?"
type StashRequest struct {
	From      string `json:"from"`
	Signature string `json:"signature"`
}

// Sign signs the request
func (s *StashRequest) Sign(keypair NaraKeypair) {
	data := []byte("stash_request:" + s.From)
	sig := keypair.Sign(data)
	s.Signature = base64.StdEncoding.EncodeToString(sig)
}

// Verify verifies the request signature
func (s *StashRequest) Verify(publicKey []byte) bool {
	sig, err := base64.StdEncoding.DecodeString(s.Signature)
	if err != nil {
		return false
	}
	data := []byte("stash_request:" + s.From)
	return VerifySignature(publicKey, data, sig)
}

// StashResponse is sent from confidant to owner with the stored stash
type StashResponse struct {
	From    string       `json:"from"`
	Owner   string       `json:"owner"`
	Payload StashPayload `json:"payload"`
}

// POST /stash/store - Owner stores stash with confidant
// Note: Authentication is handled by meshAuthMiddleware (X-Nara-* headers).
// The From field is only used for logging/debugging - handlers use getVerifiedSender().
type StashStoreRequest struct {
	Stash *StashPayload `json:"stash"`
}

type StashStoreResponse struct {
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason,omitempty"` // Why rejected
}

// POST /stash/retrieve - Owner requests stash back from confidant
// Note: Authentication is handled by meshAuthMiddleware (X-Nara-* headers).
// No request body needed - sender identity comes from mesh auth.
type StashRetrieveRequest struct {
	// Empty - sender identity from mesh auth
}

type StashRetrieveResponse struct {
	Found bool          `json:"found"`
	Stash *StashPayload `json:"stash,omitempty"`
}

// POST /stash/push - Confidant pushes stash to owner (hey-there recovery)
// Note: Authentication is handled by meshAuthMiddleware (X-Nara-* headers).
// Sender identity comes from mesh auth, not from request body.
type StashPushRequest struct {
	To    string        `json:"to"`
	Stash *StashPayload `json:"stash"`
}

type StashPushResponse struct {
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason,omitempty"`
}

// DELETE /stash/store - Owner requests confidant to delete stash
// Note: Authentication is handled by meshAuthMiddleware (X-Nara-* headers).
// No request body needed - sender identity comes from mesh auth.
type StashDeleteRequest struct {
	// Empty - sender identity from mesh auth
}

type StashDeleteResponse struct {
	Deleted bool `json:"deleted"`
}

// Timestamp validation for replay protection
const StashTimestampToleranceSeconds = 30 // 30 seconds (assumes NTP sync)

// ValidateTimestamp checks if a timestamp is within acceptable range (not too old, not in future)
func ValidateTimestamp(ts int64) bool {
	now := time.Now().Unix()
	diff := now - ts
	// Reject if timestamp is in the future (allow 30s clock skew, assumes NTP sync)
	if diff < -30 {
		return false
	}
	// Reject if timestamp is too old (replay protection)
	if diff > StashTimestampToleranceSeconds {
		return false
	}
	return true
}

// ConfidantTracker tracks which confidants have our stash (owner side)
type ConfidantTracker struct {
	confidants  map[string]int64 // name -> timestamp they confirmed (acked)
	pending     map[string]int64 // name -> timestamp we sent (awaiting ack)
	failed      map[string]int64 // name -> timestamp of last failure (timeout, reject, etc)
	targetCount int
	mu          sync.RWMutex
}

// PendingTimeout is how long to wait for an ack before giving up
const PendingTimeout = 60 * time.Second

// FailureBackoffTime is how long to wait before retrying a failed confidant
const FailureBackoffTime = 5 * time.Minute

// NewConfidantTracker creates a new tracker with the given target count
func NewConfidantTracker(targetCount int) *ConfidantTracker {
	return &ConfidantTracker{
		confidants:  make(map[string]int64),
		pending:     make(map[string]int64),
		failed:      make(map[string]int64),
		targetCount: targetCount,
	}
}

// Add adds a confidant with the given confirmation timestamp (for acked confidants)
func (t *ConfidantTracker) Add(name string, timestamp int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.confidants[name] = timestamp
	delete(t.pending, name) // Remove from pending if it was there
	delete(t.failed, name)  // Clear any previous failure
}

// AddPending marks a confidant as pending (sent but not yet acked)
func (t *ConfidantTracker) AddPending(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, confirmed := t.confidants[name]; !confirmed {
		t.pending[name] = time.Now().Unix()
	}
}

// CleanupExpiredPending removes pending confidants that have timed out
func (t *ConfidantTracker) CleanupExpiredPending() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now().Unix()
	cutoff := now - int64(PendingTimeout.Seconds())
	var expired []string
	for name, sentAt := range t.pending {
		if sentAt < cutoff {
			expired = append(expired, name)
			delete(t.pending, name)
		}
	}
	return expired
}

// MarkFailed marks a confidant as failed (timeout, rejection, etc)
func (t *ConfidantTracker) MarkFailed(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failed[name] = time.Now().Unix()
	delete(t.pending, name) // Also remove from pending
}

// CleanupExpiredFailures removes failed confidants after backoff period expires
func (t *ConfidantTracker) CleanupExpiredFailures() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now().Unix()
	cutoff := now - int64(FailureBackoffTime.Seconds())
	var recovered []string
	for name, failedAt := range t.failed {
		if failedAt < cutoff {
			recovered = append(recovered, name)
			delete(t.failed, name)
		}
	}
	return recovered
}

// Remove removes a confident
func (t *ConfidantTracker) Remove(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.confidants, name)
	delete(t.pending, name)
	delete(t.failed, name)
}

// Has returns true if the given name is a confirmed confident
func (t *ConfidantTracker) Has(name string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	_, ok := t.confidants[name]
	return ok
}

// IsPending returns true if the given name is pending (sent but not acked)
func (t *ConfidantTracker) IsPending(name string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	_, ok := t.pending[name]
	return ok
}

// IsFailed returns true if the given name is marked as failed
func (t *ConfidantTracker) IsFailed(name string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	_, ok := t.failed[name]
	return ok
}

// Count returns the number of tracked confidants
func (t *ConfidantTracker) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.confidants)
}

// IsSatisfied returns true if we have at least targetCount confidants
func (t *ConfidantTracker) IsSatisfied() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.confidants) >= t.targetCount
}

// NeedsMore returns true if we need more confidants
func (t *ConfidantTracker) NeedsMore() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.confidants) < t.targetCount
}

// NeedsCount returns how many more confidants are needed
func (t *ConfidantTracker) NeedsCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	needed := t.targetCount - len(t.confidants)
	if needed < 0 {
		return 0
	}
	return needed
}

// GetExcess returns confidants beyond the target count (oldest first)
func (t *ConfidantTracker) GetExcess() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if len(t.confidants) <= t.targetCount {
		return nil
	}

	// Sort by timestamp (oldest first)
	type entry struct {
		name string
		ts   int64
	}
	entries := make([]entry, 0, len(t.confidants))
	for name, ts := range t.confidants {
		entries = append(entries, entry{name, ts})
	}
	// Sort ascending by timestamp
	for i := 0; i < len(entries)-1; i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].ts < entries[i].ts {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

	// Return oldest entries beyond target
	excess := make([]string, len(entries)-t.targetCount)
	for i := 0; i < len(excess); i++ {
		excess[i] = entries[i].name
	}
	return excess
}

// PeerInfo holds metadata about a peer for confidant selection
type PeerInfo struct {
	Name       string
	MemoryMode MemoryMode
	UptimeSecs int64
}

// SelectRandom selects a random confidant from available peers (by name)
// Excludes self, existing confidants, pending, and failed peers
func (t *ConfidantTracker) SelectRandom(self string, online []string) string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// Build candidate list
	candidates := make([]string, 0)
	for _, name := range online {
		if name == self {
			continue
		}
		if _, exists := t.confidants[name]; exists {
			continue
		}
		if _, pending := t.pending[name]; pending {
			continue
		}
		if _, failed := t.failed[name]; failed {
			continue
		}
		candidates = append(candidates, name)
	}

	if len(candidates) == 0 {
		return ""
	}

	return candidates[rand.Intn(len(candidates))]
}

// SelectRandomFromPeers is a wrapper around SelectRandom for PeerInfo slices
// Used in confidant selection to pick random peers for better distribution
func (t *ConfidantTracker) SelectRandomFromPeers(self string, peers []PeerInfo) string {
	names := make([]string, len(peers))
	for i, p := range peers {
		names[i] = p.Name
	}
	return t.SelectRandom(self, names)
}

// SelectBest selects the best confidant from available peers based on memory mode and uptime
// Scoring: Hog=300, Medium=200, Short=100, plus uptime seconds, plus random jitter for tiebreaking
func (t *ConfidantTracker) SelectBest(self string, peers []PeerInfo) string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	type candidate struct {
		name  string
		score float64
	}

	candidates := make([]candidate, 0)
	for _, peer := range peers {
		if peer.Name == self {
			continue
		}
		if _, exists := t.confidants[peer.Name]; exists {
			continue
		}
		if _, pending := t.pending[peer.Name]; pending {
			continue
		}
		if _, failed := t.failed[peer.Name]; failed {
			continue // Skip recently failed confidants
		}

		// Calculate score
		score := 0.0

		// Memory mode score
		switch peer.MemoryMode {
		case MemoryModeHog:
			score += 300
		case MemoryModeMedium:
			score += 200
		case MemoryModeShort:
			score += 100
		default:
			score += 200 // Default to medium
		}

		// Uptime score (seconds)
		score += float64(peer.UptimeSecs)

		// Random jitter for tiebreaking (0-10)
		score += rand.Float64() * 10

		candidates = append(candidates, candidate{peer.Name, score})
	}

	if len(candidates) == 0 {
		return ""
	}

	// Find highest score
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.score > best.score {
			best = c
		}
	}

	return best.name
}

// MarkOffline removes confidants that are no longer online
func (t *ConfidantTracker) MarkOffline(name string, online []string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Check if the confidant is still online
	found := false
	for _, n := range online {
		if n == name {
			found = true
			break
		}
	}

	if !found {
		delete(t.confidants, name)
	}
}

// GetAll returns all confidant names
func (t *ConfidantTracker) GetAll() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	names := make([]string, 0, len(t.confidants))
	for name := range t.confidants {
		names = append(names, name)
	}
	return names
}

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
	shouldStartFresh  bool
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

// gzipCompress compresses data using gzip
func gzipCompress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	gzWriter := gzip.NewWriter(&buf)
	if _, err := gzWriter.Write(data); err != nil {
		gzWriter.Close()
		return nil, err
	}
	if err := gzWriter.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// gzipDecompress decompresses gzip data
func gzipDecompress(data []byte) ([]byte, error) {
	reader := bytes.NewReader(data)
	gzReader, err := gzip.NewReader(reader)
	if err != nil {
		return nil, err
	}
	defer gzReader.Close()

	decompressed, err := io.ReadAll(gzReader)
	if err != nil {
		return nil, err
	}
	return decompressed, nil
}

// CreateStashPayload creates an encrypted stash payload
func CreateStashPayload(owner string, data *StashData, encKeypair EncryptionKeypair) (*StashPayload, error) {
	// Serialize
	serialized, err := data.Marshal()
	if err != nil {
		return nil, err
	}

	// Gzip compress before encryption
	compressed, err := gzipCompress(serialized)
	if err != nil {
		return nil, err
	}

	// Encrypt the compressed data
	nonce, ciphertext, err := encKeypair.EncryptForSelf(compressed)
	if err != nil {
		return nil, err
	}

	return &StashPayload{
		Owner:      owner,
		Nonce:      nonce,
		Ciphertext: ciphertext,
	}, nil
}

// DecryptStashPayload decrypts a payload and returns the full StashData
func DecryptStashPayload(payload *StashPayload, encKeypair EncryptionKeypair) (*StashData, error) {
	// Decrypt the ciphertext (still compressed)
	compressed, err := encKeypair.DecryptForSelf(payload.Nonce, payload.Ciphertext)
	if err != nil {
		return nil, err
	}

	// Gzip decompress after decryption
	decompressed, err := gzipDecompress(compressed)
	if err != nil {
		return nil, err
	}

	// Unmarshal the decompressed JSON
	data := &StashData{}
	if err := data.Unmarshal(decompressed); err != nil {
		return nil, err
	}

	return data, nil
}

// ExtractStashTimestamp decrypts a payload and returns the timestamp
func ExtractStashTimestamp(payload *StashPayload, encKeypair EncryptionKeypair) (int64, error) {
	data, err := DecryptStashPayload(payload, encKeypair)
	if err != nil {
		return 0, err
	}
	return data.Timestamp, nil
}

// SetMaxStashes sets the maximum number of stashes to store (memory limit)
func (s *ConfidantStashStore) SetMaxStashes(max int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxStashes = max
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

// StashMetrics holds metrics about stash storage
type StashMetrics struct {
	StashesStored        int   // Number of stashes stored for others
	TotalStashBytes      int64 // Total bytes of stash data stored
	MyStashSize          int   // Size of my own stash
	ConfidantCount       int   // Number of confidants storing my stash
	TargetConfidantCount int   // Target (usually 2-3)
	EvictionCount        int   // Number of ghost prunings (stashes removed when owner offline 7+ days)
	StorageLimit         int   // Maximum number of stashes we can store
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

// Memory-aware stash storage limits
const (
	StashStorageShort  = 5  // Store max 5 stashes in short mode
	StashStorageMedium = 20 // Store max 20 stashes in medium mode
	StashStorageHog    = 50 // Store max 50 stashes in hog mode
)

// StashStorageForMemoryMode returns the appropriate stash storage limit for a memory mode
func StashStorageForMemoryMode(mode MemoryMode) int {
	switch mode {
	case MemoryModeShort:
		return StashStorageShort
	case MemoryModeHog:
		return StashStorageHog
	default:
		return StashStorageMedium
	}
}

// =============================================================================
// StashService - Unified stash business logic layer
// =============================================================================

// StashServiceDeps extends NetworkContext with stash-specific functionality.
// This interface allows the service to be decoupled from Network while still
// accessing necessary functionality.
type StashServiceDeps interface {
	NetworkContext

	// Stash-specific: emit social events for stash operations
	EmitSocialEvent(event SyncEvent)
}

// StashService encapsulates all stash business logic for both owner and confidant roles.
// It provides a clean API for HTTP handlers and network operations to use.
//
// Architecture:
//
//	StashService (orchestration layer - all business logic)
//	├── StashManager        (owner side: MY stash → distributed to confidants)
//	├── ConfidantStashStore (confidant side: OTHERS' stashes → stored by me)
//	└── StashSyncTracker    (rate limiting sync operations)
//
// - StashManager: Manages YOUR OWN stash data and tracks which confidants hold copies.
// - ConfidantStashStore: Stores OTHER naras' stashes when you act as their confidant.
// - StashService: Wraps both and provides all operations (accept, retrieve, distribute, etc.)
type StashService struct {
	// Owner-side: manages OUR stash that we distribute to confidants
	manager *StashManager

	// Confidant-side: stores OTHER naras' stashes (we're their confidant)
	confidantStore *ConfidantStashStore

	// Rate limiting for stash sync operations
	syncTracker *StashSyncTracker

	// External dependencies
	deps StashServiceDeps
}

// NewStashService creates a new StashService with the given components.
// Pass nil for components that aren't needed (e.g., nil manager if not using stash).
func NewStashService(
	manager *StashManager,
	confidantStore *ConfidantStashStore,
	syncTracker *StashSyncTracker,
	deps StashServiceDeps,
) *StashService {
	return &StashService{
		manager:        manager,
		confidantStore: confidantStore,
		syncTracker:    syncTracker,
		deps:           deps,
	}
}

// =============================================================================
// Confidant-side operations (storing stashes for others)
// =============================================================================

// AcceptStash handles an incoming stash store request from an owner.
// Returns whether the stash was accepted and a reason if rejected.
func (s *StashService) AcceptStash(owner string, stash *StashPayload) (accepted bool, reason string) {
	if s.confidantStore == nil {
		return false, "stash_disabled"
	}

	if stash == nil {
		return false, "stash_required"
	}

	if stash.Size() > MaxStashSize {
		logrus.Warnf("📦 Rejected stash from %s: too large (%d bytes)", owner, stash.Size())
		return false, "stash_too_large"
	}

	// Try to store (returns false if at capacity)
	if !s.confidantStore.Store(owner, stash) {
		metrics := s.confidantStore.GetMetrics()
		logrus.Warnf("📦 Rejected stash from %s: at capacity (%d/%d)",
			owner, metrics.StashesStored, s.confidantStore.maxStashes)
		return false, "at_capacity"
	}

	logrus.Infof("📦 Accepted stash for %s (%d bytes)", owner, stash.Size())

	// Emit social event for storing stash
	if s.deps != nil {
		socialEvent := SocialEventPayload{
			Type:    "service",
			Actor:   s.deps.MyName(),
			Target:  owner,
			Reason:  ReasonStashStored,
			Witness: s.deps.MyName(),
		}
		syncEvent := SyncEvent{
			Service:   ServiceSocial,
			Timestamp: time.Now().UnixNano(),
			Emitter:   s.deps.MyName(),
			Social:    &socialEvent,
		}
		s.deps.EmitSocialEvent(syncEvent)
	}

	return true, ""
}

// RetrieveStashFor returns the stored stash for the given owner, or nil if not found.
func (s *StashService) RetrieveStashFor(owner string) *StashPayload {
	if s.confidantStore == nil {
		return nil
	}

	s.confidantStore.mu.RLock()
	payload := s.confidantStore.stashes[owner]
	s.confidantStore.mu.RUnlock()

	if payload != nil {
		logrus.Infof("📦 Sent stash to %s (%d bytes)", owner, payload.Size())
	}

	return payload
}

// DeleteStashFor removes the stored stash for the given owner.
// Returns true if a stash was deleted, false if none existed.
func (s *StashService) DeleteStashFor(owner string) bool {
	if s.confidantStore == nil {
		return false
	}

	deleted := s.confidantStore.Delete(owner)
	if deleted {
		logrus.Infof("📦 Deleted stash for %s", owner)
	}

	return deleted
}

// HasStashFor returns true if we have a stash stored for the given owner.
func (s *StashService) HasStashFor(owner string) bool {
	if s.confidantStore == nil {
		return false
	}
	return s.confidantStore.HasStashFor(owner)
}

// AcceptPushedStash handles receiving a stash push during recovery.
// It decrypts the stash, stores it locally, and marks the sender as a confidant.
func (s *StashService) AcceptPushedStash(sender string, stash *StashPayload) (accepted bool, reason string) {
	if s.manager == nil {
		return false, "stash_disabled"
	}

	if stash == nil {
		return false, "stash_required"
	}

	// Decrypt the stash
	encKeypair := DeriveEncryptionKeys(s.deps.Keypair().PrivateKey)
	stashData, err := DecryptStashPayload(stash, encKeypair)
	if err != nil {
		logrus.Warnf("📦 Failed to decrypt pushed stash from %s: %v", sender, err)
		return false, "decrypt_failed"
	}

	// Store recovered stash
	s.manager.SetCurrentStash(stashData.Data)
	logrus.Infof("📦 Recovered stash from %s via push (%d bytes, timestamp=%d)",
		sender, len(stashData.Data), stashData.Timestamp)

	// Mark them as a confidant
	if s.manager.confidantTracker != nil {
		s.manager.confidantTracker.Add(sender, time.Now().Unix())
	}

	return true, ""
}

// =============================================================================
// Owner-side operations (managing our stash distribution)
// =============================================================================

// SetCurrentStash updates the owner's current stash data.
func (s *StashService) SetCurrentStash(data json.RawMessage) {
	if s.manager != nil {
		s.manager.SetCurrentStash(data)
	}
}

// GetCurrentStash returns the owner's current stash data.
func (s *StashService) GetCurrentStash() *StashData {
	if s.manager == nil {
		return nil
	}
	return s.manager.GetCurrentStash()
}

// HasStashData returns true if the owner has stash data to distribute.
func (s *StashService) HasStashData() bool {
	return s.manager != nil && s.manager.HasStashData()
}

// GetConfidants returns the list of confirmed confidant names.
func (s *StashService) GetConfidants() []string {
	if s.manager == nil || s.manager.confidantTracker == nil {
		return nil
	}
	return s.manager.confidantTracker.GetAll()
}

// ConfidantCount returns the number of confirmed confidants.
func (s *StashService) ConfidantCount() int {
	if s.manager == nil || s.manager.confidantTracker == nil {
		return 0
	}
	return s.manager.confidantTracker.Count()
}

// TargetConfidantCount returns the target number of confidants.
func (s *StashService) TargetConfidantCount() int {
	if s.manager == nil || s.manager.confidantTracker == nil {
		return 0
	}
	return s.manager.confidantTracker.targetCount
}

// MarkConfidantConfirmed marks a confidant as having confirmed storage of our stash.
func (s *StashService) MarkConfidantConfirmed(name string, timestamp int64) {
	if s.manager != nil && s.manager.confidantTracker != nil {
		s.manager.confidantTracker.Add(name, timestamp)
	}
}

// MarkConfidantFailed marks a confidant as having failed (timeout, rejection, etc).
func (s *StashService) MarkConfidantFailed(name string) {
	if s.manager != nil && s.manager.confidantTracker != nil {
		s.manager.confidantTracker.MarkFailed(name)
	}
}

// =============================================================================
// Metrics and status
// =============================================================================

// GetStorageMetrics returns metrics about stash storage (confidant role).
func (s *StashService) GetStorageMetrics() StashMetrics {
	if s.confidantStore == nil {
		return StashMetrics{}
	}
	return s.confidantStore.GetMetrics()
}

// GetAllStoredOwners returns the list of owners we're storing stashes for.
func (s *StashService) GetAllStoredOwners() []string {
	if s.confidantStore == nil {
		return nil
	}
	return s.confidantStore.GetAllOwners()
}

// =============================================================================
// Owner-side HTTP operations (distributing stash to confidants)
// =============================================================================

// ExchangeStashWithPeer performs bidirectional stash exchange with a peer.
// - If we have stash data and need storage, we store with them
// - If we don't have stash data, we try to retrieve from them
func (s *StashService) ExchangeStashWithPeer(targetName string) {
	// Check rate limiting - don't sync more than once per 5 minutes
	if s.syncTracker != nil && !s.syncTracker.ShouldSync(targetName, 5*time.Minute) {
		return
	}

	if s.manager == nil || s.manager.confidantTracker == nil {
		return
	}

	// First, try to store our stash with them if needed
	needsStorage := s.manager.confidantTracker.Has(targetName) || s.manager.confidantTracker.NeedsMore()
	if needsStorage && s.manager.HasStashData() && !s.manager.confidantTracker.IsFailed(targetName) {
		s.StoreStashWithPeer(targetName)
	}

	// Second, try to retrieve our stash if we don't have it
	if !s.manager.HasStashData() {
		s.RetrieveStashFromPeer(targetName)
	}

	// Mark sync timestamp
	if s.syncTracker != nil {
		s.syncTracker.MarkSyncNow(targetName)
	}
}

// StoreStashWithPeer sends our stash to a peer for storage.
func (s *StashService) StoreStashWithPeer(targetName string) {
	if s.manager == nil || s.deps == nil {
		return
	}

	// Determine base URL
	baseURL := s.deps.BuildMeshURL(targetName, "")
	if baseURL == "" {
		return
	}

	currentStash := s.manager.GetCurrentStash()
	if currentStash == nil {
		return
	}

	encKeypair := DeriveEncryptionKeys(s.deps.Keypair().PrivateKey)
	payload, err := CreateStashPayload(s.deps.MyName(), currentStash, encKeypair)
	if err != nil {
		logrus.Debugf("📦 Failed to create stash payload: %v", err)
		return
	}

	// Build request (auth is in mesh headers, not body)
	req := StashStoreRequest{
		Stash: payload,
	}

	reqBytes, _ := json.Marshal(req)
	ctx, cancel := context.WithTimeout(s.deps.Context(), 10*time.Second)
	defer cancel()

	httpReq, _ := http.NewRequestWithContext(ctx, "POST", baseURL+"/stash/store", bytes.NewBuffer(reqBytes))
	httpReq.Header.Set("Content-Type", "application/json")
	s.deps.AddMeshAuthHeaders(httpReq)

	client := s.deps.GetMeshHTTPClient()
	if client == nil {
		return
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		logrus.Debugf("📦 Failed to store stash with %s: %v", targetName, err)
		s.manager.confidantTracker.MarkFailed(targetName)
		return
	}
	defer resp.Body.Close()

	var response StashStoreResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err == nil && response.Accepted {
		s.manager.confidantTracker.Add(targetName, time.Now().Unix())
		logrus.Infof("📦 %s accepted stash", targetName)
	} else if !response.Accepted {
		logrus.Warnf("📦 %s rejected stash: %s", targetName, response.Reason)
		s.manager.confidantTracker.MarkFailed(targetName)
	}
}

// RetrieveStashFromPeer requests our stash back from a peer.
func (s *StashService) RetrieveStashFromPeer(targetName string) {
	if s.manager == nil || s.deps == nil {
		return
	}

	// Determine base URL
	baseURL := s.deps.BuildMeshURL(targetName, "")
	if baseURL == "" {
		return
	}

	// Auth is in mesh headers, body can be empty
	ctx, cancel := context.WithTimeout(s.deps.Context(), 10*time.Second)
	defer cancel()

	httpReq, _ := http.NewRequestWithContext(ctx, "POST", baseURL+"/stash/retrieve", nil)
	httpReq.Header.Set("Content-Type", "application/json")
	s.deps.AddMeshAuthHeaders(httpReq)

	client := s.deps.GetMeshHTTPClient()
	if client == nil {
		return
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		logrus.Debugf("📦 Failed to retrieve stash from %s: %v", targetName, err)
		return
	}
	defer resp.Body.Close()

	var response StashRetrieveResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err == nil && response.Found {
		encKeypair := DeriveEncryptionKeys(s.deps.Keypair().PrivateKey)
		stashData, err := DecryptStashPayload(response.Stash, encKeypair)
		if err == nil {
			s.manager.SetCurrentStash(stashData.Data)
			s.manager.confidantTracker.Add(targetName, time.Now().Unix())
			logrus.Infof("📦 Recovered stash from %s (%d bytes)", targetName, len(stashData.Data))
		}
	}
}

// PushStashToOwner pushes a stash back to its owner via HTTP POST to /stash/push.
// This is called during hey-there recovery or stash-refresh requests.
// Runs the push in a background goroutine with optional delay.
func (s *StashService) PushStashToOwner(targetName string, delay time.Duration) {
	go func() {
		// Optional delay to let them finish booting or avoid thundering herd
		if delay > 0 {
			time.Sleep(delay)
		}

		// Check if we still have their stash
		if s.confidantStore == nil || s.deps == nil {
			return
		}

		s.confidantStore.mu.RLock()
		theirStash := s.confidantStore.stashes[targetName]
		s.confidantStore.mu.RUnlock()

		if theirStash == nil {
			logrus.Debugf("📦 No stash found for %s during push", targetName)
			return
		}

		// Build push request (auth is in mesh headers, not body)
		req := StashPushRequest{
			To:    targetName,
			Stash: theirStash,
		}

		// Determine URL
		url := s.deps.BuildMeshURL(targetName, "/stash/push")
		if url == "" {
			logrus.Debugf("📦 Cannot push stash to %s: not reachable via mesh", targetName)
			return
		}

		// Send push request
		reqBytes, _ := json.Marshal(req)
		ctx, cancel := context.WithTimeout(s.deps.Context(), 10*time.Second)
		defer cancel()

		httpReq, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBytes))
		httpReq.Header.Set("Content-Type", "application/json")
		s.deps.AddMeshAuthHeaders(httpReq)

		client := s.deps.GetMeshHTTPClient()
		if client == nil {
			return
		}

		resp, err := client.Do(httpReq)
		if err != nil {
			logrus.Debugf("📦 Failed to push stash to %s: %v", targetName, err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			logrus.Infof("📦 Pushed stash to %s (recovery)", targetName)
		}
	}()
}

// =============================================================================
// Maintenance operations
// =============================================================================

// MaintainConfidants ensures we have the target number of confidants for our stash.
// It cleans up expired pending/failed entries and actively searches for new confidants if needed.
func (s *StashService) MaintainConfidants() {
	if s.manager == nil || s.deps == nil || s.deps.IsReadOnly() {
		return
	}

	// Only maintain confidants if we have data to store
	if !s.manager.HasStashData() {
		return
	}

	tracker := s.manager.confidantTracker

	// Cleanup expired pending (didn't ack in time - probably old version or offline)
	expired := tracker.CleanupExpiredPending()
	for _, name := range expired {
		logrus.Debugf("📦 %s didn't ack stash (timeout), removed from pending", name)
	}

	// Cleanup expired failures (allow retrying after backoff period)
	recovered := tracker.CleanupExpiredFailures()
	for _, name := range recovered {
		logrus.Debugf("📦 %s failure backoff expired, can retry", name)
	}

	// If we have stash data but less than target confidants, actively try to find more
	if tracker.NeedsMore() {
		needed := tracker.NeedsCount()
		logrus.Debugf("📦 Need %d more confidants (have %d/%d), searching for candidates",
			needed, tracker.Count(), tracker.targetCount)

		// Get online naras
		online := s.deps.GetOnlineMeshPeers()

		// Filter to mesh-enabled only
		var meshEnabled []string
		for _, name := range online {
			if s.deps.HasMeshConnectivity(name) {
				meshEnabled = append(meshEnabled, name)
			}
		}

		if len(meshEnabled) == 0 {
			return
		}

		// Get peer info for selection
		peers := s.deps.GetPeerInfo(meshEnabled)
		if len(peers) == 0 {
			return
		}

		// Try to find and store with confidants
		// Strategy: First confidant uses best score (reliable high-memory nara)
		//           Remaining confidants are random (better distribution, avoid hotspots)
		// Keep trying peers until we fill all slots OR run out of candidates
		attemptCount := 0
		for tracker.NeedsMore() && len(peers) > 0 && attemptCount < 20 {
			attemptCount++
			var selectedName string
			var selectionMethod string

			currentCount := tracker.Count()
			if currentCount == 0 {
				// First confidant: pick the best (highest memory + uptime)
				selectedName = tracker.SelectBest(s.deps.MyName(), peers)
				selectionMethod = "best"
			} else {
				// Remaining confidants: pick randomly (distribute load)
				selectedName = tracker.SelectRandomFromPeers(s.deps.MyName(), peers)
				selectionMethod = "random"
			}

			if selectedName == "" {
				break // No more candidates
			}

			logrus.Debugf("📦 Selected %s as confidant candidate (%s, attempt %d)", selectedName, selectionMethod, attemptCount)

			// Try to store stash with this peer
			// StoreStashWithPeer handles marking as pending/failed
			s.StoreStashWithPeer(selectedName)

			// Remove this peer from consideration (avoid retrying in same round)
			newPeers := make([]PeerInfo, 0, len(peers)-1)
			for _, p := range peers {
				if p.Name != selectedName {
					newPeers = append(newPeers, p)
				}
			}
			peers = newPeers
		}
	}
}

// CheckConfidantHealth monitors the health of our confidants and removes offline ones.
// Replacements will be found by MaintainConfidants in the next tick.
func (s *StashService) CheckConfidantHealth() {
	if s.manager == nil || s.manager.confidantTracker == nil || s.deps == nil {
		return
	}

	// Get current confirmed confidants
	confidants := s.manager.confidantTracker.GetAll()
	if len(confidants) == 0 {
		return
	}

	// Ensure projection is up-to-date before checking statuses
	s.deps.EnsureProjectionsUpdated()

	// Check each confidant's status
	for _, confidantName := range confidants {
		state := s.deps.GetOnlineStatus(confidantName)
		if state == nil {
			// Never seen - remove them
			s.manager.confidantTracker.Remove(confidantName)
			logrus.Warnf("📦 Confidant %s removed (never seen)", confidantName)
			continue
		}

		// If offline or missing, remove them
		if state.Status == "OFFLINE" || state.Status == "MISSING" {
			s.manager.confidantTracker.Remove(confidantName)
			logrus.Warnf("📦 Confidant %s went offline, will find replacement", confidantName)
		}
	}

	// MaintainConfidants() will automatically find replacements in the next tick
}

// ReactToConfidantOffline is called immediately when we detect a nara went offline.
// If they're one of our confidants, remove them and trigger immediate replacement search.
func (s *StashService) ReactToConfidantOffline(name string) {
	if s.manager == nil || s.manager.confidantTracker == nil {
		return
	}

	// Check if this nara is one of our confidants
	if !s.manager.confidantTracker.Has(name) {
		return
	}

	// Remove them immediately
	s.manager.confidantTracker.Remove(name)
	logrus.Warnf("📦 Confidant %s went offline (reactive), finding replacement immediately", name)

	// Trigger immediate replacement search
	go s.MaintainConfidants()
}

// PruneGhostStashes removes stashes of owners who have been offline for 7+ days.
func (s *StashService) PruneGhostStashes() {
	if s.confidantStore == nil || s.deps == nil {
		return
	}

	// Check all stash owners
	owners := s.confidantStore.GetAllOwners()
	if len(owners) == 0 {
		return
	}

	// Ensure projection is up-to-date before checking statuses
	s.deps.EnsureProjectionsUpdated()

	isGhost := func(name string) bool {
		// Check online status projection
		state := s.deps.GetOnlineStatus(name)
		if state == nil {
			// No state = never seen = ghost
			return true
		}

		// If online, definitely not a ghost
		if state.Status == "ONLINE" {
			return false
		}

		// If offline, check how long
		lastSeen := state.LastEventTime
		elapsedNanos := time.Now().UnixNano() - lastSeen
		elapsed := time.Duration(elapsedNanos)

		// Ghost if offline for more than 7 days
		return elapsed > 7*24*time.Hour
	}

	evicted := s.confidantStore.EvictGhosts(isGhost)
	if evicted > 0 {
		logrus.Infof("📦 Pruned %d ghost stashes (offline 7+ days)", evicted)
	}
}

// =============================================================================
// Maintenance loop
// =============================================================================

// RunMaintenanceLoop runs the stash maintenance loop.
// It should be called as a goroutine. The loop handles:
// - Immediate stash distribution when triggered
// - Periodic confidant health checks
// - Periodic ghost stash pruning
// - Periodic metrics logging
func (s *StashService) RunMaintenanceLoop(trigger <-chan struct{}) {
	if s.deps == nil {
		return
	}

	// Prune ghost stashes on boot (naras offline >7 days)
	s.PruneGhostStashes()

	// Run every 5 minutes
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-trigger:
			// Immediate stash distribution triggered (e.g., stash data updated)
			logrus.Debugf("📦 Triggered immediate stash distribution")
			s.MaintainConfidants()
		case <-ticker.C:
			// Periodic maintenance
			// Check health of our confidants and find replacements if needed
			s.CheckConfidantHealth()

			// Maintain target number of confidants
			s.MaintainConfidants()

			// Prune ghost stashes periodically
			s.PruneGhostStashes()

			// Log stash metrics every 5 minutes
			s.LogMetrics()
		case <-s.deps.Context().Done():
			return
		}
	}
}

// LogMetrics logs stash storage metrics.
func (s *StashService) LogMetrics() {
	if s.confidantStore == nil {
		return
	}

	metrics := s.confidantStore.GetMetrics()
	myConfidants := s.ConfidantCount()
	targetConfidants := s.TargetConfidantCount()

	// Calculate my stash size from actual stash data
	var myStashSize int
	if s.manager != nil {
		currentStash := s.manager.GetCurrentStash()
		if currentStash != nil {
			myStashSize = len(currentStash.Data)
		}
	}

	logrus.Infof("📦 Stash metrics: stored=%d (%.1fMB), my_confidants=%d/%d, my_size=%.1fKB",
		metrics.StashesStored,
		float64(metrics.TotalStashBytes)/1024/1024,
		myConfidants,
		targetConfidants,
		float64(myStashSize)/1024)
}
