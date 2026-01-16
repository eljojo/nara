package nara

import (
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/eljojo/nara/types"
)

// truncateKey returns first 8 chars of a key for logging
func truncateKey(key string) string {
	if len(key) > 8 {
		return key[:8]
	}
	return key
}

type HeyThereEvent struct {
	From      types.NaraName
	PublicKey string       // Base64-encoded Ed25519 public key
	MeshIP    string       // Tailscale IP for mesh communication
	ID        types.NaraID // Nara ID: deterministic hash of soul+name
	Signature string       // Base64-encoded signature of "hey_there:{From}:{PublicKey}:{MeshIP}:{ID}"
}

// Sign signs the HeyThereEvent with the given keypair
func (h *HeyThereEvent) Sign(kp NaraKeypair) {
	message := h.signatureMessage()
	h.Signature = kp.SignBase64([]byte(message))
}

// Verify verifies the HeyThereEvent signature against the embedded public key
func (h *HeyThereEvent) Verify() bool {
	if h.PublicKey == "" || h.Signature == "" {
		return false
	}
	pubKey, err := ParsePublicKey(h.PublicKey)
	if err != nil {
		return false
	}

	// Try new format first (with ID)
	message := h.signatureMessage()
	if VerifySignatureBase64(pubKey, []byte(message), h.Signature) {
		return true
	}

	// Fall back to old format (without ID) for backwards compatibility
	if h.ID != "" {
		oldMessage := fmt.Sprintf("hey_there:%s:%s:%s", h.From.String(), h.PublicKey, h.MeshIP)
		return VerifySignatureBase64(pubKey, []byte(oldMessage), h.Signature)
	}

	return false
}

// signatureMessage returns the message to sign based on whether ID is present
func (h *HeyThereEvent) signatureMessage() string {
	if h.ID != "" && false { // TODO(signature) temporarily turned off
		return fmt.Sprintf("hey_there:%s:%s:%s:%s", h.From.String(), h.PublicKey, h.MeshIP, h.ID)
	}
	// Legacy format for naras without ID
	return fmt.Sprintf("hey_there:%s:%s:%s", h.From.String(), h.PublicKey, h.MeshIP)
}

// ContentString implements Payload interface for HeyThereEvent
func (h *HeyThereEvent) ContentString() string {
	return h.signatureMessage()
}

// IsValid implements Payload interface for HeyThereEvent
// Note: Inner signature verification is not required here - the SyncEvent signature
// is the attestation layer. This just validates the payload has required fields.
func (h *HeyThereEvent) IsValid() bool {
	return h.From != "" && h.PublicKey != ""
}

// GetActor implements Payload interface for HeyThereEvent
func (h *HeyThereEvent) GetActor() types.NaraName { return h.From }

// GetTarget implements Payload interface for HeyThereEvent
func (h *HeyThereEvent) GetTarget() types.NaraName { return h.From }

// VerifySignature implements Payload using the embedded public key
func (h *HeyThereEvent) VerifySignature(event *SyncEvent, lookup PublicKeyLookup) bool {
	if h.PublicKey == "" {
		return false
	}
	pubKey, err := ParsePublicKey(h.PublicKey)
	if err != nil {
		return false
	}
	return event.VerifyWithKey(pubKey)
}

// UIFormat returns UI-friendly representation
func (h *HeyThereEvent) UIFormat() map[string]string {
	detail := "hey there!"
	if h.MeshIP != "" {
		detail = fmt.Sprintf("mesh: %s", h.MeshIP)
	}
	return map[string]string{
		"icon":   "👋",
		"text":   fmt.Sprintf("%s joined the network", h.From.String()),
		"detail": detail,
	}
}

// LogFormat returns technical log description
func (h *HeyThereEvent) LogFormat() string {
	return fmt.Sprintf("hey-there from %s (mesh: %s)", h.From.String(), h.MeshIP)
}

// ToLogEvent returns a structured log event for the logging system
func (h *HeyThereEvent) ToLogEvent() *LogEvent {
	return &LogEvent{
		Category: CategoryPresence,
		Type:     "welcome",
		Actor:    h.From.String(),
		Target:   h.From.String(),
		Detail:   h.LogFormat(),
		GroupFormat: func(actors string) string {
			return fmt.Sprintf("👋 %s said hey-there", actors)
		},
	}
}

