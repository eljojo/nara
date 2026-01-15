# Implementation Plan: Nara Runtime Architecture

A step-by-step guide for implementing the runtime architecture defined in `DESIGN_NARA_RUNTIME.md`.

---

## Overview

This plan transforms Nara from a monolithic `Network` god-object into a modular runtime with:
- **Message** as the universal primitive
- **Pipeline** pattern with **StageResult** (explicit outcomes)
- **Behavior** registry for declarative message handling
- **Services** as independent programs running on the runtime

**Reference:** `DESIGN_NARA_RUNTIME.md` for full design details.

---

## Prerequisites

Before starting, ensure you understand:
1. The current `Network` struct and its methods
2. The `SyncLedger` and how events are stored
3. The MQTT transport layer
4. The existing service patterns (presence, social, checkpoint, etc.)

---

## Phase 1: Core Types

**Goal:** Create the foundational types without breaking existing code.

### Step 1.1: Create `runtime/` package structure

Create the directory structure:
```
runtime/
├── message.go       # Message struct
├── stage.go         # Stage interface, StageResult, Pipeline
├── behavior.go      # Behavior struct, Registry
├── helpers.go       # DSL helper constructors
├── runtime.go       # Runtime struct (stub)
└── runtime_test.go  # Tests
```

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
    From       string
    Timestamp  time.Time
    Payload    any
    Signature  []byte
}

// DefaultComputeID generates deterministic ID from content
func DefaultComputeID(msg *Message) string {
    h := sha256.New()
    h.Write([]byte(msg.Kind))
    h.Write([]byte(msg.From))
    h.Write([]byte(msg.Timestamp.Format(time.RFC3339Nano)))
    h.Write(payloadHash(msg.Payload))
    return base58.Encode(h.Sum(nil))[:16]
}

// SignableContent returns the content to be signed
func (m *Message) SignableContent() []byte {
    // Implementation: serialize ID, Kind, From, Timestamp, Payload
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
    PayloadTypes   map[int]reflect.Type   // Payload type per version (nil = use PayloadType for all)
    PayloadType    reflect.Type           // Single payload type (shorthand when no versioning needed)

    // ContentKey derivation (nil = no content key)
    ContentKey func(payload any) string

    // Emit stages
    Sign      Stage
    Store     Stage
    Gossip    Stage
    Transport Stage

    // Receive stages
    Verify    Stage
    Dedupe    Stage
    RateLimit Stage
    Filter    Stage

    // Error handling
    OnTransportError ErrorStrategy
    OnStoreError     ErrorStrategy
    OnVerifyError    ErrorStrategy
}

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

### Step 1.5: Implement Helper Constructors

Create `runtime/helpers.go` with all DSL helpers:
- `DefaultSign()`, `NoSign()`
- `DefaultStore(priority)`, `NoStore()`, `ContentKeyStore(priority)`
- `Gossip()`, `NoGossip()`
- `MQTT(topic)`, `MQTTPerNara(pattern)`, `DirectFirst(topic)`, `NoTransport()`
- `DefaultVerify()`, `SelfAttesting(f)`, `CustomVerify(f)`, `NoVerify()`
- `IDDedupe()`, `ContentKeyDedupe()`
- `RateLimit(window, max, keyFunc)`
- `Critical()`, `Normal()`, `Casual(f)`

Note: No `ID` helpers needed — ID is always computed the same way (unique envelope). `ContentKey` is a function in `Behavior`, not a stage.

Each helper returns a stage. Implement the stage structs as well.

**Test:** Write unit tests for each stage type in isolation.

---

## Phase 2: Individual Stages

**Goal:** Implement all stage types with full functionality.

### Step 2.1: Emit Stages

Create `runtime/stages_emit.go`:

