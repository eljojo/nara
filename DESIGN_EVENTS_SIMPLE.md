# Design: Simple Event Kinds

No rules engine. Just a lookup table.

---

## The Model

Everything is an **Event**. Events have a **Kind**. Each Kind has properties.

```go
// Every message flowing through the system is an Event
type Event struct {
    Kind      string    // "hey-there", "observation:restart", "howdy"
    From      string
    Payload   any
    Timestamp time.Time
    Signature []byte    // nil for unsigned ephemerals
}

// EventKind defines behavior for a kind of event
type EventKind struct {
    // Storage
    Stored      bool  // Put in ledger?
    GCPriority  int   // 0 = never prune, higher = prune sooner

    // Transport
    MQTT        bool   // Broadcast to plaza?
    MQTTTopic   string // If MQTT, which topic?
    Gossip      bool   // Include in zines?
    DirectFirst bool   // Try mesh HTTP before broadcast?

    // Filtering
    Importance  int    // 1=casual, 2=normal, 3=critical

    // Optional limits
    MaxPerKey   int    // 0 = unlimited
    KeyFunc     func(*Event) string  // nil = no grouping
}
```

---

## The Registry

Just a map. No matchers. No rules.

```go
var EventKinds = map[string]*EventKind{
    // === EPHEMERALS (not stored, not gossipped) ===

    "howdy": {
        Stored:     false,
        MQTT:       true,
        MQTTTopic:  "nara/plaza/howdy",
        Gossip:     false,
        Importance: 3,
    },

    "stash-refresh": {
        Stored:     false,
        MQTT:       true,
        MQTTTopic:  "nara/plaza/stash_refresh",
        Gossip:     false,
        Importance: 3,
    },

    // === PRESENCE ===

    "hey-there": {
        Stored:     true,
        GCPriority: 1,
        MQTT:       true,
        MQTTTopic:  "nara/plaza/hey_there",
        Gossip:     true,
        Importance: 3,
    },

    "chau": {
        Stored:     true,
        GCPriority: 1,
        MQTT:       true,
        MQTTTopic:  "nara/plaza/chau",
        Gossip:     true,
        Importance: 3,
    },

    // === OBSERVATIONS ===

    "observation:restart": {
        Stored:     true,
        GCPriority: 0,  // Never prune
        MQTT:       false,
        Gossip:     true,
        Importance: 3,
        MaxPerKey:  20,
        KeyFunc:    observerSubjectKey,
    },

    "observation:first-seen": {
        Stored:     true,
        GCPriority: 0,  // Never prune
        MQTT:       false,
        Gossip:     true,
        Importance: 3,
        MaxPerKey:  20,
        KeyFunc:    observerSubjectKey,
    },

    "observation:status-change": {
        Stored:     true,
        GCPriority: 1,
        MQTT:       false,
        Gossip:     true,
        Importance: 2,
        MaxPerKey:  20,
        KeyFunc:    observerSubjectKey,
    },

    // === SOCIAL ===

    "social": {
        Stored:      true,
        GCPriority:  2,
        MQTT:        true,
        MQTTTopic:   "nara/plaza/social",
        Gossip:      true,
        DirectFirst: true,
        Importance:  1,
    },

    // === PING ===

    "ping": {
        Stored:     true,
        GCPriority: 4,  // Prune first
        MQTT:       false,
        Gossip:     true,
        Importance: 1,
        MaxPerKey:  5,
        KeyFunc:    sourceTargetKey,
    },

    // === CHECKPOINT ===

    "checkpoint": {
        Stored:     true,
        GCPriority: 0,  // Never prune
        MQTT:       true,
        MQTTTopic:  "nara/checkpoint/final",
        Gossip:     true,
        Importance: 3,
    },
}

// Key functions
func observerSubjectKey(e *Event) string {
    p := e.Payload.(*ObservationPayload)
    return e.From + ":" + p.Subject
}

func sourceTargetKey(e *Event) string {
    p := e.Payload.(*PingPayload)
    return e.From + ":" + p.Target
}
```

---

## How It Works

### Emitting an Event

```go
// Service creates an event
event := &Event{
    Kind:    "observation:restart",
    From:    s.me.Name,
    Payload: &ObservationPayload{Subject: "bob", Type: "restart"},
}
s.deps.Emit(event)
```

### Core Handles It

