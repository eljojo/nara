# Design: Event Catalog

A registry of all event types with composable policies. Simple events get default handling. Complex events register their existence but handle their own logic.

---

## Core Concepts

### 1. Base Event (the proto-event)

Every event in the system has these fields. No exceptions.

```go
// BaseEvent is the minimum contract for anything flowing through the system
type BaseEvent struct {
    ID        string    // Unique identifier (computed from kind + timestamp + content hash)
    Kind      string    // "hey-there", "observation:restart", "checkpoint"
    From      string    // Who created this event
    Timestamp time.Time // When it was created
}

// Full event adds payload and signatures
type Event struct {
    BaseEvent
    Payload   any       // Kind-specific data
    Signature []byte    // Creator's signature (nil for unsigned)
}

// ComputeID generates deterministic ID from content
func (e *Event) ComputeID() string {
    h := sha256.New()
    h.Write([]byte(e.Kind))
    h.Write([]byte(e.From))
    h.Write([]byte(e.Timestamp.Format(time.RFC3339Nano)))
    h.Write(e.payloadHash())
    return base58.Encode(h.Sum(nil))[:16]
}
```

**Everything has an ID.** Even ephemerals, even complex checkpoints. The ID is the universal handle.

---

### 2. Event Catalog

The registry of all known event kinds. Every event type is registered, but not all use default handling.

```go
// EventDef defines an event kind and its policies
type EventDef struct {
    // Identity
    Kind        string       // "observation:restart"
    Description string       // "Records when a nara restarts"
    Schema      *Schema      // For validation and docs (optional)

    // Policies (composable)
    Storage     StoragePolicy
    Transport   TransportPolicy
    Filter      FilterPolicy

    // Opt-out flag
    CustomLogic bool         // If true, service handles everything after ID assignment
}

// The catalog
var Catalog = map[string]*EventDef{}

// Register adds an event definition
func Register(def *EventDef) {
    Catalog[def.Kind] = def
}

// Lookup gets definition (returns nil for unknown)
func Lookup(kind string) *EventDef {
    return Catalog[kind]
}
```

---

### 3. Storage Policy

Where and how long events are kept.

```go
type StoragePolicy struct {
    Stored   bool  // Put in ledger? (false = ephemeral)
    Priority int   // GC priority: 0 = never, higher = prune sooner
}

// Predefined storage policies
var (
    StorageNone       = StoragePolicy{Stored: false}
    StoragePermanent  = StoragePolicy{Stored: true, Priority: 0}
    StorageImportant  = StoragePolicy{Stored: true, Priority: 1}
    StorageNormal     = StoragePolicy{Stored: true, Priority: 2}
    StorageLow        = StoragePolicy{Stored: true, Priority: 3}
    StorageExpendable = StoragePolicy{Stored: true, Priority: 4}
)
```

---

### 4. Transport Policy

How events spread through the network.

```go
type TransportPolicy struct {
    MQTT        bool   // Broadcast to plaza?
    MQTTTopic   string // Which topic?
    Gossip      bool   // Include in zines?
    DirectFirst bool   // Try mesh HTTP before broadcast?
}

// Predefined transport policies
var (
    TransportNone       = TransportPolicy{}
    TransportGossipOnly = TransportPolicy{Gossip: true}
    TransportMQTTOnly   = TransportPolicy{MQTT: true}
    TransportBroadcast  = TransportPolicy{MQTT: true, Gossip: true}
    TransportDMFirst    = TransportPolicy{MQTT: true, Gossip: true, DirectFirst: true}
)

// WithTopic returns a copy with the MQTT topic set
func (t TransportPolicy) WithTopic(topic string) TransportPolicy {
    t.MQTTTopic = topic
    return t
}
```

---

### 5. Filter Policy

Who sees the event. Critical events are never filtered. Casual events use a service-provided filter.