1. **IDStage** - always computes unique envelope ID from (kind, from, timestamp, payload)
2. **ContentKeyStage** - computes semantic identity from payload (if behavior.ContentKey defined)
3. **DefaultSignStage** - signs with keypair from context
4. **NoSignStage** - no-op
5. **DefaultStoreStage** - adds to ledger with priority
6. **NoStoreStage** - no-op
7. **ContentKeyStoreStage** - stores with ContentKey-based deduplication
8. **GossipStage** - adds to gossip queue
9. **NoGossipStage** - no-op
10. **MQTTStage** - publishes to MQTT topic
11. **MQTTPerNaraStage** - publishes to per-nara topic
12. **DirectFirstStage** - tries mesh first, falls back to MQTT
13. **NoTransportStage** - no-op
14. **NotifyStage** - emits to event bus

**Test each stage individually** with mock dependencies.

### Step 2.2: Receive Stages

Create `runtime/stages_receive.go`:

1. **DefaultVerifyStage** - verifies signature against known public key
2. **SelfAttestingVerifyStage** - extracts key from payload, verifies
3. **CustomVerifyStage** - calls custom verification function
4. **NoVerifyStage** - no-op
5. **IDDedupeStage** - rejects messages with duplicate ID (exact same message)
6. **ContentKeyDedupeStage** - rejects messages with duplicate ContentKey (same fact)
7. **RateLimitStage** - checks rate limiter
8. **ImportanceFilterStage** - filters by importance level

**Test each stage individually** with mock dependencies.

---

## Phase 3: Runtime Core

**Goal:** Implement the Runtime that ties everything together.

### Step 3.1: Define Interfaces

Create `runtime/interfaces.go`:

```go
package runtime

// RuntimeInterface is what stages can access
type RuntimeInterface interface {
    Me() *Nara
    LookupPublicKey(name string) []byte
    RegisterPublicKey(name string, key []byte)
    RateLimiter() RateLimiterInterface
}

// LedgerInterface is what store stages use
type LedgerInterface interface {
    Add(msg *Message, priority int) error
    HasID(id string) bool
    HasContentKey(contentKey string) bool
    HasMatching(kind string, matcher func(*Message) bool) bool
}

// TransportInterface is what transport stages use
type TransportInterface interface {
    PublishMQTT(topic string, data []byte) error
    TrySendDirect(target string, msg *Message) error
}

// GossipQueueInterface is what gossip stages use
type GossipQueueInterface interface {
    Add(msg *Message)
}

// KeypairInterface is what sign stages use
type KeypairInterface interface {
    Sign(data []byte) []byte
}

// EventBusInterface is what notify stages use
type EventBusInterface interface {
    Emit(msg *Message)
}

// RateLimiterInterface is what rate limit stages use
type RateLimiterInterface interface {
    Allow(key string, window time.Duration, max int) bool
}
```

### Step 3.2: Implement GossipQueue

Create `runtime/gossip_queue.go`:

```go
package runtime

import (
    "sync"
    "time"
)

// GossipQueue holds messages for gossip propagation
type GossipQueue struct {
    mu       sync.RWMutex
    messages []*Message
    maxAge   time.Duration
}

func NewGossipQueue(maxAge time.Duration) *GossipQueue {
    return &GossipQueue{
        messages: make([]*Message, 0),
        maxAge:   maxAge,
    }
}

func (q *GossipQueue) Add(msg *Message) {
    q.mu.Lock()
    defer q.mu.Unlock()
    q.messages = append(q.messages, msg)
}

// Recent returns messages from the last duration
func (q *GossipQueue) Recent(d time.Duration) []*Message {
    q.mu.RLock()
    defer q.mu.RUnlock()

    cutoff := time.Now().Add(-d)
    result := make([]*Message, 0)
    for _, msg := range q.messages {
        if msg.Timestamp.After(cutoff) {
            result = append(result, msg)
        }
    }
    return result
}

// Prune removes old messages
func (q *GossipQueue) Prune() {
    q.mu.Lock()
    defer q.mu.Unlock()

    cutoff := time.Now().Add(-q.maxAge)
    newMessages := make([]*Message, 0)
    for _, msg := range q.messages {
        if msg.Timestamp.After(cutoff) {
            newMessages = append(newMessages, msg)
        }
    }
    q.messages = newMessages
}
```

