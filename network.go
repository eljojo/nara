package nara

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
)

type Network struct {
	Neighbourhood       map[string]*Nara
	Buzz                *Buzz
	LastHeyThere        int64
	LastSelfie          int64
	skippingEvents      bool
	local               *LocalNara
	Mqtt                mqtt.Client
	heyThereInbox       chan HeyThereEvent
	newspaperInbox      chan NewspaperEvent
	chauInbox           chan Nara
	selfieInbox         chan Nara
	socialInbox         chan SocialEvent
	ledgerRequestInbox  chan LedgerRequest
	ledgerResponseInbox chan LedgerResponse
	TeaseState          *TeaseState
	ReadOnly            bool
	// SSE broadcast for web clients
	sseClients   map[chan SocialEvent]bool
	sseClientsMu sync.RWMutex
	// World journey state
	worldJourneys   []*WorldMessage // Completed journeys
	worldJourneysMu sync.RWMutex
	worldHandler    *WorldJourneyHandler
	worldMesh       *MockMeshNetwork   // Used when no tsnet configured
	worldTransport  *MockMeshTransport // Used when no tsnet configured
	tsnetMesh       *TsnetMesh         // Used when Headscale is configured
	// Pending journey tracking for timeout detection
	pendingJourneys      map[string]*PendingJourney
	pendingJourneysMu    sync.RWMutex
	journeyCompleteInbox chan JourneyCompletion
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
	JourneyID  string `json:"journey_id"`
	Originator string `json:"originator"`
	ReportedBy string `json:"reported_by"`
}

type NewspaperEvent struct {
	From   string
	Status NaraStatus
}

type HeyThereEvent struct {
	From string
}

func NewNetwork(localNara *LocalNara, host string, user string, pass string) *Network {
	network := &Network{local: localNara}
	network.Neighbourhood = make(map[string]*Nara)
	network.heyThereInbox = make(chan HeyThereEvent)
	network.chauInbox = make(chan Nara)
	network.selfieInbox = make(chan Nara)
	network.newspaperInbox = make(chan NewspaperEvent)
	network.socialInbox = make(chan SocialEvent, 100)
	network.ledgerRequestInbox = make(chan LedgerRequest, 50)
	network.ledgerResponseInbox = make(chan LedgerResponse, 50)
	network.TeaseState = NewTeaseState()
	network.sseClients = make(map[chan SocialEvent]bool)
	network.skippingEvents = false
	network.Buzz = newBuzz()
	network.worldJourneys = make([]*WorldMessage, 0)
	network.pendingJourneys = make(map[string]*PendingJourney)
	network.journeyCompleteInbox = make(chan JourneyCompletion, 50)
	network.Mqtt = initializeMQTT(network.mqttOnConnectHandler(), network.meName(), host, user, pass)
	return network
}

