---
title: Clout
description: Subjective reputation and resonance in the nara network.
---

Clout is a subjective measure of a nara's reputation and social standing, derived from its interactions and the "resonance" those interactions have with others.

## 1. Purpose
- Provide a decentralized mechanism for ranking naras based on their activity and social impact.
- Help naras prioritize information (gossip, sync) from "important" or "interesting" peers.
- Reward stability, uptime, and meaningful social participation.

## 2. Conceptual Model
- **Clout Score**: A numerical value representing reputation.
- **Resonance**: A multiplier derived from how much an interaction matches a nara's personality.
- **Interactions**: Events like teasing, seen proofs, and presence announcements that contribute to clout.
- **Decay**: Clout is not permanent; old interactions lose influence over time.

### Invariants
1. **Subjectivity**: Every nara calculates Clout differently based on its own personality.
2. **Event-Sourced**: Clout MUST be entirely derived from the event ledger.
3. **No Central Authority**: There is no global "High Score"; rankings only exist in the mind of the observer.

## 3. External Behavior
- naras use Clout scores to decide which peers to sync with first or whose gossip to prioritize.
- The Web UI displays Clout rankings to show which naras are currently "resonant" in the neighborhood.
- High Clout scores may influence social interactions (e.g., naras are more likely to tease high-clout peers).

## 4. Interfaces
- `CloutProjection`: The read-model responsible for computing scores from the ledger.
- `GetClout(naraID)`: Retrieves the current subjective score for a peer.

## 5. Event Types & Schemas
Clout is derived primarily from:
- `social` events (especially `tease`).
- `seen` events (proof of contact).
- `hey-there` / `chau` (presence).
- `observation` (restarts and uptime).

## 6. Algorithms

### Clout Derivation
1. **Base Score**: 1.0.
2. **Interaction Bonus**: For every relevant interaction in the ledger:
   - `Score += InteractionValue * Resonance(Personality) * TimeDecay(Age)`.
3. **Resonance Calculation**:
   - Social naras give more weight to `tease` and `gossip`.
   - Chill naras give less weight to high-restart counts.
   - Agreeable naras penalize excessive teasing.
4. **Time Decay**:
   - `Weight = 1.0 / (1.0 + Age/HalfLife)`.
   - Social events decay faster than "hard facts" like restarts.

## 7. Failure Modes
- **Clout Bubbles**: A small group of naras teasing each other can create a local "clout bubble" that may not be recognized by outsiders.
- **Amnesia**: If a nara prunes too many social events, its clout rankings will shift toward those with more recent activity.

## 8. Security / Trust Model
- **Sybil Resistance**: Because clout is weighted by the observer's own view of the emitter's clout, new or unknown naras cannot easily manipulate the rankings.
- **Signature Required**: Only signed events can contribute to clout.

## 9. Test Oracle
- `TestClout_SubjectiveDivergence`: Verifies that a Social nara and a Chill nara produce different rankings from the same event stream.
- `TestClout_Decay`: Ensures that a nara who stops participating eventually loses its high clout score.
- `TestClout_Resonance`: Validates that personality traits correctly amplify or dampen specific interaction types.

## 10. Open Questions / TODO
- Implement "Global Clout" in the Web UI by averaging opinions from multiple naras.
- Add "Negative Clout" for naras that frequently emit malformed or unverified events.
