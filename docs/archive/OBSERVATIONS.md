---
title: Observations
slug: concepts/observations
---

# Event-Driven Observation System

Naras form consensus about network state through distributed observation events rather than centralized broadcast.

## Philosophy

Instead of every nara broadcasting their complete view of all 5000 naras (750KB+ per broadcast), naras emit **events** when they observe changes:

- "I saw nara-x restart" (restart event)
- "I saw nara-y come online" (status-change event)
- "I'm seeing nara-z for the first time" (first-seen event)

These events spread organically through the network via:
1. **Real-time MQTT broadcasting** (for immediate awareness)
2. **Background mesh syncing** (for catching up)
3. **Boot recovery** (for new/restarting naras)

## Consensus Formation

Each nara collects observation events from neighbors and forms their own opinion using **trimmed mean**:

- Collect all reported values from observers
- Remove statistical outliers (values >5× or <0.2× median)
- Average the remaining values (naturally weighted by how many observers agree)
- Single observer fallback: use their latest value if only one reporting

This is **subjective consensus** - each nara may have slightly different views, and that's OK. It's a hazy collective memory.

## Event Lifecycle

```
Nara A observes nara B restart
    ↓
A emits restart observation event
    ↓
Event added to A's local ledger
    ↓
Event broadcast via MQTT (real-time)
    ↓
Neighbors C, D, E receive and store event
    ↓
During boot recovery, new nara F syncs from C
    ↓
F now knows about B's restart history
    ↓
Background sync spreads to remaining naras
    ↓
Eventually most naras know (eventual consistency)
```

## Event Types

### restart
Detected a nara restarted (StartTime or restart count changed).

**Fields:**
- `start_time`: Unix timestamp when nara started
- `restart_num`: Total restart count
- `importance`: Critical (3) - never dropped

**Used for:**
- Consensus on restart counts
- Detecting unstable naras
- Triggering "high-restarts" teases

### first-seen
First time this observer sees a particular nara.

**Fields:**
- `start_time`: Best guess at StartTime
- `importance`: Critical (3) - never dropped

**Used for:**
- Bootstrap StartTime consensus
- Detecting new naras joining network

### status-change
Observer detected ONLINE ↔ OFFLINE ↔ MISSING transition.

**Fields:**
- `online_state`: "ONLINE", "OFFLINE", or "MISSING"
- `cluster_name`: Current cluster (optional)
- `importance`: Normal (2) - may be filtered by very chill naras

**Used for:**
- Real-time awareness of network topology
- Triggering "comeback" teases
- Barrio/cluster tracking

### Backfill Events

Events with `is_backfill: true` represent historical knowledge being converted to events during migration from newspapers to event-driven mode.

**When created:**
- On startup if no events exist yet for a known nara
- When seeing a nara for first time but already have newspaper data about them

**Purpose:**
- Grandfather existing network knowledge into event system
- Prevent "amnesia" when switching to event mode
- Allow smooth migration without data loss

**Example:**
```
lisa has been running since 2021 (StartTime: 1624066568, Restarts: 1137)

Nara boots with event mode enabled:
  - Checks ledger: no observation events about lisa
  - Has newspaper data: lisa StartTime=1624066568, Restarts=1137
  - Creates backfill event:
    {
      Observer: "bart",
      Subject: "lisa",
      Type: "restart",
      IsBackfill: true,
      StartTime: 1624066568,
      RestartNum: 1137,
      Importance: Critical
    }
```

### Checkpoint Events

Checkpoint events are multi-party attested snapshots of historical state. They provide a more robust "historical anchor" than backfill events by requiring multiple naras to vote on and sign the data through MQTT-based consensus.

**When created:**
- Each nara proposes a checkpoint about itself every 24 hours
- Consensus is reached through a two-round MQTT voting process

**Purpose:**
- Permanent anchor for historical restart counts and uptime
- Multi-party attestation provides stronger guarantees than single-observer backfill
- Allows deriving restart count as: `checkpoint.Observation.Restarts + count(unique StartTimes after checkpoint)`

**Structure:**
```json
{
  "service": "checkpoint",
  "checkpoint": {
    "subject": "lisa",
    "subject_id": "nara-id-hash",
    "as_of_time": 1704067200,
    "observation": {
      "restarts": 47,
      "total_uptime": 23456789,
      "start_time": 1624066568
    },
    "round": 1,
    "voter_ids": ["homer-id", "marge-id", "bart-id"],
    "signatures": ["sig1", "sig2", "sig3"]
  }
}
```

**Key Concepts:**
- **NaraObservation**: Pure state data embedded in checkpoint (restarts, total_uptime, start_time)
- **Attestation**: Signed claims about a nara's state used in checkpoint voting
  - Self-attestation: Proposer signs their own values (checkpoint proposal)
  - Third-party attestation: Voters sign their view (checkpoint vote)

