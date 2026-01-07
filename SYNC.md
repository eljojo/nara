# Unified Sync Backbone

Nara uses a unified event store and gossip protocol to share information across the network. This document explains how naras discover what's happening, form opinions, and stay in sync.

## Mental Model: Waking Up From Holiday

Imagine a nara coming online after being offline for a while. It's like someone returning from vacation:

1. **Says hello publicly** (plaza broadcast) - "hey everyone, I'm back!"
2. **Asks friends privately** (mesh DMs) - "what did I miss? give me the info dump"
3. **Forms own opinion** from gathered data - personality shapes how they interpret events

The nara network is a **collective hazy memory**. No single nara has the complete picture. Events spread organically through gossip. Each nara's understanding of the world (clout scores, network topology, who's reliable) emerges from the events they've collected and how their personality interprets them.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    SyncLedger (Event Store)                     │
│                                                                 │
│  Events: [social, social, ping, social, ping, ...]             │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
                              │
                    ┌─────────┼─────────┐
                    ▼         ▼         ▼
              ┌──────────┐ ┌──────┐ ┌──────────┐
              │ Clout    │ │ RTT  │ │ Future   │
              │Projection│ │Matrix│ │Projection│
              └──────────┘ └──────┘ └──────────┘
```

The `SyncLedger` is the unified event store. It holds all syncable events regardless of type. **Projections** are derived views computed from events - like clout scores (who's respected) or the RTT matrix (network latency map).

## Event Types

### Social Events (`service: "social"`)
Social interactions between naras:
- **tease**: One nara teasing another (for high restarts, comebacks, etc.)
- **observation**: System observations (online/offline, journey events)
- **gossip**: Hearsay about what happened

### Ping Observations (`service: "ping"`)
Network latency measurements:
- **observer**: Who took the measurement
- **target**: Who was measured
- **rtt**: Round-trip time in milliseconds

Ping observations are community-driven. When nara A pings nara B, that measurement spreads through the network. Other naras can use this data to build their own picture of network topology.

## Transport Layer

Events flow through different channels depending on their nature:

### Plaza (MQTT Broadcast)
The public square - everyone sees these messages.
- `nara/plaza/hey_there` - announcing presence
- `nara/plaza/chau` - graceful shutdown
- `nara/plaza/journey_complete` - journey completions

### DMs (Mesh HTTP)
Private point-to-point communication for catching up.
- `POST /sync` - request events from a neighbor
- Used for boot recovery (catching up on missed events)
- More efficient than broadcast for bulk data

### Newspaper (MQTT Per-Nara)
Status broadcasts - current state, not history.
- `nara/newspaper/{name}` - periodic status updates
- Contains current flair, buzz, coordinates, etc.

## Sync Protocol

### Boot Recovery (Getting Up to Speed)

When a nara boots, it wants to catch up on what it missed. Target: **10,000 events**.

```
1. Announce presence on plaza (hey_there)
2. Discover mesh-enabled neighbors
3. Divide 10k target across neighbors:
   - 5 neighbors → each contributes ~2000 events
   - 2 neighbors → each contributes ~5000 events
4. Query each neighbor with interleaved slicing
5. Verify signatures on responses
6. Merge events into SyncLedger
```

### After Boot: Real-Time Watching

Once a nara is online, **no further syncing is needed**. It watches events in real-time via MQTT plaza.

Syncing is only for:
1. **Seeding**: Events from before you ever existed
2. **Catching up**: Events from while you were offline
3. **Spreading opinions**: Share your perspective when others ask

You have your own vantage point - you see events as they happen.

### Interleaved Slicing

To avoid duplicate data when querying multiple neighbors, we use interleaved slicing:

```
Neighbor 0 (slice 0/3): events 0, 3, 6, 9, 12...
Neighbor 1 (slice 1/3): events 1, 4, 7, 10, 13...
Neighbor 2 (slice 2/3): events 2, 5, 8, 11, 14...
```

Each neighbor returns a different slice of their events. Combined, you get comprehensive coverage without redundancy.

## Signed Blocks

Sync responses are cryptographically signed to ensure authenticity:

```go
type SyncResponse struct {
    From      string      `json:"from"`      // Who sent this
    Events    []SyncEvent `json:"events"`    // The events
    Timestamp int64       `json:"ts"`        // When generated
    Signature string      `json:"sig"`       // Ed25519 signature
}
```

**Signing**: The responder hashes `(from + timestamp + events_json)` and signs with their private key.

**Verification**: The receiver looks up the sender's public key (from `Status.PublicKey`) and verifies the signature before merging events.

This prevents:
- Impersonation (can't fake being another nara)
- Tampering (can't modify events in transit)

## Ping Diversity

To prevent the event store from being saturated with stale ping data while keeping useful history, we limit pings to **5 per observer→target pair** (configurable via `MaxPingsPerPair`).

When adding a new ping from A→B:
- If A→B has fewer than 5 entries, add it
- If A→B already has 5 entries, **evict the oldest** and add the new one
- This keeps recent history for trend detection

This keeps the ping data diverse across the network:
- 5 naras = max 100 ping entries (5 per pair × 20 pairs)
- 100 naras = max ~50,000 ping entries
- 5000 naras = bounded by ledger max (50k events) and time-based pruning

## Scale Considerations (5-5000 Naras)

The sync system is designed to scale:

1. **Boot-time sync only**: No ongoing sync overhead after startup
2. **Embrace incompleteness**: No one has all events, and that's OK
3. **Recency over completeness**: Recent events matter more
4. **Diversify sources**: Query multiple peers for different perspectives
5. **Self-throttling**: Ping budget doesn't grow with network size

## Event Flow Example

```
Nara A pings Nara B, measures 42ms RTT
    ↓
A's SyncLedger: [ping: A→B, 42ms]
    ↓
Nara C does mesh sync with A
    ↓
C's SyncLedger: [ping: A→B, 42ms]
    ↓
Nara D does mesh sync with C
    ↓
D's SyncLedger: [ping: A→B, 42ms]
    ...eventually reaches most naras
```

The measurement spreads organically. Different naras may receive it at different times. That's fine - eventual consistency is the goal.

## API Reference

### POST /sync

Request events from a neighbor.

**Request:**
```json
{
  "from": "requester-name",
  "services": ["social", "ping"],  // optional filter
  "subjects": ["nara-a", "nara-b"], // optional filter
  "since_time": 1704067200,        // unix timestamp
  "slice_index": 0,                // for interleaved slicing
  "slice_total": 3,
  "max_events": 2000
}
```

**Response:**
```json
{
  "from": "responder-name",
  "events": [...],
  "ts": 1704067260,
  "sig": "base64-ed25519-signature"
}
```

### GET /ping

Lightweight latency probe for Vivaldi coordinates.

**Response:**
```json
{
  "t": 1704067260,
  "from": "responder-name"
}
```
