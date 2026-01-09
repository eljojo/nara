package nara

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// verifyPingRateLimit tracks last verification ping attempts to prevent spam.
// Maps nara name → last ping attempt time (Unix timestamp).
var (
	verifyPingLastAttempt   = make(map[string]int64)
	verifyPingLastAttemptMu sync.Mutex
)

// resetVerifyPingRateLimit clears the rate limit map (for testing only).
func resetVerifyPingRateLimit() {
	verifyPingLastAttemptMu.Lock()
	verifyPingLastAttempt = make(map[string]int64)
	verifyPingLastAttemptMu.Unlock()
}

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

		// Use event-based consensus for observation data
		observation := network.local.getObservation(name)
		if network.local.Projections != nil {
			network.local.Projections.Opinion().RunOnce()
			opinion := network.local.Projections.Opinion().DeriveOpinion(name)
			if opinion.StartTime > 0 {
				observation.StartTime = opinion.StartTime
			}
			if opinion.Restarts > 0 {
				observation.Restarts = opinion.Restarts
			}
			if name != network.meName() && opinion.LastRestart > 0 {
				observation.LastRestart = opinion.LastRestart
			}
		}
		network.local.setObservation(name, observation)
	}
	if ghostCount > 0 {
		logrus.Printf("👻 skipped %d ghost naras (no data from any neighbor)", ghostCount)
	}

	// Apply consensus for our own start time (using howdy votes collected during boot)
	// This is deferred until now to have more data than the early 3-vote trigger
	network.startTimeVotesMu.Lock()
	if !network.startTimeApplied {
		network.applyStartTimeConsensus()
	}
	network.startTimeVotesMu.Unlock()

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
// the Neighbourhood, our Observations, and the event store. Returns count of deleted naras.
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

	// Remove events involving these ghost naras from the event store
	if network.local.SyncLedger != nil {
		for _, name := range toDelete {
			eventsRemoved := network.local.SyncLedger.RemoveEventsFor(name)
			logrus.Printf("🗑️  garbage collected ghost nara: %s (removed %d events)", name, eventsRemoved)
		}
	} else {
		for _, name := range toDelete {
			logrus.Printf("🗑️  garbage collected ghost nara: %s", name)
		}
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

		// Emit first-seen observation event
		if !network.local.isBooting() && network.local.SyncLedger != nil {
			event := NewFirstSeenObservationEvent(network.meName(), name, time.Now().Unix())
			if network.local.SyncLedger.AddEventWithDedup(event) && network.local.Projections != nil {
				network.local.Projections.Trigger()
			}
			logrus.Infof("📊 First-seen observation event: %s", name)
		}
	}

	// Use event-based consensus for observation data
	if observation.StartTime == 0 || name == network.meName() {
		if network.local.Projections != nil {
			network.local.Projections.Opinion().RunOnce()
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

		// Emit restart observation event
		if !network.local.isBooting() && network.local.SyncLedger != nil && name != network.meName() {
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
			event := NewStatusChangeObservationEvent(network.meName(), name, "ONLINE")
			if network.local.SyncLedger.AddEventFiltered(event, network.local.Me.Status.Personality) && network.local.Projections != nil {
				network.local.Projections.Trigger()
			}
			logrus.Infof("📊 Status-change observation event: %s → ONLINE", name)
		}
	}

	// Check teasing triggers after observation update
	// Run inline - it's cheap (mostly returns early) and cooldown prevents spam
	if !network.local.isBooting() && name != network.meName() {
		network.checkAndTease(name, previousState, previousTrend)
	}
}

