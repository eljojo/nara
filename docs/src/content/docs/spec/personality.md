# Personality

## Purpose

Personality shapes how a nara filters, remembers, and reacts to social events.
It is the primary source of subjective divergence across the network.

## Conceptual Model

Personality is a triple of integers:
- Agreeableness (0-99)
- Sociability (0-99)
- Chill (0-99)

Key invariants:
- Personality is deterministically derived from the soul.
- Personality is stable across restarts and migrations.
- Personality affects social filtering and weighting, not event validity.

## External Behavior

- Each nara seeds its personality on startup from its soul.
- Personality is shared in `NaraStatus` and visible via HTTP APIs.
- Social events may be filtered or weighted differently per observer.

## Interfaces

Public fields:
- `NaraStatus.Personality`

HTTP exposure:
- `/api.json` and `/narae.json` include personality values.
- `/status/{name}.json` returns full `NaraStatus` (including personality).
- `/metrics` exports `nara_personality{name,trait}` gauges.

## Event Types & Schemas (if relevant)

Personality does not define its own event type.
It influences how social and observation events are stored and weighted.

## Algorithms

Seeding:
- Hash the Base58 soul string with SHA256.
- Use the first 8 bytes as a deterministic RNG seed.
- Draw Agreeableness, Sociability, Chill with `rand.Intn(100)` each.

Social event filtering (meaningfulness):
- Chill > 70 drops random teases.
- Chill > 85 drops routine online/offline observations.
- Sociability < 30 drops journey pass/complete observations.
- Sociability < 20 drops random drama.
- Agreeableness > 80 drops trend-abandon teases.
- Always keep journey-timeout observations.

Social event weighting (memory):
- Base weight 1.0.
- Sociability adds up to +0.5.
- Chill subtracts up to -0.25.
- Reason- and type-based multipliers apply.
- Time decay uses a 24h half-life modified by Chill and Sociability.
- Note: `EventWeight` uses `event.Timestamp` (nanoseconds) vs `time.Now().Unix()`
  (seconds). This makes most events appear "in the future" and forces a fixed
  7-day decay baseline; this is current behavior and must be preserved.

Teasing gates:
- Sociability and Chill adjust thresholds for restart-rate, comeback, and trend-abandon.
- Random teases are probabilistic and more likely for nearby peers.

Trend behavior:
- Sociability increases the chance to start trends.
- Agreeableness increases the chance to join existing trends.
- Low agreeableness increases contrarian trend creation.
- Chill reduces the chance to leave a trend.

## Failure Modes

- Personality values outside 0-99 are not expected (seeded within range).
- Inconsistent personality values across nodes indicate soul mismatch.

## Security / Trust Model

- Personality is not secret and does not grant authority.
- It only affects local interpretation of events.

## Test Oracle

- Personality seeding is deterministic per soul. (`social_trend_test.go`)
- Personality filters social events as expected. (`sync_test.go`)
- Personality affects teasing triggers. (`social_test.go`, `teasing_test.go`)

## Open Questions / TODO

- Add configurable personality overrides for testing and experiments.
