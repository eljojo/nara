---
title: Aura and Avatar
description: Deterministic visual identity and flair in the Nara Network.
---

# Aura and Avatar

Aura and Avatar provide every Nara with a unique, deterministic visual identity derived from its soul and personality. This allows the web UI to render recognizable characters without the need for external assets or central registries.

## Purpose
- Create a sense of individual "being" and recognition for autonomous agents.
- Provide immediate visual feedback on a Nara's character (e.g., flashy colors for social naras).
- Reward network participation and uptime with visual markers (flair).
- Enable "Barrio" identification through shared visual traits.

## Conceptual Model
- **Aura**: A set of two HEX colors (Primary and Secondary) used for gradients and glows.
- **Flair**: A string of emojis that act as badges or "stickers" on a Nara's profile.
- **License Plate**: A specific "Barrio" emoji derived from coordinate-based clustering.
- **Avatar**: The combination of name, aura, and flair rendered in the UI.

### Invariants
- **Soul-Seeded**: Visual traits are derived from the soul and name, making them stable across restarts.
- **Personality-Influenced**: Traits like Agreeableness and Sociability shift the color palette and emoji selection.
- **Transparency**: All data used for visual derivation is public and verifiable via `NaraStatus`.

## External Behavior
- **Recognition**: Users and other naras can identify a peer by its unique color combination and flair set.
- **Maintenance**: A Nara's aura and flair are recomputed periodically as metrics like uptime or total restarts change.

## Algorithms

### 1. Aura Derivation
The Nara uses the **OKLCH** color space (translated to sRGB) for perceptually uniform colors:
1. **Hue**: Seeded by `FNV-1a(Soul + Name)`.
2. **Harmony**: Agreeable naras use analogous colors; disagreeable ones use complementary or high-contrast splits.
3. **Luminance & Saturation**: 
   - High **Sociability** + Low **Chill** results in bright, vibrant "Neon" palettes.
   - High **Chill** results in muted, dark "Noir" tones.
4. **Uptime Glow**: Longer uptime increases the "illuminant" strength of the colors.

### 2. Flair Derivation
A Nara's flair string is built from multiple components:
- **Base Bond**: 💎 (if soul bond is valid) or ⚪ (if generic/inauthentic).
- **Platform**: 🍓 (Raspberry Pi), ❄️ (NixOS), ☸️ (Kubernetes), or 🐧 (Generic Linux).
- **Personality Traits**: Emojis reflecting high/low Sociability or Chill.
- **Awards**: 👑 (Oldest known), 👶 (Newest known), 🌀 (Chaos award for most restarts).
- **Trends**: Current [Trend](./social-events.md#trend-logic) emoji is appended.

### 3. License Plate (Barrio)
1. Determine the Nara's position (using Vivaldi or Hash-based grid).
2. Map position to a specific grid cell (Barrio).
3. Assign the emoji corresponding to that cell as the "License Plate".
4. Peers in the same Barrio share the same plate, creating a sense of local neighborhood.

## Security / Trust Model
- **Verification**: Since the algorithm is open, any Nara can verify that another Nara's reported Aura and Flair correctly match its Soul and Personality.
- **Privacy**: Flair never reveals the underlying soul seed, only its derivative properties.

## Test Oracle
- `TestAura_Determinism`: Verifies that identical souls produce identical HEX colors.
- `TestFlair_PlatformDetection`: Checks that platform emojis (RPi, NixOS) are correctly applied.
- `TestAura_PersonalityImpact`: Validates that color vibrance changes correctly based on Chill and Sociability scores.
