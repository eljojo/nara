---
title: Memory Model
description: Hazy memory, event-sourced state, and priority-based forgetting in nara.
---

nara uses a "hazy memory" model: all network state is in-memory (RAM-only) and derived from signed events. Memory is strictly bounded to mimic human-like forgetting and maintain a lightweight footprint.

## 1. Purpose
- Maintain a subjective but consistent view of the network state.
- Ensure the software can run on resource-constrained hardware (e.g., small VMs, Raspberry Pis).
- Model "social forgetting" where non-critical data is naturally lost over time.
- Rely on collective replication across peers rather than local disk persistence.

## 2. Conceptual Model
- **SyncLedger**: The primary in-memory store for all syncable events.
- **Hazy Memory**: By design, there is no disk persistence for network data. Memory is recovered from peers on restart.
- **Subjectivity**: Every nara's memory is unique, shaped by its personality and the specific set of peers it has synced with.
- **Volatility**: Once an event is pruned from all ledgers in the network, it is lost forever.

### Invariants
1. **RAM-Only**: No network state is stored on disk (identity and local config are the only exceptions).
2. **Strict Bounds**: The ledger size is capped by `MaxEvents`.
3. **Cryptographic Integrity**: Every fact in memory MUST be cryptographically signed by its author.
4. **Social Persistence**: Data is "stored" by being gossiped to others; its longevity depends on its perceived importance to the collective.

## 3. External Behavior
- On startup, a nara's memory is empty. It must "remember" the network by syncing with neighbors.
- As the ledger fills up, the nara "forgets" less important or older events to make room for new ones.
- Memory usage is categorized into "Modes" that adjust the ledger capacity based on available system resources.

## 4. Interfaces
### Memory Modes
The mode is typically detected automatically based on available RAM:

| Mode | Max Events | Max Stashes | Behavior |
| :--- | :--- | :--- | :--- |
| **`low`** | 20,000 | 5 | Background sync limited; only essential history retained. |
| **`medium`** | 80,000 | 20 | Balanced retention for active nodes. |
| **`high`** | 320,000 | 50 | "Historian" mode; high retention for deep network history. |

## 5. Event Types & Schemas
Memory management does not define its own events but dictates the lifecycle of all events in the [Events Spec](/docs/spec/events/).

## 6. Algorithms

### Priority-Based Pruning
When `len(ledger) > MaxEvents`, events are sorted by priority (lowest first) and removed.
- **Priority 4 (Lowest)**: `ping` measurements.
- **Priority 3**: `seen` proofs-of-contact.
- **Priority 2**: `social` (teases, gossip).
- **Priority 1**: `status-change`, `hey-there`, `chau`.
- **Priority 0 (Never Pruned)**: `checkpoint`, `restart`, `first-seen`.

### Neighbourhood Pruning (Peer Tracking)
Naras are removed from the local "neighbourhood" map based on their status and age:
- **Zombies**: Entries seen briefly without meaningful data are purged early.
- **Newcomers** (< 2 days old): Pruned after 24 hours of being `MISSING`.
- **Established** (2-30 days old): Pruned after 7 days of being `MISSING`.
- **Veterans** (>= 30 days old): **Never auto-pruned.** They remain in the neighbourhood indefinitely.

### Stash Limit
The [Stash Service](/docs/spec/stash/) limits the number of stashes it will hold for others based on the current Memory Mode (e.g., a `low` mode node will only hold 5 stashes).

## 7. Failure Modes
- **Memory Pressure**: An accumulation of Priority 0 events (which are never pruned) could theoretically lead to Out-of-Memory (OOM) conditions if the ledger isn't carefully monitored.
- **History Loss**: In a small network, if every node is in `low` memory mode, older social history may be lost very quickly.
- **Total Amnesia**: If every node in the network restarts simultaneously, all un-checkpointed social history is lost.

## 8. Security / Trust Model
- **History Attestation**: High-memory nodes (`high` mode) are more likely to have a complete history and thus act as more reliable sources for historical sync.
- **Identity Retention**: By never pruning "Veteran" naras, the system ensures that long-standing identities are recognized even after long absences.

## 9. Test Oracle
- `TestMemoryMode_BudgetSelection`: Verifies that the correct mode is selected based on available RAM.
- `TestSyncLedger_PriorityPruning`: Ensures that `ping` and `seen` events are removed before `social` or `observation` events.
- `TestNeighbourhood_VeteranRetention`: Confirms that veterans are not pruned even after long periods of being offline.
- `TestStash_MemoryModeLimits`: Validates that the stash service enforces limits based on the detected memory mode.

## 10. Open Questions / TODO
- Implement "Cold Storage" for `high` mode nodes to allow even larger ledgers by offloading some events to disk.
- Add "Forgetting Curves" where personality affects how quickly certain event types are pruned.
