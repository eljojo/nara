# Design Exploration: Topics as First-Class Concepts

The question: **How do services declare transport preferences without knowing transport details?**

The insight: Multiple behaviors cluster around "what kind of message is this":
- Transport routing (MQTT? Gossip? Both?)
- Retention/GC rules (never prune? prune first?)
- Importance level (personality filtering)
- Rate limiting
- Compaction strategy

These are all **policies** that attach to a **topic**.

---

## Current State Analysis

### What varies by event type today

| Event Type | MQTT | Gossip | GC Priority | Importance | Rate Limit | Compaction |
|------------|------|--------|-------------|------------|------------|------------|
| `checkpoint` | YES (3 topics) | YES | 0 (never) | Critical | per-round | keep all |
| `observation:restart` | NO | YES | 0 (never) | Critical | 10/5min | dedup by content |
| `observation:first-seen` | NO | YES | 0 (never) | Critical | 10/5min | dedup by content |
| `observation:status-change` | NO | YES | 1 (high) | Normal | 10/5min | oldest first |
| `hey-there` | YES | YES | 1 (high) | Critical | 5sec | oldest first |
| `chau` | YES | YES | 1 (high) | Critical | none | oldest first |
| `social` | YES | YES | 2 (medium) | Casual | cooldown | oldest first |
| `seen` | NO | YES | 3 (med-low) | Casual | none | oldest first |
| `ping` | NO | YES | 4 (low) | Casual | 5/target | keep latest N |

### Observations

1. **Transport is per-topic**, not global — observations NEVER touch MQTT
2. **GC priority is per-topic** — checkpoints never pruned, pings pruned first
3. **Importance is per-topic** — affects personality filtering
4. **Compaction strategy varies** — pings keep latest N, observations dedup by content
5. **The current code scatters this knowledge** — bits in sync_ledger.go, transport_mqtt.go, etc.

---

## Proposal: Topic Registry

Define topics as first-class objects with attached policies.

### Core Types

```go
// core/topic.go

// Topic defines a channel of information with its policies
type Topic struct {
    // Identity
    Name        string   // e.g., "observation:restart", "social", "ping"
    Service     string   // maps to SyncEvent.Service
    Subtype     string   // optional, e.g., "restart" for observations

    // Transport Policy
    Transport   TransportPolicy

    // Retention Policy
    Retention   RetentionPolicy

    // Filtering Policy
    Importance  ImportanceLevel

    // Rate Limiting (optional)
    RateLimit   *RateLimitPolicy
}

// TransportPolicy defines where messages flow
type TransportPolicy struct {
    MQTT        bool     // Broadcast to plaza?
    MQTTTopic   string   // If MQTT, what topic? (e.g., "nara/plaza/hey_there")
    Gossip      bool     // Include in zines?
    DirectHTTP  bool     // Prefer direct mesh HTTP? (like social DMs)
}

// RetentionPolicy defines GC behavior
type RetentionPolicy struct {
    Priority    int           // 0 = never prune, 4 = prune first
    MaxAge      time.Duration // Optional: auto-expire after duration
    MaxPerKey   int           // Optional: keep only N per key (like pings)
    KeyFunc     string        // How to compute the key ("target", "observer:subject", etc.)
    Compaction  CompactionStrategy
}

type CompactionStrategy int
const (
    CompactOldestFirst CompactionStrategy = iota  // Default: drop oldest
    CompactKeepLatestN                            // Keep N most recent (pings)
    CompactDedup                                  // Deduplicate by content (observations)
)

// ImportanceLevel for personality filtering
type ImportanceLevel int
const (
    ImportanceCasual   ImportanceLevel = 1  // Filtered by personality
    ImportanceNormal   ImportanceLevel = 2  // Filtered only by very chill naras
    ImportanceCritical ImportanceLevel = 3  // Never filtered
)

// RateLimitPolicy defines throttling
type RateLimitPolicy struct {
    Window    time.Duration
    MaxEvents int
    KeyFunc   string  // Rate limit per what? ("source", "source:target", etc.)
}
```

### The Registry

