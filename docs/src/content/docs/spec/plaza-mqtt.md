# Plaza MQTT

## Purpose

The plaza is the broadcast layer: MQTT topics used for real-time presence,
lightweight gossip, and checkpoint consensus.

## Conceptual Model

- Topics are public broadcast channels.
- QoS is 0 (best effort).
- Many payloads are SyncEvents, so dedupe and signatures are handled by the ledger.

Key invariants:
- MQTT delivery is unreliable by design; every message is treated as optional.
- MQTT is skipped entirely in gossip-only mode.

## External Behavior

On connect, a nara:
- Subscribes to plaza topics.
- Emits a `hey_there` SyncEvent with jitter (0-5s).

Naras continually publish:
- `hey_there` on join and periodically.
- `newspaper` status heartbeats.
- social events, howdy responses, and checkpoint consensus traffic.

## Interfaces

Topics (all QoS 0 unless noted):

Presence and discovery:
- `nara/plaza/hey_there` -> SyncEvent (ServiceHeyThere)
- `nara/plaza/chau` -> SyncEvent (ServiceChau)
- `nara/plaza/howdy` -> HowdyEvent
- `nara/newspaper/{name}` -> NewspaperEvent (Status + Signature)

Social and world:
- `nara/plaza/social` -> SyncEvent (ServiceSocial)
- `nara/plaza/journey_complete` -> JourneyCompletion

Stash:
- `nara/plaza/stash_refresh` -> {from, timestamp}

Legacy ledger sync (social-only):
- `nara/ledger/{name}/request` -> LedgerRequest
- `nara/ledger/{name}/response` -> LedgerResponse

Checkpoints:
- `nara/checkpoint/propose` -> CheckpointProposal
- `nara/checkpoint/vote` -> CheckpointVote
- `nara/checkpoint/final` -> SyncEvent (ServiceCheckpoint)

## Event Types & Schemas (if relevant)

- SyncEvent payloads are defined in `events.md`.
- HowdyEvent is defined in `presence.md`.
- NewspaperEvent is defined in `presence.md`.
- LedgerRequest/Response are legacy social-only sync (not SyncEvents).

## Algorithms

- `hey_there` emission is rate-limited to once per 5 seconds.
- On connect, a random 0-5s jitter is applied before the first `hey_there`.
- MQTT topics are subscribed with retry (max 3 attempts).

## Failure Modes

- Messages may be dropped or duplicated (QoS 0).
- Missing `newspaper` events can cause stale status until other signals arrive.
- Legacy ledger sync only returns SocialEvent, not full SyncEvent history.

## Security / Trust Model

- SyncEvents are signed (Ed25519) by the emitter.
- Howdy and Newspaper events include signatures over deterministic content.
- MQTT transport security relies on broker TLS; payload signatures are the main trust layer.

## Test Oracle

- HeyThere/Chau signatures verify. (`identity_crypto_test.go`)
- Howdy signature verification and handling. (`presence_howdy_test.go`)
- Checkpoint topics accept valid proposals/votes. (`checkpoint_test.go`)

## Open Questions / TODO

- Migrate remaining legacy social-only MQTT sync to SyncEvent-based mesh APIs.
