package nara

import (
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/eljojo/nara/identity"
	"github.com/eljojo/nara/types"
	"github.com/sirupsen/logrus"
)

type ObservationRateLimit struct {
	SubjectCounts map[types.NaraName][]int64 // subject → timestamps of events
	WindowSec     int64                      // Time window in seconds
	MaxEvents     int                        // Max events per subject per window
	mu            sync.Mutex
}

// NewObservationRateLimit creates a new rate limiter
func NewObservationRateLimit() *ObservationRateLimit {
	return &ObservationRateLimit{
		SubjectCounts: make(map[types.NaraName][]int64),
		WindowSec:     300, // 5 minutes
		MaxEvents:     10,  // max 10 events per subject per 5 min
	}
}

// CheckAndAdd checks if an event would exceed rate limit, and adds it if not
// Returns true if the event is allowed, false if rate limit exceeded
func (r *ObservationRateLimit) CheckAndAdd(subject types.NaraName, timestamp int64) bool {
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
	isUnknownNara func(name types.NaraName) bool

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
func (l *SyncLedger) SetUnknownNaraChecker(checker func(name types.NaraName) bool) {
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
	var names []types.NaraName

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
// shouldFilterCheckpoint returns true if a checkpoint event should be filtered out
// Filters old checkpoints created before the bugfix that added LastRestart and other fields
func shouldFilterCheckpoint(e SyncEvent) bool {
	if e.Service != ServiceCheckpoint || e.Checkpoint == nil {
		return false // Not a checkpoint, don't filter
	}

	// Filter checkpoints created before or at the cutoff time from emitter "r2d2"
	return e.Checkpoint.AsOfTime <= CheckpointCutoffTime && e.Checkpoint.Subject == "r2d2"
}

// FilterEventsForIngestion filters a slice of events before adding them to the ledger
// This removes old buggy checkpoints and any other events that should not be ingested
func FilterEventsForIngestion(events []SyncEvent) []SyncEvent {
	filtered := make([]SyncEvent, 0, len(events))
	var filteredCount int
	for _, e := range events {
		if shouldFilterCheckpoint(e) {
			filteredCount++
		} else {
			filtered = append(filtered, e)
		}
	}
	if filteredCount > 0 {
		logrus.Debugf("🧹 Filtered %d old checkpoint(s) from batch of %d events", filteredCount, len(events))
	}
	return filtered
}

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

	// Filter out old buggy checkpoints (created before bugfix deployment)
	if shouldFilterCheckpoint(e) {
		l.mu.Unlock()
		if e.Checkpoint != nil {
			logrus.Debugf("🧹 Filtered old checkpoint for %s (AsOfTime=%d, cutoff=%d)",
				e.Checkpoint.Subject, e.Checkpoint.AsOfTime, CheckpointCutoffTime)
		}
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
func (l *SyncLedger) GetSocialEventsForSubjects(subjects []types.NaraName) []SocialEvent {
	l.mu.RLock()
	defer l.mu.RUnlock()

	subjectSet := make(map[types.NaraName]bool)
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
func (l *SyncLedger) AddPingObservation(observer types.NaraName, target types.NaraName, rtt float64) bool {
	return l.AddEvent(NewPingSyncEvent(observer, target, rtt))
}

// MaxPingsPerTarget limits how many ping observations to keep per target (receiver)
// This prevents the ledger from being saturated with stale ping data while keeping useful history.
const MaxPingsPerTarget = 5

// AddSignedPingObservation adds a signed ping observation
func (l *SyncLedger) AddSignedPingObservation(observer types.NaraName, target types.NaraName, rtt float64, emitter types.NaraName, keypair identity.NaraKeypair) bool {
	return l.AddEvent(NewSignedPingSyncEvent(observer, target, rtt, emitter, keypair))
}

// AddSignedPingObservationWithReplace adds a signed ping observation, keeping only the last N per target
func (l *SyncLedger) AddSignedPingObservationWithReplace(observer types.NaraName, target types.NaraName, rtt float64, emitter types.NaraName, keypair identity.NaraKeypair) bool {
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
func (l *SyncLedger) GetEventsInvolving(name types.NaraName) []SyncEvent {
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
func (l *SyncLedger) RemoveEventsFor(name types.NaraName) int {
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
func (l *SyncLedger) eventInvolvesNara(e SyncEvent, name types.NaraName) bool {
	if e.Emitter == name {
		return true
	}
	switch e.Service {
	case ServiceCheckpoint:
		// Remove checkpoints where this nara is the subject (checkpoint about them)
		if e.Checkpoint != nil && e.Checkpoint.Subject == name {
			return true
		}
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
func (l *SyncLedger) GetObservationEventsAbout(subject types.NaraName) []SyncEvent {
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
func (l *SyncLedger) GetBackfillEvent(subject types.NaraName) *ObservationEventPayload {
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

// GetLatestCheckpointID returns the ID of the most recent checkpoint for a subject
// Used for v2 checkpoint chaining - returns empty string if no checkpoint exists
func (l *SyncLedger) GetLatestCheckpointID(subject types.NaraName) string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var latestCheckpoint *SyncEvent
	var latestTime int64

	for i := range l.Events {
		e := &l.Events[i]
		if e.Service == ServiceCheckpoint &&
			e.Checkpoint != nil &&
			e.Checkpoint.Subject == subject {
			if e.Checkpoint.AsOfTime > latestTime {
				latestTime = e.Checkpoint.AsOfTime
				latestCheckpoint = e
			}
		}
	}

	if latestCheckpoint != nil {
		return latestCheckpoint.ID
	}
	return ""
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

	// Content-based deduplication for observation events (only if requested)
	// This is checked FIRST to avoid even counting duplicates in per-pair limits
	if withDedup && e.Observation != nil {
		for _, existing := range l.Events {
			if existing.Service == ServiceObservation && existing.Observation != nil {

				// Deduplicate restart events by subject + restart_num + start_time
				// Multiple observers can report the same restart, but we only keep one
				if e.Observation.Type == "restart" && existing.Observation.Type == "restart" {
					if existing.Observation.Subject == e.Observation.Subject &&
						existing.Observation.RestartNum == e.Observation.RestartNum &&
						existing.Observation.StartTime == e.Observation.StartTime {
						l.mu.Unlock()
						return false // Duplicate restart event
					}
				}

				// Deduplicate first-seen events per observer->subject pair
				// Only log first-seen once per observer seeing a subject
				if e.Observation.Type == "first-seen" && existing.Observation.Type == "first-seen" {
					if existing.Observation.Observer == e.Observation.Observer &&
						existing.Observation.Subject == e.Observation.Subject {
						l.mu.Unlock()
						return false // Duplicate first-seen event
					}
				}
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
	TotalUptime int64
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
func (l *SyncLedger) GetEventsForSync(services []string, subjects []types.NaraName, sinceTime int64, sliceIndex, sliceTotal, maxEvents int) []SyncEvent {
	l.mu.RLock()
	defer l.mu.RUnlock()

	// Build filter sets
	serviceSet := make(map[string]bool)
	filterByService := len(services) > 0
	for _, s := range services {
		serviceSet[s] = true
	}

	subjectSet := make(map[types.NaraName]bool)
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

// GetEventsPage returns events for cursor-based pagination (mode: "page")
// Returns events oldest-first with nextCursor for deterministic complete retrieval
// This is used for backup and checkpoint sync where completeness is required
func (l *SyncLedger) GetEventsPage(cursor string, pageSize int, services []string, subjects []types.NaraName) (events []SyncEvent, nextCursor string) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	// Parse cursor (timestamp of last event returned)
	var sinceTime int64
	if cursor != "" {
		parsed, err := strconv.ParseInt(cursor, 10, 64)
		if err == nil {
			sinceTime = parsed
		}
	}

	// Build filter sets
	serviceSet := make(map[string]bool)
	filterByService := len(services) > 0
	for _, s := range services {
		serviceSet[s] = true
	}

	subjectSet := make(map[types.NaraName]bool)
	filterBySubject := len(subjects) > 0
	for _, s := range subjects {
		subjectSet[s] = true
	}

	// Filter events after cursor timestamp
	var filtered []SyncEvent
	for _, e := range l.Events {
		// Filter by cursor (events strictly after cursor timestamp)
		if e.Timestamp <= sinceTime {
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

	// Sort oldest first for deterministic pagination
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Timestamp < filtered[j].Timestamp
	})

	// Apply page size (OLDEST events, not newest!)
	hasMore := false
	if pageSize > 0 && len(filtered) > pageSize {
		hasMore = true
		filtered = filtered[:pageSize]
	}

	// Generate next cursor if we returned a full page (might be more events)
	// If we returned fewer than pageSize, this is the last page (cursor stays empty)
	// This prevents clients from making unnecessary extra calls
	if hasMore || (pageSize > 0 && len(filtered) == pageSize) {
		nextCursor = strconv.FormatInt(filtered[len(filtered)-1].Timestamp, 10)
	}

	return filtered, nextCursor
}

// GetRecentEvents returns the most recent N events (mode: "recent")
// This is used for web UI event browsing where only recent activity is needed
func (l *SyncLedger) GetRecentEvents(limit int, services []string, subjects []types.NaraName) []SyncEvent {
	l.mu.RLock()
	defer l.mu.RUnlock()

	// Build filter sets
	serviceSet := make(map[string]bool)
	filterByService := len(services) > 0
	for _, s := range services {
		serviceSet[s] = true
	}

	subjectSet := make(map[types.NaraName]bool)
	filterBySubject := len(subjects) > 0
	for _, s := range subjects {
		subjectSet[s] = true
	}

	// Filter events
	var filtered []SyncEvent
	for _, e := range l.Events {
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

	// Sort newest first
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Timestamp > filtered[j].Timestamp
	})

	// Apply limit
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}

	return filtered
}

// MergeEvents adds events from another source (for sync/backfill)
// Events are filtered before ingestion to remove old buggy checkpoints
func (l *SyncLedger) MergeEvents(events []SyncEvent) int {
	// Filter events before ingestion
	events = FilterEventsForIngestion(events)

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
func (l *SyncLedger) GetLatestPingTo(target types.NaraName) *PingObservation {
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
func (l *SyncLedger) GetPingsBetween(a, b types.NaraName) []PingObservation {
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
func (l *SyncLedger) GetAverageRTT(target types.NaraName) float64 {
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
func (l *SyncLedger) GetTeaseCounts() map[types.NaraName]int {
	l.mu.RLock()
	defer l.mu.RUnlock()

	counts := make(map[types.NaraName]int)
	for _, e := range l.Events {
		if e.Service == ServiceSocial && e.Social != nil && e.Social.Type == "tease" {
			counts[e.Social.Actor]++
		}
	}
	return counts
}

// GetTeaseCountsReceived returns count of teases received per target
func (l *SyncLedger) GetTeaseCountsReceived() map[types.NaraName]int {
	l.mu.RLock()
	defer l.mu.RUnlock()

	counts := make(map[types.NaraName]int)
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
func (l *SyncLedger) GetSocialEventsAbout(name types.NaraName) []SyncEvent {
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
