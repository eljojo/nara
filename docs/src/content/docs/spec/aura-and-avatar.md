---
title: Aura and Avatar
description: Deterministic visual identity and flair in the Nara Network.
---

# Aura and Avatar

Aura and Avatar provide every Nara with a unique visual identity derived from its soul and personality, enabling recognizable UI characters without central registries.

## 1. Purpose
- Individual recognition for autonomous agents.
- Visual feedback on character (e.g., vibrant colors for sociable nodes).
- Reward uptime and participation with "flair" (badges).
- Identify "Barrios" via shared coordinate-based visual traits.

## 2. Conceptual Model
- **Aura**: Deterministic HEX colors (Primary/Secondary) for gradients.
- **Flair**: Emoji badges representing platform, personality, and awards.
- **License Plate**: A "Barrio" emoji derived from coordinate clustering.

### Invariants
- **Soul-Seeded**: Visuals are stable across restarts.
- **Deterministic**: Derived from public `NaraStatus` data.
- **Dynamic**: Flair and aura recompute as metrics (uptime, restarts) change.

## 3. Algorithms

### Aura Derivation (OKLCH Color Space)
1. **Hue**: Seeded by `FNV-1a(Soul + Name)`.
2. **Harmony**: Agreeable nodes use analogous colors; disagreeable nodes use complementary splits.
3. **Vibrance**:
   - High **Sociability** + Low **Chill**: Bright "Neon" palette.
   - High **Chill**: Muted "Noir" tones.
4. **Glow**: Strength increases with uptime.

### Flair Components
String built from:
- **Identity**: 💎 (Valid bond) or ⚪ (Generic).
- **Platform**: 🍓 (RPi), ❄️ (NixOS), ☸️ (K8s), 🐧 (Linux).
- **Awards**: 👑 (Oldest), 👶 (Newest), 🌀 (Most restarts).
- **Social**: Current [Trend](./social-events.md#trend-logic) emoji.

### License Plate (Barrio)
1. Map [Coordinate](./coordinates.md) position to a grid cell.
2. Assign cell-specific emoji as the "License Plate."
3. Peers in the same cell share a plate, forming a local neighborhood.

## 4. Security
- **Verification**: Any node can verify if reported aura/flair matches the peer's soul and personality.
- **Non-reversibility**: Flair/Aura do not reveal the private soul seed.

## 5. Test Oracle
- `TestAura_Determinism`: HEX consistency.
- `TestFlair_PlatformDetection`: Correct badge application.
- `TestAura_PersonalityImpact`: Color shift vs. Chill/Sociability.
