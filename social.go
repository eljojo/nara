package nara

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
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

// SocialLedger stores events with personality-based filtering
type SocialLedger struct {
	Events      []SocialEvent
	MaxEvents   int
	Personality NaraPersonality
	eventIDs    map[string]bool // for deduplication
	mu          sync.RWMutex
}

// NewSocialLedger creates a new ledger with personality and capacity
func NewSocialLedger(personality NaraPersonality, maxEvents int) *SocialLedger {
	return &SocialLedger{
		Events:      make([]SocialEvent, 0),
		MaxEvents:   maxEvents,
		Personality: personality,
		eventIDs:    make(map[string]bool),
	}
}

// AddEvent adds an event if personality finds it meaningful and it's not a duplicate
func (l *SocialLedger) AddEvent(e SocialEvent) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Compute ID if not set
	if e.ID == "" {
		e.ComputeID()
	}

	// Deduplicate
	if l.eventIDs[e.ID] {
		return false
	}

	// Personality-based filtering: do I even care about this?
	if !l.eventIsMeaningful(e) {
		return false
	}

	l.Events = append(l.Events, e)
	l.eventIDs[e.ID] = true

	return true
}

// eventIsMeaningful decides if this personality cares enough to store the event
func (l *SocialLedger) eventIsMeaningful(e SocialEvent) bool {
	// High chill naras don't bother with random jabs
	if l.Personality.Chill > 70 && e.Reason == ReasonRandom {
		return false
	}

	// Handle observation events specially
	if e.Type == "observation" {
		// Everyone keeps journey-timeout (reliability matters!)
		if e.Reason == ReasonJourneyTimeout {
			return true
		}

		// Very chill naras skip routine online/offline
		if l.Personality.Chill > 85 {
			if e.Reason == ReasonOnline || e.Reason == ReasonOffline {
				return false
			}
		}

		// Low sociability naras skip journey-pass/complete
		if l.Personality.Sociability < 30 {
			if e.Reason == ReasonJourneyPass || e.Reason == ReasonJourneyComplete {
				return false
			}
		}

		return true
	}

	// Very chill naras only care about significant events (for non-observation types)
	if l.Personality.Chill > 85 {
		// Only store comebacks and high-restarts (significant events)
		if e.Reason != ReasonComeback && e.Reason != ReasonHighRestarts {
			return false
		}
	}

	// Highly agreeable naras don't like storing negative drama
	if l.Personality.Agreeableness > 80 && e.Reason == ReasonTrendAbandon {
		return false // "who am I to judge their choices"
	}

	// Low sociability naras are less interested in others' drama
	if l.Personality.Sociability < 20 {
		// Only store if it seems important (not random)
		if e.Reason == ReasonRandom {
			return false
		}
	}

	return true
}

