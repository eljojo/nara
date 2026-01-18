package runtime

import (
	"reflect"
)

// ErrorStrategy defines how to handle errors at each pipeline stage.
type ErrorStrategy int

const (
	ErrorDrop  ErrorStrategy = iota // Drop message silently
	ErrorLog                        // Log warning and drop
	ErrorRetry                      // Retry with exponential backoff
	ErrorQueue                      // Send to dead letter queue for inspection
	ErrorPanic                      // Fail loudly (for critical messages)
)

// Behavior defines how a message kind is handled.
//
// Each message kind (like "stash:store", "checkpoint", "social") has a Behavior
// that declares:
//   - What payload type to deserialize
//   - How to process it when emitting (signing, storage, transport)
//   - How to process it when receiving (verification, dedup, filtering)
//   - Version-specific typed handlers
type Behavior struct {
	// Identity
	Kind        string // Unique identifier, e.g., "observation:restart", "stash:store"
	Description string // Human-readable description

	// Versioning
	CurrentVersion int                  // Version for new messages (default 1)
	MinVersion     int                  // Oldest version still accepted (default 1)
	PayloadTypes   map[int]reflect.Type // Payload type per version (required)

	// Version-specific handlers (typed via TypedHandler helper)
	// Each handler has signature: func(*Message, *PayloadType)
	Handlers map[int]any

	// ContentKey derivation (nil = no content key)
	// Used for cross-observer deduplication (like observations)
	ContentKey func(payload any) string

	// Pipeline stages - split by direction
	Emit    EmitBehavior
	Receive ReceiveBehavior
}

// EmitBehavior defines how outgoing messages are processed.
type EmitBehavior struct {
	Sign      Stage         // How to sign (default: DefaultSign)
	Store     Stage         // How to store (default: DefaultStore(2))
	Gossip    Stage         // Whether to gossip (default: NoGossip)
	Transport Stage         // How to send (required)
	OnError   ErrorStrategy // What to do on failure
}

// ReceiveBehavior defines how incoming messages are processed.
type ReceiveBehavior struct {
	Verify    Stage         // How to verify signature (default: DefaultVerify)
	Dedupe    Stage         // How to deduplicate (default: IDDedupe)
	RateLimit Stage         // Rate limiting (optional)
	Filter    Stage         // Personality filter (optional)
	Store     Stage         // How to store (can differ from emit!)
	OnError   ErrorStrategy // What to do on failure
}

// === Base Defaults (copy and override) ===

// EphemeralDefaults - not stored, not gossiped, unverified
var EphemeralDefaults = Behavior{
	Emit:    EmitBehavior{Sign: NoSign(), Store: NoStore(), Gossip: NoGossip()},
	Receive: ReceiveBehavior{Verify: NoVerify(), Dedupe: IDDedupe(), Store: NoStore()},
}

// ProtocolDefaults - not stored, not gossiped, verified, critical
var ProtocolDefaults = Behavior{
	Emit:    EmitBehavior{Sign: DefaultSign(), Store: NoStore(), Gossip: NoGossip()},
	Receive: ReceiveBehavior{Verify: DefaultVerify(), Dedupe: IDDedupe(), Store: NoStore(), Filter: Critical()},
}

// ProtocolUnverifiedDefaults - like Protocol but no signature verification
var ProtocolUnverifiedDefaults = Behavior{
	Emit:    EmitBehavior{Sign: NoSign(), Store: NoStore(), Gossip: NoGossip()},
	Receive: ReceiveBehavior{Verify: NoVerify(), Dedupe: IDDedupe(), Store: NoStore(), Filter: Critical()},
}

// StoredDefaults - persisted, gossiped, verified
var StoredDefaults = Behavior{
	Emit:    EmitBehavior{Sign: DefaultSign(), Store: DefaultStore(2), Gossip: Gossip(), Transport: NoTransport()},
	Receive: ReceiveBehavior{Verify: DefaultVerify(), Dedupe: IDDedupe(), Store: DefaultStore(2)},
}

// LocalDefaults - for service-to-service communication within a nara
// No network transport, no signing, no storage - just internal event routing
var LocalDefaults = Behavior{
	Emit:    EmitBehavior{Sign: NoSign(), Store: NoStore(), Gossip: NoGossip(), Transport: NoTransport()},
	Receive: ReceiveBehavior{Verify: NoVerify(), Dedupe: IDDedupe(), Store: NoStore()},
}

