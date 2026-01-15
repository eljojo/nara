---
title: Personality
description: Deterministic traits and subjective behavior in the Nara Network.
---

# Personality

Personality defines a Nara's "character" and determines how it filters events, weights clout, and interacts with peers. It is the primary engine of subjectivity in the network.

## Purpose
- Drive divergent, subjective views of the network state.
- Determine which events are "meaningful" to a specific Nara.
- Influence the frequency and type of social interactions (teasing, trends).
- Model human-like social dynamics (e.g., chill naras vs. sociable ones).

## Conceptual Model
A Nara's personality consists of three core traits, each on a scale of 0-99:

- **Agreeableness**: Affects trend participation and willingness to criticize/tease.
- **Sociability**: Influences the frequency of outgoing social events and the weighting of social data.
- **Chill**: Determines the memory decay rate and how much a Nara ignores routine or "noisy" events.

### Invariants
- **Deterministic Seeding**: Traits are seeded from a SHA256 hash of the Nara's `soul`.
- **Immutable**: Once generated, a personality is stable for that identity across restarts.
- **Public**: Personality traits are included in `NaraStatus` and visible to the network.

## External Behavior
- **Filtering**: A Nara with high `Chill` will drop routine `online`/`offline` observations from its ledger to save space.
- **Reactions**: `Sociability` lowers the threshold for a Nara to notice a problem (like high restarts) and comment on it.
- **Memory**: The half-life of social weight is subjective; "chill" naras forget drama faster than "non-chill" ones.

## Algorithms

### 1. Seeding Traits
1. `Hash = SHA256(RawSoulBytes)`
2. `Seed = BigEndianUint64(Hash[0:8])`
3. `Rand = NewRand(Seed)`
4. `Agreeableness = Rand.Intn(100)`
5. `Sociability = Rand.Intn(100)`
6. `Chill = Rand.Intn(100)`

### 2. Meaningful Event Filtering (`socialEventIsMeaningful`)
Naras use their personality to decide if an event is worth storing in their ledger:
- **Casual Events** (Importance 1): Dropped if `Chill` or `Agreeableness` thresholds are met (e.g., high-chill naras ignore random teases).
- **Routine Logs**: High-chill naras (>85) may ignore standard `online`/`offline` observations.
- **Social Engagement**: Low-sociability naras (<30) may ignore journey events or gossip.

### 3. Subjective Weighting
The weight of an event in a Nara's opinion is adjusted:
- **Sociability Bonus**: Weight increases by up to 0.5 for sociable naras.
- **Chill Penalty**: Weight decreases by up to 0.25 for chill naras.
- **Half-Life Modifier**: 
  - `chillModifier = 1.0 + (50 - Chill) / 100.0` (0.5x to 1.5x)
  - `socModifier = 1.0 + Sociability / 333.0` (1.0x to 1.3x)
  - `SubjectiveHalfLife = BaseHalfLife (24h) * chillModifier * socModifier`

## Failure Modes
- **Subjective Isolation**: A Nara with extremely high `Chill` or low `Sociability` might have an almost empty ledger, missing important social context.
- **Personality Conflicts**: Different personalities will derive different `Clout` scores for the same Nara, leading to subjective "truth" divergence.

## Security / Trust Model
- **Non-Forgeable**: Since personality is derived from the soul, you cannot "fake" a personality without having the corresponding seed.
- **Visibility**: Transparency in personality allows peers to "understand" why another Nara might be teasing them or ignoring their events.

## Test Oracle
- `TestPersonalitySeeding`: Verifies that the same soul always produces the same traits.
- `TestMeaningfulFilter`: Checks that high-chill naras correctly filter out noise events.
- `TestSubjectiveWeighting`: Validates that Sociability and Chill correctly influence event weights and decay.
