---
title: Coordinates
description: Vivaldi network coordinates and proximity-based influence in Nara.
---

# Coordinates

Coordinates use the Vivaldi algorithm to estimate P2P latency, creating a scalable "map" of network topology in virtual space.

## 1. Purpose
- Estimate RTT between naras without full-mesh pinging.
- Drive proximity-based social behaviors and "best friends."
- Optimize [World Postcard](./world-postcards.md) routing.
- Network visualization in the web UI.

## 2. Conceptual Model
- **Virtual Space**: 2D Euclidean plane where naras "move" based on latency.
- **Height**: A third dimension for "last-mile" (non-Euclidean) latency.
- **Confidence**: `Error` score (0.0 to 1.0) indicating coordinate accuracy.

### Invariants
- **Refinement**: Coordinates stabilize over time as real pings are observed.
- **Prediction**: `Estimated_RTT = Euclidean_Distance(A, B) + A.Height + B.Height`.

## 3. External Behavior
- **Automatic Updates**: Triggered by Mesh HTTP `/ping` or `PingObservation` events.
- **Influence Bonus**: 30% boost to opinions for nearby naras.
- **Barrios**: Latency clustering into neighborhoods (see [Aura & Avatar](./aura-and-avatar.md)).

## 4. Interfaces

### NetworkCoordinate Structure
- `x`, `y`, `height`, `error`.

### HTTP API
- `GET /coordinates`: Returns local virtual position.

## 5. Algorithms

### Vivaldi Update
Triggered when measuring RTT `r` to `peer`:
1. **Weight**: `w = me.Error / (me.Error + peer.Error)`.
2. **Relative Error**: `e_rel = |Predicted_RTT - r| / r`.
3. **Error Update**: `me.Error = (e_rel * Cc * w) + (me.Error * (1 - Cc * w))`.
4. **Position**: Move `me.pos` along the unit vector from `peer` to `me` based on `(r - Predicted_RTT)`.
5. **Height**: Adjust `me.Height` by displacement factor.

### Proximity Bonus
`Bonus = 100 / max(1, Measured_Distance_in_ms)`.

## 6. Failure Modes
- **Oscillation**: Unstable networks may prevent convergence.
- **Triangle Inequality Violation**: High-latency direct links "bend" virtual space.
- **Spoofing**: Coordinates are self-reported and not cryptographically verified.

## 7. Test Oracle
- `TestVivaldi_UpdateConvergence`: Stability checks.
- `TestVivaldi_DistanceCalculation`: Euclidean + height verification.
- `TestProximity_Influence`: Clout boost validation.
