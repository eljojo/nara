# Design: Nara as OS

Nara is a runtime. Services are programs that run on it. The runtime provides primitives. Services use them, and can swap out pieces with custom implementations.

---

## The Mental Model

```
┌─────────────────────────────────────────────────────────────┐
│                        SERVICES                              │
│                                                              │
│   ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐   │
│   │ presence │  │  social  │  │checkpoint│  │  stash   │   │
│   └────┬─────┘  └────┬─────┘  └────┬─────┘  └────┬─────┘   │
│        │             │             │             │          │
│        └─────────────┴──────┬──────┴─────────────┘          │
│                             │                                │
│                      emit(Message)                           │
└─────────────────────────────┼───────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                      NARA RUNTIME                            │
│                                                              │
│   ┌─────────────────────────────────────────────────────┐   │
│   │                    PIPELINE                          │   │
│   │                                                      │   │
│   │   Message → [ID] → [Sign] → [Store] → [Transport]   │   │
│   │              ↑        ↑        ↑           ↑         │   │
│   │           default  default  default    default       │   │
│   │              or       or       or         or         │   │
│   │           custom   custom   custom     custom        │   │
│   └─────────────────────────────────────────────────────┘   │
│                                                              │
│   ┌────────────┐  ┌────────────┐  ┌────────────┐            │
│   │   Ledger   │  │ Transport  │  │  Identity  │            │
│   │  (storage) │  │(mqtt/mesh) │  │ (keypair)  │            │
│   └────────────┘  └────────────┘  └────────────┘            │
└─────────────────────────────────────────────────────────────┘
```

Services emit **Messages**. The runtime runs them through a **Pipeline**. Each step has a default, or the service can provide a custom implementation.

---

## The Core Primitive: Message

Everything is a Message. The proto-event.

```go
// Message is the universal primitive - what services emit
type Message struct {
    Kind      string    // "hey-there", "observation:restart", "newspaper"
    From      string    // Who created it
    Timestamp time.Time // When
    Payload   any       // Kind-specific data

    // Computed/attached by runtime
    ID        string    // Unique identifier
    Signature []byte    // Cryptographic signature
}
```

**Everything is a Message:**
- Stored events (observations, social)
- Ephemeral broadcasts (howdy, newspaper)
- Even protocol exchanges could be Messages with different handling

The difference is **how the runtime handles them**, not what they are.

---

## The Pipeline: Middleware Pattern

Each Message flows through a pipeline of stages. Each stage has:
- A **default** behavior (most services use this)
- An optional **custom** behavior (service provides)
- Can be **skipped** entirely

```go
// Stage is one step in the pipeline
type Stage interface {
    Process(msg *Message, next func(*Message))
}

// Pipeline chains stages
type Pipeline []Stage

func (p Pipeline) Run(msg *Message) {
    // Build the chain
    var chain func(*Message)
    chain = func(m *Message) {} // End of chain

    for i := len(p) - 1; i >= 0; i-- {
        stage := p[i]
        next := chain
        chain = func(m *Message) {
            stage.Process(m, next)
        }
    }

    chain(msg)
}
```

---

## The Stages

### 1. ID Stage

Computes the message ID.

```go
// Default: hash of (kind, from, timestamp, payload)
type DefaultIDStage struct{}

func (s *DefaultIDStage) Process(msg *Message, next func(*Message)) {
    if msg.ID == "" {
        msg.ID = computeDefaultID(msg)
    }
    next(msg)
}

// Custom: for observations, ID based on content only (for dedup)
type ContentIDStage struct {
    ContentFunc func(payload any) string
}

func (s *ContentIDStage) Process(msg *Message, next func(*Message)) {
    if msg.ID == "" {
        msg.ID = hash(msg.Kind + ":" + s.ContentFunc(msg.Payload))
    }
    next(msg)
}
```

**Usage:**
```go
// Observation restarts use content-based ID
Register("observation:restart", &Behavior{
    ID: ContentID(func(p any) string {
        obs := p.(*ObservationPayload)
        return fmt.Sprintf("%s:%d:%d", obs.Subject, obs.RestartNum, obs.StartTime)
    }),
})
```

