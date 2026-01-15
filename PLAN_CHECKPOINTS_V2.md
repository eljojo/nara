# Implementation Plan: Nara Runtime Architecture

A step-by-step guide for implementing the runtime architecture defined in `DESIGN_NARA_RUNTIME.md`.

---

## Overview

This plan is divided into two chapters:

- **Chapter 1: Stash Service** — Build the minimum runtime needed for stash to work _really well_. Full-fledged for stash, but just for stash.
- **Chapter 2: Everything After Stash** — Migrate remaining services, complete the runtime, cleanup.

**Why this split?** Stash is the perfect first target:
- Not in production yet — no backwards compatibility baggage
- Self-contained — doesn't depend on other services
- Tests the new architecture end-to-end (mesh transport, request/response, encryption)
- Validates the design before committing to migrate everything else

**References:**
- `DESIGN_NARA_RUNTIME.md` — Full runtime design details
- `DESIGN_SERVICE_UTILITIES.md` — Opt-in utility patterns
- `DESIGN_CRITIQUE.md` — Risks and mitigations

---

# Chapter 1: Stash Service

**Goal:** Get stash working beautifully on the new runtime architecture.

Everything in Chapter 1 is scoped to what stash needs. Other services stay on the existing architecture until Chapter 2.

---

## Prerequisites

Before starting, ensure you understand:
1. The current stash implementation (`stash_*.go` files)
2. The mesh transport layer (`transport_mesh.go`, `http_mesh.go`)
3. The existing identity/crypto code (`identity_crypto.go`)

---

## Phase 1: Core Types (Stash-Scoped)

**Goal:** Create the foundational types needed for stash.

### Step 1.0: Create `messages/` package — Stash Types Only

Create the central package for message payload types. For Chapter 1, we only add stash-related types.

```
messages/
├── doc.go           # Package overview, how to add new messages
└── stash.go         # StashStorePayload, StashRequestPayload, StashStoreAck, etc.
```

**Note:** Other message types (checkpoint.go, social.go, presence.go, etc.) are added in Chapter 2.

**stash.go contents:**
```go
package messages

import "errors"

// StashStorePayload is sent when storing encrypted data with a confidant.
//
// Kind: stash:store
// Flow: Owner → Confidant
// Response: StashStoreAck
// Transport: MeshOnly
//
// Version History:
//   v1 (2024-01): Initial version
type StashStorePayload struct {
    Owner      string `json:"owner"`      // Who the stash belongs to
    OwnerID    string `json:"owner_id"`   // Owner's nara ID (primary identifier)
    Nonce      []byte `json:"nonce"`      // 24-byte XChaCha20 nonce
    Ciphertext []byte `json:"ciphertext"` // Encrypted stash data
    Timestamp  int64  `json:"ts"`         // When created
}

func (p *StashStorePayload) Validate() error {
    if p.OwnerID == "" && p.Owner == "" {
        return errors.New("owner_id or owner required")
    }
    if len(p.Nonce) != 24 {
        return errors.New("nonce must be 24 bytes")
    }
    if len(p.Ciphertext) == 0 {
        return errors.New("ciphertext required")
    }
    return nil
}

// StashStoreAck acknowledges successful storage.
//
// Kind: stash:ack
// Flow: Confidant → Owner (response to stash:store)
type StashStoreAck struct {
    StoredAt int64  `json:"stored_at"` // When the confidant stored the data
    OwnerID  string `json:"owner_id"`  // Echoed back for correlation
}

// StashRequestPayload requests stored data from a confidant.
//
// Kind: stash:request
// Flow: Owner → Confidant
// Response: StashResponsePayload
type StashRequestPayload struct {
    OwnerID   string `json:"owner_id"`   // Who is requesting their stash
    RequestID string `json:"request_id"` // For correlation
}

func (p *StashRequestPayload) Validate() error {
    if p.OwnerID == "" {
        return errors.New("owner_id required")
    }
    return nil
}

// StashResponsePayload returns stored data to the owner.
//
// Kind: stash:response
// Flow: Confidant → Owner (response to stash:request)
type StashResponsePayload struct {
    OwnerID    string `json:"owner_id"`
    RequestID  string `json:"request_id"` // Echoed from request
    Nonce      []byte `json:"nonce"`
    Ciphertext []byte `json:"ciphertext"`
    StoredAt   int64  `json:"stored_at"`
    Found      bool   `json:"found"` // False if confidant has no stash for this owner
}

// StashRefreshPayload triggers stash recovery from confidants.
//
// Kind: stash-refresh
// Flow: Broadcast via MQTT
// Transport: MQTT (ephemeral, not stored)
type StashRefreshPayload struct {
    OwnerID string `json:"owner_id"` // Who wants their stash back
}
```

**Import rules:**
- `messages/` has NO dependencies (pure data types)
- `runtime/` imports `messages/`
- Services import both

### Step 1.1: Create `runtime/` package structure

Create the directory structure:
```
runtime/
├── environment.go   # Environment enum (Production, Development, Test)
├── message.go       # Message struct
├── stage.go         # Stage interface, StageResult, Pipeline
├── behavior.go      # Behavior struct, Registry
├── helpers.go       # DSL helper constructors
├── runtime.go       # Runtime struct (stub)
└── runtime_test.go  # Tests
```

**Environment (like Rails):**
```go
// runtime/environment.go
type Environment int

const (
    EnvProduction Environment = iota  // Graceful: log errors, don't crash
    EnvDevelopment                     // Loud: warnings, fail on suspicious things
    EnvTest                            // Strict: panic on errors, catch bugs early
)

// Detected from NARA_ENV or explicit in RuntimeConfig
func (rt *Runtime) Env() Environment
func (rt *Runtime) IsProd() bool
func (rt *Runtime) IsDev() bool
func (rt *Runtime) IsTest() bool
```

