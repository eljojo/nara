package nara

import (
	"math/rand"
	"time"

	"github.com/sirupsen/logrus"
)

// --- Social Event Handling ---

func (network *Network) processSocialEvents() {
	for {
		select {
		case event := <-network.socialInbox:
			network.handleSocialEvent(event)
		case <-network.ctx.Done():
			logrus.Debug("processSocialEvents: shutting down")
			return
		}
	}
}

func (network *Network) handleSocialEvent(event SyncEvent) {
	if event.Service != ServiceSocial || event.Social == nil {
		return
	}

	social := event.Social

	// Don't process our own events from the network
	if social.Actor == network.meName() {
		return
	}

	// Verify SyncEvent signature if present
	if event.IsSigned() {
		pubKey := network.resolvePublicKeyForNara(event.Emitter)
		if pubKey != nil && !event.VerifyWithKey(pubKey) {
			logrus.Warnf("🚨 Invalid signature on social event from %s", event.Emitter)
			return
		}
	}

	// Add to our ledger
	if network.local.SyncLedger.AddSocialEventFiltered(event, network.local.Me.Status.Personality) {
		logrus.Printf("📢 %s teased %s: %s", social.Actor, social.Target, TeaseMessage(social.Reason, social.Actor, social.Target))
		network.Buzz.increase(5)

		// Broadcast to SSE clients
		network.broadcastSSE(event)
	}
}

// Tease sends a tease event directly to the target nara.
// The tease is added to our ledger and DM'd to the target.
// If the DM fails, the tease will spread via gossip.
// Returns true if the tease was sent, false if blocked by cooldown or readonly.
func (network *Network) Tease(target, reason string) bool {
	if network.ReadOnly {
		return false
	}

	actor := network.meName()

	// Atomically check and record cooldown (prevents TOCTOU race)
	if !network.TeaseState.TryTease(actor, target) {
		return false
	}

	// Create signed SyncEvent
	event := NewTeaseSyncEvent(actor, target, reason, network.local.Keypair)

	// Add to our own ledger
	if network.local.SyncLedger != nil {
		network.local.SyncLedger.AddSocialEventFiltered(event, network.local.Me.Status.Personality)
	}

	// DM the target directly (best-effort: if it fails, gossip will spread it)
	if network.SendDM(target, event) {
		logrus.Debugf("📬 DM'd tease to %s", target)
	} else {
		logrus.Debugf("📬 Could not DM %s, tease will spread via gossip", target)
	}

	// Broadcast to local SSE clients
	network.broadcastSSE(event)

	msg := TeaseMessage(reason, actor, target)
	logrus.Printf("😈 teasing %s: %s", target, msg)
	network.Buzz.increase(3)
	return true
}

// TeaseWithDelay implements "if no one says anything, I guess I'll say something" for teasing.
// Waits a random delay, then checks if another nara already teased the target for the same reason.
// If yes, stays silent. If no, proceeds with the tease.
// This prevents 10 naras all teasing someone at the exact same moment.
func (network *Network) TeaseWithDelay(target, reason string) {
	if network.ReadOnly {
		return
	}

	// Random delay 0-5 seconds to stagger teases (overridable for testing)
	if !network.waitWithJitter(5*time.Second, network.testTeaseDelay) {
		return
	}

	// Check if another nara already teased this target for this reason recently
	if network.hasRecentTeaseFor(target, reason) {
		logrus.Debugf("🤐 Not teasing %s (%s) - someone else already did", target, reason)
		return
	}

	// No one else teased, so we'll do it
	network.Tease(target, reason)
}

// hasRecentTeaseFor checks if there's a recent tease for the target+reason from any nara
func (network *Network) hasRecentTeaseFor(target, reason string) bool {
	if network.local.SyncLedger == nil {
		return false
	}

	// Look for teases in the last 30 seconds
	recentCutoff := time.Now().Add(-30 * time.Second).UnixNano()

	events := network.local.SyncLedger.GetSocialEventsAbout(target)
	for _, e := range events {
		if e.Timestamp > recentCutoff &&
			e.Social != nil &&
			e.Social.Type == "tease" &&
			e.Social.Reason == reason &&
			e.Social.Actor != network.meName() {
			return true
		}
	}

	return false
}

// checkAndTease evaluates teasing triggers for a given nara.
// Uses TeaseWithDelay for triggered teases to implement "if no one says anything, I'll say something"
// This prevents all naras from piling on with the same tease at the same moment.
func (network *Network) checkAndTease(name string, previousState string, previousTrend string) {
	if network.ReadOnly || name == network.meName() {
		return
	}

	obs := network.local.getObservation(name)
	personality := network.local.Me.Status.Personality

	// Check restart-based teasing (uses delay to avoid pile-on)
	if ShouldTeaseForRestarts(obs, personality) {
		go network.TeaseWithDelay(name, ReasonHighRestarts)
		return // one tease trigger at a time
	}

	// Check nice number teasing (uses delay to avoid pile-on)
	if ShouldTeaseForNiceNumber(obs.Restarts, personality) {
		go network.TeaseWithDelay(name, ReasonNiceNumber)
		return
	}

	// Check comeback teasing (uses delay to avoid pile-on)
	if ShouldTeaseForComeback(obs, previousState, personality) {
		go network.TeaseWithDelay(name, ReasonComeback)
		return
	}

	// Check trend abandon teasing (uses delay to avoid pile-on)
	if previousTrend != "" {
		trendPopularity := network.trendPopularity(previousTrend)
		nara := network.getNara(name)
		if ShouldTeaseForTrendAbandon(previousTrend, nara.Status.Trend, trendPopularity, personality) {
			go network.TeaseWithDelay(name, ReasonTrendAbandon)
			return
		}
	}

	// Random teasing (very low probability, boosted for nearby naras)
	// Random teases don't need delay since they're already probabilistic and rare
	proximityBoost := 1.0
	if network.IsInMyBarrio(name) {
		proximityBoost = 3.0 // 3x more likely to notice naras in same barrio
	}
	if ShouldRandomTeaseWithBoost(network.local.Soul, name, time.Now().Unix(), personality, proximityBoost) {
		network.Tease(name, ReasonRandom)
	}
}

// trendPopularity returns the fraction of online naras following a trend
func (network *Network) trendPopularity(trend string) float64 {
	if trend == "" {
		return 0
	}

	online := network.NeighbourhoodOnlineNames()
	if len(online) == 0 {
		return 0
	}

	following := 0
	for _, name := range online {
		nara := network.getNara(name)
		if nara.Status.Trend == trend {
			following++
		}
	}

	return float64(following) / float64(len(online))
}

func (network *Network) waitWithJitter(maxDelay time.Duration, override *time.Duration) bool {
	delay := time.Duration(0)
	if override != nil {
		delay = *override
	} else if maxDelay > 0 {
		delay = time.Duration(rand.Int63n(int64(maxDelay)))
	}

	if delay <= 0 {
		return true
	}

	select {
	case <-time.After(delay):
		return true
	case <-network.ctx.Done():
		return false
	}
}