```go
type FilterPolicy struct {
    Importance   int  // 1=casual, 2=normal, 3=critical

    // For casual events, the service provides the filter function
    // nil means "always include"
    CasualFilter func(event *Event, personality *Personality) bool
}

// Predefined filter policies
var (
    // Critical: never filtered, always propagated
    FilterCritical = FilterPolicy{Importance: 3}

    // Normal: filtered only by very chill naras (>85 chill)
    FilterNormal = FilterPolicy{Importance: 2}

    // Casual: service-specific filtering
    FilterCasual = func(filter func(*Event, *Personality) bool) FilterPolicy {
        return FilterPolicy{Importance: 1, CasualFilter: filter}
    }
)
```

**Services define their own casual filters:**

```go
// Social service defines how to filter social events
var socialFilter = func(event *Event, p *Personality) bool {
    payload := event.Payload.(*SocialPayload)

    switch payload.Reason {
    case ReasonHighRestarts:
        return p.Sociability > 30
    case ReasonComeback:
        return p.Sociability > 20
    case ReasonNiceNumber:
        return p.Chill < 80  // Chill naras don't care about funny numbers
    default:
        return true
    }
}

// Register with the filter
func init() {
    Register(&EventDef{
        Kind:      "social",
        Filter:    FilterCasual(socialFilter),
        // ...
    })
}
```

---

### 6. Schema (for docs and validation)

Optional schema for documentation generation and payload validation.

```go
type Schema struct {
    Fields []FieldDef
}

type FieldDef struct {
    Name        string
    Type        string  // "string", "int", "time", "[]string", etc.
    Required    bool
    Description string
}

// Example
var ObservationRestartSchema = &Schema{
    Fields: []FieldDef{
        {Name: "Subject", Type: "string", Required: true, Description: "The nara that restarted"},
        {Name: "Observer", Type: "string", Required: true, Description: "Who observed the restart"},
        {Name: "RestartNumber", Type: "int", Required: true, Description: "Which restart this is"},
        {Name: "StartTime", Type: "time", Required: true, Description: "When the nara came back online"},
        {Name: "DowntimeSecs", Type: "int", Required: false, Description: "How long it was down"},
    },
}
```

---

## The Full Catalog

```go
func init() {
    // === EPHEMERALS ===

    Register(&EventDef{
        Kind:        "howdy",
        Description: "Discovery poll - who's out there?",
        Storage:     StorageNone,
        Transport:   TransportMQTTOnly.WithTopic("nara/plaza/howdy"),
        Filter:      FilterCritical,
    })

    Register(&EventDef{
        Kind:        "stash-refresh",
        Description: "Request stash recovery from confidants",
        Storage:     StorageNone,
        Transport:   TransportMQTTOnly.WithTopic("nara/plaza/stash_refresh"),
        Filter:      FilterCritical,
    })

    // === PRESENCE ===

    Register(&EventDef{
        Kind:        "hey-there",
        Description: "Identity announcement with public key and mesh IP",
        Schema:      HeyThereSchema,
        Storage:     StorageImportant,
        Transport:   TransportBroadcast.WithTopic("nara/plaza/hey_there"),
        Filter:      FilterCritical,
    })

    Register(&EventDef{
        Kind:        "chau",
        Description: "Graceful shutdown announcement",
        Schema:      ChauSchema,
        Storage:     StorageImportant,
        Transport:   TransportBroadcast.WithTopic("nara/plaza/chau"),
        Filter:      FilterCritical,
    })

    // === OBSERVATIONS ===

    Register(&EventDef{
        Kind:        "observation:restart",
        Description: "Records when a nara restarts",
        Schema:      ObservationRestartSchema,
        Storage:     StoragePermanent,  // Never prune
        Transport:   TransportGossipOnly,
        Filter:      FilterCritical,
        CustomLogic: true,  // Service handles dedup by content
    })

    Register(&EventDef{
        Kind:        "observation:first-seen",
        Description: "Records first time a nara is observed",
        Schema:      ObservationFirstSeenSchema,
        Storage:     StoragePermanent,
        Transport:   TransportGossipOnly,
        Filter:      FilterCritical,
        CustomLogic: true,
    })

    Register(&EventDef{
        Kind:        "observation:status-change",
        Description: "Records online/offline transitions",
        Schema:      ObservationStatusChangeSchema,
        Storage:     StorageImportant,
        Transport:   TransportGossipOnly,
        Filter:      FilterNormal,
        CustomLogic: true,
    })

    // === SOCIAL ===

    Register(&EventDef{
        Kind:        "social",
        Description: "Social interactions like teases",
        Schema:      SocialSchema,
        Storage:     StorageNormal,
        Transport:   TransportDMFirst.WithTopic("nara/plaza/social"),
        Filter:      FilterCasual(socialFilter),
    })

    // === PING ===

    Register(&EventDef{
        Kind:        "ping",
        Description: "Latency measurement between naras",
        Schema:      PingSchema,
        Storage:     StorageExpendable,
        Transport:   TransportGossipOnly,
        Filter:      FilterCasual(nil),  // Always include pings
        CustomLogic: true,  // Service handles max-5-per-target
    })

    // === SEEN ===

    Register(&EventDef{
        Kind:        "seen",
        Description: "Lightweight observation that a nara was seen",
        Schema:      SeenSchema,
        Storage:     StorageLow,
        Transport:   TransportGossipOnly,
        Filter:      FilterCasual(nil),
    })

    // === CHECKPOINT ===

    Register(&EventDef{
        Kind:        "checkpoint",
        Description: "Multi-party signed consensus anchor",
        Schema:      CheckpointSchema,
        Storage:     StoragePermanent,
        Transport:   TransportBroadcast.WithTopic("nara/checkpoint/final"),
        Filter:      FilterCritical,
        CustomLogic: true,  // Service handles multi-sig, voting, etc.
    })
}
```

