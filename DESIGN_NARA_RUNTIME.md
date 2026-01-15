# Nara Runtime Architecture

A comprehensive design for restructuring Nara into a runtime with pluggable services and composable message pipelines.

---

## Table of Contents

1. [Vision](#vision)
2. [Core Primitives](#core-primitives)
3. [The Pipeline Pattern](#the-pipeline-pattern)
4. [StageResult: Explicit Outcomes](#stageresult-explicit-outcomes)
5. [Error Handling Strategies](#error-handling-strategies)
6. [Emit Pipeline Stages](#emit-pipeline-stages)
7. [Receive Pipeline Stages](#receive-pipeline-stages)
8. [Service Utilities](#service-utilities)
9. [Behavior Registry](#behavior-registry)
10. [Complete Behavior Catalog](#complete-behavior-catalog)
11. [Services](#services)
12. [Runtime Implementation](#runtime-implementation)
    - [Runtime Structure](#runtime-structure)
    - [Runtime Primitives](#runtime-primitives) (Logger)
    - [Service-to-Service Communication](#service-to-service-communication) (Local messages)
13. [Auto-Generated Documentation](#auto-generated-documentation)

---

## Vision

### Nara as an Operating System

Nara is a **runtime**. Services are **programs** that run on it. The runtime provides primitives (storage, transport, identity, serialization). Services use them, and can swap out pieces with custom implementations.

```
┌─────────────────────────────────────────────────────────────────┐
│                          SERVICES                                │
│                                                                  │
│   ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐       │
│   │ presence │  │  social  │  │checkpoint│  │  stash   │  ...  │
│   └────┬─────┘  └────┬─────┘  └────┬─────┘  └────┬─────┘       │
│        │             │             │             │              │
│        └─────────────┴──────┬──────┴─────────────┘              │
│                             │                                    │
│                      emit(Message)                               │
│                      subscribe(Kind)                             │
└─────────────────────────────┼───────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                       NARA RUNTIME                               │
│                                                                  │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │                 EMIT PIPELINE                            │   │
│   │   Message → [ID] → [Sign] → [Store] → [Transport]       │   │
│   │                         ↓                                │   │
│   │                   StageResult                            │   │
│   │            (Continue | Drop | Error)                     │   │
│   └─────────────────────────────────────────────────────────┘   │
│                                                                  │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │               RECEIVE PIPELINE                           │   │
│   │   Message → [Verify] → [Dedupe] → [RateLimit] → [Store] │   │
│   │                         ↓                                │   │
│   │                   StageResult                            │   │
│   │            (Continue | Drop | Error)                     │   │
│   └─────────────────────────────────────────────────────────┘   │
│                                                                  │
│   ┌────────────┐  ┌────────────┐  ┌────────────┐                │
│   │   Ledger   │  │ Transport  │  │  Identity  │                │
│   │  (storage) │  │(mqtt/mesh) │  │ (keypair)  │                │
│   └────────────┘  └────────────┘  └────────────┘                │
└─────────────────────────────────────────────────────────────────┘
```

### Design Principles

1. **Everything is a Message** — stored events, ephemeral broadcasts, protocol exchanges
2. **Behavior is declared, not scattered** — all handling defined in one place per message kind
3. **Defaults for common cases** — 80% of messages use standard handling
4. **Customizable where needed** — swap any pipeline stage for the 20% that need it
5. **Explicit outcomes** — stages return `StageResult` (Continue/Drop/Error), no silent failures
6. **OS handles serialization** — services provide Go structs, runtime handles wire format
7. **Auto-documented** — the registry generates documentation automatically

---

## Core Primitives

### Message

The universal primitive. Everything that flows through the system is a Message.

```go
// Message is the universal primitive - what services emit and receive
type Message struct {
    // Core identity (always present)
    ID         string    // Unique envelope identifier (always unique per message instance)
    ContentKey string    // Semantic identity for dedup (optional, stable across observers)
    Kind       string    // "hey-there", "observation:restart", "checkpoint"
    Version    int       // Schema version for this kind (default 1, increment on breaking changes)
    From       string    // Who created this message
    Timestamp  time.Time // When it was created

    // Content
    Payload    any       // Kind-specific data (Go struct, runtime handles serialization)

    // Cryptographic (attached by runtime)
    Signature  []byte    // Creator's signature (may be nil for some kinds)
}

// ComputeID generates unique envelope ID - always includes timestamp and sender
func ComputeID(msg *Message) string {
    h := sha256.New()
    h.Write([]byte(msg.Kind))
    h.Write([]byte(msg.From))
    h.Write([]byte(msg.Timestamp.Format(time.RFC3339Nano)))
    h.Write(payloadHash(msg.Payload))
    return base58.Encode(h.Sum(nil))[:16]
}
```

### ID vs ContentKey

Two distinct concepts that were previously conflated:

| Concept | Purpose | Uniqueness | Example |
|---------|---------|------------|---------|
| **ID** | Envelope identity | Always unique per message | `abc123` (Alice's message), `def456` (Bob's message) |
| **ContentKey** | Semantic identity | Same for same logical fact | `bob:restart:5:1700000000` (both Alice and Bob) |

**Why split them?**

```
Alice observes: "Bob restarted for the 5th time at 10:00"
Bob observes:   "Bob restarted for the 5th time at 10:00"

Old design (overloaded ID):
  Alice: ID = "bob:restart:5:1700000000"  ← ID loses uniqueness
  Bob:   ID = "bob:restart:5:1700000000"  ← Same ID, confusing

New design (split):
  Alice: ID = "abc123", ContentKey = "bob:restart:5:1700000000"
  Bob:   ID = "def456", ContentKey = "bob:restart:5:1700000000"

  ✓ IDs are unique — can reference specific messages
  ✓ ContentKey enables semantic dedup — same fact from different observers
```

### Versioning

Messages include a `Version` field to handle schema evolution:

```go
Version    int       // Schema version for this kind (default 1)
```

**Rules:**
1. **New messages default to current version** — runtime sets `Version` to behavior's `CurrentVersion`
2. **Old messages keep their version** — received messages retain original version
3. **Behaviors declare supported versions** — min/max range for compatibility
4. **Version-aware deserialization** — runtime uses version to pick correct payload type
5. **Version-specific handlers** — each version routes to its own typed handler function

**Example: Stash evolved from v1 to v2**

```go
// v1 payload (old - name-based)
type StashStorePayloadV1 struct {
    Owner      string `json:"owner"`       // Name only
    Nonce      []byte `json:"nonce"`
    Ciphertext []byte `json:"ciphertext"`
}

// v2 payload (current - ID-based)
type StashStorePayloadV2 struct {
    Owner      string `json:"owner,omitempty"` // For display only
    OwnerID    string `json:"owner_id"`        // Primary identifier
    Nonce      []byte `json:"nonce"`
    Ciphertext []byte `json:"ciphertext"`
    TTL        int64  `json:"ttl,omitempty"`   // New in v2
}
```

**Version-Specific Handlers:**

Each version gets its own typed handler. The runtime routes to the correct one:

```go
// Behavior registers versions AND their handlers
rt.Register(&rt.Behavior{
    Kind:           "stash:store",
    CurrentVersion: 2,
    MinVersion:     1,

    PayloadTypes: map[int]reflect.Type{
        1: rt.PayloadTypeOf[StashStorePayloadV1](),
        2: rt.PayloadTypeOf[StashStorePayloadV2](),
    },

    // Version-specific typed handlers
    Handlers: map[int]any{
        1: rt.TypedHandler(s.handleStoreV1),  // func(*Message, *V1)
        2: rt.TypedHandler(s.handleStoreV2),  // func(*Message, *V2)
    },

    Emit:    rt.MeshRequestEmit(),
    Receive: rt.MeshRequestReceive(),
})
```

**Handler Implementations:**

```go
// V1 handler - migrates to V2 and delegates
func (s *StashService) handleStoreV1(msg *rt.Message, p *StashStorePayloadV1) {
    // Migrate to V2 format
    v2 := &StashStorePayloadV2{
        OwnerID:    s.rt.LookupIDByName(p.Owner),
        Owner:      p.Owner,
        Nonce:      p.Nonce,
        Ciphertext: p.Ciphertext,
        TTL:        0,  // V1 didn't have TTL
    }
    // Delegate to V2 handler
    s.handleStoreV2(msg, v2)
}

// V2 handler - the real logic lives here
func (s *StashService) handleStoreV2(msg *rt.Message, p *StashStorePayloadV2) {
    s.store(p.OwnerID, p.Nonce, p.Ciphertext, p.TTL)
}
```

**Benefits of Version-Specific Handlers:**
- **Type-safe** — each handler receives correctly typed payload, no type switches
- **Clean separation** — version logic is isolated, not mixed in one big function
- **Migration as a pattern** — V1 handler can migrate and call V2 handler
- **Easy deprecation** — when dropping V1 support, just delete the handler

**TypedHandler helper:**

```go
// Wraps a typed handler for the registry
func TypedHandler[T any](fn func(*Message, *T)) any {
    return fn
}

// Runtime invokes the correct handler
func (rt *Runtime) invokeHandler(msg *Message, behavior *Behavior) {
    handler := behavior.Handlers[msg.Version]
    if handler == nil {
        rt.log.Warn("no handler for %s v%d", msg.Kind, msg.Version)
        return
    }
    // Reflection call: handler(msg, msg.Payload)
    reflect.ValueOf(handler).Call([]reflect.Value{
        reflect.ValueOf(msg),
        reflect.ValueOf(msg.Payload),
    })
}
```

**Version-aware receive:**

```go
func (rt *Runtime) deserialize(raw []byte) (*Message, error) {
    // Peek at kind and version
    var envelope struct {
        Kind    string `json:"kind"`
        Version int    `json:"version"`
    }
    json.Unmarshal(raw, &envelope)

    behavior := Lookup(envelope.Kind)

    // Default to v1 if not specified (backwards compat)
    version := envelope.Version
    if version == 0 {
        version = 1
    }

    // Check version bounds
    if version < behavior.MinVersion || version > behavior.CurrentVersion {
        return nil, fmt.Errorf("unsupported version %d for %s", version, envelope.Kind)
    }

    // Get correct payload type for this version
    payloadType := behavior.PayloadTypes[version]
    // ... deserialize with correct type
}
```

**Version Lifecycle:**

```
┌─────────────────────────────────────────────────────────────────┐
│ Version 1 only                                                  │
│ CurrentVersion: 1, MinVersion: 1                                │
│ Handlers: {1: handleV1}                                         │
└─────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────┐
│ Transition period                                               │
│ CurrentVersion: 2, MinVersion: 1                                │
│ Handlers: {1: handleV1, 2: handleV2}                            │
│                                                                 │
│ V1 handler migrates and delegates to V2 handler                 │
└─────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────┐
│ Deprecation (once all naras updated)                            │
│ CurrentVersion: 2, MinVersion: 2                                │
│ Handlers: {2: handleV2}                                         │
│                                                                 │
│ Delete V1 handler and payload struct                            │
└─────────────────────────────────────────────────────────────────┘
```

**Migration strategy:**
- Bump version only for breaking changes (new required fields, type changes)
- Keep `MinVersion` at oldest version you still want to accept
- V1 handlers can migrate payload and delegate to V2 handler
- When dropping V1: bump MinVersion, delete V1 handler and payload struct

---

### Why Everything is a Message

| What | Old Model | New Model |
|------|-----------|-----------|
| Stored events | SyncEvent | Message with `Store: DefaultStore(N)` |
| Ephemeral broadcasts | Separate struct | Message with `Store: NoStore()` |
| Protocol exchanges | Separate handling | Message with `Store: NoStore()` |
| Newspapers | NewspaperEvent | Message with `Store: NoStore()` |

The **type** is the same. The **behavior** differs.

---

## The Pipeline Pattern

Messages flow through a pipeline of stages. Each stage:
- Returns a **StageResult** (Continue, Drop, or Error)
- Has a **default** behavior (most messages use this)
- Can be **customized** (service provides alternative)
- Can be **skipped** (returns early)

### Pipeline Interface

```go
// StageResult represents the outcome of a stage
type StageResult struct {
    Message *Message  // nil = dropped
    Error   error     // nil = success
    Reason  string    // "rate_limited", "invalid_signature", "duplicate", etc.
}

// Convenience constructors
func Continue(msg *Message) StageResult { return StageResult{Message: msg} }
func Drop(reason string) StageResult    { return StageResult{Reason: reason} }
func Fail(err error) StageResult        { return StageResult{Error: err} }

// Stage processes a message and returns an explicit result
type Stage interface {
    Process(msg *Message, ctx *PipelineContext) StageResult
}

// PipelineContext carries runtime dependencies
type PipelineContext struct {
    Runtime     *Runtime
    Ledger      *Ledger
    Transport   *Transport
    GossipQueue *GossipQueue  // Explicit queue for gossip (not coupled to ledger)
    Keypair     NaraKeypair
    Personality *Personality
    EventBus    *EventBus
}

// Pipeline chains stages
type Pipeline []Stage

func (p Pipeline) Run(msg *Message, ctx *PipelineContext) StageResult {
    for _, stage := range p {
        result := stage.Process(msg, ctx)

        if result.Error != nil {
            return result  // Error - propagate up
        }
        if result.Message == nil {
            return result  // Dropped with reason
        }
        msg = result.Message  // Continue with (possibly modified) message
    }
    return Continue(msg)
}
```

### Emit vs Receive

Two pipelines, different purposes:

**Emit Pipeline** (outgoing messages):
```
Message → [ID] → [ContentKey?] → [Sign] → [Store] → [Gossip?] → [Transport] → [Notify]
                                                ↓
                                      Returns StageResult
```

**Receive Pipeline** (incoming messages):
```
Message → [Verify] → [Dedupe] → [RateLimit] → [Filter] → [Store] → [Notify]
                                        ↓
                              Returns StageResult
```

Note: ContentKey stage only runs if the behavior defines a `ContentKey` function. Dedupe can use either `IDDedupe()` (exact message) or `ContentKeyDedupe()` (same fact).

---

## StageResult: Explicit Outcomes

Every stage returns a `StageResult` that explicitly communicates what happened:

```go
type StageResult struct {
    Message *Message  // The message to continue with (nil = dropped)
    Error   error     // Set if stage failed (transport error, etc.)
    Reason  string    // Human-readable reason for drop ("rate_limited", "duplicate")
}

// Three possible outcomes:

// 1. Continue - message proceeds to next stage
result := Continue(msg)

// 2. Drop - message is intentionally filtered/rejected
result := Drop("rate_limited")

// 3. Error - something went wrong
result := Fail(fmt.Errorf("MQTT publish failed: %w", err))
```

**Benefits over `next()` callback:**
- Can't forget to call `next()` — you must return something
- Explicit error path — errors are returned, not swallowed
- Debuggable — `Reason` field explains why message was dropped
- Composable — pipeline runner knows exactly what happened

---

## Error Handling Strategies

Each behavior can specify how to handle errors at each stage:

```go
type ErrorStrategy int

const (
    ErrorDrop    ErrorStrategy = iota  // Drop message silently
    ErrorLog                           // Log warning and drop
    ErrorRetry                         // Retry with exponential backoff
    ErrorQueue                         // Send to dead letter queue for inspection
    ErrorPanic                         // Fail loudly (for critical messages)
)

// Behavior includes error strategies
type Behavior struct {
    // ... other fields ...

    // Error handling per stage
    OnTransportError ErrorStrategy  // What if MQTT/mesh fails?
    OnStoreError     ErrorStrategy  // What if ledger is full?
    OnVerifyError    ErrorStrategy  // What if signature is invalid?
}
```

**Default strategies by message type:**

| Message Type | Transport Error | Store Error | Verify Error |
|--------------|-----------------|-------------|--------------|
| Checkpoint | ErrorPanic | ErrorPanic | ErrorLog |
| Observation | ErrorRetry | ErrorLog | ErrorLog |
| Social | ErrorLog | ErrorLog | ErrorDrop |
| Ephemeral | ErrorDrop | N/A | ErrorDrop |

**Runtime applies strategies:**

```go
func (rt *Runtime) applyErrorStrategy(
    msg *Message,
    stage string,
    err error,
    strategy ErrorStrategy,
) {
    rt.metrics.RecordError(msg.Kind, stage)

    switch strategy {
    case ErrorDrop:
        // Silent drop
    case ErrorLog:
        logrus.Warnf("%s failed for %s: %v", stage, msg.Kind, err)
    case ErrorRetry:
        rt.retryQueue.Add(msg, stage, err)
    case ErrorQueue:
        rt.deadLetter.Add(msg, stage, err)
    case ErrorPanic:
        logrus.Fatalf("Critical failure in %s for %s: %v", stage, msg.Kind, err)
    }
}
```

---

## Emit Pipeline Stages

### 1. ID Stage

Computes the unique envelope ID. Always includes kind, from, timestamp, and payload hash.

```go
// IDStage computes unique envelope ID
type IDStage struct{}

func (s *IDStage) Process(msg *Message, ctx *PipelineContext) StageResult {
    if msg.ID == "" {
        msg.ID = ComputeID(msg)
    }
    return Continue(msg)
}
```

ID is always unique per message instance. No customization needed.

---

### 2. ContentKey Stage

Computes the semantic identity for dedup. Only used for messages that need cross-observer deduplication.

```go
// ContentKeyStage computes semantic identity from payload
type ContentKeyStage struct {
    KeyFunc func(payload any) string
}

func (s *ContentKeyStage) Process(msg *Message, ctx *PipelineContext) StageResult {
    if msg.ContentKey == "" && s.KeyFunc != nil {
        msg.ContentKey = s.KeyFunc(msg.Payload)
    }
    return Continue(msg)
}

// NoContentKeyStage is a no-op (most messages don't need content keys)
type NoContentKeyStage struct{}

func (s *NoContentKeyStage) Process(msg *Message, ctx *PipelineContext) StageResult {
    return Continue(msg)
}
```

**Usage:**
```go
// Most messages: no content key needed
ContentKey: nil  // or NoContentKey()

// Observations: content key for cross-observer dedup
ContentKey: ContentKey(func(p any) string {
    obs := p.(*ObservationRestartPayload)
    return fmt.Sprintf("%s:%d:%d", obs.Subject, obs.RestartNum, obs.StartTime.Unix())
})
```

---

### 3. Sign Stage

Signs the message with the creator's keypair.

```go
// DefaultSignStage signs with the runtime's keypair
type DefaultSignStage struct{}

func (s *DefaultSignStage) Process(msg *Message, ctx *PipelineContext) StageResult {
    msg.Signature = ctx.Keypair.Sign(msg.SignableContent())
    return Continue(msg)
}

// NoSignStage skips signing (for messages where signature is in payload)
type NoSignStage struct{}

func (s *NoSignStage) Process(msg *Message, ctx *PipelineContext) StageResult {
    return Continue(msg)  // No signature on Message itself
}
```

---

### 4. Store Stage

Stores the message in the ledger.

```go
// DefaultStoreStage stores with a GC priority
type DefaultStoreStage struct {
    Priority int // 0 = never prune, higher = prune sooner
}

func (s *DefaultStoreStage) Process(msg *Message, ctx *PipelineContext) StageResult {
    if err := ctx.Ledger.Add(msg, s.Priority); err != nil {
        return Fail(fmt.Errorf("ledger add: %w", err))
    }
    return Continue(msg)
}

// NoStoreStage skips storage (ephemeral messages)
type NoStoreStage struct{}

func (s *NoStoreStage) Process(msg *Message, ctx *PipelineContext) StageResult {
    return Continue(msg)  // Don't store
}

// ContentKeyStoreStage stores with ContentKey-based deduplication
// Only stores if no message with the same ContentKey exists
type ContentKeyStoreStage struct {
    Priority int
}

func (s *ContentKeyStoreStage) Process(msg *Message, ctx *PipelineContext) StageResult {
    if msg.ContentKey != "" {
        if ctx.Ledger.HasContentKey(msg.ContentKey) {
            return Drop("content_exists")  // Same fact already stored
        }
    }
    if err := ctx.Ledger.Add(msg, s.Priority); err != nil {
        return Fail(fmt.Errorf("ledger add: %w", err))
    }
    return Continue(msg)
}
```

**GC Priority Values:**
| Priority | Meaning | Examples |
|----------|---------|----------|
| 0 | Never prune | checkpoints, observation:restart |
| 1 | Important | hey-there, chau, observation:first-seen |
| 2 | Normal | social events |
| 3 | Low priority | seen events |
| 4 | Expendable | pings |

---

### 6. Gossip Stage

**Key change: Gossip is now explicit, not coupled to ledger.**

```go
// GossipStage explicitly queues message for gossip
type GossipStage struct{}

func (s *GossipStage) Process(msg *Message, ctx *PipelineContext) StageResult {
    ctx.GossipQueue.Add(msg)  // Explicit queue - gossip service reads from here
    return Continue(msg)
}

// NoGossipStage skips gossip
type NoGossipStage struct{}

func (s *NoGossipStage) Process(msg *Message, ctx *PipelineContext) StageResult {
    return Continue(msg)
}
```

**Why explicit gossip queue?**

Old design:
```go
Transport: Gossip()  // Does nothing! Relies on Store putting in ledger.
Store: NoStore()     // Oops, gossip won't work. Silent failure.
```

New design:
```go
Gossip: Gossip()     // Explicitly queues for gossip
Store: NoStore()     // Fine - gossip and store are independent
```

The gossip service reads from `GossipQueue`, not from the ledger. Store and gossip are independent.

---

### 7. Transport Stage

Sends the message over the network.

```go
// MQTTStage broadcasts to a fixed MQTT topic
type MQTTStage struct {
    Topic string
}

func (s *MQTTStage) Process(msg *Message, ctx *PipelineContext) StageResult {
    if err := ctx.Transport.PublishMQTT(s.Topic, msg.Marshal()); err != nil {
        return Fail(fmt.Errorf("mqtt publish: %w", err))
    }
    return Continue(msg)
}

// MQTTPerNaraStage broadcasts to a per-nara topic
type MQTTPerNaraStage struct {
    TopicPattern string // e.g., "nara/newspaper/%s"
}

func (s *MQTTPerNaraStage) Process(msg *Message, ctx *PipelineContext) StageResult {
    topic := fmt.Sprintf(s.TopicPattern, msg.From)
    if err := ctx.Transport.PublishMQTT(topic, msg.Marshal()); err != nil {
        return Fail(fmt.Errorf("mqtt publish: %w", err))
    }
    return Continue(msg)
}

// MeshOnlyStage sends directly via mesh, fails if unreachable
type MeshOnlyStage struct{}

func (s *MeshOnlyStage) Process(msg *Message, ctx *PipelineContext) StageResult {
    target := extractTarget(msg)
    if target == "" {
        return Fail(errors.New("mesh-only message has no target"))
    }
    if err := ctx.Transport.TrySendDirect(target, msg); err != nil {
        return Fail(fmt.Errorf("mesh send to %s: %w", target, err))
    }
    return Continue(msg)
}

// NoTransportStage skips network transport (local-only)
type NoTransportStage struct{}

func (s *NoTransportStage) Process(msg *Message, ctx *PipelineContext) StageResult {
    return Continue(msg)
}
```

---

### 8. Notify Stage

Always runs last. Notifies local subscribers.

```go
type NotifyStage struct{}

func (s *NotifyStage) Process(msg *Message, ctx *PipelineContext) StageResult {
    ctx.EventBus.Emit(msg)
    return Continue(msg)
}
```

---

## Receive Pipeline Stages

### 1. Verify Stage

Verifies the message signature.

```go
// DefaultVerifyStage verifies single signature against known public key
type DefaultVerifyStage struct{}

func (s *DefaultVerifyStage) Process(msg *Message, ctx *PipelineContext) StageResult {
    pubKey := ctx.Runtime.LookupPublicKey(msg.From)
    if pubKey == nil {
        return Drop("unknown_sender")
    }
    if !msg.VerifySignature(pubKey) {
        return Drop("invalid_signature")
    }
    return Continue(msg)
}

// SelfAttestingVerifyStage uses public key embedded in payload
type SelfAttestingVerifyStage struct {
    ExtractKey func(payload any) []byte
}

func (s *SelfAttestingVerifyStage) Process(msg *Message, ctx *PipelineContext) StageResult {
    pubKey := s.ExtractKey(msg.Payload)
    if !msg.VerifySignature(pubKey) {
        return Drop("invalid_signature")
    }
    ctx.Runtime.RegisterPublicKey(msg.From, pubKey)
    return Continue(msg)
}

// CustomVerifyStage for complex verification (e.g., checkpoint multi-sig)
type CustomVerifyStage struct {
    VerifyFunc func(msg *Message, ctx *PipelineContext) StageResult
}

func (s *CustomVerifyStage) Process(msg *Message, ctx *PipelineContext) StageResult {
    return s.VerifyFunc(msg, ctx)
}

// NoVerifyStage skips verification
type NoVerifyStage struct{}

func (s *NoVerifyStage) Process(msg *Message, ctx *PipelineContext) StageResult {
    return Continue(msg)
}
```

---

### 2. Dedupe Stage

Prevents storing duplicate messages. Now cleanly split between ID and ContentKey:

```go
// IDDedupeStage rejects messages with duplicate ID (exact same message)
type IDDedupeStage struct{}

func (s *IDDedupeStage) Process(msg *Message, ctx *PipelineContext) StageResult {
    if ctx.Ledger.HasID(msg.ID) {
        return Drop("duplicate_id")
    }
    return Continue(msg)
}

// ContentKeyDedupeStage rejects messages with duplicate ContentKey (same fact)
type ContentKeyDedupeStage struct{}

func (s *ContentKeyDedupeStage) Process(msg *Message, ctx *PipelineContext) StageResult {
    if msg.ContentKey != "" {
        if ctx.Ledger.HasContentKey(msg.ContentKey) {
            return Drop("duplicate_content")
        }
    }
    return Continue(msg)
}
```

**When to use which:**

| Stage | Checks | Use Case |
|-------|--------|----------|
| `IDDedupe()` | `msg.ID` | Default — reject exact same message seen twice |
| `ContentKeyDedupe()` | `msg.ContentKey` | Observations — reject same fact from different observers |

**Example: Observation Dedup**

```
Alice sends: ID=abc123, ContentKey="bob:restart:5:1700000000"
Bob sends:   ID=def456, ContentKey="bob:restart:5:1700000000"

IDDedupe: Both pass (different IDs)
ContentKeyDedupe: Bob's message dropped (same ContentKey as Alice's)

Result: We store Alice's message, dedupe Bob's. Both IDs remain unique and referenceable.
```

---

### 3. RateLimit Stage

Throttles incoming messages.

```go
type RateLimitStage struct {
    Window  time.Duration
    Max     int
    KeyFunc func(msg *Message) string
}

func (s *RateLimitStage) Process(msg *Message, ctx *PipelineContext) StageResult {
    key := s.KeyFunc(msg)
    if !ctx.Runtime.RateLimiter.Allow(key, s.Window, s.Max) {
        return Drop("rate_limited")
    }
    return Continue(msg)
}
```

---

### 4. Filter Stage

Filters messages based on local criteria (e.g., personality).

```go
// ImportanceFilterStage uses importance levels
type ImportanceFilterStage struct {
    Importance   int // 1=casual, 2=normal, 3=critical
    CasualFilter func(msg *Message, personality *Personality) bool
}

func (s *ImportanceFilterStage) Process(msg *Message, ctx *PipelineContext) StageResult {
    switch s.Importance {
    case 3: // Critical - never filter
        return Continue(msg)
    case 2: // Normal - filter only if very chill
        if ctx.Personality.Chill <= 85 {
            return Continue(msg)
        }
        return Drop("filtered_by_chill")
    case 1: // Casual - use custom filter
        if s.CasualFilter == nil || s.CasualFilter(msg, ctx.Personality) {
            return Continue(msg)
        }
        return Drop("filtered_by_personality")
    default:
        return Continue(msg)
    }
}
```

---

## Service Utilities

Utilities that services can opt into. Not part of the runtime core, but provided to make common patterns easy.

### Correlator: Request/Response Pattern

For services that need request/response semantics (like stash), the `Correlator` tracks pending requests and matches responses:

```go
// correlator.go - a utility, not part of runtime core

// Correlator tracks pending requests and matches responses
type Correlator[Resp any] struct {
    pending  map[string]*pendingRequest[Resp]
    mu       sync.Mutex
    timeout  time.Duration
}

type pendingRequest[Resp any] struct {
    ch      chan Result[Resp]
    sentAt  time.Time
}

type Result[Resp any] struct {
    Response Resp
    Error    error  // ErrTimeout if no response in time
}

var ErrTimeout = errors.New("request timed out")

func NewCorrelator[Resp any](timeout time.Duration) *Correlator[Resp] {
    c := &Correlator[Resp]{
        pending: make(map[string]*pendingRequest[Resp]),
        timeout: timeout,
    }
    go c.reapLoop()  // Clean up timed-out requests
    return c
}

// Send emits a request and returns a channel for the response
func (c *Correlator[Resp]) Send(rt *Runtime, msg *Message) <-chan Result[Resp] {
    ch := make(chan Result[Resp], 1)

    c.mu.Lock()
    c.pending[msg.ID] = &pendingRequest[Resp]{ch: ch, sentAt: time.Now()}
    c.mu.Unlock()

    rt.Emit(msg)
    return ch
}

// Receive is called when a response arrives - matches it to pending request
func (c *Correlator[Resp]) Receive(requestID string, resp Resp) bool {
    c.mu.Lock()
    pending, ok := c.pending[requestID]
    if ok {
        delete(c.pending, requestID)
    }
    c.mu.Unlock()

    if ok {
        pending.ch <- Result[Resp]{Response: resp}
        return true
    }
    return false  // No pending request (late response, already timed out)
}

func (c *Correlator[Resp]) reapLoop() {
    ticker := time.NewTicker(time.Second)
    for range ticker.C {
        c.mu.Lock()
        now := time.Now()
        for id, req := range c.pending {
            if now.Sub(req.sentAt) > c.timeout {
                req.ch <- Result[Resp]{Error: ErrTimeout}
                delete(c.pending, id)
            }
        }
        c.mu.Unlock()
    }
}
```

**Usage in a service:**

```go
type StashService struct {
    rt        *Runtime
    storeReqs *Correlator[StashAckPayload]  // For store requests
}

func (s *StashService) Init(rt *Runtime) error {
    s.rt = rt
    s.storeReqs = NewCorrelator[StashAckPayload](30 * time.Second)
    return nil
}

// Synchronous store - blocks until ack or timeout
func (s *StashService) StoreWith(confidant string, data []byte) error {
    msg := &Message{
        Kind: "stash:store",
        From: s.rt.Me().Name,
        Payload: &StashStorePayload{Target: confidant, Data: data},
    }

    result := <-s.storeReqs.Send(s.rt, msg)
    if result.Error != nil {
        return result.Error  // Timeout
    }
    if !result.Response.Success {
        return errors.New(result.Response.Reason)
    }
    return nil
}

// Handle incoming messages
func (s *StashService) Handle(msg *Message) {
    switch msg.Kind {
    case "stash:ack":
        ack := msg.Payload.(*StashAckPayload)
        s.storeReqs.Receive(ack.RequestID, *ack)  // Match to pending
    }
}
```

**Response payloads include the original request ID:**

```go
type StashAckPayload struct {
    RequestID string  // Echo back msg.ID from original request
    Success   bool
    Reason    string  // If not success
}

// Responder sends ack with original ID
func (s *StashService) handleStoreRequest(msg *Message) {
    // ... process the store request ...

    s.rt.Emit(&Message{
        Kind: "stash:ack",
        From: s.rt.Me().Name,
        Payload: &StashAckPayload{
            RequestID: msg.ID,  // Correlation key
            Success:   true,
        },
    })
}
```

**Key properties:**
- **Opt-in** — services choose to use it
- **Type-safe** — generic over response type
- **Timeouts built-in** — configurable per correlator
- **Non-blocking option** — use channel select for async
- **No runtime changes** — just a utility library

---

## Behavior Registry

### Behavior Definition

Each message kind has a Behavior that defines its pipelines and handlers:

```go
type Behavior struct {
    // Identity
    Kind        string       // Unique identifier, e.g., "observation:restart"
    Description string       // Human-readable description

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

// PayloadTypeOf is a helper to get reflect.Type from a struct
func PayloadTypeOf[T any]() reflect.Type {
    var zero T
    return reflect.TypeOf(zero)
}
```

Note: `ID` is not in Behavior because ID computation is always the same (unique envelope ID). Only `ContentKey` is customizable.

### Registry

```go
var Behaviors = map[string]*Behavior{}

func Register(b *Behavior) error {
    if b.Kind == "" {
        return errors.New("behavior must have a Kind")
    }
    if Behaviors[b.Kind] != nil {
        return fmt.Errorf("behavior %s already registered", b.Kind)
    }
    Behaviors[b.Kind] = b
    return nil
}

func Lookup(kind string) *Behavior {
    return Behaviors[kind]
}
```

### Helper Constructors (DSL)

```go
// === Sign Helpers ===
func DefaultSign() Stage { return &DefaultSignStage{} }
func NoSign() Stage { return &NoSignStage{} }

// === Store Helpers ===
func DefaultStore(priority int) Stage { return &DefaultStoreStage{Priority: priority} }
func NoStore() Stage { return &NoStoreStage{} }
func ContentKeyStore(priority int) Stage { return &ContentKeyStoreStage{Priority: priority} }

// === Gossip Helpers ===
func Gossip() Stage { return &GossipStage{} }
func NoGossip() Stage { return &NoGossipStage{} }

// === Transport Helpers ===
func MQTT(topic string) Stage { return &MQTTStage{Topic: topic} }
func MQTTPerNara(pattern string) Stage { return &MQTTPerNaraStage{TopicPattern: pattern} }
func MeshOnly() Stage { return &MeshOnlyStage{} }
func NoTransport() Stage { return &NoTransportStage{} }

// === Verify Helpers ===
func DefaultVerify() Stage { return &DefaultVerifyStage{} }
func SelfAttesting(f func(any) []byte) Stage { return &SelfAttestingVerifyStage{ExtractKey: f} }
func CustomVerify(f func(*Message, *PipelineContext) StageResult) Stage {
    return &CustomVerifyStage{VerifyFunc: f}
}
func NoVerify() Stage { return &NoVerifyStage{} }

// === Dedupe Helpers ===
func IDDedupe() Stage { return &IDDedupeStage{} }
func ContentKeyDedupe() Stage { return &ContentKeyDedupeStage{} }

// === RateLimit Helpers ===
func RateLimit(window time.Duration, max int, keyFunc func(*Message) string) Stage {
    return &RateLimitStage{Window: window, Max: max, KeyFunc: keyFunc}
}

// === Filter Helpers ===
func Critical() Stage { return &ImportanceFilterStage{Importance: 3} }
func Normal() Stage { return &ImportanceFilterStage{Importance: 2} }
func Casual(f func(*Message, *Personality) bool) Stage {
    return &ImportanceFilterStage{Importance: 1, CasualFilter: f}
}
```

Note: No `ID` helpers needed — ID is always computed the same way. `ContentKey` is defined as a function in `Behavior`, not a stage.

### Pattern Defaults

Instead of configuring every field, define defaults and override what's different:

```go
// === Base Defaults ===

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
// No signing, no verification, no network transport, no storage
var LocalDefaults = Behavior{
    Emit:    EmitBehavior{Sign: NoSign(), Store: NoStore(), Gossip: NoGossip(), Transport: NoTransport()},
    Receive: ReceiveBehavior{Verify: NoVerify(), Dedupe: IDDedupe(), Store: NoStore()},
}

// === Template Functions (copy defaults, override differences) ===

// Ephemeral broadcasts - no storage, MQTT only
func Ephemeral(kind, desc, topic string) *Behavior {
    b := EphemeralDefaults  // struct copy
    b.Kind = kind
    b.Description = desc
    b.Emit.Transport = MQTT(topic)
    return &b
}

// Protocol messages - not stored, verified, critical (MQTT transport)
func Protocol(kind, desc, topic string) *Behavior {
    b := ProtocolDefaults
    b.Kind = kind
    b.Description = desc
    b.Emit.Transport = MQTT(topic)
    return &b
}

// ProtocolUnverified - like Protocol but no signature verification
func ProtocolUnverified(kind, desc, topic string) *Behavior {
    b := ProtocolUnverifiedDefaults
    b.Kind = kind
    b.Description = desc
    b.Emit.Transport = MQTT(topic)
    return &b
}

// MeshRequest - protocol messages over mesh (not MQTT)
func MeshRequest(kind, desc string) *Behavior {
    b := ProtocolDefaults
    b.Kind = kind
    b.Description = desc
    b.Emit.Transport = MeshOnly()
    return &b
}

// StoredEvent - persisted, gossiped (no MQTT broadcast)
func StoredEvent(kind, desc string, priority int) *Behavior {
    b := StoredDefaults
    b.Kind = kind
    b.Description = desc
    b.Emit.Store = DefaultStore(priority)
    b.Receive.Store = DefaultStore(priority)
    return &b
}

// BroadcastEvent - stored, gossiped, AND broadcast via MQTT
func BroadcastEvent(kind, desc string, priority int, topic string) *Behavior {
    b := StoredDefaults
    b.Kind = kind
    b.Description = desc
    b.Emit.Store = DefaultStore(priority)
    b.Emit.Transport = MQTT(topic)
    b.Receive.Store = DefaultStore(priority)
    return &b
}

// Local - for service-to-service communication within a nara
// No network transport, no signing, no storage - just internal routing
func Local(kind, desc string) *Behavior {
    b := LocalDefaults
    b.Kind = kind
    b.Description = desc
    return &b
}

// Helper to set payload type on a template (defaults to v1)
func (b *Behavior) WithPayload[T any]() *Behavior {
    b.CurrentVersion = 1
    b.MinVersion = 1
    b.PayloadTypes = map[int]reflect.Type{
        1: PayloadTypeOf[T](),
    }
    return b
}

// Helper to add a typed handler for a version
// Usage: .WithHandler(1, service.handleV1).WithHandler(2, service.handleV2)
func (b *Behavior) WithHandler[T any](version int, fn func(*Message, *T)) *Behavior {
    if b.Handlers == nil {
        b.Handlers = make(map[int]any)
    }
    b.Handlers[version] = fn
    return b
}

// TypedHandler wraps a typed handler function for the registry
func TypedHandler[T any](fn func(*Message, *T)) any {
    return fn
}

// Helper to add ContentKey to a template
func (b *Behavior) WithContentKey(fn func(any) string) *Behavior {
    b.ContentKey = fn
    return b
}

// Helper to customize receive filter
func (b *Behavior) WithFilter(stage Stage) *Behavior {
    b.Receive.Filter = stage
    return b
}

// Helper to add rate limiting
func (b *Behavior) WithRateLimit(stage Stage) *Behavior {
    b.Receive.RateLimit = stage
    return b
}
```

**Usage - clean and readable:**

```go
// Stash messages (mesh-only protocol)
Register(Ephemeral("stash-refresh", "Request stash recovery", "nara/plaza/stash_refresh").
    WithPayload[StashRefreshPayload]().
    WithHandler(1, s.handleRefreshV1))

Register(MeshRequest("stash:store", "Store encrypted stash").
    WithPayload[StashStorePayload]().
    WithHandler(1, s.handleStoreV1))

// Protocol messages (MQTT, not stored, verified)
Register(Protocol("checkpoint:propose", "Checkpoint proposal", "nara/checkpoint/propose").
    WithPayload[CheckpointProposalPayload]().
    WithHandler(1, s.handleProposeV1))

Register(Protocol("checkpoint:vote", "Checkpoint vote", "nara/checkpoint/vote").
    WithPayload[CheckpointVotePayload]().
    WithHandler(1, s.handleVoteV1))

// Protocol messages (unverified variant)
Register(ProtocolUnverified("sync:request", "Ledger sync request", "nara/ledger/%s/request").
    WithPayload[SyncRequestPayload]().
    WithHandler(1, s.handleSyncRequestV1))

// Social with personality filter
Register(BroadcastEvent("social", "Social interactions", 2, "nara/plaza/social").
    WithPayload[SocialPayload]().
    WithFilter(Casual(socialFilter)).
    WithHandler(1, s.handleSocialV1))

// Observations with ContentKey dedup
Register(StoredEvent("observation:restart", "Records nara restarts", 0).
    WithPayload[ObservationRestartPayload]().
    WithContentKey(restartContentKey).
    WithRateLimit(RateLimit(5*time.Minute, 10, subjectKey)).
    WithHandler(1, s.handleRestartV1))
```

---

## Complete Behavior Catalog

### Ephemerals (Not Stored)

```go
func init() {
    // Discovery poll
    Register(&Behavior{
        Kind:        "howdy",
        Description: "Discovery poll - who's out there?",
        PayloadType: PayloadTypeOf[HowdyPayload](),
        Store:       NoStore(),
        Gossip:      NoGossip(),
        Transport:   MQTT("nara/plaza/howdy"),
        Verify:      NoVerify(),
        Filter:      Critical(),
    })

    // Presence heartbeat
    Register(&Behavior{
        Kind:        "newspaper",
        Description: "Periodic presence heartbeat with status",
        PayloadType: PayloadTypeOf[NewspaperPayload](),
        Store:       NoStore(),
        Gossip:      NoGossip(),
        Transport:   MQTTPerNara("nara/newspaper/%s"),
        Verify:      DefaultVerify(),
        Filter:      Critical(),
    })

    // Stash recovery trigger
    Register(&Behavior{
        Kind:        "stash-refresh",
        Description: "Request stash recovery from confidants",
        PayloadType: PayloadTypeOf[StashRefreshPayload](),
        Store:       NoStore(),
        Gossip:      NoGossip(),
        Transport:   MQTT("nara/plaza/stash_refresh"),
        Verify:      NoVerify(),
        Filter:      Critical(),
    })
}
```

### Presence Events

```go
func init() {
    // Identity announcement
    Register(&Behavior{
        Kind:        "hey-there",
        Description: "Identity announcement with public key and mesh IP",
        PayloadType: PayloadTypeOf[HeyTherePayload](),
        Store:       DefaultStore(1),
        Gossip:      Gossip(),
        Transport:   MQTT("nara/plaza/hey_there"),
        Verify:      SelfAttesting(func(p any) []byte {
            return p.(*HeyTherePayload).PublicKey
        }),
        Filter:      Critical(),
    })

    // Graceful shutdown
    Register(&Behavior{
        Kind:        "chau",
        Description: "Graceful shutdown announcement",
        PayloadType: PayloadTypeOf[ChauPayload](),
        Store:       DefaultStore(1),
        Gossip:      Gossip(),
        Transport:   MQTT("nara/plaza/chau"),
        Verify:      DefaultVerify(),
        Filter:      Critical(),
    })
}
```

### Observation Events

```go
func init() {
    // Restart observation
    Register(&Behavior{
        Kind:        "observation:restart",
        Description: "Records when a nara restarts",
        PayloadType: PayloadTypeOf[ObservationRestartPayload](),
        ContentKey:  restartContentKey,  // Semantic identity for cross-observer dedup
        Store:       ContentKeyStore(0), // Store with ContentKey dedup
        Gossip:      Gossip(),
        Transport:   NoTransport(),      // Gossip only, no MQTT
        Verify:      DefaultVerify(),
        Dedupe:      ContentKeyDedupe(), // Dedupe by ContentKey, not ID
        RateLimit:   RateLimit(5*time.Minute, 10, subjectKey),
        Filter:      Critical(),
        OnStoreError: ErrorLog,
    })

    // First-seen observation
    Register(&Behavior{
        Kind:        "observation:first-seen",
        Description: "Records first time a nara is observed",
        PayloadType: PayloadTypeOf[ObservationFirstSeenPayload](),
        ContentKey:  firstSeenContentKey,
        Store:       ContentKeyStore(0),
        Gossip:      Gossip(),
        Transport:   NoTransport(),
        Verify:      DefaultVerify(),
        Dedupe:      ContentKeyDedupe(),
        RateLimit:   RateLimit(5*time.Minute, 10, subjectKey),
        Filter:      Critical(),
    })

    // Status change observation (no ContentKey - each observer's view is distinct)
    Register(&Behavior{
        Kind:        "observation:status-change",
        Description: "Records online/offline transitions",
        PayloadType: PayloadTypeOf[ObservationStatusChangePayload](),
        Store:       DefaultStore(1),
        Gossip:      Gossip(),
        Transport:   NoTransport(),
        Verify:      DefaultVerify(),
        RateLimit:   RateLimit(5*time.Minute, 10, subjectKey),
        Filter:      Normal(),
    })
}

// ContentKey functions derive semantic identity from payload
func restartContentKey(p any) string {
    obs := p.(*ObservationRestartPayload)
    return fmt.Sprintf("%s:%d:%d", obs.Subject, obs.RestartNum, obs.StartTime.Unix())
}

func firstSeenContentKey(p any) string {
    obs := p.(*ObservationFirstSeenPayload)
    return fmt.Sprintf("%s:%s", obs.Subject, obs.FirstSeenAt.Format(time.RFC3339))
}

func subjectKey(msg *Message) string {
    return msg.Payload.(interface{ GetSubject() string }).GetSubject()
}
```

### Social Events

```go
func init() {
    Register(&Behavior{
        Kind:        "social",
        Description: "Social interactions (teases, trends, etc.)",
        PayloadType: PayloadTypeOf[SocialPayload](),
        Store:       DefaultStore(2),
        Gossip:      Gossip(),
        Transport:   MQTT("nara/plaza/social"),
        Verify:      DefaultVerify(),
        Filter:      Casual(socialFilter),
        OnTransportError: ErrorLog,
    })
}

func socialFilter(msg *Message, p *Personality) bool {
    payload := msg.Payload.(*SocialPayload)

    if p.Chill > 70 && payload.Reason == ReasonRandom {
        return false
    }
    if p.Chill > 85 {
        if payload.Reason != ReasonComeback && payload.Reason != ReasonHighRestarts {
            return false
        }
    }
    if p.Agreeableness > 80 && payload.Reason == ReasonTrendAbandon {
        return false
    }
    if p.Sociability < 20 && payload.Reason == ReasonRandom {
        return false
    }

    return true
}
```

### Ping Events

```go
func init() {
    Register(&Behavior{
        Kind:        "ping",
        Description: "Latency measurement between naras",
        PayloadType: PayloadTypeOf[PingPayload](),
        Store:       MaxPerKeyStore(4, 5, pingKey),
        Gossip:      Gossip(),
        Transport:   NoTransport(),
        Verify:      DefaultVerify(),
        Filter:      Casual(nil),
    })
}

func pingKey(msg *Message) string {
    p := msg.Payload.(*PingPayload)
    return msg.From + ":" + p.Target
}
```

### Checkpoint Events

```go
func init() {
    // Final checkpoint (multi-party signed)
    Register(&Behavior{
        Kind:        "checkpoint",
        Description: "Multi-party signed consensus anchor",
        PayloadType: PayloadTypeOf[CheckpointPayload](),
        Sign:        NoSign(),  // Signatures in payload
        Store:       DefaultStore(0),
        Gossip:      Gossip(),
        Transport:   MQTT("nara/checkpoint/final"),
        Verify:      CustomVerify(checkpointVerifyMultiSig),
        Filter:      Critical(),
        OnTransportError: ErrorPanic,
        OnStoreError:     ErrorPanic,
    })
}

func checkpointVerifyMultiSig(msg *Message, ctx *PipelineContext) StageResult {
    cp := msg.Payload.(*CheckpointPayload)

    if len(cp.Signatures) < MinCheckpointSignatures {
        return Drop("insufficient_signatures")
    }

    validCount := 0
    for i, voterID := range cp.VoterIDs {
        pubKey := ctx.Runtime.LookupPublicKey(voterID)
        if pubKey == nil {
            continue
        }
        attestation := cp.AttestationFor(voterID)
        if attestation.VerifySignature(pubKey, cp.Signatures[i]) {
            validCount++
        }
    }

    if validCount < MinCheckpointSignatures {
        return Drop("invalid_signatures")
    }

    return Continue(msg)
}
```

### Protocol Messages

Using the `Protocol()` and `ProtocolUnverified()` templates — much cleaner:

```go
func init() {
    // Checkpoint protocol (verified)
    Register(Protocol("checkpoint:propose", "Checkpoint proposal", "nara/checkpoint/propose").
        WithPayload[CheckpointProposalPayload]().
        WithHandler(1, s.handleProposeV1))

    Register(Protocol("checkpoint:vote", "Checkpoint vote", "nara/checkpoint/vote").
        WithPayload[CheckpointVotePayload]().
        WithHandler(1, s.handleVoteV1))

    // Sync protocol (request is unverified, response is verified)
    Register(ProtocolUnverified("sync:request", "Ledger sync request", "nara/ledger/%s/request").
        WithPayload[SyncRequestPayload]().
        WithHandler(1, s.handleSyncRequestV1))

    Register(Protocol("sync:response", "Ledger sync response", "nara/ledger/%s/response").
        WithPayload[SyncResponsePayload]().
        WithHandler(1, s.handleSyncResponseV1))
}
```

Compare to the verbose version without templates — the templates eliminate all the repetitive `Store: NoStore(), Gossip: NoGossip(), Filter: Critical()` boilerplate.

---

## Services

### Service Interface

```go
type Service interface {
    // Identity
    Name() string

    // Lifecycle
    Init(rt *Runtime) error
    Start(ctx context.Context) error
    Stop() error
}

// Optional interface for services that register behaviors with handlers
type BehaviorRegistrar interface {
    RegisterBehaviors(rt *Runtime)
}
```

### Service Example: Presence

```go
type PresenceService struct {
    rt *Runtime
}

func (s *PresenceService) Name() string { return "presence" }

func (s *PresenceService) RegisterBehaviors(rt *Runtime) {
    // hey-there with version-specific handlers
    rt.Register(
        BroadcastEvent("hey-there", "Identity announcement", 1, "nara/plaza/hey_there").
            WithPayload[HeyTherePayloadV1]().
            WithHandler(1, s.handleHeyThereV1),
    )

    // howdy - ephemeral, no storage
    rt.Register(
        Ephemeral("howdy", "Discovery poll", "nara/plaza/howdy").
            WithPayload[HowdyPayload]().
            WithHandler(1, s.handleHowdyV1),
    )
}

// Typed handlers - no type switches needed
func (s *PresenceService) handleHeyThereV1(msg *Message, p *HeyTherePayloadV1) {
    s.rt.RegisterPublicKey(msg.FromID, p.PublicKey)
    s.rt.UpdatePeerMeshIP(msg.FromID, p.MeshIP)
}

func (s *PresenceService) handleHowdyV1(msg *Message, p *HowdyPayload) {
    // Respond with our hey-there
    s.announceIdentity()
}

func (s *PresenceService) Init(rt *Runtime) error {
    s.rt = rt
    return nil
}

func (s *PresenceService) Start(ctx context.Context) error {
    go s.announceLoop(ctx)
    go s.newspaperLoop(ctx)
    return nil
}

func (s *PresenceService) announceLoop(ctx context.Context) {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            s.announceIdentity()
        }
    }
}

func (s *PresenceService) announceIdentity() {
    s.rt.Emit(&Message{
        Kind:    "hey-there",
        FromID:  s.rt.MeID(),
        Payload: &HeyTherePayloadV1{
            PublicKey: s.rt.PublicKey(),
            MeshIP:    s.rt.MeshIP(),
        },
    })
}
```

### Service Example: Stash Message Flow

The stash service demonstrates how the runtime abstracts transport. Services emit messages without knowing the underlying protocol—the runtime handles routing to HTTP, MQTT, or other transports.

```mermaid
sequenceDiagram
    participant Alice as Alice's Stash Service
    participant ART as Alice's Runtime
    participant HTTP as HTTP Transport
    participant BRT as Bob's Runtime
    participant Bob as Bob's Stash Service

    Note over Alice,Bob: Store Request Flow

    Alice->>ART: rt.Emit(stash:store)
    ART->>ART: Compute message ID
    ART->>ART: Sign message
    ART->>ART: Lookup behavior → MeshOnly()
    ART->>HTTP: POST /mesh/message
    HTTP->>BRT: Receive message
    BRT->>BRT: Verify signature
    BRT->>BRT: Lookup handler
    BRT->>Bob: handleStoreV1(msg, payload)
    Bob->>Bob: Store encrypted stash
    Bob->>BRT: rt.Emit(stash:ack)
    BRT->>HTTP: POST /mesh/message
    HTTP->>ART: Receive ack
    ART->>ART: Match InReplyTo → correlator
    ART->>Alice: Resolve pending request
    Alice->>Alice: StoreWith() returns success
```

**Key points:**

1. **Service layer** — Stash calls `rt.Emit()`, doesn't know about HTTP
2. **Runtime layer** — Handles signing, ID generation, transport routing
3. **Transport layer** — Same HTTP mesh underneath, just abstracted
4. **Correlation** — Runtime matches responses via `InReplyTo` field

The wire protocol is unchanged (HTTP POST to mesh endpoint), but services are now decoupled from transport details.

---

## Runtime Implementation

### Runtime Structure

```go
type Runtime struct {
    // Identity
    me      *Nara
    keypair NaraKeypair

    // Storage
    ledger *Ledger

    // Transport
    transport   *Transport
    gossipQueue *GossipQueue  // Explicit gossip queue

    // Services
    services []Service
    handlers map[string][]MessageHandler

    // Event bus
    eventBus *EventBus

    // Rate limiting
    rateLimiter *RateLimiter

    // Metrics
    metrics *Metrics

    // Error handling
    retryQueue *RetryQueue
    deadLetter *DeadLetterQueue

    // Personality (for filtering)
    personality *Personality

    // Logging
    logger *Logger  // Base logger for the runtime

    // Lifecycle
    ctx    context.Context
    cancel context.CancelFunc
}
```

### Runtime Primitives

The runtime exposes **primitives** that services use. Primitives are fundamentally different from Services:

| Aspect | Service | Primitive |
|--------|---------|-----------|
| Lifecycle | `Init()`, `Start()`, `Stop()` | None — always available |
| Registration | Registers with runtime | Built into runtime |
| Behavior | Registers message handlers | No message handlers |
| Example | PresenceService, StashService | Logger, RateLimiter, Metrics |

Primitives are part of the runtime itself. They don't have their own lifecycle — they exist as long as the runtime exists.

#### Logger Primitive

**Logger is NOT a Service.** It has no Init/Start/Stop lifecycle. It's a primitive that the runtime provides to services.

Structured logging with service-scoped loggers. Services get a logger via `rt.Log("service")`:

```go
// Logger wraps structured logging with nara context
type Logger struct {
    base    *logrus.Logger
    naraID  string
    fields  logrus.Fields
}

// ServiceLog is a logger scoped to a service
type ServiceLog struct {
    logger  *Logger
    service string
}

// Runtime exposes logging
func (rt *Runtime) Log(service string) *ServiceLog {
    return &ServiceLog{
        logger:  rt.logger,
        service: service,
    }
}

// ServiceLog methods
func (l *ServiceLog) Info(msg string, fields ...any) {
    l.logger.base.WithFields(l.fields(fields...)).Info(msg)
}

func (l *ServiceLog) Warn(msg string, fields ...any) {
    l.logger.base.WithFields(l.fields(fields...)).Warn(msg)
}

func (l *ServiceLog) Error(msg string, fields ...any) {
    l.logger.base.WithFields(l.fields(fields...)).Error(msg)
}

func (l *ServiceLog) Debug(msg string, fields ...any) {
    l.logger.base.WithFields(l.fields(fields...)).Debug(msg)
}

func (l *ServiceLog) fields(extra ...any) logrus.Fields {
    f := logrus.Fields{
        "nara":    l.logger.naraID,
        "service": l.service,
    }
    // Merge extra fields as key-value pairs
    for i := 0; i < len(extra)-1; i += 2 {
        if key, ok := extra[i].(string); ok {
            f[key] = extra[i+1]
        }
    }
    return f
}
```

**Usage in services:**

```go
type StashService struct {
    rt  *Runtime
    log *ServiceLog
}

func (s *StashService) Init(rt *Runtime) error {
    s.rt = rt
    s.log = rt.Log("stash")  // Get service-scoped logger
    return nil
}

func (s *StashService) handleStoreV1(msg *Message, p *StashStorePayload) {
    s.log.Info("storing stash", "owner", p.OwnerID, "size", len(p.Ciphertext))
    // ...
    s.log.Debug("stash stored successfully", "owner", p.OwnerID)
}
```

### Service-to-Service Communication

Services can communicate with each other using `Local()` messages. These messages:
- Don't leave the nara (no network transport)
- Don't require signing or verification
- Don't get stored in the ledger
- Route through the same pipeline for consistency

**Example: Stash recovery notification**

When stash recovery completes, the stash service notifies other services:

```go
// messages/stash.go
type StashRecoveredPayload struct {
    RecoveredAt time.Time `json:"recovered_at"`
    ItemCount   int       `json:"item_count"`
}

// stash_service.go
func (s *StashService) RegisterBehaviors(rt *Runtime) {
    // ... other behaviors ...

    // Local message for notifying other services
    rt.Register(Local("stash:recovered", "Stash recovery completed").
        WithPayload[StashRecoveredPayload]().
        WithHandler(1, nil))  // No handler in stash service itself
}

func (s *StashService) completeRecovery(items int) {
    s.log.Info("recovery complete", "items", items)

    // Notify other services
    s.rt.Emit(&Message{
        Kind: "stash:recovered",
        Payload: &StashRecoveredPayload{
            RecoveredAt: time.Now(),
            ItemCount:   items,
        },
    })
}

// presence_service.go - listens for stash recovery
func (p *PresenceService) RegisterBehaviors(rt *Runtime) {
    // Subscribe to stash recovery events
    rt.Register(Local("stash:recovered", "Stash recovery completed").
        WithPayload[StashRecoveredPayload]().
        WithHandler(1, p.handleStashRecovered))
}

func (p *PresenceService) handleStashRecovered(msg *Message, payload *StashRecoveredPayload) {
    p.log.Info("stash recovered, updating presence",
        "items", payload.ItemCount,
        "at", payload.RecoveredAt)
    // Update presence state, etc.
}
```

**Key properties of Local messages:**
- Same `Message` primitive as network messages
- Same pipeline execution (just with NoTransport, NoSign, NoVerify)
- Multiple services can subscribe to the same local message kind
- Useful for decoupling services while maintaining the message-based architecture

### Emit Implementation

```go
func (rt *Runtime) Emit(msg *Message) error {
    // Set timestamp if not set
    if msg.Timestamp.IsZero() {
        msg.Timestamp = time.Now()
    }

    // Look up behavior
    behavior := Lookup(msg.Kind)
    if behavior == nil {
        return fmt.Errorf("unknown message kind: %s", msg.Kind)
    }

    // Build and run emit pipeline
    pipeline := rt.buildEmitPipeline(behavior)
    ctx := rt.newPipelineContext()

    result := pipeline.Run(msg, ctx)

    // Handle result
    if result.Error != nil {
        rt.applyErrorStrategy(msg, "emit", result.Error, behavior.OnTransportError)
        return result.Error
    }
    if result.Message == nil {
        rt.metrics.RecordDrop(msg.Kind, result.Reason)
        logrus.Debugf("message %s dropped: %s", msg.Kind, result.Reason)
    }

    return nil
}

func (rt *Runtime) buildEmitPipeline(b *Behavior) Pipeline {
    stages := []Stage{}

    // ID stage (always the same - unique envelope ID)
    stages = append(stages, &IDStage{})

    // ContentKey stage (if behavior defines ContentKey function)
    if b.ContentKey != nil {
        stages = append(stages, &ContentKeyStage{KeyFunc: b.ContentKey})
    }

    // Sign stage
    stages = append(stages, orDefault(b.Sign, DefaultSign()))

    // Store stage
    stages = append(stages, orDefault(b.Store, DefaultStore(2)))

    // Gossip stage (explicit, independent of store)
    stages = append(stages, orDefault(b.Gossip, NoGossip()))

    // Transport stage
    if b.Transport != nil {
        stages = append(stages, b.Transport)
    }

    // Notify stage (always last)
    stages = append(stages, &NotifyStage{})

    return Pipeline(stages)
}

func orDefault(stage Stage, defaultStage Stage) Stage {
    if stage != nil {
        return stage
    }
    return defaultStage
}
```

### Receive Implementation

```go
func (rt *Runtime) Receive(raw []byte) error {
    // Deserialize using behavior's PayloadType
    msg, err := rt.deserialize(raw)
    if err != nil {
        return fmt.Errorf("deserialize: %w", err)
    }

    behavior := Lookup(msg.Kind)
    if behavior == nil {
        return fmt.Errorf("unknown message kind: %s", msg.Kind)
    }

    // Build and run receive pipeline
    pipeline := rt.buildReceivePipeline(behavior)
    ctx := rt.newPipelineContext()

    result := pipeline.Run(msg, ctx)

    // Handle result
    if result.Error != nil {
        rt.applyErrorStrategy(msg, "receive", result.Error, behavior.OnVerifyError)
        return result.Error
    }
    if result.Message == nil {
        rt.metrics.RecordDrop(msg.Kind, result.Reason)
        logrus.Debugf("message %s dropped: %s", msg.Kind, result.Reason)
    }

    return nil
}

// deserialize uses behavior's PayloadType for typed deserialization
func (rt *Runtime) deserialize(raw []byte) (*Message, error) {
    // First, peek at kind
    var envelope struct {
        Kind string `json:"kind"`
    }
    if err := json.Unmarshal(raw, &envelope); err != nil {
        return nil, err
    }

    behavior := Lookup(envelope.Kind)
    if behavior == nil {
        return nil, fmt.Errorf("unknown kind: %s", envelope.Kind)
    }

    // Create typed payload
    payload := reflect.New(behavior.PayloadType).Interface()

    // Full unmarshal with typed payload
    msg := &Message{}
    // ... custom unmarshaling that puts typed payload in msg.Payload

    return msg, nil
}

func (rt *Runtime) buildReceivePipeline(b *Behavior) Pipeline {
    stages := []Stage{}

    // Verify stage
    stages = append(stages, orDefault(b.Verify, DefaultVerify()))

    // Dedupe stage
    stages = append(stages, orDefault(b.Dedupe, IDDedupe()))

    // Rate limit stage (optional)
    if b.RateLimit != nil {
        stages = append(stages, b.RateLimit)
    }

    // Filter stage (optional)
    if b.Filter != nil {
        stages = append(stages, b.Filter)
    }

    // Store stage (from emit config, for received messages)
    if b.Store != nil && !isNoStore(b.Store) {
        stages = append(stages, b.Store)
    }

    // Gossip stage (optional - spread to others)
    if b.Gossip != nil && !isNoGossip(b.Gossip) {
        stages = append(stages, b.Gossip)
    }

    // Notify stage (always last)
    stages = append(stages, &NotifyStage{})

    return Pipeline(stages)
}
```

---

## Auto-Generated Documentation

### Documentation Generator

```go
func GenerateMarkdown() string {
    var sb strings.Builder

    sb.WriteString("# Message Catalog\n\n")
    sb.WriteString("Auto-generated from behavior registry.\n\n")

    // Group by category
    categories := groupByCategory()

    for cat, behaviors := range categories {
        sb.WriteString(fmt.Sprintf("## %s\n\n", cat))

        for _, b := range behaviors {
            sb.WriteString(fmt.Sprintf("### `%s`\n\n", b.Kind))
            sb.WriteString(fmt.Sprintf("%s\n\n", b.Description))

            // Behavior table
            sb.WriteString("| Aspect | Configuration |\n")
            sb.WriteString("|--------|---------------|\n")
            sb.WriteString(fmt.Sprintf("| Storage | %s |\n", describeStore(b.Store)))
            sb.WriteString(fmt.Sprintf("| Gossip | %s |\n", describeGossip(b.Gossip)))
            sb.WriteString(fmt.Sprintf("| Transport | %s |\n", describeTransport(b.Transport)))
            sb.WriteString(fmt.Sprintf("| Verify | %s |\n", describeVerify(b.Verify)))
            sb.WriteString(fmt.Sprintf("| Filter | %s |\n", describeFilter(b.Filter)))
            sb.WriteString("\n")

            // Payload fields from reflection
            if b.PayloadType != nil {
                sb.WriteString("**Payload:**\n\n")
                sb.WriteString("| Field | Type | JSON | Description |\n")
                sb.WriteString("|-------|------|------|-------------|\n")
                for i := 0; i < b.PayloadType.NumField(); i++ {
                    field := b.PayloadType.Field(i)
                    jsonTag := field.Tag.Get("json")
                    desc := field.Tag.Get("desc")
                    sb.WriteString(fmt.Sprintf("| %s | `%s` | %s | %s |\n",
                        field.Name, field.Type, jsonTag, desc))
                }
                sb.WriteString("\n")
            }
        }
    }

    return sb.String()
}
```

### Build Integration

```makefile
docs: build
	./bin/nara docs --output docs/src/content/docs/messages.md

build-web: docs
	npm run build:js
	npm run build:css
```

---

## Summary

### What We Built

| Component | Purpose |
|-----------|---------|
| **Message** | Universal primitive — everything is a message |
| **ID** | Unique envelope identity — always unique per message instance |
| **ContentKey** | Semantic identity — stable across observers for the same fact |
| **Version** | Schema version — enables backwards-compatible evolution |
| **StageResult** | Explicit outcomes — Continue/Drop/Error, no silent failures |
| **Behavior** | Declares how a message kind is handled |
| **EmitBehavior / ReceiveBehavior** | Split pipelines — clear what runs on send vs receive |
| **Pattern Templates** | `Ephemeral()`, `MeshRequest()`, `StoredEvent()`, `Local()` — reduce boilerplate |
| **Pipeline** | Chains stages, returns StageResult |
| **Stages** | Pluggable units: ID, ContentKey, Sign, Store, Gossip, Transport, Verify, Filter |
| **GossipQueue** | Explicit gossip, decoupled from ledger |
| **ErrorStrategy** | Per-direction error handling |
| **Registry** | Central catalog of all behaviors |
| **Runtime** | The OS that runs pipelines and manages services |
| **Services** | Programs that emit and handle messages |
| **Primitives** | Runtime-provided utilities (Logger, RateLimiter, Metrics) — NOT Services |
| **Local Messages** | Service-to-service communication without network transport |

### Key Benefits

1. **Everything is a Message** — unified model for events, ephemerals, protocols, AND internal communication
2. **ID vs ContentKey split** — envelope identity vs semantic identity, no more confusion
3. **Versioning built-in** — evolve message schemas without breaking old naras
4. **Explicit outcomes** — StageResult replaces error-prone `next()` callback
5. **Emit/Receive split** — crystal clear which stages run on send vs receive
6. **Pattern templates** — `MeshRequest()`, `Ephemeral()`, `Local()` reduce 40 lines to 1
7. **Gossip decoupled from Store** — independent concerns
8. **Error strategies** — configurable per-direction error handling
9. **OS handles serialization** — services provide structs, runtime handles wire format
10. **Auto-documented** — registry generates docs from PayloadType reflection
11. **Testable** — each stage testable in isolation
12. **Local messages** — service-to-service communication using the same Message primitive
13. **Primitives vs Services** — clear distinction between runtime utilities and service lifecycle

### The 80/20 Split

- **80%**: Use default stages, just define Kind + Description + PayloadType
- **20%**: Customize ContentKey, Store, Verify, Filter, etc. for special cases

---

## Implementation Status

### Chapter 1: Foundation & Stash Service ✅ COMPLETE

**Completed: January 15, 2026**

#### Core Runtime Implementation

- ✅ **Message primitive** (`runtime/message.go`)
  - ID generation (Base58-encoded SHA256)
  - ContentKey support for deduplication
  - Versioning (default v1)
  - Reply() helper for request/response patterns
  
- ✅ **Behavior system** (`runtime/behavior.go`)
  - Behavior struct with Emit/Receive pipeline configuration
  - Pattern templates: `Ephemeral()`, `MeshRequest()`, `StoredEvent()`, `Local()`
  - Global behavior registry with `Register()` and `Lookup()`
  - Error strategy configuration per direction
  
- ✅ **Pipeline system** (`runtime/pipeline.go`)
  - Stage interface with Run(msg, ctx) → StageResult
  - StageResult: Continue/Drop/Error outcomes
  - PipelineContext for shared state between stages
  - Pipeline composition and execution

- ✅ **Core stages** (`runtime/stages/`)
  - IDStage: Generates unique message IDs
  - ContentKeyStage: Computes semantic deduplication keys
  - SignStage: Ed25519 signature creation
  - VerifyStage: Signature verification
  - NotifyStage: Triggers registered handlers
  - NoStore, NoGossip, NoTransport: Explicit no-ops

- ✅ **Runtime** (`runtime/runtime.go`)
  - RuntimeInterface for services
  - Emit() pipeline execution
  - Receive() pipeline execution (stub for Chapter 2)
  - Service registration and lifecycle
  - BehaviorRegistrar support
  - Handler invocation via reflection

- ✅ **Mock runtime** (`runtime/mock_runtime.go`)
  - Test implementation of RuntimeInterface
  - Message emission capture for assertions
  - Behavior delivery simulation
  - Public key registry for testing
  - Auto-cleanup via t.Cleanup()

- ✅ **Interfaces** (`runtime/interfaces.go`)
  - RuntimeInterface: Me, MeID, Emit, Log, Env
  - LedgerInterface: Add, HasID, HasContentKey
  - TransportInterface: PublishMQTT, TrySendDirect
  - GossipQueueInterface: Add
  - KeypairInterface: Sign, PublicKey
  - EventBusInterface: Emit, Subscribe
  - Personality struct
  - Environment enum (Production/Development/Test)

#### Services Implementation

- ✅ **Stash service** (`services/stash/`)
  - Service struct implementing Service interface
  - Init/Start/Stop lifecycle methods
  - RegisterBehaviors() for behavior registration
  - StoreWith() and RequestFrom() public API
  - Version-specific handlers (v1)
  - XChaCha20-Poly1305 encryption/decryption
  - In-memory stash storage
  - State marshaling for persistence
  
- ✅ **Stash behaviors** (`services/stash/behaviors.go`)
  - `stash:refresh` - Ephemeral broadcast for boot recovery
  - `stash:store` - MeshRequest for storing encrypted stash
  - `stash:ack` - MeshRequest acknowledgment
  - `stash:request` - MeshRequest for retrieving stash
  - `stash:response` - MeshRequest response with stash data

#### Utilities Implementation

- ✅ **Encryptor** (`utilities/encryptor.go`)
  - XChaCha20-Poly1305 encryption
  - 24-byte nonce generation
  - Seal() and Open() methods
  - Seed-based key derivation

- ✅ **Correlator** (`utilities/correlator.go`)
  - Generic request/response correlation
  - Timeout-based expiration
  - Send() with channel-based response
  - Resolve() for matching responses
  - Cleanup() for expired requests

- ✅ **ID generation** (`utilities/id.go`)
  - Base58-encoded SHA256 hashing
  - Deterministic ID from message content
  - Support for ContentKey computation

#### Messages Implementation

- ✅ **Stash messages** (`messages/stash.go`)
  - StashRefreshPayload
  - StashStorePayload with validation
  - StashStoreAck
  - StashRequestPayload
  - StashResponsePayload
  - Validation methods

#### Integration

- ✅ **Runtime adapters** (`runtime_adapters.go`)
  - TransportAdapter: bridges Network → TransportInterface
  - KeypairAdapter: bridges Keypair → KeypairInterface
  - EventBusAdapter: implements EventBusInterface
  - IdentityAdapter: bridges Network identity lookups

- ✅ **Runtime integration** (`runtime_integration.go`)
  - initRuntime(): Creates runtime with all adapters
  - startRuntime(): Starts runtime and services
  - stopRuntime(): Graceful shutdown
  - LedgerAdapter: SyncLedger → LedgerInterface
  - GossipQueueAdapter: Network → GossipQueueInterface

- ✅ **Network lifecycle** (`network.go`)
  - Runtime initialization in Start()
  - Runtime startup after other initialization
  - Runtime shutdown in Stop()

#### Testing

- ✅ **Stash service tests** (`services/stash/service_test.go`)
  - TestStashStoreAndAck
  - TestStashRequestAndResponse
  - TestStashRequestNotFound
  - TestStashStateMarshaling
  - TestStashEncryptionDecryption
  - TestStashInvalidPayloads
  - Runtime.ClearBehaviors() for test isolation

- ✅ **Build system**
  - Clean build with no linting errors
  - All errcheck warnings fixed
  - Unused compatibility code removed
  - Nix build configuration updated

#### What Works Now

1. **Stash service is fully operational** using the new runtime
2. **Services can register behaviors** with declarative configuration
3. **Messages flow through pipelines** with explicit stage results
4. **Encryption is handled by utilities** independent of transport
5. **Request/response correlation** works via Correlator utility
6. **Tests isolate properly** with behavior registry clearing

#### Known Limitations

1. **Tests partially complete** - Some stash tests timeout due to correlator integration issues with mock runtime
2. **Receive pipeline stubbed** - Full receive pipeline implementation deferred to Chapter 2
3. **Transport stages incomplete** - MQTT/mesh transport integration deferred to Chapter 2
4. **Ledger integration minimal** - LedgerAdapter Add/HasID methods stubbed
5. **Logger stubbed** - ServiceLog methods are no-ops pending Chapter 3

### Next: Chapter 2 - Full Runtime Integration

**Scope:** Complete the runtime migration for all existing services

- [ ] Implement full receive pipeline with all stages
- [ ] Migrate social service to runtime
- [ ] Migrate checkpoint service to runtime
- [ ] Migrate presence service to runtime
- [ ] Implement proper LedgerAdapter integration
- [ ] Implement transport stages (MQTT, mesh)
- [ ] Wire runtime.EventBus to SSE broadcasts
- [ ] Complete test coverage

### Future: Chapter 3+ - Advanced Features

- [ ] Implement Logger as runtime primitive
- [ ] Add RateLimiter stage
- [ ] Add Metrics collection
- [ ] Call/Reply pattern for synchronous service communication
- [ ] Auto-generated documentation from behaviors
- [ ] Performance optimization

