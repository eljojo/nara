# Design Critique: Nara Runtime

A critical examination of the proposed architecture. What's overengineered? What won't scale? What's missing?

---

## Red Flags

### 1. PipelineContext is a God Object in Disguise

```go
type PipelineContext struct {
    Runtime     *Runtime
    Ledger      *Ledger
    Transport   *Transport
    Keypair     NaraKeypair
    Personality *Personality
    EventBus    *EventBus
}
```

**Problem:** Every stage gets access to everything. This is the same problem as Network — just renamed.

**Why it matters:** Stages can reach into anything. No clear boundaries. Testing requires mocking everything.

**Alternative:** Stages declare what they need:

```go
type IDStage interface {
    ComputeID(msg *Message) string
    // No context needed
}

type StoreStage interface {
    Store(msg *Message, ledger Ledger)
    // Only gets what it needs
}
```

**Or:** Accept that stages need runtime access and keep it, but acknowledge this isn't true decoupling.

---

### 2. The `next()` Pattern is Error-Prone

```go
func (s *SomeStage) Process(msg *Message, ctx *PipelineContext, next func()) {
    // Do stuff
    next()  // Easy to forget!
}
```

**Problems:**
- Forgetting to call `next()` silently drops the message
- No way to return errors
- No way to know if downstream stages succeeded
- Hard to debug "why didn't my message arrive?"

**Alternative A:** Return-based instead of callback:

```go
type Stage interface {
    Process(msg *Message, ctx *PipelineContext) (*Message, error)
}

// Pipeline chains return values
func (p Pipeline) Run(msg *Message, ctx *PipelineContext) (*Message, error) {
    for _, stage := range p {
        var err error
        msg, err = stage.Process(msg, ctx)
        if err != nil {
            return nil, err
        }
        if msg == nil {
            return nil, nil // Dropped
        }
    }
    return msg, nil
}
```

**Alternative B:** Keep `next()` but add explicit drop/error:

```go
type Stage interface {
    Process(msg *Message, ctx *PipelineContext, next func(), drop func(reason string))
}
```

---

### 3. Two Pipelines (Emit vs Receive) Adds Complexity

We have:
- `buildEmitPipeline()` with stages: ID → Sign → Store → Transport → Notify
- `buildReceivePipeline()` with stages: Verify → Dedupe → RateLimit → Filter → Store → Notify

**Problem:**
- Some stages appear in both (Store, Notify)
- Some stages are emit-only (ID, Sign, Transport)
- Some stages are receive-only (Verify, Dedupe, RateLimit, Filter)
- The Behavior struct mixes both: confusing what's emit vs receive

**Question:** Do we need two pipelines, or one pipeline with conditional stages?

```go
type Behavior struct {
    // Instead of separate emit/receive stages:
    Stages []Stage  // All stages, some skip based on direction
}

type Stage interface {
    OnEmit() bool    // Should this run on emit?
    OnReceive() bool // Should this run on receive?
    Process(...)
}
```

**Counter-argument:** Two pipelines is clearer. You know exactly what runs when.

---

### 4. Helper Constructors Will Proliferate

Current helpers:
```go
DefaultID(), ContentID(fn)
DefaultSign(), NoSign()
DefaultStore(priority), NoStore(), DedupStore(priority, fn), MaxPerKeyStore(...)
MQTT(topic), MQTTPerNara(pattern), Gossip(), Broadcast(topic), DirectFirst(topic)
DefaultVerify(), SelfAttesting(fn), CustomVerify(fn), NoVerify()
IDDedupe(), ContentDedupe(fn)
RateLimit(window, max, keyFn)
Critical(), Normal(), Casual(fn)
```

That's **20+ helpers** already. As we add features:
- `ConditionalStore(condition, store)`?
- `RetryTransport(transport, retries)`?
- `CachingVerify(verify, ttl)`?
- `CompositeFilter(filters...)`?

**Problem:** The DSL grows unbounded. New developers need to learn the vocabulary.

**Alternative:** Fewer helpers, more explicit structs:

```go
// Instead of helpers, just use structs directly
Store: &DefaultStoreStage{Priority: 2}
Store: &DedupStoreStage{Priority: 0, IsSameAs: myFunc}

// Or use a builder pattern
Store: Store().WithPriority(2).WithDedup(myFunc)
```

---

### 5. Schema is Over-Engineering (For Now)

```go
type Schema struct {
    Fields []FieldDef
}

var HeyThereSchema = &Schema{
    Fields: []FieldDef{
        {Name: "PublicKey", Type: "[]byte", Required: true, ...},
    },
}
```

**Problems:**
- Who maintains this? It'll drift from actual structs.
- Go already has struct definitions with types.
- No validation uses it (yet).
- Docs could be generated from Go struct tags instead.

**Alternative:** Skip Schema for now. Add it later if we need validation or generated docs.

```go
type Behavior struct {
    Kind        string
    Description string
    // Schema      *Schema  // Remove for now
    // ...
}

// For docs, use reflection on actual payload types
func GenerateDocs() {
    for _, b := range Behaviors {
        payloadType := reflect.TypeOf(b.ExamplePayload)
        // Generate from actual struct fields
    }
}
```

---

### 6. What If a Stage Needs to Conditionally Skip?

Current: A stage either runs or is nil.

**But what about:**
- "Only rate limit if sender isn't a trusted peer"
- "Only verify signature if message is over 1 hour old"
- "Store with priority 0 if checkpoint exists, priority 2 otherwise"

**Current approach:** Custom stage with condition baked in.

**Problem:** Every condition needs a new stage type.

**Alternative:** Wrapper stages:

```go
type ConditionalStage struct {
    Condition func(msg *Message, ctx *PipelineContext) bool
    Stage     Stage
}

func (s *ConditionalStage) Process(msg *Message, ctx *PipelineContext, next func()) {
    if s.Condition(msg, ctx) {
        s.Stage.Process(msg, ctx, next)
    } else {
        next()
    }
}

// Usage
RateLimit: Conditional(
    func(msg *Message, ctx *PipelineContext) bool {
        return !ctx.Runtime.IsTrustedPeer(msg.From)
    },
    RateLimit(5*time.Minute, 10, subjectKey),
)
```

---

### 7. The Behavior Struct Mixes Concerns

```go
type Behavior struct {
    Kind        string
    Description string
    Schema      *Schema

    // Emit stages
    ID        IDStage
    Sign      SignStage
    Store     StoreStage     // Used by both!
    Transport TransportStage

    // Receive stages
    Verify    VerifyStage
    Dedupe    DedupeStage
    RateLimit RateLimitStage
    Filter    FilterStage
}
```

**Problem:** Store appears once but is used differently:
- Emit: Always store
- Receive: Store only if not NoStore

**Problem:** Not obvious which fields are emit vs receive.

**Alternative:** Explicit separation:

```go
type Behavior struct {
    Kind        string
    Description string

    Emit    EmitBehavior
    Receive ReceiveBehavior
}

type EmitBehavior struct {
    ID        IDStage
    Sign      SignStage
    Store     StoreStage
    Transport TransportStage
}

type ReceiveBehavior struct {
    Verify    VerifyStage
    Dedupe    DedupeStage
    RateLimit RateLimitStage
    Filter    FilterStage
    Store     StoreStage  // Can differ from emit!
}
```

**Counter-argument:** More nesting, more verbose. Maybe not worth it.

---

### 8. No Error Handling Strategy

What happens when:
- MQTT publish fails?
- Ledger is full?
- Signature verification throws an exception?
- Rate limiter has a bug?

**Current:** Stages just don't call `next()`. Silent failure.

**Problem:** No logging, no metrics, no retry, no dead letter queue.

**Alternative:** Explicit error handling:

```go
type Stage interface {
    Process(msg *Message, ctx *PipelineContext) StageResult
}

type StageResult struct {
    Continue bool
    Error    error
    Reason   string // "rate_limited", "invalid_signature", etc.
}

// Runtime logs/tracks failures
func (rt *Runtime) runPipeline(msg *Message, pipeline Pipeline) {
    for _, stage := range pipeline {
        result := stage.Process(msg, ctx)
        if !result.Continue {
            rt.metrics.RecordDrop(msg.Kind, result.Reason)
            if result.Error != nil {
                logrus.Errorf("stage failed: %v", result.Error)
            }
            return
        }
    }
}
```

