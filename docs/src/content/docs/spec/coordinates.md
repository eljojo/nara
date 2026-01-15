---
title: Coordinates
description: Vivaldi network coordinates and proximity-based influence in Nara.
---

# Coordinates

Coordinates implement the Vivaldi algorithm to estimate peer-to-peer latency without requiring every node to ping every other node. This allows the network to build a scalable "map" of the topology in virtual space.

## Purpose
- Estimate Round-Trip Time (RTT) between any two naras.
- Drive proximity-based social behaviors (e.g., "best friends" are often nearby).
- Optimize routing for [World Postcards](./world-postcards.md).
- Create a visual "map" of the network in the web UI.

## Conceptual Model
- **Virtual Space**: A 2D Euclidean plane where naras "move" closer or further based on latency.
- **Height**: A third coordinate dimension that accounts for "last-mile" latency (e.g., slow home WiFi), helping model non-Euclidean network realities.
- **Coordinate**: A structure containing `X`, `Y`, `Height`, and `Error`.
- **Error**: A confidence score (0.0 to 1.0) indicating how accurately the coordinate reflects actual measured pings.

### Invariants
- **Iterative Refinement**: Coordinates start with high error and stabilize as real pings are observed.
- **Symmetry**: `Distance(A, B)` should be roughly equal to `Distance(B, A)`.
- **Latency Prediction**: `Estimated_RTT = Euclidean_Distance(A, B) + A.Height + B.Height`.

## External Behavior
- **Ping Updates**: Whenever a Nara performs a direct Mesh HTTP `/ping` or receives a `PingObservation` event, it updates its own coordinates.
- **Clout Bonus**: Naras in close proximity to each other give a 30% "influence bonus" to each other's opinions.
- **Barrio Mapping**: Coordinates are used to cluster naras into "Barrios" (see [Aura & Avatar](./aura-and-avatar.md)).

## Interfaces

### NetworkCoordinate Structure
- `x`, `y`: Floating point positions in virtual space.
- `height`: The non-Euclidean component (latency "above" the plane).
- `error`: Confidence value (lower = more confident).

### HTTP API: `GET /coordinates`
Returns the local Nara's current virtual position.

## Algorithms

### 1. Vivaldi Update
When a Nara `me` measures an RTT `r` to `peer`:
1. **Weight**: Calculate `w = me.Error / (me.Error + peer.Error)`.
2. **Relative Error**: Calculate `e_rel = |Predicted_RTT - r| / r`.
3. **Error Update**: `me.Error = (e_rel * Cc * w) + (me.Error * (1 - Cc * w))`.
4. **Coordinate Update**: Move `me.pos` by `Cc * w * (r - Predicted_RTT)` along the unit vector from `peer` to `me`.
5. **Height Update**: Adjust `me.Height` by a smaller factor of the displacement.

### 2. Proximity Bonus
Used in [Clout](./clout.md) and routing:
`Bonus = 100 / max(1, Measured_Distance_in_ms)`
Peers with 1ms latency get a 100x bonus; those with 100ms get a 1x bonus.

## Failure Modes
- **Oscillation**: In unstable networks, coordinates may "bounce" between positions without converging.
- **Triangle Inequality Violation**: High latency on a direct link can cause coordinates to "bend" into impossible Euclidean shapes, which the `Height` dimension attempts to mitigate.
- **Malicious Reporting**: A Nara can report fake coordinates to appear "closer" to high-reputation peers.

## Security / Trust Model
- **Self-Reported**: Coordinates are not cryptographically proven. They are an optimization and visualization tool, not a security boundary.
- **Hearsay Mitigation**: Only direct measurements (`PingObservations`) contribute to coordinate updates.

## Test Oracle
- `TestVivaldi_UpdateConvergence`: Verifies that coordinates stabilize towards a constant measured RTT.
- `TestVivaldi_DistanceCalculation`: Ensures `DistanceTo` correctly incorporates height and Euclidean position.
- `TestProximity_Influence`: Checks that clout scores are correctly boosted for nearby neighbors.
