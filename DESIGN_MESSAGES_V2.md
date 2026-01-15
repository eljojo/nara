# Design Exploration: Messages, Events, and Policies (v2)

The previous "Topics" design tried to fit everything into one model. But Nara has fundamentally different kinds of things flowing through it:

1. **Ephemeral broadcasts** — howdy, stash_refresh (fire-and-forget, no storage)
2. **Stored events** — observations, social, hey-there (ledger + gossip)
3. **Protocol exchanges** — sync requests, checkpoint voting (request/response)
4. **Attribute-driven variants** — observation:restart vs observation:status-change

Forcing all of these into "Topic" is the minimum common denominator problem.

---

## What Actually Exists Today

### Category 1: Ephemeral Broadcasts

Messages that go out and are forgotten. No ledger. No gossip.

| Message | Transport | Purpose |
|---------|-----------|---------|
| `howdy` | MQTT only | "Who's there?" discovery polling |
| `stash_refresh` | MQTT only | "I need my stash back" recovery trigger |

**Characteristics:**
- No persistence
- Broadcast only (no direct delivery)
- No replay, no gossip
- Simple fire-and-forget

### Category 2: Stored Events

The core of nara — events that persist, spread, and form the collective memory.

| Event | MQTT | Gossip | Ledger | Notes |
|-------|------|--------|--------|-------|
| `hey-there` | ✓ | ✓ | ✓ | Identity announcement |
| `chau` | ✓ | ✓ | ✓ | Goodbye |
| `social` | ✓ | ✓ | ✓ | Teases, etc. |
| `observation` | ✗ | ✓ | ✓ | Never MQTT |
| `checkpoint` | ✓ | ✓ | ✓ | Finalized checkpoints |
| `ping` | ✗ | ✓ | ✓ | Latency measurements |
| `seen` | ✗ | ✓ | ✓ | "I saw X" lightweight obs |

**Characteristics:**
- Stored in SyncLedger
- Spread via gossip zines
- May also broadcast via MQTT
- Have retention policies
- Have importance levels
- May have rate limits

### Category 3: Protocol Exchanges

Request/response patterns. Not events. Not stored. Just coordination.

| Protocol | Transport | Purpose |
|----------|-----------|---------|
| Ledger sync | MQTT topics + mesh HTTP | "Give me events since X" |
| Checkpoint propose | MQTT | "I propose this checkpoint" |
| Checkpoint vote | MQTT | "Here's my vote" |
| Stash exchange | Mesh HTTP | "Store this / Give me that" |
| Attestation | Mesh HTTP | "Prove your identity" |
| Zine exchange | Mesh HTTP | "Here's my zine, give me yours" |

**Characteristics:**
- Request/response pattern
- Not stored in ledger
- Not spread via gossip
- Often peer-to-peer (mesh HTTP)
- Sometimes broadcast (checkpoint proposals)

### Category 4: Attribute-Driven Variants

This is where "Topic" breaks down. Observations aren't one topic — they're a family with attribute-based policy variation:

```
observation
├── restart      → Priority 0 (never prune), Critical importance
├── first-seen   → Priority 0 (never prune), Critical importance
└── status-change → Priority 1 (high), Normal importance
```

The **event type** is `observation`. The **subtype** is `restart/first-seen/status-change`. The **policies** vary by subtype.

Same pattern could apply elsewhere:
```
social
├── tease        → Maybe different rate limit?
├── trend        → Maybe different importance?
└── achievement  → Maybe different retention?
```

---

## The Real Dimensions

Instead of "Topic", let's identify the actual orthogonal dimensions:

### Dimension 1: Lifecycle Kind

```go
type LifecycleKind int
const (
    KindEphemeral  // Fire-and-forget, no storage (howdy)
    KindStored     // Persisted in ledger, gossipped (observations)
    KindProtocol   // Request/response exchange (sync requests)
)
```

### Dimension 2: Transport Preferences

```go
type TransportPrefs struct {
    Broadcast    bool   // MQTT plaza broadcast?
    BroadcastTo  string // Which topic?
    Gossip       bool   // Include in zines?
    DirectFirst  bool   // Try mesh HTTP before broadcast?
}
```

