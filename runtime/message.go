package runtime

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"time"

	"github.com/eljojo/nara/types"
	"github.com/mr-tron/base58"
)

// Message is the universal primitive for all communication in Nara.
//
// Everything that flows through the system is a Message: stored events,
// ephemeral broadcasts, protocol exchanges, and internal service communication.
type Message struct {
	// Core identity (always present)
	ID         string         `json:"id"`                    // Unique envelope identifier (always unique per message instance)
	ContentKey string         `json:"content_key,omitempty"` // Semantic identity for dedup (optional, stable across observers)
	Kind       string         `json:"kind"`                  // Message type: "hey-there", "observation:restart", "checkpoint", etc.
	Version    int            `json:"version"`               // Schema version for this kind (default 1, increment on breaking changes)
	From       types.NaraName `json:"from,omitempty"`        // Sender name (for display)
	FromID     types.NaraID   `json:"from_id"`               // Sender nara ID (primary identifier)
	To         types.NaraName `json:"to,omitempty"`          // Target name (for direct messages, display only)
	ToID       types.NaraID   `json:"to_id,omitempty"`       // Target nara ID (for direct messages, primary identifier)
	Timestamp  time.Time      `json:"timestamp"`             // When it was created

	// Content
	Payload any `json:"payload"` // Kind-specific data (Go struct, runtime handles serialization)

	// Cryptographic (attached by runtime)
	Signature []byte `json:"signature,omitempty"` // Creator's signature (may be nil for some kinds)

	// Correlation (for Call/response pattern - Chapter 3)
	InReplyTo string `json:"in_reply_to,omitempty"` // Links response to request (for Call/response pattern)
}

// ComputeID generates a unique envelope ID from message content.
//
// The ID is deterministic but always unique per message instance because
// it includes the timestamp with nanosecond precision.
func ComputeID(msg *Message) string {
	h := sha256.New()
	h.Write([]byte(msg.Kind))
	h.Write([]byte(msg.FromID))
	h.Write([]byte(msg.Timestamp.Format(time.RFC3339Nano)))

	// Include payload hash for additional uniqueness
	if msg.Payload != nil {
		payloadBytes, _ := json.Marshal(msg.Payload)
		h.Write(payloadBytes)
	}

	return base58.Encode(h.Sum(nil))[:16]
}

// SignableContent returns the canonical bytes to be signed.
//
// This ensures consistent signing across the network.
func (m *Message) SignableContent() []byte {
	data := struct {
		ID        string       `json:"id"`
		Kind      string       `json:"kind"`
		FromID    types.NaraID `json:"from_id"`
		ToID      types.NaraID `json:"to_id,omitempty"`
		Timestamp time.Time    `json:"timestamp"`
		Payload   any          `json:"payload"`
	}{
		ID:        m.ID,
		Kind:      m.Kind,
		FromID:    m.FromID,
		ToID:      m.ToID,
		Timestamp: m.Timestamp,
		Payload:   m.Payload,
	}

	bytes, _ := json.Marshal(data)
	return bytes
}

// VerifySignature checks if the signature is valid for this message.
func (m *Message) VerifySignature(pubKey []byte) bool {
	if len(m.Signature) == 0 {
		return false
	}
	if len(pubKey) != ed25519.PublicKeySize {
		return false
	}
	return ed25519.Verify(pubKey, m.SignableContent(), m.Signature)
}

// Marshal serializes the message for network transport.
func (m *Message) Marshal() []byte {
	bytes, _ := json.Marshal(m)
	return bytes
}

// Reply creates a response message linked to the original.
//
// Automatically sets InReplyTo, ToID, and swaps the direction.
// Used for request/response patterns (Chapter 3).
func (m *Message) Reply(kind string, payload any) *Message {
	return &Message{
		Kind:      kind,
		Version:   1, // Default to v1 for all replies
		InReplyTo: m.ID,
		ToID:      m.FromID,
		To:        m.From,
		Payload:   payload,
	}
}
