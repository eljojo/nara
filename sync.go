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
	Emitter   string `json:"emitter,omitempty"` // nara name who created this event
	Signature string `json:"sig,omitempty"`     // base64 Ed25519 signature (optional)

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
	Observer string `json:"observer"` // who saw them
	Subject  string `json:"subject"`  // who was seen
	Via      string `json:"via"`      // how: "zine", "mesh", "ping", "sync"
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
	return &LogEvent{
		Category: CategoryPresence,
		Type:     "seen",
		Actor:    s.Observer,
		Target:   s.Subject,
		Detail:   fmt.Sprintf("👀 vouched for %s (%s)", s.Subject, s.Via),
	}
}

// Payload is the interface for service-specific event data
type Payload interface {
	ContentString() string
	IsValid() bool
	GetActor() string
	GetTarget() string
	LogFormat() string     // Returns technical log-friendly description
	ToLogEvent() *LogEvent // Returns structured log event (nil to skip logging)
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
		"tease": true, "observed": true, "gossip": true, "observation": true, "service": true,
	}
	return validTypes[p.Type] && p.Actor != "" && p.Target != ""
}

// GetActor implements Payload
func (p *SocialEventPayload) GetActor() string { return p.Actor }

// GetTarget implements Payload
func (p *SocialEventPayload) GetTarget() string { return p.Target }

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
		msg := TeaseMessage(p.Reason, p.Actor, p.Target)
		return &LogEvent{
			Category: CategorySocial,
			Type:     "tease",
			Actor:    p.Actor,
			Target:   p.Target,
			Detail:   fmt.Sprintf("😈 %s teased %s: \"%s\"", p.Actor, p.Target, msg),
			Instant:  true,
		}
	case "observed":
		return &LogEvent{
			Category: CategorySocial,
			Type:     "observed",
			Actor:    p.Actor,
			Target:   p.Target,
			Detail:   fmt.Sprintf("👁️ %s observed %s: %s", p.Actor, p.Target, p.Reason),
		}
	case "gossip":
		return &LogEvent{
			Category: CategoryGossip,
			Type:     "social-gossip",
			Actor:    p.Actor,
			Target:   p.Target,
			Detail:   fmt.Sprintf("🗣️ %s gossiped about %s", p.Actor, p.Target),
		}
	}
	return nil
}

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
type ObservationEventPayload struct {
	Observer   string `json:"observer"`              // who made the observation
	Subject    string `json:"subject"`               // who is being observed
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

// ObservationRateLimit tracks events per subject for rate limiting
type ObservationRateLimit struct {
	SubjectCounts map[string][]int64 // subject → timestamps of events
	WindowSec     int64              // Time window in seconds
	MaxEvents     int                // Max events per subject per window
	mu            sync.Mutex
}

// NewObservationRateLimit creates a new rate limiter
func NewObservationRateLimit() *ObservationRateLimit {
	return &ObservationRateLimit{
		SubjectCounts: make(map[string][]int64),
		WindowSec:     300, // 5 minutes
		MaxEvents:     10,  // max 10 events per subject per 5 min
	}
}

// CheckAndAdd checks if an event would exceed rate limit, and adds it if not
// Returns true if the event is allowed, false if rate limit exceeded
func (r *ObservationRateLimit) CheckAndAdd(subject string, timestamp int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	windowStart := timestamp - r.WindowSec

	// Get existing timestamps for this subject
	timestamps := r.SubjectCounts[subject]

	// Filter out events outside the current window
	var validTimestamps []int64
	for _, ts := range timestamps {
		if ts >= windowStart {
			validTimestamps = append(validTimestamps, ts)
		}
	}

	// Check if we're at the limit
	if len(validTimestamps) >= r.MaxEvents {
		return false // Rate limit exceeded
	}

	// Add this event
	validTimestamps = append(validTimestamps, timestamp)
	r.SubjectCounts[subject] = validTimestamps

	return true
}

// Cleanup removes stale entries from SubjectCounts to prevent unbounded growth.
// Should be called periodically (e.g., every 5 minutes during maintenance).
func (r *ObservationRateLimit) Cleanup() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().Unix()
	windowStart := now - r.WindowSec

	for subject, timestamps := range r.SubjectCounts {
		// Filter to only keep timestamps within the current window
		var valid []int64
		for _, ts := range timestamps {
			if ts >= windowStart {
				valid = append(valid, ts)
			}
		}

		if len(valid) == 0 {
			// No valid timestamps, remove the subject entirely
			delete(r.SubjectCounts, subject)
		} else {
			r.SubjectCounts[subject] = valid
		}
	}
}

// MaxObservationsPerPair limits observation events per observer→subject pair
const MaxObservationsPerPair = 20

// SyncLedger stores all syncable events with deduplication and sync support
// EventListener is called when a new event is added to the ledger
type EventListener func(event SyncEvent)

type SyncLedger struct {
	Events        []SyncEvent
	MaxEvents     int
	eventIDs      map[string]bool
	observationRL *ObservationRateLimit // Rate limiter for observation events
	version       int64                 // Increments on structural changes (prune, out-of-order insert)
	mu            sync.RWMutex

	// isUnknownNara is an optional callback to check if a nara is unknown (no public key).
	// Events from unknown naras are pruned first. Set via SetUnknownNaraChecker.
	isUnknownNara func(name string) bool

	// listeners are notified when new events are added
	listeners   []EventListener
	listenersMu sync.RWMutex
}

