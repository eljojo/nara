---
title: Coordinates
---

# Coordinates

Network coordinates estimate peer-to-peer latency using the Vivaldi algorithm, enabling scalable topology discovery without exhaustive pinging.

## Conceptual Model

| Concept | Rule |
| :--- | :--- |
| **Model** | 2D Euclidean Space + "Height" component (link latency). |
| **Prediction** | `Estimated_RTT = Distance(A, B)`. |
| **Iterative** | Coordinates refine locally as real ping measurements (`svc: ping`) arrive. |
| **Bias** | Nearby peers get a "Proximity Bonus" in clout and routing decisions. |

## Algorithm: Vivaldi Update

When a ping measurement `rtt` arrives from `peer`:
1. **Predict**: `dist = sqrt((me.x - peer.x)^2 + (me.y - peer.y)^2) + me.h + peer.h`.
2. **Error**: `e = rtt - dist`.
3. **Weight**: `w = me.Error / (me.Error + peer.Error)` (Trust peers with lower error).
4. **Nudge**: Move position by `0.25 * w * e` along the unit vector relative to peer.
5. **Update Error**: Weighted moving average of `|e| / rtt`.

## Interfaces

### Data Structure (`NetworkCoordinate`)
```json
{
    "x": 1.23, "y": -4.56,
    "height": 10.0,
    "error": 0.15
}
```

### Usage
- **Clout**: `Clout = (Base * 0.7) + (Base * exp(-Dist/100ms) * 0.3)`.
- **World Journeys**: Next-hop selection prefers low predicted latency.
- **Presence**: Embedded in `NaraStatus` (broadcast via Newspaper).

## Constraints
- **Initialization**: Small random offset near (0,0); `error = 1.0`.
- **Non-Euclidean**: The `Height` component absorbs internet routing violations of the triangle inequality.

## Security
- **Self-Reported**: Nodes report their own coordinates; malicious nodes can lie about position.
- **Low Impact**: Malformed coordinates affect optimization (latency estimation) but not correctness or connectivity.

## Test Oracle
- **Convergence**: Verify coordinate stabilization towards measured RTTs. (`vivaldi_test.go`)
- **Distance**: `DistanceTo` remains positive and symmetric. (`vivaldi_test.go`)
- **Influence**: Proximity bias correctly modifies Clout. (`vivaldi_test.go`)
