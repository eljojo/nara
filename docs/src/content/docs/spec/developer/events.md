---
title: Events
description: Immutable, signed facts in the nara network ledger.
---

Events are the fundamental unit of information in nara, representing immutable facts from which all state (status, reputation, uptime) is derived.

## 1. Purpose
- Provide an immutable audit trail of all network activity.
- Enable eventual consistency via gossip and historical sync.
- Cryptographic verification of authorship and integrity for every fact.
- Support deterministic state derivation (projections) across all nodes.

## 2. Conceptual Model
- **SyncEvent**: The unified container for all syncable data.
- **Service Type**: Categorization of the payload (e.g., `social`, `ping`, `observation`).
- **Ledger**: Chronological in-memory store for events.
- **Authorship**: Every event is bound to a nara's soul via Ed25519 signatures.

### Invariants
1. **Immutability**: Event `ID`s are derived from content; once computed, the event MUST NOT change.
2. **Deterministic IDs**: Identical content + identical timestamp MUST produce the same `ID`.
3. **Causality**: Events are ordered by nanosecond timestamps.
4. **Authenticity**: Every event MUST be signed by the emitter's soul to be accepted by the ledger.

## 3. External Behavior
- Components subscribe to the ledger to receive new events.
- Projections replay events from the ledger to derive current state (e.g., clout, online status).
- The ledger automatically prunes low-priority events when `MaxEvents` is exceeded, preserving critical history.

## 4. Interfaces

### SyncEvent Structure
- `id`: 32-character hex string (derived from 16 bytes of SHA256).
- `ts`: Unix timestamp in **nanoseconds**.
- `svc`: Service type string.
- `emitter`: Nara name of the author.
- `emitter_id`: Nara ID (public key hash) of the author.
- `sig`: Base64-encoded Ed25519 signature.

### Service Types & Importance
| Service | Importance | Purpose |
| Service | Importance | Purpose |
| `checkpoint` | 3 (Critical) | Multi-sig historical [state anchors](/docs/spec/services/checkpoints/). |
| `observation`| 1-3 | Consensus on restarts (3), first-seen (3), status-change (2). |
| `hey-there` | 3 (Critical) | Presence and public key announcements. |
| `chau` | 3 (Critical) | Graceful departure announcements. |
| `social` | 2 (Normal) | Teasing, gossip, and interactions via [social events](/docs/spec/services/social/). |
| `seen` | 1 (Casual) | Proof-of-contact sightings. |
| `ping` | 1 (Casual) | Latency (RTT) measurements. |

## 5. Event Types & Schemas

### Payload Types
- `SocialEventPayload`: `type`, `actor`, `actor_id`, `target`, `target_id`, `reason`, `witness`.
- `ObservationEventPayload`: `type`, `subject`, `subject_id`, `start_time`, `restart_num`, `last_restart`, `online_state`.
- `PingObservation`: `observer`, `target`, `rtt`.
- `HeyThereEvent`: `from`, `public_key`, `mesh_ip`, `id`.
- `ChauEvent`: `from`, `public_key`, `id`.
- `SeenEvent`: `observer`, `subject`, `via`.
- `CheckpointEventPayload`: Consensus votes and [anchors](/docs/spec/services/checkpoints/).

## 6. Algorithms

### ID Computation
`ID = Hex(SHA256(Timestamp + ":" + Service + ":" + Payload.ContentString()))[0:32]`
*Note: Payload.ContentString() is a colon-separated canonical representation of the payload.*

### Signing
1. **Message**: `ID + ":" + Timestamp + ":" + Service + ":" + Emitter + ":" + Payload.ContentString()`
2. **Signature**: Ed25519 signature over the SHA256 hash of the Message, derived from the nara [identity](/docs/spec/runtime/identity/) key.

### Verification
1. Resolve the `emitter_id` to a public key.
2. Verify the `sig` against the canonical `signableData`.

## 7. Failure Modes
- **Clock Skew**: Events with future timestamps may be rejected or prematurely pruned.
- **Signature Failure**: Events with invalid signatures are dropped immediately.
- **Ledger Saturation**: If the volume of Critical (Priority 0) events exceeds `MaxEvents`, the ledger will grow beyond its limit as it refuses to prune critical history.

## 8. Security / Trust Model
- **Non-Repudiation**: Authors cannot deny events they have signed.
- **Integrity**: Any modification to an event's fields will invalidate its signature and ID.
- **Hearsay Protection**: Observations can be compared across multiple emitters to reach consensus, protecting against single-node lies.

## 9. Test Oracle
- `TestSyncEvent_IDStability`: Verifies deterministic ID generation.
- `TestSyncEvent_Signing`: Ensures signing/verification round-trips correctly.
- `TestSyncEvent_Immutability`: Ensures any field change results in verification failure.
- `TestSyncEvent_CheckpointCutoff`: Verifies that checkpoints before `1768271051` are ignored.

## 10. Open Questions / TODO
- Move from name-based fallbacks to ID-only verification for all payloads.
- Unify `ObservationEventPayload` with `NaraObservation` struct used in checkpoints.