```go
// core/topic_registry.go

var TopicRegistry = map[string]*Topic{
    // === CHECKPOINT ===
    "checkpoint": {
        Name:      "checkpoint",
        Service:   "checkpoint",
        Transport: TransportPolicy{
            MQTT:      true,
            MQTTTopic: "nara/checkpoint/final",
            Gossip:    true,
        },
        Retention: RetentionPolicy{
            Priority:   0,  // NEVER prune
            Compaction: CompactOldestFirst,
        },
        Importance: ImportanceCritical,
    },

    // === OBSERVATIONS ===
    "observation:restart": {
        Name:      "observation:restart",
        Service:   "observation",
        Subtype:   "restart",
        Transport: TransportPolicy{
            MQTT:   false,  // Never MQTT
            Gossip: true,
        },
        Retention: RetentionPolicy{
            Priority:   0,  // NEVER prune
            MaxPerKey:  20,
            KeyFunc:    "observer:subject",
            Compaction: CompactDedup,
        },
        Importance: ImportanceCritical,
        RateLimit: &RateLimitPolicy{
            Window:    5 * time.Minute,
            MaxEvents: 10,
            KeyFunc:   "subject",
        },
    },

    "observation:status-change": {
        Name:      "observation:status-change",
        Service:   "observation",
        Subtype:   "status-change",
        Transport: TransportPolicy{
            MQTT:   false,
            Gossip: true,
        },
        Retention: RetentionPolicy{
            Priority:   1,
            MaxPerKey:  20,
            KeyFunc:    "observer:subject",
            Compaction: CompactOldestFirst,
        },
        Importance: ImportanceNormal,
        RateLimit: &RateLimitPolicy{
            Window:    5 * time.Minute,
            MaxEvents: 10,
            KeyFunc:   "subject",
        },
    },

    // === PRESENCE ===
    "hey-there": {
        Name:      "hey-there",
        Service:   "hey-there",
        Transport: TransportPolicy{
            MQTT:      true,
            MQTTTopic: "nara/plaza/hey_there",
            Gossip:    true,
        },
        Retention: RetentionPolicy{
            Priority:   1,
            Compaction: CompactOldestFirst,
        },
        Importance: ImportanceCritical,
        RateLimit: &RateLimitPolicy{
            Window:    5 * time.Second,
            MaxEvents: 1,
            KeyFunc:   "source",
        },
    },

    "chau": {
        Name:      "chau",
        Service:   "chau",
        Transport: TransportPolicy{
            MQTT:      true,
            MQTTTopic: "nara/plaza/chau",
            Gossip:    true,
        },
        Retention: RetentionPolicy{
            Priority:   1,
            Compaction: CompactOldestFirst,
        },
        Importance: ImportanceCritical,
    },

    // === SOCIAL ===
    "social": {
        Name:      "social",
        Service:   "social",
        Transport: TransportPolicy{
            MQTT:       true,
            MQTTTopic:  "nara/plaza/social",
            Gossip:     true,
            DirectHTTP: true,  // Prefer DM when possible
        },
        Retention: RetentionPolicy{
            Priority:   2,
            Compaction: CompactOldestFirst,
        },
        Importance: ImportanceCasual,
    },

    // === PING ===
    "ping": {
        Name:      "ping",
        Service:   "ping",
        Transport: TransportPolicy{
            MQTT:   false,  // Never MQTT
            Gossip: true,
        },
        Retention: RetentionPolicy{
            Priority:   4,  // Prune first
            MaxPerKey:  5,
            KeyFunc:    "source:target",
            Compaction: CompactKeepLatestN,
        },
        Importance: ImportanceCasual,
    },

    // === SEEN ===
    "seen": {
        Name:      "seen",
        Service:   "seen",
        Transport: TransportPolicy{
            MQTT:   false,
            Gossip: true,
        },
        Retention: RetentionPolicy{
            Priority:   3,
            Compaction: CompactOldestFirst,
        },
        Importance: ImportanceCasual,
    },
}

// Helper to get topic for an event
func GetTopic(event *SyncEvent) *Topic {
    // Try service:subtype first
    if topic, ok := TopicRegistry[event.Service+":"+event.Subtype()]; ok {
        return topic
    }
    // Fall back to service
    if topic, ok := TopicRegistry[event.Service]; ok {
        return topic
    }
    return nil
}
```

---

## How Services Use Topics

Services become **topic-ignorant**. They just emit events. The core handles routing.

### Service Emitting an Event

```go
// services/social/service.go

func (s *SocialService) tease(target string, reason TeaseReason) {
    event := &core.SyncEvent{
        Service:   "social",
        From:      s.deps.Me().Name,
        Payload:   &SocialPayload{Type: "tease", Target: target, Reason: reason},
        Timestamp: time.Now(),
    }

    // Service doesn't know about MQTT or gossip
    // It just emits to the event bus
    s.deps.Emit(event)
}
```

### Core Routes Based on Topic

