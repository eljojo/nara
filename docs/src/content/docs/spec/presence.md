# Presence

## Purpose

Presence provides the real-time liveness signals: who is here, who left, and
who can vouch for whom.

## Conceptual Model

Presence is communicated through:
- `hey_there` (join/identity announcement)
- `howdy` (direct response with neighbor hints)
- `chau` (graceful shutdown)
- `newspaper` (status heartbeat)

Key invariants:
- Presence signals are signed.
- Presence events are treated as evidence, not absolute truth.

## External Behavior

- On connect, naras broadcast `hey_there` (MQTT + gossip SyncEvent).
- Peers respond with up to 10 `howdy` messages to help discovery.
- On shutdown, naras broadcast `chau` (MQTT + gossip SyncEvent).
- Naras periodically broadcast a `newspaper` status message.

## Interfaces

MQTT topics:
- `nara/plaza/hey_there` -> SyncEvent (ServiceHeyThere)
- `nara/plaza/howdy` -> HowdyEvent
- `nara/plaza/chau` -> SyncEvent (ServiceChau)
- `nara/newspaper/{name}` -> NewspaperEvent

HowdyEvent:
- `from`, `to`, `seq`, `you`, `neighbors`, `me`, `signature`
- Signature message: `howdy:{from}:{to}:{seq}`

NewspaperEvent:
- `status` (NaraStatus) + `signature`
- Signature is over the raw JSON of `status`.

## Event Types & Schemas (if relevant)

- `hey_there` and `chau` are SyncEvents with payloads:
  - `HeyThereEvent`: `{from, public_key, mesh_ip, id, signature}`
  - `ChauEvent`: `{from, public_key, id, signature}`
- `howdy` is not a SyncEvent (MQTT-only message).
- `newspaper` is not a SyncEvent (MQTT-only status heartbeat).

## Algorithms

Hey-there:
- Emitted on connect with 0-5s jitter.
- Rate limited to once every 5 seconds.
- SyncEvent signature is the attestation; payload validation only checks required fields.

Howdy coordinator:
- On receiving `hey_there`, start a 0-3s random delay.
- At most 10 responses are sent per target (short memory: 5 neighbors per response).
- Each howdy includes:
  - `you`: the sender’s observation of the target (includes StartTime if known)
  - `neighbors`: up to 10 peers not already mentioned
  - `me`: sender’s own NaraStatus

Chau:
- Emitted on graceful shutdown.
- In sync processing, ignored if there is a more recent `hey_there` for the same nara.
- During boot recovery, chau events are ignored until projections sync to avoid
  stale offline marks from backfilled history.

Newspaper:
- Sent every 30-300 seconds (chattiness-based).
- Broadcasts a slim status (no observations), plus event counts and stash metrics.

Start time recovery:
- Howdy responses provide `you.StartTime` votes with sender uptime weights.
- Consensus is applied after boot recovery using a 60s tolerance window.

## Failure Modes

- Missing howdy responses can delay discovery.
- Stale chau events can mark peers offline unless newer hey_there exists.
- Invalid signatures are logged and ignored (when verification is possible).

## Security / Trust Model

- `hey_there` and `chau` use embedded public keys for bootstrap verification.
- `howdy` verifies against `me.public_key` in the message.
- `newspaper` verifies against `status.public_key` or known neighbor keys.

## Test Oracle

- HeyThere/Chau signature verification. (`identity_crypto_test.go`)
- Howdy discovery and signatures. (`presence_howdy_test.go`)
- Chau handling and offline transitions. (`integration_events_test.go`)

## Open Questions / TODO

- Add explicit replay protection for howdy and newspaper.
