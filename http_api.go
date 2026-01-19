package nara

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/eljojo/nara/types"
)

func (network *Network) httpApiJsonHandler(w http.ResponseWriter, r *http.Request) {
	// No lock needed - getNarae() now handles locking internally via getAllNarasSnapshot()
	allNarae := network.getNarae()

	var naras []map[string]interface{}
	for _, nara := range allNarae {
		nara.mu.Lock()
		statusMap := make(map[string]interface{})
		jsonStatus, _ := json.Marshal(nara.Status)
		if err := json.Unmarshal(jsonStatus, &statusMap); err != nil {
			logrus.WithError(err).Warn("Failed to unmarshal status to map")
		}
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
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logrus.WithError(err).Warn("Failed to encode response")
	}
}

// httpProfileJsonHandler returns a rich profile payload for a single nara.
// Path: /profile/{name}.json
func (network *Network) httpProfileJsonHandler(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/profile/") || !strings.HasSuffix(r.URL.Path, ".json") {
		http.NotFound(w, r)
		return
	}
	encoded := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/profile/"), ".json")
	name, err := url.PathUnescape(encoded)
	if err != nil || name == "" {
		http.NotFound(w, r)
		return
	}

	naraName := types.NaraName(name)

	// Check if requesting profile for local nara (me)
	var nara *Nara
	var isLocalNara bool
	if naraName == network.local.Me.Name {
		nara = network.local.Me
		isLocalNara = true
	} else {
		// Look in neighbourhood
		network.local.mu.Lock()
		var exists bool
		nara, exists = network.Neighbourhood[naraName]
		network.local.mu.Unlock()
		if !exists {
			http.NotFound(w, r)
			return
		}
	}

	nara.mu.Lock()
	status := nara.Status
	nara.mu.Unlock()

	// For local nara, use self observation; for others, use neighbourhood observation
	var obs NaraObservation
	if isLocalNara {
		obs = network.local.getMeObservation()
	} else {
		obs = network.local.getObservation(naraName)
	}

	// Event store stats
	var eventStoreByService map[string]int
	var eventStoreTotal int
	var eventStoreCritical int
	var recentTeases []map[string]interface{}
	fromCounts := make(map[types.NaraName]int)
	toCounts := make(map[types.NaraName]int)
	if network.local.SyncLedger != nil {
		eventStoreByService = network.local.SyncLedger.GetEventCountsByService()
		eventStoreTotal = network.local.SyncLedger.EventCount()
		eventStoreCritical = network.local.SyncLedger.GetCriticalEventCount()

		for i := len(network.local.SyncLedger.Events) - 1; i >= 0; i-- {
			e := network.local.SyncLedger.Events[i]
			if e.Service == ServiceSocial && e.Social != nil && e.Social.Type == "tease" {
				actor := e.Social.Actor
				target := e.Social.Target
				if target == naraName {
					fromCounts[actor]++
				}
				if actor == naraName {
					toCounts[target]++
				}
				if actor == naraName || target == naraName {
					if len(recentTeases) < 50 {
						recentTeases = append(recentTeases, map[string]interface{}{
							"actor":     actor,
							"target":    target,
							"reason":    e.Social.Reason,
							"timestamp": e.Timestamp / 1e9, // seconds
						})
					}
				}
			}
		}
	}

	bestFrom := map[string]interface{}{"name": "", "count": 0}
	for actor, c := range fromCounts {
		if c > bestFrom["count"].(int) && actor != "" && actor != naraName {
			bestFrom["name"] = actor
			bestFrom["count"] = c
		}
	}
	bestTo := map[string]interface{}{"name": "", "count": 0}
	for target, c := range toCounts {
		if c > bestTo["count"].(int) && target != "" && target != naraName {
			bestTo["name"] = target
			bestTo["count"] = c
		}
	}

	observations := make(map[string]NaraObservation)
	if status.Observations != nil {
		for k, v := range status.Observations {
			observations[k.String()] = v
		}
	}

	id := status.ID
	if id == "" {
		id = nara.ID
	}

	payload := map[string]interface{}{
		"id":                     id,
		"name":                   name,
		"flair":                  status.Flair,
		"license_plate":          status.LicensePlate,
		"aura":                   status.Aura,
		"personality":            status.Personality,
		"chattiness":             status.Chattiness,
		"buzz":                   status.Buzz,
		"memory_mode":            status.MemoryMode,
		"memory_budget_mb":       status.MemoryBudgetMB,
		"memory_max_events":      status.MemoryMaxEvents,
		"mesh_enabled":           status.MeshEnabled,
		"mesh_ip":                status.MeshIP,
		"transport_mode":         status.TransportMode,
		"trend":                  status.Trend,
		"trend_emoji":            status.TrendEmoji,
		"host_stats":             status.HostStats,
		"online":                 obs.Online,
		"last_seen":              obs.LastSeen,
		"last_restart":           obs.LastRestart,
		"start_time":             obs.StartTime,
		"restarts":               obs.Restarts,
		"observations":           observations,
		"event_store_by_service": eventStoreByService,
		"event_store_total":      eventStoreTotal,
		"event_store_critical":   eventStoreCritical,
		"recent_teases":          recentTeases,
		"best_friends": map[string]interface{}{
			"from": bestFrom,
			"to":   bestTo,
		},
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		logrus.WithError(err).Warn("Failed to encode payload")
	}
}