---

### 2. Sign Stage

Signs the message.

```go
// Default: single signature from creator
type DefaultSignStage struct {
    Keypair NaraKeypair
}

func (s *DefaultSignStage) Process(msg *Message, next func(*Message)) {
    msg.Signature = s.Keypair.Sign(msg.signable())
    next(msg)
}

// Custom: checkpoint multi-sig (signatures are in payload)
type NoSignStage struct{} // Skip signing, payload handles it

func (s *NoSignStage) Process(msg *Message, next func(*Message)) {
    next(msg) // No signature on Message itself
}
```

---

### 3. Store Stage

Stores the message in the ledger.

```go
// Default: store in ledger with priority
type DefaultStoreStage struct {
    Ledger   *Ledger
    Priority int
}

func (s *DefaultStoreStage) Process(msg *Message, next func(*Message)) {
    s.Ledger.Add(msg, s.Priority)
    next(msg)
}

// Custom: store with dedup by content
type DedupStoreStage struct {
    Ledger    *Ledger
    Priority  int
    DedupFunc func(msg *Message, existing *Message) bool
}

func (s *DedupStoreStage) Process(msg *Message, next func(*Message)) {
    if !s.Ledger.HasMatching(func(e *Message) bool {
        return s.DedupFunc(msg, e)
    }) {
        s.Ledger.Add(msg, s.Priority)
    }
    next(msg)
}

// Skip: ephemeral messages don't store
type NoStoreStage struct{}

func (s *NoStoreStage) Process(msg *Message, next func(*Message)) {
    next(msg) // Don't store, just continue
}
```

---

### 4. Transport Stage

Sends the message over the network.

```go
// Default: MQTT broadcast
type MQTTStage struct {
    MQTT  MQTTClient
    Topic string
}

func (s *MQTTStage) Process(msg *Message, next func(*Message)) {
    s.MQTT.Publish(s.Topic, msg.Marshal())
    next(msg)
}

// Gossip: no immediate send, gossip reads from ledger
type GossipStage struct{}

func (s *GossipStage) Process(msg *Message, next func(*Message)) {
    // Nothing to do - gossip service will pick up from ledger
    next(msg)
}

// Combined: MQTT + Gossip
type BroadcastStage struct {
    MQTT  MQTTClient
    Topic string
}

func (s *BroadcastStage) Process(msg *Message, next func(*Message)) {
    s.MQTT.Publish(s.Topic, msg.Marshal())
    // Gossip will also pick up from ledger
    next(msg)
}
```

---

### 5. Notify Stage (always last)

Notifies local subscribers.

```go
type NotifyStage struct {
    EventBus *EventBus
}

func (s *NotifyStage) Process(msg *Message, next func(*Message)) {
    s.EventBus.Emit(msg)
    next(msg)
}
```

---

## Receive Pipeline (Incoming Messages)

When messages arrive (via MQTT or gossip), they go through a **receive pipeline**:

```go
// Receive pipeline stages
type ReceivePipeline []ReceiveStage

type ReceiveStage interface {
    Accept(msg *Message) bool // Return false to reject/drop
}

// Verify signature
type VerifyStage struct {
    KeyLookup func(from string) []byte
}

func (s *VerifyStage) Accept(msg *Message) bool {
    key := s.KeyLookup(msg.From)
    return msg.VerifySignature(key)
}

// Custom verify for checkpoints (multi-sig in payload)
type CheckpointVerifyStage struct{}

func (s *CheckpointVerifyStage) Accept(msg *Message) bool {
    cp := msg.Payload.(*CheckpointPayload)
    return cp.VerifyAllSignatures() // Custom logic
}

// Filter by personality
type PersonalityFilterStage struct {
    Personality *Personality
    FilterFunc  func(msg *Message, p *Personality) bool
}

func (s *PersonalityFilterStage) Accept(msg *Message) bool {
    if s.FilterFunc == nil {
        return true
    }
    return s.FilterFunc(msg, s.Personality)
}

// Rate limit
type RateLimitStage struct {
    Limiter *RateLimiter
    KeyFunc func(msg *Message) string
}

func (s *RateLimitStage) Accept(msg *Message) bool {
    key := s.KeyFunc(msg)
    return s.Limiter.Allow(key)
}
```

