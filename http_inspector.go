package nara

import (
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/eljojo/nara/types"
)

// Inspector API Endpoints
// These endpoints provide comprehensive access to events, checkpoints, and projections
// for the inspector web UI.

// inspectorEventsHandler - GET /api/inspector/events
// Returns filtered event list with pagination
func (ln *LocalNara) inspectorEventsHandler(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	query := r.URL.Query()
	service := query.Get("service")
	subject := types.NaraName(query.Get("subject"))
	afterStr := query.Get("after")
	beforeStr := query.Get("before")
	limitStr := query.Get("limit")
	offsetStr := query.Get("offset")

	// Default values
	limit := 100
	offset := 0
	var after, before int64

	// Parse limit
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 1000 {
			limit = l
		}
	}

	// Parse offset
	if offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	// Parse timestamps (nanoseconds)
	if afterStr != "" {
		after, _ = strconv.ParseInt(afterStr, 10, 64)
	}
	if beforeStr != "" {
		before, _ = strconv.ParseInt(beforeStr, 10, 64)
	}

	// Get events from ledger
	ln.SyncLedger.mu.RLock()
	allEvents := ln.SyncLedger.Events
	ln.SyncLedger.mu.RUnlock()

	// Filter events
	var filteredEvents []SyncEvent
	for _, event := range allEvents {
		// Filter by service
		if service != "" && event.Service != service {
			continue
		}

		// Filter by subject (emitter or target)
		if subject != "" {
			eventActor := event.GetActor()
			eventTarget := event.GetTarget()
			if event.Emitter != subject && eventActor != subject && eventTarget != subject {
				continue
			}
		}

		// Filter by time range
		if after > 0 && event.Timestamp < after {
			continue
		}
		if before > 0 && event.Timestamp > before {
			continue
		}

		filteredEvents = append(filteredEvents, event)
	}

	// Sort by timestamp, newest first
	sort.Slice(filteredEvents, func(i, j int) bool {
		return filteredEvents[i].Timestamp > filteredEvents[j].Timestamp
	})

	// Apply pagination
	total := len(filteredEvents)
	start := offset
	end := offset + limit

	if start >= total {
		start = total
		end = total
	} else if end > total {
		end = total
	}

	paginatedEvents := filteredEvents[start:end]
	hasMore := end < total

	// Format response
	type EventResponse struct {
		ID        string            `json:"id"`
		Timestamp int64             `json:"timestamp"`
		Service   string            `json:"service"`
		Emitter   types.NaraName          `json:"emitter"`
		EmitterID types.NaraID            `json:"emitter_id"`
		Signed    bool              `json:"signed"`
		UIFormat  map[string]string `json:"ui_format,omitempty"`
	}

	events := make([]EventResponse, len(paginatedEvents))
	for i, event := range paginatedEvents {
		events[i] = EventResponse{
			ID:        event.ID,
			Timestamp: event.Timestamp,
			Service:   event.Service,
			Emitter:   event.Emitter,
			EmitterID: event.EmitterID,
			Signed:    event.IsSigned(),
		}

		// Get UI format if available
		if uiFormat := getEventUIFormat(&event); uiFormat != nil {
			events[i].UIFormat = uiFormat
		}
	}

	response := map[string]interface{}{
		"events":   events,
		"total":    total,
		"has_more": hasMore,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logrus.WithError(err).Warn("Failed to encode response")
	}
}

// getEventUIFormat extracts UIFormat from event payload
func getEventUIFormat(event *SyncEvent) map[string]string {
	var uiFormat map[string]string

	switch event.Service {
	case ServiceSocial:
		if event.Social != nil {
			uiFormat = event.Social.UIFormat()
		}
	case ServicePing:
		if event.Ping != nil {
			uiFormat = event.Ping.UIFormat()
		}
	case ServiceObservation:
		if event.Observation != nil {
			uiFormat = event.Observation.UIFormat()
		}
	case ServiceHeyThere:
		if event.HeyThere != nil {
			uiFormat = event.HeyThere.UIFormat()
		}
	case ServiceChau:
		if event.Chau != nil {
			uiFormat = event.Chau.UIFormat()
		}
	case ServiceSeen:
		if event.Seen != nil {
			uiFormat = event.Seen.UIFormat()
		}
	case ServiceCheckpoint:
		if event.Checkpoint != nil {
			// Checkpoints don't have a built-in UIFormat, create one
			uiFormat = map[string]string{
				"icon":   "📸",
				"text":   event.Checkpoint.Subject.String() + " checkpoint",
				"detail": event.Checkpoint.LogFormat(),
			}
		}
	}

	return uiFormat
}