// NewSyncLedger creates a new unified sync ledger
func NewSyncLedger(maxEvents int) *SyncLedger {
	return &SyncLedger{
		Events:        make([]SyncEvent, 0),
		MaxEvents:     maxEvents,
		eventIDs:      make(map[string]bool),
		observationRL: NewObservationRateLimit(),
	}
}

// AddListener registers a callback to be notified when new events are added.
// Listeners are called synchronously after the event is stored.
func (l *SyncLedger) AddListener(listener EventListener) {
	l.listenersMu.Lock()
	defer l.listenersMu.Unlock()
	l.listeners = append(l.listeners, listener)
}

// notifyListeners calls all registered listeners with the new event.
// Called without holding the main mutex to avoid deadlocks.
func (l *SyncLedger) notifyListeners(event SyncEvent) {
	l.listenersMu.RLock()
	listeners := l.listeners
	l.listenersMu.RUnlock()

	for _, listener := range listeners {
		listener(event)
	}
}

// SetUnknownNaraChecker sets a callback to check if a nara is unknown (no public key).
// Events from unknown naras are pruned first during storage pressure.
func (l *SyncLedger) SetUnknownNaraChecker(checker func(name string) bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.isUnknownNara = checker
}

// isEventFromUnknownNara checks if an event is from/about an unknown nara.
// Returns false if no checker is set or the nara is known.
func (l *SyncLedger) isEventFromUnknownNara(e SyncEvent) bool {
	if l.isUnknownNara == nil {
		return false
	}

	// Extract the relevant nara name(s) from the event
	var names []string

	switch e.Service {
	case ServiceHeyThere:
		if e.HeyThere != nil {
			names = append(names, e.HeyThere.From)
		}
	case ServiceChau:
		if e.Chau != nil {
			names = append(names, e.Chau.From)
		}
	case ServiceObservation:
		if e.Observation != nil {
			names = append(names, e.Observation.Observer, e.Observation.Subject)
		}
	case ServiceSocial:
		if e.Social != nil {
			names = append(names, e.Social.Actor)
			if e.Social.Target != "" {
				names = append(names, e.Social.Target)
			}
			if e.Social.Witness != "" {
				names = append(names, e.Social.Witness)
			}
		}
	case ServicePing:
		if e.Ping != nil {
			names = append(names, e.Ping.Observer, e.Ping.Target)
		}
	case ServiceSeen:
		if e.Seen != nil {
			names = append(names, e.Seen.Observer, e.Seen.Subject)
		}
	}

	// If ANY of the naras involved are unknown, consider this event lower priority
	for _, name := range names {
		if name != "" && l.isUnknownNara(name) {
			return true
		}
	}

	return false
}

// AddEvent adds an event if it's valid and not a duplicate (by ID).
//
// For observation events, use AddEventWithDedup() instead if you want
// content-based deduplication (e.g., to prevent multiple observers from
// creating duplicate restart events for the same restart occurrence).
//
// This method only deduplicates by event ID (hash of timestamp + content).
// Two observers reporting the same restart will generate different IDs
// (due to different timestamps) and both events will be stored.
func (l *SyncLedger) AddEvent(e SyncEvent) bool {
	// Observation events get special compaction handling (without deduplication)
	if e.Service == ServiceObservation && e.Observation != nil {
		return l.addObservationWithCompaction(e, false)
	}

	l.mu.Lock()

	// Compute ID if not set
	if e.ID == "" {
		e.ComputeID()
	}

	// Validate
	if !e.IsValid() {
		l.mu.Unlock()
		return false
	}

	// Deduplicate
	if l.eventIDs[e.ID] {
		l.mu.Unlock()
		return false
	}

	if e.Service == ServicePing && e.Ping != nil {
		// Limit ping history per target to avoid unbounded growth.
		oldestIdx := -1
		oldestTs := int64(0)
		oldestID := ""
		pingCount := 0
		for i, existing := range l.Events {
			if existing.Service == ServicePing && existing.Ping != nil &&
				existing.Ping.Target == e.Ping.Target {
				pingCount++
				if oldestIdx == -1 || existing.Timestamp < oldestTs {
					oldestIdx = i
					oldestTs = existing.Timestamp
					oldestID = existing.ID
				}
			}
		}

		if pingCount >= MaxPingsPerTarget {
			// Drop older ping events to keep the newest set for this target.
			if e.Timestamp <= oldestTs {
				l.mu.Unlock()
				return false
			}
			kept := make([]SyncEvent, 0, len(l.Events)-1)
			for i, existing := range l.Events {
				if i != oldestIdx {
					kept = append(kept, existing)
				}
			}
			l.Events = kept
			delete(l.eventIDs, oldestID)
			l.version++
		}
	}

	l.Events = append(l.Events, e)
	l.eventIDs[e.ID] = true

	// Prune if over MaxEvents using priority-based pruning
	if l.MaxEvents > 0 && len(l.Events) > l.MaxEvents {
		l.pruneUnlocked()
	}

	l.mu.Unlock()

	// Notify listeners outside of lock to avoid deadlocks
	l.notifyListeners(e)

	return true
}

