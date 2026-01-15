package nara

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

// Mesh Event Sync HTTP handlers

// POST /events/sync - Sync events via mesh (unified sync backbone)
// Used by booting naras to recover event history from neighbors
// Supports both social events and ping observations
func (network *Network) httpEventsSyncHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req SyncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate request
	if req.From == "" {
		http.Error(w, "from is required", http.StatusBadRequest)
		return
	}

	// Route based on mode
	var events []SyncEvent
	var nextCursor string

	if network.local.SyncLedger == nil {
		// No ledger, return empty
		events = []SyncEvent{}
	} else {
		switch req.Mode {
		case "sample":
			// Decay-weighted sampling for boot recovery (organic hazy memory)
			sampleSize := req.SampleSize
			if sampleSize <= 0 || sampleSize > 5000 {
				sampleSize = 5000
			}
			events = network.local.SyncLedger.SampleEvents(sampleSize, network.meName(), req.Services, req.Subjects)
			logrus.Printf("📤 mesh sync to %s: sampled %d events (mode: sample)", req.From, len(events))

		case "page":
			// Cursor-based pagination for backup/checkpoint sync (complete retrieval)
			pageSize := req.PageSize
			if pageSize <= 0 || pageSize > 5000 {
				pageSize = 5000
			}
			events, nextCursor = network.local.SyncLedger.GetEventsPage(req.Cursor, pageSize, req.Services, req.Subjects)
			logrus.Printf("📤 mesh sync to %s: page %d events (mode: page, cursor: %s, next: %s)", req.From, len(events), req.Cursor, nextCursor)

		case "recent":
			// Most recent N events for web UI
			limit := req.Limit
			if limit <= 0 || limit > 5000 {
				limit = 100
			}
			events = network.local.SyncLedger.GetRecentEvents(limit, req.Services, req.Subjects)
			logrus.Printf("📤 mesh sync to %s: recent %d events (mode: recent)", req.From, len(events))

		default:
			// Legacy mode (backward compatibility)
			// Sanity check slice params
			sliceTotal := req.SliceTotal
			if sliceTotal < 1 {
				sliceTotal = 1
			}
			sliceIndex := req.SliceIndex
			if sliceIndex < 0 || sliceIndex >= sliceTotal {
				sliceIndex = 0
			}

			// Default max events
			maxEvents := req.MaxEvents
			if maxEvents <= 0 || maxEvents > 5000 {
				maxEvents = 5000
			}

			events = network.local.SyncLedger.GetEventsForSync(
				req.Services,
				req.Subjects,
				req.SinceTime,
				sliceIndex,
				sliceTotal,
				maxEvents,
			)
			logrus.Printf("📤 mesh sync to %s: sent %d events (legacy mode, slice %d/%d)", req.From, len(events), sliceIndex+1, sliceTotal)
		}
	}

	// Create signed response
	response := NewSignedSyncResponse(network.meName(), events, network.local.Keypair)
	response.NextCursor = nextCursor // Set cursor for page mode

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logrus.WithError(err).Warn("Failed to encode response")
	}
}

// POST /gossip/zine - Bidirectional zine exchange for P2P event gossip
// Receives a zine, merges events, returns our zine
func (network *Network) httpGossipZineHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Decode incoming zine
	var theirZine Zine
	if err := json.NewDecoder(r.Body).Decode(&theirZine); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate basic fields
	if theirZine.From == "" {
		http.Error(w, "from is required", http.StatusBadRequest)
		return
	}

	// Verify signature if we know their public key
	pubKey := network.resolvePublicKeyForNara(theirZine.From)
	if len(pubKey) > 0 && !VerifyZine(&theirZine, pubKey) {
		logrus.Warnf("📰 Invalid zine signature from %s, rejecting", theirZine.From)
		http.Error(w, "Invalid signature", http.StatusForbidden)
		return
	}

	// Merge their events into our ledger
	added, _ := network.MergeSyncEventsWithVerification(theirZine.Events)
	if added > 0 && network.logService != nil {
		network.logService.BatchGossipMerge(theirZine.From, added)
	}

	// Mark sender as online - receiving a zine proves they're reachable
	// UNLESS they sent a chau event (graceful shutdown announcement)
	senderIsShuttingDown := false
	for _, e := range theirZine.Events {
		if e.Service == ServiceChau && e.Chau != nil && e.Chau.From == theirZine.From {
			senderIsShuttingDown = true
			break
		}
	}
	if !senderIsShuttingDown {
		// Their events in the zine already prove they're active, no need for seen event
		network.recordObservationOnlineNara(theirZine.From, theirZine.CreatedAt)
	}

	// Create our zine to send back (bidirectional exchange)
	myZine := network.createZine()
	if myZine == nil {
		// Even if we have no events, send empty signed zine
		myZine = &Zine{
			From:      network.meName(),
			CreatedAt: time.Now().Unix(),
			Events:    []SyncEvent{},
		}
		// Sign the empty zine for consistency
		if sig, err := SignZine(myZine, network.local.Keypair); err == nil {
			myZine.Signature = sig
		}
	}

	// Return our zine
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(myZine); err != nil {
		logrus.WithError(err).Warn("Failed to encode response")
	}
}

