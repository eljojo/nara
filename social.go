package nara

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sync"
	"time"
)

// TeaseReason constants - will grow over time
const (
	ReasonHighRestarts = "high-restarts"
	ReasonComeback     = "comeback"
	ReasonTrendAbandon = "trend-abandon"
	ReasonRandom       = "random"
	ReasonNiceNumber   = "nice-number" // naras appreciate good looking numbers

	// Observation reasons - system observations that shape opinion
	ReasonOnline          = "online"           // observed a nara come online
	ReasonOffline         = "offline"          // observed a nara go offline
	ReasonJourneyPass     = "journey-pass"     // a world journey passed through us
	ReasonJourneyComplete = "journey-complete" // heard a journey completed
	ReasonJourneyTimeout  = "journey-timeout"  // a journey we saw never completed
)

// SocialEvent represents an immutable social fact in the network
type SocialEvent struct {
	ID        string // hash of content for deduplication
	Timestamp int64  // when it happened
	Type      string // "tease", "observed", "gossip"
	Actor     string // who did it
	Target    string // who it was about
	Reason    string // why (e.g., "high-restarts", "trend-abandon")
	Witness   string // who reported it (empty if self-reported)
	Signature string // Base64-encoded Ed25519 signature (optional)
}

// Sign signs the social event with the given keypair
func (e *SocialEvent) Sign(kp NaraKeypair) {
	// Sign the content that makes up the ID (without the signature itself)
	message := fmt.Sprintf("%d:%s:%s:%s:%s:%s", e.Timestamp, e.Type, e.Actor, e.Target, e.Reason, e.Witness)
	e.Signature = kp.SignBase64([]byte(message))
}

// Verify verifies the social event signature
func (e *SocialEvent) Verify(publicKey []byte) bool {
	if e.Signature == "" {
		return false
	}
	message := fmt.Sprintf("%d:%s:%s:%s:%s:%s", e.Timestamp, e.Type, e.Actor, e.Target, e.Reason, e.Witness)
	return VerifySignatureBase64(publicKey, []byte(message), e.Signature)
}

// ComputeID generates a deterministic ID from event content
func (e *SocialEvent) ComputeID() {
	hasher := sha256.New()
	hasher.Write([]byte(fmt.Sprintf("%d:%s:%s:%s:%s:%s",
		e.Timestamp, e.Type, e.Actor, e.Target, e.Reason, e.Witness)))
	hash := hasher.Sum(nil)
	e.ID = fmt.Sprintf("%x", hash[:16]) // 32 hex chars
}

// IsValid checks if the event has a valid type
func (e *SocialEvent) IsValid() bool {
	validTypes := map[string]bool{
		"tease":       true,
		"observed":    true,
		"gossip":      true,
		"observation": true, // system observations (online/offline, journey events)
	}
	return validTypes[e.Type]
}

// TeaseResonates determines if a tease resonates with an observer
// This is subjective: same tease + different observer = potentially different reaction
// The result is deterministic given the same inputs
func TeaseResonates(eventID string, observerSoul string, observerPersonality NaraPersonality) bool {
	// Hash the event + observer to get a deterministic but observer-specific result
	hasher := sha256.New()
	hasher.Write([]byte(eventID))
	hasher.Write([]byte(observerSoul))
	hash := hasher.Sum(nil)

	// Extract a "resonance score" from the hash (0-99)
	resonance := binary.BigEndian.Uint64(hash[:8]) % 100

	// Threshold depends on observer personality:
	// resonance < threshold means the tease resonates
	// - High Sociability -> appreciates teasing more (higher threshold = more likely to resonate)
	// - High Chill -> harder to impress (lower threshold)
	// - High Agreeableness -> doesn't like conflict (lower threshold)
	//
	// Use int64 to avoid underflow, then clamp to valid range
	threshold := int64(50)
	threshold += int64(observerPersonality.Sociability / 5)    // +0 to +20
	threshold -= int64(observerPersonality.Chill / 10)         // -0 to -10
	threshold -= int64(observerPersonality.Agreeableness / 20) // -0 to -5

	// Clamp threshold to valid range [5, 95]
	if threshold < 5 {
		threshold = 5
	}
	if threshold > 95 {
		threshold = 95
	}

	return resonance < uint64(threshold)
}

// --- Teasing Mechanics ---

