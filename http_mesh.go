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

	// Sanity check slice params
	if req.SliceTotal < 1 {
		req.SliceTotal = 1
	}
	if req.SliceIndex < 0 || req.SliceIndex >= req.SliceTotal {
		req.SliceIndex = 0
	}

	// Default max events if not specified
	maxEvents := req.MaxEvents
	if maxEvents <= 0 || maxEvents > 2000 {
		maxEvents = 2000
	}

	// Get events from unified sync ledger
	var events []SyncEvent
	if network.local.SyncLedger != nil {
		events = network.local.SyncLedger.GetEventsForSync(
			req.Services,
			req.Subjects,
			req.SinceTime,
			req.SliceIndex,
			req.SliceTotal,
			maxEvents,
		)
	}

	logrus.Printf("📤 mesh sync to %s: sent %d events (slice %d/%d)", req.From, len(events), req.SliceIndex+1, req.SliceTotal)

	// Create signed response
	response := NewSignedSyncResponse(network.meName(), events, network.local.Keypair)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(response)
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
	json.NewEncoder(w).Encode(myZine)
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
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": added,
		"from":    network.meName(),
	})
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
	json.NewEncoder(w).Encode(map[string]interface{}{
		"t":          time.Now().UnixNano(),
		"from":       network.meName(),
		"public_key": network.local.Me.Status.PublicKey,
		"mesh_ip":    network.local.Me.Status.MeshIP,
	})
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
	json.NewEncoder(w).Encode(response)
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

// getVerifiedSender returns the mesh-authenticated sender name from the request.
// This is set by meshAuthMiddleware after verifying the request signature.
// Always use this instead of trusting a "from" field in the request body.
func getVerifiedSender(r *http.Request) string {
	return r.Header.Get("X-Nara-Verified")
}

// POST /stash/store - Owner stores stash with confidant
// DELETE /stash/store - Owner requests deletion of stash
func (network *Network) httpStashHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		network.httpStashStoreHandler(w, r)
	} else if r.Method == "DELETE" {
		network.httpStashDeleteHandler(w, r)
	} else {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (network *Network) httpStashStoreHandler(w http.ResponseWriter, r *http.Request) {
	sender := getVerifiedSender(r)

	var req StashStoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	accepted, reason := network.stashService.AcceptStash(sender, req.Stash)
	response := StashStoreResponse{
		Accepted: accepted,
		Reason:   reason,
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(response)
}

func (network *Network) httpStashDeleteHandler(w http.ResponseWriter, r *http.Request) {
	sender := getVerifiedSender(r)

	response := StashDeleteResponse{
		Deleted: network.stashService.DeleteStashFor(sender),
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(response)
}

func (network *Network) httpStashRetrieveHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sender := getVerifiedSender(r)

	response := StashRetrieveResponse{Found: false}
	if payload := network.stashService.RetrieveStashFor(sender); payload != nil {
		response.Found = true
		response.Stash = payload
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(response)
}

func (network *Network) httpStashPushHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sender := getVerifiedSender(r)

	var req StashPushRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.To == "" || req.Stash == nil {
		http.Error(w, "to and stash are required", http.StatusBadRequest)
		return
	}

	// Verify this push is for us
	if req.To != network.meName() {
		http.Error(w, "Push not intended for this nara", http.StatusBadRequest)
		return
	}

	accepted, reason := network.stashService.AcceptPushedStash(sender, req.Stash)
	response := StashPushResponse{
		Accepted: accepted,
		Reason:   reason,
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(response)
}
