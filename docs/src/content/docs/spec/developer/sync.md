---
title: Sync Protocol
description: P2P reconciliation and event exchange in the nara network.
---

The Sync Protocol enables naras to reconcile "hazy memories" by exchanging events from local ledgers, handling partial connectivity and memory constraints.

## 1. Purpose
- Reconcile missed events after downtime or network partitions.
- Distribute historical state (checkpoints) to new nodes.
- Maintain a consistent but subjective shared history.
- Support diverse sync strategies (deep sync vs. casual catch-up).

## 2. Conceptual Model
- **SyncLedger**: The in-memory store for all syncable events.
- **Deduplication**: Content-hash-based identification prevents redundant storage.
- **Hazy Memory**: Personality-based filtering and priority-based pruning ensure memory is unique and subjective.
- **Sync Modes**: `sample` (personality-weighted), `page` (deterministic retrieval), `recent` (last N events).

### Invariants
1. **Uniqueness**: No duplicate IDs in a single ledger.
2. **Priority Pruning**: Events are removed by priority and age; Critical events (Priority 0) are NEVER pruned.
3. **Rate Limiting**: Observations are limited to 10 per subject per 5-minute window per node.

## 3. External Behavior
- **Boot Sync**: On startup, a nara performs a deep sync from neighbors to recover state.
- **Steady State**: Naras periodically fetch `recent` events from neighbors to fill small gaps.
- **Gossip**: Events received via sync may be re-distributed via P2P Zine gossip.

## 4. Interfaces

### HTTP API: `POST /api/sync`
Receives a `SyncRequest` and returns a `SyncResponse`.

**SyncRequest**:
- `mode`: `sample` | `page` | `recent`.
- `services`: Filter by service types (e.g., `["social", "observation"]`).
- `subjects`: Filter by involved nara names.
- `cursor`: Starting timestamp for `page` mode.
- `limit`: Maximum events for `recent` mode.
- `sample_size`: Number of events for `sample` mode.

**SyncResponse**:
- `from`: Responder nara name.
- `events`: Array of `SyncEvent` objects.
- `next_cursor`: Cursor for the next page of results.
- `ts`: Response generation timestamp.
- `sig`: Ed25519 signature over metadata + event IDs.

## 5. Event Types & Schemas
The protocol exchanges `SyncEvent` objects as defined in the [Events Spec](/docs/spec/developer/events/).

## 6. Algorithms

### Priority-Based Pruning
When `len(ledger) > MaxEvents`, events are sorted and removed in this order:
1. **Unknown Emitter**: Events from naras without a known public key.
2. **Priority 4**: `ping` (ephemeral).
3. **Priority 3**: `seen`.
4. **Priority 2**: `social` (teases, gossip).
5. **Priority 1**: `status-change`, `hey-there`, `chau`.
6. **Priority 0 (Never Pruned)**: `checkpoint`, `restart`, `first-seen`.

### Observation Compaction
To prevent one subject from dominating the ledger:
- Max 20 observations per (Observer, Subject) pair.
- `status-change` is pruned before `restart`.
- `restart` is only pruned if a `checkpoint` exists for that subject.

### Personality & Sampling (`sample` mode)
- **Weighting**: Social events are weighted by `Sociability` and `Chill`.
- **Decay**: Newer events have higher weight; weights decay over time.
- **Relevance**: 2x weight boost for events involving the requester.
- **Interleaved Slicing**: Distributed coverage via `index % sliceTotal == sliceIndex`.

## 7. Failure Modes
- **Ledger Saturation**: If critical events exceed `MaxEvents`, the ledger grows unbounded to preserve history.
- **Clock Skew**: Events with timestamps far in the future may prevent recent events from being synced or cause premature pruning.
- **Partitioning**: Deep sync may take multiple passes to converge if the network is highly fragmented.

## 8. Security / Trust Model
- **Authenticity**: Every event is individually signed by its author.
- **Accountability**: The `SyncResponse` is signed by the responder, ensuring the neighbor is responsible for the bundle of events they provided.
- **Integrity**: Modification of any event in the response invalidates the response signature.

## 9. Test Oracle
- `TestSyncLedger_Deduplication`: Verifies that duplicate IDs are rejected.
- `TestSyncLedger_Pruning`: Ensures low-priority events are removed before high-priority ones.
- `TestSyncLedger_ObservationCompaction`: Verifies restart preservation.
- `TestSyncLedger_GetEventsPage`: Validates deterministic pagination.
- `TestObservationRateLimit`: Ensures the 10-per-5-min limit is enforced.
- `TestSyncLedger_SampleMode`: Verifies personality-aware weighting and sampling.

## 10. Open Questions / TODO
- Move `MaxEvents` to be memory-aware (already implemented for stash, needs porting to ledger).
- Implement "Sync Checkpoints" that allow faster reconciliation of large ledgers.
