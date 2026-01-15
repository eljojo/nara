# Clout

## Purpose

Clout is a subjective reputation score derived from social events.
It influences how naras choose collaborators and route world journeys.

## Conceptual Model

Entities:
- CloutProjection: stores social events and derives clout on demand.
- SocialEventRecord: a flattened record of social event fields.
- Observer: the nara deriving clout (soul + personality).

Key invariants:
- Clout is observer-dependent: same events + different personality = different scores.
- Clout is derived from stored events; it is not broadcast as a fact.
- Time decay reduces the impact of old events.

## External Behavior

- Clout is exposed via `/social/clout` and embedded in `/narae.json`.
- Clout influences world journey routing (higher clout peers are preferred).
- Proximity weighting adjusts clout based on network distance.

## Interfaces

HTTP endpoints:
- `GET /social/clout`: clout map from this nara's perspective.
- `GET /narae.json`: per-nara data including clout scores.

Public structs:
- `CloutProjection`, `SocialEventRecord`.

## Event Types & Schemas (if relevant)

Clout derives from `SyncEvent` with `svc="social"`:
- `type="tease"`: affects actor's clout, positive or negative based on resonance.
- `type="observed"`: smaller positive impact if it resonates.
- `type="gossip"`: very small positive impact.
- `type="observation"`: affects target clout based on reason (online/offline/journey).
- `type="service"`: affects actor clout; `stash-stored` is strongly positive.

## Algorithms

Event weighting:
- Base weight starts at 1.0.
- Add sociability/200, subtract chill/400.
- Reason modifiers adjust the weight (e.g., comeback increases, random decreases).
- Type modifiers: observed multiplies by 0.7, gossip by 0.4.

Resonance:
- Teases use `TeaseResonates(eventID, observerSoul, personality)`.
- If it resonates: actor gains weight * 1.0.
- If not: actor loses weight * 0.3.

Observation reasons and target impact:
- `online`: +0.1 * weight to target.
- `offline`: -0.05 * weight to target.
- `journey-pass`: +0.2 * weight to target.
- `journey-complete`: +0.5 * weight to target.
- `journey-timeout`: -0.3 * weight to target.

Service reasons:
- `stash-stored`: +2.0 * weight to actor.
- Other service reasons: +0.5 * weight to actor.

Time decay:
- Age is computed as `now_seconds - event_timestamp_seconds`.
- If age < 0 (future timestamp), clamp to 7 days.
- Half-life is 24 hours, scaled by chill and sociability.
- Decay factor is `1 / (1 + age / half_life)`.
- Weight is floored at 0.1.

Proximity weighting:
- `ApplyProximityToClout` blends clout with an exponential distance decay.
- Result is `clout*0.7 + clout*proximity_weight*0.3`, using distance from Vivaldi coordinates.

## Failure Modes

- If projections are not initialized, clout maps are empty.
- If timestamps are far in the future, their decay is forced to a 7-day age.

## Security / Trust Model (if relevant)

- Clout is derived locally and is not a signed or consensus-backed value.
- Authenticity comes from the underlying social event signatures.

## Test Oracle

- Journey-derived social events raise or lower clout as expected. (`world_integration_test.go`)
- Proximity weighting scales clout based on distance. (`vivaldi_test.go`)

## Open Questions / TODO

- None.