func (network *Network) httpNaraeJsonHandler(w http.ResponseWriter, r *http.Request) {
	// No lock needed - getNarae() now handles locking internally via getAllNarasSnapshot()
	allNarae := network.getNarae()

	// Get clout scores
	var cloutScores map[types.NaraName]float64
	if network.local.Projections != nil {
		cloutScores = network.local.Projections.Clout().DeriveClout(network.local.Soul, network.local.Me.Status.Personality)
	}

	var naras []map[string]interface{}
	for _, nara := range allNarae {
		obs := network.local.getObservationLocked(nara.Name)
		nara.mu.Lock()
		id := nara.Status.ID
		if id == "" {
			id = nara.ID
		}
		naraMap := map[string]interface{}{
			"Name":                   nara.Name,
			"ID":                     id,
			"PublicUrl":              nara.Status.PublicUrl,
			"Flair":                  nara.Status.Flair,
			"LicensePlate":           nara.Status.LicensePlate,
			"Buzz":                   nara.Status.Buzz,
			"Chattiness":             nara.Status.Chattiness,
			"LastSeen":               obs.LastSeen,
			"LastRestart":            obs.LastRestart,
			"Online":                 obs.Online,
			"StartTime":              obs.StartTime,
			"Restarts":               obs.Restarts,
			"Uptime":                 nara.Status.HostStats.Uptime,
			"Trend":                  nara.Status.Trend,
			"TrendEmoji":             nara.Status.TrendEmoji,
			"Aura":                   nara.Status.Aura.Primary,   // Flatten for backward compat
			"AuraSecondary":          nara.Status.Aura.Secondary, // Flatten for backward compat
			"Sociability":            nara.Status.Personality.Sociability,
			"Chill":                  nara.Status.Personality.Chill,
			"Agreeableness":          nara.Status.Personality.Agreeableness,
			"MemoryMode":             nara.Status.MemoryMode,
			"MemoryBudgetMB":         nara.Status.MemoryBudgetMB,
			"MemoryMaxEvents":        nara.Status.MemoryMaxEvents,
			"HostStats":              nara.Status.HostStats,
			"event_store_total":      nara.Status.EventStoreTotal,
			"event_store_by_service": nara.Status.EventStoreByService,
			"event_store_critical":   nara.Status.EventStoreCritical,
			"Clout":                  cloutScores[nara.Name],
		}
		nara.mu.Unlock()
		naras = append(naras, naraMap)
	}

	response := map[string]interface{}{
		"naras":  naras,
		"server": network.local.Me.Name,
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logrus.WithError(err).Warn("Failed to encode response")
	}
}

func (network *Network) httpStatusJsonHandler(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Path[len("/status/") : len(r.URL.Path)-len(".json")]
	network.local.mu.Lock()
	nara, exists := network.Neighbourhood[types.NaraName(name)]
	network.local.mu.Unlock()

	if !exists {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(nara.Status); err != nil {
		logrus.WithError(err).Warn("Failed to encode nara status")
	}
}

func (network *Network) getNarae() []*Nara {
	// Use snapshot helper to avoid race conditions
	naras := network.getAllNarasSnapshot()

	// Add ourselves if not read-only
	if !network.ReadOnly {
		naras = append(naras, network.local.Me)
	}
	return naras
}

func (network *Network) httpMetricsHandler(w http.ResponseWriter, r *http.Request) {
	// No lock needed - getNarae() now handles locking internally via getAllNarasSnapshot()
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

// httpCheckpointsAllHandler serves all checkpoint events for boot recovery
// DEPRECATED: Use /events/sync with Mode: "page" and Services: ["checkpoint"] instead
// This endpoint is kept for backward compatibility with older naras during transition period
// TODO: Remove after ~6 months (2026-07) when all naras use unified /events/sync API
// Supports pagination via query parameters: limit and offset
func (network *Network) httpCheckpointsAllHandler(w http.ResponseWriter, r *http.Request) {
	if network.local == nil || network.local.SyncLedger == nil {
		http.Error(w, "ledger not available", http.StatusServiceUnavailable)
		return
	}

	// Parse pagination parameters
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit := 1000 // default limit
	if limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
			if limit > 10000 {
				limit = 10000 // max limit
			}
		}
	}

	offset := 0
	if offsetStr != "" {
		if parsed, err := strconv.Atoi(offsetStr); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	// Get all checkpoint events from the ledger
	network.local.SyncLedger.mu.RLock()
	var allCheckpoints []*SyncEvent
	for _, event := range network.local.SyncLedger.Events {
		if event.Service == ServiceCheckpoint && event.Checkpoint != nil {
			allCheckpoints = append(allCheckpoints, &event)
		}
	}
	network.local.SyncLedger.mu.RUnlock()

	// Apply pagination
	total := len(allCheckpoints)
	start := offset
	end := offset + limit

	if start >= total {
		start = total
		end = total
	} else if end > total {
		end = total
	}

	paginatedCheckpoints := allCheckpoints[start:end]
	hasMore := end < total

	response := map[string]interface{}{
		"server":      network.meName(),
		"total":       total,
		"count":       len(paginatedCheckpoints),
		"checkpoints": paginatedCheckpoints,
		"has_more":    hasMore,
		"offset":      offset,
		"limit":       limit,
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logrus.WithError(err).Warn("Failed to encode response")
	}
}
