---
title: Projections
---

# Projections

## Purpose

Projections turn the event ledger into derived state: online status, clout, and
consensus observations. They keep the system deterministic without storing
mutable state in the ledger itself.

## Conceptual Model

- **Projections** are pure, deterministic read models over `SyncEvent`s.
- The same ledger state produces the same projection output.
- Projections track a ledger version and event position, allowing them to reset and replay if the ledger is restructured (e.g., pruning).

Key invariants:
- **Read-Only**: Projections never emit events and never mutate the ledger.
- **Determinism**: Replaying events in the same order produces identical state.
- **Resilience**: If the ledger is pruned or reordered, projections detect the version change and reset/replay automatically.

## External Behavior

- Projections run continuously in the background (via `RunToEnd` or `RunOnce`).
- They provide synchronous read access to derived state (e.g., `IsOnline(name)`).
- When new events arrive, projections update incrementally.

## Interfaces

### ProjectionStore
The container for all active projections.
- `OnlineStatus()` -> `OnlineStatusProjection`
- `Clout()` -> `CloutProjection`
- `Opinion()` -> `OpinionConsensusProjection`
- `Trigger()` -> forces an immediate update cycle.

### OnlineStatusProjection
Tracks who is online/offline based on presence and observation events.
- `GetStatus(name)` -> "ONLINE", "OFFLINE", "MISSING", or "" (unknown).
- `GetTotalUptime(name)` -> uptime in seconds (derived from ledger, not real-time).

### OpinionConsensusProjection
Derives consensus on network state (restarts, start time) from multiple observers.
- `DeriveOpinion(name)` -> `OpinionData` (start time, restart count, last restart, total uptime).
- `DeriveOpinionFromCheckpoint(name)` -> Calculation using checkpoint baseline + post-checkpoint events.
- `DeriveOpinionWithValidation(name)` -> Compares derived values with other methods for debugging.

### CloutProjection
Calculates social clout scores based on interaction events.
- `GetClout(name)` -> Score (0-100).

## Event Types & Schemas

- **OnlineStatus**: Consumes `hey-there`, `chau`, `ping`, `social`, `seen`, and `observation` (restart, first-seen, status-change).
- **OpinionConsensus**: Consumes `observation` events (restart, first-seen).
- **Clout**: Consumes `social` events (tease, observed).

## Algorithms

### Projection Reset & Replay
To handle ledger changes (pruning/reordering):
1. Projection stores `lastVersion` of the ledger.
2. Before processing, it checks `ledger.GetVersion()`.
3. If versions differ:
   - Reset internal state (e.g., clear maps).
   - Reset position to 0.
   - Replay **all** events from the ledger, sorted by timestamp.
4. If versions match:
   - Fetch events since `position`.
   - Apply incrementally.

### Online Status Derivation
"Most recent event wins" logic:
- `hey-there` -> ONLINE
- `chau` -> OFFLINE
- `seen`, `social`, `ping` -> ONLINE
- `observation`:
  - `status-change` -> Set to payload state (ONLINE/OFFLINE/MISSING).
  - `restart`, `first-seen` -> ONLINE.
- **Timeout**: If the last ONLINE event is older than 5 minutes (or 1 hour in gossip mode), status decays to MISSING.

### Opinion Consensus (Trimmed Mean)
- **Start Time**: Collect `first-seen` and `restart` (start_time) observations. Apply trimmed mean (remove outliers) to find the consensus start time.
- **Restart Count**:
  - If multiple observers: Trimmed mean of observed counts.
  - If single/few observers: Max observed count.
- **Last Restart**: Max observed `last_restart` timestamp.

## Failure Modes

- **Stale Projections**: If the background loop stalls, read models may be outdated. `Trigger()` forces an update.
- **Divergence**: Missing events (e.g., during partition) lead to different derived states on different nodes. Consensus algorithms mitigate this impact.
- **Replay Cost**: Heavy pruning causes frequent resets, spiking CPU usage as projections replay the full history.

## Security / Trust Model

- **Local View**: Projections represent the *local* node's interpretation of the ledger. They are not shared or signed.
- **Verifiability**: Any peer with the same events will derive the exact same projection state (event sourcing guarantee).

## Test Oracle

- **Determinism**: Replaying the same events yields the same state. (`projections_test.go`)
- **Reset Logic**: Modifying the ledger (pruning) triggers a projection reset. (`projections_test.go`)
- **Consensus**: Trimmed mean logic correctly identifies consensus values amidst outliers. (`consensus_events_test.go`)
- **Online Decay**: Status transitions to MISSING after timeout. (`presence_projection_test.go` - inferred coverage)

## Open Questions / TODO

- **Confidence Scores**: Add metadata to projection outputs indicating confidence (e.g., "based on 1 observer" vs "based on 10").
