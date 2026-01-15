---
title: Sync Protocol
description: P2P reconciliation and event exchange in the Nara Network.
---

# Sync Protocol

The Sync Protocol enables naras to reconcile their hazy memories by exchanging events from their local ledgers. It is designed to handle partial connectivity and memory constraints.

## Purpose
- Reconcile missed events after downtime or network partitions.
- Distribute historical state (checkpoints) to new nodes.
- Maintain a "hazy" but consistent shared history across the network.
- Support different synchronization strategies (full deep sync vs. casual catch-up).

## Conceptual Model
- **SyncLedger**: A unified, in-memory store for all syncable events.
- **Deduplication**: Events are uniquely identified by a hash of their timestamp and content.
- **Pruning**: Ledgers have a `MaxEvents` limit. When full, events are removed based on priority and age.
- **Critical History**: Certain events (checkpoints, restarts, first-seen) are never pruned to preserve the network's skeletal history.
- **Hazy Memory**: Personality affects which social events are stored, making each Nara's memory unique and subjective.

### Invariants
- **Content-based ID**: Modifying an event after ID computation is forbidden.
- **Deduplication**: The same event (by ID) is never stored twice in the same ledger.
- **Priority Pruning**: Critical events are preserved over ephemeral ones (pings, casual teases).
- **Rate Limiting**: Observations are limited to 10 per subject per 5 minutes to prevent ledger spam.

## External Behavior
- **Boot Sync**: On startup, a Nara attempts to sync deep history (often using `page` or `sample` mode) from multiple neighbors.
- **Steady State**: Naras periodically fetch `recent` events from peers to fill gaps.
- **Gossip Integration**: Events received via sync are often re-gossiped via zines.

## Interfaces

### HTTP API: `POST /api/sync`
Used for peer-to-peer reconciliation.

**SyncRequest Fields**:
- `from`: Requester name.
- `mode`: "sample", "page", or "recent".
- `services`: Filter to specific services (e.g., ["social", "checkpoint"]).
- `subjects`: Filter to events involving specific naras.
- `limit`: (Recent mode) Max events to return.
- `cursor`: (Page mode) Timestamp to start from.
- `page_size`: (Page mode) Max events per page.
- `sample_size`: (Sample mode) Desired number of sampled events.

**SyncResponse Fields**:
- `from`: Responder name.
- `events`: Array of `SyncEvent` objects.
- `next_cursor`: Timestamp for the next page of results.
- `ts`: Unix seconds when the response was generated.
- `sig`: Base64 Ed25519 signature of the response metadata and event IDs.

### SyncEvent Structure
- `id`: 16-character hex hash.
- `ts`: Unix timestamp in **nanoseconds**.
- `svc`: Service type ("social", "ping", "observation", "hey-there", "chau", "seen", "checkpoint").
- `emitter`: Name of the Nara that created the event.
- `emitter_id`: Nara ID of the emitter (for signature verification).
- `sig`: Base64 Ed25519 signature of the event content.

## Algorithms

### 1. ID Computation
`ID = hex(SHA256(timestamp_nanos + ":" + service + ":" + payload_content_string)[0:16])`

### 2. Priority-Based Pruning
When `len(ledger) > MaxEvents`, events are removed in this order:
1. Unknown nara events (naras with no known public key).
2. Priority 4: Ping observations (ephemeral).
3. Priority 3: Seen events (secondary status signals).
4. Priority 2: Social events (teases, gossip).
5. Priority 1: `status-change` observations, `hey-there`, `chau`.
6. **NEVER PRUNED** (Priority 0): `checkpoint`, `restart`, `first-seen`.

### 3. Observation Compaction
Per (Observer, Subject) pair, only `MaxObservationsPerPair` (default 20) are kept.
- Prefers pruning `status-change` over `restart`.
- If only restarts remain, they are only pruned if a `checkpoint` exists for that subject (to ensure data isn't lost before being anchored).

### 4. Personality-Aware Weighting
Social events are weighted based on:
- **Sociability**: High sociability increases weight of social events.
- **Chill**: High chill decreases weight of drama/teases.
- **Time Decay**: Half-life modified by personality (Chill naras forget faster).
- **Meaningfulness**: Events with weight < threshold are ignored by the ledger.

## Failure Modes
- **Ledger Saturation**: If too many "never-pruned" events accumulate, the ledger grows beyond `MaxEvents`.
- **Clock Skew**: Events with future timestamps are treated as very old (subject to immediate decay) or rejected.
- **Signature Failure**: Events with invalid signatures are discarded during ingestion.
- **Checkpoint Cutoff**: Checkpoints created before `1768271051` are filtered out due to legacy schema bugs.

## Security / Trust Model
- **Authenticity**: Every `SyncEvent` is signed by its emitter.
- **Reputation**: Responses are signed by the responder, allowing peers to hold each other accountable for the data they serve.
- **Filtering**: Nodes filter "junk" or "inauthentic" events before storage.

## Test Oracle
- `TestSyncLedger_Deduplication`: Ensures the same event ID isn't added twice.
- `TestSyncLedger_Pruning`: Verifies that low-priority events are removed first.
- `TestSyncLedger_ObservationCompaction`: Confirms that restart history is preserved until checkpointed.
- `TestSyncLedger_GetEventsPage`: Validates deterministic pagination in deep sync.
- `TestObservationRateLimit`: Ensures the 10-per-5-min limit is enforced.
- `TestSyncLedger_PersonalityFilter`: Checks that very chill naras ignore casual social events.
