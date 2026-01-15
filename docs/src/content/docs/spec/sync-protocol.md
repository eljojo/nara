---
title: Sync Protocol
---

# Sync Protocol

## Purpose

Sync answers "what did I miss?" by fetching events from peers and merging them
into the local SyncLedger. It supports hazy memory, deterministic pagination,
and legacy MQTT fallback.

## Conceptual Model

- `SyncRequest`: Asks for `SyncEvents`.
- `SyncResponse`: Returns `SyncEvents` with optional pagination cursor and signature.
- **Merge Logic**: Idempotent merge (deduplication by Event ID).
- **Hazy Memory**: Peers may only return a sample of events or events they consider "meaningful".

Key invariants:
- **Best Effort**: Sync does not guarantee 100% completeness; gossip fills gaps.
- **Append Only**: Existing events are never mutated.
- **Filtering**: Events are filtered by personality (e.g., ignoring boring teases) and cutoff times (checkpoints).

## External Behavior

### Boot Recovery
When a node starts:
1. **Discovery**: Wait for peers to appear (via `hey_there` or local cache).
2. **Mesh Sync**: Prefer HTTP-over-Mesh (Tailscale/Headscale).
   - Sample Mode: Requests weighted random samples of history.
   - Target Size: ~5k (short memory), ~50k (medium), ~80k (hog).
   - Parallelism: Fetches from multiple peers (up to 10) in round-robin.
3. **MQTT Fallback**: If mesh fails, listen for `social` events on MQTT (partial history only).
4. **Checkpoint Sync**: After event recovery, fetch the consensus checkpoint timeline.

### Background Sync
Every ~30 minutes (±5m jitter):
- Pick 1-2 online neighbors (preferring those with higher memory/uptime).
- Request `recent` events (limit 100).
- Merge new `observation`, `ping`, and `social` events.
- Update projections and Vivaldi coordinates if new data arrives.

## Interfaces

### SyncRequest (JSON)
Sent via HTTP POST `/api/sync`.
```json
{
  "from": "requester-name",
  "services": ["social", "observation"], // Optional filter
  "subjects": ["target-nara"],           // Optional filter
  "mode": "sample | page | recent",      // Defaults to 'recent' if omitted
  "sample_size": 5000,                   // For 'sample' mode
  "limit": 100,                          // For 'recent' mode
  "cursor": "1700000000000000000",       // For 'page' mode (timestamp nanos)
  "page_size": 5000                      // For 'page' mode
}
```

### SyncResponse (JSON)
```json
{
  "from": "responder-name",
  "events": [ ... ],       // List of SyncEvents
  "next_cursor": "...",    // Timestamp for next page
  "ts": 1700000000,        // Response generation time (seconds)
  "sig": "base64-sig"      // Optional signature over content
}
```

### Signing
Response signature covers: `SHA256("{from}:{ts}:" + list_of_event_ids)`.
This authenticates that *this set of events* came from *this peer* at *this time*, preventing tampering in transit.

## Algorithms

### 1. Sample Mode (The "Hazy" Sync)
Used during boot to get a representative view without downloading the whole world.
- **Input**: `sample_size` (max 5000).
- **Priority**:
  - **Critical**: Always include `checkpoint`, `hey_there`, `chau`, and critical `observation`s.
  - **Weighted**: Other events are selected based on:
    - **Recency**: Decay function (30-day half-life).
    - **Relevance**: Higher weight if `from` or `to` matches the requester or responder.
    - **Importance**: Higher weight for `normal` importance over `casual`.
- **Output**: A mix of recent facts and important historical context.

### 2. Page Mode (Deterministic)
Used for deep history traversal.
- **Input**: `cursor` (nanoseconds), `page_size`.
- **Order**: Oldest first (ascending timestamp).
- **Output**: Events with `ts > cursor`.
- **Next Cursor**: Timestamp of the last event in the batch.

### 3. Recent Mode (Catch-up)
Used for UI and background sync.
- **Input**: `limit`.
- **Order**: Newest first (descending timestamp).
- **Output**: The `limit` most recent events.

### Merge Process
When receiving `SyncResponse`:
1. **Discovery**: Detect unknown naras mentioned in payloads.
2. **Identity**: Process `hey_there` / `chau` to update identity/liveness state.
3. **Verification**: Verify event signatures (warn on failure).
4. **Ingestion**: `SyncLedger.AddEvent(event)`:
   - Check dedupe (by ID).
   - Check checkpoint cutoff.
   - Filter by personality (e.g., ignore `casual` spam if `Chill > 80`).
5. **Projections**: If any event was new, trigger `Projections.Trigger()`.

## Failure Modes

- **Missing Peers**: If no peers are reachable via mesh, sync is skipped (eventually relying on real-time gossip).
- **Auth Failure**: HTTP requests rejected (401/403) are logged; syncing continues with other peers.
- **Partial History**: "Sample" mode explicitly drops data; the system is designed to tolerate holes in history.

## Security / Trust Model

- **Transport**: Secured via WireGuard (Tailscale/Headscale) or TLS.
- **Event Integrity**: Each event is self-verifying (signed by emitter).
- **Response Integrity**: The response header is signed by the serving peer, proving they served this specific batch.
- **Trust**: We trust peers to serve *valid* events, but we don't necessarily trust the *completeness* of their response (they might withhold events).

## Test Oracle

- **Pagination**: `page` mode returns contiguous slices of history. (`sync_test.go`)
- **Sampling**: `sample` mode prioritizes critical events. (`sync_sampling_test.go`)
- **Signatures**: `SyncResponse` signature verification passes. (`sync_request.go`)
- **Merge**: Duplicate events are ignored; new events trigger projections. (`integration_events_test.go`)

## Open Questions / TODO

- **Mesh Auth Enforcement**: Currently warnings only; should stricter auth be enforced for sync?
- **Request Signing**: `SyncRequest` is currently unsigned; adding signatures would prevent spoofed requests.
