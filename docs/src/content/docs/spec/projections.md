---
title: Projections
description: Deterministic, event-sourced views of network state in Nara.
---

# Projections

Projections are deterministic, read-only views derived from the event ledger. They implement the "Opinion" part of Nara's architecture: while events are the immutable facts, projections represent a Nara's subjective interpretation of those facts.

## Purpose
- Transform a raw stream of events into actionable state (e.g., "Is Nara X online?").
- Reach deterministic consensus on peer metrics (Restarts, Uptime) without global coordination.
- Compute complex metrics like [Clout](./clout.md) on-the-fly.
- decuouple event storage from the logic used to interpret those events.

## Conceptual Model
- **Pure Function**: State is a function of replaying events in order: `State = f(Events)`.
- **Incremental**: To save resources, projections process only new events since the last update.
- **Auto-Reset**: If the underlying ledger is pruned or restructured (changing its version), the projection automatically wipes its state and replays the entire ledger.
- **Subjective**: Projections are influenced by the Nara's soul and personality, meaning two naras may derive different states from the same events.

### Invariants
- **Read-Only**: Projections do not emit events or modify the ledger.
- **Event-Driven**: Projections only update when new data arrives or a manual trigger occurs.
- **Consistent Order**: Events are always replayed in the order they appear in the ledger.

## Major Projections

### 1. Online Status Projection
Tracks the liveness of peers based on their activity.
- **Activity Proofs**: `hey-there`, `Seen`, `Ping`, and `Social` events mark a Nara as **ONLINE**.
- **Graceful Shutdown**: `chau` marks a Nara as **OFFLINE**.
- **Missing Decay**: If the most recent activity is older than a threshold (5m for standard, 1h for gossip), the Nara is marked **MISSING**.

### 2. Opinion Consensus Projection
Consolidates divergent peer observations into a single set of metrics.
- **Trinity Derivation**: Calculates the consensus `StartTime`, `Restarts`, and `TotalUptime` for every peer.
- **Checkpoint Integration**: Uses verified [Checkpoints](./checkpoints.md) as a trusted baseline to add new replayed events on top of.

### 3. Clout Projection
Accumulates social interactions to determine subjective reputation.
- **Resonance**: Uses the `TeaseResonates` algorithm (see [Clout](./clout.md)) to filter interactions.
- **Weighted History**: Recent interactions carry more weight than old ones.

## Algorithms

### 1. Projection Lifecycle (`RunOnce`)
1. Check the ledger's structural version.
2. If version mismatch:
   - Call `Reset()` (clears internal maps/counters).
   - Set `Position = 0`.
3. Fetch events from the ledger starting at `Position`.
4. Apply each event to the projection's internal logic.
5. Update `Position` and `Version` to match the ledger.

### 2. Trinity Validation
Naras compare their observation-based opinion against checkpoint-based opinions. If the two methods diverge significantly (e.g., >5 restarts or >1 hour difference in StartTime), a warning is logged to signal potential consensus drift.

## Failure Modes
- **Ledger Lag**: If the ledger is updated faster than the projection can replay it, the derived state will be "stale" until the projection catches up.
- **Pruning Reset**: Aggressive ledger pruning triggers frequent full replays, which can be CPU-intensive on high-memory nodes (`hog` mode).

## Security / Trust Model
- **Integrity**: Projections are as trustworthy as the signed events they consume.
- **Auditability**: Since the ledger is preserved, any "opinion" can be verified by replaying the source events.

## Test Oracle
- `TestProjections_ResetOnPrune`: Verifies that projections correctly wipe state when the ledger is pruned.
- `TestOnlineStatus_Decay`: Ensures that the `MISSING` state is correctly triggered after the timeout period.
- `TestOpinion_ConsensusValidation`: Checks that drift between observation and checkpoint methods is correctly detected.
- `TestClout_SubjectiveDivergence`: Validates that two different personalities derive different clout scores from the same event stream.