```go
func (c *Coordinator) Emit(event *Event) {
    kind := EventKinds[event.Kind]
    if kind == nil {
        log.Warn("unknown event kind:", event.Kind)
        return
    }

    // Sign if it will be stored or broadcast
    if kind.Stored || kind.MQTT {
        event.Sign(c.keypair)
    }

    // Store?
    if kind.Stored {
        c.ledger.Add(event, kind)
    }

    // Broadcast?
    if kind.MQTT {
        c.mqtt.Publish(kind.MQTTTopic, event.Marshal())
    }

    // Local subscribers always notified
    c.eventBus.Notify(event)
}
```

### Ledger Uses Kind Properties

```go
func (l *Ledger) Add(event *Event, kind *EventKind) {
    // Check MaxPerKey limit
    if kind.MaxPerKey > 0 && kind.KeyFunc != nil {
        key := kind.KeyFunc(event)
        l.enforceLimit(key, kind.MaxPerKey)
    }

    l.events = append(l.events, &storedEvent{
        Event:    event,
        Priority: kind.GCPriority,
    })
}

func (l *Ledger) GC() {
    // Sort by priority (higher = less important = prune first)
    // Then by age within priority
    // Stop when hitting priority 0
}
```

### Gossip Filters by Kind

```go
func (g *GossipService) createZine() *Zine {
    var events []*Event
    for _, e := range g.deps.Ledger().Recent(5 * time.Minute) {
        kind := EventKinds[e.Kind]
        if kind != nil && kind.Gossip {
            events = append(events, e)
        }
    }
    return &Zine{Events: events}
}
```

---

## How Event.Kind Gets Set

For simple events, the kind IS the service:

```go
// hey-there event
event := &Event{Kind: "hey-there", ...}

// social event
event := &Event{Kind: "social", ...}
```

For events with subtypes, the kind includes the subtype:

```go
// observation:restart
event := &Event{
    Kind:    "observation:restart",
    Payload: &ObservationPayload{Type: "restart", ...},
}

// observation:status-change
event := &Event{
    Kind:    "observation:status-change",
    Payload: &ObservationPayload{Type: "status-change", ...},
}
```

The service that creates the event knows what kind it is. No inference needed.

---

## Ephemerals Are Just Events

No special path. Ephemerals are events where `Stored: false` and `Gossip: false`.

```go
"howdy": {
    Stored:    false,  // ← This makes it ephemeral
    Gossip:    false,  // ← Not spread via zines
    MQTT:      true,   // ← Just broadcast and forget
    MQTTTopic: "nara/plaza/howdy",
}
```

Same `Emit()` call. Same event structure. Different behavior based on kind properties.

```go
// Presence service emits howdy like any other event
s.deps.Emit(&Event{Kind: "howdy", Payload: &HowdyPayload{...}})
```

---

## What About Protocols?

Checkpoint proposals, sync requests, etc. are **not events**. They're transport-layer exchanges.

Keep them in transport. Don't model them as events.

```go
// Checkpoint service uses transport directly for protocol
s.deps.Transport().Publish("nara/checkpoint/propose", proposal)
s.deps.Transport().Subscribe("nara/checkpoint/vote", s.handleVote)

// But final checkpoints ARE events
s.deps.Emit(&Event{Kind: "checkpoint", Payload: finalCheckpoint})
```

This is fine. Not everything needs to be an event. Protocols are plumbing.

---

## Summary

| Concept | Implementation |
|---------|---------------|
| Event | Simple struct with Kind field |
| Kind | String like "hey-there" or "observation:restart" |
| Behavior | Lookup in flat map, no rules engine |
| Ephemerals | Events with `Stored: false, Gossip: false` |
| Protocols | Not events, stay in transport layer |

**No matchers. No rules. Just a map.**

```go
kind := EventKinds[event.Kind]
if kind.Stored { ... }
if kind.MQTT { ... }
if kind.Gossip { ... }
```

---

## Visual

```
┌──────────────────────────────────────────────────────┐
│                     SERVICE                           │
│                                                       │
│   event := &Event{Kind: "observation:restart", ...}  │
│   s.deps.Emit(event)                                 │
└───────────────────────┬──────────────────────────────┘
                        │
                        ▼
┌──────────────────────────────────────────────────────┐
│                      CORE                             │
│                                                       │
│   kind := EventKinds["observation:restart"]          │
│                                                       │
│   if kind.Stored  → ledger.Add(event, kind)          │
│   if kind.MQTT    → mqtt.Publish(kind.MQTTTopic)     │
│   eventBus.Notify(event)                             │
└──────────────────────────────────────────────────────┘
                        │
            ┌───────────┼───────────┐
            ▼           ▼           ▼
         Ledger       MQTT       EventBus
         (maybe)     (maybe)     (always)
```

That's it. Simple lookup, conditional paths.