// InitWorldJourney sets up the world journey handler with the given mesh transport
func (network *Network) InitWorldJourney(mesh MeshTransport) {
	network.worldHandler = NewWorldJourneyHandler(
		network.local,
		mesh,
		network.getMyClout,
		network.getOnlineNaraNames,
		network.getPublicKeyForNara,
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
	if network.local.SocialLedger == nil {
		return nil
	}

	baseClout := network.local.SocialLedger.DeriveClout(network.local.Soul)

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
		obs := nara.getObservation(name)
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
		logrus.Debugf("🕸️  World journey: %d mesh-enabled naras, skipped %d non-mesh naras", len(names)-1, skippedCount)
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

func (network *Network) onWorldJourneyComplete(wm *WorldMessage) {
	network.worldJourneysMu.Lock()
	network.worldJourneys = append(network.worldJourneys, wm)
	// Keep only last 100 journeys
	if len(network.worldJourneys) > 100 {
		network.worldJourneys = network.worldJourneys[len(network.worldJourneys)-100:]
	}
	network.worldJourneysMu.Unlock()

	// Record journey-complete observation event
	if network.local.SocialLedger != nil {
		event := NewJourneyObservationEvent(network.meName(), wm.Originator, ReasonJourneyComplete, wm.ID)
		network.local.SocialLedger.AddEvent(event)
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
		}
		network.postEvent("nara/plaza/journey_complete", completion)
	}

	logrus.Printf("World journey complete! %s: %s", wm.Originator, wm.OriginalMessage)
	logrus.Printf("observation: journey %s completed! (from %s)", wm.ID[:8], wm.Originator)
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
	if network.local.SocialLedger != nil {
		event := NewJourneyObservationEvent(network.meName(), wm.Originator, ReasonJourneyPass, wm.ID)
		network.local.SocialLedger.AddEvent(event)
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

	if token := network.Mqtt.Connect(); token.Wait() && token.Error() != nil {
		logrus.Fatalf("MQTT connection error: %v", token.Error())
	}

	// Initialize world journey handler
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
					network.InitWorldJourney(tsnetMesh)
					network.local.Me.Status.MeshEnabled = true
					network.local.Me.Status.MeshIP = tsnetMesh.IP()
					logrus.Infof("World journey using tsnet mesh (IP: %s)", tsnetMesh.IP())
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

	if !network.ReadOnly {
		network.heyThere()
		network.announce()
	}

	time.Sleep(1 * time.Second)

	go network.formOpinion()
	go network.observationMaintenance()
	if !network.ReadOnly {
		go network.announceForever()
	}
	go network.processHeyThereEvents()
	go network.processSelfieEvents()
	go network.processChauEvents()
	go network.processNewspaperEvents()
	go network.processSocialEvents()
	go network.processLedgerRequests()
	go network.processLedgerResponses()
	go network.processJourneyCompleteEvents()
	go network.trendMaintenance()
	go network.maintenanceBuzz()

	// Start boot recovery after a short delay to gather neighbors
	if !network.ReadOnly {
		go network.bootRecovery()
	}

	// Start continuous mesh sync for event propagation
	if !network.ReadOnly {
		go network.continuousMeshSync()
	}

	// Start garbage collection maintenance
	go network.socialMaintenance()

	// Start journey timeout maintenance
	go network.journeyTimeoutMaintenance()

	// Start coordinate maintenance (Vivaldi pings)
	go network.coordinateMaintenance()
}

func (network *Network) meName() string {
	return network.local.Me.Name
}

func (network *Network) announce() {
	if network.ReadOnly {
		return
	}
	topic := fmt.Sprintf("%s/%s", "nara/newspaper", network.meName())
	network.recordObservationOnlineNara(network.meName())
	network.postEvent(topic, network.local.Me.Status)
}

func (network *Network) announceForever() {
	for {
		ts := network.local.chattinessRate(5, 55)
		// logrus.Debugf("time between announces = %d", ts)
		time.Sleep(time.Duration(ts) * time.Second)

		network.announce()
	}
}

func (network *Network) processNewspaperEvents() {
	for {
		network.handleNewspaperEvent(<-network.newspaperInbox)
	}
}

func (network *Network) handleNewspaperEvent(event NewspaperEvent) {
	logrus.Debugf("newspaperHandler update from %s", event.From)

	network.local.mu.Lock()
	nara, present := network.Neighbourhood[event.From]
	network.local.mu.Unlock()
	if present {
		nara.mu.Lock()
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

	network.recordObservationOnlineNara(event.From)
}

func (network *Network) processSelfieEvents() {
	for {
		network.handleSelfieEvent(<-network.selfieInbox)
	}
}

func (network *Network) handleSelfieEvent(nara Nara) {
	network.importNara(&nara)
	logrus.Debugf("%s just took a selfie", nara.Name)
	network.recordObservationOnlineNara(nara.Name)

	network.Buzz.increase(1)
}

func (network *Network) processHeyThereEvents() {
	for {
		network.handleHeyThereEvent(<-network.heyThereInbox)
	}
}

func (network *Network) handleHeyThereEvent(heyThere HeyThereEvent) {
	logrus.Printf("%s says: hey there!", heyThere.From)
	network.recordObservationOnlineNara(heyThere.From)

	// artificially slow down so if two naras boot at the same time they both get the message
	if !network.ReadOnly {
		time.Sleep(1 * time.Second)
		network.selfie()
	}
	network.Buzz.increase(1)
}

func (network *Network) heyThere() {
	if network.ReadOnly {
		return
	}
	ts := int64(5) // seconds
	network.recordObservationOnlineNara(network.meName())
	if (time.Now().Unix() - network.LastHeyThere) <= ts {
		return
	}
	network.LastHeyThere = time.Now().Unix()

	topic := "nara/plaza/hey_there"
	heyThere := &HeyThereEvent{From: network.meName()}
	network.postEvent(topic, heyThere)
	network.selfie()
	logrus.Printf("%s: 👋", heyThere.From)

	network.Buzz.increase(2)
}

func (network *Network) selfie() {
	if network.ReadOnly {
		return
	}
	ts := int64(5) // seconds
	network.recordObservationOnlineNara(network.meName())
	if (time.Now().Unix() - network.LastSelfie) <= ts {
		return
	}
	network.LastSelfie = time.Now().Unix()

	topic := "nara/selfies/" + network.meName()
	network.postEvent(topic, network.local.Me)
}

func (network *Network) processChauEvents() {
	for {
		network.handleChauEvent(<-network.chauInbox)
	}
}

func (network *Network) handleChauEvent(nara Nara) {
	if nara.Name == network.meName() || nara.Name == "" {
		return
	}

	network.local.mu.Lock()
	existingNara, present := network.Neighbourhood[nara.Name]
	network.local.mu.Unlock()
	if present {
		existingNara.setValuesFrom(nara)
	}

	observation := network.local.getObservation(nara.Name)
	previousState := observation.Online
	observation.Online = "OFFLINE"
	observation.LastSeen = time.Now().Unix()
	network.local.setObservation(nara.Name, observation)

	// Record offline observation event if state changed
	if previousState == "ONLINE" && !network.local.isBooting() && network.local.SocialLedger != nil {
		event := NewObservationEvent(network.meName(), nara.Name, ReasonOffline)
		network.local.SocialLedger.AddEvent(event)
		logrus.Printf("observation: %s went offline", nara.Name)
	}

	logrus.Printf("%s: chau!", nara.Name)
	network.Buzz.increase(2)
}

func (network *Network) Chau() {
	if network.ReadOnly {
		return
	}
	topic := "nara/plaza/chau"
	logrus.Printf("posting to %s", topic)

	observation := network.local.getMeObservation()
	observation.Online = "OFFLINE"
	observation.LastSeen = time.Now().Unix()
	network.local.setMeObservation(observation)

	network.postEvent(topic, network.local.Me)
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
		network.handleSocialEvent(<-network.socialInbox)
	}
}

func (network *Network) handleSocialEvent(event SocialEvent) {
	// Don't process our own events from the network
	if event.Actor == network.meName() {
		return
	}

	// Add to our ledger
	if network.local.SocialLedger.AddEvent(event) {
		logrus.Printf("📢 %s teased %s: %s", event.Actor, event.Target, TeaseMessage(event.Reason, event.Actor, event.Target))
		network.Buzz.increase(5)

		// Broadcast to SSE clients (for the shooting star effect!)
		network.broadcastSSE(event)
	}
}

// SSE client management

func (network *Network) subscribeSSE() chan SocialEvent {
	ch := make(chan SocialEvent, 10) // buffered to prevent blocking
	network.sseClientsMu.Lock()
	network.sseClients[ch] = true
	network.sseClientsMu.Unlock()
	return ch
}

func (network *Network) unsubscribeSSE(ch chan SocialEvent) {
	network.sseClientsMu.Lock()
	delete(network.sseClients, ch)
	network.sseClientsMu.Unlock()
	// Don't close the channel - a broadcast already in-flight might still
	// have a reference and would panic on send. Let GC collect it once
	// the SSE handler returns and stops selecting on it.
}

func (network *Network) broadcastSSE(event SocialEvent) {
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

// Tease broadcasts a tease event to the network.
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

	// Create and broadcast the event
	event := NewTeaseEvent(actor, target, reason)

	// Add to our own ledger
	if network.local.SocialLedger != nil {
		network.local.SocialLedger.AddEvent(event)
	}

	// Broadcast to network via MQTT
	topic := "nara/plaza/social"
	network.postEvent(topic, event)

	// Broadcast to local SSE clients (shooting star!)
	network.broadcastSSE(event)

	msg := TeaseMessage(reason, actor, target)
	logrus.Printf("😈 teasing %s: %s", target, msg)
	network.Buzz.increase(3)
	return true
}

// checkAndTease evaluates teasing triggers for a given nara.
// Tease() handles cooldown atomically via TryTease, so no separate CanTease checks needed.
func (network *Network) checkAndTease(name string, previousState string, previousTrend string) {
	if network.ReadOnly || name == network.meName() {
		return
	}

	obs := network.local.getObservation(name)
	personality := network.local.Me.Status.Personality

	// Check restart-based teasing
	if ShouldTeaseForRestarts(obs, personality) {
		if network.Tease(name, ReasonHighRestarts) {
			return // one tease at a time
		}
	}

	// Check nice number teasing (naras appreciate good looking numbers)
	if ShouldTeaseForNiceNumber(obs.Restarts, personality) {
		if network.Tease(name, ReasonNiceNumber) {
			return
		}
	}

	// Check comeback teasing
	if ShouldTeaseForComeback(obs, previousState, personality) {
		if network.Tease(name, ReasonComeback) {
			return
		}
	}

	// Check trend abandon teasing
	if previousTrend != "" {
		trendPopularity := network.trendPopularity(previousTrend)
		nara := network.getNara(name)
		if ShouldTeaseForTrendAbandon(previousTrend, nara.Status.Trend, trendPopularity, personality) {
			if network.Tease(name, ReasonTrendAbandon) {
				return
			}
		}
	}

	// Random teasing (very low probability, boosted for nearby naras)
	// You notice and interact more with those in your proximity group
	proximityBoost := 1.0
	if network.IsInMyProximityGroup(name, ProximityGroupSize) {
		proximityBoost = 3.0 // 3x more likely to notice nearby naras
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
		network.handleLedgerRequest(<-network.ledgerRequestInbox)
	}
}

func (network *Network) handleLedgerRequest(req LedgerRequest) {
	if network.ReadOnly {
		return
	}

	// Get events for requested subjects from our ledger
	events := network.local.SocialLedger.GetEventsForSubjects(req.Subjects)

	// Respond directly to the requester
	response := LedgerResponse{
		From:   network.meName(),
		Events: events,
	}

	topic := fmt.Sprintf("nara/ledger/%s/response", req.From)
	network.postEvent(topic, response)
	logrus.Debugf("📤 sent %d events to %s", len(events), req.From)
}

func (network *Network) processLedgerResponses() {
	for {
		network.handleLedgerResponse(<-network.ledgerResponseInbox)
	}
}

func (network *Network) handleLedgerResponse(resp LedgerResponse) {
	// Merge received events into our ledger
	added := network.local.SocialLedger.MergeEvents(resp.Events)
	if added > 0 {
		logrus.Printf("📥 merged %d events from %s", added, resp.From)
	}
}

// bootRecovery requests social events from neighbors after boot
func (network *Network) bootRecovery() {
	// Wait for initial neighbor discovery
	time.Sleep(30 * time.Second)

	// Retry up to 3 times with backoff if no neighbors found
	var online []string
	for attempt := 0; attempt < 3; attempt++ {
		online = network.NeighbourhoodOnlineNames()
		if len(online) > 0 {
			break
		}
		if attempt < 2 {
			waitTime := time.Duration(30*(attempt+1)) * time.Second
			logrus.Printf("📦 no neighbors for boot recovery, retrying in %v...", waitTime)
			time.Sleep(waitTime)
		}
	}

	if len(online) == 0 {
		logrus.Printf("📦 no neighbors for boot recovery after retries")
		return
	}

	// Try mesh HTTP recovery first
	if network.tsnetMesh != nil {
		network.bootRecoveryViaMesh(online)
		return
	}

	// Fall back to MQTT-based recovery
	network.bootRecoveryViaMQTT(online)
}

// BootRecoveryTargetEvents is the target number of events to fetch on boot
const BootRecoveryTargetEvents = 10000

// bootRecoveryViaMesh uses direct HTTP to sync events from neighbors
func (network *Network) bootRecoveryViaMesh(online []string) {
	// Get all known subjects (naras)
	subjects := append(online, network.meName())

	// Collect ALL mesh-enabled neighbors (no limit)
	var meshNeighbors []struct {
		name string
		ip   string
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

	logrus.Printf("📦 boot recovery via mesh: syncing from %d neighbors (~%d events each)", totalSlices, eventsPerNeighbor)

	// Query each neighbor with interleaved slicing
	var totalMerged int
	client := &http.Client{Timeout: 30 * time.Second}

	for i, neighbor := range meshNeighbors {
		events, verified := network.fetchSyncEventsFromMesh(client, neighbor.ip, neighbor.name, subjects, i, totalSlices, eventsPerNeighbor)
		if len(events) > 0 {
			// Merge into SyncLedger (unified event store)
			added := network.local.SyncLedger.MergeEvents(events)
			totalMerged += added
			verifiedStr := ""
			if verified {
				verifiedStr = " ✓"
			}
			logrus.Printf("📦 mesh sync from %s: received %d events, merged %d%s", neighbor.name, len(events), added, verifiedStr)
		}
	}

	logrus.Printf("📦 boot recovery via mesh complete: merged %d events total", totalMerged)
}

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
	url := fmt.Sprintf("http://%s:7433/events/sync", meshIP)
	resp, err := client.Post(url, "application/json", bytes.NewReader(jsonBody))
	if err != nil {
		logrus.Warnf("📦 mesh sync from %s failed: %v", name, err)
		return nil, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logrus.Warnf("📦 mesh sync from %s returned status %d", name, resp.StatusCode)
		return nil, false
	}

	// Parse response
	var response SyncResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		logrus.Warnf("📦 failed to decode mesh sync response from %s: %v", name, err)
		return nil, false
	}

	// Verify signature if present
	verified := false
	if response.Signature != "" {
		// Look up sender's public key from our neighborhood
		if nara := network.Neighbourhood[name]; nara != nil && nara.Status.PublicKey != "" {
			pubKey, err := ParsePublicKey(nara.Status.PublicKey)
			if err == nil {
				if response.VerifySignature(pubKey) {
					verified = true
				} else {
					logrus.Warnf("📦 signature verification failed for %s", name)
				}
			}
		}
	}

	return response.Events, verified
}

// fetchEventsFromMesh fetches social events from a neighbor via mesh HTTP (legacy)
func (network *Network) fetchEventsFromMesh(client *http.Client, meshIP, name string, subjects []string, sliceIndex, sliceTotal int) []SocialEvent {
	// Build request
	reqBody := map[string]interface{}{
		"from":        network.meName(),
		"subjects":    subjects,
		"since_time":  0, // get all events
		"slice_index": sliceIndex,
		"slice_total": sliceTotal,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		logrus.Warnf("📦 failed to marshal mesh sync request: %v", err)
		return nil
	}

	// Make HTTP request to neighbor's mesh endpoint
	// TODO: Make port configurable or discoverable
	url := fmt.Sprintf("http://%s:7433/events/sync", meshIP) // TODO: use port from mesh.go
	resp, err := client.Post(url, "application/json", bytes.NewReader(jsonBody))
	if err != nil {
		logrus.Warnf("📦 mesh sync from %s failed: %v", name, err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logrus.Warnf("📦 mesh sync from %s returned status %d", name, resp.StatusCode)
		return nil
	}

	// Parse response
	var response struct {
		From   string        `json:"from"`
		Events []SocialEvent `json:"events"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		logrus.Warnf("📦 failed to decode mesh sync response from %s: %v", name, err)
		return nil
	}

	return response.Events
}

// bootRecoveryViaMQTT uses MQTT ledger requests to sync events (fallback)
func (network *Network) bootRecoveryViaMQTT(online []string) {
	// Get all known subjects (naras)
	subjects := append(online, network.meName())

	// Pick up to 5 neighbors to query
	maxNeighbors := 5
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
		logrus.Debugf("📦 requested events about %d subjects from %s", len(partition), neighbor)
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

// continuousMeshSync periodically syncs recent events with mesh neighbors
// This ensures events propagate through the network even after boot recovery
func (network *Network) continuousMeshSync() {
	// Wait for boot recovery to complete first
	time.Sleep(2 * time.Minute)

	// Sync interval: 30-45 seconds (randomized to avoid thundering herd)
	baseTicker := time.NewTicker(30 * time.Second)
	defer baseTicker.Stop()

	client := &http.Client{Timeout: 15 * time.Second}

	for range baseTicker.C {
		if network.tsnetMesh == nil {
			continue // Only sync via mesh
		}

		// Get online mesh neighbors
		online := network.NeighbourhoodNames()
		var meshNeighbors []struct {
			name string
			ip   string
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

		if len(meshNeighbors) == 0 {
			continue
		}

		// Pick ONE random neighbor for this sync
		neighbor := meshNeighbors[time.Now().UnixNano()%int64(len(meshNeighbors))]

		// Request events from last 5 minutes only
		sinceTime := time.Now().Add(-5 * time.Minute).Unix()

		// Build request
		reqBody := SyncRequest{
			From:       network.meName(),
			SinceTime:  sinceTime,
			SliceIndex: 0,
			SliceTotal: 1,
			MaxEvents:  500, // Limit for incremental sync
		}

		jsonBody, err := json.Marshal(reqBody)
		if err != nil {
			continue
		}

		url := fmt.Sprintf("http://%s:7433/events/sync", neighbor.ip)
		resp, err := client.Post(url, "application/json", bytes.NewReader(jsonBody))
		if err != nil {
			continue
		}

		var response SyncResponse
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			resp.Body.Close()
			continue
		}
		resp.Body.Close()

		if len(response.Events) > 0 {
			// Verify signature if present
			verified := false
			if response.Signature != "" {
				if nara := network.Neighbourhood[neighbor.name]; nara != nil && nara.Status.PublicKey != "" {
					if pubKey, err := ParsePublicKey(nara.Status.PublicKey); err == nil {
						verified = response.VerifySignature(pubKey)
					}
				}
			}

			added := network.local.SyncLedger.MergeEvents(response.Events)
			if added > 0 {
				verifiedStr := ""
				if verified {
					verifiedStr = " ✓"
				}
				logrus.Debugf("🔄 continuous sync from %s: +%d events%s", neighbor.name, added, verifiedStr)
			}
		}
	}
}

// socialMaintenance periodically cleans up social data
func (network *Network) socialMaintenance() {
	// Run every 5 minutes
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		<-ticker.C

		// Prune the social ledger
		if network.local.SocialLedger != nil {
			beforeCount := network.local.SocialLedger.EventCount()
			network.local.SocialLedger.Prune()
			afterCount := network.local.SocialLedger.EventCount()

			if beforeCount != afterCount {
				logrus.Printf("🗑️  pruned %d old events (now: %d)", beforeCount-afterCount, afterCount)
			}
		}

		// Cleanup tease cooldowns
		network.TeaseState.Cleanup()
	}
}

// --- Journey Completion Handling ---

func (network *Network) processJourneyCompleteEvents() {
	for {
		network.handleJourneyCompletion(<-network.journeyCompleteInbox)
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
	if network.local.SocialLedger != nil {
		event := NewJourneyObservationEvent(network.meName(), pending.Originator, ReasonJourneyComplete, completion.JourneyID)
		network.local.SocialLedger.AddEvent(event)
	}

	logrus.Printf("observation: journey %s completed! (from %s, reported by %s)", completion.JourneyID[:8], pending.Originator, completion.ReportedBy)
}

// journeyTimeoutMaintenance checks for journeys that have timed out
func (network *Network) journeyTimeoutMaintenance() {
	const journeyTimeout = 5 * time.Minute

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		<-ticker.C

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
			if network.local.SocialLedger != nil {
				event := NewJourneyObservationEvent(network.meName(), pending.Originator, ReasonJourneyTimeout, pending.JourneyID)
				network.local.SocialLedger.AddEvent(event)
			}

			logrus.Printf("observation: journey %s timed out (from %s)", pending.JourneyID[:8], pending.Originator)
		}
	}
}