**Environment-aware defaults:**

| Behavior | Production | Development | Test |
|----------|------------|-------------|------|
| Error strategy | Log | LogWarn | Panic |
| Logger | Batched | Verbose | Captured |
| Timeouts | 30s | 10s | 1s |
| Validation | Log | Warn | Reject |

### Step 1.2: Implement Message

Create `runtime/message.go`:

```go
package runtime

import (
    "crypto/sha256"
    "time"
)

// Message is the universal primitive
type Message struct {
    ID         string    // Unique envelope identifier (always unique)
    ContentKey string    // Semantic identity for dedup (optional)
    Kind       string
    Version    int       // Schema version (default 1, increment on breaking changes)
    From       string    // Sender name (for display)
    FromID     string    // Sender nara ID (primary identifier)
    To         string    // Target name (for direct messages)
    ToID       string    // Target nara ID (primary identifier)
    Timestamp  time.Time
    Payload    any
    Signature  []byte
}

// DefaultComputeID generates deterministic ID from content
func DefaultComputeID(msg *Message) string {
    h := sha256.New()
    h.Write([]byte(msg.Kind))
    h.Write([]byte(msg.FromID))
    h.Write([]byte(msg.Timestamp.Format(time.RFC3339Nano)))
    h.Write(payloadHash(msg.Payload))
    return base58.Encode(h.Sum(nil))[:16]
}

// SignableContent returns the content to be signed
func (m *Message) SignableContent() []byte {
    // Implementation: serialize ID, Kind, FromID, ToID, Timestamp, Payload
}

// VerifySignature checks if the signature is valid
func (m *Message) VerifySignature(pubKey []byte) bool {
    // Implementation: verify using ed25519
}

// Marshal serializes the message for transport
func (m *Message) Marshal() []byte {
    // Implementation: JSON marshal
}
```

**Test:** Write unit tests for `DefaultComputeID`, `SignableContent`, `Marshal`.

### Step 1.3: Implement StageResult and Pipeline

Create `runtime/stage.go`:

```go
package runtime

// StageResult represents the outcome of a stage
type StageResult struct {
    Message *Message
    Error   error
    Reason  string
}

// Convenience constructors
func Continue(msg *Message) StageResult { return StageResult{Message: msg} }
func Drop(reason string) StageResult    { return StageResult{Reason: reason} }
func Fail(err error) StageResult        { return StageResult{Error: err} }

// IsContinue returns true if the result indicates continuation
func (r StageResult) IsContinue() bool { return r.Message != nil && r.Error == nil }

// IsDrop returns true if the result indicates an intentional drop
func (r StageResult) IsDrop() bool { return r.Message == nil && r.Error == nil }

// IsError returns true if the result indicates an error
func (r StageResult) IsError() bool { return r.Error != nil }

// Stage processes a message and returns an explicit result
type Stage interface {
    Process(msg *Message, ctx *PipelineContext) StageResult
}

// PipelineContext carries runtime dependencies
type PipelineContext struct {
    Runtime     RuntimeInterface  // Interface, not concrete type
    Ledger      LedgerInterface
    Transport   TransportInterface
    GossipQueue GossipQueueInterface
    Keypair     KeypairInterface
    Personality *Personality
    EventBus    EventBusInterface
}

// Pipeline chains stages
type Pipeline []Stage

func (p Pipeline) Run(msg *Message, ctx *PipelineContext) StageResult {
    for _, stage := range p {
        result := stage.Process(msg, ctx)
        if result.Error != nil {
            return result
        }
        if result.Message == nil {
            return result
        }
        msg = result.Message
    }
    return Continue(msg)
}
```

**Test:** Write unit tests for `Pipeline.Run` with mock stages:
- All stages continue → final message returned
- Middle stage drops → drop result returned with reason
- Middle stage errors → error result returned

### Step 1.4: Implement Behavior and Registry

Create `runtime/behavior.go`:

```go
package runtime

import (
    "errors"
    "fmt"
    "reflect"
    "sync"
)

// ErrorStrategy defines how to handle errors
type ErrorStrategy int

const (
    ErrorDrop ErrorStrategy = iota
    ErrorLog
    ErrorRetry
    ErrorQueue
    ErrorPanic
)

// Behavior defines how a message kind is handled
type Behavior struct {
    Kind        string
    Description string

    // Versioning
    CurrentVersion int                    // Version for new messages (default 1)
    MinVersion     int                    // Oldest version still accepted (default 1)
    PayloadTypes   map[int]reflect.Type   // Payload type per version (required)

    // Version-specific handlers (typed via TypedHandler helper)
    // Each handler has signature: func(*Message, *PayloadType)
    Handlers map[int]any

    // ContentKey derivation (nil = no content key)
    ContentKey func(payload any) string

    // Pipeline stages - split by direction
    Emit    EmitBehavior
    Receive ReceiveBehavior
}

// EmitBehavior defines how outgoing messages are processed
type EmitBehavior struct {
    Sign      Stage         // How to sign (default: DefaultSign)
    Store     Stage         // How to store (default: DefaultStore(2))
    Gossip    Stage         // Whether to gossip (default: NoGossip)
    Transport Stage         // How to send (required)
    OnError   ErrorStrategy // What to do on failure
}

// ReceiveBehavior defines how incoming messages are processed
type ReceiveBehavior struct {
    Verify    Stage         // How to verify signature (default: DefaultVerify)
    Dedupe    Stage         // How to deduplicate (default: IDDedupe)
    RateLimit Stage         // Rate limiting (optional)
    Filter    Stage         // Personality filter (optional)
    Store     Stage         // How to store (can differ from emit!)
    OnError   ErrorStrategy // What to do on failure
}

// === Base Defaults (copy and override) ===

var EphemeralDefaults = Behavior{
    Emit:    EmitBehavior{Sign: NoSign(), Store: NoStore(), Gossip: NoGossip()},
    Receive: ReceiveBehavior{Verify: NoVerify(), Dedupe: IDDedupe(), Store: NoStore()},
}

var ProtocolDefaults = Behavior{
    Emit:    EmitBehavior{Sign: DefaultSign(), Store: NoStore(), Gossip: NoGossip()},
    Receive: ReceiveBehavior{Verify: DefaultVerify(), Dedupe: IDDedupe(), Store: NoStore(), Filter: Critical()},
}

// LocalDefaults - for service-to-service communication within nara
// No network transport, no signing, no storage - just internal event routing
var LocalDefaults = Behavior{
    Emit:    EmitBehavior{Sign: NoSign(), Store: NoStore(), Gossip: NoGossip(), Transport: NoTransport()},
    Receive: ReceiveBehavior{Verify: NoVerify(), Dedupe: IDDedupe(), Store: NoStore()},
}

// === Template functions (copy defaults, override differences) ===

func Ephemeral(kind, desc, topic string) *Behavior { /* copy EphemeralDefaults, set Kind/Desc/Transport */ }
func Protocol(kind, desc, topic string) *Behavior { /* copy ProtocolDefaults, set Kind/Desc/Transport */ }
func MeshRequest(kind, desc string) *Behavior { /* copy ProtocolDefaults, set Transport: MeshOnly() */ }
func Local(kind, desc string) *Behavior { /* copy LocalDefaults, set Kind/Desc - no transport needed */ }
func StoredEvent(kind, desc string, priority int) *Behavior { /* ... */ }
func BroadcastEvent(kind, desc string, priority int, topic string) *Behavior { /* ... */ }

// Chainable modifiers
func (b *Behavior) WithPayload[T any]() *Behavior { /* ... */ }
func (b *Behavior) WithHandler[T any](version int, fn func(*Message, *T)) *Behavior { /* ... */ }
func (b *Behavior) WithContentKey(fn func(any) string) *Behavior { /* ... */ }
func (b *Behavior) WithFilter(stage Stage) *Behavior { /* ... */ }
func (b *Behavior) WithRateLimit(stage Stage) *Behavior { /* ... */ }

// TypedHandler wraps a typed handler function for the registry
func TypedHandler[T any](fn func(*Message, *T)) any { return fn }

// Registry
var (
    behaviors   = make(map[string]*Behavior)
    behaviorsMu sync.RWMutex
)

func Register(b *Behavior) error {
    if b.Kind == "" {
        return errors.New("behavior must have a Kind")
    }
    behaviorsMu.Lock()
    defer behaviorsMu.Unlock()
    if behaviors[b.Kind] != nil {
        return fmt.Errorf("behavior %s already registered", b.Kind)
    }
    behaviors[b.Kind] = b
    return nil
}

func Lookup(kind string) *Behavior {
    behaviorsMu.RLock()
    defer behaviorsMu.RUnlock()
    return behaviors[kind]
}

func AllBehaviors() map[string]*Behavior {
    behaviorsMu.RLock()
    defer behaviorsMu.RUnlock()
    result := make(map[string]*Behavior, len(behaviors))
    for k, v := range behaviors {
        result[k] = v
    }
    return result
}

// ClearRegistry is for testing only
func ClearRegistry() {
    behaviorsMu.Lock()
    defer behaviorsMu.Unlock()
    behaviors = make(map[string]*Behavior)
}

// PayloadTypeOf is a helper to get reflect.Type from a struct
func PayloadTypeOf[T any]() reflect.Type {
    var zero T
    return reflect.TypeOf(zero)
}
```

**Test:** Write unit tests for `Register`, `Lookup`, duplicate registration.

### Step 1.5: Implement Helper Constructors (Stash-Relevant Only)