---

## How Emit Works

```go
func (c *Coordinator) Emit(event *Event) {
    // 1. Always compute ID
    if event.ID == "" {
        event.ID = event.ComputeID()
    }

    // 2. Look up definition
    def := Lookup(event.Kind)
    if def == nil {
        log.Warn("unknown event kind:", event.Kind)
        return
    }

    // 3. If custom logic, just notify and return
    //    (Service already handled storage/transport)
    if def.CustomLogic {
        c.eventBus.Notify(event)
        return
    }

    // 4. Apply default policies

    // Sign if needed
    if def.Storage.Stored || def.Transport.MQTT {
        event.Sign(c.keypair)
    }

    // Filter by importance
    if !c.passesFilter(event, def.Filter) {
        return
    }

    // Store
    if def.Storage.Stored {
        c.ledger.Add(event, def.Storage.Priority)
    }

    // Transport
    if def.Transport.DirectFirst && c.trySendDirect(event) {
        // Sent via DM, done
    } else if def.Transport.MQTT {
        c.mqtt.Publish(def.Transport.MQTTTopic, event.Marshal())
    }
    // Gossip picks up from ledger automatically

    // 5. Always notify local subscribers
    c.eventBus.Notify(event)
}

func (c *Coordinator) passesFilter(event *Event, filter FilterPolicy) bool {
    switch filter.Importance {
    case 3: // Critical
        return true
    case 2: // Normal
        return c.personality.Chill <= 85
    case 1: // Casual
        if filter.CasualFilter == nil {
            return true
        }
        return filter.CasualFilter(event, c.personality)
    }
    return true
}
```

---

## How Services Handle CustomLogic

For events with `CustomLogic: true`, the service handles storage and transport, then calls `Emit` just for the event bus notification.

```go
// Checkpoint service handles its own complexity
func (s *CheckpointService) finalizeCheckpoint(cp *CheckpointPayload) {
    event := &Event{
        Kind:      "checkpoint",
        From:      s.me.Name,
        Timestamp: time.Now(),
        Payload:   cp,
    }

    // Service handles multi-sig (not in generic Event.Signature)
    // cp.VoterIDs and cp.Signatures are already populated

    // Service handles storage with its own rules
    s.deps.Ledger().AddCheckpoint(event)

    // Service handles transport
    s.deps.Transport().Publish("nara/checkpoint/final", event.Marshal())

    // Then emit for event bus (CustomLogic skips default handling)
    s.deps.Emit(event)
}

// Observation service handles content-aware dedup
func (s *NeighbourhoodService) recordRestart(subject string, restartNum int, startTime time.Time) {
    event := &Event{
        Kind: "observation:restart",
        From: s.me.Name,
        Timestamp: time.Now(),
        Payload: &ObservationPayload{
            Subject:       subject,
            Type:          "restart",
            RestartNumber: restartNum,
            StartTime:     startTime,
        },
    }

    // Service handles content-aware dedup
    if s.isDuplicateRestart(event) {
        return
    }

    // Service handles rate limiting
    if !s.rateLimiter.Allow(subject) {
        return
    }

    // Service handles storage with compaction
    s.deps.Ledger().AddObservation(event)

    // Then emit (gossip will pick up from ledger)
    s.deps.Emit(event)
}
```