// TeaseResonates determines if a tease resonates with an observer
// This is subjective: same tease + different observer = potentially different reaction
// The result is deterministic given the same inputs
func TeaseResonates(event SocialEvent, observerSoul string, observerPersonality NaraPersonality) bool {
	// Hash the event + observer to get a deterministic but observer-specific result
	hasher := sha256.New()
	hasher.Write([]byte(event.ID))
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

// DeriveClout computes subjective clout scores based on the ledger events
// Each observer derives their own opinion based on their soul and personality
func (l *SocialLedger) DeriveClout(observerSoul string) map[string]float64 {
	l.mu.RLock()
	defer l.mu.RUnlock()

	clout := make(map[string]float64)

	for _, event := range l.Events {
		weight := l.eventWeight(event)

		switch event.Type {
		case "tease":
			if TeaseResonates(event, observerSoul, l.Personality) {
				clout[event.Actor] += weight * 1.0 // good tease = clout
			} else {
				clout[event.Actor] -= weight * 0.3 // bad tease = cringe
			}
		case "observed":
			// Third-party observation, smaller weight
			if TeaseResonates(event, observerSoul, l.Personality) {
				clout[event.Actor] += weight * 0.5
			}
		case "gossip":
			// Gossip has minimal direct clout impact
			clout[event.Actor] += weight * 0.1
		case "observation":
			// System observations affect the TARGET's clout
			l.applyObservationClout(clout, event, weight)
		}
	}

	return clout
}

// applyObservationClout handles clout changes from observation events
// Observations affect the TARGET's clout (the one being observed)
func (l *SocialLedger) applyObservationClout(clout map[string]float64, event SocialEvent, weight float64) {
	switch event.Reason {
	case ReasonOnline:
		// Coming online is slightly positive (reliable, available)
		clout[event.Target] += weight * 0.1
	case ReasonOffline:
		// Going offline is slightly negative (less available)
		clout[event.Target] -= weight * 0.05
	case ReasonJourneyPass:
		// Participating in journeys is positive (engaged citizen)
		clout[event.Target] += weight * 0.2
	case ReasonJourneyComplete:
		// Completing journeys is very positive (success!)
		clout[event.Target] += weight * 0.5
	case ReasonJourneyTimeout:
		// Journey timeout is negative (unreliable)
		clout[event.Target] -= weight * 0.3
	}
}

// eventWeight calculates how much an event matters based on personality AND event properties
// Implements "strong opinions weakly held" - recent events matter more, old ones fade
func (l *SocialLedger) eventWeight(event SocialEvent) float64 {
	// Base weight
	weight := 1.0

	// High sociability cares more about social events
	weight += float64(l.Personality.Sociability) / 200.0 // +0 to +0.5

	// High chill diminishes drama
	weight -= float64(l.Personality.Chill) / 400.0 // -0 to -0.25

	// Event reason affects weight differently per personality
	switch event.Reason {
	case ReasonHighRestarts:
		// Technical stuff - less interesting to highly social naras
		if l.Personality.Sociability > 70 {
			weight *= 0.7
		}
	case ReasonComeback:
		// Social naras love comeback drama
		if l.Personality.Sociability > 60 {
			weight *= 1.4
		}
	case ReasonTrendAbandon:
		// Agreeable naras don't like judging trend choices
		if l.Personality.Agreeableness > 70 {
			weight *= 0.5
		}
		// But social naras find it interesting
		if l.Personality.Sociability > 60 {
			weight *= 1.2
		}
	case ReasonRandom:
		// Chill naras don't care about random jabs
		if l.Personality.Chill > 60 {
			weight *= 0.3
		}
		// Highly agreeable naras find random teasing a bit much
		if l.Personality.Agreeableness > 70 {
			weight *= 0.6
		}
	case ReasonNiceNumber:
		// Everyone appreciates good looking numbers a bit
		weight *= 1.1

	// Observation event reasons
	case ReasonOnline, ReasonOffline:
		// Online/offline are routine events, lower weight
		weight *= 0.5
	case ReasonJourneyPass:
		// Journey participation is moderately interesting
		weight *= 0.8
	case ReasonJourneyComplete:
		// Journey completion is significant
		weight *= 1.3
	case ReasonJourneyTimeout:
		// Journey timeout is notable (indicates problem)
		weight *= 1.2
	}

	// Event type affects weight
	switch event.Type {
	case "tease":
		// Direct teases carry full weight (already at 1.0)
	case "observed":
		// Third-party observations are less impactful
		weight *= 0.7
	case "gossip":
		// Gossip is the least reliable/impactful
		weight *= 0.4
	case "observation":
		// System observations carry full weight (already at 1.0)
		// The reason-based weights above already adjust appropriately
	}

	// Time decay: old events matter less than recent ones
	// Some naras have better memory than others
	now := time.Now().Unix()
	age := now - event.Timestamp
	if age < 0 {
		// Future timestamp (clock skew or malicious) - treat as very old
		age = 7 * 24 * 60 * 60 // 7 days worth of decay
	}
	if age > 0 {
		// Base half-life of 24 hours, modified by personality
		// Low chill = holds onto things longer (better memory for drama)
		// High chill = lets things go faster (shorter memory)
		// High sociability = remembers social events longer
		baseHalfLife := float64(24 * 60 * 60) // 24 hours in seconds

		// Chill naras forget faster (half-life reduced by up to 50%)
		// Non-chill naras remember longer (half-life increased by up to 50%)
		chillModifier := 1.0 + float64(50-l.Personality.Chill)/100.0 // 0.5 to 1.5

		// Sociable naras remember social stuff longer (up to 30% bonus)
		socModifier := 1.0 + float64(l.Personality.Sociability)/333.0 // 1.0 to 1.3

		halfLife := baseHalfLife * chillModifier * socModifier
		decayFactor := 1.0 / (1.0 + float64(age)/halfLife)
		weight *= decayFactor
	}

	if weight < 0.1 {
		weight = 0.1
	}

	return weight
}

// Prune removes old events to stay within MaxEvents limit
// Keeps the most recent events
func (l *SocialLedger) Prune() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.Events) <= l.MaxEvents {
		return
	}

	// Sort by timestamp (ascending)
	sort.Slice(l.Events, func(i, j int) bool {
		return l.Events[i].Timestamp < l.Events[j].Timestamp
	})

	// Keep only the most recent events
	toRemove := len(l.Events) - l.MaxEvents
	removed := l.Events[:toRemove]
	l.Events = l.Events[toRemove:]

	// Update eventIDs map
	for _, e := range removed {
		delete(l.eventIDs, e.ID)
	}
}

