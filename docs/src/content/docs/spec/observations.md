---
title: Observations
---

# Observations

Observations record peer uptime, restarts, and online state changes as signed events, enabling deterministic consensus.

## Conceptual Model

| Data Type | Description |
| :--- | :--- |
| **`NaraObservation`** | State record including the "Trinity": `Restarts`, `TotalUptime`, `StartTime`. |
| **`observation`** | `SyncEvent` service type for reporting network state changes. |

### Invariants
- **Payload Timestamps**: Seconds (Unix).
- **SyncEvent Timestamps**: Nanoseconds (Unix).
- **Consensus**: Observations are "opinions"; network truth is derived via Trimmed Mean and Checkpoints.

## Algorithms

### 1. Deduplication
- **Restarts**: Unique by `(subject, restart_num, start_time)`. Prevents inflation from multiple observers.
- **First-Seen**: Unique per observer-subject pair.

### 2. Ledger Compaction
Prevents unbound growth (Limit: 20 events per observer-subject pair):
1. Prune oldest non-restart events first.
2. If only restarts remain:
   - If Checkpoint exists: Prune oldest restart.
   - If No Checkpoint: **Preserve all** (history is critical for checkpointing).

### 3. State Derivation
- **Restarts**: `Baseline + Count(Unique StartTimes since baseline)`.
- **Total Uptime**: `Baseline + Sum(ONLINE intervals since baseline)`.
- **Online Status**: Latest activity wins. Decays to `MISSING` after 5m (Standard) or 1h (Gossip).

## Interfaces

### Observation Payload (`svc: observation`)
```json
{
  "observer": "nara-name",
  "subject": "nara-name",
  "type": "restart | first-seen | status-change",
  "importance": 1|2|3,
  "start_time": 1700000000,
  "restart_num": 42,
  "online_state": "ONLINE",
  "is_backfill": false
}
```

## Constraints
- **Rate Limiting**: Max 10 events per subject per 5 minutes.
- **Clock Tolerance**: Consensus allows ±60s skew on `StartTime`.

## Security
- **Weighting**: Observers with higher uptime carry more weight in consensus.
- **Authenticity**: Guaranteed by the signed `SyncEvent` wrapper.

## Test Oracle
- **Deduplication**: `observation_dedup_test.go`
- **Compaction**: `observation_compaction_test.go`
- **Rate Limiting**: `observation_ratelimit_test.go`
- **Derivation Accuracy**: `consensus_events_test.go`
