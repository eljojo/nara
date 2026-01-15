---
title: Memory Model
---

# Memory Model

Nara's memory is event-based, in-memory only, and intentionally bounded. Knowledge survives only through social replication and peer overlap.

## Conceptual Model

| Concept | Rule |
| :--- | :--- |
| **No Persistence** | All state is lost on process termination; recovered via sync. |
| **Bounded** | Ledger capacity is strictly capped by the selected Memory Mode. |
| **Priority-Based**| Pruning favors critical consensus over casual social noise. |
| **Social Forgetting**| Information is lost permanently if all nodes prune it. |

## Memory Modes

| Mode | Capacity (Max Events) | Behavior |
| :--- | :--- | :--- |
| **`short`** | 20,000 | Background sync disabled; aggressive pruning. |
| **`medium`**| 80,000 | Standard operational history. |
| **`hog`** | 320,000 | Deep historical retention. |

## Pruning Algorithms

### 1. Ledger Pruning
When `MaxEvents` is reached, events are removed by **Priority** (high value = prune first), then by **Age**:
- **Priority 4**: `ping` (Aggressively trimmed to most recent 5 per target).
- **Priority 3**: `seen` (Ephemeral contact proofs).
- **Priority 2**: `social` (Teases and casual observations).
- **Priority 1**: `hey-there`, `chau`, `status-change`.
- **Priority 0**: `checkpoint`, `restart`, `first-seen` (Never pruned).

### 2. Observation Compaction
Limits to 20 events per observer-subject pair:
- Prefer pruning non-restart events.
- Never prune `restart` events unless a `checkpoint` exists for the subject.

### 3. Neighbourhood Pruning (Peer Memory)
- **Zombies**: Never seen; pruned immediately.
- **Newcomers** (<2 days old): Pruned after 24h offline.
- **Established** (2-30 days): Pruned after 7 days offline.
- **Veterans** (>=30 days): Never auto-pruned.
*Pruning a peer removes their identity, observations, and all associated events.*

## Lifecycle: Boot Recovery
1. **Mesh Sync**: Request weighted random samples from peers (see [Sync Protocol](./sync-protocol.md)).
2. **Replay**: Apply re-acquired events to projections.
3. **Checkpoint Anchor**: Reconstruct historical uptime baseline.

## Security
- **Integrity**: Pruning does not affect the authenticity of remaining events.
- **Availability**: Reliability emerges from the high overlap of Priority 0 events across the network.

## Test Oracle
- **Priority**: Verify `ping` is pruned before `social`. (`sync_test.go`)
- **Retention**: Ensure `checkpoint` survives full-ledger cycles. (`checkpoint_test.go`)
- **Peer Pruning**: Threshold enforcement for newcomers vs veterans. (`network_prune_test.go`)
