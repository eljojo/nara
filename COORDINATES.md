# Network Coordinate System

Naras estimate their relative positions in "network space" using latency measurements. This enables visualization, proximity-based social behavior, and optimized journey routing.

## How It Works

### Vivaldi Coordinates

Each nara maintains a position in virtual 2D space plus a "height" component:

```
NetworkCoordinate {
    X, Y    float64  // Position in 2D space
    Height  float64  // Handles asymmetric latency (non-Euclidean reality)
    Error   float64  // Confidence (lower = more certain of position)
}
```

The **predicted latency** between two naras is:
```
distance = sqrt((x1-x2)² + (y1-y2)²) + height1 + height2
```

### Coordinate Updates

When a nara measures RTT to a peer:

1. **Predict** latency from current coordinates
2. **Measure** actual RTT via mesh ping
3. **Calculate error** = measured - predicted
4. **Weight update** by relative confidence: `w = myError / (myError + peerError)`
5. **Move** toward/away from peer proportional to error
6. **Update height** to absorb asymmetric delays
7. **Decay error** estimate (become more confident)

Over time, coordinates converge to reflect actual network topology.

## Latency Measurement

### Dedicated Ping Endpoint

```
GET /ping
Response: {"t": <server_nanoseconds>, "from": "<nara_name>"}
```

Naras measure RTT by timing the round-trip of this request.

### Opportunistic Measurement

RTT is also captured during:
- World journey message forwarding
- Event sync during boot recovery
- Any mesh HTTP communication

This provides "free" measurements without extra traffic.

## Self-Throttling Strategy

With hundreds of naras, we can't ping everyone frequently.

**Fixed budget**: Max 5 pings per minute, regardless of network size.

| Network Size | Ping Frequency per Peer |
|--------------|------------------------|
| 10 naras     | ~every 2 minutes       |
| 100 naras    | ~every 20 minutes      |
| 500 naras    | ~every 100 minutes     |

**Priority selection**: When choosing who to ping:
1. New naras (never pinged before)
2. High-error naras (uncertain positions)
3. Stale measurements (haven't pinged recently)
4. Random sampling for coverage

This ensures coordinates converge while respecting network resources.

## Usage

### Journey Routing

World journeys prefer nearby naras for faster completion:

```
score = clout + proximityBonus
proximityBonus = 100 / max(1, distance_ms)
```

Two naras with similar clout? The closer one wins.

### Social Influence

Nearby naras have stronger influence on opinions:

```
proximityWeight = e^(-distance / 100)
finalClout = 0.7 * baseClout + 0.3 * baseClout * proximityWeight
```

- Naras < 50ms away: ~40% stronger influence
- Naras > 200ms away: ~55% weaker influence

### Visualization

The `/network/map` endpoint returns all known coordinates:

```json
{
  "nodes": [
    {
      "name": "blue-jay",
      "coordinates": {"x": 0.5, "y": -0.3, "height": 0.01, "error": 0.1},
      "online": true,
      "rtt_to_us": 45.2
    }
  ],
  "server": "cardinal"
}
```

The frontend renders this as a force-directed graph where:
- Node position reflects network coordinates
- Node size reflects buzz level
- Node color reflects online status
- Edges show connections with latency

## Cold Start

When a nara first boots:
1. Coordinates start at random position with high error (1.0)
2. First few pings cause large coordinate adjustments
3. Error decreases as measurements accumulate
4. Typically stabilizes within 5-10 minutes

## API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/ping` | GET | Latency probe (returns timestamp) |
| `/coordinates` | GET | This nara's current coordinates |
| `/network/map` | GET | All known nodes with coordinates |

## Algorithm Constants

```go
Ce           = 0.25   // Error update constant
Cc           = 0.25   // Coordinate update constant
MinHeight    = 1e-5   // Minimum height value
InitialError = 1.0    // Starting error for new nodes
```

These are conservative values that prioritize stability over rapid convergence.
