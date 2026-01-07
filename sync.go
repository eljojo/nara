package nara

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Service types for the unified sync ledger
const (
	ServiceSocial = "social" // Social events (teases, observations, gossip)
	ServicePing   = "ping"   // Ping/RTT measurements
)

// SyncEvent is the unified container for all syncable data across services
// This is the fundamental unit of gossip in the nara network
type SyncEvent struct {
	ID        string `json:"id"`
	Timestamp int64  `json:"ts"`
	Service   string `json:"svc"` // "social", "ping", etc.

	// Provenance - who created this event (optional but recommended)
	Emitter   string `json:"emitter,omitempty"` // nara name who created this event
	Signature string `json:"sig,omitempty"`     // base64 Ed25519 signature (optional)

	// Payloads - only one is set based on Service
	Social *SocialEventPayload `json:"social,omitempty"`
	Ping   *PingObservation    `json:"ping,omitempty"`
}

// Payload is the interface for service-specific event data
type Payload interface {
	ContentString() string
	IsValid() bool
	GetActor() string
	GetTarget() string
}

// SocialEventPayload is the social event data within a SyncEvent
// This replaces the standalone SocialEvent for sync purposes
type SocialEventPayload struct {
	Type    string `json:"type"`    // "tease", "observed", "gossip", "observation"
	Actor   string `json:"actor"`   // who did it
	Target  string `json:"target"`  // who it was about
	Reason  string `json:"reason"`  // why (e.g., "high-restarts", "trend-abandon")
	Witness string `json:"witness"` // who reported it (empty if self-reported)
}

// ContentString returns canonical string for hashing/signing
func (p *SocialEventPayload) ContentString() string {
	return fmt.Sprintf("%s:%s:%s:%s:%s", p.Type, p.Actor, p.Target, p.Reason, p.Witness)
}

// IsValid checks if the payload is well-formed
func (p *SocialEventPayload) IsValid() bool {
	validTypes := map[string]bool{
		"tease": true, "observed": true, "gossip": true, "observation": true,
	}
	return validTypes[p.Type] && p.Actor != "" && p.Target != ""
}

// GetActor implements Payload
func (p *SocialEventPayload) GetActor() string { return p.Actor }

// GetTarget implements Payload
func (p *SocialEventPayload) GetTarget() string { return p.Target }

// PingObservation records a latency measurement between two naras
type PingObservation struct {
	Observer string  `json:"observer"` // who took the measurement
	Target   string  `json:"target"`   // who was measured
	RTT      float64 `json:"rtt"`      // round-trip time in milliseconds
}

// ContentString returns canonical string for hashing/signing
// RTT is rounded to 0.1ms to avoid float precision issues
func (p *PingObservation) ContentString() string {
	return fmt.Sprintf("%s:%s:%.1f", p.Observer, p.Target, p.RTT)
}

// IsValid checks if the payload is well-formed
func (p *PingObservation) IsValid() bool {
	return p.Observer != "" && p.Target != "" && p.RTT > 0
}

// GetActor implements Payload (Observer is the actor for pings)
func (p *PingObservation) GetActor() string { return p.Observer }

// GetTarget implements Payload
func (p *PingObservation) GetTarget() string { return p.Target }

// Payload returns the service-specific payload, or nil if none set
func (e *SyncEvent) Payload() Payload {
	switch e.Service {
	case ServiceSocial:
		return e.Social
	case ServicePing:
		return e.Ping
	}
	return nil
}
func (e *SyncEvent) payloadContentString() string {
	switch e.Service {
	case ServiceSocial:
		if e.Social != nil {
			return e.Social.ContentString()
		}
	case ServicePing:
		if e.Ping != nil {
			return e.Ping.ContentString()
		}
	}
	return ""
}

// ComputeID generates a deterministic ID from event content
func (e *SyncEvent) ComputeID() {
	hasher := sha256.New()
	hasher.Write([]byte(fmt.Sprintf("%d:%s:", e.Timestamp, e.Service)))
	hasher.Write([]byte(e.payloadContentString()))
	hash := hasher.Sum(nil)
	e.ID = fmt.Sprintf("%x", hash[:16])
}

// IsValid checks if the event is well-formed
func (e *SyncEvent) IsValid() bool {
	if e.Service == "" || e.Timestamp == 0 {
		return false
	}

	switch e.Service {
	case ServiceSocial:
		return e.Social != nil && e.Social.IsValid()
	case ServicePing:
		return e.Ping != nil && e.Ping.IsValid()
	default:
		return false // Unknown service
	}
}

