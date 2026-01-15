---
title: Memory Model
description: Hazy memory, event-sourced state, and priority-based forgetting in Nara.
---

# Memory Model

Nara implements a "hazy memory" model where state is entirely in-memory and derived from a stream of signed events. Memory is intentionally bounded to mimic human-like forgetting and to ensure the system remains lightweight.

## Purpose
- Provide a consistent but subjective view of the network state.
- Ensure the system can run on resource-constrained hardware (e.g., Raspberry Pi Zero).
- Model social forgetting: information that is no longer "meaningful" or "critical" is eventually lost.
- Drive eventual consistency through social replication rather than traditional database persistence.

## Conceptual Model
- **SyncLedger**: The primary in-memory store for all events.
- **Hazy Memory**: State is not persisted to disk. Upon restart, a Nara is "blank" and must recover its memory from peers.
- **Subjectivity**: Each Nara's memory is unique, shaped by its personality and the specific peers it has synced with.
- **Forgetting**: Information is lost permanently if it is pruned by every Nara in the network.

### Invariants
- **No Disk Persistence**: Beyond basic configuration, all network state lives only in RAM.
- **Bounded Capacity**: The ledger is strictly capped by `MaxEvents`.
- **Integrity**: Every event in memory is cryptographically signed and verified.
- **Social Reliability**: The network's collective memory is more reliable than any single node's memory.

## External Behavior
- **Boot Recovery**: On startup, a Nara aggressively syncs with neighbors to fill its "memory capacity".
- **Opinion Shifts**: As new events are synced or pruned, a Nara's opinion of its peers (e.g., their uptime or clout) may shift.
- **Resource Awareness**: Naras report their memory mode and budget to peers, which influences how others interact with them (e.g., for [Stash](./stash.md) selection).

## Memory Modes
Memory profiles are automatically determined based on available system RAM or cgroup limits.

| Mode | Budget | Max Events | Behavior |
| :--- | :--- | :--- | :--- |
| **`short`** | 256MB | 20,000 | Background sync disabled; stores only the most essential history. |
| **`medium`**| 512MB | 80,000 | Standard profile; balanced retention and resource usage. |
| **`hog`** | 2GB+ | 320,000 | High retention; acts as a "historian" for the network. |

## Algorithms

### 1. Priority-Based Pruning
When the ledger reaches its capacity, events are removed using a **high-value-first** priority scheme:
1. **Unknown Naras**: Events involving naras with no known public key are pruned first.
2. **Priority 4 (Lowest)**: `ping` observations (ephemeral latency data).
3. **Priority 3**: `seen` events (secondary status proofs).
4. **Priority 2**: `social` events (teases, gossip).
5. **Priority 1**: `status-change`, `hey-there`, `chau`.
6. **Priority 0 (Critical)**: `checkpoint`, `restart`, `first-seen`. **These are never pruned.**

### 2. Observation Compaction
Per (Observer, Subject) pair, the ledger maintains a maximum of 20 events.
- To preserve the "Trinity" (Restarts, Uptime, StartTime), non-restart events are pruned before restart events.
- Restart events are only pruned if a verified [Checkpoint](./checkpoints.md) exists for that subject, ensuring history is never lost before it is anchored.

### 3. Peer Pruning (Neighbourhood)
Naras also "forget" peers they haven't seen in a long time to keep the neighborhood list manageable:
- **Zombies**: Naras seen once briefly and never again are pruned during boot opinion passes.
- **Newcomers** (Known < 2 days): Pruned after 24h of being `MISSING`.
- **Established** (Known < 30 days): Pruned after 7 days of being `MISSING`.
- **Veterans** (Known >= 30 days): **Never auto-pruned** from memory.

## Failure Modes
- **Memory Pressure**: If too many "never-pruned" events accumulate (e.g., thousands of naras joining), the ledger will exceed its budget, potentially leading to OOM (Out of Memory).
- **Consensus Drift**: If a Nara's `MaxEvents` is too small, it may prune events necessary for accurate [Trinity](./observations.md#conceptual-model) derivation.
- **Total Network Wipe**: If every Nara in a cluster restarts at the same time, all social history is lost.

## Security / Trust Model
- **Authenticity Over Completeness**: Nara prioritizes knowing that an event is *true* (signed) over knowing *every* event that happened.
- **Historians**: High-memory naras (`hog` mode) provide the "long-term memory" for the network, acting as trusted sources for deep sync.

## Test Oracle
- `TestMemoryMode_BudgetSelection`: Verifies that the correct mode is chosen based on system RAM.
- `TestSyncLedger_PriorityPruning`: Ensures that pings are pruned before social events when the ledger is full.
- `TestNeighbourhood_VeteranRetention`: Checks that veteran naras are never pruned regardless of their online status.
- `TestObservation_RestartPreservation`: Confirms that restarts are not pruned without a corresponding checkpoint.
