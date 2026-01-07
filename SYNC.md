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

### Observation Events (`service: "observation"`)
Network state consensus events - replace newspaper broadcasts for tracking restarts and online status:
- **restart**: Detected a nara restarted (StartTime, restart count)
- **first-seen**: First time observing a nara (seeds StartTime)
- **status-change**: Online/Offline/Missing transition

These events use **importance levels** (1-3) and have anti-abuse protection (per-pair compaction, rate limiting, deduplication). Critical for distributed consensus on network state.

### Social Events (`service: "social"`)
Social interactions between naras:
- **tease**: One nara teasing another (for high restarts, comebacks, etc.)
- **observation**: Legacy system observations (online/offline, journey events)
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

### After Boot: Background Sync (Organic Memory Strengthening)

Once a nara is online, it watches events in real-time via MQTT plaza. However, with personality-based filtering and hazy memory, important events can be missed. **Background sync** helps the collective memory stay strong.

**Schedule:**
- Every ~30 minutes (±5min random jitter)
- Initial random delay (0-5 minutes) to spread startup
- Query 1-2 random online neighbors per sync

**Focus on Important Events:**
```json
{
  "from": "requester",
  "services": ["observation"],  // Observation events only
  "since_time": "<24 hours ago>",
  "max_events": 100,
  "min_importance": 2  // Only Normal and Critical
}
```

This lightweight sync helps catch up on critical observation events (restarts, first-seen) that may have been dropped by other naras' personality filters.

**Network Load (5000 nodes):**
- 250 sync requests/minute network-wide
- ~1 incoming request per nara every 6 minutes
- ~20KB payload per request
- **Total: 83 KB/s** (vs 68MB/s - 1GB/s with old newspaper system)

**Why it's needed:**
1. **Event persistence**: Critical events survive even if some naras drop them
2. **Gradual propagation**: Events spread organically through repeated syncs
3. **Personality compensation**: High-chill naras catch up on events they filtered
4. **Network healing**: Partitioned nodes eventually converge

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

### AvgPingRTT Seeding from Historical Data

When a nara restarts or receives ping observations from neighbors, it **seeds its exponential moving average (AvgPingRTT)** from historical ping data:

1. **On boot recovery:** After syncing events from neighbors, calculate average RTT from recovered ping observations
2. **During background sync:** When receiving ping events from neighbors, recalculate averages for targets with uninitialized AvgPingRTT
3. **Only if uninitialized:** Seeding only happens when `AvgPingRTT == 0` (never overwrites existing values)

This provides **immediate RTT estimates** without waiting for new pings, improving Vivaldi coordinate accuracy and proximity-based routing from the moment a nara comes online.

## Anti-Abuse Mechanisms

The observation event system includes four layers of protection against malicious or misconfigured naras:

### 1. Per-Pair Compaction
**Purpose:** Prevent one hyperactive observer from saturating storage

- Maximum **20 observation events per observer→subject pair**
- Oldest events dropped when limit exceeded
- Example: If alice has 20 observations about bob, adding a 21st evicts the oldest

### 2. Time-Window Rate Limiting
**Purpose:** Block burst flooding attacks

- Maximum **10 events about same subject per 5-minute window**
- Blocks malicious nara claiming restart every second
- Example: After 10 "bob restarted" events in 5 minutes, further events rejected
- Window slides forward automatically

### 3. Content-Based Deduplication
**Purpose:** Prevent redundant storage when multiple observers report same event

- Hash restart events by `(subject, restart_num, start_time)`
- Multiple observers reporting same restart = single stored event
- Keeps earliest observer for attribution
- Example: 10 naras report "lisa restarted (1137)" → stored once

### 4. Importance-Aware Pruning
**Purpose:** Ensure critical events survive longest

- Global ledger pruning respects importance levels:
  1. Drop Casual (importance=1) first
  2. Drop Normal (importance=2) second
  3. Keep Critical (importance=3) longest
- Restart and first-seen events marked Critical
- Survives global MaxEvents pruning

### Combined Protection

At 5000 nodes with 50 abusive naras flooding events:
- **Layer 2** blocks flood after 10 events/5min per subject ✓
- **Layer 1** limits each attacker to 20 events per victim ✓
- **Layer 3** deduplicates coordinated attack ✓
- **Layer 4** preserves critical events under pressure ✓

**Result:** Network remains functional with 1% malicious nodes

## Scale Considerations (5-5000 Naras)

The sync system is designed to scale:

1. **Boot-time sync only**: No ongoing sync overhead after startup
2. **Embrace incompleteness**: No one has all events, and that's OK
3. **Recency over completeness**: Recent events matter more
4. **Diversify sources**: Query multiple peers for different perspectives
5. **Self-throttling**: Ping budget doesn't grow with network size

## Personality-Aware Processing

While the SyncLedger stores events neutrally, **each nara interprets them subjectively** based on personality:

### Filtering on Add
Not all events are meaningful to every nara. When adding social events, personality determines what gets stored:

- **High Chill (>70)**: Ignores random jabs
- **Very High Chill (>85)**: Only keeps significant events (comebacks, high-restarts)
- **High Agreeableness (>80)**: Filters out "trend-abandon" drama
- **Low Sociability (<30)**: Less interested in others' drama

### Clout Calculation
Clout scores are **subjective** - the same events produce different clout for different observers:

```go
clout := ledger.DeriveClout(observerSoul, observerPersonality)
```

The `TeaseResonates()` function uses the observer's soul to deterministically decide if a tease was good or cringe. Same event, different reactions.

### Time Decay
Events fade over time, but personality affects memory:
- **Low Chill**: Holds grudges longer (up to 50% longer half-life)
- **High Chill**: Lets things go faster
- **High Sociability**: Remembers social events longer

## Tease Counter

Separate from subjective clout, the **tease counter** is an objective metric:

```go
counts := ledger.GetTeaseCounts() // map[actor]int
```

This simply counts how many times each nara has teased others. No personality influence - pure numbers. Useful for leaderboards and identifying the most active teasers.

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
