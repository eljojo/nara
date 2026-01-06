package nara

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/sirupsen/logrus"
)

// WorldHop represents a single hop in the world journey
type WorldHop struct {
	Nara      string `json:"nara"`      // Name of the nara
	Timestamp int64  `json:"timestamp"` // Unix timestamp when received
	Signature string `json:"signature"` // Base64 Ed25519 signature
	Stamp     string `json:"stamp"`     // Emoji stamp (token of appreciation)
}

// WorldMessage is a message that travels around the world
type WorldMessage struct {
	ID              string     `json:"id"`         // Unique journey identifier
	OriginalMessage string     `json:"message"`    // The message being carried
	Originator      string     `json:"originator"` // Who started the journey
	Hops            []WorldHop `json:"hops"`       // Chain of signatures and stamps
}

// NewWorldMessage creates a new world message ready to begin its journey
func NewWorldMessage(message string, originator string) *WorldMessage {
	// Generate unique ID from message content and timestamp
	idData := fmt.Sprintf("%s:%s:%d", originator, message, time.Now().UnixNano())
	hash := sha256.Sum256([]byte(idData))

	return &WorldMessage{
		ID:              base64.StdEncoding.EncodeToString(hash[:16]),
		OriginalMessage: message,
		Originator:      originator,
		Hops:            []WorldHop{},
	}
}

// AddHop adds a new hop to the journey, signing the current state
func (wm *WorldMessage) AddHop(nara string, keypair NaraKeypair, stamp string) error {
	// Create the data to sign (all previous state + this nara)
	toSign := wm.dataToSign(nara)
	signature := keypair.Sign(toSign)

	hop := WorldHop{
		Nara:      nara,
		Timestamp: time.Now().Unix(),
		Signature: base64.StdEncoding.EncodeToString(signature),
		Stamp:     stamp,
	}

	wm.Hops = append(wm.Hops, hop)
	return nil
}

// dataToSign creates the canonical data to sign for a hop
func (wm *WorldMessage) dataToSign(nara string) []byte {
	// Include: ID + message + originator + all previous hops + this nara
	data := struct {
		ID              string     `json:"id"`
		OriginalMessage string     `json:"message"`
		Originator      string     `json:"originator"`
		PreviousHops    []WorldHop `json:"previous_hops"`
		CurrentNara     string     `json:"current_nara"`
	}{
		ID:              wm.ID,
		OriginalMessage: wm.OriginalMessage,
		Originator:      wm.Originator,
		PreviousHops:    wm.Hops,
		CurrentNara:     nara,
	}

	jsonData, _ := json.Marshal(data)
	hash := sha256.Sum256(jsonData)
	return hash[:]
}

// dataToSignForHop recreates the data that was signed for a specific hop
func (wm *WorldMessage) dataToSignForHop(hopIndex int) []byte {
	hop := wm.Hops[hopIndex]

	data := struct {
		ID              string     `json:"id"`
		OriginalMessage string     `json:"message"`
		Originator      string     `json:"originator"`
		PreviousHops    []WorldHop `json:"previous_hops"`
		CurrentNara     string     `json:"current_nara"`
	}{
		ID:              wm.ID,
		OriginalMessage: wm.OriginalMessage,
		Originator:      wm.Originator,
		PreviousHops:    wm.Hops[:hopIndex], // All hops before this one
		CurrentNara:     hop.Nara,
	}

	jsonData, _ := json.Marshal(data)
	hash := sha256.Sum256(jsonData)
	return hash[:]
}

// HasVisited returns true if the given nara has already added a hop
func (wm *WorldMessage) HasVisited(nara string) bool {
	for _, hop := range wm.Hops {
		if hop.Nara == nara {
			return true
		}
	}
	return false
}

// IsComplete returns true if the journey has returned to the originator
func (wm *WorldMessage) IsComplete() bool {
	if len(wm.Hops) == 0 {
		return false
	}
	lastHop := wm.Hops[len(wm.Hops)-1]
	return lastHop.Nara == wm.Originator
}

// VerifyChain verifies all signatures in the hop chain
func (wm *WorldMessage) VerifyChain(getPublicKey func(string) []byte) error {
	for i, hop := range wm.Hops {
		pubKey := getPublicKey(hop.Nara)
		if pubKey == nil {
			return fmt.Errorf("unknown public key for %s", hop.Nara)
		}

		signature, err := base64.StdEncoding.DecodeString(hop.Signature)
		if err != nil {
			return fmt.Errorf("invalid signature encoding for %s: %w", hop.Nara, err)
		}

		dataToVerify := wm.dataToSignForHop(i)
		if !VerifySignature(pubKey, dataToVerify, signature) {
			return fmt.Errorf("invalid signature from %s at hop %d", hop.Nara, i)
		}
	}

	return nil
}

// ChooseNextNara selects the next nara based on clout, excluding already visited
// Falls back to arbitrary ordering if no clout data is available
// Routing rule: visit all non-originators first, then return to originator
func ChooseNextNara(currentNara string, wm *WorldMessage, myClout map[string]float64, onlineNaras []string) string {
	// Build list of candidates (online, not visited, not self, not originator)
	type candidate struct {
		name  string
		score float64
	}
	var candidates []candidate

	for _, nara := range onlineNaras {
		if nara == currentNara {
			continue // Skip self
		}
		if nara == wm.Originator {
			continue // Don't visit originator until all others visited
		}
		if wm.HasVisited(nara) {
			continue // Skip already visited
		}

		score := float64(0)
		if myClout != nil {
			score = myClout[nara]
		}
		candidates = append(candidates, candidate{nara, score})
	}

	// If no non-originator candidates left, return to originator
	if len(candidates) == 0 {
		// Only return to originator if they're online and we're not the originator
		if currentNara != wm.Originator {
			for _, nara := range onlineNaras {
				if nara == wm.Originator {
					return wm.Originator
				}
			}
		}
		return "" // Journey stuck
	}

	// Sort by clout descending (if no clout, all scores are 0 so order is arbitrary)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	return candidates[0].name
}

