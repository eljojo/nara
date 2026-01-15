---
title: Web UI
---

# Web UI

## Purpose

The Web UI provides a human-readable interface to the Nara network. It runs locally
on each node and offers visualizations of the network, timeline, and internal state.

## Conceptual Model

- **Local-Only**: The UI is served by the local `http_server` on port 8080 (default).
- **Single Page App**: Implemented as a React/Preact app embedded in the binary.
- **SSE Driven**: Real-time updates (events, status) are pushed via Server-Sent Events.

## External Behavior

- **Dashboard**: Shows local status (online, uptime, memory, mood).
- **Network Map**: Visualizes peers using Vivaldi coordinates.
- **Timeline**: Displays a feed of recent social events (teases, observations).
- **Inspector**: Deep dive into ledger events, checkpoints, and projections.
- **World**: Tracking for world postcards and journeys.

## Interfaces

### HTTP Endpoints
- `GET /`: Serves the SPA (`inspector.html`).
- `GET /events`: SSE stream for real-time updates.
- `GET /api.json`: Detailed network state.
- `GET /narae.json`: Summary list of all known naras.
- `GET /network/map`: Coordinate data for visualization.
- `GET /social/recent`: Recent social events.
- `GET /social/clout`: Current clout scores.
- `GET /social/teases`: Tease counts.
- `GET /world/journeys`: Active and completed journeys.

### SSE Events
Streamed from `/events`.
- **Types**: `social`, `ping`, `observation`, `hey_there`, `chau`, `seen`.
- **Payload**:
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

## Event Types & Schemas

The UI consumes transformed events. Raw `SyncEvent`s are processed into UI-friendly formats:
- **Icons**: Derived from event type (e.g., `👋` for `hey_there`, `😈` for `tease`).
- **Text**: Human-readable summary.
- **Detail**: Context (e.g., "via mesh", "RTT 45ms").

## Algorithms

### Clout Visualization
- Fetches clout scores from `CloutProjection`.
- Displays top naras sorted by score.

### Network Map
- Uses `NetworkCoordinate` (Vivaldi) data from `/network/map`.
- Projects 2D coordinates + Height into a visual graph.
- Self is centered or highlighted.

### Timeline
- Merges `recent` social events with real-time SSE updates.
- Dedupes events by ID to prevent stutter.

## Failure Modes

- **Connection Lost**: SSE stream disconnects; UI shows "Disconnected" badge. Auto-reconnects.
- **Empty State**: If ledger is empty (fresh boot), timelines show placeholder.

## Security / Trust Model

- **Local Trust**: The UI assumes it is talking to a trusted local node.
- **No Auth**: Endpoints are unauthenticated (bound to localhost or protected by network firewall).

## Test Oracle

- **Endpoints**: JSON endpoints return valid structures. (`http_ui_test.go` - theoretical)
- **SSE**: Stream sends initial connection event and subsequent updates. (`http_ui.go`)

## Open Questions / TODO

- **Write Actions**: Currently limited to `Stash` recovery and `World` journey start. More control actions could be added.
