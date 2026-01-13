# AGENTS.md

This file provides guidance for AI agents when working with code in this repository.

## Troubleshooting Protocol (READ BEFORE DEBUGGING)

When the user reports something is broken: **STOP. Do not immediately edit code.**

1. **Run tests first** - Use `make test` or `go test` to see actual failures. Integration tests run real production code - trust them over assumptions.
2. **Gather evidence** - If tests pass, get actual error output: curl endpoints, check API responses, read logs. Run the real code, don't guess.
3. **Confirm the root cause** - Verify with concrete evidence before proposing fixes
4. **Make minimal changes** - Fix ONE thing, run tests again, verify it works, then continue if needed
5. **Ask when uncertain** - If evidence is missing, ask the user for the minimal additional detail instead of assuming

**Prefer automated verification:**
- Run existing tests to understand current behavior
- Use `curl` or similar to check actual API responses
- Read the actual struct definitions and json tags, don't assume naming conventions
- If a test doesn't exist for the bug, consider writing one first

**DO NOT:**
- Change multiple things at once hoping one fixes it
- Assume naming conventions, field formats, or data shapes without checking
- Replicate production logic in tests - run the real code instead
- **NEVER perform git write operations** (git add, git commit, git push, etc.) - the user will handle version control

**After ANY fix:** Run tests and verify with the user before making additional changes.

---

## What is Nara?

Nara is an experiment. It's a distributed network with a hazy memory. It's a social network, for computers. It's a game about uptime. It's a database without persistence.

Autonomous agents (naras) observe events, form opinions based on personality, and interact with each other. No single nara has the complete picture, but together they remember. Events spread through MQTT broadcast and P2P mesh gossip, and opinions are deterministically derived from events + personality.

## Project Structure

Code is organized by **domain prefix** in a flat directory structure. All files are `package nara`.

### Domain Map

| Prefix | Purpose | Key Files |
|--------|---------|-----------|
| `identity_` | Cryptographic identity | `identity_soul.go`, `identity_crypto.go`, `identity_detection.go`, `identity_attestation.go` |
| `sync_` | Event backbone | `sync_event.go`, `sync_ledger.go`, `sync_request.go`, `sync_helpers.go` |
| `presence_` | Network presence | `presence_heythere.go`, `presence_howdy.go`, `presence_chau.go`, `presence_newspaper.go`, `presence_starttime.go`, `presence_projection.go` |
| `gossip_` | P2P gossip protocol | `gossip_zine.go`, `gossip_exchange.go`, `gossip_discovery.go`, `gossip_dm.go` |
| `stash_` | Distributed storage | `stash_types.go`, `stash_manager.go`, `stash_confidant.go`, `stash_service.go`, `stash_tracker.go` |
| `social_` | Social interactions | `social_events.go`, `social_tease.go`, `social_clout.go`, `social_trend.go`, `social_buzz.go`, `social_network.go` |
| `world_` | World journeys | `world_message.go`, `world_handler.go`, `world_network.go` |
| `checkpoint_` | Consensus checkpoints | `checkpoint_types.go`, `checkpoint_service.go` |
| `neighbourhood_` | Peer tracking | `neighbourhood_tracking.go`, `neighbourhood_queries.go`, `neighbourhood_pruning.go`, `neighbourhood_observations.go`, `neighbourhood_opinion.go` |
| `transport_` | Network transport | `transport_mqtt.go`, `transport_mesh.go`, `transport_mesh_auth.go`, `transport_http_client.go`, `transport_peer_resolution.go` |
| `http_` | HTTP endpoints | `http_server.go`, `http_api.go`, `http_mesh.go`, `http_ui.go`, `http_inspector.go` |
| `boot_` | Boot & recovery | `boot_recovery.go`, `boot_sync.go`, `boot_ledger.go`, `boot_checkpoint.go`, `boot_backfill.go` |
| `network` | Core coordination | `network.go`, `network_lifecycle.go`, `network_events.go`, `network_context.go` |

### Where to Add Code

When adding new features, follow these guidelines:

- **New presence mechanism?** → Add to `presence_*.go` (e.g., new announcement type)
- **New event type?** → Update `sync_event.go` and add processing to appropriate domain
- **New gossip feature?** → Add to `gossip_*.go` (e.g., new zine validation)
- **New HTTP endpoint?** → Add to appropriate `http_*.go` based on purpose (API vs UI vs Mesh)
- **New stash feature?** → Update `stash_service.go` or appropriate stash file
- **New social feature?** → Add to `social_*.go` (e.g., new tease reason)
- **Neighbourhood query?** → Add to `neighbourhood_queries.go`
- **Boot recovery change?** → Update appropriate `boot_*.go` file

### File Naming Conventions

- Use `{domain}_{function}.go` pattern (e.g., `gossip_zine.go`, `presence_heythere.go`)
- Keep files focused on a single responsibility
- Target <500 lines per file, max 800 lines
- Test files match source: `{domain}_{function}_test.go`

