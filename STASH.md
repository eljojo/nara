# Stash - Distributed Encrypted Storage

Naras store their encrypted data (arbitrary JSON) on other naras ("confidants") instead of local disk. This enables stateless operation where a nara can reboot on any machine and recover its state.

## How It Works

1. **Owner** picks 3 naras as **confidants** (prefers higher memory modes and uptime)
2. Owner encrypts their arbitrary JSON data with their own key (derived from soul)
3. Owner sends stash to confidants via `POST /stash/store` over mesh (triggered immediately on update)
4. Confidants store in **memory only** (never persisted to disk)
5. On boot, owner's hey-there event triggers confidants to push stash back via `POST /stash/push`

## Key Properties

- **Self-encrypted**: Only the owner can decrypt their stash (key derived from soul)
- **Hybrid selection**: First confidant picked by score (best reliability), remaining 2 picked randomly (better distribution)
- **Memory-only**: No disk persistence (sync state lost on restart, re-sync acceptable)
- **Tamper-proof**: Timestamp is inside the encrypted payload
- **Clear endpoints**: Separate HTTP endpoints for store, retrieve, push, and delete operations
- **Replay protection**: All requests timestamped and validated (rejects >30s old requests)
- **Commitment-based**: Confidants accept or reject storage requests based on capacity
- **Mutual promises**: Both sides track who's storing what (not an anonymous cache)
- **Memory-aware storage**: Short mode stores 5, Medium 20, Hog 50 stashes for others
- **Health monitoring**: Owners detect offline confidants and find replacements automatically
- **Ghost pruning**: Stashes for dead naras (offline 7+ days) are evicted to free capacity
- **Metrics emission**: Stash storage metrics broadcast in newspaper and logs
- **Social reward**: Storing stash for someone emits stash_stored social event

## Commitment Model Philosophy

Stash storage is **not an LRU cache** - it's a system of **mutual promises** between naras:

### Why Commitments Matter

**Bad (LRU cache model):**
- Confidant silently evicts stashes to make room
- Owner thinks their stash is safe, but it's gone
- Broken promise, no notification
- Owner doesn't know they need to find new confidant

**Good (Commitment model):**
- Confidant accepts or rejects based on capacity
- If accepted, **promise is kept** until owner dies
- Owner knows exactly who's storing their stash
- If confidant goes offline, owner detects it and finds replacement

### The Contract

When a confidant **accepts** a stash:
1. They track this commitment (not just storage)
2. They keep it until owner is confirmed dead (offline 7+ days)
3. They emit a social event (get clout for helping)

When a confidant **rejects** a stash:
1. Owner knows immediately (HTTP response)
2. Owner tries another confidant
3. No broken promises

### Health Monitoring

Both sides actively monitor the relationship:
- **Owner**: Every 5 min, checks if confidants are still online
- **Confidant**: Every 5 min, checks if owners are still alive (ghost pruning)
- **Replacement**: If confidant goes offline, owner automatically finds new one

This creates a **resilient distributed storage network** where promises are kept and failures are handled gracefully.

## Data Flow

### Storing Stash (During Gossip)

```
Owner                           Confidant
  |                                 |
  |-- POST /stash/store ----------->| Verify signature & timestamp
  |    {                            | Check capacity
  |      From: "owner",             |
  |      Timestamp: 1234567890,     | If space: Accept commitment ✓
  |      Stash: <encrypted>,        | If full: Reject (at_capacity) ✗
  |      Signature: "..."           |
  |    }                            |
  |                                 |
  |<----------- Response -----------|
  |    {                            |
  |      Accepted: true/false,      |
  |      Reason: "..."              | Why rejected (if false)
  |    }                            |
  |                                 |
  | If Accepted:                    | If Accepted:
  |   Add confidant to tracker      |   Track commitment
  | If Rejected:                    |   Emit social event
  |   Try another confidant         |
  |                                 |
  | Rate limit: 5 min intervals     |
```

**Acceptance/Rejection Reasons:**
- `accepted` - Commitment created successfully
- `at_capacity` - No room (already storing max stashes)
- `stash_too_large` - Payload exceeds size limit (>10KB)
- `stash_disabled` - Stash storage not enabled

