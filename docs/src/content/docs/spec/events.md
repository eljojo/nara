---
title: Events
---

# Events

## Purpose

Events are the only shared facts in the Nara network.
Everything else (status, opinions, UI views) is derived from them.

## Conceptual Model

A **SyncEvent** is a signed, immutable record with a service type and a payload.
Events are append-only and may arrive out of order.

Key invariants:
- **Immutability**: Events are never mutated after ID computation.
- **Determinism**: Event IDs are deterministic hashes of timestamp + service + payload content.
- **Precision**: Timestamps are in nanoseconds to prevent collisions and ensure precise ordering.
- **Authentication**: Signatures authenticate the emitter (who said it), not the truth of the payload (what happened).
- **Filtering**: Some services (e.g., legacy checkpoints) are filtered on ingestion based on cutoff times.

## External Behavior

- Naras emit `SyncEvent`s and store them in the in-memory `SyncLedger`.
- Events are exchanged over MQTT and mesh HTTP (gossip and sync APIs).
- Each nara may see a different subset of events, leading to temporary divergent views that eventually converge via gossip.

## Interfaces

### SyncEvent JSON Shape
```json
{
  "id": "16-byte hex string (deterministic hash)",
  "ts": 1700000000000000000,
  "svc": "social | ping | observation | hey-there | chau | seen | checkpoint",
  "emitter": "nara-name",
  "emitter_id": "nara-ID (Base58 public key hash)",
  "sig": "Base64 Ed25519 signature",
  "social": { ... },
  "ping": { ... },
  "observation": { ... },
  "hey_there": { ... },
  "chau": { ... },
  "seen": { ... },
  "checkpoint": { ... }
}
```
*Only one payload field is populated based on `svc`.*

## Event Types & Schemas

### 1. Social (`svc: social`)
Used for teasing, gossip, and social interactions.
- `type`: "tease", "observed", "gossip", "observation", "service".
- `actor`: Who performed the action.
- `target`: Who was the target.
- `reason`: Why (e.g., "high-restarts", "trend-abandon").
- `witness`: Who reported it (empty if self-reported).

### 2. Ping (`svc: ping`)
Latency measurements between naras.
- `observer`: Who measured.
- `target`: Who was measured.
- `rtt`: Round-trip time in milliseconds (float).
- *Note*: RTT is rounded to 0.1ms in the canonical signing string to avoid float precision issues.

### 3. Observation (`svc: observation`)
Network state observations for distributed consensus.
- `observer`: Who made the observation.
- `subject`: Who is being observed.
- `type`: "restart", "first-seen", "status-change".
- `importance`: 1 (Casual), 2 (Normal), 3 (Critical).
- `start_time`: Unix timestamp (seconds) of the subject's start time.
- `restart_num`: Monotonic restart counter.
- `online_state`: "ONLINE", "OFFLINE", "MISSING".
- `observer_uptime`: Observer's uptime in seconds (used for weighted consensus).

### 4. Presence (`svc: hey-there` / `svc: chau`)
Identity announcements and graceful shutdowns.
- `from`: Name of the announcer.
- `public_key`: Base64 Ed25519 public key.
- `mesh_ip`: Tailscale/Headscale IP (hey-there only).
- `id`: Nara ID.
- *Note*: These events embed their own legacy inner signatures for backward compatibility, but the outer SyncEvent signature is the primary authenticator in the modern protocol.

### 5. Seen (`svc: seen`)
Lightweight proof of contact.
- `observer`: Who saw them.
- `subject`: Who was seen.
- `via`: "zine", "mesh", "ping", "sync".

### 6. Checkpoint (`svc: checkpoint`)
Historical state snapshots with multi-party attestation.
- `subject`: Name of the nara being checkpointed.
- `subject_id`: Nara ID.
- `observation`: The `NaraObservation` struct (status, start time, etc.).
- `as_of_time`: Unix timestamp (seconds) of the snapshot.
- `round`: Checkpoint round number (0 = draft, 1 = consensus).
- `voter_ids`: List of Nara IDs who voted.
- `signatures`: List of signatures corresponding to voters.

## Algorithms

### Event ID Computation
```
ID = Hex(SHA256(
    Timestamp (nanos) + ":" +
    Service + ":" +
    Payload.ContentString()
))[0..16]
```
*Note: Any mutation after ID computation invalidates the ID.*

### Signing
```
SignableBytes = SHA256(
    ID + ":" +
    Timestamp + ":" +
    Service + ":" +
    Emitter + ":" +
    Payload.ContentString()
)
Signature = Ed25519_Sign(PrivateKey, SignableBytes)
```

### Verification
1. **Check Structure**: Ensure `svc`, `ts`, and payload are present.
2. **Resolve Key**: Look up public key by `emitter_id` (preferred) or `emitter` (fallback).
3. **Verify Signature**: `Ed25519_Verify(PublicKey, SignableBytes, Signature)`.
4. **Service-Specific Logic**: Some payloads (like checkpoints) have additional internal verification.

### Checkpoint Filtering
Checkpoints with timestamps earlier than `CheckpointCutoffTime` (1768271051) are dropped during ingestion to filter out malformed historical data from early protocol versions.

## Failure Modes

- **Stale IDs**: Modifying an event after creation invalidates its ID, causing signature verification failures.
- **Clock Skew**: Events with timestamps far in the future or past may be rejected or deprioritized by some logic (though generally accepted for eventual consistency).
- **Missing Keys**: If an emitter's identity is unknown, the event cannot be verified and is dropped.

## Security / Trust Model

- **Authentication**: Signatures prove *who* sent the event.
- **Non-Repudiation**: Emitters cannot deny sending an event once signed.
- **No Truth Guarantee**: A signed event only proves "A said X", not that "X is true". Consensus algorithms (like `presence_projection`) aggregate conflicting claims to determine truth.

## Test Oracle

- **ID Determinism**: Same content + same timestamp => Same ID. (`sync_test.go`)
- **Immutability**: Modifying a field changes the hash, invalidating the signature. (`sync_test.go`)
- **Signature Verification**: Valid signatures pass, tampered ones fail. (`identity_crypto_test.go`)
- **Checkpoint Filtering**: Old checkpoints are rejected. (`sync_checkpoint_filter_test.go`)
- **Payload Validation**: Invalid payloads (e.g. ping with 0 RTT) return `IsValid() = false`.