// IsSigned returns true if this event has a signature
func (e *SyncEvent) IsSigned() bool {
	return e.Emitter != "" && e.Signature != ""
}

// signableData returns the canonical bytes to sign/verify
// This includes all fields except the signature itself
func (e *SyncEvent) signableData() []byte {
	hasher := sha256.New()
	hasher.Write([]byte(fmt.Sprintf("%s:%d:%s:%s:", e.ID, e.Timestamp, e.Service, e.Emitter)))
	hasher.Write([]byte(e.payloadContentString()))
	return hasher.Sum(nil)
}

// Sign signs this event with the given keypair and sets Emitter
func (e *SyncEvent) Sign(emitter string, keypair NaraKeypair) {
	e.Emitter = emitter
	e.Signature = keypair.SignBase64(e.signableData())
}

// Verify checks the signature against the given public key
// Returns true if signature is valid, false otherwise
// Note: Returns false for unsigned events (use IsSigned() to check first)
func (e *SyncEvent) Verify(publicKey ed25519.PublicKey) bool {
	if !e.IsSigned() {
		return false
	}

	sig, err := base64.StdEncoding.DecodeString(e.Signature)
	if err != nil {
		return false
	}

	data := e.signableData()
	return VerifySignature(publicKey, data, sig)
}

// GetActor returns the primary actor of this event (for filtering)
func (e *SyncEvent) GetActor() string {
	switch e.Service {
	case ServiceSocial:
		if e.Social != nil {
			return e.Social.Actor
		}
	case ServicePing:
		if e.Ping != nil {
			return e.Ping.Observer
		}
	}
	return ""
}

// GetTarget returns the target of this event (for filtering)
func (e *SyncEvent) GetTarget() string {
	switch e.Service {
	case ServiceSocial:
		if e.Social != nil {
			return e.Social.Target
		}
	case ServicePing:
		if e.Ping != nil {
			return e.Ping.Target
		}
	}
	return ""
}

// --- Constructors ---

// NewSocialSyncEvent creates a SyncEvent from social event data
func NewSocialSyncEvent(eventType, actor, target, reason, witness string) SyncEvent {
	e := SyncEvent{
		Timestamp: time.Now().UnixNano(),
		Service:   ServiceSocial,
		Social: &SocialEventPayload{
			Type:    eventType,
			Actor:   actor,
			Target:  target,
			Reason:  reason,
			Witness: witness,
		},
	}
	e.ComputeID()
	return e
}

// NewPingSyncEvent creates a SyncEvent from a ping observation
func NewPingSyncEvent(observer, target string, rtt float64) SyncEvent {
	e := SyncEvent{
		Timestamp: time.Now().UnixNano(),
		Service:   ServicePing,
		Ping: &PingObservation{
			Observer: observer,
			Target:   target,
			RTT:      rtt,
		},
	}
	e.ComputeID()
	return e
}

// NewSignedSocialSyncEvent creates a signed SyncEvent for social events
func NewSignedSocialSyncEvent(eventType, actor, target, reason, witness string, emitter string, keypair NaraKeypair) SyncEvent {
	e := NewSocialSyncEvent(eventType, actor, target, reason, witness)
	e.Sign(emitter, keypair)
	return e
}

// NewSignedPingSyncEvent creates a signed SyncEvent for ping observations
func NewSignedPingSyncEvent(observer, target string, rtt float64, emitter string, keypair NaraKeypair) SyncEvent {
	e := NewPingSyncEvent(observer, target, rtt)
	e.Sign(emitter, keypair)
	return e
}

// SyncEventFromSocialEvent converts legacy SocialEvent to SyncEvent
func SyncEventFromSocialEvent(se SocialEvent) SyncEvent {
	e := SyncEvent{
		Timestamp: se.Timestamp,
		Service:   ServiceSocial,
		Social: &SocialEventPayload{
			Type:    se.Type,
			Actor:   se.Actor,
			Target:  se.Target,
			Reason:  se.Reason,
			Witness: se.Witness,
		},
	}
	e.ComputeID()
	return e
}

// ToSocialEvent converts back to legacy SocialEvent (for compatibility)
func (e *SyncEvent) ToSocialEvent() *SocialEvent {
	if e.Service != ServiceSocial || e.Social == nil {
		return nil
	}
	se := &SocialEvent{
		ID:        e.ID,
		Timestamp: e.Timestamp,
		Type:      e.Social.Type,
		Actor:     e.Social.Actor,
		Target:    e.Social.Target,
		Reason:    e.Social.Reason,
		Witness:   e.Social.Witness,
	}
	return se
}

