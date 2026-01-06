package nara

import (
	"embed"
	"encoding/json"
	"fmt"
	"github.com/sirupsen/logrus"
	"io/fs"
	"net"
	"net/http"
)

//go:embed nara-web/public/*
var staticContent embed.FS

func (network *Network) startHttpServer(httpAddr string) error {
	listen_interface := httpAddr
	if listen_interface == "" {
		listen_interface = ":0"
	}

	listener, err := net.Listen("tcp", listen_interface)
	if err != nil {
		return fmt.Errorf("listen error: %w", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	logrus.Printf("Listening for HTTP on port %d", port)

	http.HandleFunc("/api.json", network.httpApiJsonHandler)
	http.HandleFunc("/narae.json", network.httpNaraeJsonHandler)
	http.HandleFunc("/metrics", network.httpMetricsHandler)
	http.HandleFunc("/status/", network.httpStatusJsonHandler)
	http.HandleFunc("/events", network.httpEventsSSEHandler)
	http.HandleFunc("/social/clout", network.httpCloutHandler)
	http.HandleFunc("/social/recent", network.httpRecentEventsHandler)
	http.HandleFunc("/world/start", network.httpWorldStartHandler)
	http.HandleFunc("/world/journeys", network.httpWorldJourneysHandler)
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
