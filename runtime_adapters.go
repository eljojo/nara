package nara

import (
	"bytes"
	"fmt"
	"net/http"

	"github.com/eljojo/nara/runtime"
)

// === Transport Adapter ===

// TransportAdapter bridges the old Network transport with runtime.TransportInterface.
type TransportAdapter struct {
	network *Network
}

// NewTransportAdapter creates a transport adapter from a Network.
func NewTransportAdapter(network *Network) *TransportAdapter {
	return &TransportAdapter{network: network}
}

// TrySendDirect sends a message directly to a target nara via mesh.
func (a *TransportAdapter) TrySendDirect(targetID string, msg *runtime.Message) error {
	// Resolve targetID to mesh address
	nara := a.network.getNaraByID(targetID)
	if nara == nil {
		return fmt.Errorf("nara %s not found", targetID)
	}

	// Get mesh IP
	nara.mu.Lock()
	meshIP := nara.Status.MeshIP
	nara.mu.Unlock()

	if meshIP == "" {
		return fmt.Errorf("nara %s has no mesh IP", targetID)
	}

	// Marshal message
	msgBytes := msg.Marshal()

	// Send via mesh HTTP client
	return a.network.sendMeshMessage(meshIP, msgBytes)
}

// PublishMQTT publishes a message to an MQTT topic.
func (a *TransportAdapter) PublishMQTT(topic string, data []byte) error {
	if a.network.Mqtt == nil {
		return fmt.Errorf("MQTT not configured")
	}

	token := a.network.Mqtt.Publish(topic, 1, false, data)
	token.Wait()
	return token.Error()
}

// === Keypair Adapter ===

// KeypairAdapter bridges NaraKeypair to runtime.KeypairInterface.
type KeypairAdapter struct {
	keypair NaraKeypair
}

// NewKeypairAdapter creates a keypair adapter.
func NewKeypairAdapter(keypair NaraKeypair) *KeypairAdapter {
	return &KeypairAdapter{keypair: keypair}
}

// Sign signs data with the keypair.
func (a *KeypairAdapter) Sign(data []byte) []byte {
	return a.keypair.Sign(data)
}

// PublicKey returns the public key.
func (a *KeypairAdapter) PublicKey() []byte {
	return a.keypair.PublicKey
}

// === EventBus Adapter ===

// EventBusAdapter implements runtime.EventBusInterface.
//
// Services can subscribe to message kinds and get notified when they're emitted/received.
type EventBusAdapter struct {
	handlers map[string][]func(*runtime.Message)
}

// NewEventBusAdapter creates an event bus.
func NewEventBusAdapter() *EventBusAdapter {
	return &EventBusAdapter{
		handlers: make(map[string][]func(*runtime.Message)),
	}
}

// Emit notifies all subscribers of a message.
func (e *EventBusAdapter) Emit(msg *runtime.Message) {
	for _, handler := range e.handlers[msg.Kind] {
		handler(msg)
	}
}

// Subscribe registers a handler for a message kind.
func (e *EventBusAdapter) Subscribe(kind string, handler func(*runtime.Message)) {
	e.handlers[kind] = append(e.handlers[kind], handler)
}

// === Identity Adapter ===

// IdentityAdapter provides public key lookups by ID or name.
type IdentityAdapter struct {
	network *Network
}

// NewIdentityAdapter creates an identity adapter.
func NewIdentityAdapter(network *Network) *IdentityAdapter {
	return &IdentityAdapter{network: network}
}

// LookupPublicKey looks up a public key by nara ID.
func (a *IdentityAdapter) LookupPublicKey(id string) []byte {
	return a.network.getPublicKeyForNaraID(id)
}

// LookupPublicKeyByName looks up a public key by nara name.
func (a *IdentityAdapter) LookupPublicKeyByName(name string) []byte {
	return a.network.getPublicKeyForNara(name)
}

// RegisterPublicKey registers a public key for a nara ID.
//
// This updates the network's neighbourhood map.
func (a *IdentityAdapter) RegisterPublicKey(id string, key []byte) {
	// Look up nara by ID
	nara := a.network.getNaraByID(id)
	if nara != nil {
		nara.mu.Lock()
		if nara.Status.PublicKey == "" {
			// Convert bytes to base64 string (NaraStatus stores PublicKey as string)
			nara.Status.PublicKey = string(key)
		}
		nara.mu.Unlock()
	}
}

// === Helper: Send mesh message ===

// sendMeshMessage sends a raw message via mesh HTTP.
func (n *Network) sendMeshMessage(meshIP string, data []byte) error {
	// Use existing mesh HTTP client logic
	url := fmt.Sprintf("http://%s/mesh/message", meshIP)

	// Create a simple HTTP POST request
	client := n.getMeshHTTPClient()
	if client == nil {
		return fmt.Errorf("mesh HTTP client not configured")
	}

	// Use standard HTTP POST
	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return nil
}
