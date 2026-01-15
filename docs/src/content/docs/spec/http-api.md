# HTTP API

## Purpose

The HTTP API exposes current state, projections, and control endpoints for UI and tooling.
It is the primary interface for the web UI and for importing events.

## Conceptual Model

Entities:
- Local HTTP server: serves UI endpoints and JSON APIs.
- Mesh HTTP server: serves authenticated peer-to-peer endpoints (see mesh-http.md).
- Inspector API: read-only introspection for events and projections.

Key invariants:
- UI endpoints are only mounted on the local HTTP server (serveUI=true).
- Mesh endpoints are mounted on both local and mesh servers.
- Responses are derived from the live in-memory state and the sync ledger.

## External Behavior

- JSON endpoints provide snapshots of status, profiles, events, and projections.
- Metrics endpoint provides Prometheus-compatible metrics.
- Event import requires soul-based authentication.

## Interfaces

Core JSON endpoints:
- `GET /api.json`: list of naras with status + observation snapshot.
- `GET /narae.json`: list of naras with flattened status and clout.
- `GET /profile/{name}.json`: rich profile for a single nara.
- `GET /status/{name}.json`: raw `NaraStatus` for a single nara.

Metrics:
- `GET /metrics`: Prometheus text format metrics for all known naras and local ledger.

Events:
- `GET /events`: SSE stream of UI-formatted events.
- `POST /api/events/import`: import events with soul-based authentication.

Stash (UI-facing):
- `GET /api/stash/status`: summary of owner stash state.
- `POST /api/stash/update`: update owner stash JSON.
- `POST /api/stash/recover`: request recovery of owner stash.
- `GET /api/stash/confidants`: list confidants storing our stash.

Inspector API:
- `GET /api/inspector/events`: filtered, paginated event list.
  Query: `service`, `subject`, `after`, `before`, `limit`, `offset`.
- `GET /api/inspector/event/{id}`: full event detail by ID.
- `GET /api/inspector/checkpoints`: latest checkpoint summary per subject.
- `GET /api/inspector/checkpoint/{subject}`: checkpoint history for one subject.
- `GET /api/inspector/projections`: list available projections.
- `GET /api/inspector/projection/{type}/{subject}`: projection detail.
- `GET /api/inspector/uptime/{subject}`: derived uptime timeline.

Checkpoint sync:
- `GET /api/checkpoints/all`: all checkpoint events for boot recovery.

Public structs:
- `EventImportRequest`, `EventImportResponse`.

## Event Types & Schemas (if relevant)

`POST /api/events/import` body:
- `events`: list of SyncEvent objects.
- `ts`: Unix timestamp seconds (for replay protection).
- `sig`: Base64 Ed25519 signature of `sha256(ts:event_ids)`.

`/events` SSE payloads:
- `id`, `service`, `timestamp`, `emitter`, `icon`, `text`, `detail`.

## Algorithms

Soul-authenticated import:
- Rejects requests if timestamp is older than 5 minutes or in the future by >5 minutes.
- Signature is verified using this nara's own public key (same soul).
- Each imported event is verified; duplicates are ignored.

Inspector filtering:
- Filters by service, subject, and timestamp range.
- Sorts newest first, then applies offset/limit.

## Failure Modes

- Invalid JSON returns 400 for JSON endpoints.
- Unknown targets return 404 (profile/status/inspector detail).
- Invalid soul auth returns 403 for event import.

## Security / Trust Model (if relevant)

- Event import is restricted to the owner by soul-based signatures.
- Inspector API is read-only and unauthenticated on the local server.

## Test Oracle

- Event import rejects invalid signatures and accepts valid soul-signed requests. (`integration_backup_test.go`)
- Inspector endpoints return event data and projection data with filters applied. (`http_test.go`)

## Open Questions / TODO

- None.
