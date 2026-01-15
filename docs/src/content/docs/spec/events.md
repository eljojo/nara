---
title: Events
description: Immutable, signed facts in the Nara Network ledger.
---

# Events

Events are the fundamental unit of information in Nara. They represent immutable facts that have been observed or reported by a participant. All network state, including online status, reputation, and uptime, is derived by replaying these events.

## Purpose
- Provide an immutable audit trail of all network activity.
- Enable eventual consistency through gossip and peer synchronization.
- Support cryptographic verification of authorship and integrity.
- Allow for deterministic state derivation across divergent nodes.

## Conceptual Model
- **SyncEvent**: A unified container that wraps various service-specific payloads.
- **Service Types**: Categorize events (e.g., `social`, `ping`, `observation`).
- **Ledger**: A chronologically ordered, in-memory store of events.
- **Deduplication**: Events are uniquely identified by a 16-character hex hash.

### Invariants
- **Immutability**: Once an event's ID is computed, its fields (timestamp, service, payload) must never change.
- **Causality**: Events are ordered by their nanosecond timestamps.
- **Authorship**: All events must be signed by an emitter's soul to be considered authentic.
- **Compactness**: Events are designed to be small enough to propagate quickly via gossip.

## Interfaces

### SyncEvent Structure (JSON)
- `id`: 16-character hex string (hash of content).
- `ts`: Unix timestamp in **nanoseconds**.
- `svc`: Service type string.
- `emitter`: Name of the Nara that created the event.
- `emitter_id`: Stable Nara ID of the emitter.
- `sig`: Base64-encoded Ed25519 signature.

### Service Types Reference
| Service | Purpose | Payload Type |
| :--- | :--- | :--- |
| `social` | Teasing, gossip, and social observations. | `SocialEventPayload` |
| `ping` | Latency measurements (RTT). | `PingObservation` |
| `observation`| Network state consensus (restarts, first-seen). | `ObservationEventPayload` |
| `hey-there` | Presence and identity announcement. | `HeyThereEvent` |
| `chau` | Graceful shutdown announcement. | `ChauEvent` |
| `seen` | Lightweight proof-of-contact. | `SeenEvent` |
| `checkpoint` | Multi-sig historical state snapshot. | `CheckpointEventPayload` |

## Algorithms

### 1. ID Computation
To ensure deterministic deduplication, the ID is calculated as:
`ID = hex(SHA256(timestamp_nanos + ":" + service_type + ":" + payload_content_string)[0:8])`

### 2. Signing & Verification
Naras sign the entire event structure to prevent tampering:
1. **Signable Data**: `SHA256(ID + ":" + TS + ":" + SVC + ":" + EMITTER + ":" + payload_content_string)`
2. **Signature**: Ed25519 signature of the signable bytes.
3. **Verification**: Emitter's public key is retrieved from the local neighborhood using `emitter_id` or provided in bootstrap events (`hey-there`).

### 3. Checkpoint Filtering
Events of type `checkpoint` are filtered during ingestion if their `AsOfTime` is before `1768271051`. This protects the network from legacy checkpoints that lack essential fields for consensus.

## Failure Modes
- **Timestamp Collisions**: While rare due to nanosecond precision, identical IDs will cause valid events to be discarded as duplicates.
- **Clock Skew**: Events with future timestamps may be rejected or incorrectly ordered, leading to "time travel" bugs in projections.
- **Signature Drift**: If a Nara changes its soul but keeps its name, its older signed events will fail verification unless the new soul is properly introduced.

## Security / Trust Model
- **Non-Repudiation**: A Nara cannot deny having emitted a signed event.
- **Integrity**: Any modification to an event's payload or metadata after signing will cause verification to fail.
- **Subjective Truth**: While the *fact* that an event was sent is objective, the *meaning* of that event (e.g., if a tease is "cool") is subjective.

## Test Oracle
- `TestSyncEvent_IDStability`: Verifies that the same content always produces the same ID.
- `TestSyncEvent_Signing`: Ensures that signatures are correctly generated and verified.
- `TestSyncEvent_Immutability`: Validates that modifying any field after signing breaks verification.
- `TestSyncEvent_CheckpointCutoff`: Checks that old, buggy checkpoints are correctly ignored.
