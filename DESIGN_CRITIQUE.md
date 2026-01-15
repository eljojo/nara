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

### 4. Versioning Complexity for Simple Cases

```go
type Behavior struct {
    CurrentVersion int
    MinVersion     int
    PayloadTypes   map[int]reflect.Type
    PayloadType    reflect.Type  // Shorthand
}
```

**Concern:** Two ways to specify payload type. Which takes precedence? What if both are set?

**Rules (implicit):**
- If `PayloadTypes` is set, use version lookup
- Otherwise use `PayloadType`
- If neither... panic? Default to empty?

**Risk for stash:** Low. Stash is new, starts at v1, uses simple `PayloadType`.

**Recommendation:** Document precedence clearly. Add validation in `Register()`:
```go
if b.PayloadTypes != nil && b.PayloadType != nil {
    return errors.New("set PayloadTypes OR PayloadType, not both")
}
```

---

### 5. Testing the Runtime Itself

**Concern:** How do we test the runtime without starting MQTT, mesh, ledger?

**Current plan:** Interfaces everywhere, mock implementations.

```go
type TransportInterface interface {
    PublishMQTT(topic string, data []byte) error
    TrySendDirect(target string, msg *Message) error
}

// In tests
transport := &MockTransport{}
rt := NewRuntime(RuntimeConfig{Transport: transport})
```

**Risk for stash:** Medium. We need good mocks before we can test stash service.

**Order of implementation:**
1. Core types (Message, StageResult, Behavior)
2. Interfaces
3. Mock implementations
4. Runtime with mocks
5. Stash service with mock runtime

**Recommendation:** Write mocks as part of Phase 3, not Phase 4. Can't test anything without them.

---

### 6. Where Do Payload Structs Live?

**Current:** Payload types are defined... somewhere. Behaviors reference them.

```go
PayloadType: runtime.PayloadTypeOf[StashStorePayload]()
```

**But where is `StashStorePayload` defined?**

Options:
1. In `runtime/` package — runtime knows all payload types
2. In service packages — `stash/payloads.go`
3. In a shared `types/` package

**Concern:** If payloads are in service packages, runtime can't deserialize without importing services. Circular dependency risk.

**Risk for stash:** High. Need to solve this before writing any code.

**Recommendation:** Shared `messages/` or `payloads/` package:
```
messages/
├── stash.go      // StashStorePayload, StashRequestPayload, etc.
├── social.go     // SocialPayload
├── presence.go   // HeyTherePayload, ChauPayload, etc.
└── checkpoint.go // CheckpointPayload
```

Runtime imports `messages/`. Services import `messages/`. No cycles.

---

### 7. Error Strategy Defaults

```go
type Behavior struct {
    OnTransportError ErrorStrategy
    OnStoreError     ErrorStrategy
    OnVerifyError    ErrorStrategy
}
```

**Concern:** What if not set? Zero value is `ErrorDrop` (silent).

**Risk for stash:** Medium. Forgetting to set error strategy = silent failures.

**Recommendation:** Make default explicit:
```go
func (rt *Runtime) getErrorStrategy(b *Behavior, stage string) ErrorStrategy {
    switch stage {
    case "transport":
        if b.OnTransportError != 0 {
            return b.OnTransportError
        }
        return ErrorLog  // Default: at least log it
    // ...
    }
}
```

Or require all strategies in `Register()` validation.

---

### 8. The "Receive" Pipeline for Direct Messages

MQTT messages go through receive pipeline. But what about direct mesh messages?

```go
// Alice sends stash:store directly to Bob via HTTP
POST /mesh/message
{kind: "stash:store", ...}

// Does Bob run the receive pipeline?
// Or does the HTTP handler process it directly?
```

**Current design (implicit):** Mesh HTTP handler calls `rt.Receive(raw)`, which runs the pipeline.

**Risk for stash:** High. This is exactly how stash works. Need to be clear about the flow:

```
HTTP request → http_mesh.go → rt.Receive() → pipeline → service.Handle()
```

**Recommendation:** Document this flow explicitly. Add it to the plan.

---

## Summary: Risks by Severity

### High (solve before implementing stash)
- **#2 Request/Response correlation** — ✅ SOLVED with Correlator utility
- **#6 Payload struct location** — create `messages/` package
- **#8 Mesh receive flow** — document and implement

### Medium (solve during implementation)
- **#5 Testing mocks** — write mocks in Phase 3
- **#7 Error strategy defaults** — add sensible defaults

### Low (defer to later phases)
- **#1 PipelineContext size** — discipline, not architecture
- **#3 GossipQueue backpressure** — fix before Phase 6
- **#4 Versioning complexity** — add validation

---

## Pre-Implementation Checklist for Stash

Before writing stash service code:

- [ ] Create `messages/` package with payload structs
- [x] Decide request/response pattern → **Correlator utility** (see `DESIGN_SERVICE_UTILITIES.md`)
- [ ] Create `utilities/` package with Correlator, Encryptor, RateLimiter
- [ ] Document mesh receive flow (HTTP → Receive → pipeline → Handle)
- [ ] Write mock Transport and Ledger for testing
- [ ] Add error strategy defaults (ErrorLog, not ErrorDrop)
- [ ] Add PayloadType validation in Register()

---

## What We'll Learn From Stash

Stash will teach us:

1. **Does the pipeline pattern work for request/response?** — stash is all req/resp
2. **Is MeshOnly the right abstraction?** — stash is mesh-only, fails if unreachable
3. **How painful is manual message correlation?** — might need runtime help
4. **Are the interfaces right?** — first real consumer of TransportInterface
5. **Does the service lifecycle work?** — Init/Start/Stop/Handle pattern

If stash works cleanly, we have confidence for the rest. If it's painful, we learn what to fix before migrating production services.
