package nara

import (
	"fmt"
	"time"

	"github.com/eljojo/nara/identity"
	"github.com/eljojo/nara/types"
	"github.com/sirupsen/logrus"
)

type ChauEvent struct {
	From      types.NaraName
	PublicKey string       // Base64-encoded Ed25519 public key
	ID        types.NaraID // Nara ID: deterministic hash of soul+name
	Signature string       // Base64-encoded signature of "chau:{From}:{PublicKey}:{ID}"
}

// Sign signs the ChauEvent with the given keypair
func (c *ChauEvent) Sign(kp identity.NaraKeypair) {
	message := c.signatureMessage()
	c.Signature = kp.SignBase64([]byte(message))
}

// Verify verifies the ChauEvent signature against the embedded public key
func (c *ChauEvent) Verify() bool {
	if c.PublicKey == "" || c.Signature == "" {
		return false
	}
	pubKey, err := identity.ParsePublicKey(c.PublicKey)
	if err != nil {
		return false
	}

	// Try new format first (with ID)
	message := c.signatureMessage()
	if identity.VerifySignatureBase64(pubKey, []byte(message), c.Signature) {
		return true
	}

	// Fall back to old format (without ID) for backwards compatibility
	if c.ID != "" {
		oldMessage := fmt.Sprintf("chau:%s:%s", c.From, c.PublicKey)
		return identity.VerifySignatureBase64(pubKey, []byte(oldMessage), c.Signature)
	}

	return false
}

// signatureMessage returns the message to sign based on whether ID is present
func (c *ChauEvent) signatureMessage() string {
	if c.ID != "" && false { // TODO(signature) temporarily turned off
		return fmt.Sprintf("chau:%s:%s:%s", c.From, c.PublicKey, c.ID)
	}
	// Legacy format for naras without ID
	return fmt.Sprintf("chau:%s:%s", c.From, c.PublicKey)
}

// ContentString implements Payload interface for ChauEvent
func (c *ChauEvent) ContentString() string {
	return c.signatureMessage()
}

// IsValid implements Payload interface for ChauEvent
// Note: Inner signature verification is not required here - the SyncEvent signature
// is the attestation layer. This just validates the payload has required fields.
func (c *ChauEvent) IsValid() bool {
	return c.From != ""
}

// GetActor implements Payload interface for ChauEvent
func (c *ChauEvent) GetActor() types.NaraName { return c.From }

// GetTarget implements Payload interface for ChauEvent
func (c *ChauEvent) GetTarget() types.NaraName { return c.From }

// VerifySignature implements Payload using the embedded public key
func (c *ChauEvent) VerifySignature(event *SyncEvent, lookup PublicKeyLookup) bool {
	if c.PublicKey == "" {
		return false
	}
	pubKey, err := identity.ParsePublicKey(c.PublicKey)
	if err != nil {
		return false
	}
	return event.VerifyWithKey(pubKey)
}

// UIFormat returns UI-friendly representation
func (c *ChauEvent) UIFormat() map[string]string {
	return map[string]string{
		"icon":   "👋",
		"text":   fmt.Sprintf("%s left the network", c.From),
		"detail": "chau!",
	}
}

// LogFormat returns technical log description
func (c *ChauEvent) LogFormat() string {
	return fmt.Sprintf("chau from %s", c.From)
}

// ToLogEvent returns a structured log event for the logging system
func (c *ChauEvent) ToLogEvent() *LogEvent {
	return &LogEvent{
		Category: CategoryPresence,
		Type:     "goodbye",
		Actor:    c.From.String(),
		Detail:   c.LogFormat(),
		GroupFormat: func(actors string) string {
			return fmt.Sprintf("💨 %s bounced", actors)
		},
	}
}

func (network *Network) processChauSyncEvents(events []SyncEvent) {
	// Skip during boot to avoid race condition where backfilled chau events
	// can clobber ONLINE status set by live hey_there events.
	// The projection will handle chau events correctly, and observationMaintenanceOnce
	// will sync from projections after boot completes.
	if network.local.isBooting() {
		return
	}

	for i := range events {
		e := &events[i]
		if e.Service != ServiceChau || e.Chau == nil {
			continue
		}

		c := e.Chau
		if c.From == network.meName() {
			continue // Ignore our own chau events
		}

		// Verify the SyncEvent signature (the attestation layer)
		if !network.VerifySyncEvent(e) {
			logrus.Warnf("📡 Invalid chau SyncEvent signature from %s", c.From)
			continue
		}

		// Check if there's a more recent hey_there from this nara
		// This prevents stale chau events from incorrectly marking naras offline during backfill
		if network.hasMoreRecentHeyThere(c.From, e.Timestamp) {
			continue
		}

		// Mark the nara as OFFLINE (graceful shutdown)
		observation := network.local.getObservation(c.From)
		if observation.Online == "ONLINE" {
			observation.Online = "OFFLINE"
			observation.LastSeen = time.Now().Unix()
			network.local.setObservation(c.From, observation)
			// LogService handles logging via ledger watching
			network.Buzz.increase(2)
		}
	}
}

