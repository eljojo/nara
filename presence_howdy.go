package nara

import (
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/eljojo/nara/types"
)

// NeighborInfo contains information about a neighbor to share in howdy responses
type NeighborInfo struct {
	Name        types.NaraName
	PublicKey   string
	MeshIP      string
	ID          types.NaraID    // Nara ID: deterministic hash of soul+name
	Observation NaraObservation // What I know about this neighbor
}

// HowdyEvent is sent in response to hey_there to help with discovery and start time recovery
type HowdyEvent struct {
	From      types.NaraName  // Who's sending this howdy
	To        types.NaraName  // Who this is in response to (hey_there sender)
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

// howdyCoordinator tracks howdy responses for a given hey_there
type howdyCoordinator struct {
	target         types.NaraName          // who said hey_there
	seen           int                     // how many howdys we've seen for this target
	mentionedNaras map[types.NaraName]bool // naras already mentioned in other howdys
	timer          *time.Timer
	responded      bool
	mu             sync.Mutex
}

// processHowdyEvents handles incoming howdy events from the inbox
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

// handleHowdyEvent processes a single howdy event
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

// startHowdyCoordinator begins the self-selection process to potentially respond with howdy
func (network *Network) startHowdyCoordinator(to types.NaraName) {
	if network.ReadOnly {
		return
	}

	coord := &howdyCoordinator{
		target:         to,
		mentionedNaras: make(map[types.NaraName]bool),
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
func (network *Network) selectNeighborsForHowdy(exclude types.NaraName, alreadyMentioned map[types.NaraName]bool) []NeighborInfo {
	maxNeighbors := 10
	if network.isShortMemoryMode() {
		maxNeighbors = 5
	}

	type naraWithLastSeen struct {
		name     types.NaraName
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