// --- SyncLedger: Unified ledger for all services ---

// SyncLedger stores all syncable events with deduplication and sync support
type SyncLedger struct {
	Events    []SyncEvent
	MaxEvents int
	eventIDs  map[string]bool
	mu        sync.RWMutex
}

// NewSyncLedger creates a new unified sync ledger
func NewSyncLedger(maxEvents int) *SyncLedger {
	return &SyncLedger{
		Events:    make([]SyncEvent, 0),
		MaxEvents: maxEvents,
		eventIDs:  make(map[string]bool),
	}
}

// AddEvent adds an event if it's valid and not a duplicate
func (l *SyncLedger) AddEvent(e SyncEvent) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Compute ID if not set
	if e.ID == "" {
		e.ComputeID()
	}

	// Validate
	if !e.IsValid() {
		return false
	}

	// Deduplicate
	if l.eventIDs[e.ID] {
		return false
	}

	l.Events = append(l.Events, e)
	l.eventIDs[e.ID] = true

	// Prune if over MaxEvents (drop oldest 10%)
	if l.MaxEvents > 0 && len(l.Events) > l.MaxEvents {
		dropCount := l.MaxEvents / 10
		if dropCount < 1 {
			dropCount = 1
		}
		// Delete IDs of dropped events
		for i := 0; i < dropCount; i++ {
			delete(l.eventIDs, l.Events[i].ID)
		}
		l.Events = l.Events[dropCount:]
	}

	return true
}

// AddSocialEvent is a convenience method to add a legacy SocialEvent
func (l *SyncLedger) AddSocialEvent(se SocialEvent) bool {
	return l.AddEvent(SyncEventFromSocialEvent(se))
}

// AddPingObservation is a convenience method to add a ping observation
func (l *SyncLedger) AddPingObservation(observer, target string, rtt float64) bool {
	return l.AddEvent(NewPingSyncEvent(observer, target, rtt))
}

// MaxPingsPerPair limits how many ping observations to keep per observer→target pair
// This prevents the ledger from being saturated with stale ping data while keeping useful history
const MaxPingsPerPair = 5

// AddPingObservationWithReplace adds a ping observation, keeping only the last N per pair
// This ensures diversity: keeps recent history but prevents unbounded growth
func (l *SyncLedger) AddPingObservationWithReplace(observer, target string, rtt float64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Count existing pings for this pair and collect them with indices
	var existingPings []struct {
		idx int
		ts  int64
		id  string
	}
	for i, e := range l.Events {
		if e.Service == ServicePing && e.Ping != nil &&
			e.Ping.Observer == observer && e.Ping.Target == target {
			existingPings = append(existingPings, struct {
				idx int
				ts  int64
				id  string
			}{i, e.Timestamp, e.ID})
		}
	}

	// If at or over limit, remove the oldest one(s)
	if len(existingPings) >= MaxPingsPerPair {
		// Find the oldest ping to remove
		oldestIdx := 0
		oldestTs := existingPings[0].ts
		for i, p := range existingPings {
			if p.ts < oldestTs {
				oldestTs = p.ts
				oldestIdx = i
			}
		}

		// Remove the oldest ping
		toRemove := existingPings[oldestIdx]
		newEvents := make([]SyncEvent, 0, len(l.Events)-1)
		for i, e := range l.Events {
			if i != toRemove.idx {
				newEvents = append(newEvents, e)
			}
		}
		l.Events = newEvents
		delete(l.eventIDs, toRemove.id)
	}

	// Now add the new ping
	newEvent := NewPingSyncEvent(observer, target, rtt)
	if !newEvent.IsValid() {
		return false
	}
	if l.eventIDs[newEvent.ID] {
		return false // shouldn't happen, but safety check
	}

	l.Events = append(l.Events, newEvent)
	l.eventIDs[newEvent.ID] = true
	return true
}

// AddSignedPingObservation adds a signed ping observation
func (l *SyncLedger) AddSignedPingObservation(observer, target string, rtt float64, emitter string, keypair NaraKeypair) bool {
	return l.AddEvent(NewSignedPingSyncEvent(observer, target, rtt, emitter, keypair))
}

