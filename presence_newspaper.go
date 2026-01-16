package nara

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/eljojo/nara/identity"
	"github.com/eljojo/nara/types"
)

// NewspaperEvent represents a signed announcement of status
type NewspaperEvent struct {
	From       types.NaraName
	Status     NaraStatus
	Signature  string // Base64-encoded signature of the status JSON
	StatusJSON []byte `json:"-"` // Raw status JSON for signature verification
}

// SignNewspaper creates a signed newspaper event
func (network *Network) SignNewspaper(status NaraStatus) NewspaperEvent {
	event := NewspaperEvent{
		From:   network.meName(),
		Status: status,
	}
	// Sign the JSON-serialized status
	statusJSON, _ := json.Marshal(status)
	event.Signature = network.local.Keypair.SignBase64(statusJSON)
	return event
}

// Verify verifies a newspaper event signature
func (event *NewspaperEvent) Verify(publicKey []byte) bool {
	if event.Signature == "" {
		logrus.Warnf("Newspaper from %s is missing signature", event.From)
		return false
	}
	statusJSON := event.StatusJSON
	if len(statusJSON) == 0 {
		var err error
		statusJSON, err = json.Marshal(event.Status)
		if err != nil {
			logrus.Warnf("Failed to marshal newspaper status from %s: %v", event.From, err)
			return false
		}
	}
	return identity.VerifySignatureBase64(publicKey, statusJSON, event.Signature)
}

// announce broadcasts this nara's current status as a newspaper event
func (network *Network) announce() {
	if network.ReadOnly {
		return
	}
	network.testAnnounceCount++ // Track for testing
	topic := fmt.Sprintf("%s/%s", "nara/newspaper", network.meName())
	network.recordObservationOnlineNara(network.meName(), 0)

	// Broadcast slim newspapers without Observations (observations are event-sourced)
	network.local.Me.mu.Lock()
	slimStatus := network.local.Me.Status
	network.local.Me.mu.Unlock()
	slimStatus.Observations = nil
	if network.local.SyncLedger != nil {
		slimStatus.EventStoreByService = network.local.SyncLedger.GetEventCountsByService()
		slimStatus.EventStoreTotal = network.local.SyncLedger.EventCount()
		slimStatus.EventStoreCritical = network.local.SyncLedger.GetCriticalEventCount()
	} else {
		slimStatus.EventStoreByService = nil
		slimStatus.EventStoreTotal = 0
		slimStatus.EventStoreCritical = 0
	}

	signedEvent := network.SignNewspaper(slimStatus)
	network.postEvent(topic, signedEvent)
}

// announceForever periodically announces this nara's status
func (network *Network) announceForever() {
	for {
		// Newspapers are lightweight heartbeats (30-300s), observations are event-sourced
		ts := network.local.chattinessRate(30, 300)

		// Wait with graceful shutdown support
		select {
		case <-time.After(time.Duration(ts) * time.Second):
			network.announce()
		case <-network.ctx.Done():
			logrus.Debugf("announceForever: shutting down gracefully")
			return
		}
	}
}

// processNewspaperEvents handles incoming newspaper events from the inbox
func (network *Network) processNewspaperEvents() {
	for {
		select {
		case event := <-network.newspaperInbox:
			network.handleNewspaperEvent(event)
		case <-network.ctx.Done():
			logrus.Debug("processNewspaperEvents: shutting down")
			return
		}
	}
}

// handleNewspaperEvent processes a single newspaper event
func (network *Network) handleNewspaperEvent(event NewspaperEvent) {
	// Verify signature if present
	if event.Signature != "" {
		// Get public key - try from event status first, then from known neighbor
		var pubKey []byte
		if event.Status.PublicKey != "" {
			var err error
			pubKey, err = identity.ParsePublicKey(event.Status.PublicKey)
			if err != nil {
				logrus.Warnf("🚨 Invalid public key in newspaper from %s", event.From)
				return
			}
		} else {
			// Try to get from known neighbor
			pubKey = network.resolvePublicKeyForNara(event.From)
		}

		if pubKey != nil && !event.Verify(pubKey) {
			logrus.Warnf("🚨 Invalid signature on newspaper from %s, allowing for now...", event.From) // TODO(signatures)
			//return
		}
	}

	network.local.mu.Lock()
	nara, present := network.Neighbourhood[event.From]
	network.local.mu.Unlock()
	if present {
		nara.mu.Lock()
		// Warn if public key changed
		if event.Status.PublicKey != "" && nara.Status.PublicKey != "" && nara.Status.PublicKey != event.Status.PublicKey {
			logrus.Warnf("⚠️  Public key changed for %s! Old: %s..., New: %s...",
				event.From,
				truncateKey(nara.Status.PublicKey),
				truncateKey(event.Status.PublicKey))
		}
		// Log key field differences before updating
		var changes []string
		if nara.Status.Flair != event.Status.Flair && event.Status.Flair != "" {
			changes = append(changes, fmt.Sprintf("Flair:%s→%s", nara.Status.Flair, event.Status.Flair))
		}
		if nara.Status.Trend != event.Status.Trend && event.Status.Trend != "" {
			changes = append(changes, fmt.Sprintf("Trend:%s→%s", nara.Status.Trend, event.Status.Trend))
		}
		if nara.Status.Chattiness != event.Status.Chattiness {
			changes = append(changes, fmt.Sprintf("Chattiness:%d→%d", nara.Status.Chattiness, event.Status.Chattiness))
		}
		if nara.Version != "" && event.Status.Version != "" && nara.Version != event.Status.Version {
			changes = append(changes, fmt.Sprintf("Version:%s→%s", nara.Version, event.Status.Version))
		}
		if len(changes) > 0 && network.logService != nil {
			network.logService.BatchNewspaper(string(event.From), strings.Join(changes, ", "))
		}
		nara.Status.setValuesFrom(event.Status)
		nara.mu.Unlock()
	} else {
		logrus.Printf("%s posted a newspaper story (whodis?)", event.From)
		nara = NewNara(event.From)
		nara.Status.setValuesFrom(event.Status)
		if network.local.Me.Status.Chattiness > 0 && !network.ReadOnly {
			network.heyThere()
		}
		network.importNara(nara)
	}

	// The newspaper itself is an event emitted by them - they prove themselves
	network.recordObservationOnlineNara(event.From, 0)
}
