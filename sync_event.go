package nara

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"
)

// Service types for the unified sync ledger
const (
	ServiceSocial      = "social"      // Social events (teases, observations, gossip)
	ServicePing        = "ping"        // Ping/RTT measurements
	ServiceObservation = "observation" // Network state observations (restarts, status changes)
	ServiceHeyThere    = "hey-there"   // Peer identity announcements (public key, mesh IP)
	ServiceChau        = "chau"        // Graceful shutdown announcements
	ServiceSeen        = "seen"        // Lightweight "I saw this nara" events for status derivation
	ServiceCheckpoint  = "checkpoint"  // Historical state snapshots with multi-party attestation
)

// Importance levels for observation events
const (
	ImportanceCasual   = 1 // Casual events (teasing) - filtered by personality
	ImportanceNormal   = 2 // Normal events (status-change) - may be filtered by very chill naras
	ImportanceCritical = 3 // Critical events (restart, first-seen) - NEVER filtered
)

// CheckpointCutoffTime is the Unix timestamp (seconds) before which checkpoints are filtered out
// Checkpoints created before this time used buggy code that didn't include LastRestart and other fields
const CheckpointCutoffTime int64 = 1768271051 // 2026-01-12 ~18:24 EST

// SyncEvent is the unified container for all syncable data across services
// This is the fundamental unit of gossip in the nara network
//
// IMPORTANT: Timestamp is in NANOSECONDS (time.Now().UnixNano())
// This provides high precision for event ordering and ID computation.
// Contrast with ObservationEventPayload fields which use seconds.
type SyncEvent struct {
	ID        string `json:"id"`
	Timestamp int64  `json:"ts"`  // Unix timestamp in NANOSECONDS
	Service   string `json:"svc"` // "social", "ping", "observation"

	// Provenance - who created this event (optional but recommended)
	Emitter   string `json:"emitter,omitempty"`    // nara name who created this event
	EmitterID string `json:"emitter_id,omitempty"` // nara ID (public key hash) for signature verification
	Signature string `json:"sig,omitempty"`        // base64 Ed25519 signature (optional)

	// Payloads - only one is set based on Service
	Social      *SocialEventPayload      `json:"social,omitempty"`
	Ping        *PingObservation         `json:"ping,omitempty"`
	Observation *ObservationEventPayload `json:"observation,omitempty"`
	HeyThere    *HeyThereEvent           `json:"hey_there,omitempty"`
	Chau        *ChauEvent               `json:"chau,omitempty"`
	Seen        *SeenEvent               `json:"seen,omitempty"`
	Checkpoint  *CheckpointEventPayload  `json:"checkpoint,omitempty"`
}

// SeenEvent records when a nara was seen through some interaction.
// This is a lightweight event for "I received something from this nara"
// that proves they're reachable/online without creating heavier events.
type SeenEvent struct {
	Observer   string `json:"observer,omitempty"`    // who saw them (name, for display - legacy)
	ObserverID string `json:"observer_id,omitempty"` // who saw them (ID, for verification)
	Subject    string `json:"subject,omitempty"`     // who was seen (name, for display - legacy)
	SubjectID  string `json:"subject_id,omitempty"`  // who was seen (ID, for verification)
	Via        string `json:"via"`                   // how: "zine", "mesh", "ping", "sync"
}

// ContentString returns canonical string for hashing/signing
func (s *SeenEvent) ContentString() string {
	return fmt.Sprintf("seen:%s:%s:%s", s.Observer, s.Subject, s.Via)
}

// IsValid checks if the payload is well-formed
func (s *SeenEvent) IsValid() bool {
	return s.Observer != "" && s.Subject != "" && s.Via != ""
}

// GetActor implements Payload (Observer is the actor)
func (s *SeenEvent) GetActor() string { return s.Observer }

// GetTarget implements Payload (Subject is the target)
func (s *SeenEvent) GetTarget() string { return s.Subject }

// VerifySignature implements Payload using default verification
func (s *SeenEvent) VerifySignature(event *SyncEvent, lookup PublicKeyLookup) bool {
	return DefaultVerifySignature(event, lookup)
}

