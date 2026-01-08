package nara

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"sort"
	"time"

	"github.com/sirupsen/logrus"
)

const BlueJayURL = "https://nara.network/narae.json"

// MissingThreshold is the duration without updates before a nara is marked MISSING.
// This must be long enough to account for:
// - Variable posting intervals (naras post every 10-30 seconds, but can go quieter)
// - Network delays and occasional missed messages
// - Brief quiet periods that don't indicate actual offline status
// 5 minutes (300s) is chosen because:
// - Naras normally post every 30s, so 300s = 10 missed posts
// - This avoids false positives for quiet but online naras
// - Actual crashes/network issues will exceed this within reasonable time
const MissingThreshold int64 = 300 // seconds

var OpinionDelayOverride time.Duration = 0

type NaraObservation struct {
	Online       string
	StartTime    int64
	Restarts     int64
	LastSeen     int64
	LastRestart  int64
	ClusterName  string
	ClusterEmoji string
	// Latency measurement fields for Vivaldi coordinates
	LastPingRTT  float64 // Last measured RTT in milliseconds
	AvgPingRTT   float64 // Exponential moving average of RTT
	LastPingTime int64   // Unix timestamp of last ping
}

func (localNara *LocalNara) getMeObservation() NaraObservation {
	return localNara.getObservation(localNara.Me.Name)
}

func (localNara *LocalNara) setMeObservation(observation NaraObservation) {
	localNara.setObservation(localNara.Me.Name, observation)
}

func (localNara *LocalNara) getObservation(name string) NaraObservation {
	observation := localNara.Me.getObservation(name)
	return observation
}

func (localNara *LocalNara) getObservationLocked(name string) NaraObservation {
	// this is called when localNara.mu is already held
	// but we still need to lock localNara.Me.mu
	localNara.Me.mu.Lock()
	observation, _ := localNara.Me.Status.Observations[name]
	localNara.Me.mu.Unlock()
	return observation
}

func (localNara *LocalNara) setObservation(name string, observation NaraObservation) {
	localNara.mu.Lock()
	localNara.Me.setObservation(name, observation)
	localNara.mu.Unlock()
}

func (nara *Nara) getObservation(name string) NaraObservation {
	nara.mu.Lock()
	observation, _ := nara.Status.Observations[name]
	nara.mu.Unlock()
	return observation
}

func (nara *Nara) setObservation(name string, observation NaraObservation) {
	nara.mu.Lock()
	nara.Status.Observations[name] = observation
	nara.mu.Unlock()
}

func (network *Network) formOpinion() {
	if OpinionDelayOverride > 0 {
		logrus.Printf("🕵️  forming opinions (overridden) in %v...", OpinionDelayOverride)
		time.Sleep(OpinionDelayOverride)
	} else {
		wait := 3 * time.Minute
		if network.meName() == "blue-jay" {
			wait = 15 * time.Minute
		}

		logrus.Printf("🕵️  forming opinions in %v...", wait)
		time.Sleep(wait)
	}

	if network.meName() != "blue-jay" {
		network.fetchOpinionsFromBlueJay()
	}

	names := network.NeighbourhoodNames()

	for _, name := range names {
		observation := network.local.getObservation(name)
		startTime := network.findStartingTimeFromNeighbourhoodForNara(name)
		if startTime > 0 {
			observation.StartTime = startTime
		} else {
			logrus.Printf("couldn't adjust startTime for %s based on neighbour disagreement", name)
		}
		restarts := network.findRestartCountFromNeighbourhoodForNara(name)
		if restarts > 0 {
			observation.Restarts = restarts
		} else {
			logrus.Printf("couldn't adjust restart count for %s based on neighbour disagreement", name)
		}
		if name != network.meName() {
			lastRestart := network.findLastRestartFromNeighbourhoodForNara(name)
			if lastRestart > 0 {
				observation.LastRestart = lastRestart
			} else {
				logrus.Printf("couldn't adjust last restart date for %s based on neighbour disagreement", name)
			}
		}
		network.local.setObservation(name, observation)
	}
	logrus.Printf("👀  opinions formed")
}

