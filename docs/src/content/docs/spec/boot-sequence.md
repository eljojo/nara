---
title: Boot Sequence
description: Transition from startup to steady-state in the Nara Network.
---

# Boot Sequence

The Boot Sequence orchestrates a Nara's transition from startup to full network participation, prioritizing identity and historical reconciliation (Sync).

## 1. Purpose
- Resolve cryptographic identity (Soul, ID, Keypair).
- Establish Mesh (WireGuard) and Plaza (MQTT) connectivity.
- Reconcile "hazy memory" via peer sync and checkpoints.
- Suppress social/opinion loops until a baseline is reached.

## 2. Conceptual Model
- **Gating**: `bootRecoveryDone` channel blocks social behaviors/opinions during sync.
- **Phases**: Sequential progression from Identity to Steady State.

### Invariants
- **Identity First**: No communication before Soul/ID resolution.
- **Mesh Dependency**: Mesh IP must be known before initial `hey_there`.
- **Silent Boot**: Social teases and logging suppressed during recovery.

## 3. Timeline
```mermaid
sequenceDiagram
    participant Nara
    participant Network
    Note over Nara: 1. Identity Resolution
    Nara->>Nara: Resolve Soul/ID & Derive Keys
    Note over Nara: 2. Transport Initialization
    Nara->>Network: Connect Mesh & Plaza
    Note over Nara: 3. Discovery
    Nara->>Network: Broadcast hey_there
    Network-->>Nara: howdy responses
    Note over Nara: 4. Boot Recovery (Gated)
    Nara->>Network: Sync Events & Checkpoints
    Nara->>Nara: Merge, Verify, & Signal bootRecoveryDone
    Note over Nara: 5. Opinion Formation
    Nara->>Nara: Derive Trinity & Prune Ghosts
    Note over Nara: 6. Steady State
```

## 4. Algorithms

### Boot Recovery (`bootRecovery`)
1. **Discovery**: Wait ≤ 30s for initial peers.
2. **Parallel Sync**: Fetch events from mesh neighbors.
   - **Sample Mode**: Distributed subset retrieval.
   - **Capacity**: 5k (Short) to 80k (Hog) events.
3. **Anchor Sync**: Fetch [Checkpoints](./checkpoints.md) from ≤ 5 neighbors.
4. **Completion**: Close `bootRecoveryDone`.

### Initial Opinion Pass
Post-recovery `formOpinion`:
1. **Trinity**: Calculate `StartTime`, `Restarts`, `TotalUptime` for all known peers.
2. **Liveness**: Attempt verification pings for quiet nodes.
3. **Metrics**: Seed `AvgPingRTT` from recovered history.

## 5. Failure Modes
- **Isolation**: No neighbors in 30s; starts with empty ledger.
- **Sync Fallback**: Mesh failure forces slower MQTT sync.
- **Identity Conflict**: Clashing name/soul rejected by peers.

## 6. Security
- **Auth**: Mesh auth enabled immediately.
- **Verification**: Historical integrity verified via peer signatures.

## 7. Test Oracle
- `TestBootRecovery_Gating`: No opinions before recovery signal.
- `TestBootRecovery_ParallelSync`: Request distribution across neighbors.
- `TestBootRecovery_CheckpointBaseline`: Trinity anchoring.