### Dimension 3: Retention Policy (for stored events only)

```go
type RetentionPolicy struct {
    Priority    int              // GC priority (0 = never)
    MaxPerKey   int              // Limit per grouping key
    KeyFunc     func(*Event) string  // How to compute key
    Compaction  CompactionStrategy
}
```

### Dimension 4: Filtering Policy

```go
type FilteringPolicy struct {
    Importance ImportanceLevel  // Personality filtering
    RateLimit  *RateLimit       // Throttling
}
```

---

## Proposal: Layered Message System

Instead of one "Topic" abstraction, have **message kinds** with **policy rules**.

### Layer 1: Message Kinds

```go
// The fundamental kinds of things that flow through the system

// Ephemeral — broadcast and forget
type Ephemeral struct {
    Kind    string  // "howdy", "stash_refresh"
    Payload any
}

// Event — stored, gossipped, forms collective memory
type Event struct {
    Service   string    // "observation", "social", "hey-there"
    Payload   any       // Service-specific payload
    From      string
    Timestamp time.Time
    Signature []byte
}

// Protocol messages stay in transport layer (not first-class here)
```

### Layer 2: Policy Rules

Policies are **rules that match on attributes** and **define behavior**.

```go
// A rule matches events and defines their policies
type PolicyRule struct {
    // Matching
    Match EventMatcher

    // Policies to apply
    Transport  *TransportPrefs
    Retention  *RetentionPolicy
    Filtering  *FilteringPolicy
}

// Matchers can be combined
type EventMatcher interface {
    Matches(event *Event) bool
}

// Concrete matchers
type ServiceMatcher string           // Matches event.Service
type PayloadMatcher func(any) bool   // Matches on payload attributes
type AndMatcher []EventMatcher       // All must match
```

### Layer 3: Policy Registry

```go
var PolicyRules = []PolicyRule{
    // === OBSERVATIONS ===
    {
        Match: And(
            Service("observation"),
            PayloadField("Type", In("restart", "first-seen")),
        ),
        Retention: &RetentionPolicy{
            Priority:   0,  // Never prune
            Compaction: CompactDedup,
        },
        Filtering: &FilteringPolicy{
            Importance: ImportanceCritical,
        },
        Transport: &TransportPrefs{
            Broadcast: false,  // Never MQTT
            Gossip:    true,
        },
    },
    {
        Match: And(
            Service("observation"),
            PayloadField("Type", Eq("status-change")),
        ),
        Retention: &RetentionPolicy{
            Priority:   1,
            Compaction: CompactOldest,
        },
        Filtering: &FilteringPolicy{
            Importance: ImportanceNormal,
        },
        Transport: &TransportPrefs{
            Broadcast: false,
            Gossip:    true,
        },
    },

    // === SOCIAL ===
    {
        Match: Service("social"),
        Retention: &RetentionPolicy{
            Priority:   2,
            Compaction: CompactOldest,
        },
        Filtering: &FilteringPolicy{
            Importance: ImportanceCasual,
        },
        Transport: &TransportPrefs{
            Broadcast:   true,
            BroadcastTo: "nara/plaza/social",
            Gossip:      true,
            DirectFirst: true,  // Try DM before broadcast
        },
    },

    // === PING ===
    {
        Match: Service("ping"),
        Retention: &RetentionPolicy{
            Priority:   4,  // Prune first
            MaxPerKey:  5,
            KeyFunc:    KeySourceTarget,
            Compaction: CompactKeepLatestN,
        },
        Transport: &TransportPrefs{
            Broadcast: false,
            Gossip:    true,
        },
    },

    // === HEY-THERE ===
    {
        Match: Service("hey-there"),
        Retention: &RetentionPolicy{
            Priority:   1,
            Compaction: CompactOldest,
        },
        Filtering: &FilteringPolicy{
            Importance: ImportanceCritical,
            RateLimit:  &RateLimit{Window: 5 * time.Second, Max: 1},
        },
        Transport: &TransportPrefs{
            Broadcast:   true,
            BroadcastTo: "nara/plaza/hey_there",
            Gossip:      true,
        },
    },

    // === CHECKPOINT ===
    {
        Match: Service("checkpoint"),
        Retention: &RetentionPolicy{
            Priority:   0,  // Never prune
            Compaction: CompactOldest,
        },
        Filtering: &FilteringPolicy{
            Importance: ImportanceCritical,
        },
        Transport: &TransportPrefs{
            Broadcast:   true,
            BroadcastTo: "nara/checkpoint/final",
            Gossip:      true,
        },
    },
}
```