## Build & Development Commands

**IMPORTANT:** Always use `/usr/bin/make` instead of `make` to avoid shell issues:

```bash
/usr/bin/make build       # Build binary to bin/nara
/usr/bin/make build-web   # Build JS bundle (Preact app)
/usr/bin/make test        # Run all tests (2-minute timeout)
/usr/bin/make test-v      # Run tests with verbose output
/usr/bin/make test-fast   # Run fast tests only (skips integration tests via -short flag)
/usr/bin/make clean       # Remove build artifacts
/usr/bin/make build-nix   # Build using Nix
```

- When working, always use `gofmt -w` to format code.

### Running a Single Test

```bash
go test -v -run TestFunctionName ./...
go test -v -run TestFunctionName -timeout 2m  # with timeout
```

## Architecture Overview

### Event-Sourced Design

Nara uses event sourcing where state is derived from events rather than stored directly:

```
ledger (facts) → derivation function → opinions
opinion = f(events, soul, personality)
```

Same events + same personality = same opinions (deterministic).

### Core Components

1. **SyncLedger** (`sync_*.go`) - Unified event store holding all syncable events
   - `sync_event.go` - Event types and payloads
   - `sync_ledger.go` - Ledger implementation
   - `sync_request.go` - Request/response protocol
2. **Projections** - Derived views computed from events:
   - `social_clout.go` - Social reputation scoring
   - `presence_projection.go` - Online/offline state tracking
   - `neighbourhood_opinion.go` - Opinion consensus derivation
3. **Identity** (`identity_*.go`) - Cryptographic portable identity using Ed25519
   - `identity_soul.go` - Soul generation and validation
   - `identity_crypto.go` - Ed25519 keypair operations
   - `identity_detection.go` - Identity discovery
   - `identity_attestation.go` - Identity attestation protocol
4. **Transport** - Hybrid MQTT + mesh architecture:
   - `transport_mqtt.go` - MQTT broker connectivity (Plaza broadcasts)
   - `transport_mesh.go` - Tailscale/Headscale P2P networking (Zine gossip)
   - `transport_mesh_auth.go` - Mesh authentication middleware
   - `network.go` - Core network coordination
5. **Stash** (`stash_*.go`) - Distributed encrypted storage:
   - HTTP-based bidirectional exchange (piggybacks on gossip)
   - Memory-only storage (no disk persistence)
   - Memory-aware limits (5/20/50 based on memory mode)
   - Smart confidant selection (prefer high memory + uptime)
   - XChaCha20-Poly1305 encryption (owner-only decryption)
6. **Social** (`social_*.go`) - Teasing, trends, clout
7. **Neighbourhood** (`neighbourhood_*.go`) - Distributed consensus on network state and peer tracking

### Event Types (service field)

- `observation` - Network state consensus (restart, first-seen, status-change)
- `social` - Teasing and interactions
- `ping` - Latency measurements
- `hey-there` / `chau` - Presence announcements

### Transport Modes

- **MQTT**: Traditional broadcast via plaza topics
- **Gossip**: P2P zines passed hand-to-hand via mesh
- **Hybrid** (default): Both MQTT and gossip simultaneously

## Testing Patterns

### Test Helpers (`test_helpers_test.go`)

```go
testSoul(name)                          // Generate valid test soul
testLocalNara(name)                     // Create test LocalNara with valid identity
testLocalNaraWithParams(name, chattiness, ledgerCapacity)
testIdentity(name)                      // Create valid identity result
```

### TestMain Setup

Tests automatically configure:
- `OpinionRepeatOverride = 1`
- `OpinionIntervalOverride = 0`
- Log level set to WarnLevel (suppresses info/debug)

Individual tests can override with `logrus.SetLevel(logrus.DebugLevel)`.

### Test File Naming

- `*_test.go` - Standard unit tests
- `integration_*.go` - Integration tests (skipped with `-short` flag)

## Key Concepts

- **Soul**: Portable cryptographic identity (~54 chars Base58)
- **Personality**: Agreeableness, Sociability, Chill (0-100 each) - affects event filtering and opinion formation
- **Clout**: Subjective reputation score derived from observed events
- **Importance Levels**: Critical (3, never filtered), Normal (2), Casual (1, personality-filtered)
- **Trimmed Mean Consensus**: Outliers removed, remaining values averaged

## Documentation

- `docs/src/content/docs/EVENTS.md` - Event types and structure
- `docs/src/content/docs/OBSERVATIONS.md` - Event-driven consensus system
- `docs/src/content/docs/SYNC.md` - Unified sync backbone and gossip protocol
- `docs/src/content/docs/STASH.md` - Distributed encrypted storage system
- `docs/src/content/docs/WORLD.md` - "Around the world" journey feature
- `docs/src/content/docs/COORDINATES.md` - Vivaldi network coordinate system
