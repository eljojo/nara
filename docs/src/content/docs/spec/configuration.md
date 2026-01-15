---
title: Configuration
description: CLI flags, environment variables, and memory profiles in Nara.
---

# Configuration

Nara uses CLI flags and environment variables for identity, transport, and resource tuning.

## 1. Purpose
- Deployment flexibility (Metal, Docker, K8s).
- Identity migration via "souls."
- Auto-tuning based on host hardware.
- Hybrid network support (MQTT + Mesh).

## 2. Conceptual Model
- **Precedence**: CLI Flags > Env Vars > Defaults.
- **Identity**: Bound to `Soul + Name + HW Fingerprint`.
- **Memory Profiles**: `Short`, `Medium`, `Hog`, or `Auto`.
- **Transport Modes**: `mqtt`, `gossip`, or `hybrid`.

### Invariants
- **Soul Privacy**: The soul (private seed) is never transmitted.
- **Stable ID**: Nara ID is a deterministic hash of soul and name.
- **Safe Boot**: Operates with functional defaults if unconfigured.

## 3. Interfaces

### Core Configuration
| Flag | Env Var | Default | Description |
| :--- | :--- | :--- | :--- |
| `-mqtt-host` | `MQTT_HOST` | `tls://...` | Broker address. |
| `-http-addr` | `HTTP_ADDR` | `:8080` | API & UI address. |
| `-soul` | `NARA_SOUL` | - | Base58 identity string. |
| `-nara-id` | `NARA_ID` | - | Explicit Nara name. |
| `-transport` | `TRANSPORT_MODE`| `hybrid` | `mqtt`, `gossip`, or `hybrid`. |
| `-memory-mode` | `MEMORY_MODE` | `auto` | `short`, `medium`, `hog`, `auto`. |
| `-ledger-capacity`| `LEDGER_CAPACITY`| - | Max ledger events override. |

- `--show-default-credentials`: View embedded community credentials.

## 4. Algorithms

### Identity Resolution (`DetermineIdentity`)
1. **Name**: `-nara-id` > hostname (if specific) > generate from soul.
2. **Soul**: `-soul` > native soul from `SHA256(HostID + MACs)`.
3. **Nara ID**: `Base58(SHA256(RawSoulBytes || NameBytes))`.

### Memory Auto-Detection
If `-memory-mode` is `auto`:
- **RAM ≤ 384MB**: `short` (20k events, `GOMEMLIMIT=220MiB`, `GOGC=50`).
- **RAM ≤ 1024MB**: `medium` (80k events).
- **RAM > 1024MB**: `hog` (320k events).

### Transport Modes
- **`mqtt`**: Plaza MQTT broadcasts.
- **`gossip`**: Mesh HTTP (tsnet/WireGuard) P2P.
- **`hybrid`**: Standard dual-transport.

## 5. Security
- **Soul Sovereignty**: The soul is the root of identity and [Stash](./stash.md) decryption.
- **Environment Gating**: Use env vars to override embedded credentials in production.

## 6. Test Oracle
- `TestMemoryMode_Parsing` / `TestMemoryMode_CustomOverride`.
- `TestIdentity_Bonding`: Soul-to-name HMAC validation.
- `TestTransport_Selection`: Initialization gating.
