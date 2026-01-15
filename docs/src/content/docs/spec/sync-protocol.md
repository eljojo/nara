# Sync Protocol

## Purpose

Sync answers "what did I miss?" by fetching events from peers and merging them
into the local SyncLedger. It supports hazy recovery, deterministic pagination,
and legacy MQTT fallback.

## Conceptual Model

- SyncRequest asks for SyncEvents; SyncResponse returns them.
- Multiple modes trade completeness vs. cost.
- Merge is idempotent (dedupe by event ID).

Key invariants:
- Sync is best-effort; missing events are normal.
- Merge never mutates existing events; it only adds new ones.
- Old buggy checkpoints are filtered before ingestion.

## External Behavior

Boot recovery flow:
1. Wait for peers (immediate in gossip mode if already discovered).
2. Prefer mesh HTTP recovery; fallback to MQTT social-only sync.
3. After event recovery, fetch the checkpoint timeline.

Boot recovery (mesh, sample mode):
- short memory: target ~5,000 events, 1,000 per call
- medium memory: target ~50,000 events, 5,000 per call
- hog memory: target ~80,000 events, 5,000 per call
- Calls are round-robin across neighbors with max concurrency 10 (short: 3).

Background sync:
- Every ~30 minutes, fetch recent events from 1-2 peers (mesh only).

## Interfaces

SyncRequest:
- `from` (required)
- `services` (optional filter)
- `subjects` (optional filter)
- `mode`:
  - `sample`: decay-weighted sample (boot recovery)
  - `page`: deterministic pagination
  - `recent`: most recent events for UI
- `sample_size`, `cursor`, `page_size`, `limit` (mode-specific)
- Legacy fields: `since_time`, `slice_index`, `slice_total`, `max_events`

SyncResponse:
- `from`, `events[]`, `next_cursor`, `ts`, `sig`
- `sig` is Base64 Ed25519 over SHA256("{from}:{ts}:" + event IDs)

## Event Types & Schemas (if relevant)

- All responses contain SyncEvents (see `events.md`).
- Legacy MQTT sync uses SocialEvent only (see `social-events.md`).

## Algorithms

Mode: sample (organic hazy memory)
- `sample_size` capped at 5000; default 5000 if invalid.
- Critical events are always included:
  - checkpoints, hey_there, chau
  - observation events with critical importance
- Non-critical events are weighted by:
  - age decay (30-day half-life)
  - observation importance (casual/normal/critical)
  - self relevance (emitter/actor/target == me)
- If sample mode fails, boot recovery falls back to legacy slicing.

Mode: page (complete pagination)
- `page_size` capped at 5000.
- `cursor` is the last event timestamp (nanoseconds) from prior page.
- Returns oldest-first; `next_cursor` is timestamp of last event returned.

Mode: recent
- `limit` capped at 5000 (default 100).
- Returns newest-first.

Legacy mode
- Uses `slice_total` / `slice_index` for interleaved slices.
- Filters by `since_time`, `services`, `subjects`.

Merge (used by mesh sync, zines, and imports)
1. Discover unknown naras mentioned in events.
2. Process `hey_there` and `chau` for identity/liveness.
3. Mark emitters as seen (unless a matching chau is present).
4. Verify signatures (warnings only; unsigned is allowed).
5. Add events to ledger with ID dedupe and checkpoint filtering.
6. Trigger projections if any events were added.

## Failure Modes

- Missing peers -> no recovery.
- Mesh auth or signature failures -> events may be skipped or warned.
- Legacy MQTT sync only returns SocialEvents (partial history).

## Security / Trust Model

- Sync responses may be signed; verification is optional and best-effort.
- Event-level signatures are the primary authenticity layer.
- Unsigned events are accepted but logged.

## Test Oracle

- Page mode returns deterministic slices. (`sync_test.go`)
- Merge warns on bad signatures but still ingests events. (`integration_events_test.go`)

## Open Questions / TODO

- Require SyncResponse signature verification in MeshClient.