---

## Behavior: Composing Pipelines

Each message kind has a **Behavior** that defines its pipelines:

```go
type Behavior struct {
    Kind        string
    Description string
    Schema      *Schema  // For docs

    // Emit pipeline stages (custom or nil for default)
    ID        IDStage        // How to compute ID
    Sign      SignStage      // How to sign
    Store     StoreStage     // How/whether to store
    Transport TransportStage // How to send

    // Receive pipeline stages
    Verify    VerifyStage    // How to verify incoming
    Filter    FilterStage    // Whether to accept
    RateLimit RateLimitStage // Throttling
}
```

**Helper constructors for common patterns:**

```go
// Defaults
func DefaultID() IDStage { return &DefaultIDStage{} }
func DefaultSign() SignStage { return &DefaultSignStage{} }
func DefaultStore(priority int) StoreStage { return &DefaultStoreStage{Priority: priority} }

// Custom
func ContentID(f func(any) string) IDStage { return &ContentIDStage{ContentFunc: f} }
func NoSign() SignStage { return &NoSignStage{} }
func NoStore() StoreStage { return &NoStoreStage{} }
func DedupStore(priority int, f func(*Message, *Message) bool) StoreStage { ... }

// Transport
func MQTT(topic string) TransportStage { return &MQTTStage{Topic: topic} }
func Gossip() TransportStage { return &GossipStage{} }
func Broadcast(topic string) TransportStage { return &BroadcastStage{Topic: topic} }

// Receive
func DefaultVerify() VerifyStage { return &VerifyStage{} }
func CustomVerify(f func(*Message) bool) VerifyStage { ... }
func PersonalityFilter(f func(*Message, *Personality) bool) FilterStage { ... }
func RateLimit(window time.Duration, max int, keyFunc func(*Message) string) RateLimitStage { ... }
```

---

## The Registry

```go
var Behaviors = map[string]*Behavior{}

func Register(b *Behavior) {
    Behaviors[b.Kind] = b
}

func init() {
    // === EPHEMERAL ===

    Register(&Behavior{
        Kind:        "howdy",
        Description: "Discovery poll",
        Store:       NoStore(),
        Transport:   MQTT("nara/plaza/howdy"),
    })

    Register(&Behavior{
        Kind:        "newspaper",
        Description: "Presence heartbeat",
        Store:       NoStore(),  // For now - could change later!
        Transport:   MQTTPerNara("nara/newspaper/%s"),
    })

    // === OBSERVATIONS ===

    Register(&Behavior{
        Kind:        "observation:restart",
        Description: "Records when a nara restarts",
        Schema:      ObservationRestartSchema,
        ID:          ContentID(restartContentID),  // Custom: dedup by content
        Store:       DedupStore(0, restartDedup),  // Priority 0, custom dedup
        Transport:   Gossip(),
        Filter:      nil,  // Critical - never filtered
        RateLimit:   RateLimit(5*time.Minute, 10, subjectKey),
    })

    // === CHECKPOINT ===

    Register(&Behavior{
        Kind:        "checkpoint",
        Description: "Multi-party consensus anchor",
        Schema:      CheckpointSchema,
        Sign:        NoSign(),  // Signatures in payload
        Store:       DefaultStore(0),
        Transport:   Broadcast("nara/checkpoint/final"),
        Verify:      CustomVerify(checkpointVerify),  // Custom multi-sig verify
    })

    // === SOCIAL ===

    Register(&Behavior{
        Kind:        "social",
        Description: "Social interactions",
        Schema:      SocialSchema,
        Store:       DefaultStore(2),
        Transport:   Broadcast("nara/plaza/social"),
        Filter:      PersonalityFilter(socialFilter),  // Custom filter
    })
}
```

---

## Emit Flow

