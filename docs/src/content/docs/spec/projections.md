# Projections

## Purpose

Projections transform **events (facts)** into **opinions (state)**.

They are where personality, bias, and interpretation live.

---

## Conceptual Model

A projection is:
- pure (no side effects)
- deterministic
- local to a nara

```
opinion = f(events, personality)
```

Key invariants:
- Projections never emit events.
- Projections never mutate the ledger.
- Same inputs → same outputs.

---

## External Behavior

Different naras:
- may see the same events
- but compute different opinions

There is no “correct” projection.

---

## Interfaces

Projections surface through:
- HTTP API (derived fields)
- Web UI visualizations
- Internal decision-making

---

## Algorithms

Typical projection steps:
1. Filter relevant events
2. Sort or group by subject
3. Apply decay, thresholds, or bias
4. Produce derived state

Personality parameters influence:
- weighting
- memory span
- reaction strength

---

## Failure Modes

- Missing events → incomplete opinions
- Conflicting events → ambiguity
- Old events → faded influence

---

## Security / Trust Model

- Projections are unverifiable.
- Only events are verifiable.
- Disagreement is expected.

---

## Test Oracle

- Same events + same personality → same projection.
- Different personalities → divergent results.
- Removing events changes projections.

---

## Open Questions / TODO

- Standard confidence metrics for projections.
