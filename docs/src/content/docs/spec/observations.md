# Observations

## Purpose

Observations record uptime, restarts, and online state changes as events so that
consensus and projections can be derived deterministically.

## Conceptual Model

- Observation events are `SyncEvent`s with service `observation`.
- The derived state is stored in `NaraObservation` records per subject.

### NaraObservation Data Structure
Pure state data about a nara.
```go
type NaraObservation struct {
    // The Trinity (Checkpointable State)
    Restarts    int64 // Total restart count
    TotalUptime int64 // Total verified online seconds
    StartTime   int64 // Unix timestamp (seconds) when first observed

    // Current State
    Online      string // "ONLINE", "OFFLINE", "MISSING"
    LastSeen    int64  // Unix timestamp (seconds)
    LastRestart int64  // Unix timestamp (seconds)

    // Cluster Info
    ClusterName  string
    ClusterEmoji string

    // Local-Only Metrics (Not Synced)
    LastPingRTT  float64
    AvgPingRTT   float64
    LastPingTime int64
}
```

Key invariants:
- Observation event timestamps inside payloads are in **seconds**.
- `SyncEvent` timestamps are in **nanoseconds**.
- Restart events can be deduplicated by content (subject + restart_num + start_time).

## External Behavior

- Observations are emitted for:
  - **Restarts** (when a node detects a peer's start time changed).
  - **First Seen** (when a node encounters a new peer).
  - **Status Changes** (ONLINE/OFFLINE transitions).
- Presence signals (`hey_there`, `chau`, `ping`, `social`) also drive derived online state.
- Derived values (`Restarts`, `TotalUptime`) are recalculated from the ledger history + checkpoints.

## Interfaces

### ObservationEventPayload (JSON)
```json
{
  "observer": "observer-name",
  "subject": "subject-name",
  "type": "restart | first-seen | status-change",
  "importance": 1|2|3,
  "start_time": 1700000000,
  "restart_num": 42,
  "last_restart": 1700000000,
  "online_state": "ONLINE",
  "observer_uptime": 12345,
  "is_backfill": false
}
```

### Event Content Strings (for Signing/Dedupe)
- **Restart**: `{subject}:restart:{restart_num}:{start_time}`
- **Other**: `{observer}:{subject}:{type}:{online_state}:{start_time}:{restart_num}`

## Algorithms

### 1. Deduplication
- **Restart Events**: Deduplicated by `(subject, restart_num, start_time)`. This ensures multiple observers can report the same restart without inflating the count.
- **First-Seen**: Deduplicated per observer-subject pair.
- **Status-Change**: Generally not deduplicated (records history of transitions).

### 2. Compaction (Ledger Maintenance)
To prevent unbound ledger growth:
- Limit: Max **20** observation events per observer-subject pair.
- Pruning Strategy when full:
  1. Prefer pruning the oldest **non-restart** event.
  2. If all are restarts and a **Checkpoint** exists: Prune oldest restart.
  3. If all are restarts and **No Checkpoint**: Do NOT prune (preserve history for future checkpointing).

### 3. Rate Limiting
- **Limit**: Max 10 events per subject per 5 minutes.
- **Scope**: Applied locally before ingesting events.

### 4. State Derivation
- **Restarts**:
  - `Base + NewRestarts`
  - Base = Checkpoint.Restarts OR Backfill.Restarts.
  - NewRestarts = Unique `StartTime`s observed *after* the base timestamp.
- **Total Uptime**:
  - `Base + NewUptime`
  - Base = Checkpoint.TotalUptime.
  - NewUptime = Sum of ONLINE intervals derived from status-change events after the base timestamp.
- **Online Status**:
  - Latest `observation` or `presence` event determines state.
  - **Timeouts**: ONLINE -> MISSING after 5m (or 1h if gossip mode).

## Failure Modes

- **Missing Observations**: Leads to incomplete restart counts or uptime until a checkpoint syncs.
- **Clock Skew**: Can cause disagreement on `StartTime` (consensus tolerates ±60s).
- **Spam**: Mitigated by rate limiting and compaction.

## Security / Trust Model

- **Authentication**: All observations are signed `SyncEvent`s.
- **Consensus**: Individual observations are "opinions". The network truth is derived via **Trimmed Mean Consensus** (removing outliers) and **Checkpoints**.
- **Trust**: Observers with higher uptime have more weight in some consensus decisions.

## Test Oracle

- **Deduplication**: Multiple reports of the same restart result in one logical event. (`observation_dedup_test.go`)
- **Compaction**: Ledger respects the 20-per-pair limit and preserves restarts when needed. (`observation_compaction_test.go`)
- **Rate Limit**: Excessive spam is dropped. (`observation_ratelimit_test.go`)
- **Derivation**: `DeriveRestartCount` and `DeriveTotalUptime` match expected values. (`consensus_events_test.go`)

## Open Questions / TODO

- **Refactor Plan**: `ObservationEventPayload` should embed `NaraObservation` directly for consistency with `CheckpointEventPayload`.