// CalculateWorldRewards calculates clout rewards for a completed journey
func CalculateWorldRewards(wm *WorldMessage) map[string]float64 {
	rewards := make(map[string]float64)

	if !wm.IsComplete() {
		return rewards
	}

	// Originator gets 10 clout
	rewards[wm.Originator] = 10

	// Each participant (except originator's final hop) gets 2 clout
	for _, hop := range wm.Hops {
		if hop.Nara != wm.Originator {
			rewards[hop.Nara] = 2
		}
	}

	return rewards
}

// WorldJourneyHandler manages world journeys for a nara
type WorldJourneyHandler struct {
	localNara      *LocalNara
	mesh           MeshTransport
	getMyClout     func() map[string]float64 // This nara's clout scores for others
	getOnlineNaras func() []string
	getPublicKey   func(string) []byte
	getMeshIP      func(string) string // Resolve nara name to mesh IP
	onComplete     func(*WorldMessage)
	onJourneyPass  func(*WorldMessage) // Called when a journey passes through (before forwarding)
	stamps         []string            // Available stamps for this nara
}

// NewWorldJourneyHandler creates a new journey handler
func NewWorldJourneyHandler(
	localNara *LocalNara,
	mesh MeshTransport,
	getMyClout func() map[string]float64,
	getOnlineNaras func() []string,
	getPublicKey func(string) []byte,
	getMeshIP func(string) string,
	onComplete func(*WorldMessage),
	onJourneyPass func(*WorldMessage),
) *WorldJourneyHandler {
	return &WorldJourneyHandler{
		localNara:      localNara,
		mesh:           mesh,
		getMyClout:     getMyClout,
		getOnlineNaras: getOnlineNaras,
		getPublicKey:   getPublicKey,
		getMeshIP:      getMeshIP,
		onComplete:     onComplete,
		onJourneyPass:  onJourneyPass,
		stamps:         defaultStamps(),
	}
}

// defaultStamps returns a list of emoji stamps
func defaultStamps() []string {
	return []string{"🌟", "🎉", "🚀", "💫", "✨", "🌈", "🎊", "💖", "🌸", "🔥"}
}

// pickStamp chooses a stamp for this hop
func (h *WorldJourneyHandler) pickStamp(wm *WorldMessage) string {
	// Use hash of message ID + hop count for determinism
	idx := len(wm.Hops) % len(h.stamps)
	return h.stamps[idx]
}

// sendToNara sends a message to a nara, resolving name to IP if available
func (h *WorldJourneyHandler) sendToNara(name string, wm *WorldMessage) error {
	target := name
	if h.getMeshIP != nil {
		if ip := h.getMeshIP(name); ip != "" {
			target = ip
			logrus.Debugf("🌍 Resolved %s -> %s", name, ip)
		}
	}
	return h.mesh.Send(target, wm)
}

// StartJourney begins a new world journey
func (h *WorldJourneyHandler) StartJourney(message string) (*WorldMessage, error) {
	wm := NewWorldMessage(message, h.localNara.Me.Name)

	// Choose first recipient
	next := h.chooseNext(wm)
	if next == "" {
		return nil, fmt.Errorf("no naras available to start journey")
	}

	// Send to first hop
	if err := h.sendToNara(next, wm); err != nil {
		return nil, fmt.Errorf("failed to send to %s: %w", next, err)
	}

	return wm, nil
}

// HandleIncoming processes an incoming world message
func (h *WorldJourneyHandler) HandleIncoming(wm *WorldMessage) error {
	myName := h.localNara.Me.Name

	// Verify the chain so far
	if err := wm.VerifyChain(h.getPublicKey); err != nil {
		return fmt.Errorf("chain verification failed: %w", err)
	}

	// Add our hop with signature and stamp
	stamp := h.pickStamp(wm)
	if err := wm.AddHop(myName, h.localNara.Keypair, stamp); err != nil {
		return fmt.Errorf("failed to add hop: %w", err)
	}

	// Check if journey is complete (returned to originator)
	if wm.IsComplete() {
		// We're the originator and it came back!
		if h.onComplete != nil {
			h.onComplete(wm)
		}
		return nil
	}

	// Notify that a journey passed through us (before forwarding)
	if h.onJourneyPass != nil {
		h.onJourneyPass(wm)
	}

	// Choose next recipient
	next := h.chooseNext(wm)
	if next == "" {
		return fmt.Errorf("journey stuck - no next nara available")
	}

	// Forward the message
	if err := h.sendToNara(next, wm); err != nil {
		return fmt.Errorf("failed to forward to %s: %w", next, err)
	}

	return nil
}

// chooseNext selects the next nara based on clout
func (h *WorldJourneyHandler) chooseNext(wm *WorldMessage) string {
	myClout := h.getMyClout()
	online := h.getOnlineNaras()
	logrus.Debugf("🌍 World journey: %d online naras (mesh-enabled): %v", len(online), online)
	next := ChooseNextNara(h.localNara.Me.Name, wm, myClout, online)
	if next == "" {
		logrus.Debugf("🌍 World journey: no next nara available (self=%s, hops=%d)", h.localNara.Me.Name, len(wm.Hops))
	}
	return next
}

// Listen starts listening for incoming world messages
func (h *WorldJourneyHandler) Listen() {
	go func() {
		for msg := range h.mesh.Receive() {
			if err := h.HandleIncoming(msg); err != nil {
				// Log error but continue
				fmt.Printf("world journey error: %v\n", err)
			}
		}
	}()
}
