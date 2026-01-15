---
title: HTTP API
---

# HTTP API

## Purpose

The HTTP API exposes current state, projections, and control endpoints for UI and tooling.
It is the primary interface for the web UI and for importing events.

## Conceptual Model

Entities:
- **Local HTTP Server**: Serves UI endpoints and JSON APIs (e.g. `/api.json`).
- **Mesh HTTP Server**: Serves authenticated peer-to-peer endpoints (see `mesh-http.md`).
- **Inspector API**: Read-only introspection for events and projections.

Key invariants:
- UI endpoints are only mounted on the local HTTP server (`serveUI=true`).
- Mesh endpoints are mounted on **both** local and mesh servers.
- Responses are derived from the live in-memory state and the sync ledger.

## External Behavior

- JSON endpoints provide snapshots of status, profiles, events, and projections.
- Metrics endpoint provides Prometheus-compatible metrics.
- Event import requires soul-based authentication.

## Interfaces

### Core JSON Endpoints
- `GET /api.json`: List of all known naras with status + observation snapshot.
- `GET /narae.json`: List of naras with flattened status, clout, and memory stats.
- `GET /profile/{name}.json`: Rich profile for a single nara (includes event stats, friends, recent teases).
- `GET /status/{name}.json`: Raw `NaraStatus` for a single nara.

### Metrics
- `GET /metrics`: Prometheus text format metrics.
  - `nara_info`, `nara_online`, `nara_buzz`, `nara_chattiness`
  - `nara_last_seen`, `nara_last_restart`, `nara_start_time`, `nara_uptime_seconds`, `nara_restarts_total`
  - `nara_personality`, `nara_memory_*`, `nara_goroutines`, `nara_gc_cycles_total`
  - `nara_events_total`, `nara_teases_given_total`, `nara_teases_received_total`
  - `nara_journeys_completed_total`

### Events
- `GET /events`: SSE stream of UI-formatted events.
- `POST /api/events/import`: Import events with soul-based authentication.

### Stash (UI-Facing)
- `GET /api/stash/status`: Summary of owner stash state.
- `POST /api/stash/update`: Update owner stash JSON.
- `POST /api/stash/recover`: Request recovery of owner stash.
- `GET /api/stash/confidants`: List confidants storing our stash.

### Inspector API
- `GET /api/inspector/events`: Filtered, paginated event list.
  - Query: `service`, `subject`, `after`, `before`, `limit`, `offset`.
- `GET /api/inspector/event/{id}`: Full event detail by ID.
- `GET /api/inspector/checkpoints`: Latest checkpoint summary per subject.
- `GET /api/inspector/checkpoint/{subject}`: Checkpoint history for one subject.
- `GET /api/inspector/projections`: List available projections.
- `GET /api/inspector/projection/{type}/{subject}`: Projection detail.
- `GET /api/inspector/uptime/{subject}`: Derived uptime timeline.

### Checkpoint Sync (Deprecated)
- `GET /api/checkpoints/all`: All checkpoint events for boot recovery (legacy).
  - *Deprecated*: Use `/events/sync` with `mode=page` instead.

### Data Structures

**`EventImportRequest`**:
```json
{
  "events": [ ... ], // List of SyncEvents
  "ts": 1700000000,  // Unix timestamp (seconds)
  "sig": "base64-sig" // Ed25519 signature of sha256(ts:event_ids)
}
```

## Event Types & Schemas

`POST /api/events/import` Body:
- `events`: List of `SyncEvent` objects.
- `ts`: Unix timestamp seconds (for replay protection).
- `sig`: Base64 Ed25519 signature of `sha256(ts:event_ids)`.

`/events` SSE Payloads:
```json
{
  "id": "event-id",
  "service": "social",
  "timestamp": 1700000000000000000,
  "emitter": "nara-name",
  "icon": "👋",
  "text": "nara-name says hi",
  "detail": "via mqtt"
}
```

## Algorithms

### Soul-Authenticated Import
1. **Timestamp Check**: Rejects if `ts` is older than 5 minutes or >5 minutes in the future.
2. **Signature Verification**: Verifies `sig` using this nara's own public key (same soul).
3. **Ingestion**: Adds valid events to the ledger; duplicates are ignored.

### Inspector Filtering
- Filters by `service`, `subject`, and timestamp range.
- Sorts newest first.
- Applies `offset` and `limit`.

## Failure Modes

- **Invalid JSON**: Returns 400.
- **Not Found**: Unknown targets return 404 (`/profile`, `/status`, inspector details).
- **Auth Failure**: Invalid soul signature on import returns 403.

## Security / Trust Model

- **Local Access**: UI endpoints assume local access (unauthenticated).
- **Event Import**: Restricted to the owner via cryptographic signature (soul-based).
- **Read-Only**: Inspector API provides visibility but cannot mutate state.

## Test Oracle

- **Import Auth**: Reject invalid signatures, accept valid soul-signed requests. (`integration_backup_test.go`)
- **Inspector**: Endpoints return correct filtered/sorted data. (`http_inspector.go` - manual verification or integration tests)

## Open Questions / TODO

- **Deprecation**: Remove `/api/checkpoints/all` once all clients use `/events/sync`.
