package nara

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	// MaxStashSize is the maximum size of a stash payload (10KB)
	MaxStashSize = 10 * 1024
)

// EmojiPool is the set of emojis to choose from for identity generation
var EmojiPool = []string{
	"🌸", "🌺", "🌻", "🌼", "🌷", "🪻", "🌹", "🥀", "💐", "🪷",
	"🌳", "🌲", "🌴", "🌵", "🌾", "🌿", "🍀", "🍁", "🍂", "🍃",
	"🍄", "🪨", "🌊", "🔥", "⭐", "🌙", "☀️", "🌈", "⚡", "❄️",
	"🦋", "🐝", "🐛", "🦎", "🐢", "🐙", "🦑", "🦀", "🐠", "🐟",
	"🦅", "🦆", "🦉", "🦇", "🐺", "🦊", "🐱", "🐶", "🐰", "🐻",
}

// StashData is what gets encrypted (timestamp inside for tamper-proofing)
// This stores arbitrary JSON data - NOT tied to emojis or any specific schema.
// Naras can use this to store any JSON they want, distributed to confidants.
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

// MigrateEmojiStashToJSON converts legacy emoji-based stash data to JSON format
// This helper is for backward compatibility with old stash data
func MigrateEmojiStashToJSON(emojis []string) (json.RawMessage, error) {
	data := map[string]interface{}{
		"emojis": emojis,
	}
	return json.Marshal(data)
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
type StashStoreRequest struct {
	From      string        `json:"from"`
	Timestamp int64         `json:"timestamp"` // Unix timestamp (seconds)
	Stash     *StashPayload `json:"stash"`
	Signature string        `json:"signature"`
}

func (s *StashStoreRequest) Sign(keypair NaraKeypair) {
	data := fmt.Sprintf("stash_store:%s:%d", s.From, s.Timestamp)
	sig := keypair.Sign([]byte(data))
	s.Signature = base64.StdEncoding.EncodeToString(sig)
}

func (s *StashStoreRequest) Verify(publicKey []byte) bool {
	sig, err := base64.StdEncoding.DecodeString(s.Signature)
	if err != nil {
		return false
	}
	data := fmt.Sprintf("stash_store:%s:%d", s.From, s.Timestamp)
	return VerifySignature(publicKey, []byte(data), sig)
}

type StashStoreResponse struct {
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason,omitempty"` // Why rejected
}

// POST /stash/retrieve - Owner requests stash back from confidant
type StashRetrieveRequest struct {
	From      string `json:"from"`
	Timestamp int64  `json:"timestamp"`
	Signature string `json:"signature"`
}

func (s *StashRetrieveRequest) Sign(keypair NaraKeypair) {
	data := fmt.Sprintf("stash_retrieve:%s:%d", s.From, s.Timestamp)
	sig := keypair.Sign([]byte(data))
	s.Signature = base64.StdEncoding.EncodeToString(sig)
}

func (s *StashRetrieveRequest) Verify(publicKey []byte) bool {
	sig, err := base64.StdEncoding.DecodeString(s.Signature)
	if err != nil {
		return false
	}
	data := fmt.Sprintf("stash_retrieve:%s:%d", s.From, s.Timestamp)
	return VerifySignature(publicKey, []byte(data), sig)
}

type StashRetrieveResponse struct {
	Found bool          `json:"found"`
	Stash *StashPayload `json:"stash,omitempty"`
}

// POST /stash/push - Confidant pushes stash to owner (hey-there recovery)
type StashPushRequest struct {
	From      string        `json:"from"`
	To        string        `json:"to"`
	Timestamp int64         `json:"timestamp"`
	Stash     *StashPayload `json:"stash"`
	Signature string        `json:"signature"`
}

func (s *StashPushRequest) Sign(keypair NaraKeypair) {
	data := fmt.Sprintf("stash_push:%s:%s:%d", s.From, s.To, s.Timestamp)
	sig := keypair.Sign([]byte(data))
	s.Signature = base64.StdEncoding.EncodeToString(sig)
}

func (s *StashPushRequest) Verify(publicKey []byte) bool {
	sig, err := base64.StdEncoding.DecodeString(s.Signature)
	if err != nil {
		return false
	}
	data := fmt.Sprintf("stash_push:%s:%s:%d", s.From, s.To, s.Timestamp)
	return VerifySignature(publicKey, []byte(data), sig)
}

type StashPushResponse struct {
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason,omitempty"`
}

// DELETE /stash/store - Owner requests confidant to delete stash
type StashDeleteRequest struct {
	From      string `json:"from"`
	Timestamp int64  `json:"timestamp"`
	Signature string `json:"signature"`
}

func (s *StashDeleteRequest) Sign(keypair NaraKeypair) {
	data := fmt.Sprintf("stash_delete:%s:%d", s.From, s.Timestamp)
	sig := keypair.Sign([]byte(data))
	s.Signature = base64.StdEncoding.EncodeToString(sig)
}

func (s *StashDeleteRequest) Verify(publicKey []byte) bool {
	sig, err := base64.StdEncoding.DecodeString(s.Signature)
	if err != nil {
		return false
	}
	data := fmt.Sprintf("stash_delete:%s:%d", s.From, s.Timestamp)
	return VerifySignature(publicKey, []byte(data), sig)
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

// SelectRandom selects a random confidant from online naras, excluding self, existing, and pending confidants
// Deprecated: Use SelectBest for smarter selection based on memory mode and uptime
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
		candidates = append(candidates, name)
	}

	if len(candidates) == 0 {
		return ""
	}

	return candidates[rand.Intn(len(candidates))]
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

// GenerateRandomEmojis generates a random sequence of n emojis
func GenerateRandomEmojis(n int) []string {
	emojis := make([]string, n)
	for i := 0; i < n; i++ {
		emojis[i] = EmojiPool[rand.Intn(len(EmojiPool))]
	}
	return emojis
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
}

// GetMetrics returns current storage metrics
func (s *ConfidantStashStore) GetMetrics() StashMetrics {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return StashMetrics{
		StashesStored:   len(s.stashes),
		TotalStashBytes: s.totalBytes,
		EvictionCount:   s.evictionCount,
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
