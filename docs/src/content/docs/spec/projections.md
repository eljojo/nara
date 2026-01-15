# Projections

## Purpose

Projections turn the event ledger into derived state: online status, clout, and
consensus observations. They keep the system deterministic without storing
mutable state in the ledger itself.

## Conceptual Model

- Projections are pure, deterministic read models over SyncEvents.
- The same ledger state produces the same projection output.
- Projections can reset and replay when the ledger structure changes.

Key invariants:
- Projections never emit events.
- Projections never mutate the ledger.
- Reset + replay is allowed when the ledger is pruned or reordered.

## External Behavior

- Projections run continuously in the background.
- They may lag behind until explicitly triggered or RunOnce is called.
- Derived values are used for UI, decision-making, and maintenance logic.

## Interfaces

ProjectionStore:
- `OnlineStatus()` -> OnlineStatusProjection
- `Clout()` -> CloutProjection
- `Opinion()` -> OpinionConsensusProjection
- `Trigger()` -> run all projections immediately

OnlineStatusProjection:
- `GetStatus(name)` -> "ONLINE", "OFFLINE", "MISSING", or "" (unknown)
- `GetTotalUptime(name)` -> uptime in seconds (derived from ledger)

OpinionConsensusProjection:
- `DeriveOpinion(name)` -> start time, restart count, last restart, total uptime
- `DeriveOpinionFromCheckpoint(name)` -> checkpoint baseline + post-checkpoint events
- `DeriveOpinionWithValidation(name)` -> compares both methods and logs divergence

## Event Types & Schemas (if relevant)

- OnlineStatusProjection consumes presence, observation, ping, and social events.
- OpinionConsensusProjection consumes observation events (restart/first-seen).
- CloutProjection consumes social events.

## Algorithms

Projection reset and replay:
- Each projection tracks a ledger version and event position.
- If the ledger structure changes (pruning or reordering), projections reset to
  position 0 and replay events sorted by timestamp for determinism.

Online status derivation (most recent event wins):
- `hey-there` -> ONLINE
- `chau` -> OFFLINE
- `seen`, `social`, `ping` -> ONLINE
- `observation`:
  - `status-change` -> ONLINE/OFFLINE/MISSING
  - `restart` or `first-seen` -> ONLINE
- If the last ONLINE event is older than the missing threshold, status is MISSING.
  Threshold is 5 minutes by default, 1 hour when either node is in gossip mode.

Opinion consensus:
- Start time uses trimmed-mean consensus over `first-seen` and restart observations.
- Restart count uses trimmed mean when multiple observers exist; otherwise max.
- Last restart uses the maximum observed `last_restart` value.
- Total uptime uses ledger-derived uptime (checkpoint + status changes).

## Failure Modes

- Projections can be stale until triggered.
- Missing events can lead to divergent opinions across naras.
- Pruning can cause projections to reset and replay (temporary CPU spike).

## Security / Trust Model

- Projections are local opinions; they are not verifiable in isolation.
- Only the underlying events are signed and verifiable.

## Test Oracle

- Same ledger state yields deterministic projections. (`projections_test.go`)
- Online status transitions follow most-recent-event semantics. (`presence_howdy_test.go`)
- Opinion consensus uses trimmed-mean rules. (`consensus_events_test.go`)

## Open Questions / TODO

- Add explicit confidence scores to projection outputs.
