package nara

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/rand"
	"sync"
	"time"
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
type StashData struct {
	Timestamp int64    `json:"timestamp"` // When this stash was created
	Emojis    []string `json:"emojis"`    // Random emoji sequence (identity token)
	// Future: add more fields as needed
}

// Marshal serializes StashData to JSON
func (s *StashData) Marshal() ([]byte, error) {
	return json.Marshal(s)
}

// Unmarshal deserializes StashData from JSON
func (s *StashData) Unmarshal(data []byte) error {
	return json.Unmarshal(data, s)
}

// StashPayload is the encrypted blob stored by confidents
type StashPayload struct {
	Owner      string `json:"owner"`
	Nonce      []byte `json:"nonce"`      // 24-byte XChaCha20 nonce
	Ciphertext []byte `json:"ciphertext"` // Encrypted StashData
}

// Size returns the total size of the payload
func (p *StashPayload) Size() int {
	return len(p.Owner) + len(p.Nonce) + len(p.Ciphertext)
}

// StashStore is sent from owner to confident to store stash
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

// StashStoreAck is sent from confident to owner to confirm storage
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

// StashResponse is sent from confident to owner with the stored stash
type StashResponse struct {
	From    string       `json:"from"`
	Owner   string       `json:"owner"`
	Payload StashPayload `json:"payload"`
}

// StashDelete is sent from owner to confident to delete their stash
type StashDelete struct {
	From      string `json:"from"`
	Signature string `json:"signature"`
}

// Sign signs the delete request
func (s *StashDelete) Sign(keypair NaraKeypair) {
	data := []byte("stash_delete:" + s.From)
	sig := keypair.Sign(data)
	s.Signature = base64.StdEncoding.EncodeToString(sig)
}

// Verify verifies the delete request signature
func (s *StashDelete) Verify(publicKey []byte) bool {
	sig, err := base64.StdEncoding.DecodeString(s.Signature)
	if err != nil {
		return false
	}
	data := []byte("stash_delete:" + s.From)
	return VerifySignature(publicKey, data, sig)
}

// ConfidentTracker tracks which confidents have our stash (owner side)
type ConfidentTracker struct {
	confidents  map[string]int64 // name -> timestamp they confirmed (acked)
	pending     map[string]int64 // name -> timestamp we sent (awaiting ack)
	targetCount int
	mu          sync.RWMutex
}

// PendingTimeout is how long to wait for an ack before giving up
const PendingTimeout = 60 * time.Second

// NewConfidentTracker creates a new tracker with the given target count
func NewConfidentTracker(targetCount int) *ConfidentTracker {
	return &ConfidentTracker{
		confidents:  make(map[string]int64),
		pending:     make(map[string]int64),
		targetCount: targetCount,
	}
}

// Add adds a confident with the given confirmation timestamp (for acked confidents)
func (t *ConfidentTracker) Add(name string, timestamp int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.confidents[name] = timestamp
	delete(t.pending, name) // Remove from pending if it was there
}

// AddPending marks a confident as pending (sent but not yet acked)
func (t *ConfidentTracker) AddPending(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, confirmed := t.confidents[name]; !confirmed {
		t.pending[name] = time.Now().Unix()
	}
}

// CleanupExpiredPending removes pending confidents that have timed out
func (t *ConfidentTracker) CleanupExpiredPending() []string {
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

// Remove removes a confident
func (t *ConfidentTracker) Remove(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.confidents, name)
	delete(t.pending, name)
}

// Has returns true if the given name is a confirmed confident
func (t *ConfidentTracker) Has(name string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	_, ok := t.confidents[name]
	return ok
}

// IsPending returns true if the given name is pending (sent but not acked)
func (t *ConfidentTracker) IsPending(name string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	_, ok := t.pending[name]
	return ok
}

// Count returns the number of tracked confidents
func (t *ConfidentTracker) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.confidents)
}

// IsSatisfied returns true if we have at least targetCount confidents
func (t *ConfidentTracker) IsSatisfied() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.confidents) >= t.targetCount
}

// NeedsMore returns true if we need more confidents
func (t *ConfidentTracker) NeedsMore() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.confidents) < t.targetCount
}

// NeedsCount returns how many more confidents are needed
func (t *ConfidentTracker) NeedsCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	needed := t.targetCount - len(t.confidents)
	if needed < 0 {
		return 0
	}
	return needed
}

