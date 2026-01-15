---
title: Configuration
---

# Configuration

Controls identity, transport, memory profiles, and endpoints via CLI flags and environment variables.

## Conceptual Model

| Concept | Rule |
| :--- | :--- |
| **Precedence** | Flags > Environment Variables > Defaults. |
| **Identity** | Derived from `-nara-id` and `-soul`. |
| **Transport** | `hybrid` (default), `mqtt`, or `gossip`. |
| **Memory** | Profile (auto, short, medium, hog) determines ledger capacity and GC tuning. |

## Memory Profiles

| Mode | Budget | Max Events | Sync Behavior |
| :--- | :--- | :--- | :--- |
| **`short`** | 256MB | 20,000 | Background sync disabled; GOMEMLIMIT=220MiB. |
| **`medium`**| 512MB | 80,000 | Standard background operations. |
| **`hog`** | 2GB | 320,000 | High-capacity history. |
| **`auto`** | - | - | Derived from system/cgroup memory. |

## CLI & Environment Reference

### Core Services
- `-mqtt-host` / `MQTT_HOST`: (Default: `tls://mqtt.nara.network:8883`)
- `-http-addr` / `HTTP_ADDR`: API/UI port. (Default: `:8080`)
- `-headscale-url` / `HEADSCALE_URL`: Mesh control server.
- `-authkey` / `TS_AUTHKEY`: Mesh authentication.

### Identity
- `-nara-id` / `NARA_ID`: Explicit name override.
- `-soul` / `NARA_SOUL`: Base58 soul string.

### Performance & Behavior
- `-memory-mode`: `short`, `medium`, `hog`, `auto`.
- `-transport`: `mqtt`, `gossip`, `hybrid`.
- `-ledger-capacity`: Force override of `MaxEvents`.
- `-refresh-rate`: Presence heartbeat interval in seconds. (Default: 600)
- `-serve-ui`: Enable the web dashboard.

## Algorithms

### Memory Auto-Detection
1. Check system/cgroup memory availability.
2. Select closest default profile (Short/Medium/Hog).
3. If `-ledger-capacity` is set, switch to `custom` mode and override `MaxEvents`.

### Transport Parsing
- `mqtt`: MQTT broadcast only.
- `gossip`: P2P mesh (HTTP) only.
- `hybrid`: Simultaneous MQTT + Gossip (Primary mode).

## Security
- **Identity**: Souls (private keys) are kept in memory; never transmitted.
- **Obfuscation**: Default credentials for common services are embedded but not considered secrets.

## Test Oracle
- **Detection**: Auto-mode selects correct profile for budget. (`memory_test.go`)
- **Override**: Ledger capacity correctly replaces profile defaults. (`memory_test.go`)
