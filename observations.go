package nara

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

const BlueJayURL = "https://nara.network/narae.json"

// MissingThresholdSeconds is the duration (in SECONDS) without updates before a nara is marked MISSING.
// This must be long enough to account for:
// - Variable posting intervals (naras post every 10-30 seconds, but can go quieter)
// - Network delays and occasional missed messages
// - Brief quiet periods that don't indicate actual offline status
// 5 minutes (300s) is chosen because:
// - Naras normally post every 30s, so 300s = 10 missed posts
// - This avoids false positives for quiet but online naras
// - Actual crashes/network issues will exceed this within reasonable time
//
// NOTE: This is in SECONDS because LastSeen uses time.Now().Unix() (seconds).
// For projections that use nanoseconds, multiply by int64(time.Second).
const MissingThresholdSeconds int64 = 300 // 5 minutes

// MissingThresholdGossipSeconds is a more lenient threshold (in SECONDS) for naras in gossip mode.
// Gossip mode relies on zine exchanges which happen less frequently (every 60s),
// and may not reach all naras in each round due to the random peer selection.
// 1 hour is chosen because:
// - Zines spread every 60s to 3-5 random peers
// - A nara might not be selected for several rounds
// - 1 hour allows for natural gossip propagation delays
//
// NOTE: This is in SECONDS. For projections, multiply by int64(time.Second).
const MissingThresholdGossipSeconds int64 = 3600 // 1 hour

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
	// Signal completion when done (allows backfillObservations to proceed)
	defer func() {
		if network.formOpinionsDone != nil {
			select {
			case <-network.formOpinionsDone:
				// Already closed
			default:
				close(network.formOpinionsDone)
				logrus.Debug("👀 opinions formed, signaling backfill to proceed")
			}
		}
	}()

	// Wait for boot recovery to complete first (so we have data to form opinions on)
	// Skip waiting in test mode (OpinionDelayOverride set) or if channel is nil
	if network.bootRecoveryDone != nil && OpinionDelayOverride == 0 {
		logrus.Debug("🕵️  waiting for boot recovery to complete...")
		<-network.bootRecoveryDone
		logrus.Debug("🕵️  boot recovery done, now starting opinion timer")
	}

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

	// First pass: identify and skip ghost naras (seen once briefly, never again)
	ghostCount := 0
	for _, name := range names {
		if network.isGhostNara(name) {
			ghostCount++
			logrus.Debugf("skipping ghost nara %s (no meaningful data from any neighbor)", name)
			continue
		}

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
	if ghostCount > 0 {
		logrus.Printf("👻 skipped %d ghost naras (no data from any neighbor)", ghostCount)
	}
	logrus.Printf("👀  opinions formed")

	// Garbage collect ghost naras that are safe to delete
	deleted := network.garbageCollectGhostNaras()
	if deleted > 0 {
		logrus.Printf("🗑️  garbage collected %d ghost naras from memory", deleted)
	}
}

// isGhostNara returns true if the nara appears to be a "ghost" - seen once briefly
// and never again, with no meaningful data from any neighbor. These are naras that
// should be skipped in opinion forming to avoid noise.
func (network *Network) isGhostNara(name string) bool {
	// Check our own observation first
	myObs := network.local.getObservation(name)
	if myObs.StartTime > 0 || myObs.Restarts > 0 || myObs.LastRestart > 0 {
		return false // We have data, not a ghost
	}

	// Check if any neighbor has meaningful data
	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	for _, nara := range network.Neighbourhood {
		obs := nara.getObservation(name)
		if obs.StartTime > 0 || obs.Restarts > 0 || obs.LastRestart > 0 {
			return false // At least one neighbor has data
		}
	}

	// No one has meaningful data about this nara - it's a ghost
	return true
}

