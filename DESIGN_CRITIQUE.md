# Design Critique: Nara Runtime (Revised)

A critical examination of the proposed architecture in `DESIGN_NARA_RUNTIME.md`. What are the remaining risks? What might we discover when implementing stash?

---

## What We Got Right

Before the critique, acknowledging what's solid:

1. **StageResult monads** — explicit Continue/Drop/Fail, no silent failures
2. **ID vs ContentKey split** — clean separation of envelope vs semantic identity
3. **Versioning built-in** — can evolve schemas without breaking old naras
4. **GossipQueue decoupled** — gossip and storage are independent
5. **Emit/Receive split** — clear which stages run on send vs receive
6. **Pattern templates** — `MeshRequest()`, `Ephemeral()` reduce boilerplate dramatically
7. **Stash as first target** — no backwards compatibility baggage

---

## Remaining Concerns

### 1. PipelineContext Is Still Large

```go
type PipelineContext struct {
    Runtime     *Runtime
    Ledger      *Ledger
    Transport   *Transport
    GossipQueue *GossipQueue
    Keypair     NaraKeypair
    Personality *Personality
    EventBus    *EventBus
}
```

**Concern:** Every stage gets access to everything. A stage that only needs to sign a message can also poke at the ledger.

**Why we kept it:** Narrow interfaces per stage would mean many interface types. The complexity tradeoff wasn't worth it.

**Risk for stash:** Low. Stash stages are simple (no store, no gossip). But if stages start reaching into context for "convenience" things, we'll regret it.

**Mitigation:** Code review discipline. Stages should only touch what they need.

---

### 2. No Request/Response Correlation ✅ SOLVED

Stash is request/response:
1. Alice sends `stash:store` to Bob
2. Bob processes, sends `stash:ack` back (or error)
3. Alice sends `stash:request` to Bob
4. Bob sends `stash:response` back

**Concern:** The runtime doesn't help with correlation. No request IDs, no timeouts, no "wait for response" primitive.

**Solution:** Opt-in **Correlator** utility (see `DESIGN_SERVICE_UTILITIES.md`).

Services that need request/response can import the `Correlator[Resp]` utility:
```go
type StashService struct {
    storeCorrelator *utilities.Correlator[StashStoreAck]
}

func (s *StashService) StoreWith(confidant string, data []byte) error {
    msg := &Message{Kind: "stash:store", To: confidant, ...}
    result := <-s.storeCorrelator.Send(s.rt, msg)  // Returns channel
    if result.Err != nil {
        return result.Err  // Timeout or failure
    }
    // Handle result.Value (the ack)
}
```

**Risk:** Low. Utility provides type-safe, generic request correlation with timeouts and background cleanup. Services opt-in if needed.

---

### 3. No Backpressure on GossipQueue

```go
type GossipQueue struct {
    messages []*Message
    maxAge   time.Duration
}

func (q *GossipQueue) Add(msg *Message) {
    q.messages = append(q.messages, msg)  // Unbounded!
}
```

**Concern:** If messages arrive faster than gossip can spread them, queue grows forever.

**Risk for stash:** Low. Stash messages don't use gossip queue (they're direct mesh).

**But still a problem for:** Observations, social events, anything with `Gossip: Gossip()`.

**Recommendation:** Add max size to GossipQueue. Drop oldest when full. Not blocking for stash, but fix before Phase 6.

---

### 4. Versioning Complexity for Simple Cases ✅ SOLVED

```go
type Behavior struct {
    CurrentVersion int
    MinVersion     int
    PayloadTypes   map[int]reflect.Type  // Payload type per version
    Handlers       map[int]any           // Version-specific typed handlers
}
```

**Original concern:** Two ways to specify payload type (`PayloadTypes` map vs `PayloadType` single).

**Solution:** Removed `PayloadType` shorthand. Always use `PayloadTypes` map - it's explicit about versioning from day one, and the runtime deserializes for you.

**Additional decision:** Version-specific handlers. Each version routes to its own typed handler:

```go
// Even for v1-only services, use the maps:
PayloadTypes: map[int]reflect.Type{
    1: PayloadTypeOf[StashStorePayload](),
},
Handlers: map[int]any{
    1: TypedHandler(s.handleStoreV1),  // func(*Message, *StashStorePayload)
},
```

**Benefits:**
- **Type-safe** — each handler receives correctly typed payload, no type switches
- **Migration as a pattern** — V1 handler can migrate payload and delegate to V2 handler
- **Easy deprecation** — when dropping V1 support, just delete the handler

**Risk:** None. One way to do things = no confusion.

---

### 5. Testing the Runtime Itself ✅ SOLVED

**Original concern:** How do we test the runtime without starting MQTT, mesh, ledger?

**Solution:** Two testing levels with different approaches:

**1. Runtime tests (integration)** — Start real MQTT, test the runtime itself:
```go
func TestRuntimeEmitReceive(t *testing.T) {
    broker := startTestMQTTBroker(t)
    rt := NewRuntime(RuntimeConfig{
        Transport: NewMQTTTransport(broker.URL),
    })
    // Test real message flow through the runtime
}
```

**2. Service tests (unit)** — MockRuntime, no external dependencies:
```go
func TestStashStore(t *testing.T) {
    // No MQTT, no mesh - just test business logic
    mock := NewMockRuntime("alice")
    stash := NewStashService()
    stash.Init(mock)

    // Simulate receiving a store request
    mock.Deliver(&Message{
        Kind: "stash:store",
        From: "bob",
        Payload: &StashStorePayload{Data: []byte("secret")},
    })

    // Check that stash stored it
    assert.True(t, stash.HasStashFor("bob"))

    // Check that stash emitted an ack
    assert.Equal(t, 1, len(mock.Emitted))
    assert.Equal(t, "stash:ack", mock.Emitted[0].Kind)
}
```

**MockRuntime provides:**
- `Emitted []Message` — Capture all `Emit()` calls for assertions
- `Deliver(msg)` — Simulate incoming messages to services
- Fake `Me()`, keypair, personality — No real identity needed

**Risk:** Low. Services interact through `RuntimeInterface`, which MockRuntime implements.

---

### 6. Where Do Payload Structs Live? ✅ SOLVED

**Solution:** The `messages/` package — the "monorepo" of Nara message types.

```
messages/
├── doc.go           // Package docs, overview of all message kinds
├── stash.go         // StashStorePayload, StashRequestPayload, etc.
├── social.go        // SocialPayload, TeasePayload
├── presence.go      // HeyTherePayload, ChauPayload, NewspaperPayload
├── checkpoint.go    // CheckpointProposal, CheckpointVote
├── observation.go   // RestartObservation, StatusChangeObservation
└── gossip.go        // ZinePayload, DMPayload
```

**This package is the single source of truth:**

1. **All payload structs** — Every message type defined here
2. **Version history** — Old payload versions kept for backwards compat
3. **Documentation** — Godoc comments are the spec
4. **JSON tags** — Wire format defined by struct tags
5. **Validation** — `Validate()` methods on payloads

**Example: stash.go**
```go
package messages

// StashStorePayload is sent when storing encrypted data with a confidant.
//
// Flow: Owner → Confidant
// Response: StashStoreAck
// Transport: MeshOnly (direct HTTP)
//
// Version History:
//   v1 (2024-01): Initial version
type StashStorePayload struct {
    // Owner is who the stash belongs to (for retrieval)
    Owner string `json:"owner"`

    // Nonce for XChaCha20-Poly1305 decryption
    Nonce []byte `json:"nonce"`

    // Ciphertext is the encrypted stash data
    Ciphertext []byte `json:"ciphertext"`

    // Timestamp when the stash was created
    Timestamp int64 `json:"ts"`
}

func (p *StashStorePayload) Validate() error {
    if p.Owner == "" {
        return errors.New("owner required")
    }
    if len(p.Nonce) != 24 {
        return errors.New("nonce must be 24 bytes")
    }
    return nil
}

// StashStoreAck acknowledges successful storage.
type StashStoreAck struct {
    // StoredAt is when the confidant stored the data
    StoredAt int64 `json:"stored_at"`
}
```

