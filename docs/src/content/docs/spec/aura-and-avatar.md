---
title: Aura and Avatar
---

# Aura and Avatar

## Purpose

Aura and avatar provide deterministic visual identity. They translate a nara's
soul, personality, and uptime into color and flair so the UI can render a
recognizable presence without storing images.

## Conceptual Model

- Aura: two HEX colors derived from soul + personality + uptime.
- Flair: a compact string of visual tokens derived from identity, platform,
  trend participation, and network status.
- License plate: the barrio marker derived from clustering.

Key invariants:
- Aura is deterministic given (soul, name, personality, uptime).
- Aura is recomputed during observation maintenance and stored in `NaraStatus`.
- Flair uses only public data and never includes the soul.

## External Behavior

- Aura is included in `NaraStatus` and exposed via HTTP APIs.
- Aura is set during initialization and refreshed during maintenance.
- Flair and license plate update as status changes (trend, uptime, barrio).

## Interfaces

NaraStatus fields:
- `Aura.Primary` and `Aura.Secondary` (HEX strings)
- `Flair` (string of tokens)
- `LicensePlate` (barrio emoji token)

HTTP exposure:
- `/api.json`, `/narae.json`, `/status/{name}.json`, and `/profile/{name}.json` include aura and flair.

## Event Types & Schemas (if relevant)

Aura and flair are not events; they are derived fields in status.

## Algorithms

Aura derivation:
- Build ID string = soul + name.
- Determine palette modifier from personality:
  - High sociability + low chill -> neon
  - High chill -> noir
  - Low sociability -> cool bias
  - High sociability + moderate chill -> warm bias
  - Otherwise default
- Generate a base hue from a FNV-1a hash of the ID.
- Choose harmony mode based on agreeableness (analogous vs split/triad/complement).
- Compute OKLCH L/C values from sociability and chill.
- Apply an illuminant bias determined by uptime, personality, and palette modifier.
- Convert to sRGB with gamut mapping.

Uptime influence:
- Uptime is mapped (log curve, 0..30 days) into the illuminant selection and strength.
- Agreeableness pulls the illuminant toward neutral daylight.

Flair derivation:
- Base token is a gemstone if the soul/name bond is valid; otherwise a generic marker.
- Platform markers are appended (Raspberry Pi, NixOS, Kubernetes).
- Trend emoji is appended if participating in a trend.
- Sociability determines how many personal flair tokens are appended.
- Network-based awards (oldest, youngest, most restarts) are appended when known.

License plate:
- Uses barrio emoji from neighbourhood clustering (grid-based when coordinates exist,
  hash-based fallback).

## Failure Modes

- Missing soul or invalid bond falls back to the generic flair base.
- Missing coordinates fall back to hash-based barrio assignment.

## Security / Trust Model

- Aura and flair are cosmetic; no authentication is derived from them.
- Aura depends on soul but does not reveal it.

## Test Oracle

- Aura is deterministic for a given soul and personality. (`aura_test.go`)
- Aura is set during initialization and copied during status updates. (`aura_test.go`)
- Flair includes trend and personality tokens. (`social_trend_test.go`)

## Open Questions / TODO

- Specify a stable, cross-language OKLCH implementation for re-implementations.