// AddSocialEvent is a convenience method to add a legacy SocialEvent
// Deprecated: Use AddEvent with SyncEvent for new code.
func (l *SyncLedger) AddSocialEvent(se SocialEvent) bool {
	return l.AddEvent(SyncEventFromSocialEvent(se))
}

// AddSocialEventFilteredLegacy adds a legacy SocialEvent with personality filtering
// Deprecated: Use AddSocialEventFiltered with SyncEvent for new code.
func (l *SyncLedger) AddSocialEventFilteredLegacy(se SocialEvent, personality NaraPersonality) bool {
	return l.AddSocialEventFiltered(SyncEventFromSocialEvent(se), personality)
}

// MergeSocialEventsFiltered adds legacy SocialEvents with personality filtering
// Deprecated: Convert events to SyncEvent format for new code.
func (l *SyncLedger) MergeSocialEventsFiltered(events []SocialEvent, personality NaraPersonality) int {
	added := 0
	for _, se := range events {
		if l.AddSocialEventFilteredLegacy(se, personality) {
			added++
		}
	}
	return added
}

// GetSocialEventsForSubjects returns social events (legacy format) where any subject is actor or target
func (l *SyncLedger) GetSocialEventsForSubjects(subjects []string) []SocialEvent {
	l.mu.RLock()
	defer l.mu.RUnlock()

	subjectSet := make(map[string]bool)
	for _, s := range subjects {
		subjectSet[s] = true
	}

	var result []SocialEvent
	for _, e := range l.Events {
		if e.Service != ServiceSocial || e.Social == nil {
			continue
		}
		if subjectSet[e.Social.Actor] || subjectSet[e.Social.Target] {
			if se := e.ToSocialEvent(); se != nil {
				result = append(result, *se)
			}
		}
	}
	return result
}

// AddPingObservation is a convenience method to add a ping observation
func (l *SyncLedger) AddPingObservation(observer, target string, rtt float64) bool {
	return l.AddEvent(NewPingSyncEvent(observer, target, rtt))
}

// MaxPingsPerTarget limits how many ping observations to keep per target (receiver)
// This prevents the ledger from being saturated with stale ping data while keeping useful history.
const MaxPingsPerTarget = 5

// AddSignedPingObservation adds a signed ping observation
func (l *SyncLedger) AddSignedPingObservation(observer, target string, rtt float64, emitter string, keypair NaraKeypair) bool {
	return l.AddEvent(NewSignedPingSyncEvent(observer, target, rtt, emitter, keypair))
}

// AddSignedPingObservationWithReplace adds a signed ping observation, keeping only the last N per target
func (l *SyncLedger) AddSignedPingObservationWithReplace(observer, target string, rtt float64, emitter string, keypair NaraKeypair) bool {
	// Retention is enforced in AddEvent for all ping events.
	return l.AddEvent(NewSignedPingSyncEvent(observer, target, rtt, emitter, keypair))
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

// GetAllEvents returns all events (for testing/validation)
func (l *SyncLedger) GetAllEvents() []SyncEvent {
	l.mu.RLock()
	defer l.mu.RUnlock()

	result := make([]SyncEvent, len(l.Events))
	copy(result, l.Events)
	return result
}

// RemoveEventsFor removes all events involving a specific nara (as actor or target).
// Returns the number of events removed.
func (l *SyncLedger) RemoveEventsFor(name string) int {
	l.mu.Lock()
	defer l.mu.Unlock()

	kept := make([]SyncEvent, 0, len(l.Events))
	removed := 0

	for _, e := range l.Events {
		if l.eventInvolvesNara(e, name) {
			delete(l.eventIDs, e.ID)
			removed++
		} else {
			kept = append(kept, e)
		}
	}

	if removed > 0 {
		l.Events = kept
		l.version++
	}

	return removed
}

// eventInvolvesNara checks if an event involves a specific nara (as actor or target).
func (l *SyncLedger) eventInvolvesNara(e SyncEvent, name string) bool {
	if e.Emitter == name {
		return true
	}
	switch e.Service {
	case ServiceHeyThere:
		if e.HeyThere != nil && e.HeyThere.From == name {
			return true
		}
	case ServiceChau:
		if e.Chau != nil && e.Chau.From == name {
			return true
		}
	case ServiceObservation:
		if e.Observation != nil && (e.Observation.Observer == name || e.Observation.Subject == name) {
			return true
		}
	case ServiceSocial:
		if e.Social != nil && (e.Social.Actor == name || e.Social.Target == name || e.Social.Witness == name) {
			return true
		}
	case ServicePing:
		if e.Ping != nil && (e.Ping.Observer == name || e.Ping.Target == name) {
			return true
		}
	case ServiceSeen:
		if e.Seen != nil && (e.Seen.Observer == name || e.Seen.Subject == name) {
			return true
		}
	}
	return false
}

// GetEventsSince returns events from the given position onwards, plus the current
// total count and version. If the version has changed since the caller last saw it,
// the caller should reset and reprocess from position 0.
func (l *SyncLedger) GetEventsSince(position int) ([]SyncEvent, int, int64) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	total := len(l.Events)
	if position >= total {
		return nil, total, l.version
	}

	result := make([]SyncEvent, total-position)
	copy(result, l.Events[position:])
	return result, total, l.version
}