// ShouldTeaseForRestarts determines if we should tease based on restart RATE
// A nara with 1000 restarts over 3 years is fine, but 10 restarts in a day is sus
func ShouldTeaseForRestarts(obs NaraObservation, personality NaraPersonality) bool {
	// Calculate restart rate (restarts per day)
	now := time.Now().Unix()
	lifetime := now - obs.StartTime
	if lifetime < 86400 { // less than a day old, give them a break
		return false
	}

	restartsPerDay := float64(obs.Restarts) / (float64(lifetime) / 86400.0)

	// Base threshold: 2 restarts per day is eyebrow-raising
	// Personality adjusts this
	threshold := 2.0
	threshold -= float64(personality.Sociability) / 200.0   // -0 to -0.5 (social naras notice sooner)
	threshold += float64(personality.Chill) / 100.0         // +0 to +1 (chill naras let it slide)
	threshold += float64(personality.Agreeableness) / 200.0 // +0 to +0.5 (agreeable = benefit of doubt)

	if threshold < 0.5 {
		threshold = 0.5
	}

	return restartsPerDay >= threshold
}

// ShouldTeaseForComeback determines if we should tease for coming back
func ShouldTeaseForComeback(obs NaraObservation, previousState string, personality NaraPersonality) bool {
	// Only tease if they were MISSING (not just OFFLINE)
	if previousState != "MISSING" {
		return false
	}

	// Must be currently online
	if obs.Online != "ONLINE" {
		return false
	}

	// Personality gating: sociable naras more likely to comment
	// Use a simple threshold based on sociability
	return personality.Sociability > 40
}

// ShouldTeaseForTrendAbandon determines if we should tease for abandoning a trend
func ShouldTeaseForTrendAbandon(previousTrend, currentTrend string, trendPopularity float64, personality NaraPersonality) bool {
	// Must have been following a trend
	if previousTrend == "" {
		return false
	}

	// Must have stopped following
	if currentTrend == previousTrend {
		return false
	}

	// Only tease if the trend was popular (>30% of network)
	popularityThreshold := 0.3
	// High sociability lowers the threshold (will comment on less popular trends)
	popularityThreshold -= float64(personality.Sociability) / 500.0 // -0 to -0.2

	if trendPopularity < popularityThreshold {
		return false
	}

	// High agreeableness makes us less likely to tease
	if personality.Agreeableness > 70 {
		return false
	}

	return true
}

// IsNiceNumber checks if a number is aesthetically pleasing
// Naras tend to like good looking numbers
func IsNiceNumber(n int64) bool {
	// Classic meme numbers
	niceNumbers := map[int64]bool{
		42:   true, // answer to everything
		67:   true, // the new 69
		69:   true, // nice
		100:  true, // century
		111:  true, // repeating
		123:  true, // sequence
		200:  true,
		222:  true,
		300:  true,
		333:  true,
		365:  true, // days in year
		404:  true, // not found
		420:  true, // blaze it
		444:  true,
		500:  true,
		555:  true,
		666:  true, // spooky
		777:  true, // lucky
		888:  true,
		999:  true,
		1000: true, // big round
		1234: true, // sequence
		1337: true, // leet
	}

	if niceNumbers[n] {
		return true
	}

	// Round numbers (multiples of 100)
	if n >= 100 && n%100 == 0 {
		return true
	}

	// Palindromes (11, 22, 33, 101, 111, 121, etc.)
	if isPalindrome(n) && n >= 10 {
		return true
	}

	return false
}

func isPalindrome(n int64) bool {
	if n < 0 {
		return false
	}
	s := fmt.Sprintf("%d", n)
	for i := 0; i < len(s)/2; i++ {
		if s[i] != s[len(s)-1-i] {
			return false
		}
	}
	return true
}

// ShouldTeaseForNiceNumber determines if we should acknowledge a nice number
func ShouldTeaseForNiceNumber(restarts int64, personality NaraPersonality) bool {
	if !IsNiceNumber(restarts) {
		return false
	}

	// Everyone appreciates nice numbers, but sociable naras are more likely to comment
	// Base 60% chance, +20% for high sociability
	threshold := 60 + personality.Sociability/5

	// Use restarts as seed for determinism
	hasher := sha256.New()
	hasher.Write([]byte(fmt.Sprintf("nice-%d", restarts)))
	hash := hasher.Sum(nil)
	roll := binary.BigEndian.Uint64(hash[:8]) % 100

	return roll < uint64(threshold)
}

// ShouldRandomTease determines if we should randomly tease (deterministic based on inputs)
func ShouldRandomTease(soul, target string, timestamp int64, personality NaraPersonality) bool {
	return ShouldRandomTeaseWithBoost(soul, target, timestamp, personality, 1.0)
}

// ShouldRandomTeaseWithBoost is like ShouldRandomTease but with a probability multiplier.
// Use boost > 1.0 to increase probability (e.g., for nearby naras you notice more).
func ShouldRandomTeaseWithBoost(soul, target string, timestamp int64, personality NaraPersonality, boost float64) bool {
	// Very low probability: ~1% for average personality
	hasher := sha256.New()
	hasher.Write([]byte(soul))
	hasher.Write([]byte(target))
	hasher.Write([]byte(fmt.Sprintf("%d", timestamp)))
	hash := hasher.Sum(nil)

	// Extract probability value (0-999)
	prob := binary.BigEndian.Uint64(hash[:8]) % 1000

	// Base threshold of 10 (1% chance)
	// High sociability increases chance
	// High chill decreases chance
	threshold := float64(10)
	threshold += float64(personality.Sociability / 10) // +0 to +10
	threshold -= float64(personality.Chill / 20)       // -0 to -5

	// Apply proximity boost
	threshold *= boost

	if threshold > 50 {
		threshold = 50 // cap at 5%
	}

	return prob < uint64(threshold)
}

