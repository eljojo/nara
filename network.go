package nara

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
	"tailscale.com/tsnet"
)

// TransportMode determines how events spread through the network
type TransportMode int

const (
	// TransportMQTT uses only MQTT broadcast (traditional mode)
	TransportMQTT TransportMode = iota
	// TransportGossip uses only P2P zine exchange (pure mesh)
	TransportGossip
	// TransportHybrid uses both MQTT and gossip (default, most resilient)
	TransportHybrid
)

// String returns the string representation of the transport mode
func (t TransportMode) String() string {
	switch t {
	case TransportMQTT:
		return "mqtt"
	case TransportGossip:
		return "gossip"
	case TransportHybrid:
		return "hybrid"
	default:
		return "unknown"
	}
}

// Zine is a batch of recent events passed hand-to-hand between naras
// Like underground zines at punk shows, these spread organically through mesh network
type Zine struct {
	From      string      `json:"from"`       // Publisher nara
	CreatedAt int64       `json:"created_at"` // Unix timestamp
	Events    []SyncEvent `json:"events"`     // Recent events (last ~5 minutes)
	Signature string      `json:"signature"`  // Cryptographic signature for authenticity
}

type Network struct {
	Neighbourhood          map[string]*Nara
	Buzz                   *Buzz
	LastHeyThere           int64
	skippingEvents         bool
	local                  *LocalNara
	Mqtt                   mqtt.Client
	heyThereInbox          chan SyncEvent
	newspaperInbox         chan NewspaperEvent
	chauInbox              chan SyncEvent
	howdyInbox             chan HowdyEvent
	howdyCoordinators      sync.Map // map[string]*howdyCoordinator - tracks pending howdy responses
	startTimeVotes         []startTimeVote
	startTimeVotesMu       sync.Mutex
	socialInbox            chan SyncEvent
	ledgerRequestInbox     chan LedgerRequest
	ledgerResponseInbox    chan LedgerResponse
	stashDistributeTrigger chan struct{} // Trigger immediate stash distribution to confidants
	TeaseState             *TeaseState
	ReadOnly               bool
	// SSE broadcast for web clients
	sseClients   map[chan SyncEvent]bool
	sseClientsMu sync.RWMutex
	// World journey state
	worldJourneys   []*WorldMessage // Completed journeys
	worldJourneysMu sync.RWMutex
	worldHandler    *WorldJourneyHandler
	worldMesh       *MockMeshNetwork   // Used when no tsnet configured
	worldTransport  *MockMeshTransport // Used when no tsnet configured
	tsnetMesh       *TsnetMesh         // Used when Headscale is configured
	// Transport mode (MQTT, Gossip, or Hybrid)
	TransportMode TransportMode
	// Peer discovery strategy for gossip-only mode
	peerDiscovery PeerDiscovery
	// Pending journey tracking for timeout detection
	pendingJourneys      map[string]*PendingJourney
	pendingJourneysMu    sync.RWMutex
	journeyCompleteInbox chan JourneyCompletion
	// Graceful shutdown
	ctx        context.Context
	cancelFunc context.CancelFunc
	// Startup sequencing: operations must complete in order
	bootRecoveryDone chan struct{}
	// Test hooks (only used in tests)
	testHTTPClient        *http.Client                    // Override HTTP client for testing
	testMeshURLs          map[string]string               // Override mesh URLs for testing (nara name -> URL)
	testTeaseDelay        *time.Duration                  // Override tease delay for testing (nil = use default 0-5s random)
	testAnnounceCount     int                             // Counter for announce() calls (for testing)
	testSkipHeyThereSleep bool                            // Skip the 1s sleep in handleHeyThereEvent (for testing)
	testSkipJitter        bool                            // Skip jitter delays in hey_there for faster tests
	testSkipBootRecovery  bool                            // Skip boot recovery entirely (for checkpoint tests)
	testPingFunc          func(name string) (bool, error) // Override ping behavior for testing (returns success, error)
	// HTTP servers for graceful shutdown
	httpServer        *http.Server
	meshHttpServer    *http.Server
	httpClient        *http.Client
	meshHTTPClient    *http.Client
	meshHTTPTransport *http.Transport
	// Peer resolution tracking
	pendingResolutions sync.Map // map[string]uint64 (seconds) - tracks when we last tried to resolve a peer

	// MQTT reconnect guard
	mqttReconnectMu     sync.Mutex
	mqttReconnectActive bool

	// Stash: distributed encrypted storage
	stashManager     *StashManager        // Owner side: manages our arbitrary JSON stash on confidants
	confidantStore   *ConfidantStashStore // Confidant side: stores others' stash for them
	stashSyncTracker *StashSyncTracker    // Memory-only tracker for last sync time with each peer
	stashService     *StashService        // Unified stash service layer

	// Logging service
	logService *LogService

	// Checkpoint service: consensus-based checkpointing
	checkpointService *CheckpointService
}

// PendingJourney tracks a journey we participated in, waiting for completion
type PendingJourney struct {
	JourneyID  string
	Originator string
	SeenAt     int64  // when we first saw it
	Message    string // original message for context
}

// JourneyCompletion is the lightweight MQTT signal for journey completion
type JourneyCompletion struct {
	JourneyID  string     `json:"journey_id"`
	Originator string     `json:"originator"`
	ReportedBy string     `json:"reported_by"`
	Message    string     `json:"message,omitempty"`
	Hops       []WorldHop `json:"hops,omitempty"` // The attestation log
}

type NewspaperEvent struct {
	From       string
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

// VerifyNewspaper verifies a newspaper event signature
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
	return VerifySignatureBase64(publicKey, statusJSON, event.Signature)
}

type HeyThereEvent struct {
	From      string
	PublicKey string // Base64-encoded Ed25519 public key
	MeshIP    string // Tailscale IP for mesh communication
	ID        string // Nara ID: deterministic hash of soul+name
	Signature string // Base64-encoded signature of "hey_there:{From}:{PublicKey}:{MeshIP}:{ID}"
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
		oldMessage := fmt.Sprintf("hey_there:%s:%s:%s", h.From, h.PublicKey, h.MeshIP)
		return VerifySignatureBase64(pubKey, []byte(oldMessage), h.Signature)
	}

	return false
}