// GetVersion returns the current structural version of the ledger.
func (l *SyncLedger) GetVersion() int64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.version
}

// GetObservationEventsAbout returns all observation events about a specific subject
func (l *SyncLedger) GetObservationEventsAbout(subject string) []SyncEvent {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var result []SyncEvent
	for _, e := range l.Events {
		if e.Service == ServiceObservation && e.Observation != nil && e.Observation.Subject == subject {
			result = append(result, e)
		}
	}
	return result
}

// GetBackfillEvent returns the backfill event for a subject, or nil if none exists
func (l *SyncLedger) GetBackfillEvent(subject string) *ObservationEventPayload {
	l.mu.RLock()
	defer l.mu.RUnlock()

	for _, e := range l.Events {
		if e.Service == ServiceObservation && e.Observation != nil &&
			e.Observation.Subject == subject && e.Observation.IsBackfill {
			return e.Observation
		}
	}
	return nil
}

// DeriveRestartCount derives the total restart count for a subject
// Priority: checkpoint > backfill > count unique start times
//
// The count is calculated as:
//  1. If checkpoint exists: checkpoint.Restarts + count(unique StartTimes after checkpoint.AsOfTime)
//  2. If backfill exists: backfill.RestartNum + count(unique StartTimes after backfill timestamp)
//  3. Otherwise: count(unique StartTimes from all restart events)
func (l *SyncLedger) DeriveRestartCount(subject string) int64 {
	l.mu.RLock()
	defer l.mu.RUnlock()

	// Check for checkpoint first (highest priority)
	var checkpoint *CheckpointEventPayload
	var checkpointAsOfTime int64 = 0
	for _, e := range l.Events {
		if e.Service == ServiceCheckpoint && e.Checkpoint != nil && e.Checkpoint.Subject == subject {
			if e.Checkpoint.AsOfTime > checkpointAsOfTime {
				checkpoint = e.Checkpoint
				checkpointAsOfTime = e.Checkpoint.AsOfTime
			}
		}
	}

	if checkpoint != nil {
		// Count unique start times after checkpoint
		newRestarts := l.countUniqueStartTimesAfterLocked(subject, checkpoint.AsOfTime)
		return checkpoint.Restarts + newRestarts
	}

	// Check for backfill event (second priority)
	var backfill *ObservationEventPayload
	for _, e := range l.Events {
		if e.Service == ServiceObservation && e.Observation != nil &&
			e.Observation.Subject == subject && e.Observation.IsBackfill {
			backfill = e.Observation
			break
		}
	}

	if backfill != nil {
		// Count unique start times that are DIFFERENT from the backfill's StartTime
		// New restarts are identified by having a different StartTime than what backfill captured
		newRestarts := l.countUniqueStartTimesExcludingLocked(subject, backfill.StartTime)
		return backfill.RestartNum + newRestarts
	}

	// No checkpoint or backfill - just count all unique start times
	return l.countUniqueStartTimesAfterLocked(subject, 0)
}

// countUniqueStartTimesAfterLocked counts unique StartTime values for restart events after a given time
// Caller must hold the read lock
func (l *SyncLedger) countUniqueStartTimesAfterLocked(subject string, afterTime int64) int64 {
	startTimes := make(map[int64]bool)

	for _, e := range l.Events {
		if e.Service != ServiceObservation || e.Observation == nil {
			continue
		}
		if e.Observation.Subject != subject {
			continue
		}
		if e.Observation.Type != "restart" {
			continue
		}
		// Skip backfill events when counting new restarts
		if e.Observation.IsBackfill {
			continue
		}
		// Only count restart events whose StartTime is after the cutoff
		// This means the restart happened AFTER the checkpoint/reference point
		if afterTime > 0 && e.Observation.StartTime <= afterTime {
			continue
		}
		startTimes[e.Observation.StartTime] = true
	}

	return int64(len(startTimes))
}

// countUniqueStartTimesExcludingLocked counts unique StartTime values excluding a specific one
// Used for backfill: count all unique start times except the one the backfill already captured
// Caller must hold the read lock
func (l *SyncLedger) countUniqueStartTimesExcludingLocked(subject string, excludeStartTime int64) int64 {
	startTimes := make(map[int64]bool)

	for _, e := range l.Events {
		if e.Service != ServiceObservation || e.Observation == nil {
			continue
		}
		if e.Observation.Subject != subject {
			continue
		}
		if e.Observation.Type != "restart" {
			continue
		}
		// Skip backfill events when counting new restarts
		if e.Observation.IsBackfill {
			continue
		}
		// Skip the specific StartTime that the backfill already counted
		if e.Observation.StartTime == excludeStartTime {
			continue
		}
		startTimes[e.Observation.StartTime] = true
	}

	return int64(len(startTimes))
}