---

### 9. Testing Complexity

**To test a behavior, you need:**
- A Runtime (or mock)
- A Ledger (or mock)
- A Transport (or mock)
- A Keypair
- Potentially other services

**Example:**
```go
func TestObservationRestart(t *testing.T) {
    // Setup
    ledger := NewMockLedger()
    transport := NewMockTransport()
    keypair := generateTestKeypair()
    rt := NewRuntime(
        WithLedger(ledger),
        WithTransport(transport),
        WithKeypair(keypair),
    )

    // Test
    msg := &Message{Kind: "observation:restart", ...}
    rt.Emit(msg)

    // Verify
    assert.True(t, ledger.Has(msg.ID))
    assert.False(t, transport.Published()) // Gossip only
}
```

**Problem:** Still need to set up a lot of infrastructure.

**Alternative:** Test stages in isolation:

```go
func TestContentIDStage(t *testing.T) {
    stage := ContentID(func(p any) string {
        return p.(*ObservationPayload).Subject
    })

    msg := &Message{Payload: &ObservationPayload{Subject: "bob"}}
    stage.Process(msg, nil, func() {})

    assert.NotEmpty(t, msg.ID)
    assert.Contains(t, msg.ID, "bob")
}
```

**This is good!** Stages are testable in isolation. Keep this property.

---

### 10. What Happens at Scale?

**50 message kinds:**
- Registry becomes a big init() block
- Hard to find the behavior you want
- IDE autocomplete doesn't help

**100 message kinds:**
- Need to organize by category/package
- Behaviors need to be in separate files
- Import cycles become a risk

**Dynamic behaviors:**
- What if a service wants to register behaviors at runtime?
- What if behaviors need to change based on config?

**Current:** Static registration at init time.

**Alternative:** Allow dynamic registration, but with validation:

```go
func Register(b *Behavior) error {
    if Behaviors[b.Kind] != nil {
        return fmt.Errorf("behavior %s already registered", b.Kind)
    }
    if err := b.Validate(); err != nil {
        return err
    }
    Behaviors[b.Kind] = b
    return nil
}
```

---

### 11. Gossip's Implicit Dependency on Ledger

**Current design:**
- `Transport: Gossip()` does nothing — it relies on Store putting the message in the ledger
- Gossip service reads from ledger to create zines

**Problem:** If you set `Store: NoStore()` but expect gossip to work, it won't. This coupling is implicit.

**Worse:** What if you want gossip but NOT permanent storage? "Spread this message but don't remember it"?

**Alternative:** Make gossip explicit:

```go
type Behavior struct {
    Store     StoreStage
    Transport TransportStage
    Gossip    bool  // Explicit: include in zines?
}

// Or make gossip a transport option
Transport: Gossip()                    // Gossip only, implies store
Transport: MQTT("topic").WithGossip()  // MQTT + gossip
Transport: MQTT("topic")               // MQTT only, no gossip
```

---

### 12. The "Message" Name is Generic

`Message` is used everywhere in software. It's not searchable.

**Alternatives:**
- `NaraEvent` — but we said not everything is an event
- `Packet` — too low-level
- `Signal` — interesting but vague
- `Dispatch` — could work
- Keep `Message` — it's accurate

**Recommendation:** Keep `Message` but be consistent. Never call it "event" in code.

---

## What Could Be Removed?

### 1. Schema (for now)

Remove entirely. Use Go struct tags or reflection for docs.

### 2. Dedupe Stage (maybe)

Merge into Store stage:
```go
Store: DefaultStore(2).WithIDDedupe()
Store: DedupStore(0).WithContentDedupe(fn)
```

One less stage type to understand.

### 3. Separate VerifyStage types

Instead of `DefaultVerify`, `SelfAttesting`, `CustomVerify`, `NoVerify`...

Just have one:
```go
type VerifyStage struct {
    Verify func(msg *Message, ctx *PipelineContext) bool
}

// Helpers return configured instances
func DefaultVerify() *VerifyStage {
    return &VerifyStage{Verify: defaultVerifyFunc}
}
```