**Timestamp Validation:**
- Rejects if timestamp >30 seconds old (replay protection)
- Rejects if timestamp >30s in future (assumes NTP sync)

### Recovering Stash (Boot + Hey-There)

```
Owner (boots)                   Confidants
  |                                 |
  |-- hey-there (MQTT) ------------>| (broadcast)
  |                                 |
  |                                 | Detect: "I have their stash!"
  |                                 | Wait 2 seconds (let them boot)
  |                                 |
  |<-- POST /stash/push ------------| (HTTP push via mesh)
  |    {                            |
  |      From: "confidant",         |
  |      To: "owner",               |
  |      Timestamp: 1234567890,     |
  |      Stash: <encrypted>,        |
  |      Signature: "..."           |
  |    }                            |
  |                                 |
  | Verify signature & timestamp    |
  | Decrypt, use recovered data     |
  | Mark confidant in tracker       |
```

**Alternative Recovery (Manual):**

Owner can also explicitly request their stash back:

```
Owner                           Confidant
  |                                 |
  |-- POST /stash/retrieve -------->| Verify signature & timestamp
  |    {                            | Check if we have their stash
  |      From: "owner",             |
  |      Timestamp: 1234567890,     |
  |      Signature: "..."           |
  |    }                            |
  |                                 |
  |<----------- Response -----------|
  |    {                            |
  |      Found: true/false,         |
  |      Stash: <encrypted>         | (if found)
  |    }                            |
```

### Managing Confidants (Commitment Model)

Stash storage is based on **mutual promises** between naras:

**How Commitments Work:**
1. Owner asks confidant: "Will you store my stash?"
2. Confidant checks capacity:
   - If space available: **Accepts** (creates commitment)
   - If at capacity: **Rejects** with reason
3. Owner only adds to confirmed confidants if accepted
4. Confidant tracks commitment and keeps it until owner dies (offline 7+ days)

**Maintenance (runs every 5 minutes):**
1. **Health Check**: Detect offline/missing confidants, remove from tracker
2. **Fill Gaps**: If below target (3), pick new confidant and send stash
3. **Ghost Pruning**: Evict stashes for naras offline >7 days (frees capacity)
4. **Metrics Logging**: Report storage status

**When Confidant Goes Offline:**
- Owner detects via OnlineStatusProjection (OFFLINE or MISSING status)
- Removes from confidant list automatically
- Next maintenance round finds replacement
- New confidant receives stash immediately via direct HTTP POST

**When Owner Goes Ghost (offline 7+ days):**
- Confidant's ghost pruning detects prolonged absence
- Commitment broken - stash evicted
- Capacity freed for active naras

**Target count**: 3 confidants (configurable)
**Exchange triggers**: Gossip rounds (every 30-300s), stash data changes, manual recovery

## HTTP Endpoints

### Mesh Endpoints (Nara-to-Nara)

| Endpoint | Method | Auth | Purpose |
|----------|--------|------|---------|
| `/stash/store` | POST | Ed25519 mesh auth + timestamp | Owner stores stash with confidant |
| `/stash/store` | DELETE | Ed25519 mesh auth + timestamp | Owner requests deletion of stash |
| `/stash/retrieve` | POST | Ed25519 mesh auth + timestamp | Owner retrieves stash from confidant |
| `/stash/push` | POST | Ed25519 mesh auth + timestamp | Confidant pushes stash to owner (recovery) |

### Web UI Endpoints (Local Only)

| Endpoint | Method | Auth | Purpose |
|----------|--------|------|---------|
| `/api/stash/status` | GET | Local only | Get stash + confidants + metrics |
| `/api/stash/update` | POST | Local only | Update stash data |
| `/api/stash/recover` | POST | Local only | Trigger manual recovery |
| `/api/stash/confidants` | GET | Local only | List confidants with details |

**All mesh endpoints:**
- Require Ed25519 signature in request body
- Require timestamp (unix seconds) in request body
- Reject requests >30 seconds old or >30s in future (replay protection, assumes NTP sync)
- Use mesh authentication headers for transport security

**Note**: MQTT hey-there events still used to trigger automatic recovery (confidants detect boot and push stash via `POST /stash/push`).

## Encryption

Uses XChaCha20-Poly1305 with a symmetric key derived from the owner's Ed25519 private key:

```
Ed25519 seed -> HKDF("nara:stash:v1") -> 32-byte symmetric key
```

The encrypted payload contains:
- `timestamp`: When the stash was created (inside encrypted blob for tamper-proofing)
- `data`: Arbitrary JSON object (freeform user data)
- `version`: Schema version for future migrations

## Security

- **Signature verification** on all stash requests (Ed25519, signed with timestamp)
- **Replay protection** via timestamp validation (rejects >30s old or >30s future)
- **Memory-only** on confidants (no disk writes, no persistence)
- **Hybrid selection** balances reliability (1 best nara) and distribution (2 random naras)
- **Rate limiting** prevents spam (5-minute intervals per peer)
- **Commitment-based capacity**: Accept/reject based on limits (5/20/50 based on memory mode)
- **Ghost-only eviction**: Only evict stashes when owner offline 7+ days (keeps promises to living naras)
- **Encrypted at rest** in memory (only owner can decrypt)

## Boot Sequence

```
1. MQTT connects (if not gossip-only mode)
2. Mesh initializes (if authkey present)
3. Broadcast hey-there event (MQTT + gossip)
4. Confidants detect hey-there:
   - Wait 2 seconds (let nara finish booting)
   - POST /stash/push with their copy of owner's stash
5. Owner receives stash via push:
   - Decrypt all responses
   - Pick newest by timestamp
   - Use recovered data
6. If no responses after timeout:
   - Start with empty state (acceptable - ephemeral design)
```

**Note**: No explicit broadcast request needed - hey-there event naturally triggers recovery from confidants.

## Metrics Emission

Stash metrics are emitted in two ways:

### Newspaper Broadcasts (NaraStatus)
```go
type NaraStatus struct {
    // ... other fields ...
    StashStored     int   `json:"stash_stored"`      // # of stashes stored for others
    StashBytes      int64 `json:"stash_bytes"`       // Total bytes stored
    StashConfidants int   `json:"stash_confidants"`  // # of confidants storing my stash
}
```

### Periodic Logs (every 5 minutes)
```
INFO  📦 Stash metrics: stored=15 (1.2MB), my_confidants=3/3, my_size=2.1KB
```

Metrics help monitor:
- Storage capacity usage
- Confidant network health
- Eviction frequency
- Overall system load

## Memory-Aware Storage

Confidants store different numbers of stashes based on memory mode:

| Memory Mode | RAM    | Stash Limit | Eviction Policy                    |
|-------------|--------|-------------|------------------------------------|
| Short       | 256MB  | 5 stashes   | Accept up to 5, then reject new    |
| Medium      | 512MB  | 20 stashes  | Accept up to 20, then reject new   |
| Hog         | 2048MB | 50 stashes  | Accept up to 50, then reject new   |

**Capacity Management:**
- When limit reached, **new requests are rejected** (not evicted)
- Existing commitments are **kept until owner goes ghost** (offline 7+ days)
- Ghost pruning runs every 5 minutes, freeing capacity for active naras

**Confidant Selection**: Hybrid strategy to balance reliability and distribution:

**First confidant** (best score):
- Hog mode: +300 points
- Medium mode: +200 points
- Short mode: +100 points
- Uptime (seconds): Added to score
- Picks the most reliable nara (highest memory + longest uptime)

**Remaining confidants** (random selection):
- Randomly selected from eligible peers
- Avoids hotspots where all naras pick the same popular confidants
- Better load distribution across the network

This ensures at least one reliable confidant while spreading storage load across diverse naras.

## Social Reward

When a confidant stores stash for an owner, a social event is emitted:

```go
SocialEvent{
    Type:   "stash_stored",
    Actor:  confidantName,
    Target: ownerName,
    Reason: ReasonStashStored,
}
```

This encourages naras to participate in the stash network and rewards high-uptime, high-capacity nodes.

## Web UI

Access stash management at `/stash.html`:
- **JSON Editor**: Edit arbitrary stash data with syntax highlighting
- **Confidant Status**: View who's storing your stash
- **Metrics Dashboard**: See storage usage and health
- **Manual Recovery**: Trigger immediate recovery
- **Storage Info**: View how many stashes you're storing for others