// isGhostNaraSafeToDelete returns true if a ghost nara is safe to completely remove
// from memory. This is stricter than isGhostNara - we want to be 100% sure this isn't
// a data corruption issue before deleting.
//
// Safety criteria:
// 1. No neighbor has ANY meaningful data (StartTime, Restarts, LastRestart all zero)
// 2. Our own observation has no meaningful data
// 3. Online status is empty (never been properly tracked)
// 4. LastSeen is either 0 OR old enough that we're confident it's abandoned (> 24 hours)
// 5. At least 3 neighbors checked (avoid race conditions during boot)
func (network *Network) isGhostNaraSafeToDelete(name string) bool {
	// Never delete ourselves
	if name == network.meName() {
		return false
	}

	myObs := network.local.getObservation(name)

	// If we have any meaningful tracking data, don't delete
	if myObs.StartTime > 0 || myObs.Restarts > 0 || myObs.LastRestart > 0 {
		return false
	}

	// If it's currently marked as online, definitely don't delete
	if myObs.Online == "ONLINE" {
		return false
	}

	// If LastSeen is recent (within last 24 hours), don't delete - network needs to be resilient
	if myObs.LastSeen > 0 {
		oneDayAgo := time.Now().Unix() - 86400 // 24 hours
		if myObs.LastSeen > oneDayAgo {
			return false
		}
	}

	// Check if any neighbor has meaningful data
	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	neighborsChecked := 0
	for naraName, nara := range network.Neighbourhood {
		// Don't count the target nara itself as a witness
		if naraName == name {
			continue
		}
		obs := nara.getObservation(name)
		if obs.StartTime > 0 || obs.Restarts > 0 || obs.LastRestart > 0 {
			return false // At least one neighbor has data - not safe
		}
		// Also check if any neighbor thinks it's online
		if obs.Online == "ONLINE" {
			return false
		}
		neighborsChecked++
	}

	// Require at least 3 neighbors checked to avoid deleting during early boot
	if neighborsChecked < 3 {
		return false
	}

	return true
}