```go
// core/coordinator.go

func (c *Coordinator) Emit(event *SyncEvent) {
    // Sign the event
    event.Sign(c.nara.Keypair)

    // Look up topic policy
    topic := GetTopic(event)
    if topic == nil {
        log.Warn("unknown topic for event:", event.Service)
        return
    }

    // Check rate limit
    if topic.RateLimit != nil && c.isRateLimited(event, topic.RateLimit) {
        return
    }

    // Check personality filter
    if !c.passesImportanceFilter(event, topic.Importance) {
        return
    }

    // Store in ledger (with retention policy)
    c.ledger.Add(event, topic.Retention)

    // Route to transports based on policy
    c.route(event, topic.Transport)

    // Notify local subscribers
    c.eventBus.Emit(event)
}

func (c *Coordinator) route(event *SyncEvent, policy TransportPolicy) {
    // Try direct HTTP first if preferred
    if policy.DirectHTTP {
        if c.trySendDirect(event) {
            return  // Success, don't broadcast
        }
    }

    // MQTT broadcast
    if policy.MQTT && c.mqttEnabled() {
        c.mqtt.Publish(policy.MQTTTopic, event.Marshal())
    }

    // Gossip will pick up from ledger automatically
    // (zine creation reads recent events)
}
```

### Ledger Uses Retention Policy

```go
// core/ledger.go

func (l *SyncLedger) Add(event *SyncEvent, retention RetentionPolicy) {
    l.mu.Lock()
    defer l.mu.Unlock()

    // Apply compaction strategy
    switch retention.Compaction {
    case CompactKeepLatestN:
        l.keepLatestN(event, retention.MaxPerKey, retention.KeyFunc)
    case CompactDedup:
        if l.isDuplicate(event, retention.KeyFunc) {
            return
        }
    }

    // Add event with priority tag for GC
    l.events = append(l.events, &storedEvent{
        Event:    event,
        Priority: retention.Priority,
    })

    // Trigger GC if needed
    if len(l.events) > l.maxEvents {
        l.gc()
    }
}

func (l *SyncLedger) gc() {
    // Sort by priority (highest first = prune first), then by age
    sort.Slice(l.events, func(i, j int) bool {
        if l.events[i].Priority != l.events[j].Priority {
            return l.events[i].Priority > l.events[j].Priority
        }
        return l.events[i].Event.Timestamp.Before(l.events[j].Event.Timestamp)
    })

    // Prune from the front (highest priority = least important)
    pruneCount := len(l.events) - l.targetEvents
    for i := 0; i < pruneCount; i++ {
        if l.events[i].Priority == 0 {
            break  // Never prune priority 0
        }
    }
    l.events = l.events[i:]
}
```

---

## Alternative: Composable Policies

Instead of one Topic struct, use composable policy objects:

```go
// More flexible, but more complex

type Topic struct {
    Name       string
    Service    string
    Policies   []Policy
}

type Policy interface {
    Apply(event *SyncEvent, ctx *EmitContext) error
}

// Individual policies
type MQTTPolicy struct {
    Topic string
}

type GossipPolicy struct {
    IncludeInZine bool
}

type RetentionPolicy struct {
    Priority    int
    MaxPerKey   int
    Compaction  CompactionStrategy
}

type RateLimitPolicy struct {
    Window    time.Duration
    MaxEvents int
}

type ImportancePolicy struct {
    Level ImportanceLevel
}

// Usage
var SocialTopic = &Topic{
    Name:    "social",
    Service: "social",
    Policies: []Policy{
        &MQTTPolicy{Topic: "nara/plaza/social"},
        &GossipPolicy{IncludeInZine: true},
        &DirectHTTPPolicy{Preferred: true},
        &RetentionPolicy{Priority: 2},
        &ImportancePolicy{Level: ImportanceCasual},
    },
}
```

**Pros:** More flexible, easier to add new policy types
**Cons:** More complex, harder to see all behavior at a glance

---

## How Gossip Works With Topics

Gossip service doesn't need special knowledge. It just asks the ledger for recent events:

```go
// services/gossip/service.go

func (s *GossipService) createZine() *Zine {
    // Get events from last 5 minutes
    events := s.deps.Ledger().GetRecent(5 * time.Minute)

    // Filter to only gossip-enabled events
    var gossipEvents []*SyncEvent
    for _, event := range events {
        topic := core.GetTopic(event)
        if topic != nil && topic.Transport.Gossip {
            gossipEvents = append(gossipEvents, event)
        }
    }

    // Create and sign zine
    return &Zine{
        From:      s.deps.Me().Name,
        Events:    gossipEvents,
        Timestamp: time.Now(),
        Signature: s.sign(gossipEvents),
    }
}
```

Or even simpler — the ledger could tag events:

```go
// Ledger marks events as gossip-eligible at storage time
func (l *SyncLedger) GetGossipEvents(since time.Duration) []*SyncEvent {
    // Only returns events where topic.Transport.Gossip == true
}
```

---

## Benefits of This Design

### 1. Services are transport-ignorant

```go
// Social service just emits
s.deps.Emit(teaseEvent)

// It doesn't know or care that:
// - Teases go to MQTT topic "nara/plaza/social"
// - Teases are included in gossip zines
// - Teases prefer direct HTTP when possible
// - Teases have priority 2 for GC
// - Teases are filtered by personality
```

### 2. Policies are centralized and visible

All behavior is in `TopicRegistry`. No hunting through 5 different files to understand "what happens when I emit a social event?"

### 3. Easy to add new event types

```go
// New feature: achievements
TopicRegistry["achievement"] = &Topic{
    Name:      "achievement",
    Service:   "achievement",
    Transport: TransportPolicy{
        MQTT:      true,
        MQTTTopic: "nara/plaza/achievement",
        Gossip:    true,
    },
    Retention: RetentionPolicy{
        Priority:   1,  // Important, keep around
        Compaction: CompactOldestFirst,
    },
    Importance: ImportanceNormal,
}
```

### 4. Easy to tune behavior

Want observations to also go over MQTT? Change one line:

```go
"observation:restart": {
    Transport: TransportPolicy{
        MQTT:      true,  // Changed from false
        MQTTTopic: "nara/plaza/observation",
        Gossip:    true,
    },
    // ...
}
```

### 5. Testable

```go
func TestSocialEventsRouteToMQTT(t *testing.T) {
    topic := core.GetTopic(&SyncEvent{Service: "social"})
    assert.True(t, topic.Transport.MQTT)
    assert.Equal(t, "nara/plaza/social", topic.Transport.MQTTTopic)
}

func TestObservationsNeverMQTT(t *testing.T) {
    topic := core.GetTopic(&SyncEvent{Service: "observation"})
    assert.False(t, topic.Transport.MQTT)
}
```

---

## Open Questions

### Q1: Where does the registry live?

**Options:**
1. **Hardcoded in core** — Simple, all behavior visible
2. **Configurable** — Topics loaded from config file
3. **Services register** — Each service declares its topics

**Recommendation:** Start hardcoded. Topics don't change at runtime.

### Q2: How do services know what topics exist?

**Options:**
1. **They don't need to** — Services just emit events with a Service field
2. **Import topic constants** — `social.Emit(topics.Social, event)`
3. **Topic is on the event** — `event.Topic = "social"`

**Recommendation:** Option 1. The Service field already exists. Topics are a core concern.

### Q3: What about MQTT topics that aren't events?

Some MQTT topics aren't events:
- `nara/ledger/{name}/request` — Sync protocol
- `nara/checkpoint/propose` — Checkpoint protocol

**Proposal:** These are **protocol topics**, not **event topics**. They're transport-layer concerns, not event-layer concerns. Keep them in transport implementations.

### Q4: How does this interact with TransportMode?

Current: Global `TransportMode` (MQTT/Gossip/Hybrid)

**Proposal:** TransportMode becomes a filter on topic routing:

```go
func (c *Coordinator) route(event *SyncEvent, policy TransportPolicy) {
    if policy.MQTT && c.transportMode != TransportGossip {
        c.mqtt.Publish(policy.MQTTTopic, event.Marshal())
    }
    // Gossip always enabled (zine reads from ledger)
}
```

If `TransportMode == TransportGossip`, MQTT is just disabled. Topics don't change.

---

## Migration Path

### Phase 1: Define topics (no behavior change)

Create `TopicRegistry` that mirrors current behavior exactly. Add tests that verify current behavior matches registry definitions.

### Phase 2: Centralize GC priority

Replace `eventPruningPriority()` function with lookup from topic registry.

### Phase 3: Centralize routing

Replace scattered MQTT publish calls with `Emit()` that routes based on topic.

### Phase 4: Centralize rate limiting

Replace scattered rate limit checks with topic-based rate limiting in `Emit()`.

---

## Summary

**Topics as first-class concepts** unifies scattered behavior into a single, visible, testable registry:

- **Transport policy**: Where does it flow?
- **Retention policy**: How long do we keep it?
- **Importance**: Does personality filter it?
- **Rate limiting**: How fast can we emit?
- **Compaction**: How do we dedupe/prune?

Services become simpler — they just emit events. Core handles all the routing and storage policy based on the topic registry.

This fits naturally with the Core + Plugins architecture: services are plugins that emit events, core routes them according to declared policies.
