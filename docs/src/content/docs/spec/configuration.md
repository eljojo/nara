# Configuration

## Purpose

Configuration controls identity selection, transport mode, memory profile,
and network endpoints. It is the only way to change runtime behavior without
modifying code.

## Conceptual Model

- Flags and environment variables map 1:1 to runtime settings.
- Memory mode determines ledger capacity and background behavior.
- Transport mode selects MQTT, gossip, or both.

Key invariants:
- `-nara-id` and `-soul` are the only identity inputs from CLI/env.
- `-ledger-capacity` overrides memory mode limits.
- Short memory mode may clamp Go GC limits if not explicitly configured.

## External Behavior

On startup, the binary:
- Reads flags and env vars.
- Applies defaults for MQTT and Headscale credentials.
- Derives identity and keypair.
- Initializes memory profile and ledger limits.
- Starts network services based on transport mode.

## Interfaces

Main binary (`cmd/nara`):

Flags and env:
- `-mqtt-host` / `MQTT_HOST` (default: `tls://mqtt.nara.network:8883`)
- `-mqtt-user` / `MQTT_USER` (default: obfuscated in binary)
- `-mqtt-pass` / `MQTT_PASS` (default: obfuscated in binary)
- `-http-addr` / `HTTP_ADDR` (default: `:8080`)
- `-nara-id` / `NARA_ID` (explicit name override)
- `-soul` / `NARA_SOUL` (Base58 soul to inherit identity)
- `-show-neighbours` (default: true)
- `-refresh-rate` (seconds, default: 600)
- `-force-chattiness` (default: -1 for auto)
- `-verbose` (debug logging)
- `-vv` (debug logging + Tailscale logs)
- `-read-only` (no messages emitted)
- `-serve-ui` (serve web UI)
- `-public-url` / `PUBLIC_URL` (public UI URL)
- `-no-mesh` (disable Headscale/tsnet mesh)
- `-headscale-url` / `HEADSCALE_URL` (control server URL)
- `-authkey` / `TS_AUTHKEY` (Headscale auth key)
- `-memory-mode` / `MEMORY_MODE` (auto, short, medium, hog, custom)
- `-ledger-capacity` / `LEDGER_CAPACITY` (override MaxEvents)
- `-transport` / `TRANSPORT_MODE` (mqtt, gossip, hybrid)
- `-show-default-credentials` (changes flag descriptions to show defaults)

Memory modes (defaults):
- short: 256MB budget, 20,000 events, background sync disabled
- medium: 512MB budget, 80,000 events
- hog: 2GB budget, 320,000 events
- auto: derived from system/cgroup memory

Short mode runtime tuning:
- If `GOMEMLIMIT` is unset, set to 220MiB.
- If `GOGC` is unset, set to 50.

Backup binary (`cmd/nara-backup`):
- `dump-events` subcommand:
  - `-name` (required), `-soul` (required)
  - `-verbose`, `-timeout`, `-workers`, `-ids-from`
- `restore-events` subcommand (see `cmd/nara-backup/restore.go`).

## Event Types & Schemas (if relevant)

Configuration does not emit events directly, but it changes:
- Transport mode announced in `NaraStatus.TransportMode`.
- Memory profile fields in `NaraStatus`.

## Algorithms

Memory profile selection:
1. Parse `MEMORY_MODE` (default: auto).
2. If auto, detect memory budget from system or cgroup.
3. Apply default profile for the chosen mode.
4. If `LEDGER_CAPACITY` > 0, override `MaxEvents` and set mode to custom.

Transport mode parsing:
- `mqtt` -> MQTT only
- `gossip` -> mesh only
- `hybrid` -> both (default)
- unknown -> warning + fallback to hybrid

## Failure Modes

- Invalid `-soul` string yields an inauthentic identity with empty ID.
- Bad `MEMORY_MODE` falls back to medium.
- Unknown `TRANSPORT_MODE` falls back to hybrid.

## Security / Trust Model

- Default MQTT/Headscale credentials are obfuscated, not secret.
- Identity secrets (souls) never leave the process.

## Test Oracle

- Memory mode auto-detection chooses mode based on budget. (`memory_test.go`)
- Ledger capacity overrides memory mode. (`memory_test.go`)

## Open Questions / TODO

- Expose explicit `-name` alias for `-nara-id` (documentation-only improvement).