// DeriveTotalUptime derives the total uptime for a subject in seconds
// Priority: checkpoint baseline + uptime from status change events after checkpoint
//
// The calculation:
//  1. Start with checkpoint.TotalUptime (or 0 if no checkpoint)
//  2. Add up online periods from status-change events after checkpoint
func (l *SyncLedger) DeriveTotalUptime(subject string) int64 {
	l.mu.RLock()
	defer l.mu.RUnlock()

	// Check for checkpoint first
	var checkpoint *CheckpointEventPayload
	var checkpointAsOfTime int64 = 0
	for _, e := range l.Events {
		if e.Service == ServiceCheckpoint && e.Checkpoint != nil && e.Checkpoint.Subject == subject {
			if e.Checkpoint.AsOfTime > checkpointAsOfTime {
				checkpoint = e.Checkpoint
				checkpointAsOfTime = e.Checkpoint.AsOfTime
			}
		}
	}

	baseUptime := int64(0)
	afterTime := int64(0)
	if checkpoint != nil {
		baseUptime = checkpoint.TotalUptime
		afterTime = checkpoint.AsOfTime
	}

	// Collect status change events after checkpoint
	type statusEvent struct {
		timestamp int64
		state     string
	}
	var statusEvents []statusEvent

	for _, e := range l.Events {
		if e.Service != ServiceObservation || e.Observation == nil {
			continue
		}
		if e.Observation.Subject != subject {
			continue
		}
		if e.Observation.Type != "status-change" {
			continue
		}
		eventTimeSec := e.Timestamp / 1e9
		if afterTime > 0 && eventTimeSec <= afterTime {
			continue
		}
		statusEvents = append(statusEvents, statusEvent{
			timestamp: eventTimeSec,
			state:     e.Observation.OnlineState,
		})
	}

	// Sort by timestamp
	sort.Slice(statusEvents, func(i, j int) bool {
		return statusEvents[i].timestamp < statusEvents[j].timestamp
	})

	// Calculate online periods
	var onlineStart int64 = 0
	for _, se := range statusEvents {
		if se.state == "ONLINE" && onlineStart == 0 {
			onlineStart = se.timestamp
		} else if (se.state == "OFFLINE" || se.state == "MISSING") && onlineStart > 0 {
			baseUptime += se.timestamp - onlineStart
			onlineStart = 0
		}
	}

	// If still online, count up to now
	if onlineStart > 0 {
		baseUptime += time.Now().Unix() - onlineStart
	}

	return baseUptime
}

// GetFirstSeenFromEvents returns the earliest known StartTime for a subject from events
func (l *SyncLedger) GetFirstSeenFromEvents(subject string) int64 {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var earliest int64 = 0

	for _, e := range l.Events {
		// Check observation events (including backfill)
		if e.Service == ServiceObservation && e.Observation != nil {
			if e.Observation.Subject == subject && e.Observation.StartTime > 0 {
				if earliest == 0 || e.Observation.StartTime < earliest {
					earliest = e.Observation.StartTime
				}
			}
		}

		// Check existing checkpoints
		if e.Service == ServiceCheckpoint && e.Checkpoint != nil {
			if e.Checkpoint.Subject == subject && e.Checkpoint.FirstSeen > 0 {
				if earliest == 0 || e.Checkpoint.FirstSeen < earliest {
					earliest = e.Checkpoint.FirstSeen
				}
			}
		}
	}

	return earliest
}

// AddEventFiltered adds an event with personality-based filtering
// Critical importance events (3) are NEVER filtered
// Normal importance events (2) may be filtered by very chill naras (>85)
// Casual importance events (1) are filtered based on personality
func (l *SyncLedger) AddEventFiltered(e SyncEvent, personality NaraPersonality) bool {
	// Observation events: check importance
	if e.Service == ServiceObservation && e.Observation != nil {
		// Critical events NEVER filtered
		if e.Observation.Importance == ImportanceCritical {
			return l.AddEvent(e)
		}

		// Normal events: filter by very chill naras
		if e.Observation.Importance == ImportanceNormal {
			if personality.Chill > 85 {
				return false // Too chill to care about status changes
			}
			return l.AddEvent(e)
		}

		// Casual events: apply personality filtering
		// (Implementation can be expanded based on requirements)
	}

	// Default: add the event
	return l.AddEvent(e)
}

// AddEventWithRateLimit adds an event after checking rate limits
// For observation events, enforces max 10 events per subject per 5 minutes
func (l *SyncLedger) AddEventWithRateLimit(e SyncEvent) bool {
	if e.Service == ServiceObservation && e.Observation != nil {
		// Check rate limit
		timestamp := e.Timestamp / 1e9 // Convert nanoseconds to seconds
		if !l.observationRL.CheckAndAdd(e.Observation.Subject, timestamp) {
			return false // Rate limit exceeded
		}
	}

	return l.AddEvent(e)
}