---

## Auto-Generated Documentation

The catalog can generate docs automatically:

```go
// GenerateDocs outputs markdown for all registered events
func GenerateDocs() string {
    var sb strings.Builder

    sb.WriteString("# Event Catalog\n\n")

    // Group by category (infer from kind prefix)
    categories := groupByCategory(Catalog)

    for _, cat := range categories {
        sb.WriteString(fmt.Sprintf("## %s\n\n", cat.Name))

        for _, def := range cat.Events {
            sb.WriteString(fmt.Sprintf("### `%s`\n\n", def.Kind))
            sb.WriteString(fmt.Sprintf("%s\n\n", def.Description))

            // Policies table
            sb.WriteString("| Policy | Value |\n")
            sb.WriteString("|--------|-------|\n")
            sb.WriteString(fmt.Sprintf("| Storage | %s |\n", describeStorage(def.Storage)))
            sb.WriteString(fmt.Sprintf("| Transport | %s |\n", describeTransport(def.Transport)))
            sb.WriteString(fmt.Sprintf("| Filter | %s |\n", describeFilter(def.Filter)))

            // Schema if available
            if def.Schema != nil {
                sb.WriteString("\n**Payload:**\n\n")
                sb.WriteString("| Field | Type | Required | Description |\n")
                sb.WriteString("|-------|------|----------|-------------|\n")
                for _, f := range def.Schema.Fields {
                    req := ""
                    if f.Required { req = "✓" }
                    sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n",
                        f.Name, f.Type, req, f.Description))
                }
            }

            sb.WriteString("\n")
        }
    }

    return sb.String()
}
```

**Output example:**

```markdown
# Event Catalog

## Presence

### `hey-there`

Identity announcement with public key and mesh IP

| Policy | Value |
|--------|-------|
| Storage | Stored, priority 1 (important) |
| Transport | MQTT (`nara/plaza/hey_there`) + Gossip |
| Filter | Critical (never filtered) |

**Payload:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| PublicKey | string | ✓ | Ed25519 public key |
| MeshIP | string | ✓ | Tailscale mesh IP address |
| ID | string | ✓ | Nara ID |

### `chau`

Graceful shutdown announcement
...
```

---

## CLI Command for Docs

```go
// cmd/nara/docs.go
func docsCommand() {
    md := catalog.GenerateDocs()

    // Write to docs site
    os.WriteFile("docs/src/content/docs/events-reference.md", []byte(md), 0644)

    fmt.Println("Generated docs/src/content/docs/events-reference.md")
}
```

Run with: `nara docs` or integrate into `make build-web`.

---

## Summary

| Concept | Implementation |
|---------|---------------|
| Base event | `ID`, `Kind`, `From`, `Timestamp` — always present |
| Catalog | Map of all event kinds with policies |
| Simple events | Use default `Emit()` handling |
| Complex events | `CustomLogic: true` — service handles, emit just notifies |
| Storage policy | `Stored`, `Priority` — composable presets |
| Transport policy | `MQTT`, `Gossip`, `DirectFirst` — composable presets |
| Filter policy | `Importance` + optional `CasualFilter` function |
| Schema | Optional, for validation and docs |
| Docs | Auto-generated from catalog |

**The 80/20 split:**
- 80%: Simple events use default policies, just emit and go
- 20%: Complex events register their kind + schema, handle their own logic

**Names:**
- `EventKind` → `EventDef` (definition)
- `EventKinds` → `Catalog`
- The system → "Event Catalog"
