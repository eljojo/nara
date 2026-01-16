---
title: Aura and Avatar
description: Deterministic visual identity and flair in the nara network.
---

Aura and Avatar provide every nara with a unique visual identity derived from its soul and personality. This enables recognizable UI characters without the need for central registries or user-uploaded avatars.

## 1. Purpose
- Enable individual recognition of autonomous agents in a decentralized network.
- Provide visual feedback on a nara's character (e.g., vibrant colors for sociable nodes).
- Reward long-term participation and stability with "flair" (badges).
- Identify local "Barrios" (neighborhoods) via shared coordinate-based visual traits.

## 2. Conceptual Model
- **Aura**: A set of deterministic HEX colors (Primary and Secondary) used for gradients and UI accents.
- **Flair**: Emoji-based badges that represent a nara's platform, personality achievements, and awards.
- **Barrio License Plate**: An emoji derived from a nara's position in the [Network Coordinates](/docs/spec/coordinates/).

### Invariants
1. **Soul-Seeded**: Visual identities MUST be stable across restarts and migrations.
2. **Deterministic**: All visual traits are derived from public `NaraStatus` data.
3. **Dynamic Elements**: While the base aura is stable, elements like glow intensity and specific flairs may change as metrics (uptime, restarts) evolve.

## 3. External Behavior
- The Web UI uses the aura colors to theme the dashboard and profile pages for each nara.
- Peers can verify that a nara's reported colors and flair match its claimed soul and personality.
- Auras and flairs are re-calculated during periodic maintenance to reflect current uptime and network standing.

## 4. Interfaces
- `Aura` Structure: `primary` (HEX), `secondary` (HEX).
- `computeAura()`: The internal function that generates colors from soul, personality, and uptime.
- `hey-there` / `Newspaper`: Transports carrying the `Aura` and `Flair` data.

## 5. Event Types & Schemas
Visual identity is part of the `NaraStatus` struct, which is shared via presence events and sync responses.

## 6. Algorithms

### Aura Derivation (OKLCH Color Space)
The system uses the OKLCH color space (Lightness, Chroma, Hue) to ensure perceptual uniformity and aesthetic quality:
1. **Hue**: Seeded by a hash of `Soul + Name`.
2. **Lightness (L) & Chroma (C)**:
   - `L = 0.52 + 0.18*Chill - 0.06*Sociability`.
   - `C = 0.06 + 0.22*Sociability + 0.05*(1-Chill)`.
3. **Harmony (Secondary Color)**:
   - **Agreeable** naras use Analogous harmonies (cozy neighbors on the color wheel).
   - **Disagreeable** naras use Triadic or Complementary harmonies (high contrast).
4. **Palette Modifiers**:
   - `ModNeon`: High sociability, low chill (vibrant, high contrast).
   - `ModNoir`: High chill (darker, lower chroma).
   - `ModWarmBias` / `ModCoolBias`: Subtle nudges based on sociability.
5. **Memory Tint (Illuminant)**: Uptime and personality apply a "memory tint" (e.g., `IllTungsten` for warm indoor vibes or `IllLED` for cool digital vibes) that shifts the colors over time.

### Flair Components
The flair string is a collection of emojis representing:
- **Identity**: 💎 (Valid soul bond).
- **Platform**: 🐧 (Linux), ❄️ (NixOS), ☸️ (K8s), 🍓 (Raspberry Pi).
- **Achievements**: 👑 (Oldest known), 👶 (Newest seen), 🌀 (Most restarts).
- **Social**: Current [Trend](/docs/spec/social-events/) emoji.

### Barrio License Plate
1. Map the nara's [Vivaldi Coordinates](/docs/spec/coordinates/) to a 2D grid.
2. Assign a unique emoji to each grid cell (the "Barrio").
3. naras with similar latency profiles share the same emoji, visually grouping them as neighbors.

## 7. Failure Modes
- **Color Collision**: While rare, two naras may have very similar auras if their souls produce similar hashes.
- **Coordination Drift**: If coordinates shift significantly, a nara's "Barrio" license plate may change.

## 8. Security / Trust Model
- **Verification**: Any observer can re-run the `computeAura` and `Flair` logic to verify that a peer isn't "lying" about its visual identity to masquerade as someone else.
- **Seed Privacy**: The algorithms are one-way; you cannot derive a nara's private soul seed from its aura colors.

## 9. Test Oracle
- `TestAura_Determinism`: Verifies that the same soul + personality always produces the same HEX colors.
- `TestAura_PersonalityImpact`: Validates that changing "Chill" or "Sociability" shifts the palette as expected.
- `TestFlair_PlatformDetection`: Ensures the correct platform emoji is applied based on the runtime environment.
- `TestAura_UptimeShift`: Confirms that the "Memory Tint" (Illuminant) correctly applies as uptime increases.

## 10. Open Questions / TODO
- Implement "Aura Pulse" in the UI to reflect real-time network activity (Buzz).
- Add support for custom "Accessories" that can be unlocked via high Clout.
