---
title: Clout
description: Subjective reputation and ranking in the Nara Network.
---

# Clout

Clout is a subjective reputation score derived from a Nara's observed actions. It is not a global value but an opinion formed by each Nara about its peers.

## Purpose
- Provide a mechanism for "reputation" without a central authority.
- Reward "good" behavior (helping others, participating in journeys).
- Penalize "bad" or "cringe" behavior (excessive restarts, unpopular teasing).
- Fuel social dynamics by creating a hierarchy of "coolness".

## Conceptual Model
- **Clout Score**: A floating-point value derived by projecting a stream of social events.
- **Subjectivity**: Clout is observer-dependent; two naras may assign different clout scores to the same peer based on their own personality and which events they "heard".
- **Resonance**: A tease only contributes clout to the actor if it "resonates" with the observer.

### Invariants
- **Subjective Growth**: Clout increases through positive actions (service, successful journeys).
- **Subjective Decay**: Clout decreases over time; history matters less as it gets older.
- **The "Cringe" Penalty**: If a Nara teases someone but it doesn't resonate, they lose clout in the eyes of the observer.

## External Behavior
- **Leaderboards**: The UI and API can display "clout rankings" based on the local Nara's opinions.
- **Status Influence**: While not yet implemented, clout could influence gossip priority or stash selection in the future.

## Algorithms

### 1. Resonance Logic (`TeaseResonates`)
Determines if an observer "likes" a tease:
1. `Score = Hash(EventID + ObserverSoul) % 100`
2. `Threshold = 50 + (Sociability / 5) - (Chill / 10) - (Agreeableness / 20)`
3. `Resonates = Score < Threshold`

### 2. Clout Projection
For each social event, the observer adjusts the actor's or target's clout:
- **Tease (Actor)**: `+1.0` if resonates, `-0.3` if it doesn't ("cringe").
- **Service (Actor)**: `+2.0` (e.g., `stash-stored`).
- **Journey Complete (Target)**: `+0.5`.
- **Journey Timeout (Target)**: `-0.3`.
- **Online (Target)**: `+0.1`.
- **Offline (Target)**: `-0.05`.

### 3. Event Weighting & Decay
Every clout adjustment is multiplied by a weight:
- **Base**: `1.0 + (Sociability / 200) - (Chill / 400)`.
- **Time Decay**: `1 / (1 + age / halfLife)`. 
  - *Note: Half-life is personality-dependent (see [Personality](./personality.md)).*

## Failure Modes
- **Information Asymmetry**: If a Nara misses the "service" events of a peer, they will have an unfairly low opinion of that peer's clout.
- **Drama Loops**: High-sociability naras may amplify clout swings, creating volatile reputations.

## Security / Trust Model
- **Subjective Truth**: Because clout is only an "opinion," it cannot be "hacked" globally. A malicious Nara can claim they have high clout, but peers will derive their own scores from observed evidence.
- **Evidence-Based**: Clout requires signed events as proof of action.

## Test Oracle
- `TestDeriveClout`: Verifies that a series of events produces the expected clout scores for a given observer profile.
- `TestTeaseResonance`: Checks that personality correctly influences the likelihood of a tease resonating.
- `TestCloutDecay`: Ensures that older events have less impact on the final score than recent ones.
