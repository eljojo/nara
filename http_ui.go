package nara

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sirupsen/logrus"
)

// SSE endpoint for real-time social events (shooting stars!)
func (network *Network) httpEventsSSEHandler(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if r := recover(); r != nil {
			logrus.Errorf("SSE handler panic: %v", r)
		}
	}()

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Get flusher for streaming
	flusher, ok := w.(http.Flusher)
	if !ok {
		logrus.Error("SSE not supported - ResponseWriter doesn't implement http.Flusher")
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	// Subscribe to events
	eventChan := network.subscribeSSE()
	defer network.unsubscribeSSE(eventChan)

	// Send initial connection event
	fmt.Fprintf(w, "event: connected\ndata: {\"server\":\"%s\"}\n\n", network.meName())
	flusher.Flush()

	// Stream events until client disconnects
	for {
		select {
		case event := <-eventChan:
			// Get UI format from the payload
			var uiFormat map[string]string
			if event.Social != nil {
				uiFormat = event.Social.UIFormat()
			} else if event.Ping != nil {
				uiFormat = event.Ping.UIFormat()
			} else if event.Observation != nil {
				uiFormat = event.Observation.UIFormat()
			} else if event.HeyThere != nil {
				uiFormat = event.HeyThere.UIFormat()
			} else if event.Chau != nil {
				uiFormat = event.Chau.UIFormat()
			} else if event.Seen != nil {
				uiFormat = event.Seen.UIFormat()
			}

			// Skip if no UI format available
			if uiFormat == nil {
				continue
			}

			// Send simple event with just UI data
			data := map[string]interface{}{
				"id":        event.ID,
				"service":   event.Service,
				"timestamp": event.Timestamp,
				"emitter":   event.Emitter,
				"icon":      uiFormat["icon"],
				"text":      uiFormat["text"],
				"detail":    uiFormat["detail"],
			}

			jsonData, err := json.Marshal(data)
			if err != nil {
				logrus.Errorf("SSE: failed to marshal event: %v", err)
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Service, jsonData)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// Clout scores from this nara's perspective
func (network *Network) httpCloutHandler(w http.ResponseWriter, r *http.Request) {
	var clout map[string]float64
	if network.local.Projections != nil {
		clout = network.local.Projections.Clout().DeriveClout(network.local.Soul, network.local.Me.Status.Personality)
	} else {
		clout = make(map[string]float64)
	}

	response := map[string]interface{}{
		"server": network.meName(),
		"clout":  clout,
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logrus.WithError(err).Warn("Failed to encode response")
	}
}

// Recent social events
func (network *Network) httpRecentEventsHandler(w http.ResponseWriter, r *http.Request) {
	var events []SocialEvent
	if network.local.SyncLedger != nil {
		// Convert SyncEvents to legacy SocialEvents
		syncEvents := network.local.SyncLedger.GetRecentSocialEvents(5)
		for _, se := range syncEvents {
			if legacy := se.ToSocialEvent(); legacy != nil {
				events = append(events, *legacy)
			}
		}
	}

	// Convert to JSON-friendly format (timestamps in seconds for UI)
	var eventList []map[string]interface{}
	for _, e := range events {
		eventList = append(eventList, map[string]interface{}{
			"actor":     e.Actor,
			"target":    e.Target,
			"reason":    e.Reason,
			"message":   TeaseMessage(e.Reason, e.Actor, e.Target),
			"timestamp": e.Timestamp / 1e9, // Convert nanoseconds to seconds for UI
		})
	}

	response := map[string]interface{}{
		"server": network.meName(),
		"events": eventList,
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logrus.WithError(err).Warn("Failed to encode response")
	}
}

// Tease counts - objective count of teases per actor (no personality influence)
func (network *Network) httpTeaseCountsHandler(w http.ResponseWriter, r *http.Request) {
	var counts map[string]int
	if network.local.SyncLedger != nil {
		counts = network.local.SyncLedger.GetTeaseCounts()
	} else {
		counts = make(map[string]int)
	}

	// Convert to sorted list for the response
	type teaseCount struct {
		Actor string `json:"actor"`
		Count int    `json:"count"`
	}
	var teases []teaseCount
	for actor, count := range counts {
		teases = append(teases, teaseCount{Actor: actor, Count: count})
	}
	// Sort by count descending
	for i := 0; i < len(teases); i++ {
		for j := i + 1; j < len(teases); j++ {
			if teases[j].Count > teases[i].Count {
				teases[i], teases[j] = teases[j], teases[i]
			}
		}
	}

	response := map[string]interface{}{
		"server": network.meName(),
		"teases": teases,
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logrus.WithError(err).Warn("Failed to encode response")
	}
}

// World Journey HTTP handlers

// POST /world/relay - Receive and forward a world journey message
// This is the main transport for world messages between naras
func (network *Network) httpWorldRelayHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var wm WorldMessage
	if err := json.NewDecoder(r.Body).Decode(&wm); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if network.worldHandler == nil {
		http.Error(w, "World journey not initialized", http.StatusServiceUnavailable)
		return
	}

	// HandleIncoming verifies signatures, adds our hop, and forwards
	if err := network.worldHandler.HandleIncoming(&wm); err != nil {
		logrus.Warnf("🌍 World relay failed: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"from":    network.meName(),
	}); err != nil {
		logrus.WithError(err).Warn("Failed to encode response")
	}
}

// POST /world/start - Start a new world journey
func (network *Network) httpWorldStartHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Message == "" {
		http.Error(w, "Message is required", http.StatusBadRequest)
		return
	}

	if network.worldHandler == nil {
		http.Error(w, "World journey not initialized", http.StatusServiceUnavailable)
		return
	}

	wm, err := network.worldHandler.StartJourney(req.Message)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"id":      wm.ID,
		"message": wm.OriginalMessage,
	}); err != nil {
		logrus.WithError(err).Warn("Failed to encode response")
	}
}