// signatureMessage returns the message to sign based on whether ID is present
func (h *HeyThereEvent) signatureMessage() string {
	if h.ID != "" && false { // TODO(signature) temporarily turned off
		return fmt.Sprintf("hey_there:%s:%s:%s:%s", h.From, h.PublicKey, h.MeshIP, h.ID)
	}
	// Legacy format for naras without ID
	return fmt.Sprintf("hey_there:%s:%s:%s", h.From, h.PublicKey, h.MeshIP)
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
func (h *HeyThereEvent) GetActor() string { return h.From }

// GetTarget implements Payload interface for HeyThereEvent
func (h *HeyThereEvent) GetTarget() string { return h.From }

// UIFormat returns UI-friendly representation
func (h *HeyThereEvent) UIFormat() map[string]string {
	detail := "hey there!"
	if h.MeshIP != "" {
		detail = fmt.Sprintf("mesh: %s", h.MeshIP)
	}
	return map[string]string{
		"icon":   "👋",
		"text":   fmt.Sprintf("%s joined the network", h.From),
		"detail": detail,
	}
}

// LogFormat returns technical log description
func (h *HeyThereEvent) LogFormat() string {
	return fmt.Sprintf("hey-there from %s (mesh: %s)", h.From, h.MeshIP)
}

// ToLogEvent returns a structured log event for the logging system
func (h *HeyThereEvent) ToLogEvent() *LogEvent {
	return &LogEvent{
		Category: CategoryPresence,
		Type:     "welcome",
		Actor:    h.From,
		Target:   h.From,
		Detail:   h.LogFormat(),
	}
}

type ChauEvent struct {
	From      string
	PublicKey string // Base64-encoded Ed25519 public key
	ID        string // Nara ID: deterministic hash of soul+name
	Signature string // Base64-encoded signature of "chau:{From}:{PublicKey}:{ID}"
}

// Sign signs the ChauEvent with the given keypair
func (c *ChauEvent) Sign(kp NaraKeypair) {
	message := c.signatureMessage()
	c.Signature = kp.SignBase64([]byte(message))
}

// Verify verifies the ChauEvent signature against the embedded public key
func (c *ChauEvent) Verify() bool {
	if c.PublicKey == "" || c.Signature == "" {
		return false
	}
	pubKey, err := ParsePublicKey(c.PublicKey)
	if err != nil {
		return false
	}

	// Try new format first (with ID)
	message := c.signatureMessage()
	if VerifySignatureBase64(pubKey, []byte(message), c.Signature) {
		return true
	}

	// Fall back to old format (without ID) for backwards compatibility
	if c.ID != "" {
		oldMessage := fmt.Sprintf("chau:%s:%s", c.From, c.PublicKey)
		return VerifySignatureBase64(pubKey, []byte(oldMessage), c.Signature)
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
func (c *ChauEvent) GetActor() string { return c.From }

// GetTarget implements Payload interface for ChauEvent
func (c *ChauEvent) GetTarget() string { return c.From }

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
		Actor:    c.From,
		Detail:   c.LogFormat(),
	}
}

// PeerResponse contains identity information about a peer.
// Used by the peer resolution protocol to return discovered peer info.
type PeerResponse struct {
	Target    string `json:"target"`
	PublicKey string `json:"public_key"`
	MeshIP    string `json:"mesh_ip,omitempty"`
}

// NeighborInfo contains information about a neighbor to share in howdy responses
type NeighborInfo struct {
	Name        string
	PublicKey   string
	MeshIP      string
	ID          string          // Nara ID: deterministic hash of soul+name
	Observation NaraObservation // What I know about this neighbor
}

// HowdyEvent is sent in response to hey_there to help with discovery and start time recovery
type HowdyEvent struct {
	From      string          // Who's sending this howdy
	To        string          // Who this is in response to (hey_there sender)
	Seq       int             // Sequence number (1-10) for coordination
	You       NaraObservation // What I know about you (includes StartTime!)
	Neighbors []NeighborInfo  // ~10 other naras you should know about
	Me        NaraStatus      // My own status
	Signature string          // Ed25519 signature
}

// Sign signs the HowdyEvent with the given keypair
func (h *HowdyEvent) Sign(kp NaraKeypair) {
	// Sign a deterministic representation of the event
	message := fmt.Sprintf("howdy:%s:%s:%d", h.From, h.To, h.Seq)
	h.Signature = kp.SignBase64([]byte(message))
}

// Verify verifies the HowdyEvent signature against the public key in Me.PublicKey
func (h *HowdyEvent) Verify() bool {
	if h.Me.PublicKey == "" || h.Signature == "" {
		return false
	}
	pubKey, err := ParsePublicKey(h.Me.PublicKey)
	if err != nil {
		return false
	}
	message := fmt.Sprintf("howdy:%s:%s:%d", h.From, h.To, h.Seq)
	return VerifySignatureBase64(pubKey, []byte(message), h.Signature)
}

func NewNetwork(localNara *LocalNara, host string, user string, pass string) *Network {
	network := &Network{local: localNara}
	network.Neighbourhood = make(map[string]*Nara)
	network.heyThereInbox = make(chan SyncEvent, 50)
	network.chauInbox = make(chan SyncEvent, 50)
	network.howdyInbox = make(chan HowdyEvent, 50)
	network.newspaperInbox = make(chan NewspaperEvent, 50)
	network.socialInbox = make(chan SyncEvent, 50)
	network.ledgerRequestInbox = make(chan LedgerRequest, 50)
	network.ledgerResponseInbox = make(chan LedgerResponse, 50)
	network.stashDistributeTrigger = make(chan struct{}, 5) // Buffered to avoid blocking
	network.TeaseState = NewTeaseState()
	network.sseClients = make(map[chan SyncEvent]bool)
	network.skippingEvents = false
	network.Buzz = newBuzz()
	network.worldJourneys = make([]*WorldMessage, 0)
	network.pendingJourneys = make(map[string]*PendingJourney)
	network.journeyCompleteInbox = make(chan JourneyCompletion, 50)
	// Initialize context for graceful shutdown
	network.ctx, network.cancelFunc = context.WithCancel(context.Background())
	// Initialize startup sequencing channels
	network.bootRecoveryDone = make(chan struct{})

	// Stash storage
	network.confidantStore = NewConfidantStashStore()
	network.stashSyncTracker = NewStashSyncTracker() // Memory-only, never persisted
	// Configure stash storage limit based on memory mode
	stashLimit := StashStorageForMemoryMode(localNara.MemoryProfile.Mode)
	network.confidantStore.SetMaxStashes(stashLimit)
	logrus.Infof("📦 Stash storage limit: %d (memory mode: %s)", stashLimit, localNara.MemoryProfile.Mode)
	// stashManager initialized after keypair is available

	// Initialize log service
	network.logService = NewLogService(localNara.Me.Name)
	network.logService.RegisterWithLedger(localNara.SyncLedger)

	// Initialize checkpoint service
	network.checkpointService = NewCheckpointService(network, localNara.SyncLedger, localNara)

	network.Mqtt = network.initializeMQTT(network.mqttOnConnectHandler(), network.meName(), host, user, pass)

	// Set up pruning priority for unknown naras (events from naras without public keys are pruned first)
	if localNara.SyncLedger != nil {
		localNara.SyncLedger.SetUnknownNaraChecker(func(name string) bool {
			return !network.hasPublicKeyFor(name)
		})
	}

	return network
}

// Context returns the network's context, which is canceled when the network shuts down.
func (network *Network) Context() context.Context {
	return network.ctx
}

// IsBootRecoveryComplete returns true if boot recovery has finished and opinions are formed.
// This should be checked before participating in checkpoint consensus to ensure accurate values.
func (network *Network) IsBootRecoveryComplete() bool {
	if network.bootRecoveryDone == nil {
		return false
	}
	select {
	case <-network.bootRecoveryDone:
		return true
	default:
		return false
	}
}

// SetVerboseLogging enables or disables verbose logging mode.
// In verbose mode, all log events are printed immediately with full detail.
func (network *Network) SetVerboseLogging(verbose bool) {
	if network.logService != nil {
		network.logService.SetVerbose(verbose)
	}
}

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

// getMeshIPForNara returns the tailscale IP for a nara (for direct mesh communication)
func (network *Network) getMeshIPForNara(name string) string {
	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	nara, ok := network.Neighbourhood[name]
	if !ok {
		return ""
	}

	nara.mu.Lock()
	defer nara.mu.Unlock()
	return nara.Status.MeshIP
}

func (network *Network) getMyClout() map[string]float64 {
	// Get this nara's clout scores for other naras
	if network.local.Projections == nil {
		return nil
	}

	baseClout := network.local.Projections.Clout().DeriveClout(network.local.Soul, network.local.Me.Status.Personality)

	// Apply proximity weighting (nearby naras have more influence)
	network.local.Me.mu.Lock()
	myCoords := network.local.Me.Status.Coordinates
	network.local.Me.mu.Unlock()

	return ApplyProximityToClout(baseClout, myCoords, network.getCoordinatesForPeer)
}

func (network *Network) getOnlineNaraNames() []string {
	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	// Check if we're using real mesh (tsnet) - if so, only include mesh-enabled naras
	requireMesh := network.tsnetMesh != nil

	names := []string{network.local.Me.Name}
	skippedCount := 0
	for name, nara := range network.Neighbourhood {
		// Check our observation of this peer (not their self-observation)
		obs := network.local.getObservation(name)
		if obs.Online != "ONLINE" {
			continue
		}

		// If using tsnet, only include mesh-enabled naras
		if requireMesh {
			nara.mu.Lock()
			meshEnabled := nara.Status.MeshEnabled
			nara.mu.Unlock()
			if !meshEnabled {
				skippedCount++
				continue
			}
		}

		names = append(names, name)
	}

	if requireMesh && skippedCount > 0 {
		logrus.Infof("🕸️  World journey: %d mesh-enabled naras, skipped %d non-mesh naras", len(names)-1, skippedCount)
	}

	return names
}

func (network *Network) getPublicKeyForNara(name string) []byte {
	if name == network.local.Me.Name {
		return network.local.Keypair.PublicKey
	}

	network.local.mu.Lock()
	nara, ok := network.Neighbourhood[name]
	network.local.mu.Unlock()

	if !ok {
		return nil
	}

	// Lock nara before accessing its Status to avoid race condition
	nara.mu.Lock()
	publicKey := nara.Status.PublicKey
	nara.mu.Unlock()

	if publicKey == "" {
		return nil
	}

	pubKey, err := ParsePublicKey(publicKey)
	if err != nil {
		return nil
	}
	return pubKey
}

// resolvePublicKeyForNara returns the public key for a nara, attempting to resolve it
// via neighbors if not already known.
func (network *Network) resolvePublicKeyForNara(name string) []byte {
	pubKey := network.getPublicKeyForNara(name)
	if pubKey != nil {
		return pubKey
	}

	// Not found locally, try to resolve via neighbors (blocking)
	resp := network.resolvePeer(name)
	if resp != nil && resp.PublicKey != "" {
		logrus.Infof("📡 Resolved public key for %s via peer resolution", name)
		return network.getPublicKeyForNara(name)
	}

	// Log via LogService (batched)
	if network.logService != nil {
		network.logService.BatchPeerResolutionFailed(name)
	}
	return nil
}

// hasPublicKeyFor returns true if we have a valid public key for the named nara.
// Used to determine if a nara is "known" for pruning priority.
func (network *Network) hasPublicKeyFor(name string) bool {
	return network.getPublicKeyForNara(name) != nil
}

// getPublicKeyForNaraID looks up a public key by nara ID instead of name.
// Searches the neighborhood for a nara with matching ID.
func (network *Network) getPublicKeyForNaraID(naraID string) []byte {
	// Check self first
	if naraID == network.local.Me.Status.ID {
		return network.local.Keypair.PublicKey
	}

	// Search neighborhood by ID
	network.local.mu.Lock()
	var matchedNara *Nara
	for _, nara := range network.Neighbourhood {
		nara.mu.Lock()
		if nara.Status.ID == naraID {
			matchedNara = nara
			nara.mu.Unlock()
			break
		}
		nara.mu.Unlock()
	}
	network.local.mu.Unlock()

	if matchedNara == nil {
		return nil
	}

	matchedNara.mu.Lock()
	publicKey := matchedNara.Status.PublicKey
	matchedNara.mu.Unlock()

	if publicKey == "" {
		return nil
	}

	pubKey, err := ParsePublicKey(publicKey)
	if err != nil {
		return nil
	}
	return pubKey
}

// VerifySyncEvent verifies a sync event's signature and logs warnings
// Returns true if the event is valid (signed and verified, or unsigned but acceptable)
// The event is always added regardless - verification is informational
func (network *Network) VerifySyncEvent(e *SyncEvent) bool {
	if !e.IsSigned() {
		logrus.Tracef("Unsigned event %s from service %s (actor: %s)", e.ID[:8], e.Service, e.GetActor())
		return true // Unsigned is acceptable, just log it
	}

	// Get the emitter's public key
	pubKey := network.getPublicKeyForNara(e.Emitter)
	if pubKey == nil {
		if !network.local.isBooting() {
			logrus.Warnf("Cannot verify event %s: unknown emitter %s", e.ID[:8], e.Emitter)
			// Trigger resolution in background so we might know them for future events
			network.resolvePeerBackground(e.Emitter)
		}
		return false // Signed but can't verify - suspicious
	}

	if !e.Verify(pubKey) {
		logrus.Warnf("Invalid signature on event %s from %s", e.ID[:8], e.Emitter)
		return false // Bad signature - suspicious
	}

	logrus.Tracef("Verified event %s from %s", e.ID[:8], e.Emitter)
	return true
}

// MergeSyncEventsWithVerification merges events into SyncLedger after verifying signatures
// Returns the number of events added and number that had verification warnings
func (network *Network) MergeSyncEventsWithVerification(events []SyncEvent) (added int, warned int) {
	// First, discover any naras mentioned in these events
	network.discoverNarasFromEvents(events)

	// Process hey_there sync events to learn peer identities
	network.processHeyThereSyncEvents(events)

	// Process chau sync events for graceful shutdown detection
	network.processChauSyncEvents(events)

	// Mark event emitters as seen - if we receive events they created,
	// that's evidence they exist and are active
	network.markEmittersAsSeen(events)

	for i := range events {
		e := &events[i]
		if !network.VerifySyncEvent(e) {
			warned++
		}
	}
	added = network.local.SyncLedger.MergeEvents(events)

	// Trigger projection updates if events were added
	if added > 0 && network.local.Projections != nil {
		network.local.Projections.Trigger()
	}

	return added, warned
}

// discoverNarasFromEvents creates Nara entries for any unknown naras mentioned in events.
// This allows us to track observations about naras we hear about through the event stream,
// even before we know their public key.
func (network *Network) discoverNarasFromEvents(events []SyncEvent) {
	seen := make(map[string]bool)
	myName := network.meName()

	for _, e := range events {
		// Collect all nara names from this event
		names := []string{e.Emitter, e.GetActor(), e.GetTarget()}
		for _, name := range names {
			if name == "" || name == myName || seen[name] {
				continue
			}
			seen[name] = true

			// Check if we know this nara
			network.local.mu.Lock()
			_, known := network.Neighbourhood[name]
			network.local.mu.Unlock()

			if !known {
				isRecent := (time.Now().Unix() - e.Timestamp/1e9) < MissingThresholdSeconds
				if isRecent || network.local.isBooting() || e.Service == ServiceHeyThere {
					network.importNara(NewNara(name))
					logrus.Debugf("📖 Discovered nara %s from event stream", name)
				}
			}
		}
	}
}

// markEmittersAs seen marks event emitters as seen/online.
// When we receive events that a nara created (they're the Emitter), that's evidence
// they exist and are active. This allows us to discover naras through zine/gossip
// exchanges before we directly receive their newspaper.
func (network *Network) markEmittersAsSeen(events []SyncEvent) {
	if network.local.isBooting() {
		return // Don't mark during boot to avoid noise
	}

	// First pass: identify which emitters have chau events in this batch
	// These naras are shutting down and should NOT be marked as online,
	// even if they have other events in the batch (e.g., events created before shutdown).
	shuttingDown := make(map[string]bool)
	for _, e := range events {
		if e.Service == ServiceChau && e.Chau != nil && e.Chau.From != "" {
			shuttingDown[e.Chau.From] = true
		}
	}

	seen := make(map[string]bool)
	myName := network.meName()

	for _, e := range events {
		emitter := e.Emitter
		if emitter == "" || emitter == myName || seen[emitter] {
			continue
		}

		// Skip chau events - they indicate the emitter is going OFFLINE, not online
		if e.Service == ServiceChau {
			continue
		}

		// Skip if this emitter has a chau event in the same batch
		// This prevents marking a shutting-down nara as online based on their
		// older events (social, ping, etc.) that were created before shutdown.
		if shuttingDown[emitter] {
			continue
		}

		seen[emitter] = true

		// Check if we already have this nara marked as online
		obs := network.local.getObservation(emitter)
		if obs.Online == "ONLINE" {
			continue // Already marked as online, skip
		}

		// Mark them as seen - we received events they emitted
		// NOTE: We do NOT emit a seen event here - the emitter is proving themselves
		// through their own events. Seen events are only for vouching for quiet naras.
		network.recordObservationOnlineNara(emitter, e.Timestamp/1e9)
		logrus.Debugf("📖 Marked %s as seen via event emission", emitter)
	}
}

// processHeyThereSyncEvents extracts peer identity information from hey_there sync events.
// This enables peer discovery through gossip without requiring MQTT broadcasts.
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
			if !e.Verify(pubKey) {
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
			if updated {
				logrus.Infof("📡 Updated identity for %s via hey_there event (🔑)", h.From)
			}
		} else {
			// Create new nara and import it
			newNara := NewNara(h.From)
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

// InitGossipIdentity initializes gossip-mode identity emission.
// Called by Start() and can be called by tests to simulate startup.
// This emits the hey_there sync event that allows our identity to propagate through gossip.
func (network *Network) InitGossipIdentity() {
	if network.TransportMode != TransportMQTT {
		network.emitHeyThereSyncEvent()
	}
}

// processChauSyncEvents processes chau (graceful shutdown) events from the sync ledger.
// This allows gossip-only naras to distinguish OFFLINE (graceful) from MISSING (timeout).
func (network *Network) processChauSyncEvents(events []SyncEvent) {
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

// hasMoreRecentHeyThere checks if there's a hey_there event from the given nara
// that's more recent than the specified timestamp.
func (network *Network) hasMoreRecentHeyThere(from string, thanTimestamp int64) bool {
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

// emitChauSyncEvent creates and adds a chau sync event to our ledger.
// This allows graceful shutdown to propagate through gossip.
func (network *Network) emitChauSyncEvent() {
	publicKey := FormatPublicKey(network.local.Keypair.PublicKey)
	event := NewChauSyncEvent(network.meName(), publicKey, network.local.ID, network.local.Keypair)
	network.local.SyncLedger.MergeEvents([]SyncEvent{event})
	if network.local.Projections != nil {
		network.local.Projections.Trigger()
	}
	// LogService handles logging via ledger watching
}

// --- Peer Resolution Protocol ---
// When we don't know a peer's identity (public key), we can query neighbors.
// Uses HTTP redirects: if a neighbor doesn't know, they redirect us to try someone else.

// resolvePeerBackground triggers resolvePeer in a goroutine with rate limiting
func (network *Network) resolvePeerBackground(target string) {
	now := time.Now().Unix()
	const retryInterval = 600 // 10 minutes

	// Check if we're already resolving or tried recently
	if lastTry, loaded := network.pendingResolutions.LoadOrStore(target, now); loaded {
		if now-lastTry.(int64) < retryInterval {
			// Too soon to retry or already in progress
			return
		}
		// Update the timestamp for the new attempt
		network.pendingResolutions.Store(target, now)
	}

	go func() {
		// Ensure we eventually allow retries even if it fails/panics
		// (though the LoadOrStore above handles the interval)
		network.resolvePeer(target)
	}()
}

// resolvePeer queries neighbors to discover the identity of an unknown peer.
// Returns nil if no one knows the target within the timeout.
func (network *Network) resolvePeer(target string) *PeerResponse {
	// Double check we don't already have it (e.g. if another resolution finished)
	if network.getPublicKeyForNara(target) != nil {
		return nil
	}

	logrus.Debugf("📡 Attempting to resolve unknown peer: %s", target)

	// Check if we already know this peer
	if network.getPublicKeyForNara(target) != nil {
		network.local.mu.Lock()
		nara := network.Neighbourhood[target]
		network.local.mu.Unlock()
		if nara != nil {
			nara.mu.Lock()
			resp := &PeerResponse{
				Target:    target,
				PublicKey: nara.Status.PublicKey,
				MeshIP:    nara.Status.MeshIP,
			}
			nara.mu.Unlock()
			return resp
		}
	}

	// Track who we've already asked to prevent loops
	asked := map[string]bool{network.meName(): true}

	// Get initial neighbors to query
	neighbors := network.NeighbourhoodOnlineNames()
	if len(neighbors) == 0 {
		return nil
	}

	// Create HTTP client that doesn't follow redirects automatically
	client := network.getHTTPClient()
	noRedirectClient := &http.Client{
		Timeout: client.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // Don't follow redirects automatically
		},
	}

	// Try each neighbor, following redirects manually
	toAsk := make([]string, len(neighbors))
	copy(toAsk, neighbors)

	for len(toAsk) > 0 && len(asked) < 10 { // Max 10 hops
		name := toAsk[0]
		toAsk = toAsk[1:]

		if asked[name] {
			continue
		}
		asked[name] = true

		url := network.getMeshURLForNara(name)
		if url == "" {
			continue
		}

		// Build asked list for the request
		askedList := make([]string, 0, len(asked))
		for n := range asked {
			askedList = append(askedList, n)
		}

		resp := network.queryPeerAt(noRedirectClient, url, target, askedList)
		if resp == nil {
			continue
		}

		if resp.PublicKey != "" {
			// Found it! Import and return
			newNara := NewNara(target)
			newNara.Status.PublicKey = resp.PublicKey
			newNara.Status.MeshIP = resp.MeshIP
			newNara.Status.MeshEnabled = resp.MeshIP != ""
			network.importNara(newNara)
			logrus.Infof("📡 Resolved peer %s via query to %s (🔑)", target, name)
			return resp
		}

		// Got a redirect suggestion - add to list if not already asked
		if resp.Target != "" && !asked[resp.Target] {
			toAsk = append(toAsk, resp.Target)
		}
	}

	logrus.Debugf("📡 Peer resolution failed for %s after asking %d neighbors", target, len(asked)-1)
	return nil
}

// TODO: Add integration test for queryPeerAt using httptest.NewServer pattern
// See TestIntegration_CheckpointSync for reference on how to inject HTTP mux/client
// This should test the full HTTP request/response flow for peer queries
//
// queryPeerAt sends a peer query to a specific URL and handles the response.
// Returns a PeerResponse with PublicKey set if found, or with Target set if redirected.
func (network *Network) queryPeerAt(client *http.Client, baseURL, target string, asked []string) *PeerResponse {
	// Build query URL with parameters
	queryURL := fmt.Sprintf("%s/peer/query?target=%s&asked=%s",
		baseURL, target, strings.Join(asked, ","))

	req, err := http.NewRequest("GET", queryURL, nil)
	if err != nil {
		return nil
	}

	resp, err := client.Do(req)
	if err != nil {
		logrus.Debugf("📡 Peer query to %s failed: %v", baseURL, err)
		return nil
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// They know the target - parse response
		var peerResp PeerResponse
		if err := json.NewDecoder(resp.Body).Decode(&peerResp); err != nil {
			return nil
		}
		return &peerResp

	case http.StatusTemporaryRedirect, http.StatusSeeOther:
		// They don't know, but suggest someone else
		// The redirect location contains the suggested neighbor's name
		location := resp.Header.Get("X-Nara-Redirect-To")
		if location != "" {
			return &PeerResponse{Target: location} // Target field used to indicate redirect
		}
		return nil

	case http.StatusNotFound:
		// They don't know and have no suggestions
		return nil

	default:
		return nil
	}
}

// httpPeerQueryHandler handles incoming peer queries.
// GET /peer/query?target=name&asked=a,b,c
// Returns: 200 + JSON if known, 307 + X-Nara-Redirect-To if redirecting, 404 if unknown
func (network *Network) httpPeerQueryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	target := r.URL.Query().Get("target")
	if target == "" {
		http.Error(w, "target parameter required", http.StatusBadRequest)
		return
	}

	// Parse the asked list
	askedStr := r.URL.Query().Get("asked")
	asked := make(map[string]bool)
	if askedStr != "" {
		for _, name := range strings.Split(askedStr, ",") {
			asked[name] = true
		}
	}
	asked[network.meName()] = true // We've now been asked

	// Check if we know the target
	pubKey := network.getPublicKeyForNara(target)
	if pubKey != nil {
		network.local.mu.Lock()
		nara := network.Neighbourhood[target]
		network.local.mu.Unlock()

		if nara != nil {
			nara.mu.Lock()
			response := PeerResponse{
				Target:    target,
				PublicKey: nara.Status.PublicKey,
				MeshIP:    nara.Status.MeshIP,
			}
			nara.mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}
	}

	// We don't know - find a neighbor to redirect to
	neighbors := network.NeighbourhoodOnlineNames()
	for _, name := range neighbors {
		if !asked[name] {
			// Redirect to this neighbor
			w.Header().Set("X-Nara-Redirect-To", name)
			w.WriteHeader(http.StatusTemporaryRedirect)
			return
		}
	}

	// No one else to ask
	http.NotFound(w, r)
}

// httpPeerResponseHandler is no longer needed with the redirect-based approach
func (network *Network) httpPeerResponseHandler(w http.ResponseWriter, r *http.Request) {
	// Kept for backwards compatibility, but not used
	http.NotFound(w, r)
}

// buildMeshURL returns the full mesh URL for a nara with optional path
// Returns empty string if nara is not reachable via mesh
// Examples:
//   - buildMeshURL("alice", "") -> "http://100.64.0.1:5683"
//   - buildMeshURL("alice", "/stash/push") -> "http://100.64.0.1:5683/stash/push"
// buildMeshURLFromIP builds a mesh URL from an IP address and optional path.
// Handles both test URLs (with port already included) and production IPs (adds DefaultMeshPort).
// Examples:
//   - buildMeshURLFromIP("100.64.0.1", "/ping") -> "http://100.64.0.1:7433/ping"
//   - buildMeshURLFromIP("127.0.0.1:12345", "/ping") -> "http://127.0.0.1:12345/ping" (test)
func (network *Network) buildMeshURLFromIP(ip string, path string) string {
	if ip == "" {
		return ""
	}

	var baseURL string
	if strings.Contains(ip, ":") {
		// IP already contains port (e.g., from tests)
		baseURL = "http://" + ip
	} else {
		// Production IP without port - add DefaultMeshPort
		baseURL = fmt.Sprintf("http://%s:%d", ip, DefaultMeshPort)
	}

	if path != "" {
		return baseURL + path
	}
	return baseURL
}

func (network *Network) buildMeshURL(name string, path string) string {
	var baseURL string
	if network.testMeshURLs != nil {
		baseURL = network.testMeshURLs[name]
		if baseURL == "" {
			return ""
		}
	} else {
		meshIP := network.getMeshIPForNara(name)
		if meshIP == "" {
			return ""
		}
		baseURL = network.buildMeshURLFromIP(meshIP, "")
	}

	if path != "" {
		return baseURL + path
	}
	return baseURL
}

// getMeshURLForNara returns the base mesh URL for a nara (for backwards compatibility)
func (network *Network) getMeshURLForNara(name string) string {
	return network.buildMeshURL(name, "")
}

// hasMeshConnectivity returns true if a nara is reachable via mesh
// Checks both test URLs and production mesh IPs
func (network *Network) hasMeshConnectivity(name string) bool {
	if network.testMeshURLs != nil {
		return network.testMeshURLs[name] != ""
	}
	return network.getMeshIPForNara(name) != ""
}

// getHTTPClient returns the HTTP client to use (test override or default)
func (network *Network) getHTTPClient() *http.Client {
	if network.testHTTPClient != nil {
		return network.testHTTPClient
	}
	if network.httpClient == nil {
		network.httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	return network.httpClient
}

// getMeshHTTPClient returns the mesh HTTP client to use (test override or tsnet client).
func (network *Network) getMeshHTTPClient() *http.Client {
	if network.testHTTPClient != nil {
		return network.testHTTPClient
	}
	if network.meshHTTPClient != nil {
		return network.meshHTTPClient
	}
	if network.tsnetMesh == nil || network.tsnetMesh.Server() == nil {
		return nil
	}
	network.initMeshHTTPClients(network.tsnetMesh.Server())
	return network.meshHTTPClient
}

// initMeshHTTPClients initializes shared mesh HTTP clients and transport.
func (network *Network) initMeshHTTPClients(tsnetServer *tsnet.Server) {
	if tsnetServer == nil {
		return
	}
	if network.meshHTTPTransport == nil {
		network.meshHTTPTransport = &http.Transport{
			DialContext:         tsnetServer.Dial,
			MaxIdleConns:        200,
			MaxIdleConnsPerHost: 20,
			IdleConnTimeout:     30 * time.Second,
		}
	}
	if network.meshHTTPClient == nil {
		network.meshHTTPClient = &http.Client{
			Transport: network.meshHTTPTransport,
			Timeout:   5 * time.Second,
		}
	}
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

func (network *Network) Start(serveUI bool, httpAddr string, meshConfig *TsnetConfig) {
	if serveUI {
		err := network.startHttpServer(httpAddr)
		if err != nil {
			logrus.Panic(err)
		}
	}

	// Initialize stash manager if we have a keypair
	if network.local.Keypair.PrivateKey != nil {
		network.stashManager = NewStashManager(
			network.meName(),
			network.local.Keypair,
			3, // target 3 confidants
		)
	}

	// Initialize unified stash service
	network.initStashService()

	// Start log service
	if network.logService != nil {
		network.logService.Start(network.ctx)
	}

	// Initialize mesh networking and world journey handler
	if !network.ReadOnly {
		if meshConfig != nil {
			// Use real tsnet mesh with Headscale
			tsnetMesh, err := NewTsnetMesh(*meshConfig)
			if err != nil {
				logrus.Errorf("Failed to create tsnet mesh: %v", err)
			} else {
				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				if err := tsnetMesh.Start(ctx); err != nil {
					logrus.Errorf("Failed to start tsnet mesh: %v", err)
				} else {
					network.tsnetMesh = tsnetMesh
					network.local.Me.Status.MeshEnabled = true
					network.local.Me.Status.MeshIP = tsnetMesh.IP()

					network.initMeshHTTPClients(tsnetMesh.Server())

					// Initialize peer discovery for gossip-only mode
					network.peerDiscovery = &TailscalePeerDiscovery{
						client: network.getMeshHTTPClient(),
					}

					// Start mesh HTTP server on tsnet interface (port 7433)
					if err := network.startMeshHttpServer(tsnetMesh.Server()); err != nil {
						logrus.Errorf("Failed to start mesh HTTP server: %v", err)
					}

					// Use HTTP-based transport for world messages (unified with other mesh HTTP)
					httpTransport := NewHTTPMeshTransport(tsnetMesh.Server(), network, DefaultMeshPort)
					network.InitWorldJourney(httpTransport)
					logrus.Infof("🌍 World journey using HTTP over tsnet (IP: %s)", tsnetMesh.IP())

					// In gossip-only mode, discover peers immediately (don't wait 30s)
					if network.TransportMode == TransportGossip {
						logrus.Info("📡 Gossip mode: discovering peers immediately...")
						network.discoverMeshPeers()
					}
				}
			}
		}

		// Fall back to mock mesh if tsnet not configured or failed
		if network.worldHandler == nil {
			network.worldMesh = NewMockMeshNetwork()
			network.worldTransport = NewMockMeshTransport()
			network.worldMesh.Register(network.meName(), network.worldTransport)
			network.InitWorldJourney(network.worldTransport)
			logrus.Info("World journey using mock mesh (local only)")
		}
	}

	// Only connect to MQTT if not in gossip-only mode (after mesh init for MeshIP)
	if network.TransportMode != TransportGossip {
		if token := network.Mqtt.Connect(); token.Wait() && token.Error() != nil {
			logrus.Fatalf("MQTT connection error: %v", token.Error())
		}
	} else {
		logrus.Info("📡 Gossip-only mode: MQTT disabled")
	}

	if !network.ReadOnly {
		// Add jitter (0-5s) to prevent thundering herd when multiple narae start simultaneously
		// Skip jitter in tests for faster discovery
		if !network.testSkipJitter {
			jitter := time.Duration(rand.Intn(5000)) * time.Millisecond
			time.Sleep(jitter)
		}

		network.heyThere() // MQTT broadcast (old style - to be deprecated)
		network.announce()
		network.InitGossipIdentity() // Emit hey_there sync event (new style)
	}

	// Skip this sleep in tests for faster discovery
	if !network.testSkipJitter {
		time.Sleep(1 * time.Second)
	}

	// Start inbox processors early so we can learn neighbors before boot recovery.
	go network.processHeyThereEvents()
	go network.processHowdyEvents()
	go network.processChauEvents()
	go network.processNewspaperEvents()
	go network.processSocialEvents()
	go network.processLedgerRequests()
	go network.processLedgerResponses()
	go network.processJourneyCompleteEvents()

	// Boot recovery first, then opinion formation, then start remaining workers.
	// Suppress ledger event logging during boot recovery to avoid spamming console
	if network.logService != nil {
		network.logService.SetSuppressLedgerEvents(true)
	}
	if !network.ReadOnly && !network.testSkipBootRecovery {
		network.bootRecovery()
	} else {
		close(network.bootRecoveryDone)
	}
	if network.logService != nil {
		network.logService.SetSuppressLedgerEvents(false)
	}

	network.formOpinion()

	go network.observationMaintenance()
	if !network.ReadOnly {
		go network.announceForever()
	}
	go network.trendMaintenance()
	go network.maintenanceBuzz()

	// Start background sync for organic memory strengthening
	if !network.ReadOnly {
		if network.local == nil || network.local.MemoryProfile.EnableBackgroundSync {
			go network.backgroundSync()
		} else {
			logrus.Infof("🔄 Background sync disabled (memory mode: %s)", network.local.MemoryProfile.Mode)
		}
	}

	// Start gossip protocol (P2P zine exchange)
	if !network.ReadOnly && network.TransportMode != TransportMQTT {
		go network.gossipForever()
		// Start mesh peer discovery for gossip-only mode
		if network.TransportMode == TransportGossip {
			go network.meshDiscoveryForever()
		}
	}

	// Start garbage collection maintenance
	go network.socialMaintenance()

	// Start journey timeout maintenance
	go network.journeyTimeoutMaintenance()

	// Start coordinate maintenance (Vivaldi pings)
	go network.coordinateMaintenance()

	// Start stash maintenance (confidant selection, inventory)
	if !network.ReadOnly && network.stashService != nil {
		go network.stashMaintenance()
	}

	// Start checkpoint service (consensus-based checkpointing via MQTT)
	if !network.ReadOnly && network.checkpointService != nil {
		network.checkpointService.SetMQTTClient(network.Mqtt)
		network.checkpointService.Start()
	}
}

func (network *Network) meName() string {
	return network.local.Me.Name
}

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

	// Populate stash metrics
	if network.stashService != nil {
		metrics := network.stashService.GetStorageMetrics()
		slimStatus.StashStored = metrics.StashesStored
		slimStatus.StashBytes = metrics.TotalStashBytes
		slimStatus.StashConfidants = network.stashService.ConfidantCount()
	}

	signedEvent := network.SignNewspaper(slimStatus)
	network.postEvent(topic, signedEvent)
}

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

func (network *Network) handleNewspaperEvent(event NewspaperEvent) {
	// Verify signature if present
	if event.Signature != "" {
		// Get public key - try from event status first, then from known neighbor
		var pubKey []byte
		if event.Status.PublicKey != "" {
			var err error
			pubKey, err = ParsePublicKey(event.Status.PublicKey)
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
			network.logService.BatchNewspaper(event.From, strings.Join(changes, ", "))
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

func (network *Network) processHowdyEvents() {
	for {
		select {
		case event := <-network.howdyInbox:
			network.handleHowdyEvent(event)
		case <-network.ctx.Done():
			logrus.Debug("processHowdyEvents: shutting down")
			return
		}
	}
}

func (network *Network) handleHowdyEvent(howdy HowdyEvent) {
	// Batch observed howdys for aggregated logging
	if network.logService != nil {
		network.logService.BatchObservedHowdy(howdy.From, howdy.To)
	}

	// Notify coordinator that we saw a howdy (for self-selection)
	network.onHowdySeen(howdy)

	// Verify signature
	if !howdy.Verify() {
		logrus.Debugf("howdy from %s failed signature verification", howdy.From)
		return
	}

	// 1. Import the sender (so we know about them)
	sender := NewNara(howdy.From)
	sender.Status = howdy.Me
	network.importNara(sender)
	// The howdy itself is an event they emitted - they prove themselves
	network.recordObservationOnlineNara(howdy.From, 0)

	// 2. If this howdy is for us, process it fully
	if howdy.To == network.meName() {
		// Log via LogService (batched)
		if network.logService != nil {
			network.logService.BatchHowdyForMe(howdy.From)
		}

		// Apply their observation about us (includes StartTime!)
		network.collectStartTimeVote(howdy.You, howdy.Me.HostStats.Uptime)

		// 3. Import the neighbors they shared
		for _, neighbor := range howdy.Neighbors {
			n := NewNara(neighbor.Name)
			n.Status.PublicKey = neighbor.PublicKey
			n.Status.MeshIP = neighbor.MeshIP
			if neighbor.MeshIP != "" {
				n.Status.MeshEnabled = true
			}
			if neighbor.ID != "" {
				n.Status.ID = neighbor.ID
				n.ID = neighbor.ID
			}
			network.importNara(n)
			// Store their observation about this neighbor
			network.mergeExternalObservation(neighbor.Name, neighbor.Observation)
		}
	}

	network.Buzz.increase(1)
}

// startTimeVote represents a vote for our start time from another nara
type startTimeVote struct {
	value  int64
	uptime uint64
}

// collectStartTimeVote collects a vote about our start time from a howdy response
func (network *Network) collectStartTimeVote(obs NaraObservation, senderUptime uint64) {
	// Skip if no start time info
	if obs.StartTime == 0 {
		return
	}

	network.startTimeVotesMu.Lock()
	defer network.startTimeVotesMu.Unlock()

	network.startTimeVotes = append(network.startTimeVotes, startTimeVote{
		value:  obs.StartTime,
		uptime: senderUptime,
	})

	// Don't apply consensus early - let formOpinion() handle it with more data
	// after boot recovery completes (~3+ minutes)
}

// applyStartTimeConsensus applies consensus to determine our start time
// Must be called with startTimeVotesMu held
func (network *Network) applyStartTimeConsensus() {
	if len(network.startTimeVotes) == 0 {
		return
	}

	// Convert to consensusValue format
	var values []consensusValue
	for _, vote := range network.startTimeVotes {
		values = append(values, consensusValue{
			value:  vote.value,
			weight: vote.uptime,
		})
	}

	// Use existing consensus algorithm
	const tolerance int64 = 60 // seconds, handles clock drift
	result := consensusByUptime(values, tolerance, false)

	if result > 0 {
		obs := network.local.getMeObservation()
		if obs.StartTime != result {
			obs.StartTime = result
			network.local.setMeObservation(obs)
			logrus.Infof("🕰️ Recovered start time via howdy consensus: %d (from %d votes)", result, len(network.startTimeVotes))
		}
	}
}

// mergeExternalObservation merges an external observation into our local observations
func (network *Network) mergeExternalObservation(name string, external NaraObservation) {
	obs := network.local.getObservation(name)

	// Update fields if they provide more information
	if external.StartTime > 0 && obs.StartTime == 0 {
		obs.StartTime = external.StartTime
	}
	if external.LastSeen > obs.LastSeen {
		obs.LastSeen = external.LastSeen
	}
	if external.Restarts > obs.Restarts {
		obs.Restarts = external.Restarts
	}
	if external.LastRestart > obs.LastRestart {
		obs.LastRestart = external.LastRestart
	}

	network.local.setObservation(name, obs)
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
		if !event.Verify(pubKey) {
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
			nara.mu.Unlock()
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

// howdyCoordinator tracks howdy responses for a given hey_there
type howdyCoordinator struct {
	target         string          // who said hey_there
	seen           int             // how many howdys we've seen for this target
	mentionedNaras map[string]bool // naras already mentioned in other howdys
	timer          *time.Timer
	responded      bool
	mu             sync.Mutex
}

// startHowdyCoordinator begins the self-selection process to potentially respond with howdy
func (network *Network) startHowdyCoordinator(to string) {
	if network.ReadOnly {
		return
	}

	coord := &howdyCoordinator{
		target:         to,
		mentionedNaras: make(map[string]bool),
	}
	network.howdyCoordinators.Store(to, coord)

	// Initial random delay: 0-3 seconds to spread howdy responses
	// Skip jitter in tests for faster discovery
	var delay time.Duration
	if network.testSkipJitter {
		delay = 0
	} else {
		delay = time.Duration(rand.Intn(3000)) * time.Millisecond
	}
	coord.timer = time.AfterFunc(delay, func() {
		network.maybeRespondWithHowdy(coord)
	})

	// Clean up after 30 seconds
	time.AfterFunc(30*time.Second, func() {
		network.howdyCoordinators.Delete(to)
	})
}

// maybeRespondWithHowdy sends a howdy if we haven't already and <10 have been sent
func (network *Network) maybeRespondWithHowdy(coord *howdyCoordinator) {
	coord.mu.Lock()
	defer coord.mu.Unlock()

	if coord.responded || coord.seen >= 10 {
		return
	}
	coord.responded = true
	coord.seen++

	// Select neighbors to share
	neighbors := network.selectNeighborsForHowdy(coord.target, coord.mentionedNaras)

	network.local.Me.mu.Lock()
	meStatus := network.local.Me.Status
	network.local.Me.mu.Unlock()

	event := &HowdyEvent{
		From:      network.meName(),
		To:        coord.target,
		Seq:       coord.seen,
		You:       network.local.getObservation(coord.target),
		Neighbors: neighbors,
		Me:        meStatus,
	}
	event.Sign(network.local.Keypair)
	network.postEvent("nara/plaza/howdy", event)

	logrus.Infof("👋 Sent howdy to %s (seq=%d, neighbors=%d)", coord.target, coord.seen, len(neighbors))
}

// onHowdySeen updates the coordinator when we see another nara's howdy
func (network *Network) onHowdySeen(howdy HowdyEvent) {
	if val, ok := network.howdyCoordinators.Load(howdy.To); ok {
		coord := val.(*howdyCoordinator)
		coord.mu.Lock()
		coord.seen++

		// Track which naras have been mentioned so we don't duplicate
		coord.mentionedNaras[howdy.From] = true
		for _, neighbor := range howdy.Neighbors {
			coord.mentionedNaras[neighbor.Name] = true
		}

		if coord.seen >= 10 && coord.timer != nil {
			coord.timer.Stop() // Don't bother responding
		}
		coord.mu.Unlock()
	}
}

// selectNeighborsForHowdy selects up to 10 neighbors to share in a howdy response
// Priority: online naras first (not already mentioned), then offline if needed
func (network *Network) selectNeighborsForHowdy(exclude string, alreadyMentioned map[string]bool) []NeighborInfo {
	maxNeighbors := 10
	if network.isShortMemoryMode() {
		maxNeighbors = 5
	}

	type naraWithLastSeen struct {
		name     string
		lastSeen int64
		online   bool
	}

	var candidates []naraWithLastSeen

	network.local.mu.Lock()
	for _, nara := range network.Neighbourhood {
		// Skip self, target, and already mentioned
		if nara.Name == network.meName() || nara.Name == exclude {
			continue
		}
		if alreadyMentioned[nara.Name] {
			continue
		}

		obs := network.local.getObservationLocked(nara.Name)
		candidates = append(candidates, naraWithLastSeen{
			name:     nara.Name,
			lastSeen: obs.LastSeen,
			online:   obs.isOnline(),
		})
	}
	network.local.mu.Unlock()

	// Sort: online first, then by least-recently-active (oldest LastSeen first)
	sort.Slice(candidates, func(i, j int) bool {
		// Online naras come first
		if candidates[i].online != candidates[j].online {
			return candidates[i].online
		}
		// Then sort by LastSeen (ascending = least recently active first)
		return candidates[i].lastSeen < candidates[j].lastSeen
	})

	// Take up to maxNeighbors
	if len(candidates) > maxNeighbors {
		candidates = candidates[:maxNeighbors]
	}

	// Build result
	var result []NeighborInfo
	network.local.mu.Lock()
	for _, c := range candidates {
		nara, ok := network.Neighbourhood[c.name]
		if !ok {
			continue
		}
		nara.mu.Lock()
		info := NeighborInfo{
			Name:        nara.Name,
			PublicKey:   nara.Status.PublicKey,
			MeshIP:      nara.Status.MeshIP,
			ID:          nara.Status.ID,
			Observation: network.local.getObservationLocked(nara.Name),
		}
		nara.mu.Unlock()
		result = append(result, info)
	}
	network.local.mu.Unlock()

	return result
}

// truncateKey returns first 8 chars of a key for logging
func truncateKey(key string) string {
	if len(key) > 8 {
		return key[:8]
	}
	return key
}

// heyThere broadcasts identity via MQTT as a signed SyncEvent.
// This publishes the same SyncEvent format used in gossip, ensuring
// consistent attestation across all distribution channels.
func (network *Network) heyThere() {
	if network.ReadOnly {
		return
	}

	// Always record our own online observation (needed for local state)
	network.recordObservationOnlineNara(network.meName(), 0)

	ts := int64(5) // seconds
	if (time.Now().Unix() - network.LastHeyThere) <= ts {
		return
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
			pubKey, err = ParsePublicKey(chau.PublicKey)
			if err != nil {
				logrus.Warnf("⚠️  Invalid public key in chau from %s: %v", chau.From, err)
				return
			}
		} else {
			pubKey = network.resolvePublicKeyForNara(chau.From)
		}
		if pubKey != nil && !syncEvent.Verify(pubKey) {
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
		existingNara.Status.PublicKey = chau.PublicKey
	}
	if present && chau.ID != "" {
		existingNara.Status.ID = chau.ID
		existingNara.ID = chau.ID
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

func (network *Network) oldestNaraBarrio() Nara {
	result := *network.local.Me
	oldest := int64(network.local.getMeObservation().StartTime)
	myClusterName := network.local.getMeObservation().ClusterName
	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	for name, nara := range network.Neighbourhood {
		// question: do we follow our opinion of their neighbourhood or their opinion?
		obs := network.local.getObservationLocked(name)
		if obs.Online != "ONLINE" {
			continue
		}
		if obs.ClusterName != myClusterName {
			continue
		}
		if oldest <= obs.StartTime && name > result.Name {
			continue
		}
		if obs.StartTime > 0 && obs.StartTime <= oldest {
			oldest = obs.StartTime
			result = *nara
		}
	}
	return result
}

func (network *Network) oldestNara() Nara {
	result := *network.local.Me
	oldest := int64(network.local.getMeObservation().StartTime)

	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	for name, nara := range network.Neighbourhood {
		obs := network.local.getObservationLocked(name)
		if obs.Online != "ONLINE" {
			continue
		}
		if oldest <= obs.StartTime && name > result.Name {
			continue
		}
		if obs.StartTime > 0 && obs.StartTime <= oldest {
			oldest = obs.StartTime
			result = *nara
		}
	}
	return result
}

func (network *Network) youngestNaraBarrio() Nara {
	result := *network.local.Me
	youngest := int64(network.local.getMeObservation().StartTime)
	myClusterName := network.local.getMeObservation().ClusterName

	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	for name, nara := range network.Neighbourhood {
		obs := network.local.getObservationLocked(name)
		if obs.Online != "ONLINE" {
			continue
		}
		if obs.ClusterName != myClusterName {
			continue
		}
		if youngest >= obs.StartTime && name < result.Name {
			continue
		}
		if obs.StartTime > 0 && obs.StartTime >= youngest {
			youngest = obs.StartTime
			result = *nara
		}
	}
	return result
}

func (network *Network) youngestNara() Nara {
	result := *network.local.Me
	youngest := int64(network.local.getMeObservation().StartTime)

	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	for name, nara := range network.Neighbourhood {
		obs := network.local.getObservationLocked(name)
		if obs.Online != "ONLINE" {
			continue
		}
		if youngest >= obs.StartTime && name < result.Name {
			continue
		}
		if obs.StartTime > 0 && obs.StartTime >= youngest {
			youngest = obs.StartTime
			result = *nara
		}
	}
	return result
}

func (network *Network) mostRestarts() Nara {
	result := *network.local.Me
	most_restarts := network.local.getMeObservation().Restarts

	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	for name, nara := range network.Neighbourhood {
		obs := network.local.getObservationLocked(name)
		if obs.Online != "ONLINE" {
			continue
		}
		if most_restarts >= obs.Restarts && name > result.Name {
			continue
		}
		if obs.Restarts > 0 && obs.Restarts >= most_restarts {
			most_restarts = obs.Restarts
			result = *nara
		}
	}
	return result
}

func (network *Network) NeighbourhoodNames() []string {
	var result []string
	network.local.mu.Lock()
	defer network.local.mu.Unlock()
	for _, nara := range network.Neighbourhood {
		result = append(result, nara.Name)
	}
	return result
}

func (network *Network) NeighbourhoodOnlineNames() []string {
	var result []string
	network.local.mu.Lock()
	defer network.local.mu.Unlock()
	for _, nara := range network.Neighbourhood {
		obs := network.local.getObservationLocked(nara.Name)
		if !obs.isOnline() {
			continue
		}
		result = append(result, nara.Name)
	}
	return result
}

func (network *Network) getNara(name string) Nara {
	network.local.mu.Lock()
	nara, present := network.Neighbourhood[name]
	network.local.mu.Unlock()
	if present {
		return *nara
	}
	return Nara{}
}

func (network *Network) importNara(nara *Nara) {
	nara.mu.Lock()
	defer nara.mu.Unlock()

	// deadlock prevention: ensure we always lock in the same order
	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	n, present := network.Neighbourhood[nara.Name]
	if present {
		n.setValuesFrom(*nara)
	} else {
		network.Neighbourhood[nara.Name] = nara
	}
}

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
		if pubKey != nil && !event.Verify(pubKey) {
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

// SSE client management

func (network *Network) subscribeSSE() chan SyncEvent {
	ch := make(chan SyncEvent, 10) // buffered to prevent blocking
	network.sseClientsMu.Lock()
	network.sseClients[ch] = true
	network.sseClientsMu.Unlock()
	return ch
}

func (network *Network) unsubscribeSSE(ch chan SyncEvent) {
	network.sseClientsMu.Lock()
	delete(network.sseClients, ch)
	network.sseClientsMu.Unlock()
	// Don't close the channel - a broadcast already in-flight might still
	// have a reference and would panic on send. Let GC collect it once
	// the SSE handler returns and stops selecting on it.
}

func (network *Network) broadcastSSE(event SyncEvent) {
	network.sseClientsMu.RLock()
	defer network.sseClientsMu.RUnlock()

	for ch := range network.sseClients {
		select {
		case ch <- event:
		default:
			// Client too slow, skip this event for them
		}
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
	var delay time.Duration
	if network.testTeaseDelay != nil {
		delay = *network.testTeaseDelay
	} else {
		delay = time.Duration(rand.Intn(5)) * time.Second
	}

	select {
	case <-time.After(delay):
		// Continue to check and potentially tease
	case <-network.ctx.Done():
		// Shutdown initiated, don't tease
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

// --- Ledger Gossip and Boot Recovery ---

func (network *Network) processLedgerRequests() {
	for {
		select {
		case req := <-network.ledgerRequestInbox:
			network.handleLedgerRequest(req)
		case <-network.ctx.Done():
			logrus.Debug("processLedgerRequests: shutting down")
			return
		}
	}
}

func (network *Network) handleLedgerRequest(req LedgerRequest) {
	if network.ReadOnly {
		return
	}

	// Get events for requested subjects from our ledger
	events := network.local.SyncLedger.GetSocialEventsForSubjects(req.Subjects)

	// Respond directly to the requester
	response := LedgerResponse{
		From:   network.meName(),
		Events: events,
	}

	topic := fmt.Sprintf("nara/ledger/%s/response", req.From)
	network.postEvent(topic, response)
	logrus.Infof("📤 sent %d events to %s", len(events), req.From)
}

func (network *Network) processLedgerResponses() {
	for {
		select {
		case resp := <-network.ledgerResponseInbox:
			network.handleLedgerResponse(resp)
		case <-network.ctx.Done():
			logrus.Debug("processLedgerResponses: shutting down")
			return
		}
	}
}

func (network *Network) handleLedgerResponse(resp LedgerResponse) {
	// Merge received events into our ledger (with personality filtering)
	added := network.local.SyncLedger.MergeSocialEventsFiltered(resp.Events, network.local.Me.Status.Personality)
	if added > 0 && network.logService != nil {
		network.logService.BatchGossipMerge(resp.From, added)
	}
}

// getNeighborsForBootRecovery returns online neighbors.
// Only re-discovers if no peers are known (peers are typically discovered at connect time in gossip mode).
func (network *Network) getNeighborsForBootRecovery() []string {
	online := network.NeighbourhoodOnlineNames()

	// In gossip-only mode, only re-discover if we have no peers
	// (peers are normally discovered immediately after tsnet connects)
	if len(online) == 0 && network.TransportMode == TransportGossip {
		logrus.Debug("📡 Boot recovery: no peers known, triggering mesh discovery...")
		if network.tsnetMesh != nil {
			network.discoverMeshPeers()
			online = network.NeighbourhoodOnlineNames()
		}
	}

	return online
}

// bootRecovery requests social events from neighbors after boot
func (network *Network) bootRecovery() {
	// Signal completion when done (allows formOpinion to proceed)
	defer func() {
		close(network.bootRecoveryDone)
		logrus.Debug("📦 boot recovery complete, signaling formOpinion to proceed")
	}()

	// In gossip mode, check if peers are already discovered (from immediate discovery at connect)
	// If so, skip the 30s wait and start syncing right away
	var online []string
	if network.TransportMode == TransportGossip {
		online = network.NeighbourhoodOnlineNames()
		if len(online) > 0 {
			logrus.Printf("📦 Gossip mode: %d peers already discovered, starting boot recovery immediately", len(online))
		}
	}

	// Wait for initial neighbor discovery (only if we don't have peers yet)
	if len(online) == 0 {
		select {
		case <-time.After(30 * time.Second):
			// continue
		case <-network.ctx.Done():
			return
		}

		// Retry up to 3 times with backoff if no neighbors found
		for attempt := 0; attempt < 3; attempt++ {
			online = network.getNeighborsForBootRecovery()
			if len(online) > 0 {
				break
			}
			select {
			case <-network.ctx.Done():
				return
			default:
			}
			if attempt < 2 {
				waitTime := time.Duration(30*(attempt+1)) * time.Second
				logrus.Printf("📦 no neighbors for boot recovery, retrying in %v...", waitTime)
				select {
				case <-time.After(waitTime):
					// continue
				case <-network.ctx.Done():
					return
				}
			}
		}
	}

	if len(online) == 0 {
		logrus.Printf("📦 no neighbors for boot recovery after retries")
		return
	}

	// Try mesh HTTP recovery first
	if network.tsnetMesh != nil {
		network.bootRecoveryViaMesh(online)
	} else {
		// Fall back to MQTT-based recovery
		network.bootRecoveryViaMQTT(online)
	}

	// After regular boot recovery, sync checkpoint timeline from network
	// This recovers the full historical record of the network
	if network.tsnetMesh != nil {
		network.syncCheckpointsFromNetwork(online)
	}
}

// BootRecoveryTargetEvents is the target number of events to fetch on boot
const BootRecoveryTargetEvents = 50000

// bootRecoveryViaMesh uses direct HTTP to sync events from neighbors (parallelized)
func (network *Network) bootRecoveryViaMesh(online []string) {
	// Get all known subjects (naras)
	subjects := append(online, network.meName())

	// Collect ALL mesh-enabled neighbors (no limit)
	var meshNeighbors []struct {
		name string
		ip   string
	}

	maxMeshNeighbors := len(online)
	if network.isShortMemoryMode() {
		maxMeshNeighbors = 4
	}
	for _, name := range online {
		ip := network.getMeshIPForNara(name)
		if ip != "" {
			meshNeighbors = append(meshNeighbors, struct {
				name string
				ip   string
			}{name, ip})
		}
	}
	if len(meshNeighbors) > maxMeshNeighbors {
		meshNeighbors = meshNeighbors[:maxMeshNeighbors]
	}

	if len(meshNeighbors) == 0 {
		logrus.Printf("📦 no mesh-enabled neighbors for boot recovery, falling back to MQTT")
		network.bootRecoveryViaMQTT(online)
		return
	}

	totalSlices := len(meshNeighbors)
	// Divide target across neighbors
	eventsPerNeighbor := BootRecoveryTargetEvents / totalSlices
	if eventsPerNeighbor < 100 {
		eventsPerNeighbor = 100 // minimum events per neighbor
	}

	logrus.Printf("📦 boot recovery via mesh: syncing from %d neighbors in parallel (~%d events each)", totalSlices, eventsPerNeighbor)

	// Use tsnet HTTP client to route through Tailscale
	client := network.getMeshHTTPClient()
	if client == nil {
		logrus.Warnf("📦 mesh HTTP client unavailable for boot recovery")
		return
	}

	// Fetch from all neighbors in parallel
	type syncResult struct {
		name         string
		sliceIndex   int
		events       []SyncEvent
		respVerified bool
		success      bool
	}
	results := make(chan syncResult, len(meshNeighbors))
	var wg sync.WaitGroup

	// Limit concurrent requests to avoid overwhelming the network
	maxConcurrent := 10
	if network.isShortMemoryMode() {
		maxConcurrent = 3
	}
	sem := make(chan struct{}, maxConcurrent)

	for i, neighbor := range meshNeighbors {
		wg.Add(1)
		sem <- struct{}{} // Acquire semaphore

		go func(idx int, n struct{ name, ip string }) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore

			events, respVerified := network.fetchSyncEventsFromMesh(client, n.ip, n.name, subjects, idx, totalSlices, eventsPerNeighbor)
			results <- syncResult{
				name:         n.name,
				sliceIndex:   idx,
				events:       events,
				respVerified: respVerified,
				success:      len(events) > 0 || respVerified, // success if we got events or at least verified (empty slice is OK)
			}
		}(i, neighbor)
	}

	// Close results channel when all fetches complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Process results as they arrive (merging is thread-safe)
	var totalMerged int
	var failedSlices []int
	respondedNeighbors := make(map[string]bool)

	for result := range results {
		respondedNeighbors[result.name] = result.success
		if len(result.events) > 0 {
			added, warned := network.MergeSyncEventsWithVerification(result.events)
			totalMerged += added
			if added > 0 && network.logService != nil {
				network.logService.BatchMeshSync(result.name, added)
			}
			if warned > 0 {
				logrus.Debugf("📦 mesh sync from %s: %d events with %d verification warnings", result.name, added, warned)
			}
		} else if !result.success {
			// Track failed slices for retry
			failedSlices = append(failedSlices, result.sliceIndex)
			logrus.Printf("📦 mesh sync from %s failed (slice %d), will retry with another neighbor", result.name, result.sliceIndex)
		}
	}

	// Retry failed slices with different neighbors
	if len(failedSlices) > 0 {
		// Find neighbors that succeeded (they're available for retry)
		var availableNeighbors []struct{ name, ip string }
		for _, n := range meshNeighbors {
			if respondedNeighbors[n.name] {
				availableNeighbors = append(availableNeighbors, n)
			}
		}

		if len(availableNeighbors) > 0 {
			logrus.Printf("📦 retrying %d failed slices with %d available neighbors", len(failedSlices), len(availableNeighbors))

			retryResults := make(chan syncResult, len(failedSlices))
			var retryWg sync.WaitGroup

			for i, sliceIdx := range failedSlices {
				// Pick a different neighbor for each failed slice (round-robin)
				neighbor := availableNeighbors[i%len(availableNeighbors)]

				retryWg.Add(1)
				sem <- struct{}{}

				go func(idx int, n struct{ name, ip string }) {
					defer retryWg.Done()
					defer func() { <-sem }()

					logrus.Printf("📦 retry: asking %s for slice %d", n.name, idx)
					events, respVerified := network.fetchSyncEventsFromMesh(client, n.ip, n.name, subjects, idx, totalSlices, eventsPerNeighbor)
					retryResults <- syncResult{
						name:         n.name,
						sliceIndex:   idx,
						events:       events,
						respVerified: respVerified,
						success:      len(events) > 0,
					}
				}(sliceIdx, neighbor)
			}

			go func() {
				retryWg.Wait()
				close(retryResults)
			}()

			for result := range retryResults {
				if len(result.events) > 0 {
					added, warned := network.MergeSyncEventsWithVerification(result.events)
					totalMerged += added
					if added > 0 && network.logService != nil {
						network.logService.BatchMeshSync(result.name, added)
					}
					if warned > 0 {
						logrus.Debugf("📦 retry mesh sync from %s: %d events with %d verification warnings", result.name, added, warned)
					}
				} else {
					logrus.Debugf("📦 retry mesh sync from %s (slice %d) failed", result.name, result.sliceIndex)
				}
			}
		} else {
			logrus.Debugf("📦 no available neighbors for retry, %d slices remain unsynced", len(failedSlices))
		}
	}

	if network.logService != nil {
		network.logService.Info(CategoryMesh, "boot recovery complete: %d events total", totalMerged)
	}

	// Seed AvgPingRTT from recovered ping observations
	network.seedAvgPingRTTFromHistory()
}

// TODO: Add integration test for fetchSyncEventsFromMesh using httptest.NewServer pattern
// See TestIntegration_CheckpointSync for reference on how to inject HTTP mux/client
// This should test the full HTTP request/response flow for sync event fetching,
// including signature verification, slice-based fetching, and error handling
//
// fetchSyncEventsFromMesh fetches unified SyncEvents with signature verification
func (network *Network) fetchSyncEventsFromMesh(client *http.Client, meshIP, name string, subjects []string, sliceIndex, sliceTotal, maxEvents int) ([]SyncEvent, bool) {
	// Build request
	reqBody := SyncRequest{
		From:       network.meName(),
		Subjects:   subjects,
		SinceTime:  0, // get all events
		SliceIndex: sliceIndex,
		SliceTotal: sliceTotal,
		MaxEvents:  maxEvents,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		logrus.Warnf("📦 failed to marshal mesh sync request: %v", err)
		return nil, false
	}

	// Make HTTP request to neighbor's mesh endpoint
	url := network.buildMeshURLFromIP(meshIP, "/events/sync")
	// Boot sync requests are batched via the summary log, not individual lines
	req, err := http.NewRequest("POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		logrus.Warnf("📦 failed to create mesh sync request: %v", err)
		return nil, false
	}
	req.Header.Set("Content-Type", "application/json")

	// Add mesh authentication headers (Ed25519 signature)
	network.AddMeshAuthHeaders(req)

	resp, err := client.Do(req)
	if err != nil {
		logrus.Warnf("📦 mesh sync from %s failed: %v", name, err)
		return nil, false
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		logrus.Warnf("📦 mesh sync from %s rejected our auth (they may not know us yet)", name)
		return nil, false
	}

	if resp.StatusCode != http.StatusOK {
		logrus.Warnf("📦 mesh sync from %s returned status %d", name, resp.StatusCode)
		return nil, false
	}

	// Read and verify response body
	body, verified := network.VerifyMeshResponseBody(resp)
	if !verified {
		logrus.Warnf("📦 mesh response from %s failed signature verification", name)
		// Continue anyway - response might be valid but from a nara we don't know yet
	}

	// Parse response
	var response SyncResponse
	if err := json.Unmarshal(body, &response); err != nil {
		logrus.Warnf("📦 failed to decode mesh sync response from %s: %v", name, err)
		return nil, false
	}

	// Also verify the inner signature for extra assurance
	if response.Signature != "" && !verified {
		// Look up sender's public key from our neighborhood
		// Note: Short-circuit evaluation ensures nara.Status is only accessed if nara != nil
		network.local.mu.Lock()
		nara := network.Neighbourhood[name]
		network.local.mu.Unlock()
		if nara != nil {
			nara.mu.Lock()
			publicKey := nara.Status.PublicKey
			nara.mu.Unlock()
			if publicKey != "" {
				pubKey, err := ParsePublicKey(publicKey)
				if err == nil {
					if response.VerifySignature(pubKey) {
						verified = true
					} else {
						logrus.Warnf("📦 inner signature verification failed for %s", name)
					}
				}
			}
		}
	}

	return response.Events, verified
}

// bootRecoveryViaMQTT uses MQTT ledger requests to sync events (fallback)
func (network *Network) bootRecoveryViaMQTT(online []string) {
	// Get all known subjects (naras)
	subjects := append(online, network.meName())

	// Pick up to 5 neighbors to query
	maxNeighbors := 5
	if network.isShortMemoryMode() {
		maxNeighbors = 2
	}
	if len(online) < maxNeighbors {
		maxNeighbors = len(online)
	}

	// Partition subjects across neighbors
	partitions := PartitionSubjects(subjects, maxNeighbors)

	logrus.Printf("📦 boot recovery via MQTT: requesting events from %d neighbors", maxNeighbors)

	for i := 0; i < maxNeighbors; i++ {
		neighbor := online[i]
		partition := partitions[i]

		if len(partition) == 0 {
			continue
		}

		req := LedgerRequest{
			From:     network.meName(),
			Subjects: partition,
		}

		topic := fmt.Sprintf("nara/ledger/%s/request", neighbor)
		network.postEvent(topic, req)
		logrus.Infof("📦 requested events about %d subjects from %s", len(partition), neighbor)
	}
}

// RequestLedgerSync manually triggers a sync request to a specific neighbor
func (network *Network) RequestLedgerSync(neighbor string, subjects []string) {
	if network.ReadOnly {
		return
	}

	req := LedgerRequest{
		From:     network.meName(),
		Subjects: subjects,
	}

	topic := fmt.Sprintf("nara/ledger/%s/request", neighbor)
	network.postEvent(topic, req)
}

// seedAvgPingRTTFromHistory initializes AvgPingRTT from historical ping observations
// Called after boot recovery to seed exponential moving average with recovered data
func (network *Network) seedAvgPingRTTFromHistory() {
	if network.local.SyncLedger == nil {
		return
	}

	// Get all ping observations from the ledger
	allPings := network.local.SyncLedger.GetPingObservations()

	// Group pings by target to seed our observations
	// We use ALL pings (from any observer) to get initial RTT estimates
	// This means we learn from the network: if B→C has 50ms RTT, we seed our C observation with that
	myName := network.meName()
	pingsByTarget := make(map[string][]float64)

	for _, ping := range allPings {
		// Skip pings TO us (we care about targets we might ping, not pings to us)
		if ping.Target != myName {
			pingsByTarget[ping.Target] = append(pingsByTarget[ping.Target], ping.RTT)
		}
	}

	// Seed AvgPingRTT for each target
	seededCount := 0
	for target, rtts := range pingsByTarget {
		obs := network.local.getObservation(target)

		// Only seed if AvgPingRTT is not already set (0 means uninitialized)
		if obs.AvgPingRTT == 0 && len(rtts) > 0 {
			// Calculate simple average from historical pings
			sum := 0.0
			for _, rtt := range rtts {
				sum += rtt
			}
			avg := sum / float64(len(rtts))

			obs.AvgPingRTT = avg
			network.local.setObservation(target, obs)
			seededCount++
		}
	}

	if seededCount > 0 {
		logrus.Printf("📍 Seeded AvgPingRTT for %d targets from historical ping observations", seededCount)
	}
}

// syncCheckpointsFromNetwork fetches checkpoint history from random online naras
// This recovers the full network timeline after boot recovery completes
// Keeps trying naras until 5 successful responses or all naras exhausted
func (network *Network) syncCheckpointsFromNetwork(online []string) {
	if len(online) == 0 {
		logrus.Debug("📸 No online naras to sync checkpoints from")
		return
	}

	// Shuffle all online naras to randomize selection
	shuffled := make([]string, len(online))
	copy(shuffled, online)
	for i := range shuffled {
		j := i + int(time.Now().UnixNano()%(int64(len(shuffled)-i)))
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}

	const targetSuccessfulFetches = 5
	successfulFetches := 0
	attemptedNaras := []string{}
	totalMerged := 0
	totalWarned := 0

	// Try naras until we get 5 successful fetches or run out of candidates
	for _, naraName := range shuffled {
		if successfulFetches >= targetSuccessfulFetches {
			break
		}

		attemptedNaras = append(attemptedNaras, naraName)

		// Get the nara's IP address
		var ip string
		network.local.mu.Lock()
		if nara, exists := network.Neighbourhood[naraName]; exists {
			nara.mu.Lock()
			ip = nara.Status.MeshIP
			nara.mu.Unlock()
		}
		network.local.mu.Unlock()

		if ip == "" {
			logrus.Debugf("📸 %s: no mesh IP, skipping", naraName)
			continue
		}

		// Fetch all checkpoints from this nara (handles pagination internally)
		checkpoints := network.fetchAllCheckpointsFromNara(naraName, ip)
		if len(checkpoints) == 0 {
			logrus.Debugf("📸 %s: no checkpoints returned, trying next nara", naraName)
			continue
		}

		// Merge into our ledger using the same pattern as zine gossip
		// This handles signature verification and triggers projection updates
		added, warned := network.MergeSyncEventsWithVerification(checkpoints)

		logrus.Printf("📸 %s: fetched %d checkpoints, merged %d new ones", naraName, len(checkpoints), added)
		totalMerged += added
		totalWarned += warned
		successfulFetches++
	}

	if totalMerged > 0 {
		logrus.Printf("📸 Checkpoint sync complete: %d new checkpoints from %d/%d naras (attempted: %v)",
			totalMerged, successfulFetches, len(attemptedNaras), attemptedNaras)
		if totalWarned > 0 {
			logrus.Warnf("📸 Warning: %d checkpoints had signature verification issues", totalWarned)
		}
	} else {
		logrus.Debugf("📸 Checkpoint sync complete: no new checkpoints (attempted %d naras: %v)",
			len(attemptedNaras), attemptedNaras)
	}
}

// fetchAllCheckpointsFromNara fetches all checkpoint events from a remote nara via HTTP
// Handles pagination automatically to retrieve the complete checkpoint history
func (network *Network) fetchAllCheckpointsFromNara(naraName, ip string) []SyncEvent {
	// Allow tests to work without tsnetMesh if testHTTPClient is set
	if network.tsnetMesh == nil && network.testHTTPClient == nil {
		return nil
	}

	client := network.getMeshHTTPClient()
	if client == nil {
		return nil
	}

	var allCheckpoints []SyncEvent
	offset := 0
	limit := 1000 // fetch in batches of 1000

	for {
		// Build URL with pagination parameters using mesh helper (handles test vs production IPs)
		url := network.buildMeshURLFromIP(ip, fmt.Sprintf("/api/checkpoints/all?limit=%d&offset=%d", limit, offset))

		// Create request with timeout
		ctx, cancel := context.WithTimeout(network.ctx, 10*time.Second)
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			cancel()
			logrus.Debugf("📸 %s: failed to create request: %v", naraName, err)
			break
		}

		resp, err := client.Do(req)
		if err != nil {
			cancel()
			logrus.Debugf("📸 %s: failed to fetch checkpoints: %v", naraName, err)
			break
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			cancel()
			logrus.Debugf("📸 %s: bad status: %d", naraName, resp.StatusCode)
			break
		}

		// Parse response
		var response struct {
			Server      string       `json:"server"`
			Total       int          `json:"total"`
			Count       int          `json:"count"`
			Checkpoints []*SyncEvent `json:"checkpoints"`
			HasMore     bool         `json:"has_more"`
			Offset      int          `json:"offset"`
			Limit       int          `json:"limit"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			resp.Body.Close()
			cancel()
			logrus.Debugf("📸 %s: failed to decode response: %v", naraName, err)
			break
		}
		resp.Body.Close()
		cancel()

		// Convert pointers to values for MergeSyncEventsWithVerification
		for _, cp := range response.Checkpoints {
			if cp != nil {
				allCheckpoints = append(allCheckpoints, *cp)
			}
		}

		// Check if there are more pages
		if !response.HasMore {
			break
		}

		offset += limit
	}

	return allCheckpoints
}

// backfillObservations migrates existing observations to observation events
// This enables smooth transition from newspaper-based consensus to event-based
// Called directly from formOpinion() after opinions are formed
func (network *Network) backfillObservations() {
	myName := network.meName()
	backfillCount := 0

	// Lock Me.mu to safely read Me.Status.Observations
	network.local.Me.mu.Lock()
	observations := make(map[string]NaraObservation)
	for name, obs := range network.local.Me.Status.Observations {
		observations[name] = obs
	}
	network.local.Me.mu.Unlock()

	logrus.Printf("📦 Checking if backfill needed for %d observations...", len(observations))

	for naraName, obs := range observations {
		if naraName == myName {
			continue // Skip self
		}

		// Check if we already have a backfill event for this nara
		existingEvents := network.local.SyncLedger.GetObservationEventsAbout(naraName)
		hasBackfill := false
		for _, e := range existingEvents {
			if e.Observation != nil && e.Observation.IsBackfill {
				hasBackfill = true
				break
			}
		}
		if hasBackfill {
			// Already have backfill baseline, skip
			continue
		}

		// No backfill exists yet, but we have newspaper-based knowledge
		// Create backfill event if data is meaningful
		if obs.StartTime > 0 && obs.Restarts >= 0 {
			event := NewBackfillObservationEvent(
				myName,
				naraName,
				obs.StartTime,
				obs.Restarts,
				obs.LastRestart,
			)

			// Add with full anti-abuse protections
			added := network.local.SyncLedger.AddEventWithDedup(event)
			if added {
				backfillCount++
				logrus.Infof("📦 Backfilled observation for %s (start:%d, restarts:%d)",
					naraName, obs.StartTime, obs.Restarts)
			}
		}
	}

	if backfillCount > 0 {
		logrus.Printf("📦 Backfilled %d historical observations into event system", backfillCount)
		if network.local.Projections != nil {
			network.local.Projections.Trigger()
		}
	} else {
		logrus.Printf("📦 No backfill needed (events already present or no meaningful data)")
	}
}

// createZine creates a zine (batch of recent events) to share with neighbors
// Returns nil if no events to share or if SyncLedger unavailable
func (network *Network) createZine() *Zine {
	if network.local.SyncLedger == nil {
		return nil
	}

	// Get events from last 5 minutes
	cutoff := time.Now().Add(-5 * time.Minute).UnixNano()
	allEvents := network.local.SyncLedger.GetAllEvents()

	var recentEvents []SyncEvent
	for _, e := range allEvents {
		if e.Timestamp >= cutoff {
			recentEvents = append(recentEvents, e)
		}
	}

	if len(recentEvents) == 0 {
		return nil // Nothing to share
	}

	zine := &Zine{
		From:      network.meName(),
		CreatedAt: time.Now().Unix(),
		Events:    recentEvents,
	}

	// Sign the zine for authenticity
	sig, err := SignZine(zine, network.local.Keypair)
	if err != nil {
		logrus.Warnf("📰 Failed to sign zine: %v", err)
		return nil
	}
	zine.Signature = sig

	return zine
}

// SignZine computes the signature for a zine
func SignZine(z *Zine, keypair NaraKeypair) (string, error) {
	if len(keypair.PrivateKey) == 0 {
		return "", fmt.Errorf("no private key available")
	}

	// Create signing data (from + timestamp + event IDs)
	hasher := sha256.New()
	hasher.Write([]byte(fmt.Sprintf("%s:%d:", z.From, z.CreatedAt)))
	for _, e := range z.Events {
		hasher.Write([]byte(e.ID))
	}

	signingData := hasher.Sum(nil)
	return keypair.SignBase64(signingData), nil
}

// VerifyZine verifies a zine's signature
func VerifyZine(z *Zine, publicKey ed25519.PublicKey) bool {
	if z.Signature == "" || len(publicKey) == 0 {
		return false
	}

	// Recompute signing data
	hasher := sha256.New()
	hasher.Write([]byte(fmt.Sprintf("%s:%d:", z.From, z.CreatedAt)))
	for _, e := range z.Events {
		hasher.Write([]byte(e.ID))
	}

	signingData := hasher.Sum(nil)

	// Decode signature
	sig, err := base64.StdEncoding.DecodeString(z.Signature)
	if err != nil {
		return false
	}

	return ed25519.Verify(publicKey, signingData, sig)
}

// selectGossipTargets selects random mesh-enabled neighbors for gossip
// Returns 3-5 random online naras with mesh connectivity
func (network *Network) selectGossipTargets() []string {
	online := network.NeighbourhoodOnlineNames()

	// Filter to mesh-enabled only
	var meshEnabled []string
	for _, name := range online {
		if network.hasMeshConnectivity(name) {
			meshEnabled = append(meshEnabled, name)
		}
	}

	if len(meshEnabled) == 0 {
		return nil
	}

	// Select 3-5 random targets (short memory: 1-2)
	targetCount := 3 + rand.Intn(3) // Random between 3-5
	if network.isShortMemoryMode() {
		targetCount = 1 + rand.Intn(2)
	}
	if targetCount > len(meshEnabled) {
		targetCount = len(meshEnabled)
	}

	// Shuffle and take first N
	shuffled := make([]string, len(meshEnabled))
	copy(shuffled, meshEnabled)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	return shuffled[:targetCount]
}

// PeerDiscovery is a strategy for discovering mesh peers
// Allows testing without real network scanning
type PeerDiscovery interface {
	// ScanForPeers returns a list of discovered peers with their IPs
	ScanForPeers(myIP string) []DiscoveredPeer
}

// DiscoveredPeer represents a nara found via discovery
type DiscoveredPeer struct {
	Name      string
	MeshIP    string
	PublicKey string // Ed25519 public key (from /ping response)
}

// TailscalePeerDiscovery scans the Tailscale mesh subnet for peers
type TailscalePeerDiscovery struct {
	client *http.Client
}

// ScanForPeers scans 100.64.0.1-254 for naras responding to /ping (parallelized)
func (d *TailscalePeerDiscovery) ScanForPeers(myIP string) []DiscoveredPeer {
	logrus.Debugf("📡 Starting parallel peer scan (myIP=%s)", myIP)

	var peers []DiscoveredPeer
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Use a semaphore to limit concurrent requests (avoid overwhelming the network)
	const maxConcurrent = 50
	sem := make(chan struct{}, maxConcurrent)

	// Scan mesh subnet (100.64.0.0/10 for Tailscale)
	for i := 1; i <= 254; i++ {
		ip := fmt.Sprintf("100.64.0.%d", i)

		// Skip our own IP
		if ip == myIP {
			continue
		}

		wg.Add(1)
		sem <- struct{}{} // Acquire semaphore

		go func(ip string) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore

			// Try to ping this IP
			url := fmt.Sprintf("http://%s:%d/ping", ip, DefaultMeshPort)
			resp, err := d.client.Get(url)
			if err != nil {
				return // Not a nara or unreachable
			}

			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				return
			}

			// Decode to get the nara's name
			var pingResp map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&pingResp); err != nil {
				resp.Body.Close()
				return
			}
			resp.Body.Close()

			naraName, ok := pingResp["from"].(string)
			if !ok || naraName == "" {
				return
			}

			// Extract public key if present
			pubKey, _ := pingResp["public_key"].(string)

			mu.Lock()
			peers = append(peers, DiscoveredPeer{
				Name:      naraName,
				MeshIP:    ip,
				PublicKey: pubKey,
			})
			mu.Unlock()
		}(ip)
	}

	wg.Wait()
	logrus.Debugf("📡 Parallel peer scan complete: found %d peers", len(peers))
	return peers
}

// discoverMeshPeers discovers peers and fetches their public keys
// This replaces MQTT-based discovery in gossip-only mode
// Note: Does NOT bootstrap from peers - that's handled by bootRecovery
func (network *Network) discoverMeshPeers() {
	if network.tsnetMesh == nil {
		return
	}

	myName := network.meName()
	logrus.Debugf("📡 Mesh discovery starting: myName=%s", myName)

	// Try Status API first (instant, no network scanning)
	var peers []DiscoveredPeer
	ctx, cancel := context.WithTimeout(network.ctx, 5*time.Second)
	defer cancel()

	tsnetPeers, err := network.tsnetMesh.Peers(ctx)
	if err != nil {
		logrus.Debugf("📡 Status API failed, falling back to IP scan: %v", err)
		// Fall back to IP scanning if Status API fails (includes public keys)
		if network.peerDiscovery != nil {
			peers = network.peerDiscovery.ScanForPeers(network.tsnetMesh.IP())
		}
	} else {
		// Convert TsnetPeer to DiscoveredPeer (no public keys yet)
		for _, p := range tsnetPeers {
			peers = append(peers, DiscoveredPeer{Name: p.Name, MeshIP: p.IP})
		}
		logrus.Infof("📡 Got %d peers from tsnet Status API (instant!)", len(peers))

		// Fetch public keys from peers in parallel
		peers = network.fetchPublicKeysFromPeers(peers)
	}

	discovered := 0
	for _, peer := range peers {
		// Skip self
		if peer.Name == myName {
			continue
		}

		// Check if we already know this nara
		network.local.mu.Lock()
		existing, exists := network.Neighbourhood[peer.Name]
		network.local.mu.Unlock()

		if !exists {
			// New peer - add to neighborhood with public key
			nara := NewNara(peer.Name)
			nara.Status.MeshIP = peer.MeshIP
			nara.Status.MeshEnabled = true
			nara.Status.PublicKey = peer.PublicKey
			network.importNara(nara)
			network.recordObservationOnlineNara(peer.Name, 0) // Properly sets both Online and LastSeen
			network.emitSeenEvent(peer.Name, "mesh")
			discovered++
			if peer.PublicKey != "" {
				logrus.Infof("📡 Discovered mesh peer: %s at %s (🔑)", peer.Name, peer.MeshIP)
			} else {
				logrus.Infof("📡 Discovered mesh peer: %s at %s (no key yet)", peer.Name, peer.MeshIP)
			}
		} else if peer.PublicKey != "" && existing.Status.PublicKey == "" {
			// Update existing peer with newly discovered public key
			existing.Status.PublicKey = peer.PublicKey
			logrus.Infof("📡 Updated public key for %s (🔑)", peer.Name)
		}
	}

	if discovered > 0 {
		logrus.Printf("📡 Mesh discovery complete: found %d new peers", discovered)
	}
}

// TODO: Add integration test for fetchPublicKeysFromPeers using httptest.NewServer pattern
// See TestIntegration_CheckpointSync for reference on how to inject HTTP mux/client
// This should test parallel fetching of public keys from multiple peers via HTTP
//
// fetchPublicKeysFromPeers pings peers in parallel to get their public keys
// This is necessary because tsnet Status API only gives us names and IPs
func (network *Network) fetchPublicKeysFromPeers(peers []DiscoveredPeer) []DiscoveredPeer {
	if network.tsnetMesh == nil || network.tsnetMesh.Server() == nil {
		return peers
	}

	client := network.getMeshHTTPClient()
	if client == nil {
		return peers
	}

	var wg sync.WaitGroup
	var mu sync.Mutex

	// Process in parallel with a semaphore
	const maxConcurrent = 20
	sem := make(chan struct{}, maxConcurrent)

	for i := range peers {
		if peers[i].PublicKey != "" {
			continue // Already has public key
		}

		wg.Add(1)
		sem <- struct{}{} // Acquire semaphore

		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore

			url := network.buildMeshURLFromIP(peers[idx].MeshIP, "/ping")
			ctx, cancel := context.WithTimeout(network.ctx, 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
			if err != nil {
				return
			}

			resp, err := client.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return
			}

			var pingResp map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&pingResp); err != nil {
				return
			}

			mu.Lock()
			// Get the real nara name from the ping response (not Tailscale hostname)
			if name, ok := pingResp["from"].(string); ok && name != "" {
				peers[idx].Name = name
			}
			if pubKey, ok := pingResp["public_key"].(string); ok && pubKey != "" {
				peers[idx].PublicKey = pubKey
			}
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	// Count how many have public keys
	withKeys := 0
	for _, p := range peers {
		if p.PublicKey != "" {
			withKeys++
		}
	}
	logrus.Debugf("📡 Fetched public keys: %d/%d peers have keys", withKeys, len(peers))

	return peers
}

// bootstrapFromDiscoveredPeers fetches initial state from newly discovered peers
// This is the gossip-only equivalent of boot recovery
func (network *Network) bootstrapFromDiscoveredPeers(peers []DiscoveredPeer) {
	if len(peers) == 0 {
		return
	}

	// Check if we have a working mesh client before attempting bootstrap
	// (prevents crashes in tests with mock meshes)
	if network.tsnetMesh == nil || network.tsnetMesh.Server() == nil {
		logrus.Debug("📦 Skipping bootstrap: no mesh client available")
		return
	}

	// Convert peers to online names for boot recovery
	peerNames := make([]string, len(peers))
	for i, peer := range peers {
		peerNames[i] = peer.Name
	}

	logrus.Printf("📦 Bootstrapping from %d discovered peers...", len(peers))

	// Use existing boot recovery mechanism via mesh
	// This will fetch events, sync ledgers, and seed ping RTTs
	network.bootRecoveryViaMesh(peerNames)
}

// meshDiscoveryForever periodically scans for new mesh peers
// Runs in gossip-only or hybrid mode to discover peers without MQTT
func (network *Network) meshDiscoveryForever() {
	// Initial discovery after mesh is ready
	select {
	case <-time.After(35 * time.Second):
		// continue
	case <-network.ctx.Done():
		return
	}
	network.discoverMeshPeers()

	// Periodic re-discovery (every 5 minutes)
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-network.ctx.Done():
			return
		case <-ticker.C:
			network.discoverMeshPeers()
		}
	}
}

// gossipForever periodically exchanges zines with random mesh neighbors
// Runs in background, spreading events organically through the network
func (network *Network) gossipForever() {
	// Wait for mesh to be ready
	select {
	case <-time.After(30 * time.Second):
		// continue
	case <-network.ctx.Done():
		return
	}

	for {
		interval := network.gossipInterval()
		select {
		case <-network.ctx.Done():
			return
		case <-time.After(interval):
			if network.ReadOnly || network.tsnetMesh == nil {
				continue
			}

			// Skip if in MQTT-only mode
			if network.TransportMode == TransportMQTT {
				continue
			}

			network.performGossipRound()
		}
	}
}

// gossipInterval returns the time to wait between gossip rounds
// Personality-based: 30-300 seconds, similar to chattiness
func (network *Network) gossipInterval() time.Duration {
	baseInterval := network.local.chattinessRate(30, 300)
	return time.Duration(baseInterval) * time.Second
}

// performGossipRound creates a zine and exchanges it with random neighbors
func (network *Network) performGossipRound() {
	// Create our zine
	zine := network.createZine()
	if zine == nil {
		logrus.Infof("📰 No events to gossip")
		return
	}

	// Select targets
	targets := network.selectGossipTargets()
	if len(targets) == 0 {
		logrus.Infof("📰 No gossip targets available")
		return
	}

	// Count events by type for diagnostics
	typeCounts := make(map[string]int)
	for _, e := range zine.Events {
		typeCounts[e.Service]++
	}
	logrus.Infof("📰 Gossiping with %d neighbors [%s] (zine has %d events: %v)", len(targets), strings.Join(targets, ", "), len(zine.Events), typeCounts)

	// Exchange zines with each target
	var wg sync.WaitGroup
	for _, targetName := range targets {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			network.exchangeZine(name, zine)
			// Also exchange stashes (rate-limited via stashSyncTracker)
			// Only if we have stash data to share
			if network.stashService != nil && network.stashService.HasStashData() {
				network.exchangeStashWithPeer(name)
			}
		}(targetName)
	}
	wg.Wait()
}

// exchangeZine sends our zine to a neighbor and receives theirs back
func (network *Network) exchangeZine(targetName string, myZine *Zine) {
	// Determine URL
	url := network.buildMeshURL(targetName, "/gossip/zine")
	if url == "" {
		return
	}

	// Encode our zine
	zineBytes, err := json.Marshal(myZine)
	if err != nil {
		logrus.Warnf("📰 Failed to encode zine for %s: %v", targetName, err)
		return
	}

	// Create request with 30s timeout to prevent goroutine leaks
	ctx, cancel := context.WithTimeout(network.ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(zineBytes))
	if err != nil {
		logrus.Warnf("📰 Failed to create zine request for %s: %v", targetName, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	// Add mesh authentication headers (Ed25519 signature)
	network.AddMeshAuthHeaders(req)

	client := network.getMeshHTTPClient()
	if client == nil {
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		logrus.Infof("📰 Failed to exchange zine with %s: %v", targetName, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logrus.Infof("📰 Zine exchange with %s failed: status %d", targetName, resp.StatusCode)
		return
	}

	// Decode their zine
	var theirZine Zine
	if err := json.NewDecoder(resp.Body).Decode(&theirZine); err != nil {
		logrus.Warnf("📰 Failed to decode zine from %s: %v", targetName, err)
		return
	}

	// Verify signature
	pubKey := network.resolvePublicKeyForNara(targetName)
	if len(pubKey) > 0 && !VerifyZine(&theirZine, pubKey) {
		logrus.Warnf("📰 Invalid zine signature from %s, rejecting", targetName)
		return
	}

	// Merge their events into our ledger
	added, _ := network.MergeSyncEventsWithVerification(theirZine.Events)
	if added > 0 && network.logService != nil {
		network.logService.BatchGossipMerge(targetName, added)
	}

	// Mark peer as online - successful zine exchange proves they're reachable
	// Their events in the zine already prove they're active, no need for seen event
	network.recordObservationOnlineNara(targetName, theirZine.CreatedAt)
}

// exchangeStashWithPeer performs stash operations with a peer (store or retrieve)
func (network *Network) exchangeStashWithPeer(targetName string) {
	if network.stashService != nil {
		network.stashService.ExchangeStashWithPeer(targetName)
	}
}

// pushStashToOwner pushes a stash back to its owner via HTTP POST to /stash/push.
// This is called during hey-there recovery or stash-refresh requests.
// Runs the push in a background goroutine with optional delay.
func (network *Network) pushStashToOwner(targetName string, delay time.Duration) {
	if network.stashService != nil {
		network.stashService.PushStashToOwner(targetName, delay)
	}
}

// SendDM sends a SyncEvent directly to a target nara via HTTP POST to /dm.
// Returns true if the DM was successfully delivered.
// If delivery fails, the event should already be in the sender's ledger and
// will spread via gossip instead.
func (network *Network) SendDM(targetName string, event SyncEvent) bool {
	// Determine URL - use test override if available
	url := network.buildMeshURL(targetName, "/dm")
	if url == "" {
		logrus.Debugf("📬 Cannot DM %s: not reachable via mesh", targetName)
		return false
	}

	// Encode the event
	eventBytes, err := json.Marshal(event)
	if err != nil {
		logrus.Warnf("📬 Failed to encode DM for %s: %v", targetName, err)
		return false
	}

	ctx, cancel := context.WithTimeout(network.ctx, 15*time.Second)
	defer cancel()

	// Create request with auth headers
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(eventBytes))
	if err != nil {
		logrus.Warnf("📬 Failed to create DM request for %s: %v", targetName, err)
		return false
	}
	req.Header.Set("Content-Type", "application/json")

	// Add mesh authentication headers (Ed25519 signature)
	network.AddMeshAuthHeaders(req)

	client := network.getMeshHTTPClient()

	resp, err := client.Do(req)
	if err != nil {
		logrus.Debugf("📬 Failed to send DM to %s: %v", targetName, err)
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// backgroundSync performs lightweight periodic syncing to strengthen collective memory
// Runs every ~30 minutes (±5min jitter) to catch up on missed critical events
func (network *Network) backgroundSync() {
	// Initial random delay (0-5 minutes) to spread startup load
	initialDelay := time.Duration(rand.Intn(5)) * time.Minute

	select {
	case <-time.After(initialDelay):
		// continue
	case <-network.ctx.Done():
		logrus.Debugf("backgroundSync: shutting down before initial delay")
		return
	}

	// Main sync loop: every 30 minutes ±5min jitter
	for {
		baseInterval := 30 * time.Minute
		jitter := time.Duration(rand.Intn(10)-5) * time.Minute // -5 to +5 minutes
		interval := baseInterval + jitter

		select {
		case <-time.After(interval):
			network.performBackgroundSync()
		case <-network.ctx.Done():
			logrus.Debugf("backgroundSync: shutting down gracefully")
			return
		}
	}
}

// performBackgroundSync executes a single background sync cycle
func (network *Network) performBackgroundSync() {
	network.recoverSelfStartTimeFromMesh()

	// Get online neighbors
	online := network.NeighbourhoodOnlineNames()
	if len(online) == 0 {
		logrus.Debug("🔄 Background sync: no neighbors online")
		return
	}

	// Prefer neighbors that advertise medium/hog memory profiles
	eligible := make([]string, 0, len(online))
	for _, name := range online {
		if network.neighborSupportsBackgroundSync(name) {
			eligible = append(eligible, name)
		}
	}
	if len(eligible) == 0 {
		logrus.Debug("🔄 Background sync: no eligible neighbors (memory-limited)")
		return
	}

	// Pick 1-2 random neighbors to query
	numNeighbors := 1
	if len(eligible) > 1 && rand.Float64() > 0.5 {
		numNeighbors = 2
	}

	// Shuffle and pick neighbors
	rand.Shuffle(len(eligible), func(i, j int) {
		eligible[i], eligible[j] = eligible[j], eligible[i]
	})

	neighbors := eligible[:numNeighbors]

	// Query each neighbor via mesh if available
	for _, neighbor := range neighbors {
		if network.tsnetMesh != nil {
			ip := network.getMeshIPForNara(neighbor)
			if ip != "" {
				network.performBackgroundSyncViaMesh(neighbor, ip)
			} else {
				logrus.Debugf("🔄 Background sync: neighbor %s not mesh-enabled, skipping", neighbor)
			}
		} else {
			// Could add MQTT fallback here if needed
			logrus.Debug("🔄 Background sync: mesh not available, skipping")
		}
	}
}

func (network *Network) neighborSupportsBackgroundSync(name string) bool {
	nara := network.getNara(name)
	if nara.Name == "" {
		return true
	}
	if nara.Status.MemoryMode == string(MemoryModeShort) {
		return false
	}
	return true
}

func (network *Network) isShortMemoryMode() bool {
	if network.local == nil {
		return false
	}
	return network.local.MemoryProfile.Mode == MemoryModeShort
}

func (network *Network) recoverSelfStartTimeFromMesh() {
	if network.local.SyncLedger == nil || network.tsnetMesh == nil || network.local.isBooting() {
		return
	}

	obs := network.local.getMeObservation()
	if obs.StartTime > 0 {
		return
	}

	online := network.NeighbourhoodOnlineNames()
	if len(online) == 0 {
		return
	}

	// Ask a few neighbors for their opinions about our start time.
	rand.Shuffle(len(online), func(i, j int) {
		online[i], online[j] = online[j], online[i]
	})

	targetCount := len(online)
	if targetCount > 3 {
		targetCount = 3
	}

	client := network.getMeshHTTPClient()
	if client == nil {
		return
	}
	subjects := []string{network.meName()}
	totalAdded := 0

	for _, neighbor := range online[:targetCount] {
		ip := network.getMeshIPForNara(neighbor)
		if ip == "" {
			continue
		}

		events, respVerified := network.fetchSyncEventsFromMesh(client, ip, neighbor, subjects, 0, 1, 500)
		if len(events) == 0 {
			continue
		}

		filtered := make([]SyncEvent, 0, len(events))
		for _, event := range events {
			if event.Service != ServiceObservation || event.Observation == nil {
				continue
			}
			if event.Observation.Subject != network.meName() {
				continue
			}
			filtered = append(filtered, event)
		}

		if len(filtered) == 0 {
			continue
		}

		added, warned := network.MergeSyncEventsWithVerification(filtered)
		totalAdded += added
		verifiedStr := ""
		if respVerified && warned == 0 {
			verifiedStr = " ✓"
		} else if warned > 0 {
			verifiedStr = fmt.Sprintf(" ⚠%d", warned)
		}
		logrus.Printf("📦 start time recovery from %s: received %d events, merged %d%s", neighbor, len(filtered), added, verifiedStr)
	}

	if totalAdded == 0 {
		return
	}

	// Try to recover start time from event-based consensus
	if network.local.Projections != nil {
		updated := network.local.getMeObservation()
		before := updated.StartTime
		network.local.Projections.Opinion().RunOnce()
		opinion := network.local.Projections.Opinion().DeriveOpinionWithValidation(network.meName())
		if updated.StartTime == 0 && opinion.StartTime > 0 {
			updated.StartTime = opinion.StartTime
		}
		if updated.StartTime > 0 && updated.StartTime != before {
			network.local.setObservation(network.meName(), updated)
			logrus.Printf("🕰️ recovered start time for %s via event consensus: %d", network.meName(), updated.StartTime)
		}
	}
}

// performBackgroundSyncViaMesh performs background sync with a specific neighbor via mesh
//
// IMPORTANT: This function determines which event types are continuously synced across the network.
// When adding new service types (in sync.go), consider whether they should be synced here.
// Current services synced:
//   - ServiceObservation: restart/first-seen/status-change events (24h window, personality filtered)
//   - ServicePing: RTT measurements (no time filter, used for Vivaldi coordinates)
//   - ServiceSocial: teases, observations, gossip (24h window, personality filtered)
//
// Boot recovery (bootRecoveryViaMesh) syncs ALL events without filtering.
// This background sync maintains eventual consistency for recent events.
func (network *Network) performBackgroundSyncViaMesh(neighbor, ip string) {
	logrus.Infof("🔄 background sync: requesting events from %s (%s)", neighbor, ip)

	// Use existing fetchSyncEventsFromMesh method with lightweight parameters
	client := network.getMeshHTTPClient()
	if client == nil {
		logrus.Infof("🔄 Background sync with %s skipped: mesh HTTP client unavailable", neighbor)
		return
	}

	// Fetch events from this neighbor (all types, we filter below)
	events, success := network.fetchSyncEventsFromMesh(
		client,
		ip,
		neighbor,
		nil, // subjects: nil = all naras
		0,   // sliceIndex: 0 for simple query
		1,   // sliceTotal: 1 for simple query (no slicing)
		100, // maxEvents: lightweight query
	)

	if !success {
		logrus.Infof("🔄 Background sync with %s failed", neighbor)
		return
	}

	// Filter and merge events by type
	// Time cutoff for observation and social events (24h window)
	cutoff := time.Now().Add(-24 * time.Hour).UnixNano()
	added := 0
	hasPingEvents := false

	for _, event := range events {
		switch event.Service {
		case ServiceObservation:
			// Observation events: 24h window with personality filtering
			if event.Timestamp >= cutoff {
				if network.local.SyncLedger.AddEventFiltered(event, network.local.Me.Status.Personality) {
					added++
				}
			}

		case ServicePing:
			// Ping events: no time filtering (always useful for coordinate estimation)
			if network.local.SyncLedger.AddEvent(event) {
				added++
				hasPingEvents = true
			}

		case ServiceSocial:
			// Social events: 24h window with personality filtering
			// This ensures teases, gossip, and social observations propagate across the network
			if event.Timestamp >= cutoff {
				if network.local.SyncLedger.AddEventFiltered(event, network.local.Me.Status.Personality) {
					added++
				}
			}
		}
	}

	if added > 0 {
		logrus.Printf("🔄 background sync from %s: received %d events, merged %d", neighbor, len(events), added)

		// If we received ping events, update AvgPingRTT from history
		if hasPingEvents {
			network.seedAvgPingRTTFromHistory()
		}

		// Trigger projection update for new events
		if network.local.Projections != nil {
			network.local.Projections.Trigger()
		}
	} else if len(events) > 0 {
		logrus.Debugf("🔄 background sync from %s: received %d events (all duplicates)", neighbor, len(events))
	}
}

// pruneInactiveNaras removes transient/zombie naras to prevent memory bloat.
// Uses tiered retention based on how established a nara is:
// - Newcomers (< 2d old): pruned after 24h offline (proving period)
// - Established (2d-30d old): pruned after 7d offline (generous grace period)
// - Veterans (30d+ old): kept indefinitely (important community members like bart, r2d2, lisa)
// - Zombies (never seen): pruned immediately if old enough
func (network *Network) pruneInactiveNaras() {
	network.local.mu.Lock()

	now := time.Now().Unix()

	const (
		newcomerAge        = int64(2 * 86400)  // 2 days - proving period
		establishedAge     = int64(30 * 86400) // 30 days - veteran threshold
		newcomerTimeout    = int64(24 * 3600)  // Prune newcomers after 24h offline
		establishedTimeout = int64(7 * 86400)  // Prune established after 7d offline
		// Veterans (30d+) are never auto-pruned - they're part of the community
	)

	var toRemove []string
	var established, veterans, zombies int
	var prunedNewcomers, prunedEstablished int

	for name, nara := range network.Neighbourhood {
		obs := network.local.getObservationLocked(name)

		// Never remove ourselves
		if name == network.meName() {
			continue
		}

		// Count naras by age category (regardless of online status)
		naraAge := int64(0)
		if obs.StartTime > 0 {
			naraAge = now - obs.StartTime
		}
		if naraAge >= establishedAge {
			veterans++
		} else if naraAge >= newcomerAge {
			established++
		}

		// Never remove currently ONLINE naras
		if obs.Online == "ONLINE" {
			continue
		}

		// Calculate how long since we last saw them
		timeSinceLastSeen := int64(0)
		if obs.LastSeen > 0 {
			timeSinceLastSeen = now - obs.LastSeen
		}

		nara.mu.Lock()
		hasActivity := nara.Status.Flair != "" || nara.Status.Buzz > 0 || nara.Status.Chattiness > 0
		nara.mu.Unlock()

		// Zombie detection: never seen but "first seen" is old
		// These are malformed entries that should be cleaned up immediately
		if obs.LastSeen == 0 && obs.StartTime > 0 && (now-obs.StartTime) > 3600 {
			toRemove = append(toRemove, name)
			zombies++
			continue
		}

		// Also catch zombies with no timestamps and no activity
		if obs.LastSeen == 0 && obs.StartTime == 0 && !hasActivity {
			toRemove = append(toRemove, name)
			zombies++
			continue
		}

		// Skip if we don't have LastSeen (but they're not a zombie)
		if obs.LastSeen == 0 {
			logrus.Debugf("⏭️  Skipping %s: no LastSeen timestamp (but has activity, not a zombie)", name)
			continue
		}

		// Tiered pruning based on nara age
		shouldPrune := false

		if naraAge < newcomerAge {
			// Newcomer: prune if offline for 24h
			if timeSinceLastSeen > newcomerTimeout {
				shouldPrune = true
				prunedNewcomers++
			}
		} else if naraAge < establishedAge {
			// Established: prune if offline for 7 days
			if timeSinceLastSeen > establishedTimeout {
				shouldPrune = true
				prunedEstablished++
			}
		} else {
			// Veteran (30d+): keep indefinitely, they're part of the community
			// Even if they're offline for months, we remember them (bart, r2d2, lisa, etc.)
			// (already counted in veterans counter above)
		}

		if shouldPrune {
			toRemove = append(toRemove, name)
		}
	}

	// Remove inactive naras
	if len(toRemove) > 0 {
		// Lock ordering: We release local.mu before taking Me.mu to avoid holding both simultaneously
		// This is safe because we're only deleting (not checking existence then acting on it)
		// Other code that needs both locks must follow: local.mu → Me.mu (never the reverse)

		// First remove from Neighbourhood (protected by network.local.mu)
		for _, name := range toRemove {
			delete(network.Neighbourhood, name)
			network.howdyCoordinators.Delete(name)
		}

		// Also remove their events from the sync ledger to prevent re-discovery
		// This removes ALL events involving the ghost nara, including:
		// - Events they emitted (hey-there, chau, social, etc.)
		// - Events about them (observations, pings, social events where they're target)
		// - Checkpoints WHERE THEY ARE THE SUBJECT (checkpoints about them)
		//   Note: Checkpoints where they were only a voter/emitter are kept
		// See sync.go:eventInvolvesNara() for the full pruning logic
		if network.local.SyncLedger != nil {
			for _, name := range toRemove {
				network.local.SyncLedger.RemoveEventsFor(name)
			}
		}

		// Clean up verification caches and trigger projection updates
		network.pruneVerifyPingCache(toRemove)
		if network.local.Projections != nil {
			network.local.Projections.Trigger()
		}
		network.local.mu.Unlock()

		// Then remove from our observations (protected by network.local.Me.mu)
		network.local.Me.mu.Lock()
		for _, name := range toRemove {
			delete(network.local.Me.Status.Observations, name)
		}
		network.local.Me.mu.Unlock()

		logrus.Printf("🧹 Pruned %d inactive naras: %d zombies, %d newcomers (24h), %d established (7d) | kept %d established, %d veterans (30d+)",
			len(toRemove), zombies, prunedNewcomers, prunedEstablished, established-prunedEstablished, veterans)
		logrus.Printf("🕯️ 🪦 🕯️  In memory of: %v", toRemove)
	} else {
		network.local.mu.Unlock()
	}
}

// contains checks if a string is in a slice
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// socialMaintenance periodically cleans up social data
func (network *Network) socialMaintenance() {
	// Run every 5 minutes
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// continue
		case <-network.ctx.Done():
			return
		}

		// Prune inactive naras from neighbourhood
		network.pruneInactiveNaras()

		// Prune the sync ledger
		if network.local.SyncLedger != nil {
			beforeCount := network.local.SyncLedger.EventCount()
			network.local.SyncLedger.Prune()
			afterCount := network.local.SyncLedger.EventCount()

			// Log event store stats
			serviceCounts := network.local.SyncLedger.GetEventCountsByService()
			criticalCount := network.local.SyncLedger.GetCriticalEventCount()
			var statsStr string
			for service, count := range serviceCounts {
				if statsStr != "" {
					statsStr += ", "
				}
				statsStr += fmt.Sprintf("%s=%d", service, count)
			}

			// Get memory stats
			var memStats runtime.MemStats
			runtime.ReadMemStats(&memStats)
			memAllocMB := memStats.Alloc / 1024 / 1024
			memSysMB := memStats.Sys / 1024 / 1024
			memHeapMB := memStats.HeapSys / 1024 / 1024
			memStackMB := memStats.StackSys / 1024 / 1024
			numGoroutines := runtime.NumGoroutine()
			// Calculate overhead: Sys - HeapSys - StackSys = other runtime structures
			memOtherMB := memSysMB - memHeapMB - memStackMB

			if beforeCount != afterCount {
				logrus.Printf("📊 event store: %d events (%s, critical=%d) - pruned %d | mem: %dMB alloc, %dMB sys (heap:%dMB stack:%dMB other:%dMB) | goroutines:%d",
					afterCount, statsStr, criticalCount, beforeCount-afterCount, memAllocMB, memSysMB, memHeapMB, memStackMB, memOtherMB, numGoroutines)
			} else {
				logrus.Printf("📊 event store: %d events (%s, critical=%d) | mem: %dMB alloc, %dMB sys (heap:%dMB stack:%dMB other:%dMB) | goroutines:%d",
					afterCount, statsStr, criticalCount, memAllocMB, memSysMB, memHeapMB, memStackMB, memOtherMB, numGoroutines)
			}

			// Cleanup rate limiter to prevent unbounded map growth
			network.local.SyncLedger.observationRL.Cleanup()
		}

		// Cleanup tease cooldowns
		network.TeaseState.Cleanup()
	}
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

// --- Event-Sourced Online Status ---

// getRecentEventsFor returns all recent events relevant to a nara's online status,
// sorted by timestamp descending (most recent first).
// This includes:
// - Events FROM the nara (hey_there, chau, seen)
// - Events ABOUT the nara (status-change observations)
func (network *Network) getRecentEventsFor(name string) []SyncEvent {
	if network.local.SyncLedger == nil {
		return nil
	}

	var relevant []SyncEvent

	// Get all events involving this nara
	allEvents := network.local.SyncLedger.GetAllEvents()

	for _, e := range allEvents {
		switch e.Service {
		case ServiceHeyThere:
			if e.HeyThere != nil && e.HeyThere.From == name {
				relevant = append(relevant, e)
			}
		case ServiceChau:
			if e.Chau != nil && e.Chau.From == name {
				relevant = append(relevant, e)
			}
		case ServiceSeen:
			if e.Seen != nil && e.Seen.Subject == name {
				relevant = append(relevant, e)
			}
		case ServiceObservation:
			if e.Observation != nil && e.Observation.Subject == name {
				// Only include status-relevant observation types
				switch e.Observation.Type {
				case "status-change", "restart", "first-seen":
					relevant = append(relevant, e)
				}
			}
		}
	}

	// Sort by timestamp descending (most recent first)
	sort.Slice(relevant, func(i, j int) bool {
		return relevant[i].Timestamp > relevant[j].Timestamp
	})

	return relevant
}

// emitSeenEvent emits a "seen" event to vouch for a quiet nara.
// Only emits if the subject hasn't emitted any events themselves in the last 5 minutes.
// This way, active naras prove themselves through their own events,
// while quiet naras get vouched for by those who interact with them.
// Also rate-limited to 1 per 2 minutes per subject to avoid spam.
func (network *Network) emitSeenEvent(subject, via string) {
	if network.local.SyncLedger == nil || network.local.isBooting() {
		return
	}

	// Don't emit seen events for ourselves
	if subject == network.meName() {
		return
	}

	// Don't emit if subject is already known to be online
	obs := network.local.getObservation(subject)
	if obs.Online == "ONLINE" {
		return
	}

	me := network.meName()
	allEvents := network.local.SyncLedger.GetAllEvents()
	now := time.Now().UnixNano()
	quietThreshold := int64(5 * time.Minute)
	rateLimit := int64(2 * time.Minute)

	// Check two things:
	// 1. Has the subject emitted any events recently? (if so, they don't need vouching)
	// 2. Have we already emitted a seen event for them recently? (rate limit)
	subjectIsQuiet := true
	weEmittedRecently := false

	// Scan all events (can't break early since events aren't sorted by timestamp)
	for i := len(allEvents) - 1; i >= 0; i-- {
		e := allEvents[i]
		age := now - e.Timestamp

		// Skip events outside our time windows
		if age > quietThreshold {
			continue
		}

		// Check if subject emitted this event (they're active, don't need vouching)
		if subjectIsQuiet && e.Emitter == subject {
			subjectIsQuiet = false
		}

		// Check if we emitted a seen event for this subject recently
		if !weEmittedRecently && age < rateLimit &&
			e.Service == ServiceSeen && e.Seen != nil &&
			e.Seen.Observer == me && e.Seen.Subject == subject {
			weEmittedRecently = true
		}

		// Early exit if we've found both conditions
		if !subjectIsQuiet && weEmittedRecently {
			break
		}
	}

	// Only emit if: subject is quiet AND we haven't emitted recently
	if !subjectIsQuiet {
		return // Subject is active, their own events prove they're online
	}
	if weEmittedRecently {
		return // We already vouched for them recently
	}

	event := NewSeenSyncEvent(me, subject, via, network.local.Keypair)
	network.local.SyncLedger.AddEvent(event)
	if network.local.Projections != nil {
		network.local.Projections.Trigger()
	}
}

// stashMaintenance manages confidant selection and inventory
func (network *Network) stashMaintenance() {
	if network.stashService != nil {
		network.stashService.RunMaintenanceLoop(network.stashDistributeTrigger)
	}
}

// reactToConfidantOffline is called immediately when we detect a nara went offline
// If they're one of our confidants, remove them and trigger immediate replacement search
func (network *Network) reactToConfidantOffline(name string) {
	if network.stashService != nil {
		network.stashService.ReactToConfidantOffline(name)
	}
}

// getPeerInfo gathers metadata about peers for confidant selection
func (network *Network) getPeerInfo(names []string) []PeerInfo {
	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	peers := make([]PeerInfo, 0, len(names))
	for _, name := range names {
		nara, ok := network.Neighbourhood[name]
		if !ok {
			continue
		}

		nara.mu.Lock()
		memoryMode := MemoryModeMedium // Default
		if modeStr := nara.Status.MemoryMode; modeStr != "" {
			memoryMode = MemoryMode(modeStr)
		}

		// Calculate uptime approximation
		// Use the time since last event as a proxy for reliability
		uptimeSecs := int64(0)
		if state := network.local.Projections.OnlineStatus().GetState(name); state != nil {
			if state.Status == "ONLINE" {
				// Longer time online (in this session) = more reliable
				elapsedNanos := time.Now().UnixNano() - state.LastEventTime
				uptimeSecs = elapsedNanos / 1e9 // Convert to seconds
				if uptimeSecs < 0 {
					uptimeSecs = 0
				}
			}
		}
		nara.mu.Unlock()

		peers = append(peers, PeerInfo{
			Name:       name,
			MemoryMode: memoryMode,
			UptimeSecs: uptimeSecs,
		})
	}

	return peers
}

// broadcastStashRefresh broadcasts an MQTT event asking confidants to push stash back
// This is used during boot if hey-there didn't trigger recovery
func (network *Network) broadcastStashRefresh() {
	if network.Mqtt == nil || !network.Mqtt.IsConnected() {
		logrus.Debugf("📦 Cannot broadcast stash-refresh: MQTT not connected")
		return
	}

	if network.stashService == nil {
		return
	}

	// Create a stash-refresh event (similar to hey-there)
	event := map[string]interface{}{
		"from":      network.meName(),
		"timestamp": time.Now().Unix(),
	}

	// Broadcast on MQTT
	topic := "nara/plaza/stash_refresh"
	network.postEvent(topic, event)
	logrus.Infof("📦 Broadcast stash-refresh request")
}

// =============================================================================
// Stash Service initialization
// =============================================================================

// initStashService initializes the unified stash service.
// This can be called by tests after manually setting up stashManager.
func (network *Network) initStashService() {
	network.stashService = NewStashService(
		network.stashManager,
		network.confidantStore,
		network.stashSyncTracker,
		newStashServiceContext(network),
	)
}