**MQTT Checkpoint Consensus Flow:**
1. Nara proposes checkpoint about itself via `nara/checkpoint/propose`
2. Other naras respond within 5-minute vote window via `nara/checkpoint/vote`
3. Voters can APPROVE (sign proposer's values) or REJECT (sign their own values)
4. If majority agrees on same values → checkpoint finalized
5. If no consensus → Round 2 with trimmed mean values
6. Round 2 is final - if no consensus, try again in 24 hours

**Minimum Requirements:**
- At least 2 voters required (outside the proposer, so 3+ total signatures)
- Each signature is for specific values (verifiable)

**Deriving Restart Count:**
```
Total Restarts = checkpoint.Observation.Restarts + count(unique StartTimes after checkpoint.AsOfTime)

Priority order:
1. Checkpoint (if exists) - strongest guarantee
2. Backfill (if exists) - single observer historical data
3. Count events - no historical baseline
```

## Importance Levels

### Critical (3)
**Never filtered** by personality - essential for consensus. **Never pruned.**

Events: restart, first-seen, backfill, checkpoint

### Normal (2)
**May be filtered** by very chill naras (>85).

Events: status-change

### Casual (1)
**Filtered** based on personality preferences.

Events: teasing, routine social interactions

## Anti-Abuse Mechanisms

Four layers protect against malicious or misconfigured naras:

### 1. Per-Pair Compaction
- Max 20 observation events per observer→subject pair
- Oldest events dropped when limit exceeded
- Prevents hyperactive observer from saturating storage

### 2. Time-Window Rate Limiting
- Max 10 events about same subject per 5-minute window
- Blocks burst flooding (e.g., restart event every second)
- Window slides forward automatically

### 3. Content-Based Deduplication
- Hash restart events by (subject, restart_num, start_time)
- Multiple observers reporting same restart = single stored event
- Prevents redundant storage

### 4. Importance-Aware Pruning
- Global ledger pruning respects importance levels
- Critical events (restarts) survive longest
- Casual events pruned first under storage pressure

**Combined result:** Network remains functional with 1% malicious nodes at 5000 node scale.

## Comparison: Newspapers vs Events

### Old Approach (Newspapers)

**How it worked:**
- Broadcast entire Observations map every 5-55 seconds
- Contains one entry per known nara (StartTime, Restarts, LastRestart, etc.)
- At 5000 nodes: 750KB × 5000 nodes / 5s = **68MB/s - 1GB/s**

**Pros:**
- Instant consistency
- Centralized state (easy to understand)

**Cons:**
- O(N²) traffic growth
- Doesn't scale past ~100 nodes
- No protection against malicious broadcasts

### New Approach (Events)

**How it works:**
- Emit events only when changes occur
- Background sync: 83 KB/s network-wide
- Eventual consistency via gossip

**Pros:**
- **99.99% traffic reduction**
- Scales to 5000+ nodes
- Anti-abuse protection built-in
- Gradual propagation (resilient to spikes)

**Cons:**
- Eventual consistency (not instant)
- Requires background sync for memory strengthening
- More complex (events + consensus)

## Migration Path

### Phase 1: Backfill (Complete)
- On startup: backfill existing observations into events
- Consensus switches to events-primary, newspapers-fallback
- Newspapers stop broadcasting Observations map
- Newspaper frequency reduced (30-300s)
- Network supports both old (newspaper) and new (event) modes simultaneously

### Phase 2: Checkpoints (Current)
- Naras create multi-signed checkpoint events via MQTT consensus
- Checkpoint takes precedence over backfill for restart counting
- Historical data anchored with stronger guarantees
- Each nara proposes checkpoint about itself every 24 hours

### Phase 3: Event-Only (Future)
- Remove Observations map from NaraStatus struct entirely
- Remove newspaper fallback from consensus
- Events are sole source of truth
- Restart count derived from checkpoint + unique StartTimes

## Background Sync

To help the collective memory stay strong, naras perform lightweight periodic syncing:

**Schedule:**
- Every ~30 minutes (±5min random jitter)
- Query 1-2 random online neighbors

**Query focus:**
```json
{
  "services": ["observation"],
  "since_time": "<24 hours ago>",
  "max_events": 100,
  "min_importance": 2
}
```

**Why needed:**
- Event persistence (critical events survive even if some naras drop them)
- Gradual propagation (events spread organically)
- Personality compensation (high-chill naras catch up)
- Network healing (partitioned nodes eventually converge)

## Consensus Algorithm

Uses **trimmed mean** to calculate robust consensus from observation events:

1. **Query events:** Get all observation events about subject from SyncLedger
2. **Collect values:** Extract reported values (StartTime, Restarts, etc.) from each observer
3. **Filter outliers:** Remove values outside [median×0.2, median×5.0] range
4. **Calculate average:** Average remaining values (naturally weighted by frequency)
   - If 5 observers report "270" and 2 report "271", the average weights toward 270
   - Single-observer fallback: Use max value if only one observer reporting
5. **Log quality metrics:**
   - Agreement % - how many observers agree on most common value
   - Std deviation - spread of opinions (low = tight consensus, high = noisy)
   - Outliers removed - which values were filtered as suspicious

**Result:** Distributed consensus on StartTime, Restarts, LastRestart without centralized broadcast.

**Key insight:** Large discrepancies indicate bugs (false MISSING detections, backfill failures) rather than natural variance.

## Code Locations

- `sync.go` - ObservationEventPayload struct, anti-abuse logic
- `observations.go` - Event emission, consensus formation, NaraObservation struct
- `attestation.go` - Attestation type for signed claims (checkpoint voting)
- `checkpoint_types.go` - CheckpointEventPayload struct
- `checkpoint_service.go` - Checkpoint consensus and MQTT voting
- `network.go` - Background sync, backfill mechanism
- `nara.go` - NaraStatus (will remove Observations map in v1.0.0)

## Example Flow

```
T=0: lisa restarts (actual restart #1137)

T=1: bart observes via newspaper
  - Creates restart event: {Observer: bart, Subject: lisa, RestartNum: 1137}
  - Adds to local ledger
  - Broadcasts via MQTT

T=2: nelly receives event
  - Adds to local ledger (unless filtered by personality)

T=5: jojo boots (new node)
  - Requests sync from bart
  - Gets restart event in response
  - Now knows lisa has 1137 restarts

T=30min: alice does background sync
  - Queries random neighbor (nelly)
  - Gets restart event about lisa
  - Fills in gap from when she wasn't listening

T=3h: Consensus runs on all nodes
  - Each reads observation events about lisa
  - Weighted clustering: most agree on 1137 restarts
  - Consensus: lisa.Restarts = 1137
```
