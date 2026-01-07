package nara

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// High-frequency endpoints that skip logging entirely
var silentEndpoints = map[string]bool{
	"/ping":    true,
	"/metrics": true,
}

// pingLogger tracks recent pings for batched logging
type pingLoggerState struct {
	mu      sync.Mutex
	count   int
	pingers []string
}

var pingLogger = &pingLoggerState{}

// loggingMiddleware wraps an http.HandlerFunc with request/response logging
func (network *Network) loggingMiddleware(path string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Try to identify the caller
		caller := r.Header.Get("X-Nara-From")
		if caller == "" {
			caller = r.RemoteAddr
		}

		// For POST requests, peek at the body
		var bodySummary string
		if r.Method == "POST" && r.Body != nil {
			bodyBytes, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes)) // restore body

			// Summarize based on endpoint
			if path == "/events/sync" {
				var req SyncRequest
				if json.Unmarshal(bodyBytes, &req) == nil {
					bodySummary = fmt.Sprintf("from=%s since=%s services=%v max=%d",
						req.From,
						time.Unix(req.SinceTime, 0).Format("15:04:05"),
						req.Services,
						req.MaxEvents)
				}
			} else if len(bodyBytes) > 0 && len(bodyBytes) < 200 {
				bodySummary = string(bodyBytes)
			} else if len(bodyBytes) >= 200 {
				bodySummary = fmt.Sprintf("(%d bytes)", len(bodyBytes))
			}
		}

		// Wrap response writer to capture status
		wrapped := &responseLogger{ResponseWriter: w, status: 200}
		handler(wrapped, r)

		duration := time.Since(start)

		// Skip logging for high-frequency endpoints
		if silentEndpoints[path] {
			return
		}

		// Log the request
		if bodySummary != "" {
			logrus.Debugf("📨 %s %s from %s [%s] → %d (%v)",
				r.Method, path, caller, bodySummary, wrapped.status, duration.Round(time.Millisecond))
		} else {
			logrus.Debugf("📨 %s %s from %s → %d (%v)",
				r.Method, path, caller, wrapped.status, duration.Round(time.Millisecond))
		}
	}
}

// responseLogger wraps ResponseWriter to capture status code
type responseLogger struct {
	http.ResponseWriter
	status int
}

func (rl *responseLogger) WriteHeader(code int) {
	rl.status = code
	rl.ResponseWriter.WriteHeader(code)
}

//go:embed nara-web/public/*
var staticContent embed.FS

