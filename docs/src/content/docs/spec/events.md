---
title: Events
description: Immutable, signed facts in the Nara Network ledger.
---

Events are the fundamental unit of information in Nara, representing immutable facts from which all state (status, reputation, uptime) is derived.

## 1. Purpose
- Immutable audit trail of network activity.
- Eventual consistency via gossip and sync.
- Cryptographic verification of authorship and integrity.
- Deterministic state derivation across nodes.

## 2. Conceptual Model
- **SyncEvent**: Unified container for service-specific payloads.
- **Service Types**: Categorization (e.g., `social`, `ping`, `observation`).
- **Ledger**: Chronological in-memory store.
- **Deduplication**: Unique 16-char hex IDs.

### Invariants
- **Immutability**: IDs are content-derived; fields must never change.
- **Causality**: Ordered by nanosecond timestamps.
- **Authorship**: Must be signed by the emitter's soul.

## 3. Interfaces

### SyncEvent Structure
- `id`: 16-char hex hash.
- `ts`: Unix nanoseconds.
- `svc`: Service type.
- `emitter` / `emitter_id`.
- `sig`: Base64 Ed25519 signature.

### Service Types
| Service | Purpose |
| :--- | :--- |
| `social` | Teasing, gossip, interactions. |
| `ping` | Latency (RTT) measurements. |
| `observation`| Consensus (restarts, first-seen). |
| `hey-there` | Presence/identity announcement. |
| `chau` | Graceful departure. |
| `seen` | Proof-of-contact. |
| `checkpoint` | Multi-sig historical snapshot. |

## 4. Algorithms

### ID Computation
`ID = hex(SHA256(ts_nanos + ":" + svc + ":" + payload)[0:8])`

### Signing & Verification
1. **Data**: `SHA256(ID + ":" + ts + ":" + svc + ":" + emitter + ":" + payload)`
2. **Signature**: Ed25519 (RFC 8032).
3. **Verification**: Emitter's public key (fetched by `emitter_id`).

### Legacy Filtering
Ignore `checkpoint` events with `AsOfTime` < `1768271051`.

## 5. Security & Trust
- **Non-Repudiation**: Signed events cannot be denied.
- **Integrity**: Modification after signing invalidates the event.
- **Authenticity**: Guaranteed by soul-based signatures.

## 6. Test Oracle
- `TestSyncEvent_IDStability`: Deterministic ID generation.
- `TestSyncEvent_Signing`: Generation/verification round-trip.
- `TestSyncEvent_Immutability`: Integrity checks.
- `TestSyncEvent_CheckpointCutoff`: Legacy data filtering.