```go
func (r *Runtime) Emit(msg *Message) {
    behavior := Behaviors[msg.Kind]
    if behavior == nil {
        log.Warn("unknown message kind:", msg.Kind)
        return
    }

    // Build emit pipeline from behavior
    pipeline := r.buildEmitPipeline(behavior)

    // Run it
    pipeline.Run(msg)
}

func (r *Runtime) buildEmitPipeline(b *Behavior) Pipeline {
    stages := []Stage{}

    // ID (default or custom)
    if b.ID != nil {
        stages = append(stages, b.ID)
    } else {
        stages = append(stages, DefaultID())
    }

    // Sign (default or custom or skip)
    if b.Sign != nil {
        stages = append(stages, b.Sign)
    } else {
        stages = append(stages, DefaultSign())
    }

    // Store (default or custom or skip)
    if b.Store != nil {
        stages = append(stages, b.Store)
    } else {
        stages = append(stages, DefaultStore(2)) // Default priority
    }

    // Transport (required)
    stages = append(stages, b.Transport)

    // Always notify
    stages = append(stages, &NotifyStage{EventBus: r.eventBus})

    return Pipeline(stages)
}
```

---

## Receive Flow

```go
func (r *Runtime) Receive(msg *Message) {
    behavior := Behaviors[msg.Kind]
    if behavior == nil {
        log.Warn("unknown message kind:", msg.Kind)
        return
    }

    // Build receive pipeline
    pipeline := r.buildReceivePipeline(behavior)

    // Run each stage - any false rejects the message
    for _, stage := range pipeline {
        if !stage.Accept(msg) {
            return // Rejected
        }
    }

    // Passed all stages - store and notify
    if behavior.Store != nil {
        behavior.Store.Process(msg, func(*Message) {})
    }
    r.eventBus.Emit(msg)
}
```

---

## What About Protocols?

Checkpoint proposals, sync requests, etc. — these are **request/response patterns**, not fire-and-forget messages.

**Option A: Protocols are just Messages with special handling**

```go
Register(&Behavior{
    Kind:        "checkpoint:propose",
    Description: "Checkpoint proposal (protocol)",
    Store:       NoStore(),  // Don't persist proposals
    Transport:   MQTT("nara/checkpoint/propose"),
    // Service handles response logic
})

Register(&Behavior{
    Kind:        "sync:request",
    Description: "Ledger sync request (protocol)",
    Store:       NoStore(),
    Transport:   MQTTPerNara("nara/ledger/%s/request"),
})
```

**Option B: Protocols are a separate primitive**

```go
type Protocol struct {
    Name    string
    Request  *Behavior  // Request message behavior
    Response *Behavior  // Response message behavior
}

var Protocols = map[string]*Protocol{
    "sync": {
        Name: "sync",
        Request:  &Behavior{Kind: "sync:request", ...},
        Response: &Behavior{Kind: "sync:response", ...},
    },
}
```

I lean toward **Option A** — protocols are just Messages with `Store: NoStore()`. The request/response correlation is handled by the service, not the runtime.

---

## Summary

**Nara as OS:**
- Services emit **Messages** (the universal primitive)
- Runtime runs them through **Pipelines**
- Each pipeline stage has defaults, customizable per-message-kind
- Behaviors define which stages to use

**Pipeline stages:**
| Stage | Default | Customizable |
|-------|---------|--------------|
| ID | hash(kind, from, time, payload) | ContentID for dedup |
| Sign | Single signature | NoSign, multi-sig in payload |
| Store | Add to ledger | NoStore (ephemeral), DedupStore |
| Transport | MQTT/Gossip/Both | Per-nara topics |
| Verify | Single sig verify | Custom (checkpoint multi-sig) |
| Filter | Accept all | PersonalityFilter |
| RateLimit | None | Per-key throttling |

**Everything is a Message:**
- Stored events, ephemeral broadcasts, protocol exchanges
- The difference is behavior, not type

**Middleware pattern:**
- Stages chain like HTTP middleware
- Each stage can pass, transform, or reject
- Easy to reason about, easy to extend