// POST /dm - Receive a direct message (arbitrary SyncEvent)
// This is a generic endpoint for naras to send events directly to each other.
// Events are added to the local ledger and spread via gossip.
func (network *Network) httpDMHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var event SyncEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Event must be signed
	if !event.IsSigned() {
		http.Error(w, "Unsigned event rejected", http.StatusBadRequest)
		return
	}

	// Verify signature against emitter's public key
	pubKey := network.resolvePublicKeyForNara(event.Emitter)
	if pubKey == nil {
		http.Error(w, "Unknown emitter", http.StatusForbidden)
		return
	}
	if !event.VerifyWithKey(pubKey) {
		logrus.Warnf("📬 Invalid DM signature from %s, rejecting", event.Emitter)
		http.Error(w, "Invalid signature", http.StatusForbidden)
		return
	}

	// Add to local ledger (with personality filtering for social events)
	added := false
	if event.Service == ServiceSocial {
		added = network.local.SyncLedger.AddSocialEventFiltered(event, network.local.Me.Status.Personality)
	} else {
		added = network.local.SyncLedger.AddEvent(event)
	}

	// Trigger projection updates
	if added && network.local.Projections != nil {
		network.local.Projections.Trigger()
	}

	// Broadcast to local SSE clients (all event types, not just social)
	if added {
		network.broadcastSSE(event)
	}

	// Mark sender as online (unless this is a chau event - those mean "I'm shutting down")
	if event.Service != ServiceChau {
		// The DM itself is an event they emitted - they prove themselves
		network.recordObservationOnlineNara(event.Emitter, event.Timestamp/1e9)
	}

	// Log via LogService (batched) - skip for social events since the ledger listener handles teases
	if network.logService != nil && event.Service != ServiceSocial {
		network.logService.BatchDMReceived(event.Emitter)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"success": added,
		"from":    network.meName(),
	}); err != nil {
		logrus.WithError(err).Warn("Failed to encode response")
	}
}

// Network Coordinate HTTP handlers

// GET /ping - Lightweight latency probe for Vivaldi coordinates
// Returns server timestamp and nara name for RTT measurement
func (network *Network) httpPingHandler(w http.ResponseWriter, r *http.Request) {
	// Track who's pinging us (with mutex for concurrent safety)
	caller := r.Header.Get("X-Nara-From")
	if caller == "" {
		caller = r.RemoteAddr
	}

	// Log via LogService (batched)
	if network.logService != nil {
		network.logService.BatchPingsReceived(caller)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"t":          time.Now().UnixNano(),
		"from":       network.meName(),
		"public_key": network.local.Me.Status.PublicKey,
		"mesh_ip":    network.local.Me.Status.MeshIP,
	}); err != nil {
		logrus.WithError(err).Warn("Failed to encode response")
	}
}

// GET /coordinates - This nara's Vivaldi coordinates
func (network *Network) httpCoordinatesHandler(w http.ResponseWriter, r *http.Request) {
	network.local.Me.mu.Lock()
	coords := network.local.Me.Status.Coordinates
	network.local.Me.mu.Unlock()

	response := map[string]interface{}{
		"name":        network.meName(),
		"coordinates": coords,
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logrus.WithError(err).Warn("Failed to encode response")
	}
}

// Mesh Authentication for HTTP Handlers
//
// All mesh endpoints are protected by meshAuthMiddleware, which:
// 1. Verifies X-Nara-Name, X-Nara-Timestamp, X-Nara-Signature headers
// 2. Rejects requests with invalid/expired signatures (30s window)
// 3. Sets X-Nara-Verified header with the authenticated sender name
//
// Handlers should:
// - Use getVerifiedSender(r) to get the authenticated sender (never trust req.From)
// - NOT implement their own signature verification (mesh auth handles it)
// - NOT include Timestamp/Signature fields in request structs (redundant)
//
// Request structs can still include a "From" field for logging/debugging,
// but handlers should verify it matches getVerifiedSender() if used.
