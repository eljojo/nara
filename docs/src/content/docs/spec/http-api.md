---
title: HTTP API
---

# HTTP API

Exposes state, projections, and control endpoints for UI and tooling.

## Servers
- **Local Server**: Serves UI and introspection APIs (`serveUI=true`).
- **Mesh Server**: Peer-to-peer authenticated endpoints (see [Mesh HTTP](./mesh-http.md)).

## Interfaces

### 1. State & UI APIs
| Endpoint | Description |
| :--- | :--- |
| `GET /api.json` | Snapshot of all known naras. |
| `GET /narae.json` | Flattened status, clout, and memory metrics. |
| `GET /profile/{name}.json` | Rich profile (events, friends, teases). |
| `GET /events` | SSE stream of UI-formatted events. |
| `GET /metrics` | Prometheus metrics (uptime, restarts, events, buzz). |

### 2. Inspector API (Introspection)
| Endpoint | Purpose |
| :--- | :--- |
| `GET /api/inspector/events` | Filtered/paginated event list. Query: `service`, `subject`, `after`. |
| `GET /api/inspector/checkpoints` | Latest checkpoint summary per subject. |
| `GET /api/inspector/projection/{type}/{name}` | Specific projection detail. |
| `GET /api/inspector/uptime/{subject}` | Derived uptime timeline. |

### 3. Event Import
`POST /api/events/import` allows batch event injection.
- **Payload**: `{"events": [...], "ts": 1700000000, "sig": "base64"}`
- **Auth**: Ed25519 signature of `SHA256(ts:event_ids)` using the node's own soul.
- **Constraints**: `ts` must be within ±5 minutes of server time.

### 4. Stash Control
- `GET /api/stash/status`: Owner stash summary.
- `POST /api/stash/update`: Update owner stash JSON.
- `POST /api/stash/recover`: Trigger stash recovery from confidants.
- `GET /api/stash/confidants`: List peers storing our stash.

## Logic & Constraints
- **Filtering**: Inspector filters support `service`, `subject`, `after`, `before`, `limit`, and `offset`.
- **Duplicates**: Imported events are deduplicated by Event ID via the ledger.

## Security
- **Local Access**: UI/Inspector assumes local/trusted network access.
- **Sovereignty**: Modification (import/stash update) requires soul-based authentication.
- **Integrity**: SSE stream provides real-time visibility into ledger activity.

## Test Oracle
- **Authentication**: `integration_backup_test.go`
- **Data Integrity**: Verify JSON snapshots match projection states.
