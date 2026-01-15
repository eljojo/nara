---
title: Clout
---

# Clout

## Purpose

Clout is a subjective measure of social standing derived from observed interactions.
It powers leaderboards and influences social decision-making (e.g., trend following).

## Conceptual Model

- **Clout** is subjective: each observer calculates clout differently based on their personality and resonance.
- **Tease Resonance**: A deterministic but observer-specific function decides if a tease "lands" (improving clout) or "flops" (reducing clout).
- **Service Reward**: Helpful actions (like storing stashes) reliably increase clout.

Key invariants:
- Clout is derived from `SocialEvent`s.
- Clout decays over time (recent actions matter more).
- Clout can be negative ("cringe").

## External Behavior

- Clout scores are calculated on demand for UI leaderboards.
- Clout influences `Buzz` (general network chatter level).
- High clout naras may have more influence on trend adoption.

## Interfaces

### Projection API
`CloutProjection` exposes:
- `DeriveClout(observerSoul, personality)` -> `map[name]float64`

### UI Endpoints
- `/clout`: Returns the local node's subjective clout rankings.

## Event Types & Schemas

Consumes `SyncEvent` with `svc: social`.

### SocialEventPayload Types
- `tease`: Direct social interaction (risky: can gain or lose clout).
- `observed`: Third-party observation (lower impact).
- `gossip`: Hearsay (lowest impact).
- `observation`: System state changes (e.g. online/offline, journey participation).
- `service`: Helpful actions (e.g. `stash-stored`).

## Algorithms

### 1. Resonance (The Vibe Check)
Determines if a tease is "good" or "bad" for a specific observer.
```go
Hash = SHA256(EventID + ObserverSoul)
Resonance = Hash[0..8] % 100
Threshold = 50 + (Sociability/5) - (Chill/10) - (Agreeableness/20)
Result = Resonance < Threshold
```
- **High Sociability**: Easier to impress (higher threshold).
- **High Chill/Agreeableness**: Harder to impress (lower threshold).

### 2. Scoring Logic
Base weight is 1.0, adjusted by personality and event type.

| Event Type | Condition | Impact |
|------------|-----------|--------|
| `tease` | Resonates | `+1.0 * Weight` |
| `tease` | !Resonates | `-0.3 * Weight` (Cringe) |
| `observed` | Resonates | `+0.5 * Weight` |
| `gossip` | Always | `+0.1 * Weight` |
| `service` | `stash-stored` | `+2.0 * Weight` (Big reward) |
| `service` | Other | `+0.5 * Weight` |

### 3. Observation Scoring (Target's Clout)
System observations affect the **target**.
- `online`: `+0.1` (Reliable)
- `offline`: `-0.05` (Flaky)
- `journey-pass`: `+0.2` (Participant)
- `journey-complete`: `+0.5` (Winner)
- `journey-timeout`: `-0.3` (Failure)

### 4. Weight Calculation
Adjusts impact based on personality and reason.
- **Sociability**: Boosts weight for `comeback`, `trend-abandon`.
- **Chill**: Reduces weight for `random` teases.
- **Time Decay**: Events fade with a 24h half-life (adjusted by Chill/Sociability).

## Failure Modes

- **Subjectivity**: Two nodes will see different clout scores for the same target (intended feature).
- **Missing Events**: If history is incomplete (e.g. fresh boot), clout scores will be lower/different until sync.

## Security / Trust Model

- Clout is purely local opinion. There is no "global clout" consensus.
- Relying on local calculation prevents gaming global metrics.

## Test Oracle

- **Resonance**: Deterministic for a given Soul+EventID. (`projection_clout.go`)
- **Scoring**: `stash-stored` yields higher points than teases. (`projection_clout.go`)
- **Decay**: Old events contribute less than new ones. (`projection_clout.go`)

## Open Questions / TODO

- **Clout Decay**: Should clout have a floor (e.g. 0) or can it go infinitely negative? Currently allows negative.
