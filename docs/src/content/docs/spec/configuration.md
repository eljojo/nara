---
title: Configuration
description: CLI flags, environment variables, and memory profiles in Nara.
---

# Configuration

Nara is configured primarily through command-line flags and environment variables. The configuration system handles identity resolution, transport selection, and automatic memory profile detection.

## Purpose
- Provide flexible deployment options (bare metal, Docker, Kubernetes).
- Allow identity migration through the use of "souls".
- Automatically tune resource usage based on the host's hardware.
- Support both public (MQTT) and private (Mesh) network modes.

## Conceptual Model
- **Precedence**: Command-line flags override environment variables, which override default values.
- **Identity Resolution**: Identity is determined by combining the Nara's name, its soul, and its hardware fingerprint.
- **Memory Profiles**: Profiles (Auto, Short, Medium, Hog) define the memory budget and event ledger capacity.
- **Transport Modes**: Determines which network layers are active (MQTT, Gossip, or Hybrid).

### Invariants
- **Soul Security**: The "soul" (which contains the private key seed) is never transmitted over the network or stored in heartbeats.
- **Stable ID**: The `Nara ID` is a deterministic hash of the soul and name, ensuring it stays stable even if the Nara moves between machines.
- **Default-Safe**: Nara should boot with functional defaults if no configuration is provided.

## Interfaces

### Core CLI Flags & Environment Variables
| Flag | Env Var | Default | Description |
| :--- | :--- | :--- | :--- |
| `-mqtt-host` | `MQTT_HOST` | `tls://...` | MQTT broker address. |
| `-http-addr` | `HTTP_ADDR` | `:8080` | Address for the API and Web UI. |
| `-soul` | `NARA_SOUL` | - | Base58 string to inherit a specific identity. |
| `-nara-id` | `NARA_ID` | - | Explicitly set the Nara's name. |
| `-transport` | `TRANSPORT_MODE`| `hybrid` | Network mode: `mqtt`, `gossip`, or `hybrid`. |
| `-memory-mode` | `MEMORY_MODE` | `auto` | Memory profile: `short`, `medium`, `hog`, `auto`. |
| `-ledger-capacity`| `LEDGER_CAPACITY`| - | Manual override for max events in the ledger. |
| `-serve-ui` | - | `false` | Enable the embedded web dashboard. |
| `-read-only` | - | `false` | Monitor the network without participating. |

### Default Credentials
Nara includes embedded default credentials for the community MQTT broker and Headscale mesh. These can be viewed using the `--show-default-credentials` flag.

## Algorithms

### 1. Identity Resolution (`DetermineIdentity`)
1. **Name**: Use `-nara-id`, or the system hostname if it's "interesting" (non-generic), otherwise generate from the soul.
2. **Soul**: Use the provided `-soul` flag, or derive a "native" soul from the hardware fingerprint (HostID + MAC addresses).
3. **Nara ID**: `Base58(SHA256(RawSoulBytes || NameBytes))`.

### 2. Memory Auto-Detection (`AutoMemoryProfile`)
If `-memory-mode` is `auto`:
1. Detect system memory or cgroup memory limit.
2. Choose profile:
   - **Budget <= 384MB**: `short` mode (20k events).
   - **Budget <= 1024MB**: `medium` mode (80k events).
   - **Budget > 1024MB**: `hog` mode (320k events).
3. In `short` mode, also set `GOMEMLIMIT=220MiB` and `GOGC=50` to minimize heap usage.

### 3. Transport Selection
- **`mqtt`**: Broadcast-only mode using Plaza MQTT.
- **`gossip`**: P2P-only mode using Mesh HTTP (tsnet/WireGuard).
- **`hybrid`**: The standard mode using both MQTT for real-time signaling and Gossip for bulk data.

## Failure Modes
- **Invalid Soul**: If a provided soul is malformed, Nara will fail to boot or fall back to its native hardware identity.
- **Port Conflict**: If port `8080` or `7433` is taken, the HTTP or Mesh server will fail to start.
- **Auth Failure**: Incorrect MQTT or Mesh credentials will isolate the Nara from parts of the network.

## Security / Trust Model
- **Soul Sovereignty**: The Nara's soul is its ultimate credential. Knowledge of the soul allows anyone to impersonate that Nara or decrypt its [Stash](./stash.md).
- **Hardened Defaults**: Production deployments should override default credentials using environment variables.

## Test Oracle
- `TestMemoryMode_Parsing`: Validates that mode strings are correctly normalized.
- `TestMemoryMode_CustomOverride`: Ensures `-ledger-capacity` correctly replaces profile defaults.
- `TestIdentity_Bonding`: Checks that the soul-to-name HMAC bond is correctly validated.
- `TestTransport_Selection`: Verifies that only the requested transport layers are initialized.
