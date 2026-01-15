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
8. [Behavior Registry](#behavior-registry)
9. [Complete Behavior Catalog](#complete-behavior-catalog)
10. [Services](#services)
11. [Runtime Implementation](#runtime-implementation)
12. [Auto-Generated Documentation](#auto-generated-documentation)

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
    ID        string    // Unique identifier (computed by runtime)
    Kind      string    // "hey-there", "observation:restart", "checkpoint"
    From      string    // Who created this message
    Timestamp time.Time // When it was created

    // Content
    Payload   any       // Kind-specific data (Go struct, runtime handles serialization)

    // Cryptographic (attached by runtime)
    Signature []byte    // Creator's signature (may be nil for some kinds)
}

// ComputeID generates deterministic ID from content
// Default implementation - can be overridden per kind
func DefaultComputeID(msg *Message) string {
    h := sha256.New()
    h.Write([]byte(msg.Kind))
    h.Write([]byte(msg.From))
    h.Write([]byte(msg.Timestamp.Format(time.RFC3339Nano)))
    h.Write(payloadHash(msg.Payload))
    return base58.Encode(h.Sum(nil))[:16]
}
```

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
Message → [ID] → [Sign] → [Store] → [Gossip?] → [Transport] → [Notify]
                                        ↓
                              Returns StageResult
```

**Receive Pipeline** (incoming messages):
```
Message → [Verify] → [Dedupe] → [RateLimit] → [Filter] → [Store] → [Notify]
                                        ↓
                              Returns StageResult
```

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

Computes the message ID. Default uses (kind, from, timestamp, payload hash).

```go
// DefaultIDStage computes ID from all fields
type DefaultIDStage struct{}

func (s *DefaultIDStage) Process(msg *Message, ctx *PipelineContext) StageResult {
    if msg.ID == "" {
        msg.ID = DefaultComputeID(msg)
    }
    return Continue(msg)
}

// ContentIDStage computes ID from payload content only (for dedup across observers)
type ContentIDStage struct {
    ContentFunc func(payload any) string
}

func (s *ContentIDStage) Process(msg *Message, ctx *PipelineContext) StageResult {
    if msg.ID == "" {
        content := s.ContentFunc(msg.Payload)
        h := sha256.Sum256([]byte(msg.Kind + ":" + content))
        msg.ID = base58.Encode(h[:])[:16]
    }
    return Continue(msg)
}
```

**Usage:**
```go
// Default: most messages
ID: DefaultID()

// Custom: observations use content-based ID for cross-observer dedup
ID: ContentID(func(p any) string {
    obs := p.(*ObservationPayload)
    return fmt.Sprintf("%s:%d:%d", obs.Subject, obs.RestartNum, obs.StartTime)
})
```

---

### 2. Sign Stage

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

### 3. Store Stage

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

// DedupStoreStage stores with content-based deduplication
type DedupStoreStage struct {
    Priority  int
    IsSameAs  func(new, existing *Message) bool
}

func (s *DedupStoreStage) Process(msg *Message, ctx *PipelineContext) StageResult {
    if ctx.Ledger.HasMatching(msg.Kind, func(existing *Message) bool {
        return s.IsSameAs(msg, existing)
    }) {
        return Drop("duplicate")  // Explicit drop reason
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

### 4. Gossip Stage

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

### 5. Transport Stage

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

// DirectFirstStage tries mesh HTTP before falling back to broadcast
type DirectFirstStage struct {
    Topic string
}

func (s *DirectFirstStage) Process(msg *Message, ctx *PipelineContext) StageResult {
    // Try direct mesh delivery first
    if target := extractTarget(msg); target != "" {
        if err := ctx.Transport.TrySendDirect(target, msg); err == nil {
            return Continue(msg)  // Sent directly
        }
    }
    // Fall back to broadcast
    if err := ctx.Transport.PublishMQTT(s.Topic, msg.Marshal()); err != nil {
        return Fail(fmt.Errorf("mqtt publish: %w", err))
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

### 6. Notify Stage

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

Prevents storing duplicate messages.

```go
// IDDedupeStage rejects messages with duplicate ID
type IDDedupeStage struct{}

func (s *IDDedupeStage) Process(msg *Message, ctx *PipelineContext) StageResult {
    if ctx.Ledger.HasID(msg.ID) {
        return Drop("duplicate_id")
    }
    return Continue(msg)
}

// ContentDedupeStage rejects messages with duplicate content
type ContentDedupeStage struct {
    IsSameAs func(new, existing *Message) bool
}

func (s *ContentDedupeStage) Process(msg *Message, ctx *PipelineContext) StageResult {
    if ctx.Ledger.HasMatching(msg.Kind, func(existing *Message) bool {
        return s.IsSameAs(msg, existing)
    }) {
        return Drop("duplicate_content")
    }
    return Continue(msg)
}
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

## Behavior Registry

### Behavior Definition

Each message kind has a Behavior that defines its pipelines:

```go
type Behavior struct {
    // Identity
    Kind        string       // Unique identifier, e.g., "observation:restart"
    Description string       // Human-readable description
    PayloadType reflect.Type // The Go struct type for payload (runtime handles serialization)

    // Emit pipeline stages (nil = use default)
    ID        Stage  // IDStage
    Sign      Stage  // SignStage
    Store     Stage  // StoreStage
    Gossip    Stage  // GossipStage (explicit, not coupled to Store)
    Transport Stage  // TransportStage

    // Receive pipeline stages (nil = use default or skip)
    Verify    Stage  // VerifyStage
    Dedupe    Stage  // DedupeStage
    RateLimit Stage  // RateLimitStage
    Filter    Stage  // FilterStage

    // Error handling
    OnTransportError ErrorStrategy
    OnStoreError     ErrorStrategy
    OnVerifyError    ErrorStrategy
}

// PayloadTypeOf is a helper to get reflect.Type from a struct
func PayloadTypeOf[T any]() reflect.Type {
    var zero T
    return reflect.TypeOf(zero)
}
```

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
// === ID Helpers ===
func DefaultID() Stage { return &DefaultIDStage{} }
func ContentID(f func(any) string) Stage { return &ContentIDStage{ContentFunc: f} }

// === Sign Helpers ===
func DefaultSign() Stage { return &DefaultSignStage{} }
func NoSign() Stage { return &NoSignStage{} }

// === Store Helpers ===
func DefaultStore(priority int) Stage { return &DefaultStoreStage{Priority: priority} }
func NoStore() Stage { return &NoStoreStage{} }
func DedupStore(priority int, isSame func(*Message, *Message) bool) Stage {
    return &DedupStoreStage{Priority: priority, IsSameAs: isSame}
}

// === Gossip Helpers ===
func Gossip() Stage { return &GossipStage{} }
func NoGossip() Stage { return &NoGossipStage{} }

// === Transport Helpers ===
func MQTT(topic string) Stage { return &MQTTStage{Topic: topic} }
func MQTTPerNara(pattern string) Stage { return &MQTTPerNaraStage{TopicPattern: pattern} }
func DirectFirst(topic string) Stage { return &DirectFirstStage{Topic: topic} }
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
func ContentDedupe(f func(*Message, *Message) bool) Stage { return &ContentDedupeStage{IsSameAs: f} }

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
        ID:          ContentID(restartContentID),
        Store:       DedupStore(0, restartIsSame),
        Gossip:      Gossip(),
        Transport:   NoTransport(),  // Gossip only, no MQTT
        Verify:      DefaultVerify(),
        Dedupe:      ContentDedupe(restartIsSame),
        RateLimit:   RateLimit(5*time.Minute, 10, subjectKey),
        Filter:      Critical(),
        OnStoreError: ErrorLog,
    })

    // First-seen observation
    Register(&Behavior{
        Kind:        "observation:first-seen",
        Description: "Records first time a nara is observed",
        PayloadType: PayloadTypeOf[ObservationFirstSeenPayload](),
        ID:          ContentID(firstSeenContentID),
        Store:       DedupStore(0, firstSeenIsSame),
        Gossip:      Gossip(),
        Transport:   NoTransport(),
        Verify:      DefaultVerify(),
        Dedupe:      ContentDedupe(firstSeenIsSame),
        RateLimit:   RateLimit(5*time.Minute, 10, subjectKey),
        Filter:      Critical(),
    })

    // Status change observation
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

// Helper functions
func restartContentID(p any) string {
    obs := p.(*ObservationRestartPayload)
    return fmt.Sprintf("%s:%d:%d", obs.Subject, obs.RestartNum, obs.StartTime.Unix())
}

func restartIsSame(new, existing *Message) bool {
    n := new.Payload.(*ObservationRestartPayload)
    e := existing.Payload.(*ObservationRestartPayload)
    return n.Subject == e.Subject &&
           n.RestartNum == e.RestartNum &&
           n.StartTime.Equal(e.StartTime)
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
        Transport:   DirectFirst("nara/plaza/social"),
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

```go
func init() {
    // Checkpoint proposal
    Register(&Behavior{
        Kind:        "checkpoint:propose",
        Description: "Checkpoint proposal (consensus protocol)",
        PayloadType: PayloadTypeOf[CheckpointProposalPayload](),
        Store:       NoStore(),
        Gossip:      NoGossip(),
        Transport:   MQTT("nara/checkpoint/propose"),
        Verify:      DefaultVerify(),
        Filter:      Critical(),
    })

    // Checkpoint vote
    Register(&Behavior{
        Kind:        "checkpoint:vote",
        Description: "Checkpoint vote (consensus protocol)",
        PayloadType: PayloadTypeOf[CheckpointVotePayload](),
        Store:       NoStore(),
        Gossip:      NoGossip(),
        Transport:   MQTT("nara/checkpoint/vote"),
        Verify:      DefaultVerify(),
        Filter:      Critical(),
    })

    // Sync request
    Register(&Behavior{
        Kind:        "sync:request",
        Description: "Ledger sync request",
        PayloadType: PayloadTypeOf[SyncRequestPayload](),
        Store:       NoStore(),
        Gossip:      NoGossip(),
        Transport:   MQTTPerNara("nara/ledger/%s/request"),
        Verify:      NoVerify(),
        Filter:      Critical(),
    })

    // Sync response
    Register(&Behavior{
        Kind:        "sync:response",
        Description: "Ledger sync response",
        PayloadType: PayloadTypeOf[SyncResponsePayload](),
        Store:       NoStore(),
        Gossip:      NoGossip(),
        Transport:   MQTTPerNara("nara/ledger/%s/response"),
        Verify:      DefaultVerify(),
        Filter:      Critical(),
    })
}
```

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

// Optional interfaces services can implement
type MessageHandler interface {
    Kinds() []string
    Handle(msg *Message)
}

type BehaviorRegistrar interface {
    RegisterBehaviors()
}
```

### Service Example: Presence

```go
type PresenceService struct {
    rt *Runtime
}

func (s *PresenceService) Name() string { return "presence" }

func (s *PresenceService) RegisterBehaviors() {
    Register(&Behavior{
        Kind:        "hey-there",
        Description: "Identity announcement",
        // ...
    })
    // ...
}

func (s *PresenceService) Kinds() []string {
    return []string{"hey-there", "chau", "newspaper", "howdy"}
}

func (s *PresenceService) Handle(msg *Message) {
    switch msg.Kind {
    case "hey-there":
        s.handleHeyThere(msg)
    case "howdy":
        s.handleHowdy(msg)
    }
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
            s.rt.Emit(&Message{
                Kind:    "hey-there",
                From:    s.rt.Me().Name,
                Payload: s.buildHeyTherePayload(),
            })
        }
    }
}
```

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

    // Lifecycle
    ctx    context.Context
    cancel context.CancelFunc
}
```

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

    // ID stage
    stages = append(stages, orDefault(b.ID, DefaultID()))

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
| **StageResult** | Explicit outcomes — Continue/Drop/Error, no silent failures |
| **Behavior** | Declares how a message kind is handled |
| **Pipeline** | Chains stages, returns StageResult |
| **Stages** | Pluggable units: ID, Sign, Store, Gossip, Transport, Verify, Filter |
| **GossipQueue** | Explicit gossip, decoupled from ledger |
| **ErrorStrategy** | Per-behavior error handling |
| **Registry** | Central catalog of all behaviors |
| **Runtime** | The OS that runs pipelines and manages services |
| **Services** | Programs that emit and handle messages |

### Key Benefits

1. **Everything is a Message** — unified model for events, ephemerals, protocols
2. **Explicit outcomes** — StageResult replaces error-prone `next()` callback
3. **Gossip decoupled from Store** — independent concerns
4. **Error strategies** — configurable per-behavior error handling
5. **OS handles serialization** — services provide structs, runtime handles wire format
6. **Auto-documented** — registry generates docs from PayloadType reflection
7. **Testable** — each stage testable in isolation

### The 80/20 Split

- **80%**: Use default stages, just define Kind + Description + PayloadType
- **20%**: Customize specific stages (ID, Sign, Verify, Filter, etc.)