func (network *Network) startHttpServer(httpAddr string) error {
	listen_interface := httpAddr
	if listen_interface == "" {
		listen_interface = ":8080"
	}

	listener, err := net.Listen("tcp", listen_interface)
	if err != nil {
		return fmt.Errorf("listen error: %w", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	logrus.Printf("Listening for HTTP on port %d", port)

	// Register handlers with logging middleware
	http.HandleFunc("/api.json", network.loggingMiddleware("/api.json", network.httpApiJsonHandler))
	http.HandleFunc("/narae.json", network.loggingMiddleware("/narae.json", network.httpNaraeJsonHandler))
	http.HandleFunc("/metrics", network.loggingMiddleware("/metrics", network.httpMetricsHandler))
	http.HandleFunc("/status/", network.loggingMiddleware("/status/", network.httpStatusJsonHandler))
	http.HandleFunc("/events", network.loggingMiddleware("/events", network.httpEventsSSEHandler))
	http.HandleFunc("/social/clout", network.loggingMiddleware("/social/clout", network.httpCloutHandler))
	http.HandleFunc("/social/recent", network.loggingMiddleware("/social/recent", network.httpRecentEventsHandler))
	http.HandleFunc("/world/start", network.loggingMiddleware("/world/start", network.httpWorldStartHandler))
	http.HandleFunc("/world/journeys", network.loggingMiddleware("/world/journeys", network.httpWorldJourneysHandler))
	http.HandleFunc("/events/sync", network.loggingMiddleware("/events/sync", network.httpEventsSyncHandler))
	http.HandleFunc("/ping", network.loggingMiddleware("/ping", network.httpPingHandler))
	http.HandleFunc("/coordinates", network.loggingMiddleware("/coordinates", network.httpCoordinatesHandler))
	http.HandleFunc("/network/map", network.loggingMiddleware("/network/map", network.httpNetworkMapHandler))
	http.HandleFunc("/proximity", network.loggingMiddleware("/proximity", network.httpProximityHandler))
	publicFS, _ := fs.Sub(staticContent, "nara-web/public")
	http.Handle("/", http.FileServer(http.FS(publicFS)))

	go http.Serve(listener, nil)
	return nil
}

func (network *Network) httpApiJsonHandler(w http.ResponseWriter, r *http.Request) {
	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	allNarae := network.getNarae()

	var naras []map[string]interface{}
	for _, nara := range allNarae {
		nara.mu.Lock()
		statusMap := make(map[string]interface{})
		jsonStatus, _ := json.Marshal(nara.Status)
		json.Unmarshal(jsonStatus, &statusMap)
		nara.mu.Unlock()
		statusMap["Name"] = nara.Name
		naras = append(naras, statusMap)
	}

	response := map[string]interface{}{
		"naras":  naras,
		"server": network.local.Me.Name,
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(response)
}

func (network *Network) httpNaraeJsonHandler(w http.ResponseWriter, r *http.Request) {
	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	allNarae := network.getNarae()

	var naras []map[string]interface{}
	for _, nara := range allNarae {
		obs := nara.getObservation(nara.Name)
		nara.mu.Lock()
		naraMap := map[string]interface{}{
			"Name":         nara.Name,
			"PublicUrl":    nara.Status.PublicUrl,
			"Flair":        nara.Status.Flair,
			"LicensePlate": nara.Status.LicensePlate,
			"Buzz":         nara.Status.Buzz,
			"Chattiness":   nara.Status.Chattiness,
			"LastSeen":     obs.LastSeen,
			"LastRestart":  obs.LastRestart,
			"Online":       obs.Online,
			"StartTime":    obs.StartTime,
			"Restarts":     obs.Restarts,
			"Uptime":       nara.Status.HostStats.Uptime,
			"Trend":        nara.Status.Trend,
			"TrendEmoji":   nara.Status.TrendEmoji,
		}
		nara.mu.Unlock()
		naras = append(naras, naraMap)
	}

	response := map[string]interface{}{
		"naras":  naras,
		"server": network.local.Me.Name,
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(response)
}

func (network *Network) httpStatusJsonHandler(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Path[len("/status/") : len(r.URL.Path)-len(".json")]
	network.local.mu.Lock()
	nara, exists := network.Neighbourhood[name]
	network.local.mu.Unlock()

	if !exists {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(nara.Status)
}

func (network *Network) getNarae() []*Nara {
	var naras []*Nara
	for _, nara := range network.Neighbourhood {
		naras = append(naras, nara)
	}
	if !network.ReadOnly {
		naras = append(naras, network.local.Me)
	}
	return naras
}

// SSE endpoint for real-time social events (shooting stars!)
func (network *Network) httpEventsSSEHandler(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Get flusher for streaming
	flusher, ok := w.(http.Flusher)
	if !ok {
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
			data, _ := json.Marshal(map[string]interface{}{
				"type":      event.Type,
				"actor":     event.Actor,
				"target":    event.Target,
				"reason":    event.Reason,
				"message":   TeaseMessage(event.Reason, event.Actor, event.Target),
				"timestamp": event.Timestamp,
			})
			fmt.Fprintf(w, "event: social\ndata: %s\n\n", data)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// Clout scores from this nara's perspective
func (network *Network) httpCloutHandler(w http.ResponseWriter, r *http.Request) {
	var clout map[string]float64
	if network.local.SocialLedger != nil {
		clout = network.local.SocialLedger.DeriveClout(network.local.Soul)
	} else {
		clout = make(map[string]float64)
	}

	response := map[string]interface{}{
		"server": network.meName(),
		"clout":  clout,
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(response)
}

// Recent social events
func (network *Network) httpRecentEventsHandler(w http.ResponseWriter, r *http.Request) {
	var events []SocialEvent
	if network.local.SocialLedger != nil {
		events = network.local.SocialLedger.GetRecentEvents(5)
	}

	// Convert to JSON-friendly format
	var eventList []map[string]interface{}
	for _, e := range events {
		eventList = append(eventList, map[string]interface{}{
			"actor":     e.Actor,
			"target":    e.Target,
			"reason":    e.Reason,
			"message":   TeaseMessage(e.Reason, e.Actor, e.Target),
			"timestamp": e.Timestamp,
		})
	}

	response := map[string]interface{}{
		"server": network.meName(),
		"events": eventList,
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(response)
}

func (network *Network) httpMetricsHandler(w http.ResponseWriter, r *http.Request) {
	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	var lines []string

	lines = append(lines, "# HELP nara_info Basic string data from each Nara")
	lines = append(lines, "# TYPE nara_info gauge")
	lines = append(lines, "# HELP nara_online 1 if the Nara is ONLINE, else 0")
	lines = append(lines, "# TYPE nara_online gauge")
	lines = append(lines, "# HELP nara_buzz Buzz level reported by the Nara")
	lines = append(lines, "# TYPE nara_buzz gauge")
	lines = append(lines, "# HELP nara_chattiness Chattiness level reported by the Nara")
	lines = append(lines, "# TYPE nara_chattiness gauge")
	lines = append(lines, "# HELP nara_last_seen Unix timestamp when the Nara was last seen")
	lines = append(lines, "# TYPE nara_last_seen gauge")
	lines = append(lines, "# HELP nara_last_restart Unix timestamp when the Nara last restarted")
	lines = append(lines, "# TYPE nara_last_restart gauge")
	lines = append(lines, "# HELP nara_start_time Unix timestamp when the Nara started")
	lines = append(lines, "# TYPE nara_start_time gauge")
	lines = append(lines, "# HELP nara_uptime_seconds Uptime reported by the host")
	lines = append(lines, "# TYPE nara_uptime_seconds gauge")
	lines = append(lines, "# HELP nara_restarts_total Restart count from the Nara")
	lines = append(lines, "# TYPE nara_restarts_total counter")

	allNarae := network.getNarae()

	for _, nara := range allNarae {
		obs := nara.getObservation(nara.Name)

		nara.mu.Lock()
		lines = append(lines, fmt.Sprintf(`nara_info{name="%s",flair="%s",license_plate="%s"} 1`, nara.Name, nara.Status.Flair, nara.Status.LicensePlate))
		buzz := nara.Status.Buzz
		chattiness := nara.Status.Chattiness
		uptime := nara.Status.HostStats.Uptime
		nara.mu.Unlock()

		onlineValue := 0
		if obs.Online == "ONLINE" {
			onlineValue = 1
		}
		lines = append(lines, fmt.Sprintf(`nara_online{name="%s"} %d`, nara.Name, onlineValue))
		lines = append(lines, fmt.Sprintf(`nara_buzz{name="%s"} %d`, nara.Name, buzz))
		lines = append(lines, fmt.Sprintf(`nara_chattiness{name="%s"} %d`, nara.Name, chattiness))

		if obs.LastSeen > 0 {
			lines = append(lines, fmt.Sprintf(`nara_last_seen{name="%s"} %d`, nara.Name, obs.LastSeen))
		}
		if obs.LastRestart > 0 {
			lines = append(lines, fmt.Sprintf(`nara_last_restart{name="%s"} %d`, nara.Name, obs.LastRestart))
		}
		if obs.StartTime > 0 {
			lines = append(lines, fmt.Sprintf(`nara_start_time{name="%s"} %d`, nara.Name, obs.StartTime))
		}
		if uptime > 0 {
			lines = append(lines, fmt.Sprintf(`nara_uptime_seconds{name="%s"} %d`, nara.Name, uptime))
		}
		lines = append(lines, fmt.Sprintf(`nara_restarts_total{name="%s"} %d`, nara.Name, obs.Restarts))
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	for _, line := range lines {
		fmt.Fprintln(w, line)
	}
}

// World Journey HTTP handlers

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
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"id":      wm.ID,
		"message": wm.OriginalMessage,
	})
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
	json.NewEncoder(w).Encode(map[string]interface{}{
		"journeys": response,
		"server":   network.meName(),
	})
}

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

	logrus.Debugf("📦 mesh sync: sent %d events to %s (slice %d/%d)", len(events), req.From, req.SliceIndex+1, req.SliceTotal)

	// Create signed response
	response := NewSignedSyncResponse(network.meName(), events, network.local.Keypair)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(response)
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

	pingLogger.mu.Lock()
	pingLogger.pingers = append(pingLogger.pingers, caller)
	pingLogger.count++
	if pingLogger.count >= 10 {
		logrus.Infof("🏓 received 10 pings from: %v", pingLogger.pingers)
		pingLogger.count = 0
		pingLogger.pingers = nil
	}
	pingLogger.mu.Unlock()

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"t":    time.Now().UnixNano(),
		"from": network.meName(),
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

		obs := nara.getObservation(name)

		// Get our RTT observation to this peer
		myObs := network.local.getObservationLocked(name)

		node := map[string]interface{}{
			"name":        name,
			"coordinates": coords,
			"online":      obs.Online == "ONLINE",
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
	json.NewEncoder(w).Encode(response)
}

// httpProximityHandler returns this nara's barrio information
// Naras in the same barrio (grid cell) share the same emoji and are considered "nearby"
func (network *Network) httpProximityHandler(w http.ResponseWriter, r *http.Request) {
	// Get my barrio info
	myEmoji := network.GetMyBarrioEmoji()

	// Find all naras in my barrio
	var barrioMembers []string
	for name := range network.Neighbourhood {
		if network.IsInMyBarrio(name) {
			barrioMembers = append(barrioMembers, name)
		}
	}

	response := map[string]interface{}{
		"server":         network.meName(),
		"barrio_emoji":   myEmoji,
		"barrio_members": barrioMembers,
		"grid_size":      network.calculateGridSize(),
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(response)
}
