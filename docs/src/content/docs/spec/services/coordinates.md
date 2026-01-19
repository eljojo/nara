---
title: Network Coordinates
description: Vivaldi network coordinates and proximity-based influence in the nara network.
---

Coordinates use the Vivaldi algorithm to estimate peer-to-peer latency, creating a scalable "map" of network topology in virtual space. This allows naras to understand their proximity to others without needing to ping every node in the network.

## 1. Purpose
- Estimate Round-Trip Time (RTT) between naras without exhaustive full-mesh measurements.
- Drive proximity-based social behaviors, such as "best friends" and local influence bonuses.
- Optimize World Postcard routing by picking peers that are "nearby" in virtual space.
- Enable network visualization (Radar) in the Web UI.

## 2. Conceptual Model
- **Virtual Space**: A 2D Euclidean plane where naras "move" based on observed latency.
- **Height**: A third dimension used to model "last-mile" latency that doesn't fit in the Euclidean plane.
- **Confidence (Error)**: A score (0.0 to 1.0) indicating how accurately the current coordinates reflect actual measured latencies.

### Invariants
1. **Iterative Refinement**: Coordinates MUST stabilize and converge over time as more measurements are processed.
2. **Prediction**: `Estimated_RTT = Euclidean_Distance(A, B) + A.Height + B.Height`.
3. **Subjective Convergence**: Every nara has its own view of its position in the network based on its specific set of interactions.

## 3. External Behavior
- Coordinates are automatically updated whenever a nara performs a Mesh HTTP `/ping` or receives a `PingObservation` event.
- Peers with low latency (nearby in virtual space) receive a "Proximity Bonus" that increases their Clout and influence on opinions.
- naras use their coordinates to determine their "Barrio" (neighborhood) visual trait.

## 4. Interfaces
### NetworkCoordinate Structure
- `x`: X-position in virtual space.
- `y`: Y-position in virtual space.
- `height`: Non-Euclidean latency factor.
- `error`: Confidence score.

### API / Transports
- Coordinates are shared via the `hey-there` presence event and the periodic `Newspaper` status update.

## 5. Event Types & Schemas
- `hey-there`: Includes the nara's current coordinates.
- `ping`: Trigger for Vivaldi updates.

## 6. Algorithms

### Vivaldi Update Logic
Whenever a nara measures a real RTT `r` to a peer:
1. **Weight Computation**: `w = local.Error / (local.Error + peer.Error)`.
2. **Relative Error**: `e_rel = |Predicted_RTT - r| / r`.
3. **Update Confidence**: `local.Error = (e_rel * Cc * w) + (local.Error * (1 - Cc * w))`.
4. **Update Position**: Shift `local.pos` along the unit vector from the peer toward the local nara by an amount proportional to the difference between measured and predicted RTT.
5. **Adjust Height**: Update the height factor to account for residual non-Euclidean latency.

### Proximity-Based Clout
The subjective influence of a peer is amplified if they are nearby:
- `CloutBonus = 1.3x` for peers with `< 50ms` estimated RTT.
- This represents "neighbors" who are more likely to have relevant observations about the same segment of the network.

## 7. Failure Modes
- **Oscillation**: In networks with highly volatile latency, naras may "bounce" around virtual space without ever converging.
- **Triangle Inequality Violations**: Non-Euclidean network paths can cause "warping" in virtual space, which the `height` factor attempts to mitigate.
- **Mismatched Dimensions**: If different nodes use different coordinate dimensions (e.g., 2D vs 3D), estimated latencies will be incorrect.

## 8. Security / Trust Model
- **Self-Reported**: Coordinates are currently self-reported and are not cryptographically verified beyond the signature of the message carrying them.
- **Mitigation**: The Vivaldi algorithm's error weighting naturally reduces the influence of reports from nodes with high internal error scores.

## 9. Test Oracle
- `TestVivaldi_UpdateConvergence`: Verifies that coordinates stabilize when presented with consistent RTT measurements.
- `TestVivaldi_DistanceCalculation`: Validates that the estimated RTT correctly combines Euclidean distance and height factors.
- `TestProximity_Influence`: Confirms that "nearby" peers have a higher weight in opinion consensus.

## 10. Open Questions / TODO
- Implement "Anchor Nodes" with fixed coordinates to provide a stable reference frame for the network map.
- Explore using coordinates to optimize Zine gossip target selection.
