---
title: Clout
description: Subjective reputation and ranking in the Nara Network.
---

# Clout

Clout is a subjective reputation score derived from observed actions. It is an observer-dependent opinion, not a global value.

## 1. Purpose
- Decentralized reputation without central authority.
- Reward participation (journeys, service).
- Penalize "cringe" (failed teases) or instability (restarts).
- Drive social hierarchy and "coolness" dynamics.

## 2. Conceptual Model
- **Score**: Floating-point projection of social events.
- **Subjectivity**: Derived by each node based on personality and seen history.
- **Resonance**: Interaction impact depends on observer "liking" (resonating with) the act.

### Invariants
- **Growth**: Tied to positive service and journey participation.
- **Decay**: Half-life-based reduction over time.
- **Cringe Penalty**: Actor loses clout if their tease fails to resonate with the observer.

## 3. Algorithms

### Resonance Logic (`TeaseResonates`)
Determines if an observer approves of a tease:
1. `Score = Hash(EventID + ObserverSoul) % 100`.
2. `Threshold = 50 + (Sociability/5) - (Chill/10) - (Agreeableness/20)`.
3. `Resonates = Score < Threshold`.

### Clout Projection
Observer-side adjustments:
- **Tease (Actor)**: `+1.0` (Resonates) / `-0.3` (Cringe).
- **Service (Actor)**: `+2.0` (e.g., `stash-stored`).
- **Journey Complete (Target)**: `+0.5`.
- **Journey Timeout (Target)**: `-0.3`.
- **Online (Target)**: `+0.1`.
- **Offline (Target)**: `-0.05`.

### Weighting & Decay
`Adjustment = BaseWeight * TimeDecay`.
- **BaseWeight**: `1.0 + (Sociability/200) - (Chill/400)`.
- **TimeDecay**: `1 / (1 + age/halfLife)`. (See [Personality](./personality.md) for half-life).

## 4. Failure Modes
- **Asymmetry**: Missing events (e.g., service logs) leads to unfairly low opinions.
- **Volatility**: High-sociability nodes may experience rapid clout swings.

## 5. Security & Trust
- **Subjective Immunity**: Cannot be hacked globally; each peer verifies claims via signed evidence.
- **Proof-of-Action**: Clout adjustments require cryptographically signed event proofs.

## 6. Test Oracle
- `TestDeriveClout`: Event-to-score projection accuracy.
- `TestTeaseResonance`: Personality-based approval checks.
- `TestCloutDecay`: Temporal reduction verification.
