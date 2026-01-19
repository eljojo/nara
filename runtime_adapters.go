package nara

import (
	"fmt"

	"github.com/eljojo/nara/identity"
	"github.com/eljojo/nara/runtime"
	"github.com/eljojo/nara/types"
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
func (a *TransportAdapter) TrySendDirect(targetID types.NaraID, msg *runtime.Message) error {
	// Check if meshClient is available
	if a.network.meshClient == nil {
		return fmt.Errorf("mesh client not initialized")
	}

	// Marshal message
	msgBytes, err := msg.Marshal()
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	// Send via meshClient (which handles test mode automatically)
	ctx := a.network.Context()
	return a.network.meshClient.SendRuntimeMessage(ctx, targetID, msgBytes)
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

// KeypairAdapter bridges identity.NaraKeypair to runtime.KeypairInterface.
type KeypairAdapter struct {
	keypair identity.NaraKeypair
}

// NewKeypairAdapter creates a keypair adapter.
func NewKeypairAdapter(keypair identity.NaraKeypair) *KeypairAdapter {
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

// Seal encrypts plaintext using the keypair's cached encryption key.
func (a *KeypairAdapter) Seal(plaintext []byte) (nonce, ciphertext []byte, err error) {
	return a.keypair.Seal(plaintext)
}

// Open decrypts ciphertext using the keypair's cached encryption key.
func (a *KeypairAdapter) Open(nonce, ciphertext []byte) ([]byte, error) {
	return a.keypair.Open(nonce, ciphertext)
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
func (a *IdentityAdapter) LookupPublicKey(id types.NaraID) []byte {
	return a.network.getPublicKeyForNaraID(id)
}

// RegisterPublicKey registers a public key for a nara ID.
//
// This updates the network's neighbourhood map.
func (a *IdentityAdapter) RegisterPublicKey(id types.NaraID, key []byte) {
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
