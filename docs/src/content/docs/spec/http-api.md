---
title: HTTP API
description: Public, private, and introspection endpoints in the nara network.
---

The HTTP API is the primary interface for interacting with a nara, providing endpoints for status updates, real-time events, network introspection, and peer communication.

## 1. Purpose
- Provide a standardized interface for the [Web UI](/docs/spec/web-ui/) and external tools.
- Enable high-bandwidth peer-to-peer data exchange via [Mesh HTTP](/docs/spec/mesh-http/).
- Facilitate network debugging and auditing through "Inspector" endpoints.
- Allow users to trigger manual actions (e.g., starting a journey or updating a stash).

## 2. Conceptual Model
- **Local Server**: Serves the UI and general-purpose JSON APIs, typically on port 8080.
- **Mesh Server**: Serves peer-authenticated endpoints on port 7433.
- **Subjective View**: Every API response reflects the local nara's specific view of the network (Hazy Memory).
- **Introspection**: Deep-dive endpoints that reveal the internal state of projections and the ledger.

### Invariants
1. **Consistency**: JSON responses MUST follow the documented schemas to ensure compatibility with the Web UI.
2. **Security Gating**: Sensitive or destructive endpoints MUST be protected by mesh authentication or restricted to local access.
3. **Availability**: The API SHOULD remain responsive even during heavy sync or gossip activity.

## 3. External Behavior
- The API is the source of truth for the local dashboard.
- Real-time updates are delivered via a Server-Sent Events (SSE) stream.
- Developers and operators use the Inspector API to debug consensus issues or verify event ingestion.

## 4. Interfaces

### General API (Default: Port 8080)
- `GET /api.json`: A full snapshot of the neighbourhood and local status.
- `GET /narae.json`: A lightweight summary of all known peers.
- `GET /profile/{name}.json`: Detailed information about a specific nara.
- `GET /events`: SSE stream of real-time UI-formatted events.

### Inspector API
- `GET /api/inspector/events`: Paginated access to the `SyncLedger`.
- `GET /api/inspector/checkpoints`: Summary of the latest verified checkpoints.
- `GET /api/inspector/projections`: Current internal state of all projections.
- `GET /api/inspector/uptime/{subject}`: The derived uptime timeline for a peer.

### Mesh API (Default: Port 7433)
See [Mesh HTTP](/docs/spec/mesh-http/) for authenticated peer endpoints.

## 5. Event Types & Schemas
The `/events` SSE stream uses a specific JSON format:
```json
{
  "id": "event-id",
  "service": "social",
  "timestamp": 1700000000000000000,
  "icon": "👋",
  "text": "Human-readable summary",
  "detail": "Additional context"
}
```

## 6. Algorithms

### Uptime Timeline Derivation
Used by the `/api/inspector/uptime/` endpoint:
1. **Start with Baseline**: Use the `TotalUptime` and `LastRestart` from the latest valid [Checkpoint](/docs/spec/checkpoints/).
2. **Replay Events**: Process all `ONLINE`, `OFFLINE`, and `MISSING` events in the ledger since the checkpoint.
3. **Calculate Intervals**: Sum the durations where the nara was in an `ONLINE` state.
4. **Project to Now**: If the nara is currently `ONLINE`, add the duration since the last recorded state change.

## 7. Failure Modes
- **SSE Disconnection**: Clients must handle reconnections gracefully.
- **Stale Snapshots**: High-frequency network changes may make `api.json` feel slightly behind the SSE stream.
- **OOM on Large Ledgers**: The Inspector API must use pagination to avoid crashing the nara when querying massive ledgers.

## 8. Security / Trust Model
- **Local-Only**: Mutating endpoints (like `/api/events/import`) are restricted to local/authenticated access.
- **Mesh Auth**: All peer-to-peer mesh calls are verified using Ed25519 signatures.
- **Privacy**: The API reveals the nara's subjective view but never its private soul seed.

## 9. Test Oracle
- `TestHTTP_ApiJson`: Validates the structure and content of the neighbourhood snapshot.
- `TestHTTP_InspectorEvents`: Ensures that ledger events are correctly paginated and returned.
- `TestHTTP_UptimeTimeline`: Confirms that the timeline reconstruction logic correctly handles interleaved events.
- `TestHTTP_SSE_Heartbeat`: Verifies that the event stream remains active and delivers formatted events.

## 10. Open Questions / TODO
- Add GraphQL support for more flexible neighbourhood queries.
- Implement rate-limiting for the public API endpoints.
