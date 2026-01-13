package nara

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/rand"
	"sync"
	"time"
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