// inspectorCheckpointsHandler - GET /api/inspector/checkpoints
// Returns list of all checkpoints with summary info
func (ln *LocalNara) inspectorCheckpointsHandler(w http.ResponseWriter, r *http.Request) {
	// Get all checkpoint events from ledger
	ln.SyncLedger.mu.RLock()
	events := ln.SyncLedger.GetEventsByService(ServiceCheckpoint)
	ln.SyncLedger.mu.RUnlock()

	type CheckpointSummary struct {
		Subject              types.NaraName `json:"subject"`
		SubjectID            types.NaraID   `json:"subject_id"`
		AsOfTime             int64    `json:"as_of_time"`
		Round                int      `json:"round"`
		Restarts             int64    `json:"restarts"`
		TotalUptime          int64    `json:"total_uptime"`
		StartTime            int64    `json:"start_time"`
		VoterCount           int      `json:"voter_count"`
		VerifiedCount        int      `json:"verified_count"`
		KnownCount           int      `json:"known_count"`
		Verified             bool     `json:"verified"`
		Version              int      `json:"version"`                          // v2: checkpoint version
		PreviousCheckpointID string   `json:"previous_checkpoint_id,omitempty"` // v2: chain link
	}

	checkpoints := make([]CheckpointSummary, 0)
	seen := make(map[types.NaraName]bool) // Track latest checkpoint per subject

	// Process checkpoints (latest first)
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if event.Checkpoint == nil {
			continue
		}

		// Only include latest checkpoint per subject
		if seen[event.Checkpoint.Subject] {
			continue
		}
		seen[event.Checkpoint.Subject] = true

		checkpoint := event.Checkpoint

		// Verify signatures
		var result CheckpointVerificationResult
		if ln.Network.checkpointService != nil {
			result = ln.Network.checkpointService.verifyCheckpointSignatures(checkpoint)
		}

		checkpoints = append(checkpoints, CheckpointSummary{
			Subject:              checkpoint.Subject,
			SubjectID:            checkpoint.SubjectID,
			AsOfTime:             checkpoint.AsOfTime,
			Round:                checkpoint.Round,
			Restarts:             checkpoint.Observation.Restarts,
			TotalUptime:          checkpoint.Observation.TotalUptime,
			StartTime:            checkpoint.Observation.StartTime,
			VoterCount:           len(checkpoint.VoterIDs),
			VerifiedCount:        result.ValidCount,
			KnownCount:           result.KnownCount,
			Verified:             result.Valid,
			Version:              checkpoint.Version,
			PreviousCheckpointID: checkpoint.PreviousCheckpointID,
		})
	}

	response := map[string]interface{}{
		"checkpoints": checkpoints,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logrus.WithError(err).Warn("Failed to encode response")
	}
}