Create `runtime/helpers.go` with helpers needed for stash:
- `DefaultSign()`, `NoSign()`
- `NoStore()` (stash doesn't need ledger storage)
- `NoGossip()` (stash doesn't use gossip)
- `MeshOnly()` (stash is mesh-only)
- `DefaultVerify()`
- `IDDedupe()`

**Note:** Other helpers (MQTT, Gossip, personality filters, etc.) are added in Chapter 2.

**Test:** Write unit tests for each stage type in isolation.

---

## Phase 2: Stash-Relevant Stages

**Goal:** Implement only the stages stash needs.

### Step 2.1: Emit Stages for Stash

Create `runtime/stages_emit.go` with:

1. **IDStage** - computes unique envelope ID
2. **DefaultSignStage** - signs with keypair from context
3. **NoStoreStage** - no-op (stash messages aren't stored in ledger)
4. **NoGossipStage** - no-op (stash doesn't use gossip)
5. **MeshOnlyStage** - sends directly via mesh, fails if unreachable
6. **NotifyStage** - emits to event bus for internal handlers

**Test each stage individually** with mock dependencies.

### Step 2.2: Receive Stages for Stash

Create `runtime/stages_receive.go` with:

1. **DefaultVerifyStage** - verifies signature against known public key (by ID)
2. **IDDedupeStage** - rejects messages with duplicate ID

**Note:** Personality filtering, rate limiting, content-key dedup — not needed for stash. Added in Chapter 2.

**Test each stage individually** with mock dependencies.

---

## Phase 3: Runtime Core (Minimal)

**Goal:** Implement the minimum Runtime for stash.

### Step 3.1: Define Interfaces

Create `runtime/interfaces.go`:

```go
package runtime

// RuntimeInterface is what services and stages can access
type RuntimeInterface interface {
    // Identity
    Me() *Nara
    MeID() string

    // Public key management
    LookupPublicKey(id string) []byte
    LookupPublicKeyByName(name string) []byte
    RegisterPublicKey(id string, key []byte)

    // Messaging (including service-to-service via Local messages)
    Emit(msg *Message) error

    // Logging (runtime primitive, not a service)
    Log(service string) *ServiceLog
}

// LedgerInterface is what store stages use
type LedgerInterface interface {
    Add(msg *Message, priority int) error
    HasID(id string) bool
    HasContentKey(contentKey string) bool
}

// TransportInterface is what transport stages use
type TransportInterface interface {
    PublishMQTT(topic string, data []byte) error
    TrySendDirect(targetID string, msg *Message) error
}

// GossipQueueInterface is what gossip stages use (Chapter 2)
type GossipQueueInterface interface {
    Add(msg *Message)
}

// KeypairInterface is what sign stages use
type KeypairInterface interface {
    Sign(data []byte) []byte
}
```

### Step 3.1.1: Logger as Runtime Primitive

Logger is a **runtime primitive**, not a Service. Services get a logger handle from the runtime:

```go
// runtime/logger.go

// Logger is the central logging coordinator (owned by Runtime)
type Logger struct {
    services map[string]*ServiceLog
    mu       sync.RWMutex
}

// ServiceLog is what each service gets
type ServiceLog struct {
    name   string
    logger *Logger
}

func (l *Logger) For(service string) *ServiceLog {
    l.mu.Lock()
    defer l.mu.Unlock()
    if log, ok := l.services[service]; ok {
        return log
    }
    log := &ServiceLog{name: service, logger: l}
    l.services[service] = log
    return log
}

// ServiceLog methods
func (l *ServiceLog) Debug(format string, args ...any) { /* ... */ }
func (l *ServiceLog) Info(format string, args ...any)  { /* ... */ }
func (l *ServiceLog) Warn(format string, args ...any)  { /* ... */ }
func (l *ServiceLog) Error(format string, args ...any) { /* ... */ }

// Structured events (batched)
func (l *ServiceLog) Event(eventType, actor string, opts ...LogOption) { /* ... */ }
```

**Usage in services:**

```go
func (s *StashService) Init(rt *Runtime) error {
    s.rt = rt
    s.log = rt.Log("stash")  // Get logger from runtime
    return nil
}

func (s *StashService) handleStoreV1(msg *Message, p *StashStorePayload) {
    s.log.Debug("received store from %s", msg.FromID)
    // ... do work ...
    s.log.Event("store", msg.FromID, WithMessage("stored 2.3KB"))
}
```

### Step 3.1.2: Service-to-Service Communication via Local Messages

Services communicate with each other using the same Message primitive with `Local()` behaviors.
No network transport, no signing — just internal event routing through the runtime.

**Why use messages for internal communication?**
- Services are decoupled — no direct imports between services
- Communication is explicit and observable (can log/trace all internal messages)
- Same patterns work (versioning, typed handlers)
- Easy to test — MockRuntime captures internal messages too

**Example: Stash notifies other services when recovery completes**

```go
// messages/stash.go - add internal event payload
type StashRecoveredPayload struct {
    Size      int   `json:"size"`
    Timestamp int64 `json:"ts"`
}

// stash/behaviors.go - register local event
Register(Local("stash:recovered", "Stash recovery completed").
    WithPayload[messages.StashRecoveredPayload]())

// stash/service.go - emit when recovery completes
func (s *StashService) completeRecovery(data []byte) {
    s.data = data
    s.rt.Emit(&Message{
        Kind:    "stash:recovered",
        Payload: &messages.StashRecoveredPayload{
            Size:      len(data),
            Timestamp: time.Now().Unix(),
        },
    })
}

// presence/behaviors.go - another service subscribes
Register(Local("stash:recovered", "Stash recovery completed").
    WithPayload[messages.StashRecoveredPayload]().
    WithHandler(1, p.handleStashRecovered))

// presence/service.go - react to stash events
func (p *PresenceService) handleStashRecovered(msg *Message, payload *messages.StashRecoveredPayload) {
    p.log.Info("stash recovered, %d bytes", payload.Size)
    // Maybe announce presence now that we have state
    p.announceIdentity()
}
```

**How it works internally:**

```go
func (rt *Runtime) Emit(msg *Message) error {
    behavior := Lookup(msg.Kind)
    pipeline := rt.buildEmitPipeline(behavior)
    result := pipeline.Run(msg, ctx)

    // NotifyStage at the end invokes handlers for ALL messages.
    // For Local messages, Transport is NoTransport() so no network send happens.
    // Handlers still get called — that's how service-to-service works.
    return nil
}
```

### Step 3.2: Implement MockRuntime (for service tests)

Create `runtime/mock_runtime.go` for testing stash service without MQTT/mesh:

```go
package runtime

import "testing"

// MockRuntime implements RuntimeInterface for testing services
type MockRuntime struct {
    t           *testing.T           // For auto-cleanup and assertions
    name        string
    id          string
    Emitted     []*Message           // Captured Emit() calls
    handlers    map[string][]func(*Message)
    keypair     *MockKeypair
}

// NewMockRuntime creates a mock runtime with auto-cleanup via t.Cleanup()
func NewMockRuntime(t *testing.T, name, id string) *MockRuntime {
    t.Helper()
    mock := &MockRuntime{
        t:        t,
        name:     name,
        id:       id,
        Emitted:  make([]*Message, 0),
        handlers: make(map[string][]func(*Message)),
        keypair:  NewMockKeypair(),
    }

    t.Cleanup(func() {
        mock.Stop()
    })

    return mock
}

func (m *MockRuntime) Stop() {
    m.handlers = nil
}

func (m *MockRuntime) MeID() string { return m.id }

// Emit captures messages for test assertions
func (m *MockRuntime) Emit(msg *Message) error {
    if msg.ID == "" {
        msg.ID = DefaultComputeID(msg)
    }
    if msg.FromID == "" {
        msg.FromID = m.id
    }
    m.Emitted = append(m.Emitted, msg)
    return nil
}

// Deliver simulates receiving a message (calls service handlers)
func (m *MockRuntime) Deliver(msg *Message) {
    for _, handler := range m.handlers[msg.Kind] {
        handler(msg)
    }
}

// Subscribe registers a handler for a message kind
func (m *MockRuntime) Subscribe(kind string, handler func(*Message)) {
    m.handlers[kind] = append(m.handlers[kind], handler)
}

// Me returns a fake Nara for the mock
func (m *MockRuntime) Me() *Nara {
    return &Nara{Name: m.name, ID: m.id}
}

// Test helpers
func (m *MockRuntime) EmittedCount() int { return len(m.Emitted) }
func (m *MockRuntime) LastEmitted() *Message {
    if len(m.Emitted) == 0 { return nil }
    return m.Emitted[len(m.Emitted)-1]
}
func (m *MockRuntime) EmittedOfKind(kind string) []*Message {
    var result []*Message
    for _, msg := range m.Emitted {
        if msg.Kind == kind {
            result = append(result, msg)
        }
    }
    return result
}
func (m *MockRuntime) Clear() { m.Emitted = make([]*Message, 0) }
```

### Step 3.3: Implement Runtime (Minimal)

Create `runtime/runtime.go` with only what stash needs:

```go
package runtime

import (
    "context"
    "fmt"
)

type Runtime struct {
    me          *Nara
    keypair     KeypairInterface
    ledger      LedgerInterface    // May be nil for stash
    transport   TransportInterface
    eventBus    EventBusInterface
    personality *Personality

    services []Service
    handlers map[string][]MessageHandler

    ctx    context.Context
    cancel context.CancelFunc
}

type RuntimeConfig struct {
    Me          *Nara
    Keypair     KeypairInterface
    Ledger      LedgerInterface    // Optional for stash
    Transport   TransportInterface
    EventBus    EventBusInterface
    Personality *Personality
}

func NewRuntime(cfg RuntimeConfig) *Runtime {
    return &Runtime{
        me:          cfg.Me,
        keypair:     cfg.Keypair,
        ledger:      cfg.Ledger,
        transport:   cfg.Transport,
        eventBus:    cfg.EventBus,
        personality: cfg.Personality,
        handlers:    make(map[string][]MessageHandler),
    }
}

func (rt *Runtime) MeID() string {
    if rt.me != nil {
        return rt.me.ID
    }
    return ""
}

func (rt *Runtime) Emit(msg *Message) error {
    if msg.Timestamp.IsZero() {
        msg.Timestamp = time.Now()
    }
    if msg.FromID == "" {
        msg.FromID = rt.MeID()
    }

    behavior := Lookup(msg.Kind)
    if behavior == nil {
        return fmt.Errorf("unknown message kind: %s", msg.Kind)
    }

    // Set version to current if not specified
    if msg.Version == 0 {
        msg.Version = behavior.CurrentVersion
        if msg.Version == 0 {
            msg.Version = 1
        }
    }

    pipeline := rt.buildEmitPipeline(behavior)
    ctx := rt.newPipelineContext()

    result := pipeline.Run(msg, ctx)

    if result.Error != nil {
        return result.Error
    }
    return nil
}

func (rt *Runtime) Receive(raw []byte) error {
    msg, err := rt.deserialize(raw)
    if err != nil {
        return fmt.Errorf("deserialize: %w", err)
    }

    behavior := Lookup(msg.Kind)
    if behavior == nil {
        return fmt.Errorf("unknown message kind: %s", msg.Kind)
    }

    pipeline := rt.buildReceivePipeline(behavior)
    ctx := rt.newPipelineContext()

    result := pipeline.Run(msg, ctx)

    if result.Error != nil {
        return result.Error
    }
    return nil
}

// ... pipeline building methods ...
```

**Test:** Integration tests for Emit and Receive.

---

## Phase 4: Adapter Layer (Stash-Relevant)

**Goal:** Create adapters to bridge existing mesh code with new runtime.

### Step 4.1: Create Transport Adapter

Wrap existing mesh transport for stash:

```go
type MeshTransportAdapter struct {
    mesh *MeshClient
    network *Network  // For peer resolution by ID
}

func (a *MeshTransportAdapter) TrySendDirect(targetID string, msg *runtime.Message) error {
    // Resolve targetID to mesh address
    addr := a.network.getMeshAddressByID(targetID)
    if addr == "" {
        return fmt.Errorf("no mesh address for nara ID %s", targetID)
    }
    // Send via mesh
    return a.mesh.Send(addr, msg.Marshal())
}
```

### Step 4.2: Create Identity Adapter

Wrap existing identity code for public key lookups by ID:

```go
type IdentityAdapter struct {
    network *Network
}

func (a *IdentityAdapter) LookupPublicKey(id string) []byte {
    return a.network.getPublicKeyForNaraID(id)
}
```

---

## Phase 5: Migrate Stash Service

**Goal:** Port stash to the new runtime architecture.

### Step 5.0: Create Service Utilities

Create utilities stash needs (see `DESIGN_SERVICE_UTILITIES.md`):

```
utilities/
├── correlator.go       # Request/response correlation
├── encryptor.go        # XChaCha20-Poly1305 encryption (extract from identity_crypto.go)
└── utilities_test.go   # Tests
```

**Correlator** - Generic request/response correlation:
```go
type Correlator[Resp any] struct { ... }
func NewCorrelator[Resp any](timeout time.Duration) *Correlator[Resp]
func (c *Correlator[Resp]) Send(rt *Runtime, msg *Message) <-chan Result[Resp]
func (c *Correlator[Resp]) Receive(requestID string, resp Resp) bool
```

**Encryptor** - Self-encryption (extracted from `identity_crypto.go`):
```go
type Encryptor struct { ... }
func NewEncryptor(seed []byte) *Encryptor
func (e *Encryptor) Seal(plaintext []byte) (nonce, ciphertext []byte, err error)
func (e *Encryptor) Open(nonce, ciphertext []byte) ([]byte, error)
```

**Test each utility in isolation** before using in stash service.

### Step 5.1: Register Stash Behaviors

Create behavior registrations in new `stash/behaviors.go`:

```go
package stash

import (
    rt "nara/runtime"
    "nara/messages"
)

// RegisterBehaviors registers stash message behaviors with version-specific handlers
func (s *StashService) RegisterBehaviors(runtime *rt.Runtime) {
    // Ephemeral broadcast: trigger stash recovery from confidants
    runtime.Register(
        rt.Ephemeral("stash-refresh", "Request stash recovery", "nara/plaza/stash_refresh").
            WithPayload[messages.StashRefreshPayload]().
            WithHandler(1, s.handleRefreshV1),
    )

    // Mesh-only request/response messages with typed handlers
    runtime.Register(
        rt.MeshRequest("stash:store", "Store encrypted stash").
            WithPayload[messages.StashStorePayload]().
            WithHandler(1, s.handleStoreV1),
    )
    runtime.Register(
        rt.MeshRequest("stash:ack", "Acknowledge stash storage").
            WithPayload[messages.StashStoreAck]().
            WithHandler(1, s.handleStoreAckV1),
    )
    runtime.Register(
        rt.MeshRequest("stash:request", "Request stored stash").
            WithPayload[messages.StashRequestPayload]().
            WithHandler(1, s.handleRequestV1),
    )
    runtime.Register(
        rt.MeshRequest("stash:response", "Return stored stash").
            WithPayload[messages.StashResponsePayload]().
            WithHandler(1, s.handleResponseV1),
    )
}

// Version-specific typed handlers - no type switches needed
func (s *StashService) handleStoreV1(msg *rt.Message, p *messages.StashStorePayload) {
    // Store the encrypted data
    s.mu.Lock()
    s.stored[p.OwnerID] = &EncryptedStash{
        Nonce:      p.Nonce,
        Ciphertext: p.Ciphertext,
        StoredAt:   time.Now().Unix(),
    }
    s.mu.Unlock()

    // Send ack
    s.rt.Emit(&rt.Message{
        Kind: "stash:ack",
        ToID: msg.FromID,
        Payload: &messages.StashStoreAck{
            OwnerID:  p.OwnerID,
            StoredAt: time.Now().Unix(),
        },
    })
}

func (s *StashService) handleStoreAckV1(msg *rt.Message, p *messages.StashStoreAck) {
    // Match to pending request via correlator
    s.storeCorrelator.Receive(msg.InReplyTo, *p)
}
```

### Step 5.2: Update Stash Service to Use Runtime

```go
type StashService struct {
    rt               *runtime.Runtime
    mu               sync.Mutex
    stored           map[string]*EncryptedStash  // Stashes we hold for others (keyed by owner ID)
    confidants       []string                     // Peer IDs holding our stash
    storeCorrelator  *utilities.Correlator[messages.StashStoreAck]
    requestCorrelator *utilities.Correlator[messages.StashResponsePayload]
    encryptor        *utilities.Encryptor
}

func (s *StashService) Name() string { return "stash" }

func (s *StashService) Init(rt *runtime.Runtime) error {
    s.rt = rt
    s.stored = make(map[string]*EncryptedStash)
    s.storeCorrelator = utilities.NewCorrelator[messages.StashStoreAck](10 * time.Second)
    s.requestCorrelator = utilities.NewCorrelator[messages.StashResponsePayload](10 * time.Second)
    s.encryptor = utilities.NewEncryptor(rt.Keypair().Seed())

    // Register behaviors with version-specific handlers (see Step 5.1)
    s.RegisterBehaviors(rt)

    return nil
}

// No more Kinds() or Handle() - version-specific handlers are registered in RegisterBehaviors()

// Store our stash with a confidant (uses correlator for request/response)
func (s *StashService) StoreWith(confidantID string, data []byte) error {
    nonce, ciphertext, err := s.encryptor.Seal(data)
    if err != nil {
        return err
    }

    msg := &runtime.Message{
        Kind: "stash:store",
        ToID: confidantID,
        Payload: &messages.StashStorePayload{
            OwnerID:    s.rt.MeID(),
            Nonce:      nonce,
            Ciphertext: ciphertext,
            Timestamp:  time.Now().Unix(),
        },
    }

    result := <-s.storeCorrelator.Send(s.rt, msg)
    if result.Err != nil {
        return result.Err  // Timeout or send failure
    }
    // Ack received successfully
    return nil
}
```

### Step 5.3: Testing Stash

Since stash isn't in production, no dual-mode testing needed. Test the new implementation directly:

```go
func TestStashStoreAndAck(t *testing.T) {
    // Auto-cleanup via t.Cleanup()
    aliceMock := runtime.NewMockRuntime(t, "alice", "alice-id-123")
    bobMock := runtime.NewMockRuntime(t, "bob", "bob-id-456")

    aliceStash := stash.NewStashService()
    aliceStash.Init(aliceMock)

    bobStash := stash.NewStashService()
    bobStash.Init(bobMock)

    // Simulate Alice sending a store request to Bob
    bobMock.Deliver(&runtime.Message{
        Kind:   "stash:store",
        FromID: "alice-id-123",
        Payload: &messages.StashStorePayload{
            OwnerID:    "alice-id-123",
            Nonce:      make([]byte, 24),
            Ciphertext: []byte("encrypted-data"),
        },
    })

    // Verify Bob stored it
    assert.True(t, bobStash.HasStashFor("alice-id-123"))

    // Verify Bob emitted an ack
    assert.Equal(t, 1, bobMock.EmittedCount())
    assert.Equal(t, "stash:ack", bobMock.LastEmitted().Kind)
}

func TestStashRoundTrip(t *testing.T) {
    // Integration test with real mesh transport
    alice := testRuntimeNara(t, "alice")
    bob := testRuntimeNara(t, "bob")

    // Alice stores with Bob
    err := alice.StashService().StoreWith(bob.ID(), []byte("secret-data"))
    require.NoError(t, err)

    // Verify Bob has it
    assert.Eventually(t, func() bool {
        return bob.StashService().HasStashFor(alice.ID())
    }, 5*time.Second, 100*time.Millisecond)

    // Alice requests recovery
    data, err := alice.StashService().RequestFrom(bob.ID())
    require.NoError(t, err)
    assert.Equal(t, []byte("secret-data"), data)
}
```

---

## Chapter 1 Checkpoints

### After Phase 1:
- [ ] `messages/stash.go` created with all stash payload structs
- [ ] Each payload has godoc comments and `Validate()` method
- [ ] Core runtime types compile (Message, StageResult, Pipeline, Behavior)
- [ ] Unit tests pass

### After Phase 2:
- [ ] Stash-relevant stages implemented (Sign, NoStore, NoGossip, MeshOnly, Verify, Dedupe)
- [ ] Each stage has unit tests

### After Phase 3:
- [ ] Runtime compiles with minimal implementation
- [ ] MockRuntime works for service testing
- [ ] Emit/Receive work for mesh messages

### After Phase 4:
- [ ] MeshTransportAdapter bridges existing mesh code
- [ ] IdentityAdapter provides public key lookup by ID

### After Phase 5:
- [ ] Service utilities created (Correlator, Encryptor)
- [ ] Stash service fully migrated to new runtime
- [ ] All stash message kinds working
- [ ] Round-trip tests pass (store → ack, request → response)
- [ ] No backwards compatibility needed — clean slate

**Once Chapter 1 is complete:** Stash works beautifully on the new architecture. The rest of the system continues on the existing architecture until Chapter 2.

---

# Chapter 2: Everything After Stash

**Goal:** Migrate remaining services to the new runtime and clean up.

This chapter is executed **after Chapter 1 is complete and validated**.

---

## Phase 6: Complete the messages/ Package

Add remaining payload types to `messages/`:

```
messages/
├── doc.go           # (from Chapter 1)
├── stash.go         # (from Chapter 1)
├── social.go        # NEW: SocialPayload, TeasePayload
├── presence.go      # NEW: HeyTherePayload, ChauPayload, NewspaperPayload
├── checkpoint.go    # NEW: CheckpointProposal, CheckpointVote
├── observation.go   # NEW: RestartObservation, StatusChangeObservation
└── gossip.go        # NEW: ZinePayload, DMPayload
```

Each file follows the same pattern established in `stash.go`:
- Godoc comments with Kind, Flow, Response, Transport, Version History
- `Validate()` method
- ID fields as primary identifiers, name fields with `omitempty` for legacy support

---

## Phase 7: Complete Runtime Infrastructure

### Step 7.1: Add Remaining Stages

Add to `runtime/stages_emit.go`:
- **DefaultStoreStage** - adds to ledger with priority
- **ContentKeyStoreStage** - stores with ContentKey-based deduplication
- **GossipStage** - adds to gossip queue
- **MQTTStage** - publishes to MQTT topic
- **MQTTPerNaraStage** - publishes to per-nara topic

Add to `runtime/stages_receive.go`:
- **SelfAttestingVerifyStage** - extracts key from payload, verifies
- **CustomVerifyStage** - calls custom verification function
- **ContentKeyDedupeStage** - rejects messages with duplicate ContentKey
- **RateLimitStage** - checks rate limiter
- **ImportanceFilterStage** - filters by importance level

### Step 7.2: Add Remaining Helpers

Add to `runtime/helpers.go`:
- `DefaultStore(priority)`, `ContentKeyStore(priority)`
- `Gossip()`
- `MQTT(topic)`, `MQTTPerNara(pattern)`
- `SelfAttesting(f)`, `CustomVerify(f)`, `NoVerify()`
- `ContentKeyDedupe()`
- `RateLimit(window, max, keyFunc)`
- `Critical()`, `Normal()`, `Casual(f)`

### Step 7.3: Implement GossipQueue

Create `runtime/gossip_queue.go`:

```go
type GossipQueue struct {
    mu       sync.RWMutex
    messages []*Message
    maxAge   time.Duration
    maxSize  int  // Backpressure: drop oldest when full
}

func NewGossipQueue(maxAge time.Duration, maxSize int) *GossipQueue
func (q *GossipQueue) Add(msg *Message)
func (q *GossipQueue) Recent(d time.Duration) []*Message
func (q *GossipQueue) Prune()
```

### Step 7.4: Complete Adapter Layer

- **LedgerAdapter** - wraps SyncLedger for LedgerInterface
- **EventBusAdapter** - wraps internal event bus
- **Complete TransportAdapter** - add MQTT alongside mesh

### Step 7.5: Add Logger to Runtime

Create `runtime/logger.go` (replaces `logservice.go`):

```go
type Logger struct { ... }           // Central coordinator
type ServiceLog struct { ... }       // Per-service logger

func (rt *Runtime) Log(service string) *ServiceLog

func (l *ServiceLog) Debug(format string, args ...any)
func (l *ServiceLog) Info(format string, args ...any)
func (l *ServiceLog) Event(eventType, actor string, opts ...LogOption)  // Batched
```

---

## Phase 8: Migrate Remaining Services

**Order by complexity:**

1. **social** (simple, one message kind, validates personality filtering)
2. **world** (self-contained journeys)
3. **presence** (hey-there, chau, newspaper, howdy)
4. **neighbourhood** (observations with ContentKey dedup)
5. **gossip** (reads from GossipQueue)
6. **checkpoint** (complex multi-sig, versioning)

### For each service:

1. Add payload types to `messages/` package (if not done in Phase 6)
2. Create behavior registrations
3. Update service struct to use Runtime
4. Implement MessageHandler interface
5. Run dual-mode tests (old and new paths)
6. Remove old code paths once validated

---

## Phase 9: Update Gossip Service

**Goal:** Make gossip service read from GossipQueue instead of Ledger.

```go
func (g *GossipService) createZine() *Zine {
    // OLD: events := g.ledger.Recent(5 * time.Minute)
    // NEW:
    messages := g.rt.GossipQueue().Recent(5 * time.Minute)

    // Build zine from messages
}
```

---

## Phase 10: Cleanup

### Step 10.1: Remove Old Network Methods

Once all services are migrated:
- Remove emit-related methods from Network
- Remove receive handling from Network
- Remove event bus from Network

### Step 10.2: Simplify Network

Network becomes a thin wrapper:
- Services talk directly to Runtime
- Network only holds configuration

### Step 10.3: Update Tests

- Remove tests for old paths
- Ensure all new paths have coverage
- Add integration tests

### Step 10.4: Update Documentation

- Run `nara docs --messages` to generate message catalog
- Update AGENTS.md with new architecture
- Remove obsolete design docs

---

## Chapter 2 Checkpoints

### After Phase 6:
- [ ] All payload types in `messages/` package
- [ ] Each has godoc, Validate(), ID fields

### After Phase 7:
- [ ] All stages implemented and tested
- [ ] GossipQueue with backpressure
- [ ] All adapters complete
- [ ] Logger integrated

### After Phase 8:
- [ ] All services migrated
- [ ] Dual-mode tests pass for each

### After Phase 9:
- [ ] Gossip uses GossipQueue
- [ ] Gossip-only messages work without storage

### After Phase 10:
- [ ] Old code removed
- [ ] Tests updated
- [ ] Docs generated

---

## Risk Mitigation

### Risk: Breaking existing functionality
**Mitigation:** Dual-mode testing during Chapter 2. Keep old paths until new ones validated. Chapter 1 (stash) has no backwards compatibility risk.

### Risk: Circular imports
**Mitigation:** Use interfaces extensively. runtime package has no dependencies on nara package.

### Risk: Performance regression
**Mitigation:** Benchmark critical paths before and after. Pipeline should add minimal overhead.

### Risk: Complexity explosion
**Mitigation:** Follow "start with functions, add stages when needed" principle. Don't over-abstract.

---

## Notes for Implementation

1. **Complete Chapter 1 before starting Chapter 2** — validate the architecture with stash first
2. **One service at a time in Chapter 2** — don't try to migrate everything at once
3. **Test each stage in isolation** before integrating
4. **Interfaces everywhere** to avoid circular dependencies
5. **Measure twice, cut once** — validate design with simple cases before complex ones

---

## Effort Summary

| Phase | Chapter | Complexity | Dependencies |
|-------|---------|------------|--------------|
| Phase 1: Core Types (Stash) | 1 | Low | None |
| Phase 2: Stash Stages | 1 | Low | Phase 1 |
| Phase 3: Runtime (Minimal) | 1 | Medium | Phase 1, 2 |
| Phase 4: Adapters (Stash) | 1 | Low | Phase 3 |
| Phase 5: Migrate Stash | 1 | Medium | Phase 4 |
| Phase 6: Complete messages/ | 2 | Low | Chapter 1 |
| Phase 7: Complete Runtime | 2 | Medium | Phase 6 |
| Phase 8: Migrate Services | 2 | High | Phase 7 |
| Phase 9: Gossip Update | 2 | Low | Phase 8 |
| Phase 10: Cleanup | 2 | Medium | Phase 9 |

**Recommended approach:** Complete Chapter 1, deploy stash, evaluate. Then proceed with Chapter 2 incrementally.
