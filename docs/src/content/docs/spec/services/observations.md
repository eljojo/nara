---
title: Observations
description: Monitoring uptime, restarts, and network state consensus in the nara network.
---

Observations allow naras to monitor and agree on peer state (restarts, first-seen, status changes) to derive a collective "opinion" without a central registry.

## 1. Core Objectives
- Track the **Trinity**: `StartTime`, `Restarts`, and `TotalUptime` for every nara in the network, culminating in multi-party [checkpoints](/docs/spec/services/checkpoints/).
- Provide inputs for [social interactions](/docs/spec/services/social/) (e.g., teasing based on high restart counts).

- Maintain historical continuity across node failures.

## 2. Conceptual Model
- **Observation**: A signed claim by an **Observer** about a **Subject**.
- **The Trinity**:
    - **`StartTime`**: Unix timestamp (seconds) of the subject's first appearance.
    - **`Restarts`**: Cumulative restart count.
    - **`TotalUptime`**: Total verified seconds the subject has been online.
- **Opinion**: The locally derived consensus value for a peer's Trinity, computed by looking at all available observations in the ledger.
- **Ghost Nara**: A nara for which we have an entry but no meaningful observation data; these are eventually garbage collected.

### Invariants
1. **Recency**: The latest timestamped observation is authoritative for current `ONLINE`/`OFFLINE` status.
2. **Deduplication**: Content-based deduplication ensures that multiple observers reporting the same restart count only once toward the total.
3. **Observation Persistence**: Restart and first-seen events are never pruned until they are anchored in a multi-sig checkpoint.
4. **Consensus Tolerance**: A 60-second window is used to account for clock drift when comparing `StartTime` observations.

## 3. External Behavior
- Naras emit observations when they detect a peer's state change (e.g., coming online after being missing).
- The system periodically runs "maintenance" to update opinions and prune stale or malformed records.
- Before marking a nara as `MISSING`, an observer MUST attempt a direct ping to verify unreachability.
- Observations spread through the mesh via zines and historical sync.

## 4. Interfaces
- `DeriveOpinion(subject)`: Function to compute consensus Trinity values from the ledger.
- `Maintenance()`: Periodic task that updates local opinions and prunes the neighbourhood.
- `Blue Jay`: An optional initialization step that fetches a baseline of opinions from `https://nara.network/narae.json`.

## 5. Event Types & Schemas
### `observation` (SyncEvent Payload)
- `type`: `restart`, `first-seen`, `status-change`.
- `importance`: 1 (Casual), 2 (Normal), 3 (Critical).
- `online_state`: `ONLINE`, `OFFLINE`, `MISSING`.
- `start_time`: Unix timestamp (seconds).
- `restart_num`: Cumulative count.
- `observer_uptime`: The observer's own uptime (used for weighting).

## 6. Algorithms

### Opinion Consensus (`DeriveOpinion`)
1. **StartTime**: Computed as the **Trimmed Mean** of all reported values in the ledger.
2. **Restarts**: Highest `RestartNum` from a reliable observer + count of unique `StartTime`s in subsequent restart events.
3. **TotalUptime**: Calculated as `BaseUptime` (from latest checkpoint) + sum of intervals between `ONLINE` and `OFFLINE`/`MISSING` events since that checkpoint.

### Trimmed Mean Positive
Used to filter out outliers (e.g., buggy clocks or byzantine reports):
1. Filter out non-positive values.
2. Calculate the median.
3. Keep only values within a range of 0.2x to 5.0x of the median.
4. Return the average of the remaining values.

### Restart Detection
1. Nara detects a heartbeat from a subject previously marked `MISSING` or `OFFLINE`.
2. Observer increments local `Restarts` count for that subject.
3. Observer waits for a random jitter delay (0-5s).
4. Observer checks the ledger: if no "restart" event for this count exists, it emits a new observation.

### Tiered Neighbourhood Pruning
- **Newcomers** (< 2 days old): Pruned after 24h of being offline.
- **Established** (2-30 days old): Pruned after 7 days of being offline.
- **Veterans** (30+ days old): Never auto-pruned.
- **Zombies**: Immediately pruned if malformed (e.g., `StartTime` > 1h ago but `LastSeen` is empty).

## 7. Failure Modes
- **Byzantine Observers**: False reporting of restarts or uptimes is mitigated by the trimmed mean and verification pings.
- **Ledger Gaps**: If a node misses social gossip or sync cycles, its `TotalUptime` calculation will be lower than reality (subjective truth).
- **Clock Drift**: Significant drift may cause multiple unique `StartTime` values to be recorded for the same restart.

## 8. Security / Trust Model
- **Consensus Weighting**: Observations from naras with higher uptime carry more weight in certain derivations.
- **Anchoring**: Multi-party signed checkpoints serve as the definitive "history" that overrides individual observations.

## 9. Test Oracle
- `TestOpinionConsensus`: Verifies Trinity derivation from a set of mock sync events.
- `TestGhostGarbageCollection`: Ensures "ghost" entries are correctly identified and purged.
- `TestRestartDeduplication`: Confirms that multiple reports of the same restart (same subject/num/start_time) don't inflate the count.
- `TestVerificationPing`: Ensures `MISSING` state is only reached after a failed ping.

## 10. Open Questions / TODO
- Unify `ObservationEventPayload` with the `NaraObservation` struct to reduce duplication.
- Port the importance-based filtering to the new runtime's `ImportanceFilterStage`.