// garbageCollectGhostNaras removes ghost naras that are safe to delete from
// both the Neighbourhood and our Observations. Returns count of deleted naras.
func (network *Network) garbageCollectGhostNaras() int {
	names := network.NeighbourhoodNames()
	toDelete := []string{}

	for _, name := range names {
		if network.isGhostNaraSafeToDelete(name) {
			toDelete = append(toDelete, name)
		}
	}

	if len(toDelete) == 0 {
		return 0
	}

	// Actually delete them
	network.local.mu.Lock()
	for _, name := range toDelete {
		delete(network.Neighbourhood, name)
	}
	network.local.mu.Unlock()

	// Also remove from our observations
	network.local.Me.mu.Lock()
	for _, name := range toDelete {
		delete(network.local.Me.Status.Observations, name)
	}
	network.local.Me.mu.Unlock()

	for _, name := range toDelete {
		logrus.Printf("🗑️  garbage collected ghost nara: %s", name)
	}

	return len(toDelete)
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

	var observations []consensusValue
	var sources []string // track who provided what
	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	for naraName, nara := range network.Neighbourhood {
		obs := nara.getObservation(name)
		if obs.StartTime > 0 {
			uptime := nara.Status.HostStats.Uptime
			if uptime == 0 {
				uptime = 1 // minimum weight
			}
			observations = append(observations, consensusValue{
				value:  obs.StartTime,
				weight: uptime,
			})
			sources = append(sources, naraName)
		}
	}

	result := consensusByUptime(observations, tolerance, true)
	if result == 0 && len(observations) > 0 {
		logrus.Infof("  startTime data for %s: %d sources", name, len(observations))
		for i, obs := range observations {
			logrus.Infof("    %s: startTime=%d weight=%d", sources[i], obs.value, obs.weight)
		}
	}
	return result
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
			if network.local.SyncLedger.AddEventWithDedup(event) && network.local.Projections != nil {
				network.local.Projections.Trigger()
			}
			logrus.Infof("📊 First-seen observation event: %s", name)
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

		if observation.StartTime == 0 || observation.Restarts == 0 || observation.LastRestart == 0 {
			network.applyEventConsensusIfMissing(name, &observation)
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
			if network.local.SyncLedger.AddEventWithDedup(event) && network.local.Projections != nil {
				network.local.Projections.Trigger()
			}
			logrus.Infof("📊 Restart observation event: %s (restart #%d)", name, observation.Restarts)
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
				if network.local.SyncLedger.AddEventFiltered(event, network.local.Me.Status.Personality) && network.local.Projections != nil {
					network.local.Projections.Trigger()
				}
				logrus.Infof("📊 Status-change observation event: %s → ONLINE", name)
			} else {
				// Legacy: State changed from MISSING/OFFLINE to ONLINE
				event := NewObservationEvent(network.meName(), name, ReasonOnline)
				if network.local.SyncLedger.AddSocialEventFilteredLegacy(event, network.local.Me.Status.Personality) && network.local.Projections != nil {
					network.local.Projections.Trigger()
				}
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
	var values []int64
	var sources []string

	for naraName, nara := range network.Neighbourhood {
		restarts := nara.getObservation(name).Restarts
		values = append(values, restarts)
		sources = append(sources, naraName)
		if restarts > maxSeen {
			maxSeen = restarts
		}
	}

	if maxSeen == 0 && len(values) > 0 {
		logrus.Infof("  restartCount data for %s: %d sources", name, len(values))
		for i, val := range values {
			logrus.Infof("    %s: restarts=%d", sources[i], val)
		}
	}
	return maxSeen
}

func (network *Network) findLastRestartFromNeighbourhoodForNara(name string) int64 {
	values := make(map[int64]int)
	sources := make(map[int64][]string)
	network.local.mu.Lock()
	defer network.local.mu.Unlock()
	for naraName, nara := range network.Neighbourhood {
		last_restart := nara.getObservation(name).LastRestart
		if last_restart > 0 {
			values[last_restart] += 1
			sources[last_restart] = append(sources[last_restart], naraName)
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

	if result == 0 && len(values) > 0 {
		logrus.Infof("  lastRestart data for %s: threshold=%d, total_votes=%d", name, threshold, total_votes)
		for ts, count := range values {
			logrus.Infof("    timestamp=%d votes=%d from=%v", ts, count, sources[ts])
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

		// Ensure projection is up-to-date before checking statuses
		// This synchronously processes any pending events, avoiding race
		// conditions where we read stale data during/after a zine merge.
		if useObservationEvents() && network.local.Projections != nil && !network.local.isBooting() {
			network.local.Projections.OnlineStatus().RunOnce()
		}

		for name, observation := range observations {
			// Event-sourced status derivation when enabled
			// This uses the event log to determine online status rather than LastSeen
			if useObservationEvents() && network.local.Projections != nil && !network.local.isBooting() {
				derivedStatus := network.local.Projections.OnlineStatus().GetStatus(name)
				if derivedStatus != "" && derivedStatus != observation.Online {
					previousState := observation.Online
					observation.Online = derivedStatus
					network.local.setObservation(name, observation)

					// Log status changes
					if previousState == "ONLINE" && derivedStatus == "MISSING" {
						logrus.Printf("observation: %s has disappeared (event-derived)", name)
						network.Buzz.increase(10)
						if name != network.meName() {
							go network.reportMissingWithDelay(name)
						}
					} else if previousState == "ONLINE" && derivedStatus == "OFFLINE" {
						logrus.Printf("observation: %s went offline gracefully (event-derived)", name)
					}
				}

				// Reset cluster for non-online naras
				if !observation.isOnline() && observation.ClusterName != "" {
					observation.ClusterName = ""
					observation.ClusterEmoji = ""
					network.local.setObservation(name, observation)
				}
				continue
			}

			// Legacy LastSeen-based maintenance (when event sourcing is disabled)
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

			// mark missing after threshold seconds of no updates
			// Use longer threshold when:
			// 1. The observed nara is in gossip mode (they update less frequently)
			// 2. We (the observer) are in gossip mode (we receive updates less frequently)
			threshold := MissingThresholdSeconds
			nara := network.getNara(name)
			subjectIsGossip := nara.Name != "" && nara.Status.TransportMode == "gossip"
			observerIsGossip := network.TransportMode == TransportGossip
			if subjectIsGossip || observerIsGossip {
				threshold = MissingThresholdGossipSeconds
			}

			if (now-observation.LastSeen) > threshold && !network.skippingEvents && !network.local.isBooting() {
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

func (network *Network) applyEventConsensusIfMissing(name string, observation *NaraObservation) {
	if network.local.Projections == nil {
		return
	}

	opinion := network.local.Projections.Opinion().DeriveOpinion(name)
	if observation.StartTime == 0 && opinion.StartTime > 0 {
		observation.StartTime = opinion.StartTime
	}
	if observation.Restarts == 0 && opinion.Restarts > 0 {
		observation.Restarts = opinion.Restarts
	}
	if observation.LastRestart == 0 && name != network.meName() && opinion.LastRestart > 0 {
		observation.LastRestart = opinion.LastRestart
	}
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
		if network.local.SyncLedger.AddEventFiltered(event, network.local.Me.Status.Personality) && network.local.Projections != nil {
			network.local.Projections.Trigger()
		}
		logrus.Infof("📊 Status-change observation event: %s → MISSING (after %v delay)", subject, delay)
	} else {
		// Legacy mode
		event := NewObservationEvent(network.meName(), subject, ReasonOffline)
		if network.local.SyncLedger.AddSocialEventFilteredLegacy(event, network.local.Me.Status.Personality) && network.local.Projections != nil {
			network.local.Projections.Trigger()
		}
		logrus.Printf("observation: %s went offline (disappeared) after %v delay", subject, delay)
	}
}
