# Coordinates

## Purpose

Network coordinates estimate latency between naras without requiring direct ping measurements.
They allow the network to discover topology (who is close to whom) scalably.

## Conceptual Model

- Based on the **Vivaldi** algorithm.
- Nodes exist in a 2D Euclidean space + a "Height" component (modeling non-Euclidean access links).
- Latency `RTT = Distance(A, B)`.

Key invariants:
- Coordinates update iteratively based on real ping measurements.
- Coordinates predict latency between nodes that have never communicated.
- Coordinates influence routing (e.g., world journey next-hop selection) and clout (proximity bias).

## External Behavior

- Each nara maintains its own `NetworkCoordinate`.
- Coordinates are piggybacked on `HeyThere` and `Newspaper` messages (not `SyncEvent`s directly, but part of `NaraStatus`).
- Nodes ping random peers periodically to refine their coordinates.

## Interfaces

### Data Structure (`NetworkCoordinate`)
```go
type NetworkCoordinate struct {
    X      float64 `json:"x"`
    Y      float64 `json:"y"`
    Height float64 `json:"height"` // Models 'last mile' latency
    Error  float64 `json:"error"`  // Confidence (lower is better)
}
```

### Methods
- `DistanceTo(other)`: Returns estimated RTT in milliseconds.
- `Update(peer, rtt)`: Adjusts local coordinates based on measured RTT.

### Usage in Other Systems
- **Clout**: `ApplyProximityToClout` boosts clout for nearby peers.
- **Routing**: `ProximityBonus` prefers nearby peers for world journey hops.

## Event Types & Schemas

Coordinates are not a distinct event type but are embedded in:
- `NaraStatus` (used in `NewspaperEvent` and `HowdyEvent`).

Ping events (`svc: ping`) are the inputs:
- `PingObservation`: `{Observer, Target, RTT}`.

## Algorithms

### Initialization
- Start at a small random position near origin (prevents singularity).
- High initial error (1.0).

### Update Rule (Vivaldi)
When a ping measurement `rtt` arrives from `peer`:
1. **Prediction**: `pred = Distance(me, peer)`.
2. **Error**: `e = rtt - pred`.
3. **Weight**: `w = me.Error / (me.Error + peer.Error)` (trust peer if they are more confident).
4. **Move**: Nudge position by `Cc * w * e` along the unit vector towards/away from peer.
5. **Update Error**: `me.Error` updated using weighted moving average of `|e|`.

Defaults:
- `Cc` (Coordinate Step): 0.25
- `Ce` (Error Step): 0.25

### Distance Calculation
```go
func Distance(a, b) float64 {
    euclidean = Sqrt((a.x - b.x)^2 + (a.y - b.y)^2)
    return euclidean + a.height + b.height
}
```

### Proximity Influence on Clout
Nearby peers have more influence on opinion.
```go
ProximityWeight = Exp(-Distance / 100ms)
ResultClout = (BaseClout * 0.7) + (BaseClout * ProximityWeight * 0.3)
```

## Failure Modes

- **High Error**: Nodes with unstable links or few peers will have high error estimates.
- **Drift**: Without anchors, the entire coordinate system can drift (translation/rotation), but relative distances remain valid.
- **Triangle Inequality**: Internet routing often violates Euclidean rules; the `Height` component absorbs this error.

## Security / Trust Model

- **No Verification**: Coordinates are self-reported. A malicious node could lie about its position to attract or repel traffic.
- **Impact Limiting**: Bad coordinates mainly affect optimization (latency estimation), not correctness (connectivity).

## Test Oracle

- **Convergence**: Coordinates converge to accurate latency estimates over time. (`vivaldi_test.go`)
- **Distance**: `DistanceTo` returns positive values. (`vivaldi_test.go`)
- **Proximity**: Clout adjustment boosts nearby peers. (`vivaldi_test.go`)

## Open Questions / TODO

- **Anchors**: No fixed anchors are currently used; the system floats.