func (network *Network) fetchOpinionsFromBlueJay() {
	client := http.Client{
		Timeout: 5 * time.Second,
	}
	resp, err := client.Get(BlueJayURL)
	if err != nil {
		logrus.Warnf("failed to fetch opinions from blue-jay: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logrus.Warnf("blue-jay returned non-OK status: %d", resp.StatusCode)
		return
	}

	var data struct {
		Naras []struct {
			Name        string
			StartTime   int64
			Restarts    int64
			LastRestart int64
		}
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		logrus.Warnf("failed to decode blue-jay response: %v", err)
		return
	}

	logrus.Printf("📋 fetched %d opinions from blue-jay", len(data.Naras))

	for _, n := range data.Naras {
		if n.Name == "" {
			continue
		}
		observation := network.local.getObservation(n.Name)
		if n.StartTime > 0 {
			observation.StartTime = n.StartTime
		}
		observation.Restarts = n.Restarts
		observation.LastRestart = n.LastRestart
		network.local.setObservation(n.Name, observation)
	}
}

// weightedObservation pairs a value with the observer's uptime (credibility weight)
type weightedObservation struct {
	value  int64
	uptime uint64
}

// observationCluster groups similar observations and tracks total credibility
type observationCluster struct {
	values      []int64
	totalUptime uint64
}

// findStartingTimeFromNeighbourhoodForNara uses uptime-weighted clustering consensus
// with a hierarchy of strategies from strictest to most permissive:
//
// Strategy 1 (Strong): Pick cluster with >= 2 agreeing observers and highest uptime
// Strategy 2 (Weak): Pick cluster with highest uptime (even single observer)
// Strategy 3 (Coin flip): If top 2 clusters are close in uptime, flip a coin
//
// This handles: exact agreement, small clock drift, competing opinions,
// and total chaos (with a sense of humor).
func (network *Network) findStartingTimeFromNeighbourhoodForNara(name string) int64 {
	const tolerance int64 = 60 // seconds - handles clock drift

	var observations []weightedObservation
	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	for _, nara := range network.Neighbourhood {
		obs := nara.getObservation(name)
		if obs.StartTime > 0 {
			uptime := nara.Status.HostStats.Uptime
			if uptime == 0 {
				uptime = 1 // minimum weight
			}
			observations = append(observations, weightedObservation{
				value:  obs.StartTime,
				uptime: uptime,
			})
		}
	}

	if len(observations) == 0 {
		return 0
	}

	// Sort by value for clustering
	sort.Slice(observations, func(i, j int) bool {
		return observations[i].value < observations[j].value
	})

	// Build clusters: group values within tolerance of cluster's first value
	// This prevents "chaining" where [100, 159, 218] would incorrectly cluster
	// (159-100<=60, 218-159<=60, but 218-100>60)
	var clusters []observationCluster
	currentCluster := observationCluster{
		values:      []int64{observations[0].value},
		totalUptime: observations[0].uptime,
	}
	clusterStart := observations[0].value

	for i := 1; i < len(observations); i++ {
		obs := observations[i]

		if obs.value-clusterStart <= tolerance {
			// Within tolerance of cluster start - add to current cluster
			currentCluster.values = append(currentCluster.values, obs.value)
			currentCluster.totalUptime += obs.uptime
		} else {
			// Gap too large - start new cluster
			clusters = append(clusters, currentCluster)
			currentCluster = observationCluster{
				values:      []int64{obs.value},
				totalUptime: obs.uptime,
			}
			clusterStart = obs.value
		}
	}
	clusters = append(clusters, currentCluster)

	// Sort clusters by total uptime (descending)
	sort.Slice(clusters, func(i, j int) bool {
		return clusters[i].totalUptime > clusters[j].totalUptime
	})

	// Strategy 1 (Strong): cluster with >= 2 observers and highest uptime
	for _, cluster := range clusters {
		if len(cluster.values) >= 2 {
			return cluster.values[len(cluster.values)/2]
		}
	}

	// Strategy 2 & 3: No cluster has 2+ observers
	// Check if we should flip a coin between top candidates
	if len(clusters) >= 2 {
		first := clusters[0]
		second := clusters[1]

		// If top 2 are within 20% of each other, flip a coin
		threshold := first.totalUptime * 80 / 100
		if second.totalUptime >= threshold {
			// 🪙 Coin flip time!
			coin := time.Now().UnixNano() % 2
			if coin == 0 {
				logrus.Printf("🪙 coin flip! chose %d over %d (both had ~%d uptime)",
					first.values[0], second.values[0], first.totalUptime)
				return first.values[len(first.values)/2]
			} else {
				logrus.Printf("🪙 coin flip! chose %d over %d (both had ~%d uptime)",
					second.values[0], first.values[0], second.totalUptime)
				return second.values[len(second.values)/2]
			}
		}
	}

	// Strategy 2 (Weak): just pick highest uptime cluster
	return clusters[0].values[len(clusters[0].values)/2]
}

func (network *Network) recordObservationOnlineNara(name string) {
	network.local.mu.Lock()
	_, present := network.Neighbourhood[name]
	network.local.mu.Unlock()
	if !present && name != network.meName() {
		network.importNara(NewNara(name))
	}

	observation := network.local.getObservation(name)

	// Track previous state for teasing triggers
	previousState := observation.Online
	previousTrend := ""
	if nara := network.getNara(name); nara.Name != "" {
		previousTrend = nara.Status.Trend
	}

	if observation.Online == "" && name != network.meName() {
		logrus.Printf("observation: seen %s for the first time", name)
		network.Buzz.increase(3)

		// Emit first-seen observation event in event-primary mode
		if useObservationEvents() && !network.local.isBooting() && network.local.SyncLedger != nil {
			event := NewFirstSeenObservationEvent(network.meName(), name, time.Now().Unix())
			network.local.SyncLedger.AddEventWithDedup(event)
			logrus.Debugf("📊 First-seen observation event: %s", name)
		}
	}

	// "our" observation is mostly a mirror of what others think of us
	if observation.StartTime == 0 || name == network.meName() {
		restarts := network.findRestartCountFromNeighbourhoodForNara(name)
		startTime := network.findStartingTimeFromNeighbourhoodForNara(name)
		lastRestart := network.findLastRestartFromNeighbourhoodForNara(name)

		if restarts > 0 {
			observation.Restarts = restarts
		}
		if startTime > 0 {
			observation.StartTime = startTime
		}
		if lastRestart > 0 && name != network.meName() {
			observation.LastRestart = lastRestart
		}

		if observation.StartTime == 0 && name == network.meName() && !network.local.isBooting() {
			observation.StartTime = observation.LastRestart
			logrus.Printf("⚠️ set StartTime to LastRestart")
		}
	}

	// Track if we detected a restart (to avoid duplicate event emissions)
	wasRestart := false
	if !observation.isOnline() && observation.Online != "" {
		observation.LastRestart = time.Now().Unix()
		observation.Restarts = observation.Restarts + 1
		logrus.Printf("observation: %s came back online", name)
		network.Buzz.increase(3)
		wasRestart = true

		// Emit restart observation event in event-primary mode
		if useObservationEvents() && !network.local.isBooting() && network.local.SyncLedger != nil && name != network.meName() {
			event := NewRestartObservationEvent(network.meName(), name, observation.StartTime, observation.Restarts)
			network.local.SyncLedger.AddEventWithDedup(event)
			logrus.Debugf("📊 Restart observation event: %s (restart #%d)", name, observation.Restarts)
		}
	}

	observation.Online = "ONLINE"
	observation.LastSeen = time.Now().Unix()
	network.local.setObservation(name, observation)

	// Record observation event when state changes to ONLINE
	// Only emit status-change if this wasn't already handled by restart event
	// This avoids duplicate events for the same transition
	if name != network.meName() && !network.local.isBooting() && network.local.SyncLedger != nil {
		if previousState != "" && previousState != "ONLINE" && !wasRestart {
			// Emit status-change observation event in event-primary mode
			if useObservationEvents() {
				event := NewStatusChangeObservationEvent(network.meName(), name, "ONLINE")
				network.local.SyncLedger.AddEventFiltered(event, network.local.Me.Status.Personality)
				logrus.Debugf("📊 Status-change observation event: %s → ONLINE", name)
			} else {
				// Legacy: State changed from MISSING/OFFLINE to ONLINE
				event := NewObservationEvent(network.meName(), name, ReasonOnline)
				network.local.SyncLedger.AddSocialEventFilteredLegacy(event, network.local.Me.Status.Personality)
				logrus.Printf("observation: %s came online", name)
			}
		}
	}

	// Check teasing triggers after observation update
	// Run inline - it's cheap (mostly returns early) and cooldown prevents spam
	if !network.local.isBooting() && name != network.meName() {
		network.checkAndTease(name, previousState, previousTrend)
	}
}

func (network *Network) findRestartCountFromNeighbourhoodForNara(name string) int64 {
	network.local.mu.Lock()
	defer network.local.mu.Unlock()
	var maxSeen int64 = 0

	for _, nara := range network.Neighbourhood {
		restarts := nara.getObservation(name).Restarts
		if restarts > maxSeen {
			maxSeen = restarts
		}
	}

	return maxSeen
}

func (network *Network) findLastRestartFromNeighbourhoodForNara(name string) int64 {
	values := make(map[int64]int)
	network.local.mu.Lock()
	defer network.local.mu.Unlock()
	for _, nara := range network.Neighbourhood {
		last_restart := nara.getObservation(name).LastRestart
		if last_restart > 0 {
			values[last_restart] += 1
		}
	}

	var result int64
	maxSeen := 0

	total_votes := 0
	for _, count := range values {
		total_votes += count
	}
	threshold := total_votes / 4

	for last_restart, count := range values {
		if count > maxSeen && count > threshold {
			maxSeen = count
			result = last_restart
		}
	}

	return result
}

func (network *Network) observationMaintenance() {
	for {
		network.local.Me.mu.Lock()
		observations := make(map[string]NaraObservation)
		for name, obs := range network.local.Me.Status.Observations {
			observations[name] = obs
		}
		network.local.Me.mu.Unlock()

		now := time.Now().Unix()

		for name, observation := range observations {
			// only do maintenance on naras that are online
			if !observation.isOnline() {
				if observation.ClusterName != "" {
					// reset cluster for offline naras
					observation.ClusterName = ""
					observation.ClusterEmoji = ""
					network.local.setObservation(name, observation)
				}

				continue
			}

			// mark missing after MissingThreshold seconds of no updates
			if (now-observation.LastSeen) > MissingThreshold && !network.skippingEvents && !network.local.isBooting() {
				observation.Online = "MISSING"
				network.local.setObservation(name, observation)
				logrus.Printf("observation: %s has disappeared", name)
				network.Buzz.increase(10)

				// Use delayed reporting to avoid redundant MISSING events from multiple observers
				// "If no one says anything, I guess I'll say something about it"
				if name != network.meName() && network.local.SyncLedger != nil {
					go network.reportMissingWithDelay(name)
				}
			}
		}

		network.neighbourhoodMaintenance()

		// set own Flair
		newFlair := network.local.Flair()
		if newFlair != network.local.Me.Status.Flair {
			network.Buzz.increase(2)
		}
		network.local.Me.Status.Flair = newFlair
		network.local.Me.Status.LicensePlate = network.local.LicensePlate()

		select {
		case <-time.After(1 * time.Second):
			// Continue to next iteration
		case <-network.ctx.Done():
			logrus.Debugf("observationMaintenance: shutting down gracefully")
			return
		}
	}
}

func (obs NaraObservation) isOnline() bool {
	return obs.Online == "ONLINE"
}

// reportMissingWithDelay implements "if no one says anything, I guess I'll say something"
// Waits a random delay, then checks if another observer already reported the subject as missing.
// If yes, stays silent. If no, emits the MISSING event.
// To read more, see: https://meshtastic.org/docs/overview/mesh-algo/#broadcasts-using-managed-flooding
func (network *Network) reportMissingWithDelay(subject string) {
	// Random delay 0-10 seconds to stagger reports
	delay := time.Duration(rand.Intn(10)) * time.Second

	select {
	case <-time.After(delay):
		// Continue to check and potentially report
	case <-network.ctx.Done():
		// Shutdown initiated, don't report
		return
	}

	// Check if another observer already reported this subject as MISSING/offline
	// Look for recent events (within last 15 seconds)
	recentCutoff := time.Now().Add(-15 * time.Second).UnixNano()

	events := network.local.SyncLedger.GetObservationEventsAbout(subject)
	for _, e := range events {
		// Check if this is a recent MISSING event from another observer
		if e.Timestamp > recentCutoff &&
			e.Observation != nil &&
			e.Observation.Observer != network.meName() &&
			(e.Observation.Type == "status-change" && e.Observation.OnlineState == "MISSING") {
			logrus.Debugf("🤐 Not reporting %s MISSING - %s already reported it", subject, e.Observation.Observer)
			return
		}
	}

	// No one else reported it, so we'll report it
	if useObservationEvents() {
		event := NewStatusChangeObservationEvent(network.meName(), subject, "MISSING")
		network.local.SyncLedger.AddEventFiltered(event, network.local.Me.Status.Personality)
		logrus.Debugf("📊 Status-change observation event: %s → MISSING (after %v delay)", subject, delay)
	} else {
		// Legacy mode
		event := NewObservationEvent(network.meName(), subject, ReasonOffline)
		network.local.SyncLedger.AddSocialEventFilteredLegacy(event, network.local.Me.Status.Personality)
		logrus.Printf("observation: %s went offline (disappeared) after %v delay", subject, delay)
	}
}
