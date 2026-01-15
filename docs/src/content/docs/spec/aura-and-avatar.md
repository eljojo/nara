---
title: Aura and Avatar
---

# Aura and Avatar

Deterministic visual identity derived from a nara's soul, personality, and uptime. Aura and flair allow the UI to render a unique, recognizable presence without external assets.

## Conceptual Model

| Concept | Description |
| :--- | :--- |
| **Aura** | Two HEX colors (Primary/Secondary) derived from soul + personality. |
| **Flair** | A string of visual tokens (emojis) reflecting identity, platform, and status. |
| **License Plate** | A barrio emoji marker derived from coordinate-based clustering. |

### Invariants
- **Aura**: Deterministic for a given (soul, name, personality, uptime). Recomputed during maintenance.
- **Flair**: Uses public metadata; never reveals the soul.
- **Exposure**: Fields are part of `NaraStatus` and included in all API snapshots.

## Algorithms

### 1. Aura Derivation (OKLCH → sRGB)
1. **Palette**: Determined by personality:
   - High Sociability + Low Chill: **Neon**.
   - High Chill: **Noir**.
   - Low Sociability: **Cool bias**.
   - High Sociability + Mod. Chill: **Warm bias**.
2. **Base Hue**: `FNV-1a(Soul + Name)`.
3. **Harmony**: Selection (Analogous vs Complementary) based on Agreeableness.
4. **Saturation/Lightness**: Derived from Sociability and Chill.
5. **Uptime Influence**: Log-scaled uptime (0-30 days) biases the illuminant strength.

### 2. Flair Derivation
Base + Platform + Trend + Personality + Awards:
- **Base**: 💎 (Valid soul bond) vs ⚪ (Generic/Inauthentic).
- **Platform**: 🍓 (RPi), ❄️ (NixOS), ☸️ (K8s).
- **Social**: Trend-specific emojis; extra tokens if Sociability is high.
- **Awards**: 👑 (Oldest), 👶 (Youngest), 🌀 (Most Restarts).

### 3. License Plate
- Derived from **Barrio Emoji** via neighborhood clustering.
- Uses Vivaldi coordinates if available; falls back to `Hash(Name)`-based grid.

## Interfaces
Fields in `NaraStatus`:
- `Aura.Primary` / `Aura.Secondary` (HEX strings).
- `Flair` (Token string).
- `LicensePlate` (Emoji).

## Test Oracle
- **Determinism**: Identical inputs yield identical HEX values. (`aura_test.go`)
- **Initialization**: Aura is set during boot. (`aura_test.go`)
- **Trends**: Participation correctly modifies Flair. (`social_trend_test.go`)