### How Matching Works

```go
// Helper constructors for readable rules
func Service(s string) EventMatcher {
    return ServiceMatcher(s)
}

func PayloadField(field string, check func(any) bool) EventMatcher {
    return PayloadMatcher(func(p any) bool {
        // Reflect to get field value, apply check
        v := reflect.ValueOf(p).FieldByName(field)
        return check(v.Interface())
    })
}

func In(values ...string) func(any) bool {
    return func(v any) bool {
        s, ok := v.(string)
        if !ok { return false }
        for _, val := range values {
            if s == val { return true }
        }
        return false
    }
}

func And(matchers ...EventMatcher) EventMatcher {
    return AndMatcher(matchers)
}

// Find policies for an event
func GetPolicies(event *Event) *PolicyRule {
    for _, rule := range PolicyRules {
        if rule.Match.Matches(event) {
            return &rule
        }
    }
    return nil  // No matching rule
}
```

---

## How Ephemerals Fit In

Ephemerals are separate — they don't go through the event system at all.

```go
// Ephemeral broadcasts are a different path entirely
type EphemeralKind struct {
    Name      string
    MQTTTopic string
}

var Ephemerals = map[string]EphemeralKind{
    "howdy": {
        Name:      "howdy",
        MQTTTopic: "nara/plaza/howdy",
    },
    "stash_refresh": {
        Name:      "stash_refresh",
        MQTTTopic: "nara/plaza/stash_refresh",
    },
}

// Services use a different method for ephemerals
func (s *PresenceService) pollHowdy() {
    s.deps.Broadcast("howdy", &HowdyPayload{...})
}

// Core handles it simply
func (c *Coordinator) Broadcast(kind string, payload any) {
    eph, ok := Ephemerals[kind]
    if !ok {
        log.Warn("unknown ephemeral kind:", kind)
        return
    }
    c.mqtt.Publish(eph.MQTTTopic, marshal(payload))
    // That's it. No storage. No gossip. Fire and forget.
}
```

---

## How Protocols Fit In

Protocols stay in the transport/service layer. They're not first-class in core.

```go
// In checkpoint service
func (s *CheckpointService) proposeCheckpoint(cp *Checkpoint) {
    // This is protocol, not event emission
    s.deps.Transport().Publish("nara/checkpoint/propose", marshal(cp))
}

func (s *CheckpointService) handleVote(vote *Vote) {
    // Protocol handling, not event processing
    s.votes[vote.Round] = append(s.votes[vote.Round], vote)
}

// The FINAL checkpoint becomes an event
func (s *CheckpointService) finalizeCheckpoint(cp *Checkpoint) {
    event := &Event{
        Service: "checkpoint",
        Payload: cp,
    }
    s.deps.Emit(event)  // Now it goes through the event system
}
```

---

## Visual Summary

```
┌─────────────────────────────────────────────────────────────┐
│                      SERVICES                                │
│                                                              │
│  ┌─────────────────┐  ┌─────────────────┐  ┌──────────────┐ │
│  │ Emit(event)     │  │ Broadcast(eph)  │  │ Protocol     │ │
│  │ (stored events) │  │ (ephemerals)    │  │ (req/resp)   │ │
│  └────────┬────────┘  └────────┬────────┘  └──────┬───────┘ │
└───────────┼─────────────────────┼─────────────────┼─────────┘
            │                     │                 │
            ▼                     ▼                 ▼
┌───────────────────────┐  ┌──────────────┐  ┌──────────────┐
│       CORE            │  │   SIMPLE     │  │  TRANSPORT   │
│                       │  │   BROADCAST  │  │   LAYER      │
│  PolicyRules          │  │              │  │              │
│  ├─ Match event       │  │  MQTT only   │  │  MQTT/HTTP   │
│  ├─ Apply retention   │  │  No storage  │  │  Req/resp    │
│  ├─ Apply filtering   │  │  No gossip   │  │  Not events  │
│  ├─ Store in ledger   │  │              │  │              │
│  └─ Route to transport│  │              │  │              │
└───────────┬───────────┘  └──────────────┘  └──────────────┘
            │
            ▼
┌─────────────────────────────────────────────────────────────┐
│                     LEDGER + TRANSPORTS                      │
│                                                              │
│  Ledger stores events with retention policy                  │
│  MQTT broadcasts if policy says so                           │
│  Gossip reads from ledger for zines                          │
└─────────────────────────────────────────────────────────────┘
```

