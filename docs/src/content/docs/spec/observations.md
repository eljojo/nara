---
title: Observations
description: Monitoring uptime, restarts, and network state consensus in the Nara Network.
---

# Observations

Observations are the mechanism by which naras monitor and agree upon the state of their peers. They record discrete events like restarts, first-seen times, and status changes, which are then used to derive a collective "opinion" about the network.
## 1. Purpose
- Track the "Trinity" of network identity: `StartTime`, `Restarts`, and `TotalUptime`.
- Reach a decentralized consensus on peer state without a central heartbeat registry.
- Provide inputs for social interactions (e.g., teasing about high restarts).
- Enable robust historical tracking that survives individual node failures.
## 2. Conceptual Model
- **Observation Event**: A `SyncEvent` containing a claim by an **Observer** about a **Subject**.
- **Trinity**: The core metrics:
- **Observation Event**: A `SyncEvent` containing a claim by an **Observer** about a **Subject**.
- **Trinity**: The core metrics:
    - **`StartTime`**: The Unix timestamp of the Nara's first-ever appearance (ideally).
    - **`Restarts`**: The cumulative count of times the Nara has restarted.
    - **`TotalUptime`**: The total number of verified seconds the Nara has been online.
- **Opinion**: A Nara's subjective calculation of a peer's Trinity based on its local ledger.
- **Ghost Nara**: A Nara seen briefly but never again, with no meaningful data from any neighbor; these are eventually garbage collected.

### Invariants
- **Most Recent Wins**: For online status, the event with the latest timestamp is considered authoritative.
- **Deduplication**: Multiple observers reporting the same restart are deduplicated to avoid count inflation.
- **Consensus Tolerance**: Small differences in reported timestamps (up to 60s) are ignored to account for clock drift.
- **Consensus Tolerance**: Small differences in reported timestamps (up to 60s) are ignored to account for clock drift.
- **Critical History**: Restart and first-seen events are prioritized and never pruned until they are safely anchored in a checkpoint.
## 3. External Behavior
- **Maintenance**: Naras periodically run observation maintenance to update their opinions and prune dead/ghost naras.
- **Verification**: Before marking a Nara as `MISSING`, an observer may attempt a direct verification ping to ensure the "silence" isn't due to a local network issue.
- **Gossip**: Observations spread through zines and sync, allowing a Nara to form opinions about peers it hasn't seen directly.
## 4. Interfaces
### Observation Event (SyncEvent Payload)
- `type`: "restart", "first-seen", or "status-change".
### Observation Event (SyncEvent Payload)
- `type`: "restart", "first-seen", or "status-change".
- `importance`: 1 (Casual), 2 (Normal), 3 (Critical).
- `online_state`: "ONLINE", "OFFLINE", or "MISSING".
- `start_time` / `restart_num`: Metrics at the time of observation.
- `observer_uptime`: The uptime of the reporter (used for weighting).

### External Source: Blue Jay
Naras may optionally fetch a "baseline" of opinions from `https://nara.network/narae.json` during boot to seed their initial world view.

## Algorithms

Naras may optionally fetch a "baseline" of opinions from `https://nara.network/narae.json` during boot to seed their initial world view.
## 5. Opinion Trinity
The "Opinion" about a Nara consists of:
- **StartTime**: Earliest known appearance.
- **Restarts**: Cumulative restart count.
- **LastRestart**: Timestamp of the most recent restart.
- **TotalUptime**: Verified seconds online.
## 6. Algorithms

### Opinion Consensus (`DeriveOpinion`)
To calculate a subject's state:
1. **StartTime**: Collect all reported `StartTime` values and calculate a **Trimmed Mean** (removing outliers).
2. **Restarts**:
    - Use the highest `RestartNum` from a reliable observer as a baseline.
    - Add the count of **Unique StartTimes** seen in restart events after that baseline.
3. **TotalUptime**:
    - Start with the `TotalUptime` from the most recent **Checkpoint**.
    - Add the sum of intervals between `ONLINE` and `OFFLINE`/`MISSING` events observed since that checkpoint.
### Trimmed Mean Positive
Used for StartTime and Restarts to remove malicious or buggy outliers.
1. Filter out zeros and negative values.
2. Calculate the median.
3. Keep values within a range (e.g., 0.2x to 5.0x of median).
4. Return the average of remaining values.

### Restart Detection
If a Nara that was previously `MISSING` or `OFFLINE` appears with a new heartbeat:
1. Increment local `Restarts` count.
2. Wait a random jitter delay (0-5s).
3. Check the ledger for a recent restart event for this subject.
4. If none, emit a new `restart` observation.
### Tiered Pruning
Naras are removed from memory (neighbourhood) if they are not currently **ONLINE** and meet these criteria:
- **Newcomers** (< 2 days old): Pruned after 24h offline.
- **Established** (2-30 days old): Pruned after 7 days offline.
- **Veterans** (30+ days old): Never auto-pruned.

**Zombies** are malformed entries pruned immediately if:
- Never seen but `StartTime` > 1 hour ago.
- No `LastSeen`, no `StartTime`, and no activity (no Flair, Buzz, or Chattiness).
## 7. Failure Modes
- **Byzantine Observers**: A malicious Nara could report false restarts or status changes. The consensus algorithm (trimmed mean) and verification pings mitigate this.
5. At least 3 neighbors have been checked and agree they have no data.
## 7. Failure Modes
- **Byzantine Observers**: A malicious Nara could report false restarts or status changes. The consensus algorithm (trimmed mean) and verification pings mitigate this.
- **Divergent History**: If a Nara misses many gossip cycles, its derived `TotalUptime` will be lower than reality.
- **Clock Drift**: Significant skew between observers can lead to multiple "unique" StartTimes being counted for the same restart.
## 8. Security / Trust Model
- **Weighting**: Observations from naras with higher uptime are trusted more in consensus calculations.
- **Self-Correction**: Checkpoints (see [Checkpoints](./checkpoints.md)) provide a multi-sig anchor to resolve long-term opinion divergence.
## 9. Neighbourhood Queries
Naras provide public interfaces to query the state of the network:
- **Oldest/Youngest Nara**: Identifies peers by `StartTime` (global or per-barrio).
- **Most Restarts**: Identifies the most "resilient" (or unstable) peers.
- **Online Names**: Returns a list of currently active peers.

## 9. Test Oracle
- `TestOpinionConsensus`: Verifies that Trinity derivation matches expected values from a set of mock events.
- `TestGhostGarbageCollection`: Ensures that only truly "dead" naras are purged from memory.
- `TestRestartDeduplication`: Checks that multiple reports of the same restart don't double-count.
- `TestVerificationPing`: Validates the "ping-before-missing" logic.
- `TestOpinionMethodDivergence`: Compares observation-based vs checkpoint-based derivation and warns on mismatch.