// GET /world/journeys - Get completed world journeys
func (network *Network) httpWorldJourneysHandler(w http.ResponseWriter, r *http.Request) {
	network.worldJourneysMu.RLock()
	journeys := make([]*WorldMessage, len(network.worldJourneys))
	copy(journeys, network.worldJourneys)
	network.worldJourneysMu.RUnlock()

	// Return most recent first, limit to 20
	if len(journeys) > 20 {
		journeys = journeys[len(journeys)-20:]
	}

	// Reverse order (most recent first)
	for i, j := 0, len(journeys)-1; i < j; i, j = i+1, j-1 {
		journeys[i], journeys[j] = journeys[j], journeys[i]
	}

	// Convert to response format
	var response []map[string]interface{}
	for _, wm := range journeys {
		hops := make([]map[string]interface{}, len(wm.Hops))
		for i, hop := range wm.Hops {
			hops[i] = map[string]interface{}{
				"nara":      hop.Nara,
				"timestamp": hop.Timestamp,
				"stamp":     hop.Stamp,
				"signature": hop.Signature,
			}
		}

		rewards := CalculateWorldRewards(wm)

		response = append(response, map[string]interface{}{
			"id":         wm.ID,
			"message":    wm.OriginalMessage,
			"originator": wm.Originator,
			"hops":       hops,
			"rewards":    rewards,
			"complete":   wm.IsComplete(),
		})
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"journeys": response,
		"server":   network.meName(),
	}); err != nil {
		logrus.WithError(err).Warn("Failed to encode response")
	}
}

// GET /network/map - All known nodes with coordinates for visualization
func (network *Network) httpNetworkMapHandler(w http.ResponseWriter, r *http.Request) {
	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	var nodes []map[string]interface{}

	// Add ourselves
	network.local.Me.mu.Lock()
	meCoords := network.local.Me.Status.Coordinates
	network.local.Me.mu.Unlock()

	nodes = append(nodes, map[string]interface{}{
		"name":        network.meName(),
		"coordinates": meCoords,
		"online":      true,
		"is_self":     true,
	})

	// Add all neighbours
	for name, nara := range network.Neighbourhood {
		nara.mu.Lock()
		coords := nara.Status.Coordinates
		nara.mu.Unlock()

		// Get our observation of this peer
		myObs := network.local.getObservationLocked(NaraName(name))

		node := map[string]interface{}{
			"name":        name,
			"coordinates": coords,
			"online":      myObs.Online == "ONLINE",
			"is_self":     false,
		}

		if myObs.LastPingRTT > 0 {
			node["rtt_to_us"] = myObs.LastPingRTT
		}
		if myObs.AvgPingRTT > 0 {
			node["avg_rtt"] = myObs.AvgPingRTT
		}

		nodes = append(nodes, node)
	}

	response := map[string]interface{}{
		"nodes":  nodes,
		"server": network.meName(),
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logrus.WithError(err).Warn("Failed to encode response")
	}
}

// httpProximityHandler returns this nara's barrio information
// Naras in the same barrio (grid cell) share the same emoji and are considered "nearby"
func (network *Network) httpProximityHandler(w http.ResponseWriter, r *http.Request) {
	// Get my barrio info
	myEmoji := network.GetMyBarrioEmoji()

	// Find all naras in my barrio
	var barrioMembers []string
	for name := range network.Neighbourhood {
		if network.IsInMyBarrio(name.String()) {
			barrioMembers = append(barrioMembers, name.String())
		}
	}

	response := map[string]interface{}{
		"server":         network.meName(),
		"barrio_emoji":   myEmoji,
		"barrio_members": barrioMembers,
		"grid_size":      network.calculateGridSize(),
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logrus.WithError(err).Warn("Failed to encode response")
	}
}