// UIFormat returns UI-friendly representation
func (s *SeenEvent) UIFormat() map[string]string {
	viaText := map[string]string{
		"zine": "via zine",
		"mesh": "on the mesh",
		"ping": "by ping",
		"sync": "while syncing",
	}
	detail := viaText[s.Via]
	if detail == "" {
		detail = fmt.Sprintf("via %s", s.Via)
	}
	return map[string]string{
		"icon":   "👀",
		"text":   fmt.Sprintf("%s spotted %s", s.Observer, s.Subject),
		"detail": detail,
	}
}

// LogFormat returns technical log description
func (s *SeenEvent) LogFormat() string {
	return fmt.Sprintf("seen: %s saw %s via %s", s.Observer, s.Subject, s.Via)
}

// ToLogEvent returns a log event for vouching (shown in verbose mode)
func (s *SeenEvent) ToLogEvent() *LogEvent {
	subject := s.Subject
	via := s.Via
	return &LogEvent{
		Category: CategoryPresence,
		Type:     "seen",
		Actor:    s.Observer,
		Target:   s.Subject,
		Detail:   via, // Use via as detail for grouping
		GroupFormat: func(actors string) string {
			return fmt.Sprintf("👀 %s vouched for %s (%s)", actors, subject, via)
		},
	}
}

// PublicKeyLookup is a function that resolves a public key by nara ID or name
type PublicKeyLookup func(id, name string) ed25519.PublicKey

// Payload is the interface for service-specific event data
type Payload interface {
	ContentString() string
	IsValid() bool
	GetActor() string
	GetTarget() string
	LogFormat() string     // Returns technical log-friendly description
	ToLogEvent() *LogEvent // Returns structured log event (nil to skip logging)

	// VerifySignature verifies this event's signature. Each payload type handles
	// its own verification logic (embedded keys, multi-sig, etc).
	// The lookup function resolves public keys by nara ID or name.
	VerifySignature(event *SyncEvent, lookup PublicKeyLookup) bool
}

// SocialEventPayload is the social event data within a SyncEvent
// This replaces the standalone SocialEvent for sync purposes
type SocialEventPayload struct {
	Type     string `json:"type"`                // "tease", "observed", "gossip", "observation"
	Actor    string `json:"actor,omitempty"`     // who did it (name, for display - legacy)
	ActorID  string `json:"actor_id,omitempty"`  // who did it (ID, for verification)
	Target   string `json:"target,omitempty"`    // who it was about (name, for display - legacy)
	TargetID string `json:"target_id,omitempty"` // who it was about (ID, for verification)
	Reason   string `json:"reason,omitempty"`    // why (e.g., "high-restarts", "trend-abandon")
	Witness  string `json:"witness,omitempty"`   // who reported it (empty if self-reported)
}

// ContentString returns canonical string for hashing/signing
func (p *SocialEventPayload) ContentString() string {
	return fmt.Sprintf("%s:%s:%s:%s:%s", p.Type, p.Actor, p.Target, p.Reason, p.Witness)
}

// IsValid checks if the payload is well-formed
func (p *SocialEventPayload) IsValid() bool {
	validTypes := map[string]bool{
		"tease": true, "observed": true, "gossip": true, "observation": true, "service": true,
	}
	return validTypes[p.Type] && p.Actor != "" && p.Target != ""
}

// GetActor implements Payload
func (p *SocialEventPayload) GetActor() string { return p.Actor }

// GetTarget implements Payload
func (p *SocialEventPayload) GetTarget() string { return p.Target }

// VerifySignature implements Payload using default verification
func (p *SocialEventPayload) VerifySignature(event *SyncEvent, lookup PublicKeyLookup) bool {
	return DefaultVerifySignature(event, lookup)
}

// UIFormat returns UI-friendly representation
func (p *SocialEventPayload) UIFormat() map[string]string {
	// Use TeaseMessage if available for more interesting messages
	message := TeaseMessage(p.Reason, p.Actor, p.Target)
	if message == "" {
		message = p.Reason
	}

	icon := "💬"
	if p.Type == "tease" {
		icon = "😈"
	} else if p.Type == "observed" {
		icon = "👀"
	} else if p.Type == "gossip" {
		icon = "🗣️"
	}

	return map[string]string{
		"icon":   icon,
		"text":   fmt.Sprintf("%s → %s", p.Actor, p.Target),
		"detail": message,
	}
}