---

## Benefits Over "Topic" Model

### 1. No minimum common denominator

Ephemerals, events, and protocols are **different things** with **different paths**. We don't pretend they're all the same.

### 2. Attribute-based policy matching

observation:restart vs observation:status-change isn't two topics — it's one event type with attribute-based policy rules. The rule matcher handles this naturally:

```go
{
    Match: And(
        Service("observation"),
        PayloadField("Type", Eq("restart")),
    ),
    // ... policies for restart
}
```

### 3. Composable and extensible

Add a new policy dimension? Add it to PolicyRule. Add a new matcher? Implement EventMatcher. No structural changes.

### 4. Clear separation of concerns

- **Services** emit events or broadcast ephemerals
- **Core** matches policies and routes
- **Transports** deliver

Services don't know about policies. Core doesn't know about payload details (beyond matching). Transports don't know about retention.

### 5. Testable at each layer

```go
// Test matching
func TestRestartMatchesNeverPrune(t *testing.T) {
    event := &Event{
        Service: "observation",
        Payload: &ObservationPayload{Type: "restart"},
    }
    rule := GetPolicies(event)
    assert.Equal(t, 0, rule.Retention.Priority)
}

// Test ephemeral path
func TestHowdyIsEphemeral(t *testing.T) {
    _, ok := Ephemerals["howdy"]
    assert.True(t, ok)
}

// Test protocol is separate
func TestCheckpointProposeIsNotAnEvent(t *testing.T) {
    // Proposals go through transport, not Emit()
    // Final checkpoints go through Emit()
}
```

---

## Open Questions

### Q1: Is PayloadField too magical?

Using reflection to match on payload fields is flexible but opaque.

**Alternative:** Type-specific matchers

```go
type ObservationMatcher struct {
    Types []string  // "restart", "first-seen", etc.
}

func (m ObservationMatcher) Matches(event *Event) bool {
    if event.Service != "observation" { return false }
    obs, ok := event.Payload.(*ObservationPayload)
    if !ok { return false }
    return slices.Contains(m.Types, obs.Type)
}
```

More code, but more type-safe.

### Q2: Rule ordering matters?

First matching rule wins. Is that okay?

**Alternative:** Rules have priority, or rules compose (merge policies from multiple matches).

**Recommendation:** First match is simpler. Order rules from most-specific to least-specific.

### Q3: Where do default policies come from?

What if no rule matches?

**Options:**
1. Error — unknown event type
2. Default policy — log warning, apply safe defaults
3. Require exhaustive rules

**Recommendation:** Default policy with warning. Don't crash on new event types.

### Q4: How does this affect migration?

**Good news:** This is an internal reorganization. External behavior doesn't change.

**Migration path:**
1. Define PolicyRules that match current behavior exactly
2. Add tests that verify current behavior
3. Refactor code to use GetPolicies() instead of scattered logic
4. Clean up old code

---

## Summary

**Three paths, not one:**

| Path | For | Storage | Gossip | Example |
|------|-----|---------|--------|---------|
| `Emit(event)` | Stored events | Ledger | Yes | observations, social |
| `Broadcast(ephemeral)` | Fire-and-forget | None | No | howdy |
| Transport protocol | Req/response | None | No | sync requests |

**Policy rules match on attributes:**

```go
Match: And(Service("observation"), PayloadField("Type", Eq("restart")))
```

Not "topic per subtype", but "rules that match attributes".

**Clean separation:**
- Services emit/broadcast without knowing policies
- Core matches and routes based on rules
- Transports deliver

This handles the heterogeneity without forcing everything into one model.
