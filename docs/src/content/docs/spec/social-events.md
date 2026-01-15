---
title: Social Events
description: Teasing, trends, and buzz in the Nara Network.
---

# Social Events

Social events drive the "living" aspect of the Nara Network, enabling naras to interact, judge each other, and participate in collective behaviors like trends and "buzz".

## Purpose
- Provide human-readable activity for the UI and logs.
- Drive subjective reputation (Clout) through interactions.
- Enable collective behaviors like trends (mainstream vs. underground).
- Model network activity levels through the "Buzz" metric.

## Conceptual Model
- **SocialEvent**: A `SyncEvent` payload capturing an interaction.
- **Teasing**: A subjective interaction where one Nara comments on another's state.
- **Buzz**: A metric representing the "energy" or activity level of a Nara and the network.
- **Trends**: Collective behaviors where naras join or start shared movements (e.g., a specific emoji or style).

### Invariants
- **Subjective Resonance**: A tease only matters if it "resonates" with the observer (personality-dependent).
- **Anti-Pile-On**: Naras wait a random interval and check for existing comments before teasing to prevent everyone from saying the same thing.
- **Cooldown**: A 5-minute cooldown prevents a Nara from spamming the same target.
- **Deterministic Personality**: Teasing and trend behaviors are deterministic based on the Nara's soul-derived personality.

## External Behavior
- **Teasing**: Triggered by specific conditions (high restarts, coming back from MISSING, abandoning a popular trend).
- **Direct Delivery**: Teases are DM'd directly to the target via Mesh HTTP for speed, but also spread via gossip/sync.
- **Trend Participation**: Naras periodically evaluate whether to join a mainstream trend, start an underground one, or remain independent.

## Interfaces

### SocialEvent (SyncEvent Payload)
- `type`: "tease", "observed", "gossip", "observation", "service".
- `actor`: Who initiated the event.
- `target`: Who the event is about.
- `reason`: Why the event happened (e.g., `high-restarts`, `comeback`, `trend-abandon`, `random`, `nice-number`).
- `witness`: Who reported it (optional).

### Tease Reasons
- `high-restarts`: Triggered if restarts per day > threshold.
- `comeback`: Triggered when a Nara returns from `MISSING` to `ONLINE`.
- `trend-abandon`: Triggered when leaving a trend that has >30% popularity.
- `nice-number`: Triggered when a count (like restarts) hits a meme or aesthetically pleasing number (42, 69, 420, etc.).
- `random`: A rare, probabilistic "poke" or "boop".

## Algorithms

### 1. Anti-Pile-On Mechanism
When a tease trigger is detected:
1. Wait a random jitter delay (0-5 seconds).
2. Check the local ledger for any social event about the same target and reason within the last 30 seconds.
3. If a recent event exists, **abort** (someone else said it first).
4. Otherwise, emit the tease.

### 2. Buzz Calculation
Buzz represents the local and network activity level (0-182):
- **Local Buzz**: Increases with events (+3 for sending a tease, +5 for receiving one, etc.); decreases by 3 per second.
- **Weighted Buzz**: `(Local * 0.5) + (NetworkAverage * 0.2) + (HighestBuzzInNetwork * 0.3)`.
- This creates a "vibe" that spreads across the network.

### 3. Trend Logic
Naras evaluate trends every 30 seconds:
- **Joining**: Chance based on `Agreeableness` and how many "same vibe" naras are already in it.
- **Starting**: Chance based on `Sociability`. Higher chance if no trends exist; rebels (low `Agreeableness`) might start an "underground" trend if the top trend is too mainstream (>50%).
- **Leaving**: Chance based on `100 - Chill`. Higher chance if no one else is in the trend.

## Failure Modes
- **Divergent Timelines**: Because naras filter events based on personality, no two naras see the exact same social history.
- **Cooldown Lag**: The 5-minute cooldown is local; if a Nara restarts, their cooldown state is reset unless recovered from a stash.

## Security / Trust Model
- **Authenticity**: All social events are signed by the actor.
- **No Global Truth**: Social events are "hazy" by design; they don't represent hard network state but subjective opinions.

## Test Oracle
- `TestTeaseCooldown`: Verifies that `TryTease` correctly enforces the 5-minute limit.
- `TestAntiPileOn`: Ensures that multiple naras don't trigger the same tease for the same event.
- `TestNiceNumbers`: Checks that the aesthetic number detector works (palindromes, meme numbers).
- `TestTrendTransition`: Validates that naras join/leave trends according to their personality traits.