// GetEventsAbout returns all events where the given name is the target
func (l *SocialLedger) GetEventsAbout(name string) []SocialEvent {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var result []SocialEvent
	for _, e := range l.Events {
		if e.Target == name {
			result = append(result, e)
		}
	}
	return result
}

// GetEventsByActor returns all events by a given actor
func (l *SocialLedger) GetEventsByActor(name string) []SocialEvent {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var result []SocialEvent
	for _, e := range l.Events {
		if e.Actor == name {
			result = append(result, e)
		}
	}
	return result
}

// GetRecentEvents returns the N most recent events
func (l *SocialLedger) GetRecentEvents(n int) []SocialEvent {
	l.mu.RLock()
	defer l.mu.RUnlock()

	// Sort by timestamp descending
	sorted := make([]SocialEvent, len(l.Events))
	copy(sorted, l.Events)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Timestamp > sorted[j].Timestamp
	})

	if n > len(sorted) {
		n = len(sorted)
	}
	return sorted[:n]
}

// MergeEvents adds events from another source (for boot recovery)
func (l *SocialLedger) MergeEvents(events []SocialEvent) int {
	added := 0
	for _, e := range events {
		if l.AddEvent(e) {
			added++
		}
	}
	return added
}

// EventCount returns the number of events in the ledger
func (l *SocialLedger) EventCount() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.Events)
}

// HasEvent checks if an event ID exists in the ledger
func (l *SocialLedger) HasEvent(id string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.eventIDs[id]
}

// GetEventHashes returns all event IDs for gossip protocol
func (l *SocialLedger) GetEventHashes() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	hashes := make([]string, 0, len(l.Events))
	for _, e := range l.Events {
		hashes = append(hashes, e.ID)
	}
	return hashes
}

// GetEventsInterleaved returns an interleaved slice of events for distributed sync.
// When multiple responders serve events, each returns a different slice:
// - sliceIndex 0 returns events 0, N, 2N, 3N...
// - sliceIndex 1 returns events 1, N+1, 2N+1...
// This provides time-spread coverage so the requester gets variety.
func (l *SocialLedger) GetEventsInterleaved(sliceIndex, totalSlices int) []SocialEvent {
	return l.GetEventsInterleavedWithLimit(sliceIndex, totalSlices, 0)
}

