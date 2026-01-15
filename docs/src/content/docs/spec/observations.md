# Observations

## Purpose

Observations record uptime, restarts, and online state changes as events so that
consensus and projections can be derived deterministically.

## Conceptual Model

- Observation events are SyncEvents with service `observation`.
- The derived state is stored in `NaraObservation` records per subject.

NaraObservation fields:
- `restarts` (int64)
- `total_uptime` (seconds)
- `start_time` (Unix seconds)
- `online` ("ONLINE" | "OFFLINE" | "MISSING")
- `last_seen` (Unix seconds)
- `last_restart` (Unix seconds)
- `cluster_name`, `cluster_emoji`
- Ping fields (local-only): `last_ping_rtt`, `avg_ping_rtt`, `last_ping_time`

Key invariants:
- Observation event timestamps inside payloads are in seconds.
- SyncEvent timestamps are in nanoseconds.
- Restart events can be deduplicated by content.

## External Behavior

- Observations are emitted for restarts, first-seen events, and status changes.
- Presence signals (hey_there, chau, ping, social) also drive derived online state.
- Derived values are recalculated from the ledger, not stored as truth.

## Interfaces

ObservationEventPayload:
- `observer`, `subject`, `type`, `importance`
- `type` is "restart", "first-seen", or "status-change"
- `start_time`, `restart_num`, `last_restart` (seconds)
- `online_state` ("ONLINE", "OFFLINE", "MISSING")
- `observer_uptime` (seconds)
- `is_backfill` (bool)

## Event Types & Schemas (if relevant)

Observation event content strings:
- Restart: `{subject}:restart:{restart_num}:{start_time}`
- Other types: `{observer}:{subject}:{type}:{online_state}:{start_time}:{restart_num}`

## Algorithms

Deduplication (optional path):
- `AddEventWithDedup` deduplicates restart events by
  `(subject, restart_num, start_time)`.
- First-seen events are only deduped per observer->subject pair.
- Status-change events are not deduplicated.

Compaction:
- Max 20 observation events per observer->subject pair.
- When over limit:
  - Prefer pruning the oldest non-restart event.
  - If all are restarts and a checkpoint exists for the subject, prune oldest restart.
  - If all are restarts and no checkpoint exists, do not prune (preserve history).

Rate limiting (optional path):
- `AddEventWithRateLimit` limits to 10 events per subject per 5 minutes.

Derived restart count:
1. If a checkpoint exists: checkpoint.restarts + unique restart StartTimes after checkpoint.
2. Else if a backfill exists: backfill.restart_num + unique StartTimes excluding the backfill’s StartTime.
3. Else: count unique StartTimes from all restart events.

Derived total uptime:
1. If checkpoint exists: checkpoint.total_uptime + uptime from status-change events after checkpoint.
2. Else if backfill exists: assume online since backfill start_time, adjusted by status-change events.
3. Else: sum online intervals from status-change events.

Online status:
- Most recent presence/observation event wins.
- ONLINE decays to MISSING after 5 minutes, or 1 hour if either node is in gossip mode.

Consensus opinion (projection):
- Start time uses trimmed-mean consensus over observations.
- Restart count uses trimmed mean when multiple observers exist; otherwise max.
- Last restart is the max observed `last_restart`.

## Failure Modes

- Missing observations lead to incomplete restart counts or uptime.
- Clock skew affects start_time consensus (tolerance is 60s).
- Excess observation spam is trimmed by rate limiting (if used).

## Security / Trust Model

- Observations are signed as SyncEvents; signatures authenticate the observer.
- Observations can be wrong; consensus is used to reduce outliers.

## Test Oracle

- Restart dedupe by content. (`observation_dedup_test.go`)
- Compaction respects the 20-per-pair limit. (`observation_compaction_test.go`)
- Rate limit of 10 events per 5 minutes. (`observation_ratelimit_test.go`)
- Derived restart counts and uptime align with checkpoints. (`consensus_events_test.go`)

## Open Questions / TODO

- Migrate ObservationEventPayload to embed NaraObservation directly (planned refactor).
