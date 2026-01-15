---
title: Boot Sequence
---

# Boot Sequence

## Purpose

Boot sequencing ensures naras start in a consistent order:
identity and transport first, then recovery, then projections and background loops.

## Conceptual Model

Entities:
- Network.Start: orchestrates startup.
- Boot recovery: reconciles state from peers before opinions are formed.
- Maintenance loops: long-running goroutines for presence, gossip, projections.

Key invariants:
- Boot recovery runs before opinions and projection loops.
- Mesh initialization happens before MQTT so MeshIP is available for hey_there.
- Read-only mode skips all network mutation and background emitters.

## External Behavior

Startup order (simplified):
1. Start local HTTP server if UI is enabled.
2. Initialize stash manager and stash service (if keypair available).
3. Start log service.
4. Initialize mesh transport, mesh HTTP server, and world journey handler.
5. Connect to MQTT (unless gossip-only).
6. Broadcast presence (hey_there + announce) and emit gossip identity.
7. Start inbox processors.
8. Run boot recovery (unless read-only or test override).
9. Form opinions and start maintenance loops.

## Interfaces

Entry points:
- `Network.Start(serveUI, httpAddr, meshConfig)`.

Internal services:
- Boot recovery (`boot_recovery.go`).
- Checkpoint service (`checkpoint_service.go`).
- Stash service (`stash_service.go`).

## Event Types & Schemas (if relevant)

- Presence events: hey_there, howdy, chau, seen.
- Sync events during boot recovery (ledger replay and merges).

## Algorithms

Presence broadcast:
- On startup, broadcast hey_there (MQTT) and announce (newspaper) with jitter.
- In gossip-only mode, emit hey_there as a sync event instead.

Boot recovery sequencing:
- Suppress ledger event logging during boot recovery.
- Run boot recovery if not read-only and not test-skipped.
- Close `bootRecoveryDone` when complete.

Maintenance loops (post-recovery):
- Observation maintenance (presence, restarts, seen).
- Periodic announce (if not read-only).
- Trend maintenance and buzz decay.
- Background sync (if memory profile allows).
- Gossip exchange (if transport includes gossip).
- Social maintenance (tease cleanup).
- Journey timeout maintenance.
- Coordinate maintenance (Vivaldi pings).
- Stash maintenance and checkpoint consensus (if enabled).

## Failure Modes

- Mesh initialization failure falls back to a local mock mesh for journeys.
- MQTT connection errors are fatal unless in gossip-only mode.
- If boot recovery is skipped, opinions may be incomplete until new events arrive.

## Security / Trust Model (if relevant)

- Mesh HTTP endpoints use Ed25519 request signatures.
- MQTT events are signed where applicable and verified on receipt.

## Test Oracle

- Startup sequencing initializes services and loops in order. (`network_test.go`)
- Boot recovery gating and checkpoint interactions are correct. (`boot_recovery_test.go`)

## Open Questions / TODO

- None.
