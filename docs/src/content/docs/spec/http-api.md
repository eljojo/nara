---
title: HTTP API
description: Public and private HTTP endpoints for state, projections, and control.
---

# HTTP API

The Nara HTTP API provides a comprehensive interface for retrieving network state, inspecting derived opinions, and controlling the local Nara. It is divided into **Public UI/API** (serving the web dashboard) and **Inspector API** (providing deep visibility into the ledger and projections).

## Purpose
- Drive the web-based dashboard and visualization tools.
- Provide introspection for debugging consensus and history.
- Enable integration with external monitoring (Prometheus).
- Support state recovery through event imports.

## Servers
- **Local Server** (Default: `:8080`): Serves the Web UI and all API endpoints. Typically restricted to localhost or a private network.
- **Mesh Server** (Default: `:7433`): Serves only the peer-to-peer authenticated endpoints (see [Mesh HTTP](./mesh-http.md)).

## Interfaces

### 1. State & UI APIs
| Endpoint | Method | Purpose |
| :--- | :--- | :--- |
| `/api.json` | `GET` | Snapshot of all known naras and their raw statuses. |
| `/narae.json` | `GET` | Aggregated view of all naras, enriched with derived opinions and clout. |
| `/profile/{name}.json`| `GET` | Detailed profile for a Nara, including best friends and recent teases. |
| `/events` | `GET` | **SSE stream** of real-time events formatted for the UI. |
| `/metrics` | `GET` | **Prometheus** metrics for naras, events, and world journeys. |

### 2. Inspector API (Introspection)
These endpoints provide deep access to the local Nara's hazy memory.

| Endpoint | Method | Purpose |
| :--- | :--- | :--- |
| `/api/inspector/events` | `GET` | Filtered and paginated event list. |
| `/api/inspector/checkpoints`| `GET` | Summary of the latest verified checkpoint for every Nara. |
| `/api/inspector/checkpoint/{subject}`| `GET` | Detailed voter breakdown and verification status for a checkpoint. |
| `/api/inspector/projections`| `GET` | Current state of all projections (Online, Clout, Opinions). |
| `/api/inspector/uptime/{subject}`| `GET` | Derived timeline of online/offline periods. |

### 3. Stash & World Control
| Endpoint | Method | Purpose |
| :--- | :--- | :--- |
| `/world/start` | `POST` | Initiate a new [World Postcard](./world-postcards.md). |
| `/api/stash/status`| `GET` | Current storage metrics and confidant list. |
| `/api/stash/update`| `POST` | Update the local Nara's stash data and trigger distribution. |
| `/api/events/import`| `POST` | Authenticated batch import of events (e.g., from backup). |

## Algorithms

### 1. Event UI Formatting
Events in the `/events` SSE stream are enriched with a `ui_format` object:
- `icon`: An emoji representing the event type (e.g., ✨ for first-seen, 🔄 for restart).
- `text`: A human-readable summary.
- `detail`: Extra context (e.g., "via zine" or a tease message).

### 2. Uptime Timeline Derivation
The `/api/inspector/uptime/{subject}` endpoint reconstructs a subject's history:
1. **Baseline**: Starts with the `TotalUptime` from the latest **Checkpoint**.
2. **Backfill**: If no checkpoint, uses the `StartTime` from a **Backfill** event.
3. **Events**: Interleaves `ONLINE`, `OFFLINE`, and `MISSING` events to build discrete time periods.
4. **Ongoing**: If the subject is currently online, the final period extends to the current time.

### 3. Event Import Verification
The `/api/events/import` endpoint requires proof of ownership:
1. **Request**: Contains a list of events, a timestamp, and a signature.
2. **Timestamp**: Must be within ±5 minutes of the server's clock to prevent replays.
3. **Signature**: Must be a valid Ed25519 signature of `SHA256(timestamp + ":" + list_of_event_ids)` using the Nara's own private key.

## Failure Modes
- **SSE Connection Failure**: Clients should implement automatic reconnection; the server sends a `connected` event on join to confirm state.
- **Import Conflicts**: Duplicate events (by ID) are silently ignored during import.
- **Stale Inspector Data**: Projections may lag slightly behind the ledger; the Inspector API calls `RunOnce()` on projections to ensure data is fresh.

## Security / Trust Model
- **Authentication**: Destructive or private operations (Import, Stash Update) require soul-based authentication.
- **Transparency**: The Inspector API allows full visibility into why a Nara holds a certain "opinion," making the consensus process auditable.
- **Local Sovereignty**: The HTTP API only exposes the *local* Nara's subjective view of the world.

## Test Oracle
- `TestHTTP_ApiJson`: Verifies that the Nara list is correctly marshaled.
- `TestHTTP_InspectorEvents`: Validates pagination and filtering logic for the event store.
- `TestHTTP_EventImport`: Ensures that only signed, timestamp-valid imports are accepted.
- `TestHTTP_UptimeTimeline`: Checks that checkpoints and events are correctly interleaved to form a timeline.
