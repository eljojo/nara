---
title: Boot Sequence
description: Orchestrating the transition from startup to steady-state in the Nara Network.
---

# Boot Sequence

The Boot Sequence orchestrates a Nara's transition from an empty process to a fully integrated participant in the network. It prioritizes identity resolution and historical reconciliation (Sync) before activating social and observation loops.

## Purpose
- Resolve the Nara's cryptographic identity (Soul, ID, Keypair).
- Establish connectivity via Mesh (WireGuard) and Plaza (MQTT).
- Reconcile "hazy memory" by fetching historical events and checkpoints from peers.
- Suppress social triggers and "opinions" until a consistent baseline is reached.

## Conceptual Model
- **Phases**: A series of ordered steps (Identity → Transport → Recovery → Steady State).
- **Gating**: The `bootRecoveryDone` channel prevents social behaviors from firing until history is synced.
- **Recovery Target**: The Nara attempts to fill its ledger capacity based on its [Memory Model](./memory-model.md).

### Invariants
- **Identity First**: No network communication occurs until the Soul and Nara ID are resolved.
- **Mesh Before MQTT**: The Mesh IP must be known before the initial `hey_there` broadcast so peers know how to reach the node.
- **Silent Boot**: Event logging and social teases are suppressed during the recovery phase to avoid spamming the network with "stale" observations.

## External Behavior
- **Join Announcement**: Peers observe a `hey_there` message followed by a spike in sync requests from the new node.
- **Opinion Formation**: A Nara's reported metrics (Restarts, Uptime) may fluctuate during boot as it merges divergent views from peers, eventually converging.

## Interfaces
- `ln.Start()` / `network.Start()`: The primary entry points for the boot sequence.
- `bootRecoveryDone`: A coordination channel used to unblock dependent services like `formOpinion`.

## Algorithms

### 1. Boot Recovery (`bootRecovery`)
1. **Wait for Neighbors**: Wait up to 30s (or 3 retries) for initial peer discovery via MQTT or Mesh.
2. **Parallel Sync**: 
   - Identify all online, mesh-enabled neighbors.
   - Use **Sample Mode** (preferred) or **Legacy Slicing** to fetch events in parallel.
   - **Target**: Attempt to fetch up to 50,000 events (adjusted by memory mode).
3. **Checkpoint Sync**: Explicitly fetch verified [Checkpoints](./checkpoints.md) to anchor historical stats.
4. **Signal Completion**: Close the `bootRecoveryDone` channel.

### 2. Opinion Formation Pass
Once recovery is complete, the Nara runs 6-30 "opinion passes":
1. **Seed Opinions**: Optionally fetch baseline data from Blue Jay (nara.network).
2. **Derive Trinity**: Calculate `StartTime`, `Restarts`, and `TotalUptime` from the merged ledger.
3. **Ghost GC**: Prune "ghost" naras that have no meaningful data after sync.

### 3. Steady-State Activation
Long-running background loops are started:
- **Gossip**: Hand-to-hand zine exchange (30-300s).
- **Background Sync**: Periodic "recent" mode catch-up (30m).
- **Maintenance**: Observations (1s), Trends (30s), Buzz (10s).
- **Vivaldi**: Network coordinate updates.

## Failure Modes
- **Isolaton**: If no neighbors respond during the 30s window, the Nara starts with an empty ledger and must rely on slow background sync to recover history.
- **Sync Failure**: If mesh sync fails, the Nara falls back to MQTT-based ledger requests, which are slower and less reliable.
- **Identity Clash**: If a Nara boots with a soul that clashes with an existing name, it may be rejected by peers.

## Security / Trust Model
- **Mesh Auth**: Initialized immediately to ensure all P2P sync traffic is authenticated.
- **Attestation**: During boot, the Nara relies on the signatures of peers to verify the historical events it receives.

## Test Oracle
- `TestBootRecovery_Gating`: Ensures that `formOpinion` does not run until the recovery signal is sent.
- `TestBootRecovery_ParallelSync`: Verifies that sync requests are correctly distributed across multiple neighbors.
- `TestBootRecovery_CheckpointBaseline`: Confirms that checkpoints correctly anchor the initial Trinity derivation.
