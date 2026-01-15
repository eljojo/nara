# Events

## Purpose

Events are the only shared facts in the Nara network.
Everything else (status, opinions, UI views) is derived from them.

## Conceptual Model

A SyncEvent is a signed, immutable record with a service type and a payload.
Events are append-only and may arrive out of order.

Key invariants:
- Events are never mutated after ID computation.
- Event IDs are deterministic hashes of timestamp + service + payload content.
- Signatures authenticate the emitter, not the truth of the payload.
- Some services (e.g., checkpoints) are filtered on ingestion.

## External Behavior

- Naras emit SyncEvents and store them in the in-memory SyncLedger.
- Events are exchanged over MQTT and mesh HTTP (gossip and sync APIs).
- Each nara may see a different subset of events.

## Interfaces

SyncEvent JSON shape:
- `id` (string): deterministic event ID.
- `ts` (int64): Unix timestamp in nanoseconds.
- `svc` (string): service type.
- `emitter` (string, optional): name of the emitter.
- `emitter_id` (string, optional): Nara ID; currently usually empty.
- `sig` (string, optional): Base64 Ed25519 signature.
- One payload field set based on `svc`:
  - `social`, `ping`, `observation`, `hey_there`, `chau`, `seen`, `checkpoint`.

## Event Types & Schemas (if relevant)

Service types:
- `social` -> SocialEventPayload
- `ping` -> PingObservation
- `observation` -> ObservationEventPayload
- `hey-there` -> HeyThereEvent
- `chau` -> ChauEvent
- `seen` -> SeenEvent
- `checkpoint` -> CheckpointEventPayload

SocialEventPayload:
- `type`: "tease", "observed", "gossip", "observation", or "service".
- `actor`, `target` (required), `reason`, `witness`.

PingObservation:
- `observer`, `target` (required), `rtt` (ms, > 0).
- Signable content rounds RTT to 0.1ms.

ObservationEventPayload:
- `observer`, `subject`, `type` (required).
- `type` is "restart", "first-seen", or "status-change".
- `importance` is 1..3.
- `start_time`, `restart_num`, `last_restart` are seconds (not nanoseconds).
- `online_state` is "ONLINE", "OFFLINE", or "MISSING".
- `observer_uptime` is used for consensus weighting.

HeyThereEvent:
- `from`, `public_key` (required), `mesh_ip`, `id`, `signature`.
- Inner signature uses legacy format: `hey_there:{from}:{public_key}:{mesh_ip}`
  (ID is currently ignored in signing).

ChauEvent:
- `from` (required), `public_key`, `id`, `signature`.
- Inner signature uses legacy format: `chau:{from}:{public_key}`
  (ID is currently ignored in signing).

SeenEvent:
- `observer`, `subject`, `via` (required).
- `via` is typically "zine", "mesh", "ping", or "sync".

CheckpointEventPayload:
- `subject` (name), `subject_id` (Nara ID), `observation`, `as_of_time`, `round`.
- `voter_ids` and `signatures` are parallel arrays (Base64 Ed25519 signatures).

## Algorithms

Event ID:
- ID = hex(SHA256("{ts}:{svc}:" + payload.ContentString)) truncated to 16 bytes.
- Any mutation after ID computation makes the ID stale.

Event signature:
- Signable bytes = SHA256("{id}:{ts}:{svc}:{emitter}:" + payload.ContentString).
- `sig` is Base64 Ed25519 signature over signable bytes.

Validation:
- `IsValid` requires `svc`, `ts`, and a payload with its own validation rules.
- `Verify` uses the payload’s verification logic (default: resolve public key
  by `emitter_id` or `emitter`).
- `hey-there` and `chau` verify using the embedded public key.

Ledger ingestion:
- Duplicate events are rejected by ID.
- Checkpoints before the cutoff time are filtered before ingestion.
- Observation events may be added with content-based dedupe (see observations spec).

## Failure Modes

- Missing or out-of-order events lead to incomplete or inconsistent projections.
- Invalid signatures cause events to be ignored or logged as warnings.
- Legacy checkpoints before the cutoff time are dropped.

## Security / Trust Model

- Ed25519 signatures authenticate the emitter’s soul-derived key.
- Events do not claim truth; they only claim that an emitter observed or asserted a fact.
- Some events bootstrap identity by embedding the public key (hey_there/chau).

## Test Oracle

- Tampering with signed events fails verification. (`identity_crypto_test.go`)
- Event IDs are deterministic from payload + timestamp. (`sync_test.go`)
- Old checkpoints are filtered on ingestion. (`sync_checkpoint_filter_test.go`)

## Open Questions / TODO

- Use `emitter_id` consistently (currently mostly empty in emitted events).