// LogFormat returns technical log description
func (p *SocialEventPayload) LogFormat() string {
	return fmt.Sprintf("social event: %s %s → %s (reason: %s)", p.Type, p.Actor, p.Target, p.Reason)
}

// ToLogEvent returns a structured log event for the logging system
func (p *SocialEventPayload) ToLogEvent() *LogEvent {
	switch p.Type {
	case "tease":
		target := p.Target
		reason := p.Reason
		return &LogEvent{
			Category: CategorySocial,
			Type:     "tease",
			Actor:    p.Actor,
			Target:   p.Target,
			Detail:   p.Reason, // Store reason as detail for grouping key
			Instant:  false,    // Batch teases together
			GroupFormat: func(actors string) string {
				return fmt.Sprintf("😈 %s teased %s (%s)", actors, target, reason)
			},
		}
	case "observed":
		target := p.Target
		reason := p.Reason
		return &LogEvent{
			Category: CategorySocial,
			Type:     "observed",
			Actor:    p.Actor,
			Target:   p.Target,
			Detail:   p.Reason,
			GroupFormat: func(actors string) string {
				return fmt.Sprintf("👀 %s observed %s: %s", actors, target, reason)
			},
		}
	case "gossip":
		target := p.Target
		return &LogEvent{
			Category: CategoryGossip,
			Type:     "social-gossip",
			Actor:    p.Actor,
			Target:   p.Target,
			GroupFormat: func(actors string) string {
				return fmt.Sprintf("🗣️ %s gossiped about %s", actors, target)
			},
		}
	}
	return nil
}

