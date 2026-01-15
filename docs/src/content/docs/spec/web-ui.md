---
title: Web UI
---

# Web UI

The human-readable interface to Nara, providing real-time visualizations of the network, timeline, and internal projections.

## Conceptual Model

| Concept | Rule |
| :--- | :--- |
| **Architecture** | Single Page App (Preact) embedded in the Nara binary. |
| **Serving** | Local-only via the primary HTTP server (Default: port 8080). |
| **Data Flow** | Snapshots via JSON REST APIs; real-time updates via Server-Sent Events (SSE). |

## Views & Features

- **Dashboard**: Local status, uptime, memory usage, and current "mood" (Aura).
- **Network Map**: 2D visualization of peers using Vivaldi coordinates and Barrio markers.
- **Timeline**: Real-time feed of social interactions, presence signals, and pings.
- **Inspector**: Deep-dive tools for auditing the `SyncLedger`, Checkpoints, and Projections.
- **World**: Tracking for active and historical "World Postcard" journeys.

## Interfaces

### 1. REST APIs (JSON)
- `GET /api.json`: Full network snapshot.
- `GET /narae.json`: Summary peer list.
- `GET /social/clout`: Subjective clout rankings.
- `GET /world/journeys`: Journey history.

### 2. Real-time Stream (`GET /events`)
SSE-formatted stream.
**Payload Schema**:
```json
{
  "id": "event-id",
  "service": "social",
  "timestamp": 1700000000000000000,
  "icon": "👋",
  "text": "Human-readable summary",
  "detail": "Context (e.g., 'via mesh')"
}
```

## Logic

- **Event Transformation**: Raw `SyncEvent`s are mapped to UI formats (e.g., `hey-there` → `👋`).
- **Deduplication**: The UI uses Event IDs to prevent redundant entries in the timeline.
- **Auto-Reconnect**: The SSE client automatically attempts to re-establish dropped connections.

## Security
- **Local Access**: Endpoints are unauthenticated, assuming local or firewalled network access.
- **Read-Only**: Most actions are non-mutating, except for Stash recovery and World journey initiation.

## Test Oracle
- **Structure**: Verify JSON responses match expected schemas.
- **Streaming**: Verify SSE heartbeats and event delivery. (`http_ui.go`)