**Test:** Write tests for Add, Recent, Prune.

### Step 3.3: Implement Runtime

Create `runtime/runtime.go`:

```go
package runtime

import (
    "context"
    "fmt"
)

type Runtime struct {
    me          *Nara
    keypair     KeypairInterface
    ledger      LedgerInterface
    transport   TransportInterface
    gossipQueue *GossipQueue
    eventBus    EventBusInterface
    rateLimiter RateLimiterInterface
    personality *Personality
    metrics     *Metrics

    services []Service
    handlers map[string][]MessageHandler

    ctx    context.Context
    cancel context.CancelFunc
}

type RuntimeConfig struct {
    Me          *Nara
    Keypair     KeypairInterface
    Ledger      LedgerInterface
    Transport   TransportInterface
    EventBus    EventBusInterface
    RateLimiter RateLimiterInterface
    Personality *Personality
}

func NewRuntime(cfg RuntimeConfig) *Runtime {
    return &Runtime{
        me:          cfg.Me,
        keypair:     cfg.Keypair,
        ledger:      cfg.Ledger,
        transport:   cfg.Transport,
        gossipQueue: NewGossipQueue(10 * time.Minute),
        eventBus:    cfg.EventBus,
        rateLimiter: cfg.RateLimiter,
        personality: cfg.Personality,
        handlers:    make(map[string][]MessageHandler),
    }
}

func (rt *Runtime) Emit(msg *Message) error {
    if msg.Timestamp.IsZero() {
        msg.Timestamp = time.Now()
    }

    behavior := Lookup(msg.Kind)
    if behavior == nil {
        return fmt.Errorf("unknown message kind: %s", msg.Kind)
    }

    // Set version to current if not specified
    if msg.Version == 0 {
        msg.Version = behavior.CurrentVersion
        if msg.Version == 0 {
            msg.Version = 1  // Default to v1
        }
    }

    pipeline := rt.buildEmitPipeline(behavior)
    ctx := rt.newPipelineContext()

    result := pipeline.Run(msg, ctx)

    if result.Error != nil {
        rt.applyErrorStrategy(msg, "emit", result.Error, behavior.OnTransportError)
        return result.Error
    }
    if result.Message == nil {
        rt.recordDrop(msg.Kind, result.Reason)
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
        rt.applyErrorStrategy(msg, "receive", result.Error, behavior.OnVerifyError)
        return result.Error
    }
    if result.Message == nil {
        rt.recordDrop(msg.Kind, result.Reason)
    }

    return nil
}

func (rt *Runtime) newPipelineContext() *PipelineContext {
    return &PipelineContext{
        Runtime:     rt,
        Ledger:      rt.ledger,
        Transport:   rt.transport,
        GossipQueue: rt.gossipQueue,
        Keypair:     rt.keypair,
        Personality: rt.personality,
        EventBus:    rt.eventBus,
    }
}

func (rt *Runtime) buildEmitPipeline(b *Behavior) Pipeline {
    stages := []Stage{}

    // ID stage - always computes unique envelope ID
    stages = append(stages, &IDStage{})

    // ContentKey stage - if behavior defines ContentKey function
    if b.ContentKey != nil {
        stages = append(stages, &ContentKeyStage{KeyFunc: b.ContentKey})
    }

    stages = append(stages, orDefault(b.Sign, DefaultSign()))
    stages = append(stages, orDefault(b.Store, DefaultStore(2)))
    stages = append(stages, orDefault(b.Gossip, NoGossip()))
    if b.Transport != nil {
        stages = append(stages, b.Transport)
    }
    stages = append(stages, &NotifyStage{})
    return Pipeline(stages)
}

func (rt *Runtime) buildReceivePipeline(b *Behavior) Pipeline {
    stages := []Stage{}
    stages = append(stages, orDefault(b.Verify, DefaultVerify()))
    stages = append(stages, orDefault(b.Dedupe, IDDedupe()))
    if b.RateLimit != nil {
        stages = append(stages, b.RateLimit)
    }
    if b.Filter != nil {
        stages = append(stages, b.Filter)
    }
    if b.Store != nil && !isNoStore(b.Store) {
        stages = append(stages, b.Store)
    }
    if b.Gossip != nil && !isNoGossip(b.Gossip) {
        stages = append(stages, b.Gossip)
    }
    stages = append(stages, &NotifyStage{})
    return Pipeline(stages)
}

func orDefault(stage Stage, def Stage) Stage {
    if stage != nil {
        return stage
    }
    return def
}

func (rt *Runtime) applyErrorStrategy(msg *Message, stage string, err error, strategy ErrorStrategy) {
    // Implementation as per design doc
}

func (rt *Runtime) recordDrop(kind, reason string) {
    // Metrics recording
}
```

