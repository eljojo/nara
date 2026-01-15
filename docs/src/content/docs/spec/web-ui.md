---
title: Web UI
description: Real-time visualization and inspection dashboard for Nara.
---

# Web UI

The Web UI is an embedded Single Page App (Preact) providing real-time visualizations of network state, timeline, and projections.

## 1. Conceptual Model
- **Architecture**: Embedded SPA (Preact) in the Go binary.
- **Serving**: Local-only by default on port `8080`.
- **Data Flow**: REST (JSON) snapshots + Server-Sent Events (SSE) for real-time updates.

## 2. Views
- **Dashboard**: Local status, memory usage, and Aura.
- **Network Map**: 2D Vivaldi coordinate visualization with Barrio markers.
- **Timeline**: Real-time event feed (interactions, presence, pings).
- **Inspector**: Auditing tools for `SyncLedger`, Checkpoints, and Projections.
- **World**: Journey tracking.

## 3. Interfaces

### REST Endpoints
- `GET /api.json`: Full network snapshot.
- `GET /narae.json`: Summary peer list.
- `GET /social/clout`: Subjective rankings.

### Real-time Stream (`GET /events`)
SSE-formatted stream.
```json
{
  "id": "event-id",
  "service": "social",
  "timestamp": 1700000000000000000,
  "icon": "👋",
  "text": "Human-readable summary",
  "detail": "Context"
}
```

## 4. Logic & Security
- **Transformation**: `SyncEvent` mapped to UI format (e.g., `hey-there` → 👋).
- **Resilience**: Client-side deduplication and auto-reconnecting SSE.
- **Access**: Local/firewalled access assumed; non-mutating except for Stash/World triggers.

## 5. Test Oracle
- `TestHTTP_UI_JSON`: Schema validation.
- `TestHTTP_UI_SSE`: Heartbeat and event delivery.