// PingObservation records a latency measurement between two naras
type PingObservation struct {
	Observer   string  `json:"observer,omitempty"`    // who took the measurement (name, for display - legacy)
	ObserverID string  `json:"observer_id,omitempty"` // who took the measurement (ID, for verification)
	Target     string  `json:"target,omitempty"`      // who was measured (name, for display - legacy)
	TargetID   string  `json:"target_id,omitempty"`   // who was measured (ID, for verification)
	RTT        float64 `json:"rtt"`                   // round-trip time in milliseconds
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

// VerifySignature implements Payload using default verification
func (p *PingObservation) VerifySignature(event *SyncEvent, lookup PublicKeyLookup) bool {
	return DefaultVerifySignature(event, lookup)
}

// UIFormat returns UI-friendly representation
func (p *PingObservation) UIFormat() map[string]string {
	// Add some personality based on latency
	quality := ""
	if p.RTT < 10 {
		quality = "besties"
	} else if p.RTT < 50 {
		quality = "i can see you nearby"
	} else if p.RTT < 150 {
		quality = "not too shabby"
	} else if p.RTT < 300 {
		quality = "i wouldn't wanna play counter strike with you"
	} else {
		quality = "where u at?"
	}

	return map[string]string{
		"icon":   "🏓",
		"text":   fmt.Sprintf("%s pinged %s", p.Observer, p.Target),
		"detail": fmt.Sprintf("%.1fms · %s", p.RTT, quality),
	}
}

// LogFormat returns technical log description
func (p *PingObservation) LogFormat() string {
	return fmt.Sprintf("ping: %s → %s (%.2fms)", p.Observer, p.Target, p.RTT)
}

// ToLogEvent returns nil - pings are too noisy for individual logging
func (p *PingObservation) ToLogEvent() *LogEvent {
	return nil
}

// ObservationEventPayload records network state observations (restarts, status changes)
// Used for distributed consensus on network state without O(N²) newspaper broadcasts
//
// NOTE: Time fields in this payload use SECONDS (Unix epoch), not nanoseconds.
// This differs from SyncEvent.Timestamp which uses nanoseconds.
// The reason is that observation data represents coarse-grained state changes
// where second precision is sufficient.
//
// =============================================================================
// FUTURE REFACTOR: Unify with NaraObservation
// =============================================================================
//
// This struct should be refactored to embed NaraObservation for consistency
// with CheckpointEventPayload and to encourage data structure reuse.
//
// Current problems:
//   - Field name divergence: RestartNum vs Restarts, StartTime vs FirstSeen
//   - Missing TotalUptime (checkpoints need it, observations don't track it)
//   - Observer identity duplicates SyncEvent.Emitter (should use EmitterID)
//   - No SubjectID field for cryptographic verification
//
// Target structure:
//
//	type ObservationEvent struct {
//	    // The observation data (embedded, reusable)
//	    Observation NaraObservation `json:"observation"`
//
//	    // Observation metadata
//	    Type           string `json:"type"`                    // "restart", "first-seen", "status-change"
//	    // no inheritance field, it's implied by Observation.Type
//	    // no backfill field, we have a new Type for it
//	    ObserverUptime int64  `json:"observer_uptime,omitempty"`
//
//	    // Note: Observer identity comes from SyncEvent.Emitter/EmitterID
//	    // Note: Signature comes from SyncEvent.Signature
//	}
//
// Migration path:
//  1. Add TotalUptime and SubjectID to NaraObservation
//  2. Add EmitterID to SyncEvent
//  3. Create ObservationEvent type embedding NaraObservation
//  4. Write migration function: ObservationEventPayload → ObservationEvent
//  5. Update event creation to use new type
//  6. Keep reading old format for backward compatibility
//
// =============================================================================
type ObservationEventPayload struct {
	Observer   string `json:"observer,omitempty"`    // who made the observation (name, for display - legacy)
	ObserverID string `json:"observer_id,omitempty"` // who made the observation (ID, for verification)
	Subject    string `json:"subject,omitempty"`     // who is being observed (name, for display - legacy)
	SubjectID  string `json:"subject_id,omitempty"`  // who is being observed (ID, for verification)
	Type       string `json:"type"`                  // "restart", "first-seen", "status-change"
	Importance int    `json:"importance"`            // 1=casual, 2=normal, 3=critical
	IsBackfill bool   `json:"is_backfill,omitempty"` // true if grandfathering existing data

	// Data specific to observation type (all timestamps in SECONDS)
	StartTime   int64  `json:"start_time,omitempty"`   // Unix timestamp in SECONDS when nara started
	RestartNum  int64  `json:"restart_num,omitempty"`  // Total restart count
	LastRestart int64  `json:"last_restart,omitempty"` // Unix timestamp in SECONDS of most recent restart
	OnlineState string `json:"online_state,omitempty"` // "ONLINE", "OFFLINE", "MISSING" (for status-change)
	ClusterName string `json:"cluster_name,omitempty"` // Current cluster/barrio

	// Observer metadata for consensus weighting
	ObserverUptime uint64 `json:"observer_uptime,omitempty"` // Observer's uptime in seconds (for consensus weighting)
}

// ContentString returns canonical string for hashing/signing
// For restart events, includes (subject, restart_num, start_time) for deduplication
func (p *ObservationEventPayload) ContentString() string {
	if p.Type == "restart" {
		// Restart events are deduplicated by content (same restart reported by multiple observers)
		return fmt.Sprintf("%s:restart:%d:%d", p.Subject, p.RestartNum, p.StartTime)
	}
	// Other observation types include observer for uniqueness
	return fmt.Sprintf("%s:%s:%s:%s:%d:%d", p.Observer, p.Subject, p.Type, p.OnlineState, p.StartTime, p.RestartNum)
}

// IsValid checks if the payload is well-formed
func (p *ObservationEventPayload) IsValid() bool {
	// Basic validation
	if p.Observer == "" || p.Subject == "" || p.Type == "" {
		return false
	}

	// Validate type
	validTypes := map[string]bool{
		"restart":       true,
		"first-seen":    true,
		"status-change": true,
	}
	if !validTypes[p.Type] {
		return false
	}

	// Validate importance level
	if p.Importance < ImportanceCasual || p.Importance > ImportanceCritical {
		return false
	}

	// Type-specific validation
	switch p.Type {
	case "restart":
		return p.StartTime > 0 && p.RestartNum >= 0
	case "first-seen":
		return p.StartTime > 0
	case "status-change":
		validStates := map[string]bool{
			"ONLINE":  true,
			"OFFLINE": true,
			"MISSING": true,
		}
		return validStates[p.OnlineState]
	}

	return true
}

// GetActor implements Payload (Observer is the actor for observations)
func (p *ObservationEventPayload) GetActor() string { return p.Observer }

// GetTarget implements Payload (Subject is the target being observed)
func (p *ObservationEventPayload) GetTarget() string { return p.Subject }

// VerifySignature implements Payload using default verification
func (p *ObservationEventPayload) VerifySignature(event *SyncEvent, lookup PublicKeyLookup) bool {
	return DefaultVerifySignature(event, lookup)
}

// UIFormat returns UI-friendly representation
func (p *ObservationEventPayload) UIFormat() map[string]string {
	var text, detail string
	icon := "👁️"

	switch p.Type {
	case "restart":
		icon = "🔄"
		text = fmt.Sprintf("%s restarted", p.Subject)
		if p.RestartNum > 1 {
			detail = fmt.Sprintf("restart #%d", p.RestartNum)
		} else {
			detail = "first restart"
		}
	case "first-seen":
		icon = "✨"
		text = fmt.Sprintf("%s appeared", p.Subject)
		detail = "new nara spotted"
	case "status-change":
		if p.OnlineState == "ONLINE" {
			icon = "🟢"
			text = fmt.Sprintf("%s came online", p.Subject)
			detail = "back!"
		} else if p.OnlineState == "MISSING" {
			icon = "❓"
			text = fmt.Sprintf("%s went missing", p.Subject)
			detail = "where'd they go?"
		} else {
			icon = "🔴"
			text = fmt.Sprintf("%s went offline", p.Subject)
			detail = "see you later"
		}
	default:
		text = fmt.Sprintf("%s observed %s", p.Observer, p.Subject)
		detail = p.OnlineState
	}

	return map[string]string{
		"icon":   icon,
		"text":   text,
		"detail": detail,
	}
}

// LogFormat returns technical log description
func (p *ObservationEventPayload) LogFormat() string {
	return fmt.Sprintf("observation: %s observed %s as %s (type: %s)",
		p.Observer, p.Subject, p.OnlineState, p.Type)
}

// ToLogEvent returns a structured log event for notable observations
func (p *ObservationEventPayload) ToLogEvent() *LogEvent {
	switch p.Type {
	case "first-seen":
		return &LogEvent{
			Category: CategoryPresence,
			Type:     "first-seen",
			Actor:    p.Subject,
			Detail:   fmt.Sprintf("✨ spotted %s for the first time", p.Subject),
			Instant:  true,
		}
	case "restart":
		// Only log milestone restarts
		if p.RestartNum > 0 && p.RestartNum%50 == 0 {
			return &LogEvent{
				Category: CategoryPresence,
				Type:     "restart-milestone",
				Actor:    p.Subject,
				Detail:   fmt.Sprintf("🔄 %s hit restart #%d", p.Subject, p.RestartNum),
				Instant:  true,
			}
		}
	}
	return nil
}

// Payload returns the service-specific payload, or nil if none set
func (e *SyncEvent) Payload() Payload {
	switch e.Service {
	case ServiceSocial:
		return e.Social
	case ServicePing:
		return e.Ping
	case ServiceObservation:
		return e.Observation
	case ServiceHeyThere:
		return e.HeyThere
	case ServiceChau:
		return e.Chau
	case ServiceSeen:
		return e.Seen
	case ServiceCheckpoint:
		return e.Checkpoint
	}
	return nil
}

// ComputeID generates a deterministic ID from event content.
//
// IMPORTANT: Once ComputeID is called (typically in constructors), the event
// should be treated as immutable. Modifying Timestamp, Service, or payload
// fields after ID computation will cause the ID to become stale, leading to:
// - Deduplication failures (same event may be stored multiple times)
// - Signature verification failures
// - Inconsistent event references across the network
func (e *SyncEvent) ComputeID() {
	hasher := sha256.New()
	hasher.Write([]byte(fmt.Sprintf("%d:%s:", e.Timestamp, e.Service)))
	if p := e.Payload(); p != nil {
		hasher.Write([]byte(p.ContentString()))
	}
	hash := hasher.Sum(nil)
	e.ID = fmt.Sprintf("%x", hash[:16])
}

// IsValid checks if the event is well-formed
func (e *SyncEvent) IsValid() bool {
	if e.Service == "" || e.Timestamp == 0 {
		return false
	}
	p := e.Payload()
	return p != nil && p.IsValid()
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
	if p := e.Payload(); p != nil {
		hasher.Write([]byte(p.ContentString()))
	}
	return hasher.Sum(nil)
}

// Sign signs this event with the given keypair and sets Emitter
func (e *SyncEvent) Sign(emitter string, keypair NaraKeypair) {
	e.Emitter = emitter
	e.Signature = keypair.SignBase64(e.signableData())
}

// Verify verifies this event's signature by delegating to the payload.
// The lookup function resolves public keys by nara ID or name.
// Returns true if signature is valid, false otherwise.
func (e *SyncEvent) Verify(lookup PublicKeyLookup) bool {
	if !e.IsSigned() {
		return false
	}
	if p := e.Payload(); p != nil {
		return p.VerifySignature(e, lookup)
	}
	return false
}

// VerifyWithKey checks the signature against the given public key.
// Use this when you already have the public key. For automatic key resolution,
// use Verify(lookup) instead.
func (e *SyncEvent) VerifyWithKey(publicKey ed25519.PublicKey) bool {
	if !e.IsSigned() || publicKey == nil {
		return false
	}

	sig, err := base64.StdEncoding.DecodeString(e.Signature)
	if err != nil {
		return false
	}

	data := e.signableData()
	return VerifySignature(publicKey, data, sig)
}

// DefaultVerifySignature is the default verification logic for payloads without
// embedded keys. It looks up the public key by EmitterID (or Emitter name as fallback)
// and verifies the SyncEvent signature.
func DefaultVerifySignature(event *SyncEvent, lookup PublicKeyLookup) bool {
	if lookup == nil {
		return false
	}
	pubKey := lookup(event.EmitterID, event.Emitter)
	return event.VerifyWithKey(pubKey)
}

// GetActor returns the primary actor of this event (for filtering)
func (e *SyncEvent) GetActor() string {
	if p := e.Payload(); p != nil {
		return p.GetActor()
	}
	return ""
}

// GetTarget returns the target of this event (for filtering)
func (e *SyncEvent) GetTarget() string {
	if p := e.Payload(); p != nil {
		return p.GetTarget()
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

// NewRestartObservationEvent creates a SyncEvent for a restart observation
func NewRestartObservationEvent(observer, subject string, startTime, restartNum int64) SyncEvent {
	e := SyncEvent{
		Timestamp: time.Now().UnixNano(),
		Service:   ServiceObservation,
		Observation: &ObservationEventPayload{
			Observer:    observer,
			Subject:     subject,
			Type:        "restart",
			Importance:  ImportanceCritical,
			StartTime:   startTime,
			RestartNum:  restartNum,
			LastRestart: time.Now().Unix(),
		},
	}
	e.ComputeID()
	return e
}

// NewRestartObservationEventWithUptime creates a restart observation with explicit observer uptime
// Used for uptime-weighted consensus where longer-running observers get more weight
func NewRestartObservationEventWithUptime(observer, subject string, startTime, restartNum int64, observerUptime uint64) SyncEvent {
	e := SyncEvent{
		Timestamp: time.Now().UnixNano(),
		Service:   ServiceObservation,
		Observation: &ObservationEventPayload{
			Observer:       observer,
			Subject:        subject,
			Type:           "restart",
			Importance:     ImportanceCritical,
			StartTime:      startTime,
			RestartNum:     restartNum,
			LastRestart:    time.Now().Unix(),
			ObserverUptime: observerUptime,
		},
	}
	e.ComputeID()
	return e
}

// NewFirstSeenObservationEvent creates a SyncEvent for first-seen observation
func NewFirstSeenObservationEvent(observer, subject string, startTime int64) SyncEvent {
	e := SyncEvent{
		Timestamp: time.Now().UnixNano(),
		Service:   ServiceObservation,
		Observation: &ObservationEventPayload{
			Observer:   observer,
			Subject:    subject,
			Type:       "first-seen",
			Importance: ImportanceCritical,
			StartTime:  startTime,
		},
	}
	e.ComputeID()
	return e
}

// NewStatusChangeObservationEvent creates a SyncEvent for status change observation
func NewStatusChangeObservationEvent(observer, subject, onlineState string) SyncEvent {
	e := SyncEvent{
		Timestamp: time.Now().UnixNano(),
		Service:   ServiceObservation,
		Observation: &ObservationEventPayload{
			Observer:    observer,
			Subject:     subject,
			Type:        "status-change",
			Importance:  ImportanceNormal,
			OnlineState: onlineState,
		},
	}
	e.ComputeID()
	return e
}

// --- ID-aware constructors ---
// These constructors include both name (for display) and ID (for verification) fields.
// Use these when creating events to ensure proper ID-based verification.

// NewRestartObservationEventWithIDs creates a restart observation with explicit IDs
func NewRestartObservationEventWithIDs(observer, observerID, subject, subjectID string, startTime, restartNum int64) SyncEvent {
	e := SyncEvent{
		Timestamp: time.Now().UnixNano(),
		Service:   ServiceObservation,
		Observation: &ObservationEventPayload{
			Observer:    observer,
			ObserverID:  observerID,
			Subject:     subject,
			SubjectID:   subjectID,
			Type:        "restart",
			Importance:  ImportanceCritical,
			StartTime:   startTime,
			RestartNum:  restartNum,
			LastRestart: time.Now().Unix(),
		},
	}
	e.ComputeID()
	return e
}

// NewFirstSeenObservationEventWithIDs creates a first-seen observation with explicit IDs
func NewFirstSeenObservationEventWithIDs(observer, observerID, subject, subjectID string, startTime int64) SyncEvent {
	e := SyncEvent{
		Timestamp: time.Now().UnixNano(),
		Service:   ServiceObservation,
		Observation: &ObservationEventPayload{
			Observer:   observer,
			ObserverID: observerID,
			Subject:    subject,
			SubjectID:  subjectID,
			Type:       "first-seen",
			Importance: ImportanceCritical,
			StartTime:  startTime,
		},
	}
	e.ComputeID()
	return e
}

// NewStatusChangeObservationEventWithIDs creates a status change observation with explicit IDs
func NewStatusChangeObservationEventWithIDs(observer, observerID, subject, subjectID, onlineState string) SyncEvent {
	e := SyncEvent{
		Timestamp: time.Now().UnixNano(),
		Service:   ServiceObservation,
		Observation: &ObservationEventPayload{
			Observer:    observer,
			ObserverID:  observerID,
			Subject:     subject,
			SubjectID:   subjectID,
			Type:        "status-change",
			Importance:  ImportanceNormal,
			OnlineState: onlineState,
		},
	}
	e.ComputeID()
	return e
}

// NewSeenSyncEventWithIDs creates a signed seen event with explicit IDs
func NewSeenSyncEventWithIDs(observer, observerID, subject, subjectID, via string, keypair NaraKeypair) SyncEvent {
	e := SyncEvent{
		Timestamp: time.Now().UnixNano(),
		Service:   ServiceSeen,
		Seen: &SeenEvent{
			Observer:   observer,
			ObserverID: observerID,
			Subject:    subject,
			SubjectID:  subjectID,
			Via:        via,
		},
	}
	e.ComputeID()
	e.Sign(observer, keypair)
	return e
}

// NewTeaseSyncEventWithIDs creates a signed tease event with explicit IDs
func NewTeaseSyncEventWithIDs(actor, actorID, target, targetID, reason string, keypair NaraKeypair) SyncEvent {
	e := SyncEvent{
		Timestamp: time.Now().UnixNano(),
		Service:   ServiceSocial,
		Social: &SocialEventPayload{
			Type:     "tease",
			Actor:    actor,
			ActorID:  actorID,
			Target:   target,
			TargetID: targetID,
			Reason:   reason,
		},
	}
	e.ComputeID()
	e.Sign(actor, keypair)
	return e
}

// NewBackfillObservationEvent creates a backfill event for migrating historical observations
func NewBackfillObservationEvent(observer, subject string, startTime, restartNum, lastRestart int64) SyncEvent {
	e := SyncEvent{
		Timestamp: time.Now().UnixNano(),
		Service:   ServiceObservation,
		Observation: &ObservationEventPayload{
			Observer:    observer,
			Subject:     subject,
			Type:        "restart",
			Importance:  ImportanceCritical,
			IsBackfill:  true,
			StartTime:   startTime,
			RestartNum:  restartNum,
			LastRestart: lastRestart,
		},
	}
	e.ComputeID()
	return e
}

// NewSignedPingSyncEvent creates a signed SyncEvent for ping observations
func NewSignedPingSyncEvent(observer, target string, rtt float64, emitter string, keypair NaraKeypair) SyncEvent {
	e := NewPingSyncEvent(observer, target, rtt)
	e.Sign(emitter, keypair)
	return e
}

// NewHeyThereSyncEvent creates a signed SyncEvent for peer identity announcements.
// This allows hey_there events to propagate through gossip, enabling peer discovery
// without MQTT broadcasts. The SyncEvent signature is the attestation - inner event
// is just payload data.
func NewHeyThereSyncEvent(name string, publicKey string, meshIP string, id string, keypair NaraKeypair) SyncEvent {
	heyThere := &HeyThereEvent{
		From:      name,
		PublicKey: publicKey,
		MeshIP:    meshIP,
		ID:        id,
	}

	e := SyncEvent{
		Timestamp: time.Now().UnixNano(),
		Service:   ServiceHeyThere,
		HeyThere:  heyThere,
	}
	e.ComputeID()
	e.Sign(name, keypair)
	return e
}

// NewChauSyncEvent creates a signed SyncEvent for graceful shutdown announcements.
// This allows chau events to propagate through gossip, enabling gossip-only naras
// to distinguish OFFLINE (graceful) from MISSING (timeout). The SyncEvent signature
// is the attestation - inner event is just payload data.
func NewChauSyncEvent(name string, publicKey string, id string, keypair NaraKeypair) SyncEvent {
	chau := &ChauEvent{
		From:      name,
		PublicKey: publicKey,
		ID:        id,
	}

	e := SyncEvent{
		Timestamp: time.Now().UnixNano(),
		Service:   ServiceChau,
		Chau:      chau,
	}
	e.ComputeID()
	e.Sign(name, keypair)
	return e
}

// NewSeenSyncEvent creates a signed SyncEvent for when a nara is seen through some interaction.
// The via parameter indicates how they were seen: "zine", "mesh", "ping", "sync".
// Signed so other naras can verify who made the observation when events propagate.
func NewSeenSyncEvent(observer, subject, via string, keypair NaraKeypair) SyncEvent {
	e := SyncEvent{
		Timestamp: time.Now().UnixNano(),
		Service:   ServiceSeen,
		Seen: &SeenEvent{
			Observer: observer,
			Subject:  subject,
			Via:      via,
		},
	}
	e.ComputeID()
	e.Sign(observer, keypair)
	return e
}

// NewTeaseSyncEvent creates a signed SyncEvent for teasing another nara.
func NewTeaseSyncEvent(actor, target, reason string, keypair NaraKeypair) SyncEvent {
	return NewSignedSocialSyncEvent("tease", actor, target, reason, "", actor, keypair)
}

// NewObservationSocialSyncEvent creates a signed SyncEvent for system observations.
func NewObservationSocialSyncEvent(actor, target, reason string, keypair NaraKeypair) SyncEvent {
	return NewSignedSocialSyncEvent("observation", actor, target, reason, "", actor, keypair)
}

// NewJourneyObservationSyncEvent creates a signed SyncEvent for journey observations.
// The journeyID is stored in the Witness field for tracking.
func NewJourneyObservationSyncEvent(observer, journeyOriginator, reason, journeyID string, keypair NaraKeypair) SyncEvent {
	return NewSignedSocialSyncEvent("observation", observer, journeyOriginator, reason, journeyID, observer, keypair)
}

// SyncEventFromSocialEvent converts legacy SocialEvent to SyncEvent
// Deprecated: Use NewSocialSyncEvent instead for new code.
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