**Test:** Integration tests for Emit and Receive with mock dependencies.

---

## Phase 4: Adapter Layer

**Goal:** Create adapters to bridge existing code with new runtime.

### Step 4.1: Create Ledger Adapter

Wrap existing `SyncLedger` to implement `LedgerInterface`:

```go
// In nara package (not runtime)
type LedgerAdapter struct {
    ledger *SyncLedger
}

func (a *LedgerAdapter) Add(msg *runtime.Message, priority int) error {
    // Convert runtime.Message to SyncEvent
    event := convertMessageToEvent(msg)
    a.ledger.Add(event, priority)
    return nil
}

func (a *LedgerAdapter) HasID(id string) bool {
    return a.ledger.HasID(id)
}

func (a *LedgerAdapter) HasContentKey(contentKey string) bool {
    return a.ledger.HasContentKey(contentKey)
}

func (a *LedgerAdapter) HasMatching(kind string, matcher func(*runtime.Message) bool) bool {
    // Implementation
}
```

### Step 4.2: Create Transport Adapter

Wrap existing MQTT/mesh transport:

```go
type TransportAdapter struct {
    mqtt *MQTTClient
    mesh *MeshClient
}

func (a *TransportAdapter) PublishMQTT(topic string, data []byte) error {
    return a.mqtt.Publish(topic, data)
}

func (a *TransportAdapter) TrySendDirect(target string, msg *runtime.Message) error {
    // Use mesh client
}
```

### Step 4.3: Create EventBus Adapter

```go
type EventBusAdapter struct {
    bus *InternalEventBus
}

func (a *EventBusAdapter) Emit(msg *runtime.Message) {
    // Convert and emit
}
```

---

## Phase 5: Migrate First Service

**Goal:** Migrate one service to validate the architecture.

### Step 5.1: Choose Target Service

Start with **stash** service:
- Not used in production yet — no backwards compatibility needed
- Can port completely in one go
- Multiple message kinds to test (`stash:store`, `stash:request`, `stash:response`, `stash-refresh`)
- Uses mesh transport (direct HTTP) — tests DirectFirst pattern
- Good proving ground for the new architecture

### Step 5.2: Register Behaviors

Create behavior registrations in `stash/behaviors.go`:

```go
package stash

import (
    "nara/runtime"
)

func init() {
    // Ephemeral: trigger stash recovery from confidants
    runtime.Register(&runtime.Behavior{
        Kind:        "stash-refresh",
        Description: "Request stash recovery from confidants",
        PayloadType: runtime.PayloadTypeOf[StashRefreshPayload](),
        Store:       runtime.NoStore(),
        Gossip:      runtime.NoGossip(),
        Transport:   runtime.MQTT("nara/plaza/stash_refresh"),
        Verify:      runtime.NoVerify(),
        Filter:      runtime.Critical(),
    })

    // Store request (peer wants to store their stash with us)
    runtime.Register(&runtime.Behavior{
        Kind:        "stash:store",
        Description: "Request to store encrypted stash",
        PayloadType: runtime.PayloadTypeOf[StashStorePayload](),
        Store:       runtime.NoStore(),  // Don't store the request itself
        Gossip:      runtime.NoGossip(),
        Transport:   runtime.DirectFirst(""),  // Mesh only, no MQTT fallback
        Verify:      runtime.DefaultVerify(),
        Filter:      runtime.Critical(),
    })

    // Retrieve request (peer wants their stash back)
    runtime.Register(&runtime.Behavior{
        Kind:        "stash:request",
        Description: "Request to retrieve stored stash",
        PayloadType: runtime.PayloadTypeOf[StashRequestPayload](),
        Store:       runtime.NoStore(),
        Gossip:      runtime.NoGossip(),
        Transport:   runtime.DirectFirst(""),
        Verify:      runtime.DefaultVerify(),
        Filter:      runtime.Critical(),
    })

    // Response with encrypted stash data
    runtime.Register(&runtime.Behavior{
        Kind:        "stash:response",
        Description: "Response containing encrypted stash",
        PayloadType: runtime.PayloadTypeOf[StashResponsePayload](),
        Store:       runtime.NoStore(),
        Gossip:      runtime.NoGossip(),
        Transport:   runtime.DirectFirst(""),
        Verify:      runtime.DefaultVerify(),
        Filter:      runtime.Critical(),
    })
}
```

### Step 5.3: Update Service to Use Runtime

```go
type StashService struct {
    rt         *runtime.Runtime
    stored     map[string]*EncryptedStash  // Stashes we hold for others
    confidants []string                     // Peers holding our stash
}

func (s *StashService) Name() string { return "stash" }

func (s *StashService) Init(rt *runtime.Runtime) error {
    s.rt = rt
    s.stored = make(map[string]*EncryptedStash)
    return nil
}

func (s *StashService) Kinds() []string {
    return []string{"stash-refresh", "stash:store", "stash:request", "stash:response"}
}

func (s *StashService) Handle(msg *runtime.Message) {
    switch msg.Kind {
    case "stash-refresh":
        s.handleRefresh(msg)
    case "stash:store":
        s.handleStore(msg)
    case "stash:request":
        s.handleRequest(msg)
    case "stash:response":
        s.handleResponse(msg)
    }
}

// Request stash recovery from all confidants
func (s *StashService) RequestRecovery() {
    s.rt.Emit(&runtime.Message{
        Kind:    "stash-refresh",
        From:    s.rt.Me().Name,
        Payload: &StashRefreshPayload{},
    })
}

// Store our stash with a confidant
func (s *StashService) StoreWith(confidant string, encrypted []byte) {
    s.rt.Emit(&runtime.Message{
        Kind: "stash:store",
        From: s.rt.Me().Name,
        Payload: &StashStorePayload{
            Target:    confidant,
            Encrypted: encrypted,
        },
    })
}
```

### Step 5.4: Testing

Since stash isn't in production, no dual-mode testing needed. Just test the new implementation:

```go
func TestStashRoundTrip(t *testing.T) {
    // Create two test naras with runtime
    alice := testRuntimeNara(t, "alice")
    bob := testRuntimeNara(t, "bob")

    // Alice stores with Bob
    alice.StashService().StoreWith("bob", []byte("encrypted-data"))

    // Verify Bob received and stored it
    assert.Eventually(t, func() bool {
        return bob.StashService().HasStashFor("alice")
    }, 5*time.Second, 100*time.Millisecond)

    // Alice requests recovery
    alice.StashService().RequestRecovery()

    // Verify Alice gets her stash back
    assert.Eventually(t, func() bool {
        return alice.StashService().HasRecovered()
    }, 5*time.Second, 100*time.Millisecond)
}
```