// AddEventWithDedup adds an event after checking for content-based deduplication.
//
// USE THIS METHOD when adding observation events where multiple observers might
// report the same underlying occurrence (e.g., restarts). For restart events,
// deduplicates by (subject, restart_num, start_time) - meaning if observer A
// and observer B both report that nara X had restart #5 at time T, only one
// event is stored.
//
// USE AddEvent() when:
// - Adding non-observation events (social, ping)
// - You intentionally want multiple reports of the same occurrence
//
// This distinction matters for consensus: we want diverse observations but
// don't want to count the same restart twice when tallying.
func (l *SyncLedger) AddEventWithDedup(e SyncEvent) bool {
	// Deduplication is handled atomically inside addObservationWithCompaction
	return l.addObservationWithCompaction(e, true)
}

// addObservationWithCompaction adds an observation event with per-pair compaction
// Enforces max MaxObservationsPerPair per observer→subject pair
// withDedup controls whether content-based deduplication should be applied
// Caller must ensure e.Service == ServiceObservation && e.Observation != nil
func (l *SyncLedger) addObservationWithCompaction(e SyncEvent, withDedup bool) bool {
	l.mu.Lock()

	// Content-based deduplication for restart events (only if requested)
	// Check if we already have this exact restart (by subject, restart_num, start_time)
	// This is checked FIRST to avoid even counting duplicates in per-pair limits
	if withDedup && e.Observation != nil && e.Observation.Type == "restart" {
		for _, existing := range l.Events {
			if existing.Service == ServiceObservation && existing.Observation != nil &&
				existing.Observation.Type == "restart" &&
				existing.Observation.Subject == e.Observation.Subject &&
				existing.Observation.RestartNum == e.Observation.RestartNum &&
				existing.Observation.StartTime == e.Observation.StartTime {
				l.mu.Unlock()
				return false // Duplicate restart event
			}
		}
	}

	// Count existing observations for this observer→subject pair
	var existingObs []struct {
		idx int
		ts  int64
		id  string
	}
	for i, existing := range l.Events {
		if existing.Service == ServiceObservation && existing.Observation != nil &&
			existing.Observation.Observer == e.Observation.Observer &&
			existing.Observation.Subject == e.Observation.Subject {
			existingObs = append(existingObs, struct {
				idx int
				ts  int64
				id  string
			}{i, existing.Timestamp, existing.ID})
		}
	}

	// If at or over limit, remove the oldest one(s)
	// IMPORTANT: Don't prune restart events if no checkpoint exists for this subject
	// This ensures we don't lose historical restart data before it's checkpointed
	if len(existingObs) >= MaxObservationsPerPair {
		// Check if a checkpoint exists for this subject
		hasCheckpoint := false
		for _, existing := range l.Events {
			if existing.Service == ServiceCheckpoint && existing.Checkpoint != nil &&
				existing.Checkpoint.Subject == e.Observation.Subject {
				hasCheckpoint = true
				break
			}
		}

		// Find the oldest NON-RESTART event to prune (prefer pruning status-change over restart)
		// If all events are restarts and we have no checkpoint, skip pruning to preserve history
		var oldestNonRestartIdx = -1
		var oldestNonRestartTs int64 = 0
		var oldestAnyIdx = 0
		var oldestAnyTs = existingObs[0].ts

		for i, obs := range existingObs {
			// Track overall oldest
			if obs.ts < oldestAnyTs {
				oldestAnyTs = obs.ts
				oldestAnyIdx = i
			}

			// Track oldest non-restart
			ev := l.Events[obs.idx]
			if ev.Service == ServiceObservation && ev.Observation != nil &&
				ev.Observation.Type != "restart" {
				if oldestNonRestartIdx == -1 || obs.ts < oldestNonRestartTs {
					oldestNonRestartTs = obs.ts
					oldestNonRestartIdx = i
				}
			}
		}

		// Decision: what to prune
		var toRemoveIdx int
		if oldestNonRestartIdx != -1 {
			// Have a non-restart to prune - use it
			toRemoveIdx = oldestNonRestartIdx
		} else if hasCheckpoint {
			// All events are restarts, but we have a checkpoint - safe to prune oldest
			toRemoveIdx = oldestAnyIdx
		} else {
			// All events are restarts and NO checkpoint - DON'T prune, skip this step
			// This preserves restart history until a checkpoint captures it
			toRemoveIdx = -1
		}

		if toRemoveIdx >= 0 {
			toRemove := existingObs[toRemoveIdx]
			newEvents := make([]SyncEvent, 0, len(l.Events)-1)
			for i, existing := range l.Events {
				if i != toRemove.idx {
					newEvents = append(newEvents, existing)
				}
			}
			l.Events = newEvents
			delete(l.eventIDs, toRemove.id)
			// Increment version - structure has changed, projections need to reset
			l.version++
		}
	}

	// Validate and add the new event
	if e.ID == "" {
		e.ComputeID()
	}
	if !e.IsValid() {
		l.mu.Unlock()
		return false
	}
	if l.eventIDs[e.ID] {
		l.mu.Unlock()
		return false
	}

	l.Events = append(l.Events, e)
	l.eventIDs[e.ID] = true
	l.mu.Unlock()

	// Notify listeners outside of lock to avoid deadlocks
	l.notifyListeners(e)

	return true
}

// --- Consensus from Events ---

