# Web UI

## Purpose

The web UI is the primary human interface to Nara.
It visualizes the network map, timelines, projections, and world journeys.

## Conceptual Model

Entities:
- Single Page App (SPA) served as `inspector.html`.
- Views: Home, Map, Postcards, Timeline, Projections, Profile, Event Detail.
- Inspector API: read-only data feeds for the UI.

Key invariants:
- The UI is static and embedded into the binary at build time.
- All routes are client-side; the server serves the same HTML for SPA paths.

## External Behavior

- `/` serves the SPA root and navigation shell.
- Named routes render specific views and fetch corresponding JSON data.
- `/docs/` serves static documentation built by the docs pipeline.

## Interfaces

SPA routes:
- `/` or `/home`: Home view.
- `/map`: Network map view.
- `/postcards`: World journeys view.
- `/timeline` and `/events`: Timeline view.
- `/events/{id}`: Event detail view.
- `/projections`: Projections explorer.
- `/projections/uptime/{subject}`: Uptime timeline.
- `/nara/{name}`: Nara profile view.

Linked static pages:
- `/docs/`: documentation site.
- `/stash.html`, `/resources.html`: static pages from UI assets.

Primary data sources:
- `GET /api.json`, `GET /narae.json`.
- `GET /profile/{name}.json`.
- `GET /network/map`.
- `GET /events` (SSE).
- Inspector endpoints under `/api/inspector/*`.
- World journey endpoints under `/world/*`.

## Event Types & Schemas (if relevant)

- UI uses the SSE `icon/text/detail` fields from event payloads.
- Profile and projections use `NaraStatus` and projection-specific JSON payloads.

## Algorithms

- SPA routing uses path-prefix matching (e.g., `/timeline`, `/events`).
- The network map renders coordinates when available and clusters nodes without coordinates.

## Failure Modes

- If API endpoints are unavailable, views may render empty placeholders.
- If docs are not built, `/docs/` may return 404.

## Security / Trust Model (if relevant)

- UI endpoints are unauthenticated; trust is derived from local server access.

## Test Oracle

- UI route handlers map to SPA routes and inspector endpoints. (`http_server.go`, `nara-web/src/app.jsx`)

## Open Questions / TODO

- None.