// inspectorCheckpointDetailHandler - GET /api/inspector/checkpoint/{subject}
// Returns detailed checkpoint for a specific nara with voter breakdown
func (ln *LocalNara) inspectorCheckpointDetailHandler(w http.ResponseWriter, r *http.Request) {
	subject := strings.TrimPrefix(r.URL.Path, "/api/inspector/checkpoint/")
	if subject == "" {
		http.Error(w, "subject required", http.StatusBadRequest)
		return
	}

	// Get latest checkpoint event for subject
	checkpointEvent := ln.SyncLedger.GetCheckpointEvent(types.NaraName(subject))
	if checkpointEvent == nil {
		http.Error(w, "checkpoint not found", http.StatusNotFound)
		return
	}
	checkpoint := checkpointEvent.Checkpoint

	// Verify each voter signature
	type VoterInfo struct {
		VoterID           types.NaraID   `json:"voter_id"`
		VoterName         types.NaraName `json:"voter_name"`
		Signature         string   `json:"signature"`
		Verified          bool     `json:"verified"`
		VerificationError *string  `json:"verification_error"`
	}

	voters := make([]VoterInfo, len(checkpoint.VoterIDs))
	for i, voterID := range checkpoint.VoterIDs {
		signature := ""
		if i < len(checkpoint.Signatures) {
			signature = checkpoint.Signatures[i]
		}

		var voterName types.NaraName
		if voterID == checkpoint.SubjectID {
			voterName = checkpoint.Subject
		} else {
			// Try to find voter name from network
			ln.mu.Lock()
			for name, nara := range ln.Network.Neighbourhood {
				if nara.Status.ID == voterID {
					voterName = name
					break
				}
			}
			ln.mu.Unlock()
		}

		// Verify signature
		verified := false
		var verificationError *string
		if ln.Network.checkpointService != nil {
			pubKey := ln.Network.getPublicKeyForNaraID(voterID)
			if pubKey == nil {
				errMsg := "public key not found"
				verificationError = &errMsg
			} else {
				// Build attestation for verification
				version := checkpoint.Version
				if version == 0 {
					version = 1
				}
				attestation := Attestation{
					Version:     version,
					Subject:     checkpoint.Subject,
					SubjectID:   checkpoint.SubjectID,
					Observation: checkpoint.Observation,
					Attester:    voterName,
					AttesterID:  voterID,
					AsOfTime:    checkpoint.AsOfTime,
				}

				signableContent := attestation.SignableContent()
				if VerifySignatureBase64(pubKey, []byte(signableContent), signature) {
					verified = true
				} else {
					errMsg := "signature verification failed"
					verificationError = &errMsg
				}
			}
		}

		voters[i] = VoterInfo{
			VoterID:           voterID,
			VoterName:         voterName,
			Signature:         signature,
			Verified:          verified,
			VerificationError: verificationError,
		}
	}

	// Build response
	isSelfAttestation := false
	for _, voterID := range checkpoint.VoterIDs {
		if voterID == checkpoint.SubjectID {
			isSelfAttestation = true
			break
		}
	}

	verifiedCount := 0
	for _, v := range voters {
		if v.Verified {
			verifiedCount++
		}
	}

	response := map[string]interface{}{
		"event":  checkpointEvent,
		"voters": voters,
		"summary": map[string]interface{}{
			"total_voters":        len(checkpoint.VoterIDs),
			"verified_voters":     verifiedCount,
			"is_self_attestation": isSelfAttestation,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logrus.WithError(err).Warn("Failed to encode response")
	}
}

// inspectorProjectionsHandler - GET /api/inspector/projections
// Returns current state of all projections
func (ln *LocalNara) inspectorProjectionsHandler(w http.ResponseWriter, r *http.Request) {
	if ln.Projections == nil {
		http.Error(w, "projections not available", http.StatusServiceUnavailable)
		return
	}

	// Get all online statuses
	onlineStatusMap := make(map[types.NaraName]interface{})
	if ln.Projections.OnlineStatus() != nil {
		statuses := ln.Projections.OnlineStatus().GetAllStatuses()
		for name, status := range statuses {
			state := ln.Projections.OnlineStatus().GetState(name)
			totalUptime := ln.SyncLedger.DeriveTotalUptime(name)
			onlineStatusMap[name] = map[types.NaraName]interface{}{
				"status":          status,
				"last_event_time": state.LastEventTime,
				"last_event_type": state.LastEventType,
				"observer":        state.Observer,
				"total_uptime":    totalUptime,
			}
		}
	}

	// Get clout scores
	cloutMap := make(map[types.NaraName]float64)
	if ln.Projections.Clout() != nil {
		cloutScores := ln.Projections.Clout().DeriveClout(ln.Soul, ln.Me.Status.Personality)
		for name, score := range cloutScores {
			cloutMap[name] = score
		}
	}

	// Get opinion consensus for all known naras
	opinionsMap := make(map[types.NaraName]interface{})
	if ln.Projections.Opinion() != nil {
		// Get all unique subjects from neighbourhood
		ln.mu.Lock()
		subjects := make([]types.NaraName, 0, len(ln.Network.Neighbourhood))
		for name := range ln.Network.Neighbourhood {
			subjects = append(subjects, name)
		}
		ln.mu.Unlock()

		// Add self
		subjects = append(subjects, ln.Me.Name)

		// Derive opinion for each subject
		for _, subject := range subjects {
			opinion := ln.Projections.Opinion().DeriveOpinion(subject)
			observations := ln.Projections.Opinion().GetObservationsFor(subject)

			consensus := "none"
			if len(observations) > 5 {
				consensus = "strong"
			} else if len(observations) > 2 {
				consensus = "moderate"
			} else if len(observations) > 0 {
				consensus = "weak"
			}

			opinionsMap[subject] = map[string]interface{}{
				"start_time":        opinion.StartTime,
				"restart_num":       opinion.Restarts,
				"last_restart":      opinion.LastRestart,
				"observation_count": len(observations),
				"consensus":         consensus,
			}
		}
	}

	response := map[string]interface{}{
		"online_status": onlineStatusMap,
		"clout":         cloutMap,
		"opinions":      opinionsMap,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logrus.WithError(err).Warn("Failed to encode response")
	}
}

// inspectorProjectionDetailHandler - GET /api/inspector/projection/{type}/{subject}
// Returns detailed projection for a nara with source events
func (ln *LocalNara) inspectorProjectionDetailHandler(w http.ResponseWriter, r *http.Request) {
	// Parse path: /api/inspector/projection/{type}/{subject}
	path := strings.TrimPrefix(r.URL.Path, "/api/inspector/projection/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 {
		http.Error(w, "invalid path format: expected /api/inspector/projection/{type}/{subject}", http.StatusBadRequest)
		return
	}

	projectionType := parts[0]
	subject := types.NaraName(parts[1])

	if ln.Projections == nil {
		http.Error(w, "projections not available", http.StatusServiceUnavailable)
		return
	}

	var response map[string]interface{}

	switch projectionType {
	case "online_status":
		if ln.Projections.OnlineStatus() == nil {
			http.Error(w, "online status projection not available", http.StatusServiceUnavailable)
			return
		}

		status := ln.Projections.OnlineStatus().GetStatus(subject)
		state := ln.Projections.OnlineStatus().GetState(subject)

		// Find source events (last few events that determined status)
		ln.SyncLedger.mu.RLock()
		events := ln.SyncLedger.GetObservationEventsAbout(subject)
		ln.SyncLedger.mu.RUnlock()

		// Get last 5 relevant events
		sourceEvents := make([]map[string]interface{}, 0)
		count := 0
		for i := len(events) - 1; i >= 0 && count < 5; i-- {
			event := events[i]
			sourceEvents = append(sourceEvents, map[string]interface{}{
				"id":              event.ID,
				"timestamp":       event.Timestamp,
				"service":         event.Service,
				"emitter":         event.Emitter,
				"last_event_type": state.LastEventType,
			})
			count++
		}

		response = map[string]interface{}{
			"type":    "online_status",
			"subject": subject,
			"derived_state": map[string]interface{}{
				"status":          status,
				"last_event_time": state.LastEventTime,
				"last_event_type": state.LastEventType,
				"observer":        state.Observer,
			},
			"source_events": sourceEvents,
		}

	case "clout":
		if ln.Projections.Clout() == nil {
			http.Error(w, "clout projection not available", http.StatusServiceUnavailable)
			return
		}

		cloutScores := ln.Projections.Clout().DeriveClout(ln.Soul, ln.Me.Status.Personality)
		cloutScore := cloutScores[subject]

		// Get social events about subject
		ln.SyncLedger.mu.RLock()
		allEvents := ln.SyncLedger.GetEventsByService(ServiceSocial)
		ln.SyncLedger.mu.RUnlock()

		sourceEvents := make([]map[string]interface{}, 0)
		for _, event := range allEvents {
			if event.Social == nil {
				continue
			}
			if event.Social.Target == subject || event.Social.Actor == subject {
				sourceEvents = append(sourceEvents, map[string]interface{}{
					"id":        event.ID,
					"timestamp": event.Timestamp,
					"type":      event.Social.Type,
					"actor":     event.Social.Actor,
					"target":    event.Social.Target,
					"reason":    event.Social.Reason,
				})
			}
		}

		response = map[string]interface{}{
			"type":    "clout",
			"subject": subject,
			"derived_state": map[string]interface{}{
				"clout_score": cloutScore,
			},
			"source_events": sourceEvents,
		}

	case "opinion":
		if ln.Projections.Opinion() == nil {
			http.Error(w, "opinion projection not available", http.StatusServiceUnavailable)
			return
		}

		opinion := ln.Projections.Opinion().DeriveOpinion(subject)
		observations := ln.Projections.Opinion().GetObservationsFor(subject)

		sourceEvents := make([]map[string]interface{}, len(observations))
		for i, obs := range observations {
			sourceEvents[i] = map[string]interface{}{
				"id":              "", // observations don't have event IDs directly
				"observer":        obs.Observer,
				"type":            obs.Type,
				"start_time":      obs.StartTime,
				"restart_num":     obs.RestartNum,
				"last_restart":    obs.LastRestart,
				"observer_uptime": obs.ObserverUptime,
			}
		}

		// Determine outliers (simplified - just count if observations differ significantly)
		outliersRemoved := 0
		if len(observations) > 3 {
			// If restart numbers vary by more than 5, assume outliers were removed
			minRestart := observations[0].RestartNum
			maxRestart := observations[0].RestartNum
			for _, obs := range observations {
				if obs.RestartNum < minRestart {
					minRestart = obs.RestartNum
				}
				if obs.RestartNum > maxRestart {
					maxRestart = obs.RestartNum
				}
			}
			if maxRestart-minRestart > 5 {
				outliersRemoved = 1
			}
		}

		response = map[string]interface{}{
			"type":    "opinion",
			"subject": subject,
			"derived_state": map[string]interface{}{
				"start_time":   opinion.StartTime,
				"restart_num":  opinion.Restarts,
				"last_restart": opinion.LastRestart,
			},
			"source_events":    sourceEvents,
			"consensus_method": "trimmed_mean",
			"outliers_removed": outliersRemoved,
		}

	default:
		http.Error(w, "unknown projection type: "+projectionType, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logrus.WithError(err).Warn("Failed to encode response")
	}
}

// inspectorEventDetailHandler - GET /api/inspector/event/{id}
// Returns full event details with signature verification
func (ln *LocalNara) inspectorEventDetailHandler(w http.ResponseWriter, r *http.Request) {
	eventID := strings.TrimPrefix(r.URL.Path, "/api/inspector/event/")
	if eventID == "" {
		http.Error(w, "event ID required", http.StatusBadRequest)
		return
	}

	// Find event by ID
	ln.SyncLedger.mu.RLock()
	var foundEvent *SyncEvent
	eventIndex := -1
	totalEvents := len(ln.SyncLedger.Events)

	for i, event := range ln.SyncLedger.Events {
		if event.ID == eventID {
			foundEvent = &event
			eventIndex = i
			break
		}
	}
	ln.SyncLedger.mu.RUnlock()

	if foundEvent == nil {
		http.Error(w, "event not found", http.StatusNotFound)
		return
	}

	// Verify signature if signed
	isSigned := foundEvent.IsSigned()
	signatureValid := false
	publicKeyKnown := false
	signableContent := ""
	var verificationError *string

	if isSigned {
		// Create a lookup function that resolves public keys
		lookup := func(id types.NaraID, name types.NaraName) ed25519.PublicKey {
			if id != "" {
				if key := ln.Network.getPublicKeyForNaraID(id); key != nil {
					return key
				}
			}
			if name != "" {
				return ln.Network.resolvePublicKeyForNara(name)
			}
			return nil
		}

		// Determine if public key is known based on payload type
		payload := foundEvent.Payload()
		switch p := payload.(type) {
		case *HeyThereEvent:
			// Embedded key - known if it parses
			if p != nil && p.PublicKey != "" {
				if _, err := ParsePublicKey(p.PublicKey); err == nil {
					publicKeyKnown = true
				}
			}
		case *ChauEvent:
			// Embedded key - known if it parses
			if p != nil && p.PublicKey != "" {
				if _, err := ParsePublicKey(p.PublicKey); err == nil {
					publicKeyKnown = true
				}
			}
		case *CheckpointEventPayload:
			// Multi-sig - check if we know at least one voter's key
			if p != nil {
				for _, voterID := range p.VoterIDs {
					if lookup(voterID, "") != nil {
						publicKeyKnown = true
						break
					}
				}
			}
		default:
			// Standard payloads - check if we can look up the emitter
			publicKeyKnown = lookup(foundEvent.EmitterID, foundEvent.Emitter) != nil
		}

		// Verify using the payload's verification logic
		signatureValid = foundEvent.Verify(lookup)

		if !signatureValid {
			if !publicKeyKnown {
				errMsg := "public key not found for emitter"
				verificationError = &errMsg
			} else {
				errMsg := "signature verification failed"
				verificationError = &errMsg
			}
		}
	}

	// Calculate age
	ageSeconds := (time.Now().UnixNano() - foundEvent.Timestamp) / 1e9

	response := map[string]interface{}{
		"event": foundEvent,
		"verification": map[string]interface{}{
			"is_signed":          isSigned,
			"signature_valid":    signatureValid,
			"public_key_known":   publicKeyKnown,
			"signable_content":   signableContent,
			"verification_error": verificationError,
		},
		"metadata": map[string]interface{}{
			"event_index":  eventIndex,
			"total_events": totalEvents,
			"age_seconds":  ageSeconds,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logrus.WithError(err).Warn("Failed to encode response")
	}
}

// UptimePeriod represents a period of time when a nara was online or offline
type UptimePeriod struct {
	Type      string `json:"type"`       // "online" or "offline"
	StartTime int64  `json:"start_time"` // Unix timestamp (seconds)
	EndTime   int64  `json:"end_time"`   // Unix timestamp (seconds), 0 if ongoing
	Duration  int64  `json:"duration"`   // Duration in seconds
	Ongoing   bool   `json:"ongoing"`    // True if this period is still active
}

// inspectorUptimeHandler - GET /api/inspector/uptime/{subject}
// Returns timeline of online/offline periods for a nara.
// Uses checkpoint data when available, falls back to backfill, then real-time events.
func (ln *LocalNara) inspectorUptimeHandler(w http.ResponseWriter, r *http.Request) {
	subject := types.NaraName(strings.TrimPrefix(r.URL.Path, "/api/inspector/uptime/"))
	if subject == "" {
		http.Error(w, "subject required", http.StatusBadRequest)
		return
	}

	// Collect all status-relevant events for this subject
	type statusEvent struct {
		timestamp int64
		status    string // "online" or "offline"
		source    string // event type that caused this
	}

	var events []statusEvent
	var checkpoint *CheckpointEventPayload
	var backfill *ObservationEventPayload

	ln.SyncLedger.mu.RLock()

	// Find checkpoint and backfill first
	for _, event := range ln.SyncLedger.Events {
		if event.Service == ServiceCheckpoint && event.Checkpoint != nil && event.Checkpoint.Subject == subject {
			if checkpoint == nil || event.Checkpoint.AsOfTime > checkpoint.AsOfTime {
				checkpoint = event.Checkpoint
			}
		}
		if event.Service == ServiceObservation && event.Observation != nil &&
			event.Observation.Subject == subject && event.Observation.IsBackfill {
			backfill = event.Observation
		}
	}

	// Collect status events
	for _, event := range ln.SyncLedger.Events {
		switch event.Service {
		case ServiceHeyThere:
			if event.HeyThere != nil && event.HeyThere.From == subject {
				events = append(events, statusEvent{
					timestamp: event.Timestamp,
					status:    "online",
					source:    "hey_there",
				})
			}
		case ServiceChau:
			if event.Chau != nil && event.Chau.From == subject {
				events = append(events, statusEvent{
					timestamp: event.Timestamp,
					status:    "offline",
					source:    "chau",
				})
			}
		case ServiceObservation:
			if event.Observation != nil && event.Observation.Subject == subject {
				switch event.Observation.Type {
				case "status-change":
					status := "offline"
					if event.Observation.OnlineState == "ONLINE" {
						status = "online"
					}
					events = append(events, statusEvent{
						timestamp: event.Timestamp,
						status:    status,
						source:    "observation:" + event.Observation.Type,
					})
				case "restart", "first-seen":
					events = append(events, statusEvent{
						timestamp: event.Timestamp,
						status:    "online",
						source:    "observation:" + event.Observation.Type,
					})
				}
			}
		}
	}
	ln.SyncLedger.mu.RUnlock()

	// Sort events by timestamp
	sort.Slice(events, func(i, j int) bool {
		return events[i].timestamp < events[j].timestamp
	})

	// Build timeline of periods
	var periods []UptimePeriod
	now := time.Now().Unix()

	// Use DeriveTotalUptime for accurate total (includes checkpoint/backfill)
	totalUptime := ln.SyncLedger.DeriveTotalUptime(subject)

	// Determine baseline from checkpoint or backfill
	var baselineSource string
	var historicalUptime int64
	var baselineTime int64 // Time from which we have detailed events

	if checkpoint != nil {
		baselineSource = "checkpoint"
		historicalUptime = checkpoint.Observation.TotalUptime
		baselineTime = checkpoint.AsOfTime
	} else if backfill != nil {
		baselineSource = "backfill"
		baselineTime = backfill.StartTime
		// For backfill, historical uptime is calculated from StartTime
	}

	// If we have historical data, add a "historical" period representing it
	if checkpoint != nil && historicalUptime > 0 {
		// Add historical period - we know the total uptime but not the exact on/off transitions
		periods = append(periods, UptimePeriod{
			Type:      "historical",
			StartTime: checkpoint.Observation.StartTime,
			EndTime:   checkpoint.AsOfTime,
			Duration:  historicalUptime,
			Ongoing:   false,
		})
	}

	// Filter events to only those after the baseline time (if we have a checkpoint)
	var filteredEvents []statusEvent
	if checkpoint != nil {
		for _, e := range events {
			eventTimeSec := e.timestamp / 1e9
			if eventTimeSec > baselineTime {
				filteredEvents = append(filteredEvents, e)
			}
		}
	} else {
		filteredEvents = events
	}

	// Process events to build periods
	var currentStatus string
	var periodStart int64

	// If we have a checkpoint, assume online from AsOfTime
	// (checkpoints are created while the nara is running)
	if checkpoint != nil {
		currentStatus = "online"
		periodStart = checkpoint.AsOfTime
	}

	// If we have a backfill but no checkpoint, assume online from StartTime
	if checkpoint == nil && backfill != nil {
		currentStatus = "online"
		periodStart = backfill.StartTime
	}

	for _, event := range filteredEvents {
		eventTimeSec := event.timestamp / 1e9 // Convert nanoseconds to seconds

		// If we haven't established a start yet, use first event
		if periodStart == 0 {
			currentStatus = event.status
			periodStart = eventTimeSec
			continue
		}

		// Status changed
		if event.status != currentStatus {
			// Close previous period
			duration := eventTimeSec - periodStart
			periods = append(periods, UptimePeriod{
				Type:      currentStatus,
				StartTime: periodStart,
				EndTime:   eventTimeSec,
				Duration:  duration,
				Ongoing:   false,
			})

			// Start new period
			currentStatus = event.status
			periodStart = eventTimeSec
		}
	}

	// Add final ongoing period if we have a start time
	if periodStart > 0 {
		duration := now - periodStart
		periods = append(periods, UptimePeriod{
			Type:      currentStatus,
			StartTime: periodStart,
			EndTime:   0,
			Duration:  duration,
			Ongoing:   true,
		})
	}

	// Reverse periods so newest is first
	for i, j := 0, len(periods)-1; i < j; i, j = i+1, j-1 {
		periods[i], periods[j] = periods[j], periods[i]
	}

	response := map[string]interface{}{
		"subject":         subject,
		"periods":         periods,
		"total_uptime":    totalUptime,
		"baseline_source": baselineSource,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logrus.WithError(err).Warn("Failed to encode response")
	}
}