// === Template functions (copy defaults, override differences) ===

// Ephemeral creates a behavior for ephemeral broadcasts (no storage, MQTT only).
func Ephemeral(kind, desc, topic string) *Behavior {
	b := EphemeralDefaults // struct copy
	b.Kind = kind
	b.Description = desc
	b.Emit.Transport = MQTT(topic)
	return &b
}

// Protocol creates a behavior for protocol messages (not stored, verified, critical).
func Protocol(kind, desc, topic string) *Behavior {
	b := ProtocolDefaults
	b.Kind = kind
	b.Description = desc
	b.Emit.Transport = MQTT(topic)
	return &b
}

// ProtocolUnverified creates a protocol behavior without signature verification.
func ProtocolUnverified(kind, desc, topic string) *Behavior {
	b := ProtocolUnverifiedDefaults
	b.Kind = kind
	b.Description = desc
	b.Emit.Transport = MQTT(topic)
	return &b
}

// MeshRequest creates a behavior for protocol messages over mesh (not MQTT).
func MeshRequest(kind, desc string) *Behavior {
	b := ProtocolDefaults
	b.Kind = kind
	b.Description = desc
	b.Emit.Transport = MeshOnly()
	return &b
}

// StoredEvent creates a behavior for stored, gossiped events (no MQTT broadcast).
func StoredEvent(kind, desc string, priority int) *Behavior {
	b := StoredDefaults
	b.Kind = kind
	b.Description = desc
	b.Emit.Store = DefaultStore(priority)
	b.Receive.Store = DefaultStore(priority)
	return &b
}

// BroadcastEvent creates a behavior for stored, gossiped, AND broadcast via MQTT.
func BroadcastEvent(kind, desc string, priority int, topic string) *Behavior {
	b := StoredDefaults
	b.Kind = kind
	b.Description = desc
	b.Emit.Store = DefaultStore(priority)
	b.Emit.Transport = MQTT(topic)
	b.Receive.Store = DefaultStore(priority)
	return &b
}

// Local creates a behavior for service-to-service communication within a nara.
// No network transport, no signing, no storage - just internal routing.
func Local(kind, desc string) *Behavior {
	b := LocalDefaults
	b.Kind = kind
	b.Description = desc
	return &b
}

// === Chainable modifiers ===

// WithPayload sets the payload type for v1 (defaults to v1).
func (b *Behavior) WithPayload(t reflect.Type) *Behavior {
	b.CurrentVersion = 1
	b.MinVersion = 1
	b.PayloadTypes = map[int]reflect.Type{1: t}
	return b
}

// WithPayloadType is a generic version of WithPayload.
func WithPayloadType[T any]() reflect.Type {
	var zero T
	return reflect.TypeOf(zero)
}

// WithHandler adds a typed handler for a specific version.
// Usage: .WithHandler(1, service.handleV1).WithHandler(2, service.handleV2)
func (b *Behavior) WithHandler(version int, fn any) *Behavior {
	if b.Handlers == nil {
		b.Handlers = make(map[int]any)
	}
	b.Handlers[version] = fn
	return b
}

// TypedHandler wraps a typed handler function for the registry.
func TypedHandler[T any](fn func(*Message, *T)) any {
	return fn
}

// WithContentKey adds ContentKey derivation to a behavior.
func (b *Behavior) WithContentKey(fn func(any) string) *Behavior {
	b.ContentKey = fn
	return b
}

// WithFilter customizes the receive filter.
func (b *Behavior) WithFilter(stage Stage) *Behavior {
	b.Receive.Filter = stage
	return b
}

// WithRateLimit adds rate limiting to the receive pipeline.
func (b *Behavior) WithRateLimit(stage Stage) *Behavior {
	b.Receive.RateLimit = stage
	return b
}

// === Registry ===
// NOTE: Behaviors are now registered per-runtime using Runtime.RegisterBehavior().
// This provides test isolation and allows multiple runtime instances to coexist
// with different behavior handlers.

// PayloadTypeOf is a helper to get reflect.Type from a struct.
func PayloadTypeOf[T any]() reflect.Type {
	var zero T
	return reflect.TypeOf(zero)
}
