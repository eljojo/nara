package nara

import (
	"context"
	"math/rand"
	"net/http"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"

	"github.com/eljojo/nara/runtime"
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

type Network struct {
	Neighbourhood          map[string]*Nara  // Index by name (legacy, to be deprecated)
	NeighbourhoodByID      map[string]*Nara  // Primary index: by nara ID
	nameToID               map[string]string // Secondary index: name → ID for quick lookup
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
	meshClient      *MeshClient        // Reusable mesh HTTP client for authenticated requests
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
	testHTTPClient            *http.Client                    // Override HTTP client for testing
	testMeshURLs              map[string]string               // Override mesh URLs for testing (nara name -> URL)
	testTeaseDelay            *time.Duration                  // Override tease delay for testing (nil = use default 0-5s random)
	testObservationDelay      *time.Duration                  // Override observation debounce delay for testing
	testAnnounceCount         int                             // Counter for announce() calls (for testing)
	testSkipHeyThereSleep     bool                            // Skip the 1s sleep in handleHeyThereEvent (for testing)
	testSkipJitter            bool                            // Skip jitter delays in hey_there for faster tests
	testSkipBootRecovery      bool                            // Skip boot recovery entirely (for checkpoint tests)
	testSkipHeyThereRateLimit bool                            // Skip the 5s rate limit on hey_there (for testing)
	testSkipCoordinateWait    bool                            // Skip the 30s initial wait in coordinateMaintenance (for testing)
	testPingFunc              func(name string) (bool, error) // Override ping behavior for testing (returns success, error)
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

	// Logging service
	logService *LogService

	// Checkpoint service: consensus-based checkpointing
	checkpointService *CheckpointService

	// Runtime: new message-based runtime
	runtime *runtime.Runtime
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

// PeerResponse contains identity information about a peer.
// Used by the peer resolution protocol to return discovered peer info.
type PeerResponse struct {
	Target    string `json:"target"`
	PublicKey string `json:"public_key"`
	MeshIP    string `json:"mesh_ip,omitempty"`
}

func NewNetwork(localNara *LocalNara, host string, user string, pass string) *Network {
	network := &Network{local: localNara}
	network.Neighbourhood = make(map[string]*Nara)
	network.NeighbourhoodByID = make(map[string]*Nara)
	network.nameToID = make(map[string]string)
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
// Uses O(1) lookup via the NeighbourhoodByID index when available,
// falls back to O(N) search of Neighbourhood for backwards compatibility.
func (network *Network) getPublicKeyForNaraID(naraID string) []byte {
	if naraID == "" {
		return nil
	}

	// Check self first
	if naraID == network.local.Me.Status.ID {
		return network.local.Keypair.PublicKey
	}

	network.local.mu.Lock()
	var matchedNara *Nara

	// Try O(1) lookup via NeighbourhoodByID index if available
	if network.NeighbourhoodByID != nil {
		matchedNara = network.NeighbourhoodByID[naraID]
	}

	// Fall back to O(N) search of Neighbourhood for backwards compatibility
	// (handles test cases that don't initialize NeighbourhoodByID)
	if matchedNara == nil {
		for _, nara := range network.Neighbourhood {
			nara.mu.Lock()
			if nara.Status.ID == naraID {
				matchedNara = nara
				nara.mu.Unlock()
				break
			}
			nara.mu.Unlock()
		}
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

	if !e.VerifyWithKey(pubKey) {
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
// InitGossipIdentity initializes gossip-mode identity emission.
// Called by Start() and can be called by tests to simulate startup.
// This emits the hey_there sync event that allows our identity to propagate through gossip.
// Note: Only needed in gossip-only mode. In hybrid mode, heyThere() already adds the event to the ledger.
func (network *Network) InitGossipIdentity() {
	if network.TransportMode == TransportGossip {
		network.emitHeyThereSyncEvent()
	}
}

func (network *Network) Start(serveUI bool, httpAddr string, meshConfig *TsnetConfig) {
	if serveUI {
		err := network.startHttpServer(httpAddr)
		if err != nil {
			logrus.Panic(err)
		}
	}

	// Initialize runtime with adapters and services
	if err := network.initRuntime(); err != nil {
		logrus.Errorf("Failed to initialize runtime: %v", err)
	}

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
					tsnetMesh.SetHTTPClient(network.getMeshHTTPClient())

					// Initialize mesh client for authenticated mesh HTTP requests
					network.meshClient = NewMeshClient(
						network.getMeshHTTPClient(),
						network.meName(),
						network.local.Keypair,
					)

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

	// Start stash maintenance (confidant selection, inventory) (OLD - replaced by runtime services)
	// if !network.ReadOnly && network.stashService != nil {
	// 	go network.stashMaintenance()
	// }

	// Start runtime services
	if network.runtime != nil {
		if err := network.startRuntime(); err != nil {
			logrus.Errorf("Failed to start runtime: %v", err)
		}
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

// --- Ledger Gossip and Boot Recovery ---
// (Extracted to boot_recovery.go, boot_sync.go, boot_ledger.go, boot_checkpoint.go, boot_backfill.go)

// --- Social Network ---
// (Extracted to social_network.go)

func (network *Network) isShortMemoryMode() bool {
	if network.local == nil {
		return false
	}
	return network.local.MemoryProfile.Mode == MemoryModeShort
}

// pruneInactiveNaras removes transient/zombie naras to prevent memory bloat.
// Uses tiered retention based on how established a nara is:
// - Newcomers (< 2d old): pruned after 24h offline (proving period)
// - Established (2d-30d old): pruned after 7d offline (generous grace period)
// - Veterans (30d+ old): kept indefinitely (important community members like bart, r2d2, lisa)
// - Zombies (never seen): pruned immediately if old enough

// --- Journey Completion Handling ---
// (Extracted to world_network.go)

// --- Event-Sourced Online Status ---

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

// --- Stash Network Methods ---
// (Extracted to stash_network.go)
