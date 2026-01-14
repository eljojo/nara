# Events

## Purpose

Events are the **only shared truth** in the Nara Network.

They represent:
- observations
- actions
- social interactions
- consensus artifacts

Events are facts about *what was claimed or observed*, not guarantees of correctness.

---

## Conceptual Model

An event is:
- immutable
- signed
- timestamped
- append-only

The event ledger stores **what happened**, never **what is**.

Key invariants:
- Events are never modified or deleted.
- State is always derived from events.
- Events may arrive out of order or be missing.

---

## External Behavior

Naras:
- emit events
- receive events
- forward events
- store events in memory

The same event may be seen by some naras and missed by others.

---

## Interfaces

Events are exchanged via:
- MQTT (broadcast)
- HTTP mesh (gossip, zines)

Each event includes:
- event type
- author identity
- timestamp
- signature
- payload

---

## Event Types (non-exhaustive)

- Presence events (online/offline)
- Observation events
- Social events (teases, trends)
- World journey hops
- Checkpoints

Each feature defines its own payload schema.

---

## Algorithms

### Event acceptance
1. Verify signature
2. Verify identity authenticity
3. Deduplicate by ID
4. Append to local ledger

### Ordering
- Events are not globally ordered.
- Local order is best-effort based on arrival and timestamps.

---

## Failure Modes

- Missing events → incomplete memory
- Duplicate events → ignored
- Clock skew → conflicting timelines

All are acceptable and expected.

---

## Security / Trust Model

- Events prove authorship, not truth.
- Observations can be wrong.
- Consensus is layered on top via checkpoints.

---

## Test Oracle

- An event cannot be altered without invalidating its signature.
- Replaying events produces deterministic projections.
- Missing events change derived state.

---

## Open Questions / TODO

- Explicit confidence metadata on events.