// GetExcess returns confidents beyond the target count (oldest first)
func (t *ConfidentTracker) GetExcess() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if len(t.confidents) <= t.targetCount {
		return nil
	}

	// Sort by timestamp (oldest first)
	type entry struct {
		name string
		ts   int64
	}
	entries := make([]entry, 0, len(t.confidents))
	for name, ts := range t.confidents {
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

// SelectRandom selects a random confident from online naras, excluding self, existing, and pending confidents
func (t *ConfidentTracker) SelectRandom(self string, online []string) string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// Build candidate list
	candidates := make([]string, 0)
	for _, name := range online {
		if name == self {
			continue
		}
		if _, exists := t.confidents[name]; exists {
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

// MarkOffline removes confidents that are no longer online
func (t *ConfidentTracker) MarkOffline(name string, online []string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Check if the confident is still online
	found := false
	for _, n := range online {
		if n == name {
			found = true
			break
		}
	}

	if !found {
		delete(t.confidents, name)
	}
}

// GetAll returns all confident names
func (t *ConfidentTracker) GetAll() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	names := make([]string, 0, len(t.confidents))
	for name := range t.confidents {
		names = append(names, name)
	}
	return names
}

// StashManager manages stash operations for the owner
type StashManager struct {
	ownerName        string
	keypair          NaraKeypair
	encKeypair       EncryptionKeypair
	confidentTracker *ConfidentTracker

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
		confidentTracker: NewConfidentTracker(targetConfidents),
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
		// Decrypt and parse the payload
		decrypted, err := m.encKeypair.DecryptForSelf(resp.Payload.Nonce, resp.Payload.Ciphertext)
		if err != nil {
			continue // Skip invalid responses
		}

		data := &StashData{}
		if err := data.Unmarshal(decrypted); err != nil {
			continue
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
		m.confidentTracker.Add(resp.From, time.Now().Unix())
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

// GetRecoveredEmojis returns the recovered emoji sequence, or nil if none
func (m *StashManager) GetRecoveredEmojis() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.recoveredStash != nil {
		return m.recoveredStash.Emojis
	}
	return nil
}

// GenerateRandomEmojis generates a random sequence of n emojis
func GenerateRandomEmojis(n int) []string {
	emojis := make([]string, n)
	for i := 0; i < n; i++ {
		emojis[i] = EmojiPool[rand.Intn(len(EmojiPool))]
	}
	return emojis
}

// ConfidentStashStore stores stash payloads in memory (confident side)
type ConfidentStashStore struct {
	stashes map[string]*StashPayload // owner -> payload
	mu      sync.RWMutex
}

// NewConfidentStashStore creates a new in-memory stash store
func NewConfidentStashStore() *ConfidentStashStore {
	return &ConfidentStashStore{
		stashes: make(map[string]*StashPayload),
	}
}

// HandleStashStore handles an incoming StashStore message
func (s *ConfidentStashStore) HandleStashStore(msg *StashStore, getPublicKey func(string) []byte) error {
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

// Store stores a payload for the given owner
func (s *ConfidentStashStore) Store(owner string, payload *StashPayload) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stashes[owner] = payload
}

// HasStashFor returns true if we have a stash for the given owner
func (s *ConfidentStashStore) HasStashFor(owner string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.stashes[owner]
	return ok
}

// CreateAck creates an ack message for the given owner
func (s *ConfidentStashStore) CreateAck(owner string) *StashStoreAck {
	return &StashStoreAck{
		Owner: owner,
	}
}

// HandleStashRequest handles a stash request and returns a response if we have the stash
func (s *ConfidentStashStore) HandleStashRequest(req *StashRequest, getPublicKey func(string) []byte) *StashResponse {
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

// HandleStashDelete handles a delete request
func (s *ConfidentStashStore) HandleStashDelete(msg *StashDelete, getPublicKey func(string) []byte) error {
	// Verify signature
	pubKey := getPublicKey(msg.From)
	if pubKey == nil {
		return errors.New("unknown sender")
	}
	if !msg.Verify(pubKey) {
		return errors.New("invalid signature")
	}

	s.mu.Lock()
	delete(s.stashes, msg.From)
	s.mu.Unlock()

	return nil
}

// CreateStashPayload creates an encrypted stash payload
func CreateStashPayload(owner string, data *StashData, encKeypair EncryptionKeypair) (*StashPayload, error) {
	// Serialize
	serialized, err := data.Marshal()
	if err != nil {
		return nil, err
	}

	// Encrypt
	nonce, ciphertext, err := encKeypair.EncryptForSelf(serialized)
	if err != nil {
		return nil, err
	}

	return &StashPayload{
		Owner:      owner,
		Nonce:      nonce,
		Ciphertext: ciphertext,
	}, nil
}

// ExtractStashTimestamp decrypts a payload and returns the timestamp
func ExtractStashTimestamp(payload *StashPayload, encKeypair EncryptionKeypair) (int64, error) {
	decrypted, err := encKeypair.DecryptForSelf(payload.Nonce, payload.Ciphertext)
	if err != nil {
		return 0, err
	}

	data := &StashData{}
	if err := data.Unmarshal(decrypted); err != nil {
		return 0, err
	}

	return data.Timestamp, nil
}