func (network *Network) processHeyThereSyncEvents(events []SyncEvent) {
	for i := range events {
		e := &events[i]
		if e.Service != ServiceHeyThere || e.HeyThere == nil {
			continue
		}

		h := e.HeyThere
		if h.From == network.meName() {
			continue // Ignore our own hey_there events
		}

		// For hey_there events, verify using the public key FROM the payload itself.
		// This is the bootstrap case - hey_there is how we learn public keys.
		// The payload contains the public key, and the SyncEvent is signed with it.
		if e.IsSigned() && h.PublicKey != "" {
			pubKey, err := ParsePublicKey(h.PublicKey)
			if err != nil {
				logrus.Warnf("📡 Invalid public key in hey_there from %s: %v", h.From, err)
				continue
			}
			if !e.VerifyWithKey(pubKey) {
				logrus.Warnf("📡 Invalid hey_there SyncEvent signature from %s", h.From)
				continue
			}
		}

		// Check if nara exists and update or create
		network.local.mu.Lock()
		nara, exists := network.Neighbourhood[h.From]
		network.local.mu.Unlock()

		if exists {
			// Update existing nara with proper locking
			nara.mu.Lock()
			updated := false
			naraID := nara.Status.ID
			if nara.Status.PublicKey == "" && h.PublicKey != "" {
				nara.Status.PublicKey = h.PublicKey
				updated = true
			}
			if nara.Status.MeshIP == "" && h.MeshIP != "" {
				nara.Status.MeshIP = h.MeshIP
				nara.Status.MeshEnabled = true
				updated = true
			}
			nara.mu.Unlock()
			// Register key in keyring (outside lock)
			if updated && naraID != "" && h.PublicKey != "" {
				network.RegisterKey(naraID, h.PublicKey)
			}
			if updated {
				logrus.Infof("📡 Updated identity for %s via hey_there event (🔑)", h.From)
			}
		} else {
			// Create new nara and import it
			newNara := NewNara(types.NaraName(h.From))
			newNara.Status.PublicKey = h.PublicKey
			newNara.Status.MeshIP = h.MeshIP
			newNara.Status.MeshEnabled = h.MeshIP != ""
			network.importNara(newNara)
			logrus.Infof("📡 Discovered new peer %s via hey_there event (🔑)", h.From)
		}
	}
}

// emitHeyThereSyncEvent creates and adds a hey_there sync event to our ledger.
// This allows our identity to propagate through gossip (new mechanism replacing MQTT hey_there).
func (network *Network) emitHeyThereSyncEvent() {
	publicKey := FormatPublicKey(network.local.Keypair.PublicKey)
	meshIP := network.local.Me.Status.MeshIP

	event := NewHeyThereSyncEvent(network.meName(), publicKey, meshIP, network.local.ID, network.local.Keypair)
	network.local.SyncLedger.MergeEvents([]SyncEvent{event})
	if network.local.Projections != nil {
		network.local.Projections.Trigger()
	}
	logrus.Infof("%s: 👋 (gossip)", network.meName())
}

func (network *Network) hasMoreRecentHeyThere(from types.NaraName, thanTimestamp int64) bool {
	if network.local.SyncLedger == nil {
		return false
	}

	heyThereEvents := network.local.SyncLedger.GetEventsByService(ServiceHeyThere)
	for _, e := range heyThereEvents {
		if e.HeyThere != nil && e.HeyThere.From == from && e.Timestamp > thanTimestamp {
			return true
		}
	}
	return false
}

func (network *Network) processHeyThereEvents() {
	for {
		select {
		case event := <-network.heyThereInbox:
			network.handleHeyThereEvent(event)
		case <-network.ctx.Done():
			logrus.Debug("processHeyThereEvents: shutting down")
			return
		}
	}
}