func (network *Network) observationMaintenance() {
	for {
		network.local.Me.mu.Lock()
		observations := make(map[string]NaraObservation)
		for name, obs := range network.local.Me.Status.Observations {
			observations[name] = obs
		}
		network.local.Me.mu.Unlock()

		// Ensure projection is up-to-date before checking statuses
		// This synchronously processes any pending events, avoiding race
		// conditions where we read stale data during/after a zine merge.
		if network.local.Projections != nil && !network.local.isBooting() {
			network.local.Projections.OnlineStatus().RunOnce()
		}

		for name, observation := range observations {
			// Skip self - we can't observe ourselves as disappeared
			if name == network.meName() {
				continue
			}

			// Event-sourced status derivation
			// This uses the event log to determine online status rather than LastSeen
			if network.local.Projections != nil && !network.local.isBooting() {
				derivedStatus := network.local.Projections.OnlineStatus().GetStatus(name)
				if derivedStatus != "" && derivedStatus != observation.Online {
					previousState := observation.Online

					// Before transitioning ONLINE → MISSING, verify with a ping
					// This guards against buggy naras spreading false "offline" observations
					if previousState == "ONLINE" && derivedStatus == "MISSING" {
						// Skip ping verification for old events (>1 hour) - likely historical data
						state := network.local.Projections.OnlineStatus().GetState(name)
						eventAge := time.Duration(0)
						if state != nil {
							eventAge = time.Since(time.Unix(0, state.LastEventTime))
						}

						if eventAge > 1*time.Hour {
							logrus.Debugf("🔍 Skipping ping verification for %s - event is old (%v)", name, eventAge)
						} else if network.verifyOnlineWithPing(name) {
							// Ping succeeded - they're still online, don't mark as MISSING
							logrus.Infof("🔍 Disagreement resolved: %s reported MISSING but ping succeeded - keeping ONLINE", name)
							continue
						}
					}

					observation.Online = derivedStatus
					network.local.setObservation(name, observation)

					// Log status changes
					if previousState == "ONLINE" && derivedStatus == "MISSING" {
						logrus.Printf("observation: %s has disappeared (verified)", name)
						network.Buzz.increase(10)
						go network.reportMissingWithDelay(name)
					} else if previousState == "ONLINE" && derivedStatus == "OFFLINE" {
						logrus.Printf("observation: %s went offline gracefully", name)
					}
				}

				// Reset cluster for non-online naras
				if !observation.isOnline() && observation.ClusterName != "" {
					observation.ClusterName = ""
					observation.ClusterEmoji = ""
					network.local.setObservation(name, observation)
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

	// Final verification: try pinging before broadcasting they're missing
	// This gives the nara one more chance to respond after the delay
	if network.verifyOnlineWithPing(subject) {
		logrus.Debugf("🔍 %s responded to verification ping - not reporting MISSING", subject)
		return
	}

	// No one else reported it and ping failed, so we'll report it
	event := NewStatusChangeObservationEvent(network.meName(), subject, "MISSING")
	if network.local.SyncLedger.AddEventFiltered(event, network.local.Me.Status.Personality) && network.local.Projections != nil {
		network.local.Projections.Trigger()
	}
	logrus.Infof("📊 Status-change observation event: %s → MISSING (after %v delay, verified)", subject, delay)
}

// verifyOnlineWithPing attempts to ping a nara before marking them as offline/missing.
// Returns true if the nara is verified online (and emits an event), false if unreachable.
// This guards against buggy naras spreading false "offline" observations.
func (network *Network) verifyOnlineWithPing(name string) bool {
	// Skip verification for ourselves
	if name == network.meName() {
		return false
	}

	// Rate limit: don't ping the same nara more than once per 60 seconds
	verifyPingLastAttemptMu.Lock()
	lastAttempt := verifyPingLastAttempt[name]
	now := time.Now().Unix()
	if now-lastAttempt < 60 {
		verifyPingLastAttemptMu.Unlock()
		logrus.Debugf("🔍 Skipping ping verification for %s - rate limited (last attempt %ds ago)", name, now-lastAttempt)
		return false
	}
	verifyPingLastAttempt[name] = now
	verifyPingLastAttemptMu.Unlock()

	// Test hook: allows tests to override ping behavior
	if network.testPingFunc != nil {
		success, _ := network.testPingFunc(name)
		if success {
			network.markOnlineFromPing(name, 1.0) // Use 1ms for test RTT
		}
		return success
	}

	// Need mesh to ping
	if network.tsnetMesh == nil {
		return false
	}

	// Get the mesh IP for this nara
	network.local.mu.Lock()
	nara, exists := network.Neighbourhood[name]
	network.local.mu.Unlock()

	if !exists {
		return false
	}

	nara.mu.Lock()
	meshIP := nara.Status.MeshIP
	nara.mu.Unlock()

	if meshIP == "" {
		logrus.Debugf("🔍 Cannot verify %s - no mesh IP", name)
		return false
	}

	// Try to ping with a short timeout (2 seconds)
	logrus.Debugf("🔍 Verifying %s is really offline via ping...", name)
	rtt, err := network.tsnetMesh.Ping(meshIP, network.meName(), 2*time.Second)
	if err != nil {
		logrus.Debugf("🔍 Verification ping to %s failed: %v - confirmed offline", name, err)
		return false
	}

	// Ping succeeded! Mark them as online
	rttMs := float64(rtt.Milliseconds())
	logrus.Infof("🔍 Verification ping to %s succeeded (%.2fms) - still online!", name, rttMs)
	network.markOnlineFromPing(name, rttMs)

	return true
}

// markOnlineFromPing updates observation and emits a ping event after a successful ping.
// This is the shared logic for both real pings and test pings.
func (network *Network) markOnlineFromPing(name string, rttMs float64) {
	// Update our observation
	obs := network.local.getObservation(name)
	obs.Online = "ONLINE"
	obs.LastSeen = time.Now().Unix()
	obs.LastPingRTT = rttMs
	obs.LastPingTime = time.Now().Unix()
	network.local.setObservation(name, obs)

	// Emit a ping event to prove they're still alive
	if network.local.SyncLedger != nil {
		network.local.SyncLedger.AddSignedPingObservationWithReplace(
			network.meName(), name, rttMs,
			network.meName(), network.local.Keypair,
		)
		if network.local.Projections != nil {
			network.local.Projections.Trigger()
		}
	}
}
