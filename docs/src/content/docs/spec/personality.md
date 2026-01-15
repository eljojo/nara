---
title: Personality
---

# Personality

Personality shapes how a nara filters, remembers, and reacts to social events. It is the primary source of subjective divergence in the network.

## Conceptual Model

| Trait | Scale | Network Impact |
| :--- | :--- | :--- |
| **Agreeableness** | 0-99 | Trend participation and conflict filtering. |
| **Sociability** | 0-99 | Tease frequency and event weighting. |
| **Chill** | 0-99 | Noise reduction and memory decay rate. |

### Invariants
- **Deterministic**: Seeded from a SHA256 hash of the soul. Stable across restarts.
- **Subjective**: Affects local interpretation/filtering, not event validity.
- **Visibility**: Shared via `NaraStatus` in heartbeats and API snapshots.

## Algorithms

### 1. Seeding
1. `Hash = SHA256(Base58(Soul))`.
2. `Seed = Hash[0..8]`.
3. Draw traits via `rand.NewSource(Seed)`.

### 2. Social Filtering (`socialEventIsMeaningful`)
Logic for dropping "noise" based on personality:
- **Chill > 70**: Drop `random` teases.
- **Chill > 85**: Drop routine `online`/`offline` logs.
- **Sociability < 30**: Drop `journey-pass`/`complete` logs.
- **Agreeableness > 80**: Drop `trend-abandon` teases.

### 3. Event Weighting & Memory
- **Base Weight**: 1.0.
- **Sociability Bonus**: Up to `+0.5`.
- **Chill Penalty**: Up to `-0.25`.
- **Decay**: 24h half-life. *Note: Current implementation uses nanosecond event TS vs second-based current time, resulting in a fixed 7-day decay baseline.*

### 4. Social Behavior
- **Teasing**: Sociability lowers thresholds for restart/comeback teases.
- **Trends**: High Agreeableness favors joining trends; High Sociability favors starting them. Low Agreeableness favors contrarian/unique trends.

## Interfaces
- `NaraStatus.Personality`: Exported field.
- `nara_personality{trait}`: Prometheus gauges.

## Test Oracle
- **Determinism**: Verify consistent traits for a given soul. (`social_trend_test.go`)
- **Filtering**: Verify event dropping for specific profiles. (`sync_test.go`)
- **Triggers**: Personality-adjusted tease thresholds. (`teasing_test.go`)