Fewer types, same flexibility.

### 4. The NotifyStage

It's always last. It's never customized. Just hardcode it in the runtime:

```go
func (rt *Runtime) runPipeline(msg *Message, pipeline Pipeline) {
    // Run user-defined stages
    pipeline.Run(msg, ctx)

    // Always notify (not a stage)
    rt.eventBus.Emit(msg)
}
```

---

## What's Missing?

### 1. Observability

No hooks for:
- Metrics (messages per second, drop rate, latency)
- Tracing (follow a message through the pipeline)
- Logging (structured logs per stage)

**Need:** Observer pattern or middleware for cross-cutting concerns.

### 2. Backpressure

What if the pipeline is overwhelmed?
- Messages queue up
- Memory grows
- Eventually OOM

**Need:** Bounded channels, drop policies, circuit breakers.

### 3. Priority/QoS

All messages treated equally. But:
- Checkpoints are critical
- Pings are expendable
- During recovery, prioritize certain kinds

**Need:** Priority queues or weighted scheduling.

### 4. Versioning

What happens when message format changes?
- Old naras send v1, new naras send v2
- How to handle backwards compatibility?

**Need:** Version field in Message, version-aware stages.

---

## Summary: Keep, Change, Remove

### Keep
- **Message as universal primitive** — good unifying concept
- **Behavior registry** — centralized, visible, documented
- **Pipeline pattern** — composable, testable
- **Stage isolation** — each stage testable alone
- **Helper constructors** — readable behavior definitions

### Change
- **`next()` pattern** → Return-based with explicit errors
- **PipelineContext** → Consider narrower interfaces per stage
- **Schema** → Defer or use reflection
- **Gossip coupling** → Make explicit
- **Emit/Receive in Behavior** → Consider explicit separation

### Remove (for v1)
- **Schema** — over-engineering for now
- **NotifyStage** — hardcode in runtime
- **Some stage type proliferation** — use functions instead of types where possible

### Add
- **Error handling** — explicit errors, logging, metrics
- **Observability hooks** — for debugging and monitoring
- **Backpressure** — bounded queues, drop policies

---

## Revised Minimal Design

If we strip to essentials:

```go
// Message — the universal primitive
type Message struct {
    ID        string
    Kind      string
    From      string
    Timestamp time.Time
    Payload   any
    Signature []byte
}

// Behavior — how a message kind is handled
type Behavior struct {
    Kind        string
    Description string

    // Functions, not stage objects
    ComputeID   func(msg *Message) string                    // nil = default
    Sign        func(msg *Message, keypair Keypair) []byte   // nil = default, false = skip
    Store       func(msg *Message, ledger *Ledger) bool      // nil = default, return false to skip
    Transport   func(msg *Message, transport *Transport)     // required
    Verify      func(msg *Message, lookup KeyLookup) bool    // nil = default
    Filter      func(msg *Message, personality *Personality) bool // nil = accept all
}

// Registry
var Behaviors = map[string]*Behavior{}

// Runtime
func (rt *Runtime) Emit(msg *Message) error {
    b := Behaviors[msg.Kind]
    if b == nil {
        return fmt.Errorf("unknown kind: %s", msg.Kind)
    }

    // ID
    if b.ComputeID != nil {
        msg.ID = b.ComputeID(msg)
    } else {
        msg.ID = defaultComputeID(msg)
    }

    // Sign
    if b.Sign != nil {
        msg.Signature = b.Sign(msg, rt.keypair)
    } else {
        msg.Signature = defaultSign(msg, rt.keypair)
    }

    // Store
    shouldStore := true
    if b.Store != nil {
        shouldStore = b.Store(msg, rt.ledger)
    } else {
        rt.ledger.Add(msg, 2) // default priority
    }

    // Transport
    b.Transport(msg, rt.transport)

    // Notify
    rt.eventBus.Emit(msg)

    return nil
}
```

**~50 lines** instead of hundreds. Is this enough?

**Trade-off:** Less flexible, but simpler. Add complexity only when needed.