// AddSignedPingObservationWithReplace adds a signed ping observation, keeping only the last N per pair
func (l *SyncLedger) AddSignedPingObservationWithReplace(observer, target string, rtt float64, emitter string, keypair NaraKeypair) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Count existing pings for this pair and collect them with indices
	var existingPings []struct {
		idx int
		ts  int64
		id  string
	}
	for i, e := range l.Events {
		if e.Service == ServicePing && e.Ping != nil &&
			e.Ping.Observer == observer && e.Ping.Target == target {
			existingPings = append(existingPings, struct {
				idx int
				ts  int64
				id  string
			}{i, e.Timestamp, e.ID})
		}
	}

	// If at or over limit, remove the oldest one(s)
	if len(existingPings) >= MaxPingsPerPair {
		oldestIdx := 0
		oldestTs := existingPings[0].ts
		for i, p := range existingPings {
			if p.ts < oldestTs {
				oldestTs = p.ts
				oldestIdx = i
			}
		}

		toRemove := existingPings[oldestIdx]
		newEvents := make([]SyncEvent, 0, len(l.Events)-1)
		for i, e := range l.Events {
			if i != toRemove.idx {
				newEvents = append(newEvents, e)
			}
		}
		l.Events = newEvents
		delete(l.eventIDs, toRemove.id)
	}

	// Create and add the new signed ping
	newEvent := NewSignedPingSyncEvent(observer, target, rtt, emitter, keypair)
	if !newEvent.IsValid() {
		return false
	}
	if l.eventIDs[newEvent.ID] {
		return false
	}

	l.Events = append(l.Events, newEvent)
	l.eventIDs[newEvent.ID] = true
	return true
}

// HasEvent checks if an event ID exists
func (l *SyncLedger) HasEvent(id string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.eventIDs[id]
}

// EventCount returns total event count
func (l *SyncLedger) EventCount() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.Events)
}

// --- Filtering ---

// GetEventsByService returns events for a specific service
func (l *SyncLedger) GetEventsByService(service string) []SyncEvent {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var result []SyncEvent
	for _, e := range l.Events {
		if e.Service == service {
			result = append(result, e)
		}
	}
	return result
}

// GetSocialEvents returns all social events (converted to legacy format)
func (l *SyncLedger) GetSocialEvents() []SocialEvent {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var result []SocialEvent
	for _, e := range l.Events {
		if se := e.ToSocialEvent(); se != nil {
			result = append(result, *se)
		}
	}
	return result
}

// GetPingObservations returns all ping observations
func (l *SyncLedger) GetPingObservations() []PingObservation {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var result []PingObservation
	for _, e := range l.Events {
		if e.Service == ServicePing && e.Ping != nil {
			result = append(result, *e.Ping)
		}
	}
	return result
}

// GetEventsInvolving returns events where the given name is actor or target
func (l *SyncLedger) GetEventsInvolving(name string) []SyncEvent {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var result []SyncEvent
	for _, e := range l.Events {
		if e.GetActor() == name || e.GetTarget() == name {
			result = append(result, e)
		}
	}
	return result
}

// --- Sync/Gossip Support ---

// GetEventHashes returns all event IDs for sync protocol
func (l *SyncLedger) GetEventHashes() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	hashes := make([]string, 0, len(l.Events))
	for _, e := range l.Events {
		hashes = append(hashes, e.ID)
	}
	return hashes
}

// GetEventsForSync returns events matching sync criteria
// - services: filter to these services (empty = all)
// - subjects: filter to events involving these naras (empty = all)
// - sinceTime: only events after this timestamp (0 = no filter)
// - sliceIndex/sliceTotal: for interleaved slicing across multiple responders
// - maxEvents: maximum events to return (0 = no limit)
func (l *SyncLedger) GetEventsForSync(services []string, subjects []string, sinceTime int64, sliceIndex, sliceTotal, maxEvents int) []SyncEvent {
	l.mu.RLock()
	defer l.mu.RUnlock()

	// Build filter sets
	serviceSet := make(map[string]bool)
	filterByService := len(services) > 0
	for _, s := range services {
		serviceSet[s] = true
	}

	subjectSet := make(map[string]bool)
	filterBySubject := len(subjects) > 0
	for _, s := range subjects {
		subjectSet[s] = true
	}

	// First pass: filter events
	var filtered []SyncEvent
	for _, e := range l.Events {
		// Filter by time
		if sinceTime > 0 && e.Timestamp <= sinceTime {
			continue
		}
		// Filter by service
		if filterByService && !serviceSet[e.Service] {
			continue
		}
		// Filter by subject
		if filterBySubject && !subjectSet[e.GetActor()] && !subjectSet[e.GetTarget()] {
			continue
		}
		filtered = append(filtered, e)
	}

	// Sort by timestamp for deterministic ordering
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Timestamp < filtered[j].Timestamp
	})

	// Apply interleaved slicing if requested
	if sliceTotal > 1 && sliceIndex >= 0 && sliceIndex < sliceTotal {
		var sliced []SyncEvent
		for i, e := range filtered {
			if i%sliceTotal == sliceIndex {
				sliced = append(sliced, e)
			}
		}
		filtered = sliced
	}

	// Apply max limit
	if maxEvents > 0 && len(filtered) > maxEvents {
		// Keep most recent (they're at the end since sorted ascending)
		filtered = filtered[len(filtered)-maxEvents:]
	}

	return filtered
}

