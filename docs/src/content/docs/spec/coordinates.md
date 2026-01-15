# Coordinates

## Purpose

Coordinates provide a shared, approximate map of network latency.
They are used for proximity-aware routing and visualization.

## Conceptual Model

Entities:
- NetworkCoordinate: (x, y, height, error) in virtual space.
- VivaldiConfig: update constants and initial error values.
- PingObservation: signed RTT measurement between two naras.

Key invariants:
- Coordinates are local estimates and can diverge across naras.
- RTT measurements are recorded as ping sync events with signatures.
- Coordinate updates are throttled to at most 2 pings per minute.

## External Behavior

- Each nara periodically pings a selected peer and updates its coordinate.
- Coordinates are exposed via `GET /coordinates` for UI use.
- Ping RTTs are stored in the ledger to propagate measurements.

## Interfaces

HTTP endpoints (mesh):
- `GET /ping`: returns timestamp and identity for RTT measurement.
- `GET /coordinates`: returns this nara's coordinates.

Public structs:
- `NetworkCoordinate`, `VivaldiConfig`, `PingObservation`.

## Event Types & Schemas (if relevant)

Service: `ping` (`SyncEvent.Service`).

`PingObservation` fields:
- `observer`: who measured the RTT.
- `target`: who was measured.
- `rtt`: milliseconds (rounded to 0.1ms in signature content).

Ping events are signed by the observer.

## Algorithms

Coordinate initialization:
- New coordinates are a small random offset near the origin.
- Initial error is 1.0; height starts at `MinHeight`.

Vivaldi update (per ping):
1. Predict distance using Euclidean distance plus both heights.
2. Compute error = measured RTT - predicted.
3. Weight by relative error between nodes.
4. Update error estimate with `Ce`.
5. Move coordinates along the unit vector by `Cc * w * error`.
6. Adjust height by `Cc * w * error * 0.1` (clamped to MinHeight).

Ping selection:
- Priority order: never-pinged peers, high-error peers, stale measurements, random jitter.
- Requires peer to be ONLINE and have a mesh IP.

Ping recording:
- Update local observation with last RTT, average RTT, and last ping time.
- Record a signed ping event in the ledger, keeping only the last 5 pings per target.

Proximity weighting:
- Clout is adjusted using exponential decay `exp(-distance/100)`.
- Final clout is a 70/30 blend of base clout and proximity-weighted clout.

## Failure Modes

- No mesh IP or mesh transport means coordinates cannot update.
- Invalid or missing coordinates skip proximity weighting.

## Security / Trust Model (if relevant)

- Ping events are signed by the observer and can be verified by peers.
- Ping HTTP is intentionally unauthenticated to minimize latency.

## Test Oracle

- Vivaldi updates move coordinates and adjust error as expected. (`vivaldi_test.go`)
- Proximity weighting modifies clout deterministically. (`vivaldi_test.go`)

## Open Questions / TODO

- None.
