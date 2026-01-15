---
title: HTTP API
description: Public and private HTTP endpoints for state, projections, and control.
---

# HTTP API

The Nara HTTP API provides interfaces for network state retrieval, opinion inspection, and local control. It is divided into **Public UI/API** and **Inspector API**.

## 1. Servers
- **Local Server** (Default `:8080`): Serves Web UI and general API.
- **Mesh Server** (Default `:7433`): Serves peer-authenticated endpoints (see [Mesh HTTP](./mesh-http.md)).

## 2. Interfaces

### State & UI
| Endpoint | Method | Purpose |
| :--- | :--- | :--- |
| `/api.json` | GET | Known naras and raw statuses. |
| `/narae.json` | GET | Aggregated view with derived opinions/clout. |
| `/profile/{name}.json`| GET | Detailed profile: friends, teases, etc. |
| `/events` | GET | **SSE stream** of real-time UI-formatted events. |
| `/metrics` | GET | **Prometheus** metrics. |

### Inspector (Introspection)
| Endpoint | Method | Purpose |
| :--- | :--- | :--- |
| `/api/inspector/events` | GET | Paginated ledger events. |
| `/api/inspector/checkpoints`| GET | Summary of latest verified checkpoints. |
| `/api/inspector/projections`| GET | Current projection states (Online, Clout, Opinions). |
| `/api/inspector/uptime/{subject}`| GET | Reconstructed timeline of status periods. |

### Control & Stash
| Endpoint | Method | Purpose |
| :--- | :--- | :--- |
| `/world/start` | POST | Initiate a [World Postcard](./world-postcards.md). |
| `/api/stash/update`| POST | Update and distribute local stash. |
| `/api/events/import`| POST | Signed batch event import. |

## 3. Algorithms

### Uptime Timeline Derivation
1. **Baseline**: Latest **Checkpoint** `TotalUptime`.
2. **Backfill**: If no checkpoint, use `StartTime` from **Backfill** event.
3. **Interleave**: Process `ONLINE`, `OFFLINE`, `MISSING` events chronologically.
4. **Active**: If currently online, extend final period to `now`.

### Event Import Verification
1. **Proof**: Requires `SHA256(ts + ":" + event_ids)` signed by Nara's soul.
2. **Freshness**: Timestamp must be within ±5m.

## 4. Security
- **Authentication**: Destructive/private operations require soul-based signatures.
- **Auditability**: Inspector API exposes the subjective rationale behind "opinions."
- **Sovereignty**: API only reflects the *local* Nara's world view.

## 5. Test Oracle
- `TestHTTP_ApiJson` / `TestHTTP_InspectorEvents`.
- `TestHTTP_EventImport`: Validity and replay protection.
- `TestHTTP_UptimeTimeline`: Interleaving logic.
- `TestHTTP_MeshAuthMiddleware`: Access gating.
