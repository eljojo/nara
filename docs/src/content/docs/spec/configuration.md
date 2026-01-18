---
title: Configuration
description: CLI flags, environment variables, and system defaults in the nara network.
---

Configuration allows users to customize their nara's behavior, identity, and resource usage. nara follows a hierarchy of precedence where CLI flags override environment variables, which in turn override system defaults.

## 1. Purpose
- Enable identity migration via "souls."
- Allow auto-tuning of memory and performance based on host hardware.
- Support hybrid networking configurations (MQTT, Mesh, or both).
- Provide a flexible way to run naras in various environments (local, cloud, embedded).

## 2. Conceptual Model
- **Precedence**: `CLI Flags` > `Environment Variables` > `System Defaults`.
- **Identity Binding**: Identity is bound to a combination of the `Soul`, `Name`, and optionally a `Hardware Fingerprint`.
- **Memory Profiles**: Pre-defined resource limits (`low`, `medium`, `high`) that tune the ledger and stash capacity.
- **Transport Modes**: Selection of the communication backbone (`mqtt`, `gossip`, or `hybrid`).

### Invariants
1. **Soul Privacy**: The soul string (private seed) MUST NEVER be transmitted over the network or stored in public logs.
2. **Stable Identity**: The nara ID MUST be a deterministic result of the soul and name configuration.
3. **Safe Boot**: A nara MUST be able to operate with sensible defaults even if no configuration is provided.

## 3. External Behavior
- naras detect their environment (RAM, hostname) at startup to set initial defaults.
- Configuration is immutable for the lifetime of the process; changes require a restart.
- The `--show-default-credentials` flag allows users to see embedded community credentials for the plaza and mesh.

## 4. Interfaces

### Core Configuration Options
| Flag | Env Var | Default | Description |
| :--- | :--- | :--- | :--- |
| `-mqtt-host` | `MQTT_HOST` | `tls://...` | The plaza MQTT broker address. |
| `-http-addr` | `HTTP_ADDR` | `:8080` | The address for the Web UI and API. |
| `-soul` | `NARA_SOUL` | - | The Base58 identity string. |
| `-name` | `NARA_NAME` | - | The human-readable name for this nara. |
| `-transport` | `TRANSPORT_MODE`| `hybrid` | `mqtt`, `gossip`, or `hybrid`. |
| `-memory-mode` | `MEMORY_MODE` | `auto` | `low`, `medium`, `high`, or `auto`. |
| `-ledger-capacity`| `LEDGER_CAPACITY`| - | Manual override for max ledger events. |

## 5. Event Types & Schemas
Configuration influences the content of the `hey-there` presence event and the `Newspaper` status update.

## 6. Algorithms

### Identity Resolution (`DetermineIdentity`)
1. **Name**: Use `-name` flag > use environment variable > generate a name from the soul hash.
2. **Soul**: Use `-soul` flag > generate a "native" soul from a hash of local hardware IDs and MAC addresses.
3. **Nara ID**: Compute `Base58(SHA256(RawSoulBytes || NameBytes))`.

### Memory Auto-Detection
If `-memory-mode` is set to `auto`:
- **RAM ≤ 384MB**: Select `low` profile (20k events, limited background sync).
- **RAM ≤ 1024MB**: Select `medium` profile (80k events).
- **RAM > 1024MB**: Select `high` profile (320k events).

## 7. Failure Modes
- **Soul/Name Mismatch**: If a provided soul is already bonded to a different name, the nara may be rejected by peers.
- **Port Conflict**: If the `HTTP_ADDR` is already in use, the nara will fail to start.
- **Broker Unreachable**: If the `MQTT_HOST` is invalid or down, the nara will fallback to mesh-only operation (if transport is `hybrid`).

## 8. Security / Trust Model
- **Soul Sovereignty**: The soul is the root of both cryptographic identity and the symmetric key for [Stash](/docs/spec/stash/) decryption.
- **Environment Gating**: Sensitive credentials should be provided via environment variables rather than CLI flags to prevent them from appearing in process lists.

## 9. Test Oracle
- `TestMemoryMode_Parsing`: Verifies that CLI and Env overrides are correctly applied.
- `TestIdentity_Bonding`: Validates that the soul-to-name HMAC bond is correctly checked.
- `TestTransport_Selection`: Ensures the correct transport adapters are initialized based on the config.

## 10. Open Questions / TODO
- Add support for a `config.json` file as an alternative to flags and env vars.
- Implement dynamic memory-mode switching if available RAM changes during runtime.
