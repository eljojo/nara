---
title: Personality
description: Deterministic traits and subjective behavior in the Nara Network.
---

Personality defines a Nara's "character," driving subjectivity by determining event filtering, clout weighting, and interaction frequency.

## 1. Purpose
- Drive divergent, subjective views of network state.
- Define "meaningful" events for a specific Nara.
- Influence social dynamics (teasing, trends).
- Model human-like social behaviors (e.g., Chill vs. Sociable).

## 2. Conceptual Model
Three core traits (0-99):
- **Agreeableness**: Trend participation and willingness to tease.
- **Sociability**: Social event frequency and data weighting.
- **Chill**: Memory decay rate and noise sensitivity.

### Invariants
- **Deterministic**: Seeded from `SHA256(soul)`.
- **Immutable**: Stable for a given identity across restarts.
- **Transparent**: Traits are included in public `NaraStatus`.

## 3. Algorithms

### Seeding Traits
1. `Seed = BigEndianUint64(SHA256(soul)[0:8])`.
2. `Rand = NewRand(Seed)`.
3. Traits = `Rand.Intn(100)`.

### Event Filtering (`socialEventIsMeaningful`)
- **Casual Events** (Importance 1): Dropped if `Chill` or `Agreeableness` thresholds are met.
- **Routine Logs**: High-chill (>85) nodes may ignore standard online/offline observations.
- **Engagement**: Low-sociability (<30) nodes may ignore journeys or gossip.

### Subjective Weighting & Decay
Weight adjustment:
- **Sociability Bonus**: Up to +0.5.
- **Chill Penalty**: Up to -0.25.
- **Half-Life**: `Base (24h) * chillModifier * socModifier`.
  - `chillModifier`: 0.5x to 1.5x based on Chill.
  - `socModifier`: 1.0x to 1.3x based on Sociability.

## 4. Failure Modes
- **Subjective Isolation**: Extreme traits (e.g., high Chill + low Sociability) lead to sparse ledgers and loss of social context.
- **Truth Divergence**: Different personalities derive different `Clout` from identical event streams.

## 5. Security & Trust
- **Authenticity**: Personality is tied to soul; cannot be faked without the seed.
- **Auditability**: Public traits allow peers to interpret a Nara's "behavior" (e.g., why it ignores events).

## 6. Test Oracle
- `TestPersonalitySeeding`: Consistency verification.
- `TestMeaningfulFilter`: Noise reduction logic.
- `TestSubjectiveWeighting`: Variance in weight/decay calculation.