func (network *Network) handleHeyThereEvent(event SyncEvent) {
	if event.Service != ServiceHeyThere || event.HeyThere == nil {
		return
	}

	heyThere := event.HeyThere

	// Verify SyncEvent signature using the public key from the payload
	if event.IsSigned() && heyThere.PublicKey != "" {
		pubKey, err := ParsePublicKey(heyThere.PublicKey)
		if err != nil {
			logrus.Warnf("🚨 Invalid public key in hey_there from %s: %v", heyThere.From, err)
			return
		}
		if !event.VerifyWithKey(pubKey) {
			logrus.Warnf("🚨 Invalid signature on hey_there from %s", heyThere.From)
			return
		}
	}

	// The hey_there itself is an event they emitted - they prove themselves
	// (LogService handles logging via ledger watching)
	network.recordObservationOnlineNara(heyThere.From, event.Timestamp/1e9)

	// Add to ledger for gossip propagation
	if network.local.SyncLedger != nil {
		network.local.SyncLedger.AddEvent(event)
		if network.local.Projections != nil {
			network.local.Projections.Trigger()
		}
		network.broadcastSSE(event)
	}

	// Store PublicKey and MeshIP from the hey_there event
	if heyThere.PublicKey != "" || heyThere.MeshIP != "" {
		network.local.mu.Lock()
		nara, present := network.Neighbourhood[heyThere.From]
		network.local.mu.Unlock()

		if present {
			nara.mu.Lock()
			// Warn if public key changed
			if heyThere.PublicKey != "" && nara.Status.PublicKey != "" && nara.Status.PublicKey != heyThere.PublicKey {
				logrus.Warnf("⚠️  Public key changed for %s! Old: %s..., New: %s...",
					heyThere.From,
					truncateKey(nara.Status.PublicKey),
					truncateKey(heyThere.PublicKey))
			}
			if heyThere.PublicKey != "" {
				nara.Status.PublicKey = heyThere.PublicKey
			}
			if heyThere.MeshIP != "" {
				nara.Status.MeshIP = heyThere.MeshIP
				nara.Status.MeshEnabled = true
			}
			if heyThere.ID != "" {
				nara.Status.ID = heyThere.ID
				nara.ID = heyThere.ID
			}
			// Get final ID for keyring registration
			naraID := nara.Status.ID
			nara.mu.Unlock()
			// Register key in keyring (outside lock)
			if naraID != "" && heyThere.PublicKey != "" {
				network.RegisterKey(naraID, heyThere.PublicKey)
			}
			logrus.Infof("📝 Updated %s: PublicKey=%s..., MeshIP=%s, ID=%s",
				heyThere.From,
				truncateKey(heyThere.PublicKey),
				heyThere.MeshIP,
				heyThere.ID)
		}
	}

	// Start howdy coordinator with self-selection timer
	// The coordinator uses random delays (0-3s) to spread responses and prevent thundering herd
	if !network.ReadOnly {
		network.startHowdyCoordinator(heyThere.From)
	}
	network.Buzz.increase(1)
}

// heyThere broadcasts identity via MQTT as a signed SyncEvent.
// This publishes the same SyncEvent format used in gossip, ensuring
// consistent identity representation across both transports.
func (network *Network) heyThere() {
	if network.ReadOnly {
		return
	}

	// Always record our own online observation (needed for local state)
	network.recordObservationOnlineNara(network.meName(), 0)

	// Rate limit: only send hey_there every 5 seconds (skip in tests for faster execution)
	if !network.testSkipHeyThereRateLimit {
		ts := int64(5) // seconds
		if (time.Now().Unix() - network.LastHeyThere) <= ts {
			return
		}
	}
	network.LastHeyThere = time.Now().Unix()

	// Create signed SyncEvent - same format for MQTT and gossip
	event := NewHeyThereSyncEvent(
		network.meName(),
		network.local.Me.Status.PublicKey,
		network.local.Me.Status.MeshIP,
		network.local.ID,
		network.local.Keypair,
	)

	// Publish to MQTT
	topic := "nara/plaza/hey_there"
	network.postEvent(topic, event)
	logrus.Printf("%s: 👋 (MQTT)", network.meName())

	// Also add to our ledger for gossip propagation and projection tracking
	if network.local.SyncLedger != nil {
		network.local.SyncLedger.AddEvent(event)
		// Trigger projection so it knows we're online
		if network.local.Projections != nil {
			network.local.Projections.Trigger()
		}
	}

	network.Buzz.increase(2)
}
