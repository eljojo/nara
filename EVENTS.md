# Event-Sourced Nara

Naras are deterministic machines driven by event streams. Every observation that shapes a nara's opinion is recorded as an event, shared with peers, and can be replayed.

## Philosophy

Instead of storing computed state, naras store the **events** that led to that state. This enables:

- **Rewinding time**: Reconstruct any past state by replaying events
- **Determinism**: Same events = same opinion (modulo personality)
- **Auditability**: See exactly what shaped a nara's view of the world
- **Resilience**: Recover state from peers after a crash

## Event Types

### Network State Observations (`service: "observation"`)

Critical events for distributed consensus on network state. These events replace newspaper broadcasts for tracking restarts and online status.

| Type | Description | Importance | Purpose |
|------|-------------|------------|---------|
| `restart` | Detected a nara restarted | Critical (3) | Consensus on StartTime, restart count |
| `first-seen` | First time observing a nara | Critical (3) | Seeds StartTime consensus |
| `status-change` | Online/Offline transition | Normal (2) | Real-time network awareness |

**Importance Levels:**
- **Critical (3):** Never filtered by personality, essential for consensus
- **Normal (2):** Can be filtered by very chill naras (>85)
- **Casual (1):** Filtered based on personality preferences

**Anti-Abuse:**
- Max 20 observation events per observer→subject pair
- Max 10 events about same subject per 5-minute window
- Restart content deduplication (same restart reported by multiple observers = 1 event)
- Global pruning respects importance (critical events survive longest)

**Backfill Events:**
Events with `is_backfill: true` represent historical knowledge being converted to events during migration from newspapers. They participate in consensus like any other observation event.

### Legacy Observation Events (`service: "social"`)

Events recorded when a nara directly observes something (social service):

| Reason | Description | Clout Impact |
|--------|-------------|--------------|
| `online` | Observed a nara come online | +0.1 (reliable) |
| `offline` | Observed a nara go offline or disappear | -0.05 (less available) |
| `journey-pass` | A world journey passed through us | +0.2 (participating) |
| `journey-complete` | Heard a journey completed successfully | +0.5 (success) |
| `journey-timeout` | A journey we saw never completed | -0.3 (unreliable) |

### Social Events

Events from social interactions (teasing):

| Reason | Description |
|--------|-------------|
| `high-restarts` | Nara has restarted too many times |
| `comeback` | Nara returned after being missing |
| `trend-abandon` | Nara abandoned a popular trend |
| `random` | Random social jab |
| `nice-number` | Appreciating aesthetically pleasing numbers |

## Event Structure

### SyncEvent (Universal Container)

```go
type SyncEvent struct {
    ID        string // SHA256 hash for deduplication
    Timestamp int64  // Unix nanoseconds
    Service   string // "social", "ping", "observation", or "checkpoint"
    Emitter   string // Who created this (optional, for signing)
    Signature string // Ed25519 signature (optional)

    // Payloads - exactly one is set based on Service
    Social      *SocialEventPayload      // For service="social"
    Ping        *PingObservation          // For service="ping"
    Observation *ObservationEventPayload  // For service="observation"
    Checkpoint  *CheckpointEventPayload   // For service="checkpoint"
}
```

### ObservationEventPayload (Network State)

```go
type ObservationEventPayload struct {
    Observer    string // Who made the observation
    Subject     string // Who is being observed
    Type        string // "restart", "first-seen", "status-change"
    Importance  int    // 1=casual, 2=normal, 3=critical
    IsBackfill  bool   // True if grandfathering existing data

    // Data specific to observation type
    StartTime   int64  // For restart/first-seen
    RestartNum  int64  // Restart counter
    OnlineState string // "ONLINE", "OFFLINE", "MISSING"
    ClusterName string // Current cluster
}
```

### SocialEventPayload (Legacy)

```go
type SocialEventPayload struct {
    Type    string // "observation", "tease", "observed", "gossip"
    Actor   string // Who recorded/did it
    Target  string // Who it's about
    Reason  string // Why (see tables above)
    Witness string // Who reported it (for journey events: journey ID)
}
```

### CheckpointEventPayload (Historical Snapshot)

Checkpoints are multi-party attested snapshots of historical state. They anchor restart counts and uptime from before proper event tracking began.

```go
type CheckpointEventPayload struct {
    Subject     string   // Who this checkpoint is about
    AsOfTime    int64    // Unix timestamp (seconds) when snapshot was taken
    FirstSeen   int64    // When network first saw this nara
    Restarts    int64    // Historical restart count at checkpoint time
    TotalUptime int64    // Total verified online seconds at checkpoint time
    Importance  int      // Always Critical (3) - never pruned

    // Multi-party attestation
    Attesters   []string // Nara names who attest to this data
    Signatures  []string // Base64 Ed25519 signatures from attesters
}
```

