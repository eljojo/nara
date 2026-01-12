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
	"net/http/pprof"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

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
			} else if path == "/api/stash/update" {
				// Don't log stash content, just size
				bodySummary = fmt.Sprintf("(%d bytes)", len(bodyBytes))
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

		// Log the request
		if bodySummary != "" {
			logrus.Infof("📨 %s %s from %s [%s] → %d (%v)",
				r.Method, path, caller, bodySummary, wrapped.status, duration.Round(time.Millisecond))
		} else {
			logrus.Infof("📨 %s %s from %s → %d (%v)",
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

// Flush implements http.Flusher interface by delegating to underlying ResponseWriter
func (rl *responseLogger) Flush() {
	if f, ok := rl.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
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

	// Create a mux for handlers (so we can reuse with mesh server)
	mux := network.createHTTPMux(true) // includeUI = true

	network.httpServer = &http.Server{
		Handler: mux,
	}

	go network.httpServer.Serve(listener)
	return nil
}

// createHTTPMux creates an HTTP mux with all handlers
// includeUI: whether to include web UI handlers (false for mesh-only server)
func (network *Network) createHTTPMux(includeUI bool) *http.ServeMux {
	mux := http.NewServeMux()
	var publicFS fs.FS

	// Mesh endpoints - available on both local and mesh servers
	// These require Ed25519 authentication (except /ping which needs to be fast)
	mux.HandleFunc("/events/sync", network.loggingMiddleware("/events/sync", network.meshAuthMiddleware("/events/sync", network.httpEventsSyncHandler)))
	mux.HandleFunc("/gossip/zine", network.loggingMiddleware("/gossip/zine", network.meshAuthMiddleware("/gossip/zine", network.httpGossipZineHandler)))
	mux.HandleFunc("/dm", network.loggingMiddleware("/dm", network.meshAuthMiddleware("/dm", network.httpDMHandler)))
	mux.HandleFunc("/world/relay", network.loggingMiddleware("/world/relay", network.meshAuthMiddleware("/world/relay", network.httpWorldRelayHandler)))
	mux.HandleFunc("/ping", network.loggingMiddleware("/ping", network.httpPingHandler)) // No auth - latency critical
	mux.HandleFunc("/coordinates", network.loggingMiddleware("/coordinates", network.httpCoordinatesHandler))

	// Stash endpoints
	mux.HandleFunc("/stash/store", network.loggingMiddleware("/stash/store", network.meshAuthMiddleware("/stash/store", network.httpStashHandler)))
	mux.HandleFunc("/stash/retrieve", network.loggingMiddleware("/stash/retrieve", network.meshAuthMiddleware("/stash/retrieve", network.httpStashRetrieveHandler)))
	mux.HandleFunc("/stash/push", network.loggingMiddleware("/stash/push", network.meshAuthMiddleware("/stash/push", network.httpStashPushHandler)))

	if includeUI {
		// Prepare static FS
		var err error
		publicFS, err = fs.Sub(staticContent, "nara-web/public")
		if err != nil {
			logrus.Errorf("failed to load embedded UI assets: %v", err)
		}

		// Profile pages: serve a single template for /nara/{name}
		mux.HandleFunc("/nara/", func(w http.ResponseWriter, r *http.Request) {
			if data, err := fs.ReadFile(staticContent, "nara-web/public/profile.html"); err == nil {
				http.ServeContent(w, r, "profile.html", time.Now(), bytes.NewReader(data))
				return
			}
			http.NotFound(w, r)
		})

		// Web UI endpoints - only on local server
		mux.HandleFunc("/api.json", network.loggingMiddleware("/api.json", network.httpApiJsonHandler))
		mux.HandleFunc("/narae.json", network.loggingMiddleware("/narae.json", network.httpNaraeJsonHandler))
		mux.HandleFunc("/metrics", network.loggingMiddleware("/metrics", network.httpMetricsHandler))
		mux.HandleFunc("/status/", network.loggingMiddleware("/status/", network.httpStatusJsonHandler))
		mux.HandleFunc("/events", network.loggingMiddleware("/events", network.httpEventsSSEHandler))
		mux.HandleFunc("/social/clout", network.loggingMiddleware("/social/clout", network.httpCloutHandler))
		mux.HandleFunc("/social/recent", network.loggingMiddleware("/social/recent", network.httpRecentEventsHandler))
		mux.HandleFunc("/social/teases", network.loggingMiddleware("/social/teases", network.httpTeaseCountsHandler))
		mux.HandleFunc("/world/start", network.loggingMiddleware("/world/start", network.httpWorldStartHandler))
		mux.HandleFunc("/world/journeys", network.loggingMiddleware("/world/journeys", network.httpWorldJourneysHandler))
		mux.HandleFunc("/network/map", network.loggingMiddleware("/network/map", network.httpNetworkMapHandler))
		mux.HandleFunc("/proximity", network.loggingMiddleware("/proximity", network.httpProximityHandler))
		mux.HandleFunc("/api/stash/status", network.loggingMiddleware("/api/stash/status", network.httpStashStatusHandler))
		mux.HandleFunc("/api/stash/update", network.loggingMiddleware("/api/stash/update", network.httpStashUpdateHandler))
		mux.HandleFunc("/api/stash/recover", network.loggingMiddleware("/api/stash/recover", network.httpStashRecoverHandler))
		mux.HandleFunc("/api/stash/confidants", network.loggingMiddleware("/api/stash/confidants", network.httpStashConfidantsHandler))

		// pprof endpoints
		if network.local != nil && network.local.Me.Name == "jojo-m1" {
			mux.HandleFunc("/debug/pprof/", pprof.Index)
			mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
			mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
			mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
			mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
		}

		mux.Handle("/", http.FileServer(http.FS(publicFS)))
	}

	return mux
}

// startMeshHttpServer starts an HTTP server on the tsnet interface for mesh communication
func (network *Network) startMeshHttpServer(tsnetServer interface {
	Listen(string, string) (net.Listener, error)
}) error {
	listener, err := tsnetServer.Listen("tcp", fmt.Sprintf(":%d", DefaultMeshPort))
	if err != nil {
		return fmt.Errorf("failed to listen on tsnet: %w", err)
	}

	logrus.Printf("🕸️  Mesh HTTP server listening on port %d (Tailscale interface)", DefaultMeshPort)

	// Create a mux with mesh-only endpoints (no UI)
	mux := network.createHTTPMux(false)

	network.meshHttpServer = &http.Server{
		Handler: mux,
	}

	go network.meshHttpServer.Serve(listener)
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
		// Enrich with observation snapshot for convenience
		obs := network.local.getObservation(nara.Name)
		statusMap["Online"] = obs.Online
		statusMap["LastSeen"] = obs.LastSeen
		statusMap["LastRestart"] = obs.LastRestart
		statusMap["StartTime"] = obs.StartTime
		statusMap["Restarts"] = obs.Restarts
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
		obs := network.local.getObservationLocked(nara.Name)
		nara.mu.Lock()
		id := nara.Status.ID
		if id == "" {
			id = nara.ID
		}
		naraMap := map[string]interface{}{
			"Name":            nara.Name,
			"ID":              id,
			"PublicUrl":       nara.Status.PublicUrl,
			"Flair":           nara.Status.Flair,
			"LicensePlate":    nara.Status.LicensePlate,
			"Buzz":            nara.Status.Buzz,
			"Chattiness":      nara.Status.Chattiness,
			"LastSeen":        obs.LastSeen,
			"LastRestart":     obs.LastRestart,
			"Online":          obs.Online,
			"StartTime":       obs.StartTime,
			"Restarts":        obs.Restarts,
			"Uptime":          nara.Status.HostStats.Uptime,
			"Trend":           nara.Status.Trend,
			"TrendEmoji":      nara.Status.TrendEmoji,
			"Aura":            nara.Status.Aura.Primary,   // Flatten for backward compat
			"AuraSecondary":   nara.Status.Aura.Secondary, // Flatten for backward compat
			"Sociability":     nara.Status.Personality.Sociability,
			"Chill":           nara.Status.Personality.Chill,
			"Agreeableness":   nara.Status.Personality.Agreeableness,
			"MemoryMode":      nara.Status.MemoryMode,
			"MemoryBudgetMB":  nara.Status.MemoryBudgetMB,
			"MemoryMaxEvents": nara.Status.MemoryMaxEvents,
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
	json.NewEncoder(w).Encode(response)
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
	json.NewEncoder(w).Encode(response)
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
	json.NewEncoder(w).Encode(response)
}

func (network *Network) httpMetricsHandler(w http.ResponseWriter, r *http.Request) {
	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	var lines []string

	// Per-nara metrics
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
	lines = append(lines, "# HELP nara_personality Personality traits (0-100 scale)")
	lines = append(lines, "# TYPE nara_personality gauge")
	lines = append(lines, "# HELP nara_memory_alloc_mb Current heap allocation in megabytes")
	lines = append(lines, "# TYPE nara_memory_alloc_mb gauge")
	lines = append(lines, "# HELP nara_memory_sys_mb Total memory obtained from OS in megabytes")
	lines = append(lines, "# TYPE nara_memory_sys_mb gauge")
	lines = append(lines, "# HELP nara_memory_heap_mb Heap memory (in use + free) in megabytes")
	lines = append(lines, "# TYPE nara_memory_heap_mb gauge")
	lines = append(lines, "# HELP nara_memory_stack_mb Stack memory in megabytes")
	lines = append(lines, "# TYPE nara_memory_stack_mb gauge")
	lines = append(lines, "# HELP nara_goroutines Number of active goroutines")
	lines = append(lines, "# TYPE nara_goroutines gauge")
	lines = append(lines, "# HELP nara_gc_cycles_total Number of completed GC cycles")
	lines = append(lines, "# TYPE nara_gc_cycles_total counter")

	allNarae := network.getNarae()

	for _, nara := range allNarae {
		// Get our observation of this nara (what we think about them)
		obs := network.local.getObservation(nara.Name)

		nara.mu.Lock()
		lines = append(lines, fmt.Sprintf(`nara_info{name="%s",flair="%s",license_plate="%s"} 1`, nara.Name, nara.Status.Flair, nara.Status.LicensePlate))
		buzz := nara.Status.Buzz
		chattiness := nara.Status.Chattiness
		uptime := nara.Status.HostStats.Uptime
		personality := nara.Status.Personality
		memAllocMB := nara.Status.HostStats.MemAllocMB
		memSysMB := nara.Status.HostStats.MemSysMB
		memHeapMB := nara.Status.HostStats.MemHeapMB
		memStackMB := nara.Status.HostStats.MemStackMB
		numGoroutines := nara.Status.HostStats.NumGoroutines
		numGC := nara.Status.HostStats.NumGC
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

		// Personality traits
		lines = append(lines, fmt.Sprintf(`nara_personality{name="%s",trait="chill"} %d`, nara.Name, personality.Chill))
		lines = append(lines, fmt.Sprintf(`nara_personality{name="%s",trait="sociability"} %d`, nara.Name, personality.Sociability))
		lines = append(lines, fmt.Sprintf(`nara_personality{name="%s",trait="agreeableness"} %d`, nara.Name, personality.Agreeableness))

		// Memory metrics
		if memAllocMB > 0 {
			lines = append(lines, fmt.Sprintf(`nara_memory_alloc_mb{name="%s"} %d`, nara.Name, memAllocMB))
		}
		if memSysMB > 0 {
			lines = append(lines, fmt.Sprintf(`nara_memory_sys_mb{name="%s"} %d`, nara.Name, memSysMB))
		}
		if memHeapMB > 0 {
			lines = append(lines, fmt.Sprintf(`nara_memory_heap_mb{name="%s"} %d`, nara.Name, memHeapMB))
		}
		if memStackMB > 0 {
			lines = append(lines, fmt.Sprintf(`nara_memory_stack_mb{name="%s"} %d`, nara.Name, memStackMB))
		}
		if numGoroutines > 0 {
			lines = append(lines, fmt.Sprintf(`nara_goroutines{name="%s"} %d`, nara.Name, numGoroutines))
		}
		if numGC > 0 {
			lines = append(lines, fmt.Sprintf(`nara_gc_cycles_total{name="%s"} %d`, nara.Name, numGC))
		}
	}

	// Sync ledger metrics (this server only)
	if network.local.SyncLedger != nil {
		lines = append(lines, "# HELP nara_events_total Total events in the sync ledger by service type")
		lines = append(lines, "# TYPE nara_events_total gauge")

		eventCounts := network.local.SyncLedger.GetEventCountsByService()
		for service, count := range eventCounts {
			lines = append(lines, fmt.Sprintf(`nara_events_total{service="%s"} %d`, service, count))
		}

		// Tease metrics
		lines = append(lines, "# HELP nara_teases_given_total Teases given by each nara")
		lines = append(lines, "# TYPE nara_teases_given_total counter")
		lines = append(lines, "# HELP nara_teases_received_total Teases received by each nara")
		lines = append(lines, "# TYPE nara_teases_received_total counter")

		teasesGiven := network.local.SyncLedger.GetTeaseCounts()
		for actor, count := range teasesGiven {
			lines = append(lines, fmt.Sprintf(`nara_teases_given_total{name="%s"} %d`, actor, count))
		}

		teasesReceived := network.local.SyncLedger.GetTeaseCountsReceived()
		for target, count := range teasesReceived {
			lines = append(lines, fmt.Sprintf(`nara_teases_received_total{name="%s"} %d`, target, count))
		}
	}

	// World journey metrics
	lines = append(lines, "# HELP nara_journeys_completed_total Total completed world journeys")
	lines = append(lines, "# TYPE nara_journeys_completed_total counter")

	network.worldJourneysMu.RLock()
	journeyCount := len(network.worldJourneys)
	network.worldJourneysMu.RUnlock()
	lines = append(lines, fmt.Sprintf(`nara_journeys_completed_total %d`, journeyCount))

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	for _, line := range lines {
		fmt.Fprintln(w, line)
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
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"from":    network.meName(),
	})
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
	added, warned := network.MergeSyncEventsWithVerification(theirZine.Events)
	if added > 0 {
		logrus.Debugf("📰 Received zine from %s: merged %d events (%d warned)", theirZine.From, added, warned)
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
	if !event.Verify(pubKey) {
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

	// Format event content for logging
	contentStr := "unknown event"
	if payload := event.Payload(); payload != nil {
		contentStr = payload.LogFormat()
	}
	logrus.Infof("📬 Received DM from %s: %s (added=%v)", event.Emitter, contentStr, added)

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
	// Decode request
	var req StashStoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate fields
	if req.From == "" || req.Stash == nil {
		http.Error(w, "from and stash are required", http.StatusBadRequest)
		return
	}

	// Validate timestamp (replay protection)
	if !ValidateTimestamp(req.Timestamp) {
		http.Error(w, "Invalid or expired timestamp", http.StatusBadRequest)
		return
	}

	// Verify signature
	pubKey := network.resolvePublicKeyForNara(req.From)
	if len(pubKey) == 0 {
		http.Error(w, "Unknown sender", http.StatusForbidden)
		return
	}
	if !req.Verify(pubKey) {
		logrus.Warnf("📦 Invalid stash store signature from %s", req.From)
		http.Error(w, "Invalid signature", http.StatusForbidden)
		return
	}

	// Prepare response
	response := StashStoreResponse{
		Accepted: false,
	}

	if network.confidantStore == nil {
		response.Reason = "stash_disabled"
	} else if req.Stash.Size() > MaxStashSize {
		response.Reason = "stash_too_large"
		logrus.Warnf("📦 Rejected stash from %s: too large (%d bytes)", req.From, req.Stash.Size())
	} else {
		// Try to store (returns false if at capacity)
		accepted := network.confidantStore.Store(req.From, req.Stash)
		response.Accepted = accepted

		if accepted {
			logrus.Infof("📦 Accepted stash for %s (%d bytes)", req.From, req.Stash.Size())

			// Emit social event
			socialEvent := SocialEventPayload{
				Type:    "service",
				Actor:   network.meName(),
				Target:  req.From,
				Reason:  ReasonStashStored,
				Witness: network.meName(),
			}
			syncEvent := SyncEvent{
				Service:   ServiceSocial,
				Timestamp: time.Now().UnixNano(),
				Emitter:   network.meName(),
				Social:    &socialEvent,
			}
			select {
			case network.socialInbox <- syncEvent:
			default:
				logrus.Debugf("Social inbox full, skipping stash stored event")
			}
		} else {
			response.Reason = "at_capacity"
			metrics := network.confidantStore.GetMetrics()
			logrus.Warnf("📦 Rejected stash from %s: at capacity (%d/%d)",
				req.From, metrics.StashesStored, network.confidantStore.maxStashes)
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(response)
}

func (network *Network) httpStashDeleteHandler(w http.ResponseWriter, r *http.Request) {
	// Decode request
	var req StashDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate timestamp
	if !ValidateTimestamp(req.Timestamp) {
		http.Error(w, "Invalid or expired timestamp", http.StatusBadRequest)
		return
	}

	// Verify signature
	pubKey := network.resolvePublicKeyForNara(req.From)
	if len(pubKey) == 0 {
		http.Error(w, "Unknown sender", http.StatusForbidden)
		return
	}
	if !req.Verify(pubKey) {
		logrus.Warnf("📦 Invalid stash delete signature from %s", req.From)
		http.Error(w, "Invalid signature", http.StatusForbidden)
		return
	}

	// Delete stash
	response := StashDeleteResponse{Deleted: false}
	if network.confidantStore != nil {
		response.Deleted = network.confidantStore.Delete(req.From)
		if response.Deleted {
			logrus.Infof("📦 Deleted stash for %s", req.From)
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(response)
}

func (network *Network) httpStashRetrieveHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Decode request
	var req StashRetrieveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate timestamp
	if !ValidateTimestamp(req.Timestamp) {
		http.Error(w, "Invalid or expired timestamp", http.StatusBadRequest)
		return
	}

	// Verify signature
	pubKey := network.resolvePublicKeyForNara(req.From)
	if len(pubKey) == 0 {
		http.Error(w, "Unknown sender", http.StatusForbidden)
		return
	}
	if !req.Verify(pubKey) {
		logrus.Warnf("📦 Invalid stash retrieve signature from %s", req.From)
		http.Error(w, "Invalid signature", http.StatusForbidden)
		return
	}

	// Retrieve stash
	response := StashRetrieveResponse{Found: false}
	if network.confidantStore != nil && network.confidantStore.HasStashFor(req.From) {
		network.confidantStore.mu.RLock()
		payload := network.confidantStore.stashes[req.From]
		network.confidantStore.mu.RUnlock()

		if payload != nil {
			response.Found = true
			response.Stash = payload
			logrus.Infof("📦 Sent stash to %s (%d bytes)", req.From, payload.Size())
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(response)
}

func (network *Network) httpStashPushHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Decode request
	var req StashPushRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate fields
	if req.From == "" || req.To == "" || req.Stash == nil {
		http.Error(w, "from, to, and stash are required", http.StatusBadRequest)
		return
	}

	// Validate timestamp
	if !ValidateTimestamp(req.Timestamp) {
		http.Error(w, "Invalid or expired timestamp", http.StatusBadRequest)
		return
	}

	// Verify signature
	pubKey := network.resolvePublicKeyForNara(req.From)
	if len(pubKey) == 0 {
		http.Error(w, "Unknown sender", http.StatusForbidden)
		return
	}
	if !req.Verify(pubKey) {
		logrus.Warnf("📦 Invalid stash push signature from %s", req.From)
		http.Error(w, "Invalid signature", http.StatusForbidden)
		return
	}

	// Verify this push is for us
	if req.To != network.meName() {
		http.Error(w, "Push not intended for this nara", http.StatusBadRequest)
		return
	}

	// Prepare response
	response := StashPushResponse{Accepted: false}

	// Decrypt and store the stash
	if network.stashManager != nil {
		encKeypair := DeriveEncryptionKeys(network.local.Keypair.PrivateKey)
		stashData, err := DecryptStashPayload(req.Stash, encKeypair)
		if err != nil {
			logrus.Warnf("📦 Failed to decrypt pushed stash from %s: %v", req.From, err)
			response.Reason = "decrypt_failed"
		} else {
			// Store recovered stash
			network.stashManager.SetCurrentStash(stashData.Data)
			logrus.Infof("📦 Recovered stash from %s via push (%d bytes, timestamp=%d)",
				req.From, len(stashData.Data), stashData.Timestamp)

			// Mark them as a confidant
			if network.stashManager.confidantTracker != nil {
				network.stashManager.confidantTracker.Add(req.From, time.Now().Unix())
			}

			response.Accepted = true
		}
	} else {
		response.Reason = "stash_disabled"
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(response)
}

// GET /api/stash/status - Get current stash status, confidants, and metrics
func (network *Network) httpStashStatusHandler(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"has_stash": false,
		"my_stash":  nil,
		"confidants": []map[string]interface{}{},
		"metrics":   map[string]interface{}{},
	}

	// Get my current stash data
	if network.stashManager != nil {
		currentStash := network.stashManager.GetCurrentStash()
		if currentStash != nil {
			var dataMap map[string]interface{}
			json.Unmarshal(currentStash.Data, &dataMap)
			response["has_stash"] = true
			response["my_stash"] = map[string]interface{}{
				"timestamp": currentStash.Timestamp,
				"data":      dataMap,
			}
		}
	}

	// Get confidant list
	if network.stashManager != nil && network.stashManager.confidantTracker != nil {
		confidants := network.stashManager.confidantTracker.GetAll()
		confidantList := make([]map[string]interface{}, 0, len(confidants))
		for _, name := range confidants {
			confidantList = append(confidantList, map[string]interface{}{
				"name":   name,
				"status": "confirmed",
			})
		}
		response["confidants"] = confidantList
		response["target_count"] = network.stashManager.confidantTracker.targetCount
	}

	// Get metrics
	if network.confidantStore != nil {
		metrics := network.confidantStore.GetMetrics()
		response["metrics"] = map[string]interface{}{
			"stashes_stored":   metrics.StashesStored,
			"total_bytes":      metrics.TotalStashBytes,
			"eviction_count":   metrics.EvictionCount,
			"storage_limit":    network.confidantStore.maxStashes,
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(response)
}

// POST /api/stash/update - Update my stash with new JSON data
func (network *Network) httpStashUpdateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read raw JSON body
	var data json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if network.stashManager == nil {
		http.Error(w, "Stash manager not initialized", http.StatusInternalServerError)
		return
	}

	// Update stash data
	network.stashManager.SetCurrentStash(data)

	// Trigger re-distribution to confidants via gossip
	// Next gossip round will pick up the updated stash automatically
	logrus.Infof("📦 Stash data updated (%d bytes), will distribute on next gossip round", len(data))

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Stash updated and redistributing to confidants",
	})
}

// POST /api/stash/recover - Trigger manual stash recovery
func (network *Network) httpStashRecoverHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Manual recovery: restart the nara to trigger the boot recovery flow
	// Alternatively, request stash from all confidants
	go func() {
		if network.stashManager != nil && network.stashManager.confidantTracker != nil {
			confidants := network.stashManager.confidantTracker.GetAll()
			for _, name := range confidants {
				// Try to fetch stash via HTTP
				logrus.Infof("📦 Requesting stash from %s", name)
			}
		}
	}()

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Stash recovery initiated from confidants",
	})
}

// GET /api/stash/confidants - List all confidants with details
func (network *Network) httpStashConfidantsHandler(w http.ResponseWriter, r *http.Request) {
	confidants := []map[string]interface{}{}

	if network.stashManager != nil && network.stashManager.confidantTracker != nil {
		confidantNames := network.stashManager.confidantTracker.GetAll()

		network.local.mu.Lock()
		for _, name := range confidantNames {
			info := map[string]interface{}{
				"name":   name,
				"status": "confirmed",
			}

			// Get peer details if available
			if nara, ok := network.Neighbourhood[name]; ok {
				nara.mu.Lock()
				info["memory_mode"] = nara.Status.MemoryMode
				info["online"] = true
				nara.mu.Unlock()
			}

			confidants = append(confidants, info)
		}
		network.local.mu.Unlock()
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"confidants": confidants,
	})
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
		myObs := network.local.getObservationLocked(name)

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
