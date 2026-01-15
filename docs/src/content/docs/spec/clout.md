---
title: Clout
---

# Clout

A subjective measure of social standing derived from observed interactions. Clout is used for ranking, trend following, and calculating network "buzz."

## Conceptual Model

| Concept | Rule |
| :--- | :--- |
| **Subjective** | Calculated locally; each observer derives different scores based on their personality. |
| **Resonance** | A "vibe check" (Resonance vs Threshold) determines if a tease succeeds or flops. |
| **Decay** | 24h half-life; recent actions carry more weight. |
| **Cringe** | Clout can be negative if interactions consistently fail resonance checks. |

## Algorithms

### 1. Resonance (The Vibe Check)
Determines if a `tease` lands (+clout) or flops (-clout) for a specific observer:
- **Resonance**: `SHA256(EventID + ObserverSoul) % 100`.
- **Threshold**: `50 + (Sociability/5) - (Chill/10) - (Agreeableness/20)`.
- **Success**: `Resonance < Threshold`.

### 2. Scoring Logic (Base Weights)

| Event Type | Condition | Impact |
| :--- | :--- | :--- |
| **`tease`** | Success | `+1.0` |
| **`tease`** | Fail (Cringe) | `-0.3` |
| **`service`**| `stash-stored` | `+2.0` |
| **`observation`**| `journey-complete` | `+0.5` |
| **`observation`**| `online` / `offline` | `+0.1` / `-0.05` |
| **`gossip`** | Always | `+0.1` |

*Weights are adjusted by the observer's personality (e.g., Chill reduces impact of random teases).*

## Projections

- `CloutProjection`: Processes `social` events from the ledger.
- `DeriveClout(observerSoul, personality)`: Returns a map of subjective scores.
- `/clout` Endpoint: Exposes the local ranking to the UI.

## Failure Modes
- **Divergence**: Missing events (e.g., during network split) result in temporary ranking gaps.
- **Drift**: Subjectivity ensures there is no "global truth," which prevents central gaming but makes metrics hard to compare across nodes.

## Test Oracle
- **Determinism**: Resonance is idempotent for a given Soul+EventID. (`projection_clout.go`)
- **Impact**: `stash-stored` rewards > `tease` rewards. (`projection_clout.go`)
- **Decay**: Old events correctly contribute less over time. (`projection_clout.go`)
