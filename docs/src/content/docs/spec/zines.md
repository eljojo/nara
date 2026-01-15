# Zines

## Purpose

Zines are compact, signed bundles of recent events exchanged peer-to-peer
over mesh HTTP to spread memory without a central broker.

## Conceptual Model

- A zine is a snapshot of recent SyncEvents.
- Zines are exchanged bidirectionally via `/gossip/zine`.
- Zines are signed by the sender’s Ed25519 key.

Key invariants:
- Zines only include recent events (last ~5 minutes).
- Signatures cover the event IDs, not full payloads.

## External Behavior

- Gossip runs every 30-300 seconds (chattiness-based).
- Each round selects 3-5 mesh-enabled peers (short memory: 1-2).
- Exchange is HTTP POST with mesh auth headers.

## Interfaces

Zine JSON shape:
- `from`: sender name
- `created_at`: Unix seconds
- `events`: []SyncEvent
- `signature`: Base64 Ed25519 signature

Endpoint:
- `POST /gossip/zine` (mesh auth required)

## Event Types & Schemas (if relevant)

- Zines only carry SyncEvents. See `events.md` for payload schemas.

## Algorithms

Zine creation:
- Collect all ledger events with `Timestamp >= now - 5 minutes`.
- If none, skip gossip.
- Sign SHA256("{from}:{created_at}:" + event IDs).

Zine verification:
- If the sender’s public key is known, verify the signature.
- If unknown, accept without verification (identity discovery comes later).

Exchange:
- Sender posts its zine, receiver merges events and returns its own zine.
- Receiver marks sender as online unless a matching `chau` event is present.

## Failure Modes

- Empty zines are not sent (sender logs "No events to gossip").
- Invalid signatures from known peers cause the zine to be rejected.
- Missing peers reduces propagation but does not block the system.

## Security / Trust Model

- Zine signatures authenticate the sender and the event IDs they chose to include.
- Event signatures still provide the primary authenticity for each payload.

## Test Oracle

- Zine signatures verify against public keys. (`integration_gossip_test.go`)
- Zine exchange merges events into the ledger. (`integration_gossip_test.go`)

## Open Questions / TODO

- Add explicit tests for zine sampling windows and peer selection.