// OpinionData holds derived consensus values for a subject
type OpinionData struct {
	StartTime   int64
	Restarts    int64
	LastRestart int64
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

// eventPruningPriority returns the pruning priority for an event.
// Lower numbers = more important (pruned last).
// Priority 0 events are NEVER pruned (critical history).
func eventPruningPriority(e SyncEvent) int {
	// Critical (priority 0): checkpoint events - historical anchors, NEVER prune
	if e.Service == ServiceCheckpoint {
		return 0 // Never prune
	}

	// Critical (priority 0): restart and first-seen observations
	// These establish nara identity and history - NEVER prune
	if e.Service == ServiceObservation && e.Observation != nil {
		switch e.Observation.Type {
		case "restart", "first-seen":
			return 0 // Never prune
		case "status-change":
			return 1 // High priority
		}
	}

	// High priority (1): hey_there and chau events - direct announcements from naras
	// These are authoritative primary signals of online/offline status
	if e.Service == ServiceHeyThere || e.Service == ServiceChau {
		return 1
	}

	// Medium priority (2): social events (teases, gossip)
	if e.Service == ServiceSocial {
		return 2
	}

	// Medium-low priority (3): seen events - secondary observations of online status
	// Less authoritative than direct hey_there/chau announcements
	if e.Service == ServiceSeen {
		return 3
	}

	// Low priority (4): ping observations - ephemeral, can be recalculated
	if e.Service == ServicePing {
		return 4
	}

	// Default medium priority
	return 2
}

// Prune removes old events to stay within MaxEvents limit.
// Uses priority-based pruning: seen events pruned first, then pings, then social,
// then status-change. Critical events (restart, first-seen, checkpoint) are NEVER pruned.
func (l *SyncLedger) Prune() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneUnlocked()
}