**Auto-generated documentation:**

The runtime can generate a message catalog from this package:
```bash
nara docs --messages
```

Outputs markdown or JSON with all message types, their fields, flows, and versions.

**Import structure (no cycles):**
```
messages/     ← Pure data types, no dependencies
    ↑
runtime/      ← Imports messages/ for deserialization
    ↑
services/     ← Import both runtime/ and messages/
```

**Risk:** None. Clean separation, single source of truth.

---

### 7. Error Strategy Defaults ✅ SOLVED

**Solution:** Environment-aware defaults, like Rails environments.

```go
// runtime/environment.go
type Environment int

const (
    EnvProduction Environment = iota  // Graceful: log errors, don't crash
    EnvDevelopment                     // Loud: log + warnings, fail on suspicious things
    EnvTest                            // Strict: panic on errors, catch bugs early
)
```

**Error strategy defaults by environment:**

| Environment | Default Strategy | Rationale |
|-------------|------------------|-----------|
| Production | `ErrorLog` | Log and continue - don't crash in prod |
| Development | `ErrorLogWarn` | Log at WARN level - make errors visible |
| Test | `ErrorPanic` | Fail fast - catch bugs in CI |

**Runtime config:**
```go
type RuntimeConfig struct {
    Environment Environment  // Defaults to EnvProduction if not set
    // ...
}

func NewRuntime(cfg RuntimeConfig) *Runtime {
    env := cfg.Environment
    if env == 0 && os.Getenv("NARA_ENV") != "" {
        env = parseEnv(os.Getenv("NARA_ENV"))  // "production", "development", "test"
    }
    // ...
}
```

**Default strategy resolution:**
```go
func (rt *Runtime) defaultErrorStrategy() ErrorStrategy {
    switch rt.env {
    case EnvTest:
        return ErrorPanic      // Fail fast in tests
    case EnvDevelopment:
        return ErrorLogWarn    // Loud warnings in dev
    default:
        return ErrorLog        // Graceful in prod
    }
}

func (rt *Runtime) getErrorStrategy(b *Behavior, direction string) ErrorStrategy {
    var explicit ErrorStrategy
    if direction == "emit" {
        explicit = b.Emit.OnError
    } else {
        explicit = b.Receive.OnError
    }

    if explicit != 0 {
        return explicit  // Behavior explicitly set it
    }
    return rt.defaultErrorStrategy()  // Environment default
}
```

**Other environment-specific behaviors:**

| Behavior | Production | Development | Test |
|----------|------------|-------------|------|
| Error strategy | Log | LogWarn | Panic |
| Logger verbosity | Batched | Verbose | Silent/Captured |
| Validation | Log invalid | Warn invalid | Reject invalid |
| Timeouts | Long (30s) | Medium (10s) | Short (1s) |
| MockRuntime | N/A | N/A | Auto-cleanup |

**Usage:**
```go
// In main.go
rt := NewRuntime(RuntimeConfig{
    Environment: EnvProduction,
})

// In tests - auto-detected or explicit
rt := NewMockRuntime(t, "alice")  // Always EnvTest

// Via environment variable
NARA_ENV=development ./nara
```

**Risk:** None. Sensible defaults that fail loudly in dev/test, gracefully in prod.

---

### 8. The "Receive" Pipeline for Direct Messages ✅ SOLVED

**Rule: ALL incoming messages go through the receive pipeline. No exceptions.**

Whether a message arrives via MQTT broadcast or direct mesh HTTP, it runs through the same pipeline. This ensures:
- Signature verification always happens
- Deduplication always happens
- Rate limiting always happens
- No "backdoors" that bypass security

**The flow for mesh messages:**

