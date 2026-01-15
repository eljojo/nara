# Nara Runtime Architecture

A comprehensive design for restructuring Nara into a runtime with pluggable services and composable message pipelines.

---

## Table of Contents

1. [Vision](#vision)
2. [Core Primitives](#core-primitives)
3. [The Pipeline Pattern](#the-pipeline-pattern)
4. [Emit Pipeline Stages](#emit-pipeline-stages)
5. [Receive Pipeline Stages](#receive-pipeline-stages)
6. [Behavior Registry](#behavior-registry)
7. [Complete Behavior Catalog](#complete-behavior-catalog)
8. [Services](#services)
9. [Runtime Implementation](#runtime-implementation)
10. [Migration Strategy](#migration-strategy)
11. [Auto-Generated Documentation](#auto-generated-documentation)

---

## Vision

### Nara as an Operating System

Nara is a **runtime**. Services are **programs** that run on it. The runtime provides primitives (storage, transport, identity). Services use them, and can swap out pieces with custom implementations.

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
│   └─────────────────────────────────────────────────────────┘   │
│                                                                  │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │               RECEIVE PIPELINE                           │   │
│   │   Message → [Verify] → [RateLimit] → [Filter] → [Store] │   │
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
5. **Middleware pattern** — stages chain like HTTP middleware, easy to reason about
6. **Auto-documented** — the registry generates documentation automatically

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
    Payload   any       // Kind-specific data (struct)

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
- Has a **default** behavior (most messages use this)
- Can be **customized** (service provides alternative)
- Can be **skipped** (stage returns early or is nil)

### Pipeline Interface

```go
// Stage processes a message and calls next to continue
type Stage interface {
    Process(msg *Message, ctx *PipelineContext, next func())
}

// PipelineContext carries runtime dependencies
type PipelineContext struct {
    Runtime     *Runtime
    Ledger      *Ledger
    Transport   *Transport
    Keypair     NaraKeypair
    Personality *Personality
    EventBus    *EventBus
}

// Pipeline chains stages
type Pipeline []Stage

func (p Pipeline) Run(msg *Message, ctx *PipelineContext) {
    var run func(int)
    run = func(i int) {
        if i >= len(p) {
            return // End of pipeline
        }
        p[i].Process(msg, ctx, func() { run(i + 1) })
    }
    run(0)
}
```

### Emit vs Receive

Two pipelines, different purposes:

**Emit Pipeline** (outgoing messages):
```
Message → [ID] → [Sign] → [Store] → [Transport] → [Notify]
```

**Receive Pipeline** (incoming messages):
```
Message → [Verify] → [Dedupe] → [RateLimit] → [Filter] → [Store] → [Notify]
```

---

## Emit Pipeline Stages

### 1. ID Stage

Computes the message ID. Default uses (kind, from, timestamp, payload hash).

```go
// DefaultIDStage computes ID from all fields
type DefaultIDStage struct{}

func (s *DefaultIDStage) Process(msg *Message, ctx *PipelineContext, next func()) {
    if msg.ID == "" {
        msg.ID = DefaultComputeID(msg)
    }
    next()
}

// ContentIDStage computes ID from payload content only (for dedup across observers)
type ContentIDStage struct {
    // ContentFunc extracts the content string for hashing
    ContentFunc func(payload any) string
}

func (s *ContentIDStage) Process(msg *Message, ctx *PipelineContext, next func()) {
    if msg.ID == "" {
        content := s.ContentFunc(msg.Payload)
        h := sha256.Sum256([]byte(msg.Kind + ":" + content))
        msg.ID = base58.Encode(h[:])[:16]
    }
    next()
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

func (s *DefaultSignStage) Process(msg *Message, ctx *PipelineContext, next func()) {
    msg.Signature = ctx.Keypair.Sign(msg.SignableContent())
    next()
}

// NoSignStage skips signing (for messages where signature is in payload)
type NoSignStage struct{}

func (s *NoSignStage) Process(msg *Message, ctx *PipelineContext, next func()) {
    next() // No signature on Message itself
}
```

**Usage:**
```go
// Default: most messages
Sign: DefaultSign()

// Custom: checkpoints have multi-sig in payload
Sign: NoSign()
```

---

### 3. Store Stage

Stores the message in the ledger.

```go
// DefaultStoreStage stores with a GC priority
type DefaultStoreStage struct {
    Priority int // 0 = never prune, higher = prune sooner
}

func (s *DefaultStoreStage) Process(msg *Message, ctx *PipelineContext, next func()) {
    ctx.Ledger.Add(msg, s.Priority)
    next()
}

// NoStoreStage skips storage (ephemeral messages)
type NoStoreStage struct{}

func (s *NoStoreStage) Process(msg *Message, ctx *PipelineContext, next func()) {
    next() // Don't store
}

// DedupStoreStage stores with content-based deduplication
type DedupStoreStage struct {
    Priority  int
    IsSameAs  func(new, existing *Message) bool
}

func (s *DedupStoreStage) Process(msg *Message, ctx *PipelineContext, next func()) {
    // Check for existing duplicate
    if ctx.Ledger.HasMatching(msg.Kind, func(existing *Message) bool {
        return s.IsSameAs(msg, existing)
    }) {
        return // Duplicate, don't store or continue
    }
    ctx.Ledger.Add(msg, s.Priority)
    next()
}
```

**Usage:**
```go
// Default: store with priority
Store: DefaultStore(2)

// Ephemeral: don't store
Store: NoStore()

// Custom: dedup by content
Store: DedupStore(0, func(new, existing *Message) bool {
    n := new.Payload.(*ObservationPayload)
    e := existing.Payload.(*ObservationPayload)
    return n.Subject == e.Subject &&
           n.RestartNum == e.RestartNum &&
           n.StartTime == e.StartTime
})
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

### 4. Transport Stage

Sends the message over the network.

```go
// MQTTStage broadcasts to a fixed MQTT topic
type MQTTStage struct {
    Topic string
}

func (s *MQTTStage) Process(msg *Message, ctx *PipelineContext, next func()) {
    ctx.Transport.PublishMQTT(s.Topic, msg.Marshal())
    next()
}

// MQTTPerNaraStage broadcasts to a per-nara topic
type MQTTPerNaraStage struct {
    TopicPattern string // e.g., "nara/newspaper/%s"
}

func (s *MQTTPerNaraStage) Process(msg *Message, ctx *PipelineContext, next func()) {
    topic := fmt.Sprintf(s.TopicPattern, msg.From)
    ctx.Transport.PublishMQTT(topic, msg.Marshal())
    next()
}

// GossipStage relies on gossip to spread (reads from ledger)
type GossipStage struct{}

func (s *GossipStage) Process(msg *Message, ctx *PipelineContext, next func()) {
    // Nothing to do here - gossip service reads from ledger
    next()
}

// BroadcastStage does both MQTT and gossip
type BroadcastStage struct {
    Topic string
}

func (s *BroadcastStage) Process(msg *Message, ctx *PipelineContext, next func()) {
    ctx.Transport.PublishMQTT(s.Topic, msg.Marshal())
    // Gossip will also pick up from ledger
    next()
}

// DirectFirstStage tries mesh HTTP before falling back to broadcast
type DirectFirstStage struct {
    Topic string
}

func (s *DirectFirstStage) Process(msg *Message, ctx *PipelineContext, next func()) {
    // Try direct mesh delivery first
    if target := extractTarget(msg); target != "" {
        if ctx.Transport.TrySendDirect(target, msg) {
            next()
            return
        }
    }
    // Fall back to broadcast
    ctx.Transport.PublishMQTT(s.Topic, msg.Marshal())
    next()
}
```

**Usage:**
```go
// MQTT only (ephemeral)
Transport: MQTT("nara/plaza/howdy")

// Per-nara topic
Transport: MQTTPerNara("nara/newspaper/%s")

// Gossip only (no MQTT)
Transport: Gossip()

// Both MQTT and gossip
Transport: Broadcast("nara/plaza/social")

// Try DM first, then broadcast
Transport: DirectFirst("nara/plaza/social")
```

---

### 5. Notify Stage

Always runs last. Notifies local subscribers.

```go
type NotifyStage struct{}

func (s *NotifyStage) Process(msg *Message, ctx *PipelineContext, next func()) {
    ctx.EventBus.Emit(msg)
    next()
}
```

---

## Receive Pipeline Stages

### 1. Verify Stage

Verifies the message signature.

```go
// DefaultVerifyStage verifies single signature against known public key
type DefaultVerifyStage struct{}

func (s *DefaultVerifyStage) Process(msg *Message, ctx *PipelineContext, next func()) {
    pubKey := ctx.Runtime.LookupPublicKey(msg.From)
    if pubKey == nil {
        return // Unknown sender, reject
    }
    if !msg.VerifySignature(pubKey) {
        return // Invalid signature, reject
    }
    next()
}

// SelfAttestingVerifyStage uses public key embedded in payload
type SelfAttestingVerifyStage struct {
    ExtractKey func(payload any) []byte
}

func (s *SelfAttestingVerifyStage) Process(msg *Message, ctx *PipelineContext, next func()) {
    pubKey := s.ExtractKey(msg.Payload)
    if !msg.VerifySignature(pubKey) {
        return // Invalid signature, reject
    }
    // Optionally register the key for future lookups
    ctx.Runtime.RegisterPublicKey(msg.From, pubKey)
    next()
}

// CustomVerifyStage for complex verification (e.g., checkpoint multi-sig)
type CustomVerifyStage struct {
    VerifyFunc func(msg *Message, ctx *PipelineContext) bool
}

func (s *CustomVerifyStage) Process(msg *Message, ctx *PipelineContext, next func()) {
    if !s.VerifyFunc(msg, ctx) {
        return // Verification failed, reject
    }
    next()
}

// NoVerifyStage skips verification
type NoVerifyStage struct{}

func (s *NoVerifyStage) Process(msg *Message, ctx *PipelineContext, next func()) {
    next()
}
```

**Usage:**
```go
// Default: verify against known public key
Verify: DefaultVerify()

// Self-attesting: hey-there embeds public key
Verify: SelfAttesting(func(p any) []byte {
    return p.(*HeyTherePayload).PublicKey
})

// Custom: checkpoint multi-sig
Verify: CustomVerify(checkpointVerifyMultiSig)

// Skip: unsigned ephemerals
Verify: NoVerify()
```

---

### 2. Dedupe Stage

Prevents storing duplicate messages.

```go
// IDDedupeStage rejects messages with duplicate ID
type IDDedupeStage struct{}

func (s *IDDedupeStage) Process(msg *Message, ctx *PipelineContext, next func()) {
    if ctx.Ledger.HasID(msg.ID) {
        return // Already have this exact message
    }
    next()
}

// ContentDedupeStage rejects messages with duplicate content
type ContentDedupeStage struct {
    IsSameAs func(new, existing *Message) bool
}

func (s *ContentDedupeStage) Process(msg *Message, ctx *PipelineContext, next func()) {
    if ctx.Ledger.HasMatching(msg.Kind, func(existing *Message) bool {
        return s.IsSameAs(msg, existing)
    }) {
        return // Duplicate content
    }
    next()
}
```

---

### 3. RateLimit Stage

Throttles incoming messages.

```go
type RateLimitStage struct {
    Window  time.Duration
    Max     int
    KeyFunc func(msg *Message) string // What to rate limit by
}

func (s *RateLimitStage) Process(msg *Message, ctx *PipelineContext, next func()) {
    key := s.KeyFunc(msg)
    if !ctx.Runtime.RateLimiter.Allow(key, s.Window, s.Max) {
        return // Rate limited, reject
    }
    next()
}
```

**Usage:**
```go
// Rate limit observations per subject
RateLimit: RateLimit(5*time.Minute, 10, func(msg *Message) string {
    return msg.Payload.(*ObservationPayload).Subject
})

// No rate limit
RateLimit: nil
```

---

### 4. Filter Stage

Filters messages based on local criteria (e.g., personality).

```go
// PersonalityFilterStage filters based on local nara's personality
type PersonalityFilterStage struct {
    FilterFunc func(msg *Message, personality *Personality) bool
}

func (s *PersonalityFilterStage) Process(msg *Message, ctx *PipelineContext, next func()) {
    if s.FilterFunc != nil && !s.FilterFunc(msg, ctx.Personality) {
        return // Filtered out by personality
    }
    next()
}

// ImportanceFilterStage uses importance levels
type ImportanceFilterStage struct {
    Importance int // 1=casual, 2=normal, 3=critical
    // For casual, provide custom filter
    CasualFilter func(msg *Message, personality *Personality) bool
}

func (s *ImportanceFilterStage) Process(msg *Message, ctx *PipelineContext, next func()) {
    switch s.Importance {
    case 3: // Critical - never filter
        next()
    case 2: // Normal - filter only if very chill
        if ctx.Personality.Chill <= 85 {
            next()
        }
    case 1: // Casual - use custom filter
        if s.CasualFilter == nil || s.CasualFilter(msg, ctx.Personality) {
            next()
        }
    default:
        next()
    }
}
```

**Usage:**
```go
// Critical: never filter
Filter: Critical()

// Normal: filtered by very chill naras
Filter: Normal()

// Casual with custom filter
Filter: Casual(func(msg *Message, p *Personality) bool {
    payload := msg.Payload.(*SocialPayload)
    switch payload.Reason {
    case ReasonRandom:
        return p.Chill < 70
    case ReasonNiceNumber:
        return p.Chill < 80
    default:
        return true
    }
})
```

---

## Behavior Registry

### Behavior Definition

Each message kind has a Behavior that defines its pipelines:

```go
type Behavior struct {
    // Identity
    Kind        string  // Unique identifier, e.g., "observation:restart"
    Description string  // Human-readable description
    Schema      *Schema // Payload schema (for validation and docs)

    // Emit pipeline stages (nil = use default)
    ID        IDStage
    Sign      SignStage
    Store     StoreStage
    Transport TransportStage

    // Receive pipeline stages (nil = use default or skip)
    Verify    VerifyStage
    Dedupe    DedupeStage
    RateLimit RateLimitStage
    Filter    FilterStage
}
```

### Registry

```go
var Behaviors = map[string]*Behavior{}

func Register(b *Behavior) {
    if b.Kind == "" {
        panic("behavior must have a Kind")
    }
    Behaviors[b.Kind] = b
}

func Lookup(kind string) *Behavior {
    return Behaviors[kind]
}
```

### Helper Constructors

```go
// === ID Helpers ===
func DefaultID() IDStage { return &DefaultIDStage{} }
func ContentID(f func(any) string) IDStage { return &ContentIDStage{ContentFunc: f} }

// === Sign Helpers ===
func DefaultSign() SignStage { return &DefaultSignStage{} }
func NoSign() SignStage { return &NoSignStage{} }

// === Store Helpers ===
func DefaultStore(priority int) StoreStage { return &DefaultStoreStage{Priority: priority} }
func NoStore() StoreStage { return &NoStoreStage{} }
func DedupStore(priority int, isSame func(*Message, *Message) bool) StoreStage {
    return &DedupStoreStage{Priority: priority, IsSameAs: isSame}
}

// === Transport Helpers ===
func MQTT(topic string) TransportStage { return &MQTTStage{Topic: topic} }
func MQTTPerNara(pattern string) TransportStage { return &MQTTPerNaraStage{TopicPattern: pattern} }
func Gossip() TransportStage { return &GossipStage{} }
func Broadcast(topic string) TransportStage { return &BroadcastStage{Topic: topic} }
func DirectFirst(topic string) TransportStage { return &DirectFirstStage{Topic: topic} }

// === Verify Helpers ===
func DefaultVerify() VerifyStage { return &DefaultVerifyStage{} }
func SelfAttesting(f func(any) []byte) VerifyStage { return &SelfAttestingVerifyStage{ExtractKey: f} }
func CustomVerify(f func(*Message, *PipelineContext) bool) VerifyStage { return &CustomVerifyStage{VerifyFunc: f} }
func NoVerify() VerifyStage { return &NoVerifyStage{} }

// === Dedupe Helpers ===
func IDDedupe() DedupeStage { return &IDDedupeStage{} }
func ContentDedupe(f func(*Message, *Message) bool) DedupeStage { return &ContentDedupeStage{IsSameAs: f} }

// === RateLimit Helpers ===
func RateLimit(window time.Duration, max int, keyFunc func(*Message) string) RateLimitStage {
    return &RateLimitStage{Window: window, Max: max, KeyFunc: keyFunc}
}

// === Filter Helpers ===
func Critical() FilterStage { return &ImportanceFilterStage{Importance: 3} }
func Normal() FilterStage { return &ImportanceFilterStage{Importance: 2} }
func Casual(f func(*Message, *Personality) bool) FilterStage {
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
        Store:       NoStore(),
        Transport:   MQTT("nara/plaza/howdy"),
        Verify:      NoVerify(),
        Filter:      Critical(),
    })

    // Presence heartbeat
    Register(&Behavior{
        Kind:        "newspaper",
        Description: "Periodic presence heartbeat with status",
        Schema:      NewspaperSchema,
        Store:       NoStore(),
        Transport:   MQTTPerNara("nara/newspaper/%s"),
        Verify:      DefaultVerify(),
        Filter:      Critical(),
    })

    // Stash recovery trigger
    Register(&Behavior{
        Kind:        "stash-refresh",
        Description: "Request stash recovery from confidants",
        Store:       NoStore(),
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
        Schema:      HeyThereSchema,
        Store:       DefaultStore(1),
        Transport:   Broadcast("nara/plaza/hey_there"),
        Verify:      SelfAttesting(func(p any) []byte {
            return p.(*HeyTherePayload).PublicKey
        }),
        Filter:      Critical(),
    })

    // Graceful shutdown
    Register(&Behavior{
        Kind:        "chau",
        Description: "Graceful shutdown announcement",
        Schema:      ChauSchema,
        Store:       DefaultStore(1),
        Transport:   Broadcast("nara/plaza/chau"),
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
        Schema:      ObservationRestartSchema,
        ID:          ContentID(restartContentID),
        Store:       DedupStore(0, restartIsSame),
        Transport:   Gossip(),
        Verify:      DefaultVerify(),
        Dedupe:      ContentDedupe(restartIsSame),
        RateLimit:   RateLimit(5*time.Minute, 10, subjectKey),
        Filter:      Critical(),
    })

    // First-seen observation
    Register(&Behavior{
        Kind:        "observation:first-seen",
        Description: "Records first time a nara is observed",
        Schema:      ObservationFirstSeenSchema,
        ID:          ContentID(firstSeenContentID),
        Store:       DedupStore(0, firstSeenIsSame),
        Transport:   Gossip(),
        Verify:      DefaultVerify(),
        Dedupe:      ContentDedupe(firstSeenIsSame),
        RateLimit:   RateLimit(5*time.Minute, 10, subjectKey),
        Filter:      Critical(),
    })

    // Status change observation
    Register(&Behavior{
        Kind:        "observation:status-change",
        Description: "Records online/offline transitions",
        Schema:      ObservationStatusChangeSchema,
        Store:       DefaultStore(1),
        Transport:   Gossip(),
        Verify:      DefaultVerify(),
        RateLimit:   RateLimit(5*time.Minute, 10, subjectKey),
        Filter:      Normal(),
    })
}

// Helper functions for observations
func restartContentID(p any) string {
    obs := p.(*ObservationPayload)
    return fmt.Sprintf("%s:%d:%d", obs.Subject, obs.RestartNum, obs.StartTime.Unix())
}

func restartIsSame(new, existing *Message) bool {
    n := new.Payload.(*ObservationPayload)
    e := existing.Payload.(*ObservationPayload)
    return n.Subject == e.Subject &&
           n.RestartNum == e.RestartNum &&
           n.StartTime.Equal(e.StartTime)
}

func subjectKey(msg *Message) string {
    return msg.Payload.(*ObservationPayload).Subject
}
```

### Social Events

```go
func init() {
    Register(&Behavior{
        Kind:        "social",
        Description: "Social interactions (teases, trends, etc.)",
        Schema:      SocialSchema,
        Store:       DefaultStore(2),
        Transport:   DirectFirst("nara/plaza/social"),
        Verify:      DefaultVerify(),
        Filter:      Casual(socialFilter),
    })
}

func socialFilter(msg *Message, p *Personality) bool {
    payload := msg.Payload.(*SocialPayload)

    // High chill: doesn't store random teases
    if p.Chill > 70 && payload.Reason == ReasonRandom {
        return false
    }

    // Very chill: only stores significant events
    if p.Chill > 85 {
        if payload.Reason != ReasonComeback && payload.Reason != ReasonHighRestarts {
            return false
        }
    }

    // Highly agreeable: doesn't store negative drama
    if p.Agreeableness > 80 && payload.Reason == ReasonTrendAbandon {
        return false
    }

    // Low sociability: less interested in drama
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
        Schema:      PingSchema,
        Store:       MaxPerKeyStore(4, 5, pingKey), // Priority 4, max 5 per target
        Transport:   Gossip(),
        Verify:      DefaultVerify(),
        Filter:      Casual(nil), // Always include
    })
}

func pingKey(msg *Message) string {
    p := msg.Payload.(*PingPayload)
    return msg.From + ":" + p.Target
}
```

### Seen Events

```go
func init() {
    Register(&Behavior{
        Kind:        "seen",
        Description: "Lightweight observation that a nara was seen",
        Schema:      SeenSchema,
        Store:       DefaultStore(3),
        Transport:   Gossip(),
        Verify:      DefaultVerify(),
        Filter:      Casual(nil),
    })
}
```

### Checkpoint Events

```go
func init() {
    // Final checkpoint (multi-party signed)
    Register(&Behavior{
        Kind:        "checkpoint",
        Description: "Multi-party signed consensus anchor",
        Schema:      CheckpointSchema,
        Sign:        NoSign(), // Signatures in payload
        Store:       DefaultStore(0), // Never prune
        Transport:   Broadcast("nara/checkpoint/final"),
        Verify:      CustomVerify(checkpointVerifyMultiSig),
        Filter:      Critical(),
    })
}

func checkpointVerifyMultiSig(msg *Message, ctx *PipelineContext) bool {
    cp := msg.Payload.(*CheckpointPayload)

    // Need minimum signatures
    if len(cp.Signatures) < MinCheckpointSignatures {
        return false
    }

    // Verify each signature
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

    return validCount >= MinCheckpointSignatures
}
```

### Protocol Messages

```go
func init() {
    // Checkpoint proposal
    Register(&Behavior{
        Kind:        "checkpoint:propose",
        Description: "Checkpoint proposal (consensus protocol)",
        Schema:      CheckpointProposalSchema,
        Store:       NoStore(),
        Transport:   MQTT("nara/checkpoint/propose"),
        Verify:      DefaultVerify(),
        Filter:      Critical(),
    })

    // Checkpoint vote
    Register(&Behavior{
        Kind:        "checkpoint:vote",
        Description: "Checkpoint vote (consensus protocol)",
        Schema:      CheckpointVoteSchema,
        Store:       NoStore(),
        Transport:   MQTT("nara/checkpoint/vote"),
        Verify:      DefaultVerify(),
        Filter:      Critical(),
    })

    // Sync request
    Register(&Behavior{
        Kind:        "sync:request",
        Description: "Ledger sync request",
        Schema:      SyncRequestSchema,
        Store:       NoStore(),
        Transport:   MQTTPerNara("nara/ledger/%s/request"),
        Verify:      NoVerify(),
        Filter:      Critical(),
    })

    // Sync response
    Register(&Behavior{
        Kind:        "sync:response",
        Description: "Ledger sync response",
        Schema:      SyncResponseSchema,
        Store:       NoStore(),
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
    // Kinds returns message kinds this service handles
    Kinds() []string
    // Handle is called when a message of a registered kind arrives
    Handle(msg *Message)
}

type BehaviorRegistrar interface {
    // RegisterBehaviors is called during init to let services register their behaviors
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
        // ... as defined above
    })
    Register(&Behavior{
        Kind:        "chau",
        // ...
    })
    Register(&Behavior{
        Kind:        "newspaper",
        // ...
    })
    Register(&Behavior{
        Kind:        "howdy",
        // ...
    })
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
    // ...
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
    transport *Transport

    // Services
    services []Service
    handlers map[string][]MessageHandler

    // Event bus
    eventBus *EventBus

    // Rate limiting
    rateLimiter *RateLimiter

    // Personality (for filtering)
    personality *Personality

    // Lifecycle
    ctx    context.Context
    cancel context.CancelFunc
}
```

### Emit Implementation

```go
func (rt *Runtime) Emit(msg *Message) {
    // Set timestamp if not set
    if msg.Timestamp.IsZero() {
        msg.Timestamp = time.Now()
    }

    // Look up behavior
    behavior := Lookup(msg.Kind)
    if behavior == nil {
        logrus.Warnf("unknown message kind: %s", msg.Kind)
        return
    }

    // Build and run emit pipeline
    pipeline := rt.buildEmitPipeline(behavior)
    ctx := &PipelineContext{
        Runtime:     rt,
        Ledger:      rt.ledger,
        Transport:   rt.transport,
        Keypair:     rt.keypair,
        Personality: rt.personality,
        EventBus:    rt.eventBus,
    }
    pipeline.Run(msg, ctx)
}

func (rt *Runtime) buildEmitPipeline(b *Behavior) Pipeline {
    stages := []Stage{}

    // ID stage
    if b.ID != nil {
        stages = append(stages, b.ID)
    } else {
        stages = append(stages, DefaultID())
    }

    // Sign stage
    if b.Sign != nil {
        stages = append(stages, b.Sign)
    } else {
        stages = append(stages, DefaultSign())
    }

    // Store stage
    if b.Store != nil {
        stages = append(stages, b.Store)
    } else {
        stages = append(stages, DefaultStore(2))
    }

    // Transport stage (required)
    if b.Transport != nil {
        stages = append(stages, b.Transport)
    }

    // Notify stage (always last)
    stages = append(stages, &NotifyStage{})

    return Pipeline(stages)
}
```

### Receive Implementation

```go
func (rt *Runtime) Receive(msg *Message) {
    behavior := Lookup(msg.Kind)
    if behavior == nil {
        logrus.Warnf("unknown message kind: %s", msg.Kind)
        return
    }

    // Build and run receive pipeline
    pipeline := rt.buildReceivePipeline(behavior)
    ctx := &PipelineContext{
        Runtime:     rt,
        Ledger:      rt.ledger,
        Transport:   rt.transport,
        Keypair:     rt.keypair,
        Personality: rt.personality,
        EventBus:    rt.eventBus,
    }

    // Receive pipeline stages return early if they reject
    pipeline.Run(msg, ctx)
}

func (rt *Runtime) buildReceivePipeline(b *Behavior) Pipeline {
    stages := []Stage{}

    // Verify stage
    if b.Verify != nil {
        stages = append(stages, b.Verify)
    } else {
        stages = append(stages, DefaultVerify())
    }

    // Dedupe stage
    if b.Dedupe != nil {
        stages = append(stages, b.Dedupe)
    } else {
        stages = append(stages, IDDedupe())
    }

    // Rate limit stage (optional)
    if b.RateLimit != nil {
        stages = append(stages, b.RateLimit)
    }

    // Filter stage
    if b.Filter != nil {
        stages = append(stages, b.Filter)
    }

    // Store stage (from emit config, for received messages)
    if b.Store != nil && !isNoStore(b.Store) {
        stages = append(stages, b.Store)
    }

    // Notify stage (always last)
    stages = append(stages, &NotifyStage{})

    return Pipeline(stages)
}
```

---

## Migration Strategy

### Phase 1: Foundation

1. Create `runtime/` package with Message, Behavior, Stage interfaces
2. Create `runtime/stages/` with all stage implementations
3. Create `runtime/catalog/` with behavior registry
4. Write comprehensive tests for pipelines

### Phase 2: Parallel Implementation

1. Implement Runtime alongside existing Network
2. Port one service at a time (start with simplest: social)
3. Keep old path working, new path opt-in
4. Verify behavior matches old implementation

### Phase 3: Services Migration

Order by complexity (simplest first):

1. **social** — simple events, good test case
2. **world** — self-contained journeys
3. **presence** — hey-there, chau, newspaper, howdy
4. **neighbourhood** — observations
5. **gossip** — reads from ledger, creates zines
6. **stash** — confidant management
7. **checkpoint** — complex multi-sig

### Phase 4: Cleanup

1. Remove old Network methods
2. Remove old event handling code
3. Consolidate tests
4. Update documentation

---

## Auto-Generated Documentation

### Schema Definition

```go
type Schema struct {
    Fields []FieldDef
}

type FieldDef struct {
    Name        string
    Type        string // "string", "int", "time", "[]string", etc.
    Required    bool
    Description string
}

// Example
var HeyThereSchema = &Schema{
    Fields: []FieldDef{
        {Name: "PublicKey", Type: "[]byte", Required: true, Description: "Ed25519 public key"},
        {Name: "MeshIP", Type: "string", Required: true, Description: "Tailscale mesh IP address"},
        {Name: "ID", Type: "string", Required: true, Description: "Nara ID"},
        {Name: "Version", Type: "int", Required: false, Description: "Protocol version"},
    },
}
```

### Documentation Generator

```go
func GenerateMarkdown() string {
    var sb strings.Builder

    sb.WriteString("# Message Catalog\n\n")
    sb.WriteString("Auto-generated from behavior registry.\n\n")

    // Group by category
    categories := map[string][]*Behavior{
        "Ephemeral":    {},
        "Presence":     {},
        "Observation":  {},
        "Social":       {},
        "Checkpoint":   {},
        "Protocol":     {},
    }

    for _, b := range Behaviors {
        cat := categorize(b.Kind)
        categories[cat] = append(categories[cat], b)
    }

    for cat, behaviors := range categories {
        sb.WriteString(fmt.Sprintf("## %s\n\n", cat))

        for _, b := range behaviors {
            sb.WriteString(fmt.Sprintf("### `%s`\n\n", b.Kind))
            sb.WriteString(fmt.Sprintf("%s\n\n", b.Description))

            // Behavior table
            sb.WriteString("| Aspect | Configuration |\n")
            sb.WriteString("|--------|---------------|\n")
            sb.WriteString(fmt.Sprintf("| Storage | %s |\n", describeStore(b.Store)))
            sb.WriteString(fmt.Sprintf("| Transport | %s |\n", describeTransport(b.Transport)))
            sb.WriteString(fmt.Sprintf("| Verify | %s |\n", describeVerify(b.Verify)))
            sb.WriteString(fmt.Sprintf("| Filter | %s |\n", describeFilter(b.Filter)))
            sb.WriteString("\n")

            // Schema table
            if b.Schema != nil {
                sb.WriteString("**Payload:**\n\n")
                sb.WriteString("| Field | Type | Required | Description |\n")
                sb.WriteString("|-------|------|----------|-------------|\n")
                for _, f := range b.Schema.Fields {
                    req := ""
                    if f.Required {
                        req = "✓"
                    }
                    sb.WriteString(fmt.Sprintf("| %s | `%s` | %s | %s |\n",
                        f.Name, f.Type, req, f.Description))
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
# Makefile
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
| **Behavior** | Declares how a message kind is handled |
| **Pipeline** | Chains stages like HTTP middleware |
| **Stages** | Pluggable units: ID, Sign, Store, Transport, Verify, Filter |
| **Registry** | Central catalog of all behaviors |
| **Runtime** | The OS that runs pipelines and manages services |
| **Services** | Programs that emit and handle messages |

### Key Benefits

1. **Everything is a Message** — unified model for events, ephemerals, protocols
2. **Behavior is visible** — all handling declared in one place
3. **Middleware pattern** — easy to reason about, easy to extend
4. **Customizable** — swap any stage for the 20% that need it
5. **Auto-documented** — registry generates docs
6. **Testable** — each stage testable in isolation

### The 80/20 Split

- **80%**: Use default stages, just define Kind + Description + Schema
- **20%**: Customize specific stages (ID, Sign, Verify, Filter, etc.)
