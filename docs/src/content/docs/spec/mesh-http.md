---
title: Mesh HTTP
---

# Mesh HTTP

## Purpose

Mesh HTTP is the authenticated, point-to-point transport used for gossip,
boot recovery sync, direct messaging, and stash exchange over tsnet/Headscale.

## Conceptual Model

- All mesh HTTP requests (except `/ping`) are signed with Ed25519 headers.
- Responses are also signed and verified when the caller chooses to verify.
- Endpoints accept JSON and return JSON.

Key invariants:
- Mesh auth uses request headers, not request body fields.
- `/ping` is unauthenticated for latency measurement and discovery.

## External Behavior

- When tsnet is enabled, each nara hosts an HTTP server on the mesh port (7433).
- Peers communicate using mesh IPs (100.64.x.x), not DNS.
- Unknown senders may be discovered via `/ping` before verification.

## Interfaces

Mesh authentication headers (required for all endpoints except `/ping`):
- `X-Nara-Name`: sender name
- `X-Nara-Timestamp`: Unix millis
- `X-Nara-Signature`: Base64 Ed25519 signature

Request signature:
- sign(name + timestamp + method + path)

Response signature:
- sign(name + timestamp + base64(sha256(body)))

Endpoints:

Sync and gossip:
- `POST /events/sync` -> SyncResponse (signed)
- `POST /gossip/zine` -> Zine (bidirectional exchange)
- `POST /dm` -> {success, from}

Presence/coords:
- `GET /ping` -> {t, from, public_key, mesh_ip}
- `GET /coordinates` -> {name, coordinates}

Stash:
- `POST /stash/store`
- `DELETE /stash/store`
- `POST /stash/retrieve`
- `POST /stash/push`

Checkpoint and import:
- `GET /api/checkpoints/all` (no mesh auth)
- `POST /api/events/import` (soul-based auth, not mesh auth)

## Event Types & Schemas (if relevant)

- `/events/sync`: SyncRequest -> SyncResponse (see `sync-protocol.md`).
- `/gossip/zine`: Zine (see `zines.md`).
- `/dm`: SyncEvent (signed) in request body.
- `/stash/*`: Stash request/response types in `stash.md`.
- `/api/events/import`: EventImportRequest (see `mesh-http` section below).

## Algorithms

Mesh auth verification:
- Reject if headers missing or timestamp outside +/-30s.
- Verify signature against sender public key.
- If sender unknown, attempt discovery via `/ping` and retry verification.

DM validation:
- Require a signed SyncEvent.
- Verify against the emitter’s public key from the neighbourhood.
- Merge into ledger (social events filtered by personality).

Events import (owner-only):
- Request is signed with the local soul-derived keypair.
- Signing data is SHA256("{ts}:" + event IDs).
- Timestamp must be within +/-5 minutes.

## Failure Modes

- Unknown public keys -> mesh auth rejection until discovery succeeds.
- Clock skew beyond 30s -> mesh auth failure.
- Invalid DM signatures -> 403.

## Security / Trust Model

- Mesh auth provides request authenticity and freshness.
- Payload signatures (SyncEvent, Zine) provide end-to-end authenticity.
- `/ping` is intentionally unauthenticated and should not be trusted for truth.

## Test Oracle

- Mesh auth header signing/verification. (`transport_mesh_auth_test.go`)
- Sync events via mesh are verified and merged. (`integration_events_test.go`)
- Zine exchanges accept valid signatures. (`integration_gossip_test.go`)

## Open Questions / TODO

- Standardize response signature verification in MeshClient.
