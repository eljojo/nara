---
title: Memory Model
description: Hazy memory, event-sourced state, and priority-based forgetting in Nara.
---

# Memory Model

Nara uses a "hazy memory" model: state is in-memory and derived from signed events. Memory is strictly bounded to mimic human-like forgetting and maintain a lightweight footprint.

## 1. Purpose
- Subjective, consistent network state view.
- Support for resource-constrained hardware.
- Model social forgetting of non-critical data.
- Eventual consistency via social replication.

## 2. Conceptual Model
- **SyncLedger**: Primary in-memory event store.
- **Hazy Memory**: No disk persistence. Memory is recovered from peers on restart.
- **Subjectivity**: Memory is unique per Nara, shaped by personality and sync history.
- **Volatility**: Pruned data is lost if no Nara in the network retains it.

### Invariants
- **RAM-Only**: No network state on disk beyond config.
- **Bounded**: Ledger strictly capped by `MaxEvents`.
- **Integrity**: Every event is cryptographically signed.
- **Collective Reliability**: Shared memory exceeds single-node reliability.

## 3. Memory Modes
Profiles based on available RAM:

| Mode | RAM Budget | Max Events | Behavior |
| :--- | :--- | :--- | :--- |
| **`short`** | 256MB | 20,000 | Background sync disabled; essential history only. |
| **`medium`**| 512MB | 80,000 | Balanced retention/usage. |
| **`hog`** | 2GB+ | 320,000 | "Historian" mode; high retention for deep sync. |

## 4. Algorithms

### Priority-Based Pruning
When `len(ledger) > MaxEvents`, remove by priority (lowest first):
1. **Unknown Naras**: No known public key.
2. **Priority 4**: `ping` (ephemeral).
3. **Priority 3**: `seen` (status proof).
4. **Priority 2**: `social` (teases, gossip).
5. **Priority 1**: `status-change`, `hey-there`, `chau`.
6. **Priority 0**: `checkpoint`, `restart`, `first-seen`. **Never pruned.**

### Observation Compaction
- Max 20 events per (Observer, Subject) pair.
- Prune non-restart events first.
- Restart events pruned only if a subject [Checkpoint](./checkpoints.md) exists.

### Peer Pruning (Neighbourhood)
- **Zombies**: Seen briefly once; pruned during boot.
- **Newcomers** (< 2d): Pruned after 24h `MISSING`.
- **Established** (< 30d): Pruned after 7d `MISSING`.
- **Veterans** (>= 30d): **Never auto-pruned.**

## 5. Failure Modes
- **Memory Pressure**: Accumulation of Priority 0 events can cause OOM.
- **Drift**: Low `MaxEvents` may lose events needed for Trinity derivation.
- **Network Wipe**: Simultaneous restart of all nodes clears social history.

## 6. Security
- **Authenticity**: Prioritize signed truth over completeness.
- **Trust**: `hog` nodes act as authoritative sources for historical data.

## 7. Test Oracle
- `TestMemoryMode_BudgetSelection`: Mode choice vs. RAM.
- `TestSyncLedger_PriorityPruning`: Order of removal.
- `TestNeighbourhood_VeteranRetention`: Immortality of veteran nodes.
- `TestObservation_RestartPreservation`: Checkpoint-dependent pruning.