// emitChauSyncEvent creates and adds a chau sync event to our ledger.
// This allows graceful shutdown to propagate through gossip.
func (network *Network) emitChauSyncEvent() {
	publicKey := identity.FormatPublicKey(network.local.Keypair.PublicKey)
	event := NewChauSyncEvent(network.meName(), publicKey, network.local.ID, network.local.Keypair)
	network.local.SyncLedger.MergeEvents([]SyncEvent{event})
	if network.local.Projections != nil {
		network.local.Projections.Trigger()
	}
	// LogService handles logging via ledger watching
}

func (network *Network) processChauEvents() {
	for {
		select {
		case event := <-network.chauInbox:
			network.handleChauEvent(event)
		case <-network.ctx.Done():
			logrus.Debug("processChauEvents: shutting down")
			return
		}
	}
}

func (network *Network) handleChauEvent(syncEvent SyncEvent) {
	if syncEvent.Service != ServiceChau || syncEvent.Chau == nil {
		return
	}

	chau := syncEvent.Chau
	if chau.From == network.meName() || chau.From == "" {
		return
	}

	// Verify SyncEvent signature using the public key from the payload (if available)
	// or from our neighbourhood (if we already know them)
	if syncEvent.IsSigned() {
		var pubKey []byte
		if chau.PublicKey != "" {
			var err error
			pubKey, err = identity.ParsePublicKey(chau.PublicKey)
			if err != nil {
				logrus.Warnf("⚠️  Invalid public key in chau from %s: %v", chau.From, err)
				return
			}
		} else {
			pubKey = network.resolvePublicKeyForNara(chau.From)
		}
		if pubKey != nil && !syncEvent.VerifyWithKey(pubKey) {
			logrus.Warnf("⚠️  chau from %s has invalid signature, ignoring", chau.From)
			return
		}
	}

	network.local.mu.Lock()
	existingNara, present := network.Neighbourhood[chau.From]
	network.local.mu.Unlock()

	// Check for public key changes
	if present && existingNara.Status.PublicKey != "" && chau.PublicKey != "" {
		if existingNara.Status.PublicKey != chau.PublicKey {
			logrus.Warnf("⚠️  PUBLIC KEY CHANGED for %s! old=%s new=%s",
				chau.From, truncateKey(existingNara.Status.PublicKey), truncateKey(chau.PublicKey))
		}
	}

	// Update the nara's public key and ID if provided
	if present && chau.PublicKey != "" {
		existingNara.mu.Lock()
		existingNara.Status.PublicKey = chau.PublicKey
		existingNara.mu.Unlock()
	}
	if present && chau.ID != "" {
		existingNara.mu.Lock()
		existingNara.Status.ID = chau.ID
		existingNara.ID = chau.ID
		existingNara.mu.Unlock()
	}
	// Register key in keyring (use chau.ID since it may have just been set)
	if present && chau.PublicKey != "" && chau.ID != "" {
		network.RegisterKey(chau.ID, chau.PublicKey)
	}

	// Add to ledger for gossip propagation
	if network.local.SyncLedger != nil {
		network.local.SyncLedger.AddEvent(syncEvent)
		if network.local.Projections != nil {
			network.local.Projections.Trigger()
		}
		network.broadcastSSE(syncEvent)
	}

	observation := network.local.getObservation(chau.From)
	previousState := observation.Online
	observation.Online = "OFFLINE"
	observation.LastSeen = time.Now().Unix()
	network.local.setObservation(chau.From, observation)

	// Record offline observation event if state changed
	if previousState == "ONLINE" && !network.local.isBooting() && network.local.SyncLedger != nil {
		obsEvent := NewObservationSocialSyncEvent(network.meName(), chau.From, ReasonOffline, network.local.Keypair)
		network.local.SyncLedger.AddSocialEventFiltered(obsEvent, network.local.Me.Status.Personality)
		// LogService handles logging via ledger watching
	}

	network.Buzz.increase(2)
}

func (network *Network) Chau() {
	if network.ReadOnly {
		return
	}

	// Update our own observation to OFFLINE
	observation := network.local.getMeObservation()
	observation.Online = "OFFLINE"
	observation.LastSeen = time.Now().Unix()
	network.local.setMeObservation(observation)

	// Create signed SyncEvent - same format for MQTT and gossip
	event := NewChauSyncEvent(
		network.meName(),
		network.local.Me.Status.PublicKey,
		network.local.ID,
		network.local.Keypair,
	)

	// Publish to MQTT
	topic := "nara/plaza/chau"
	network.postEvent(topic, event)
	// LogService handles logging via ledger watching

	// Also add to ledger for gossip propagation
	if network.local.SyncLedger != nil {
		network.local.SyncLedger.AddEvent(event)
		if network.local.Projections != nil {
			network.local.Projections.Trigger()
		}
	}
}