// MergeEvents adds events from another source (for sync/backfill)
func (l *SyncLedger) MergeEvents(events []SyncEvent) int {
	added := 0
	for _, e := range events {
		if l.AddEvent(e) {
			added++
		}
	}
	return added
}

// --- Maintenance ---

// Prune removes old events to stay within MaxEvents limit
func (l *SyncLedger) Prune() {
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

// --- Ping-specific queries ---

// GetLatestPingTo returns the most recent ping observation to a target
func (l *SyncLedger) GetLatestPingTo(target string) *PingObservation {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var latest *PingObservation
	var latestTime int64

	for _, e := range l.Events {
		if e.Service == ServicePing && e.Ping != nil && e.Ping.Target == target {
			if e.Timestamp > latestTime {
				latestTime = e.Timestamp
				ping := *e.Ping // copy
				latest = &ping
			}
		}
	}
	return latest
}

// GetPingsBetween returns all pings between two naras (in either direction)
func (l *SyncLedger) GetPingsBetween(a, b string) []PingObservation {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var result []PingObservation
	for _, e := range l.Events {
		if e.Service == ServicePing && e.Ping != nil {
			if (e.Ping.Observer == a && e.Ping.Target == b) ||
				(e.Ping.Observer == b && e.Ping.Target == a) {
				result = append(result, *e.Ping)
			}
		}
	}
	return result
}

// GetAverageRTT computes average RTT from all ping observations to a target
func (l *SyncLedger) GetAverageRTT(target string) float64 {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var sum float64
	var count int

	for _, e := range l.Events {
		if e.Service == ServicePing && e.Ping != nil && e.Ping.Target == target {
			sum += e.Ping.RTT
			count++
		}
	}

	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

// --- Sync Request/Response types ---

// SyncRequest is sent to request events from a neighbor
type SyncRequest struct {
	From       string   `json:"from"`        // who is asking
	Services   []string `json:"services"`    // which services (empty = all)
	Subjects   []string `json:"subjects"`    // which naras (empty = all)
	SinceTime  int64    `json:"since_time"`  // events after this time
	SliceIndex int      `json:"slice_index"` // for interleaved slicing
	SliceTotal int      `json:"slice_total"` // total slices
	MaxEvents  int      `json:"max_events"`  // limit
}

// SyncResponse contains events from a neighbor with optional signature
type SyncResponse struct {
	From      string      `json:"from"`
	Events    []SyncEvent `json:"events"`
	Timestamp int64       `json:"ts,omitempty"`  // When response was generated
	Signature string      `json:"sig,omitempty"` // Base64 Ed25519 signature
}

// NewSignedSyncResponse creates a signed sync response
func NewSignedSyncResponse(from string, events []SyncEvent, keypair NaraKeypair) SyncResponse {
	resp := SyncResponse{
		From:      from,
		Events:    events,
		Timestamp: time.Now().Unix(),
	}

	// Sign the response
	resp.sign(keypair)
	return resp
}

// sign computes the signature for this response
func (r *SyncResponse) sign(keypair NaraKeypair) {
	// Skip signing if no valid keypair
	if len(keypair.PrivateKey) == 0 {
		return
	}
	r.Signature = keypair.SignBase64(r.signingData())
}

// signingData returns the canonical bytes to sign/verify
func (r *SyncResponse) signingData() []byte {
	hasher := sha256.New()

	// Include from and timestamp
	hasher.Write([]byte(fmt.Sprintf("%s:%d:", r.From, r.Timestamp)))

	// Include events (just their IDs for efficiency)
	for _, e := range r.Events {
		hasher.Write([]byte(e.ID))
	}

	return hasher.Sum(nil)
}

// VerifySignature verifies the response signature against a public key
func (r *SyncResponse) VerifySignature(publicKey ed25519.PublicKey) bool {
	if r.Signature == "" {
		return false
	}

	sigBytes, err := base64.StdEncoding.DecodeString(r.Signature)
	if err != nil {
		return false
	}

	data := r.signingData()
	return VerifySignature(publicKey, data, sigBytes)
}