// GetEventsInterleavedWithLimit is like GetEventsInterleaved but with a max event limit
func (l *SocialLedger) GetEventsInterleavedWithLimit(sliceIndex, totalSlices, maxEvents int) []SocialEvent {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if totalSlices <= 0 {
		totalSlices = 1
	}
	if sliceIndex < 0 || sliceIndex >= totalSlices {
		sliceIndex = 0
	}

	// Sort events by timestamp for deterministic ordering
	sorted := make([]SocialEvent, len(l.Events))
	copy(sorted, l.Events)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Timestamp < sorted[j].Timestamp
	})

	// Extract interleaved slice
	var result []SocialEvent
	for i := sliceIndex; i < len(sorted); i += totalSlices {
		result = append(result, sorted[i])
	}

	// Apply max limit if specified
	if maxEvents > 0 && len(result) > maxEvents {
		// Keep most recent events (they're at the end since sorted ascending)
		result = result[len(result)-maxEvents:]
	}

	return result
}

// --- Teasing Mechanics ---

// NewTeaseEvent creates a new tease social event
func NewTeaseEvent(actor, target, reason string) SocialEvent {
	event := SocialEvent{
		Timestamp: time.Now().Unix(),
		Type:      "tease",
		Actor:     actor,
		Target:    target,
		Reason:    reason,
	}
	event.ComputeID()
	return event
}

// NewObservationEvent creates a new observation social event
// Type is "observation" for system observations (online/offline, etc.)
func NewObservationEvent(actor, target, reason string) SocialEvent {
	event := SocialEvent{
		Timestamp: time.Now().Unix(),
		Type:      "observation",
		Actor:     actor,
		Target:    target,
		Reason:    reason,
	}
	event.ComputeID()
	return event
}

// NewJourneyObservationEvent creates a journey-related observation event
// The journeyID is stored in the Witness field for tracking
func NewJourneyObservationEvent(observer, journeyOriginator, reason string, journeyID string) SocialEvent {
	event := SocialEvent{
		Timestamp: time.Now().Unix(),
		Type:      "observation",
		Actor:     observer,
		Target:    journeyOriginator,
		Reason:    reason,
		Witness:   journeyID, // Repurpose Witness field for journey tracking
	}
	event.ComputeID()
	return event
}

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

// GetEventsForSubjects returns events where any subject is actor or target
func (l *SocialLedger) GetEventsForSubjects(subjects []string) []SocialEvent {
	l.mu.RLock()
	defer l.mu.RUnlock()

	subjectSet := make(map[string]bool)
	for _, s := range subjects {
		subjectSet[s] = true
	}

	var result []SocialEvent
	for _, e := range l.Events {
		if subjectSet[e.Actor] || subjectSet[e.Target] {
			result = append(result, e)
		}
	}
	return result
}

// GetEventsSyncSlice returns events for mesh sync with interleaved slicing
// - subjects: filter to events involving these naras (empty = all)
// - sinceTime: only events after this timestamp (0 = no filter)
// - sliceIndex: which slice of interleaved events to return
// - sliceTotal: total number of slices
// - maxEvents: maximum events to return
func (l *SocialLedger) GetEventsSyncSlice(subjects []string, sinceTime int64, sliceIndex, sliceTotal, maxEvents int) []SocialEvent {
	l.mu.RLock()
	defer l.mu.RUnlock()

	// Build subject set for filtering
	subjectSet := make(map[string]bool)
	filterBySubject := len(subjects) > 0
	for _, s := range subjects {
		subjectSet[s] = true
	}

	// First pass: filter events
	var filtered []SocialEvent
	for _, e := range l.Events {
		// Filter by time
		if sinceTime > 0 && e.Timestamp <= sinceTime {
			continue
		}
		// Filter by subject
		if filterBySubject && !subjectSet[e.Actor] && !subjectSet[e.Target] {
			continue
		}
		filtered = append(filtered, e)
	}

	// Apply interleaved slicing if requested
	if sliceTotal > 1 && sliceIndex >= 0 && sliceIndex < sliceTotal {
		var sliced []SocialEvent
		for i, e := range filtered {
			if i%sliceTotal == sliceIndex {
				sliced = append(sliced, e)
			}
		}
		filtered = sliced
	}

	// Apply max limit
	if maxEvents > 0 && len(filtered) > maxEvents {
		filtered = filtered[:maxEvents]
	}

	return filtered
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