```
┌─────────────────────────────────────────────────────────────────┐
│  HTTP Request (mesh)                                             │
│  POST /mesh/message                                              │
│  {kind: "stash:store", from: "alice", ...}                      │
└─────────────────────────┬───────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│  http_mesh.go                                                    │
│  func handleMeshMessage(w, r) {                                  │
│      raw := readBody(r)                                          │
│      rt.Receive(raw)  // ← ALWAYS use runtime, never bypass     │
│  }                                                               │
└─────────────────────────┬───────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│  rt.Receive(raw)                                                 │
│                                                                  │
│  1. Deserialize (using messages/ package + version)             │
│  2. Lookup behavior for msg.Kind                                 │
│  3. Run receive pipeline:                                        │
│     [Verify] → [Dedupe] → [RateLimit] → [Filter] → [Store]      │
│  4. If Continue: notify service handlers                         │
│  5. If Drop: record metric, done                                 │
│  6. If Error: apply error strategy (env-aware)                   │
└─────────────────────────┬───────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│  service.Handle(msg)                                             │
│                                                                  │
│  Stash service receives verified, deduplicated message          │
└─────────────────────────────────────────────────────────────────┘
```

**Same flow for MQTT:**
```
MQTT message → mqtt_handler.go → rt.Receive(raw) → pipeline → service.Handle()
```

**Anti-pattern (DON'T DO THIS):**
```go
// BAD: Bypassing the pipeline
func handleMeshMessage(w, r) {
    var msg Message
    json.Unmarshal(readBody(r), &msg)
    stashService.Handle(&msg)  // ← NO! Skips verification, dedup, etc.
}
```

**Correct pattern:**
```go
// GOOD: Always go through runtime
func handleMeshMessage(w, r) {
    rt.Receive(readBody(r))  // Pipeline handles everything
}
```

**Risk:** None. Clear rule, single entry point for all messages.

---

## Summary: Risks by Severity

### High (solve before implementing stash)
- **#2 Request/Response correlation** — ✅ SOLVED with Correlator utility
- **#6 Payload struct location** — ✅ SOLVED with `messages/` package (the "monorepo")
- **#8 Mesh receive flow** — ✅ SOLVED (all messages go through `rt.Receive()`, no exceptions)

### Medium (solve during implementation)
- **#5 Testing mocks** — ✅ SOLVED (MockRuntime for services, real MQTT for runtime)
- **#7 Error strategy defaults** — ✅ SOLVED (environment-aware: Panic in test, LogWarn in dev, Log in prod)

### Low (defer to later phases)
- **#1 PipelineContext size** — discipline, not architecture
- **#3 GossipQueue backpressure** — fix before Phase 6
- **#4 Versioning complexity** — ✅ SOLVED (PayloadTypes + version-specific Handlers)

---

## Pre-Implementation Checklist for Stash

Before writing stash service code:

- [x] Create `messages/` package → **The monorepo of Nara message types**
- [x] Decide request/response pattern → **Correlator utility** (see `DESIGN_SERVICE_UTILITIES.md`)
- [ ] Create `utilities/` package with Correlator, Encryptor, RateLimiter
- [x] Document mesh receive flow → **All messages through `rt.Receive()`, no exceptions**
- [x] Write MockRuntime for service testing → **Phase 3.3**
- [x] Add error strategy defaults → **Environment-aware** (Panic/LogWarn/Log)
- [x] PayloadTypes validation → **Removed PayloadType shorthand, only PayloadTypes exists**
- [x] Version-specific handlers → **Each version gets typed handler, no type switches**

---

## What We'll Learn From Stash

Stash will teach us:

1. **Does the pipeline pattern work for request/response?** — stash is all req/resp
2. **Is MeshOnly the right abstraction?** — stash is mesh-only, fails if unreachable
3. **Does Correlator utility work well?** — first real consumer of the utility pattern
4. **Are the interfaces right?** — first real consumer of RuntimeInterface
5. **Do version-specific handlers work?** — typed handlers, no type switches
6. **Does MockRuntime enable fast iteration?** — test services without MQTT

If stash works cleanly, we have confidence for the rest. If it's painful, we learn what to fix before migrating production services.