// TeaseMessage returns a tease message for the given reason
func TeaseMessage(reason, actor, target string) string {
	templates := map[string][]string{
		ReasonHighRestarts: {
			"nice uptime there, %s",
			"%s keeping things exciting with all those restarts",
			"%s discovering the restart button",
			"persistence is key, right %s?",
		},
		ReasonComeback: {
			"oh look who decided to show up, %s",
			"welcome back %s, we almost forgot about you",
			"%s rises from the grave",
			"the prodigal %s returns",
		},
		ReasonTrendAbandon: {
			"%s marching to their own beat now",
			"too cool for trends now, %s?",
			"%s going solo",
			"guess %s got bored",
		},
		ReasonRandom: {
			"hey",
			"sup",
			"*pokes*",
			"boop",
		},
		ReasonNiceNumber: {
			"nice.",
			"ooh, %s hit a good one",
			"%s with the satisfying numbers",
			"appreciate the aesthetics, %s",
		},
	}

	msgs, ok := templates[reason]
	if !ok {
		return ""
	}

	// Pick message deterministically based on target name
	idx := 0
	for _, c := range target {
		idx += int(c)
	}
	idx = idx % len(msgs)

	msg := msgs[idx]
	// Format message with target if it has a placeholder
	if containsPlaceholder(msg) {
		return fmt.Sprintf(msg, target)
	}
	return msg
}

func containsPlaceholder(s string) bool {
	for i := 0; i < len(s)-1; i++ {
		if s[i] == '%' && s[i+1] == 's' {
			return true
		}
	}
	return false
}

// TeaseState tracks cooldowns to prevent spam
type TeaseState struct {
	lastTease map[string]int64 // "actor:target" -> timestamp
	mu        sync.RWMutex
	cooldown  int64 // seconds between teases to same target
}

// NewTeaseState creates a new tease cooldown tracker
func NewTeaseState() *TeaseState {
	return &TeaseState{
		lastTease: make(map[string]int64),
		cooldown:  300, // 5 minutes between teases to same target
	}
}

// CanTease checks if the cooldown has passed
func (s *TeaseState) CanTease(actor, target string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := actor + ":" + target
	lastTime, exists := s.lastTease[key]
	if !exists {
		return true
	}

	return time.Now().Unix()-lastTime > s.cooldown
}

// RecordTease records that a tease happened
func (s *TeaseState) RecordTease(actor, target string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := actor + ":" + target
	s.lastTease[key] = time.Now().Unix()
}

// TryTease atomically checks if a tease is allowed and records it if so.
// Returns true if the tease was allowed and recorded, false otherwise.
// This prevents the TOCTOU race between CanTease and RecordTease.
func (s *TeaseState) TryTease(actor, target string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := actor + ":" + target
	now := time.Now().Unix()

	lastTime, exists := s.lastTease[key]
	if exists && now-lastTime <= s.cooldown {
		return false // still in cooldown
	}

	s.lastTease[key] = now
	return true
}

// Cleanup removes old entries from the cooldown tracker
func (s *TeaseState) Cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Unix()
	for key, timestamp := range s.lastTease {
		if now-timestamp > s.cooldown*2 {
			delete(s.lastTease, key)
		}
	}
}

// --- Gossip and Boot Recovery ---

// LedgerRequest is sent to request events from a neighbor
type LedgerRequest struct {
	From     string   // who is asking
	Subjects []string // which subjects (naras) we want events about
}

// LedgerResponse contains events from a neighbor
type LedgerResponse struct {
	From   string        // who is responding
	Events []SocialEvent // events matching the request
}

// PartitionSubjects divides subjects into N roughly equal chunks
// Uses deterministic hashing so different naras get consistent partitions
func PartitionSubjects(subjects []string, n int) [][]string {
	if n <= 0 {
		n = 1
	}
	if n > len(subjects) {
		n = len(subjects)
	}

	partitions := make([][]string, n)
	for i := range partitions {
		partitions[i] = make([]string, 0)
	}

	for _, subject := range subjects {
		// Simple hash-based assignment
		hash := 0
		for _, c := range subject {
			hash = ((hash*31+int(c))%n + n) % n // ensure non-negative
		}
		partitions[hash] = append(partitions[hash], subject)
	}

	return partitions
}