**Key properties:**
- **Never pruned**: Checkpoints are critical events that survive all pruning
- **Multi-signed**: Requires multiple high-uptime naras to attest
- **Historical anchor**: Allows deriving restart count as `checkpoint.Restarts + count(new restarts)`

## Personality Filtering

Not all naras keep all events. Personality affects what's meaningful:

**Importance-Based Filtering (Observation Events):**
- **Critical importance (3)**: NEVER filtered - essential for consensus (restarts, first-seen, backfill)
- **Normal importance (2)**: May be filtered by very chill naras (>85) - status changes
- **Casual importance (1)**: Filtered based on personality - routine social events

**Legacy Social Event Filtering:**
- **Very chill (>85)**: Skip routine online/offline events
- **Low sociability (<30)**: Skip journey-pass/complete events
- **Everyone**: Keeps journey-timeout (reliability matters)

## Architecture: MQTT + Mesh

Lightweight discovery over broadcast, heavy transfer over direct connection.

### MQTT (Lightweight Coordination)

- `nara/plaza/hey_there` - Nara joined the network
- `nara/plaza/chau` - Nara leaving the network
- `nara/plaza/journey_complete` - Journey finished (signal only)
- `nara/plaza/social` - Teasing events

### Mesh HTTP (High-Bandwidth Transfer)

Event streams are synced directly over the Tailscale mesh:

```
POST /events/sync
{
    "from": "requester-name",
    "subjects": ["nara-a", "nara-b"],
    "since_time": 1234567890,
    "slice_index": 0,
    "slice_total": 5
}
```

### Interleaved Slicing

When multiple naras respond during boot sync, each returns a different slice:

- Responder 0: events 0, 5, 10, 15...
- Responder 1: events 1, 6, 11, 16...
- Responder 2: events 2, 7, 12, 17...

This provides time-spread coverage without everyone returning the same events.

## Boot Recovery Flow

```
Nara starts
    │
    ▼
MQTT connects, discovers neighbors (30s)
    │
    ▼
For each neighbor (up to 5):
    POST /events/sync to their mesh IP
    Pass slice_index for interleaved coverage
    │
    ▼
Merge received events into local ledger
    │
    ▼
DeriveClout() from merged events
    │
    ▼
Ready to participate
```

## Clout Derivation

Events affect the **target's** clout score:

```go
func applyObservationClout(clout map[string]float64, event SocialEvent, weight float64) {
    switch event.Reason {
    case "online":          clout[event.Target] += weight * 0.1
    case "offline":         clout[event.Target] -= weight * 0.05
    case "journey-pass":    clout[event.Target] += weight * 0.2
    case "journey-complete": clout[event.Target] += weight * 0.5
    case "journey-timeout": clout[event.Target] -= weight * 0.3
    }
}
```

Weights are further modified by:
- **Event age**: Older events decay exponentially
- **Personality**: Chill naras forget faster, social naras remember longer
- **Resonance**: Some events "click" with certain souls (deterministic hash)

## Journey Lifecycle Events

When a world journey passes through:

1. **Pass through**: Record `journey-pass` event, track as pending
2. **Completion heard**: Record `journey-complete`, remove from pending
3. **Timeout (5min)**: Record `journey-timeout`, remove from pending

```
Journey passes through
    │
    ├─► Record "journey-pass" event
    │
    ├─► Track in pendingJourneys
    │
    ▼
Wait for completion signal...
    │
    ├─► If completion received:
    │       Record "journey-complete"
    │       Good vibe (+0.5 clout)
    │
    └─► If 5 minutes pass:
            Record "journey-timeout"
            Bad vibe (-0.3 clout)
```

## Checkpoint Events (Historical Snapshots)

Checkpoint events capture historical state with multi-party attestation:

1. High-uptime naras agree on historical data (restart count, uptime, first-seen time)
2. They sign a checkpoint event containing this snapshot
3. Deriving current state: `checkpoint.value + count(events after checkpoint)`

**Feature flags:**
- `USE_CHECKPOINTS=true`: Enable reading checkpoint events
- `USE_CHECKPOINT_CREATION=true`: Participate in creating checkpoints
- `MIN_CHECKPOINT_ATTESTERS`: Minimum signatures required (default: 2)

**Migration path:**
```
Phase 1: Backfill events spread historical knowledge
    ↓
Phase 2: High-uptime naras create checkpoints from backfill data
    ↓
Phase 3: Checkpoints become the permanent historical anchor
    ↓
Future: Restart count = checkpoint.Restarts + unique StartTimes after checkpoint
```

This allows bounded storage while preserving accurate historical data.
