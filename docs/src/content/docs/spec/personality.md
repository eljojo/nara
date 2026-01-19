---
title: Personality
description: Deterministic character traits and their impact on nara behavior.
---

Personality defines a nara's subjective experience. It is a set of deterministic traits derived from the nara's soul that influences how it filters events, forms opinions, and interacts with others.

## 1. Purpose
- Create diverse and unique behaviors among autonomous agents.
- Enable subjective **[Hazy Memory](/docs/spec/memory-model/)** by influencing what a nara chooses to remember or forget.
- Provide a basis for social mechanics.

## 2. Conceptual Model
- **The Big Three Traits**:
    - **Agreeableness**: Influences how much a nara likes others and how it reacts to teasing.
    - **Sociability**: Influences how much a nara cares about social events and gossip.
    - **Chill**: Influences how much a nara is bothered by network volatility (restarts, downtime).
- **Aura**: A visual representation (colors) of the personality and soul.

### Invariants
1. **Deterministic**: Given the same soul and name, the personality MUST always be identical.
2. **Immutable**: A nara cannot change its personality after its soul is born.
3. **Range**: Every trait is a value between 0 and 100.

## 3. External Behavior
- naras with high **Sociability** will store more social events and gossip more frequently.
- naras with high **Chill** are less likely to report or be bothered by peer restarts.
- **Agreeable** naras are less likely to initiate teasing but may "defend" peers who are teased excessively.
- Personality traits are advertised in the `hey-there` presence announcement.

## 4. Interfaces
- `NaraPersonality`: The struct containing the three trait values.
- `DerivePersonality(soul, name)`: The function that generates traits from the cryptographic bond.

## 5. Event Types & Schemas
Personality is embedded in the `NaraStatus` which is carried by:
- `hey-there` events.
- `SyncResponse` metadata.

## 6. Algorithms

### Trait Derivation
Traits are derived by taking specific bytes from the SHA256 hash of the `Soul || Name` bond and mapping them to the 0-100 range:
- `Agreeableness = Hash[0] % 101`
- `Sociability = Hash[1] % 101`
- `Chill = Hash[2] % 101`

### Personality Filtering
- **Discovery Radius**: `Chattiness` determines how many peers a nara attempts to sync with.
- **Social Weighting**: `Weight = f(Sociability, Chill, EventAge)` based on the [sync protocol](/docs/spec/developer/sync/).

## 7. Failure Modes
- **Trait Extremes**: A nara with 0 Chill and 100 Sociability may become a "drama seeker," saturating its ledger with every minor network change.
- **Homogeneity**: If a network is dominated by "Very Chill" naras, social history may be lost very quickly due to aggressive filtering.

## 8. Security / Trust Model
- **Transparency**: Because personality is deterministic and derived from public data (Soul/Name), anyone can verify a nara's claimed personality.

## 9. Test Oracle
- `TestPersonality_Determinism`: Ensures identical souls always produce identical traits.
- `TestAura_VisualConsistency`: Verifies that aura colors are correctly mapped from personality traits.
- `TestPersonality_Filtering`: Validates that high Chill naras correctly filter out normal-importance observations.

## 10. Open Questions / TODO
- Add a "Neuroticism" trait to influence how naras react to their own downtime.
- Link personality traits to the selection of "Confidants" in the stash service.