// pruneUnlocked is the internal pruning logic, called with lock already held.
func (l *SyncLedger) pruneUnlocked() {
	if len(l.Events) <= l.MaxEvents {
		return
	}

	// Sort by priority (higher number = prune first), then by timestamp (oldest first)
	// Events from unknown naras (no public key) are pruned first
	sort.Slice(l.Events, func(i, j int) bool {
		// Check if events are from unknown naras (prune first)
		unknownI := l.isEventFromUnknownNara(l.Events[i])
		unknownJ := l.isEventFromUnknownNara(l.Events[j])
		if unknownI != unknownJ {
			return unknownI // Unknown nara events sorted to front (pruned first)
		}

		// Then sort by priority
		pi, pj := eventPruningPriority(l.Events[i]), eventPruningPriority(l.Events[j])
		if pi != pj {
			return pi > pj // Higher priority number = prune first
		}
		return l.Events[i].Timestamp < l.Events[j].Timestamp // Oldest first within same priority
	})

	// Calculate how many to remove, but never remove priority 0 events
	toRemove := len(l.Events) - l.MaxEvents
	actualRemoved := 0

	for i := 0; i < toRemove && i < len(l.Events); i++ {
		if eventPruningPriority(l.Events[i]) == 0 {
			// Stop - we've reached critical events
			break
		}
		actualRemoved++
	}

	if actualRemoved == 0 {
		return
	}

	removed := l.Events[:actualRemoved]
	l.Events = l.Events[actualRemoved:]

	// Update eventIDs map
	for _, e := range removed {
		delete(l.eventIDs, e.ID)
	}

	// Re-sort by timestamp for normal operation
	sort.Slice(l.Events, func(i, j int) bool {
		return l.Events[i].Timestamp < l.Events[j].Timestamp
	})

	// Increment version - structure has changed, projections need to reset
	l.version++
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
	Timestamp int64       `json:"ts,omitempty"`  // When response was generated (Unix SECONDS, not nanoseconds)
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

// --- Personality-Aware Methods ---

// socialEventIsMeaningful decides if a personality cares enough to store the event
// This is the filter logic from SocialLedger.eventIsMeaningful, adapted for SyncEvent
func socialEventIsMeaningful(payload *SocialEventPayload, personality NaraPersonality) bool {
	if payload == nil {
		return false
	}

	// High chill naras don't bother with random jabs
	if personality.Chill > 70 && payload.Reason == ReasonRandom {
		return false
	}

	// Handle observation events specially
	if payload.Type == "observation" {
		// Everyone keeps journey-timeout (reliability matters!)
		if payload.Reason == ReasonJourneyTimeout {
			return true
		}

		// Very chill naras skip routine online/offline
		if personality.Chill > 85 {
			if payload.Reason == ReasonOnline || payload.Reason == ReasonOffline {
				return false
			}
		}

		// Low sociability naras skip journey-pass/complete
		if personality.Sociability < 30 {
			if payload.Reason == ReasonJourneyPass || payload.Reason == ReasonJourneyComplete {
				return false
			}
		}

		return true
	}

	// Very chill naras only care about significant events (for non-observation types)
	if personality.Chill > 85 {
		// Only store comebacks and high-restarts (significant events)
		if payload.Reason != ReasonComeback && payload.Reason != ReasonHighRestarts {
			return false
		}
	}

	// Highly agreeable naras don't like storing negative drama
	if personality.Agreeableness > 80 && payload.Reason == ReasonTrendAbandon {
		return false // "who am I to judge their choices"
	}

	// Low sociability naras are less interested in others' drama
	if personality.Sociability < 20 {
		// Only store if it seems important (not random)
		if payload.Reason == ReasonRandom {
			return false
		}
	}

	return true
}

// AddSocialEventFiltered adds a social event if personality finds it meaningful
func (l *SyncLedger) AddSocialEventFiltered(e SyncEvent, personality NaraPersonality) bool {
	// Only filter social events
	if e.Service != ServiceSocial || e.Social == nil {
		return l.AddEvent(e)
	}

	// Personality-based filtering: do I even care about this?
	if !socialEventIsMeaningful(e.Social, personality) {
		return false
	}

	return l.AddEvent(e)
}

// EventWeight calculates how much an event matters based on personality AND event properties
// Implements "strong opinions weakly held" - recent events matter more, old ones fade
func (l *SyncLedger) EventWeight(event SyncEvent, personality NaraPersonality) float64 {
	// Only social events have personality-aware weight
	if event.Service != ServiceSocial || event.Social == nil {
		return 1.0
	}

	payload := event.Social

	// Base weight
	weight := 1.0

	// High sociability cares more about social events
	weight += float64(personality.Sociability) / 200.0 // +0 to +0.5

	// High chill diminishes drama
	weight -= float64(personality.Chill) / 400.0 // -0 to -0.25

	// Event reason affects weight differently per personality
	switch payload.Reason {
	case ReasonHighRestarts:
		// Technical stuff - less interesting to highly social naras
		if personality.Sociability > 70 {
			weight *= 0.7
		}
	case ReasonComeback:
		// Social naras love comeback drama
		if personality.Sociability > 60 {
			weight *= 1.4
		}
	case ReasonTrendAbandon:
		// Agreeable naras don't like judging trend choices
		if personality.Agreeableness > 70 {
			weight *= 0.5
		}
		// But social naras find it interesting
		if personality.Sociability > 60 {
			weight *= 1.2
		}
	case ReasonRandom:
		// Chill naras don't care about random jabs
		if personality.Chill > 60 {
			weight *= 0.3
		}
		// Highly agreeable naras find random teasing a bit much
		if personality.Agreeableness > 70 {
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
	switch payload.Type {
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
		chillModifier := 1.0 + float64(50-personality.Chill)/100.0 // 0.5 to 1.5

		// Sociable naras remember social stuff longer (up to 30% bonus)
		socModifier := 1.0 + float64(personality.Sociability)/333.0 // 1.0 to 1.3

		halfLife := baseHalfLife * chillModifier * socModifier
		decayFactor := 1.0 / (1.0 + float64(age)/halfLife)
		weight *= decayFactor
	}

	if weight < 0.1 {
		weight = 0.1
	}

	return weight
}

// GetTeaseCounts returns objective count of teases per actor (no personality influence)
func (l *SyncLedger) GetTeaseCounts() map[string]int {
	l.mu.RLock()
	defer l.mu.RUnlock()

	counts := make(map[string]int)
	for _, e := range l.Events {
		if e.Service == ServiceSocial && e.Social != nil && e.Social.Type == "tease" {
			counts[e.Social.Actor]++
		}
	}
	return counts
}

// GetTeaseCountsReceived returns count of teases received per target
func (l *SyncLedger) GetTeaseCountsReceived() map[string]int {
	l.mu.RLock()
	defer l.mu.RUnlock()

	counts := make(map[string]int)
	for _, e := range l.Events {
		if e.Service == ServiceSocial && e.Social != nil && e.Social.Type == "tease" {
			counts[e.Social.Target]++
		}
	}
	return counts
}

// GetEventCountsByService returns event counts grouped by service type
func (l *SyncLedger) GetEventCountsByService() map[string]int {
	l.mu.RLock()
	defer l.mu.RUnlock()

	counts := make(map[string]int)
	for _, e := range l.Events {
		counts[e.Service]++
	}
	return counts
}

// GetCriticalEventCount returns the number of critical (never-pruned) events
func (l *SyncLedger) GetCriticalEventCount() int {
	l.mu.RLock()
	defer l.mu.RUnlock()

	count := 0
	for _, e := range l.Events {
		if eventPruningPriority(e) == 0 {
			count++
		}
	}
	return count
}

// GetRecentSocialEvents returns the N most recent social events
func (l *SyncLedger) GetRecentSocialEvents(n int) []SyncEvent {
	l.mu.RLock()
	defer l.mu.RUnlock()

	// Collect social events
	var social []SyncEvent
	for _, e := range l.Events {
		if e.Service == ServiceSocial {
			social = append(social, e)
		}
	}

	// Sort by timestamp descending
	sort.Slice(social, func(i, j int) bool {
		return social[i].Timestamp > social[j].Timestamp
	})

	if n > len(social) {
		n = len(social)
	}
	return social[:n]
}

// GetSocialEventsAbout returns all social events where the given name is the target
func (l *SyncLedger) GetSocialEventsAbout(name string) []SyncEvent {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var result []SyncEvent
	for _, e := range l.Events {
		if e.Service == ServiceSocial && e.Social != nil && e.Social.Target == name {
			result = append(result, e)
		}
	}
	return result
}
