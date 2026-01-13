package nara

import (
	"time"

	"github.com/sirupsen/logrus"
)

// InitWorldJourney sets up the world journey handler with the given mesh transport
func (network *Network) InitWorldJourney(mesh MeshTransport) {
	network.worldHandler = NewWorldJourneyHandler(
		network.local,
		mesh,
		network.getMyClout,
		network.getOnlineNaraNames,
		network.resolvePublicKeyForNara,
		network.getMeshIPForNara,
		network.onWorldJourneyComplete,
		network.onWorldJourneyPassThrough,
	)
	network.worldHandler.Listen()
	logrus.Printf("World journey handler initialized")
}

func (network *Network) onWorldJourneyComplete(wm *WorldMessage) {
	network.worldJourneysMu.Lock()
	network.worldJourneys = append(network.worldJourneys, wm)
	// Keep only last 100 journeys
	if len(network.worldJourneys) > 100 {
		network.worldJourneys = network.worldJourneys[len(network.worldJourneys)-100:]
	}
	network.worldJourneysMu.Unlock()

	// Record journey-complete observation event
	if network.local.SyncLedger != nil {
		event := NewJourneyObservationSyncEvent(network.meName(), wm.Originator, ReasonJourneyComplete, wm.ID, network.local.Keypair)
		network.local.SyncLedger.AddSocialEventFiltered(event, network.local.Me.Status.Personality)
	}

	// Remove from pending journeys (we were the originator)
	network.pendingJourneysMu.Lock()
	delete(network.pendingJourneys, wm.ID)
	network.pendingJourneysMu.Unlock()

	// Broadcast completion via MQTT so others can resolve their pending journeys
	if !network.ReadOnly {
		completion := JourneyCompletion{
			JourneyID:  wm.ID,
			Originator: wm.Originator,
			ReportedBy: network.meName(),
			Message:    wm.OriginalMessage,
			Hops:       wm.Hops,
		}
		network.postEvent("nara/plaza/journey_complete", completion)
	}

	// Log journey completion with attestation chain
	logrus.Infof("🌍 Journey complete! %s: \"%s\" (%d hops)", wm.Originator, wm.OriginalMessage, len(wm.Hops))
	for i, hop := range wm.Hops {
		sig := hop.Signature
		if len(sig) > 12 {
			sig = sig[:12] + "..."
		}
		t := time.Unix(hop.Timestamp, 0).Format("15:04:05")
		logrus.Infof("🌍   %d. %s%s @ %s (sig: %s)", i+1, hop.Nara, hop.Stamp, t, sig)
	}
	network.Buzz.increase(10)
}

// onWorldJourneyPassThrough is called when a journey passes through us (before forwarding)
func (network *Network) onWorldJourneyPassThrough(wm *WorldMessage) {
	// Track as pending journey (for timeout detection)
	network.pendingJourneysMu.Lock()
	network.pendingJourneys[wm.ID] = &PendingJourney{
		JourneyID:  wm.ID,
		Originator: wm.Originator,
		SeenAt:     time.Now().Unix(),
		Message:    wm.OriginalMessage,
	}
	network.pendingJourneysMu.Unlock()

	// Record journey-pass observation event
	if network.local.SyncLedger != nil {
		event := NewJourneyObservationSyncEvent(network.meName(), wm.Originator, ReasonJourneyPass, wm.ID, network.local.Keypair)
		network.local.SyncLedger.AddSocialEventFiltered(event, network.local.Me.Status.Personality)
	}

	logrus.Printf("observation: journey %s passed through (from %s)", wm.ID[:8], wm.Originator)
	network.Buzz.increase(2)
}

// --- Journey Completion Handling ---

func (network *Network) processJourneyCompleteEvents() {
	for {
		select {
		case completion := <-network.journeyCompleteInbox:
			network.handleJourneyCompletion(completion)
		case <-network.ctx.Done():
			logrus.Debug("processJourneyCompleteEvents: shutting down")
			return
		}
	}
}

func (network *Network) handleJourneyCompletion(completion JourneyCompletion) {
	// Check if we have this journey pending
	network.pendingJourneysMu.Lock()
	pending, exists := network.pendingJourneys[completion.JourneyID]
	if exists {
		delete(network.pendingJourneys, completion.JourneyID)
	}
	network.pendingJourneysMu.Unlock()

	if !exists {
		// We didn't participate in this journey, nothing to do
		return
	}

	// Record journey-complete observation event (we heard it completed)
	if network.local.SyncLedger != nil {
		event := NewJourneyObservationSyncEvent(network.meName(), pending.Originator, ReasonJourneyComplete, completion.JourneyID, network.local.Keypair)
		network.local.SyncLedger.AddSocialEventFiltered(event, network.local.Me.Status.Personality)
	}

	// Log with attestation chain if available
	if len(completion.Hops) > 0 {
		logrus.Infof("🌍 Heard journey complete! %s: \"%s\" (%d hops, reported by %s)", pending.Originator, completion.Message, len(completion.Hops), completion.ReportedBy)
		for i, hop := range completion.Hops {
			sig := hop.Signature
			if len(sig) > 12 {
				sig = sig[:12] + "..."
			}
			t := time.Unix(hop.Timestamp, 0).Format("15:04:05")
			logrus.Infof("🌍   %d. %s%s @ %s (sig: %s)", i+1, hop.Nara, hop.Stamp, t, sig)
		}
	} else {
		logrus.Printf("observation: journey %s completed! (from %s, reported by %s)", completion.JourneyID[:8], pending.Originator, completion.ReportedBy)
	}
}

// journeyTimeoutMaintenance checks for journeys that have timed out
func (network *Network) journeyTimeoutMaintenance() {
	const journeyTimeout = 5 * time.Minute

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// continue
		case <-network.ctx.Done():
			return
		}

		now := time.Now().Unix()
		var timedOut []*PendingJourney

		network.pendingJourneysMu.Lock()
		for id, pending := range network.pendingJourneys {
			if now-pending.SeenAt > int64(journeyTimeout.Seconds()) {
				timedOut = append(timedOut, pending)
				delete(network.pendingJourneys, id)
			}
		}
		network.pendingJourneysMu.Unlock()

		// Record timeout events for each timed out journey
		for _, pending := range timedOut {
			if network.local.SyncLedger != nil {
				event := NewJourneyObservationSyncEvent(network.meName(), pending.Originator, ReasonJourneyTimeout, pending.JourneyID, network.local.Keypair)
				network.local.SyncLedger.AddSocialEventFiltered(event, network.local.Me.Status.Personality)
			}

			logrus.Printf("observation: journey %s timed out (from %s)", pending.JourneyID[:8], pending.Originator)
		}
	}
}