---

## Phase 6: Migrate Remaining Services

**Order by complexity:**

1. **social** (simple, one message kind, good validation of personality filtering)
2. **world** (self-contained journeys)
3. **presence** (hey-there, chau, newspaper, howdy)
4. **neighbourhood** (observations with ContentKey dedup)
5. **gossip** (reads from GossipQueue now)
6. **checkpoint** (complex multi-sig, versioning already proven useful)

### For each service:

1. Create behavior registrations
2. Update service struct to use Runtime
3. Implement MessageHandler interface
4. Run dual-mode tests
5. Remove old code paths once validated

---

## Phase 7: Update Gossip Service

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

## Phase 8: Cleanup

### Step 8.1: Remove Old Network Methods

Once all services are migrated:
- Remove emit-related methods from Network
- Remove receive handling from Network
- Remove event bus from Network

### Step 8.2: Simplify Network

Network becomes a thin wrapper or is removed entirely:
- Services talk directly to Runtime
- Network only holds configuration

### Step 8.3: Update Tests

- Remove tests for old paths
- Ensure all new paths have coverage
- Add integration tests

### Step 8.4: Update Documentation

- Run `nara docs` to generate message catalog
- Update CLAUDE.md with new architecture
- Remove obsolete design docs

---

## Checkpoints

### After Phase 1:
- [ ] All core types compile
- [ ] Unit tests pass for Message, StageResult, Pipeline, Behavior

### After Phase 2:
- [ ] All stages implemented
- [ ] Each stage has unit tests
- [ ] Stages are testable in isolation

### After Phase 3:
- [ ] Runtime compiles
- [ ] Emit/Receive work with mock dependencies
- [ ] GossipQueue works

### After Phase 4:
- [ ] Adapters wrap existing code
- [ ] No changes to existing code required

### After Phase 5:
- [ ] Stash service migrated to new runtime
- [ ] All stash message kinds working (`stash-refresh`, `stash:store`, `stash:request`, `stash:response`)
- [ ] Round-trip tests pass (store → request → response)
- [ ] No backwards compatibility needed — clean slate

### After Phase 6:
- [ ] All services migrated
- [ ] All dual-mode tests pass

### After Phase 7:
- [ ] Gossip uses GossipQueue
- [ ] Gossip-only messages work without storage

### After Phase 8:
- [ ] Old code removed
- [ ] Tests updated
- [ ] Docs generated

---

## Risk Mitigation

### Risk: Breaking existing functionality
**Mitigation:** Dual-mode testing at each step. Keep old paths until new ones validated.

### Risk: Circular imports
**Mitigation:** Use interfaces extensively. runtime package has no dependencies on nara package.

### Risk: Performance regression
**Mitigation:** Benchmark critical paths before and after. Pipeline should add minimal overhead.

### Risk: Complexity explosion
**Mitigation:** Follow "start with functions, add stages when needed" principle. Don't over-abstract.

---

## Notes for Implementation

1. **Keep the old code working** until the new code is proven
2. **One service at a time** - don't try to migrate everything at once
3. **Test each stage in isolation** before integrating
4. **Interfaces everywhere** to avoid circular dependencies
5. **Measure twice, cut once** - validate design with simple cases before complex ones

---

## Estimated Effort

| Phase | Complexity | Dependencies |
|-------|------------|--------------|
| Phase 1: Core Types | Low | None |
| Phase 2: Stages | Medium | Phase 1 |
| Phase 3: Runtime | Medium | Phase 1, 2 |
| Phase 4: Adapters | Low | Phase 3 |
| Phase 5: First Service | Medium | Phase 4 |
| Phase 6: Remaining Services | High | Phase 5 |
| Phase 7: Gossip Update | Low | Phase 6 |
| Phase 8: Cleanup | Medium | Phase 7 |

**Recommended approach:** Complete phases 1-5 first, then evaluate. Phases 6-8 can be done incrementally.
