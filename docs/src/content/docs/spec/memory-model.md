# Memory Model

## Purpose

The memory model defines how naras keep, forget, and prune information while
staying memory-only and eventually consistent.

## Conceptual Model

- All durable knowledge is event-based; there is no disk persistence.
- Memory is bounded by the ledger capacity and pruning policy.
- Naras may forget or prune peers and events based on age and relevance.

Key invariants:
- SyncLedger is in-memory only.
- Critical events are never pruned.
- Pruning is deterministic given the same ledger contents.

## External Behavior

- Memory mode controls maximum event capacity and background sync behavior.
- Periodic maintenance prunes old events and inactive peers.
- Boot recovery reconstructs memory by sampling peers.

## Interfaces

Memory modes and defaults (see `memory.go`):
- short: 20,000 events, background sync disabled
- medium: 80,000 events
- hog: 320,000 events
- custom: user-specified `LEDGER_CAPACITY`

Maintenance loops:
- `socialMaintenance` (every 5 minutes) prunes events and inactive naras.
- Observation maintenance runs every 1 second.

## Event Types & Schemas (if relevant)

Memory behavior is driven by event types and priorities:
- Checkpoints and critical observations are never pruned.
- Pings are low-priority and aggressively trimmed.

## Algorithms

Ledger pruning:
- If over `MaxEvents`, prune by priority (higher number pruned first):
  - ping (4)
  - seen (3)
  - social (2)
  - status-change observation (1)
  - hey_there/chau (1)
  - restart/first-seen observation and checkpoints (0, never pruned)
- Events from unknown naras (no public key) are pruned first.
- Within the same priority, oldest events are pruned first.

Ping retention:
- Keep only the most recent 5 ping events per target.
- Older pings are dropped when a new ping arrives.

Observation compaction:
- At most 20 observation events per observer->subject pair.
- Prefer pruning non-restart events.
- Do not prune restart events unless a checkpoint exists for the subject.

Neighbourhood pruning:
- Zombies (never seen) are pruned immediately.
- Newcomers (<2 days old) are pruned after 24h offline.
- Established (2-30 days) are pruned after 7 days offline.
- Veterans (>=30 days) are never auto-pruned.
- Pruning removes neighbourhood entry, observations, and all events involving the pruned name.

Boot recovery:
- Uses mesh sync (sample mode) to repopulate the ledger.
- Falls back to legacy MQTT social sync if mesh is unavailable.

## Failure Modes

- If all peers forget an event, it is lost permanently.
- Aggressive pruning in short memory mode reduces historical context.
- Ghost pruning removes checkpoints about the ghost subject.

## Security / Trust Model

- Memory loss is expected and not considered a security failure.
- Event signatures still verify authenticity for remaining memory.

## Test Oracle

- Critical events are never pruned. (`sync_test.go`, `checkpoint_test.go`)
- Unknown-nara events are pruned first. (`sync_test.go`)
- Neighbourhood pruning thresholds are enforced. (`network_prune_test.go`)

## Open Questions / TODO

- Formalize memory guarantees for hybrid MQTT + gossip deployments.
