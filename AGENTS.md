# AGENTS.md

This file provides guidance for AI agents when working with code in this repository.

## What is Nara?

Nara is an experiment. It's a distributed network with a hazy memory. It's a social network, for computers. It's a game about uptime. It's a database without persistence.

Autonomous agents (naras) observe events, form opinions based on personality, and interact with each other. No single nara has the complete picture, but together they remember. Events spread through MQTT broadcast and P2P mesh gossip, and opinions are deterministically derived from events + personality.

## Build & Development Commands

```bash
make build       # Build binary to bin/nara
make test        # Run all tests (2-minute timeout)
make test-v      # Run tests with verbose output
make test-fast   # Run fast tests only (skips integration tests via -short flag)
make clean       # Remove build artifacts
make build-nix   # Build using Nix
```

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

1. **SyncLedger** (`sync.go`) - Unified event store holding all syncable events
2. **Projections** - Derived views computed from events:
   - `projection_clout.go` - Social reputation scoring
   - `projection_online_status.go` - Online/offline state tracking
   - `projection_opinion.go` - Opinion consensus derivation
3. **Identity** (`soul.go`, `identity.go`, `crypto.go`) - Cryptographic portable identity using Ed25519
4. **Transport** - Hybrid MQTT + mesh architecture:
   - `mqtt.go` - MQTT broker connectivity (Plaza broadcasts)
   - `mesh.go` - Tailscale/Headscale P2P networking (Zine gossip)
   - `network.go` - Core network coordination
5. **Stash** (`stash.go`, `stash_sync_tracker.go`) - Distributed encrypted storage:
   - HTTP-based bidirectional exchange (piggybacks on gossip)
   - Memory-only storage (no disk persistence)
   - Memory-aware limits (5/20/50 based on memory mode)
   - Smart confidant selection (prefer high memory + uptime)
   - XChaCha20-Poly1305 encryption (owner-only decryption)
6. **Social** (`social.go`, `teasing_test.go`) - Teasing, trends, clout
7. **Observations** (`observations.go`) - Distributed consensus on network state

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

- `EVENTS.md` - Event types and structure
- `OBSERVATIONS.md` - Event-driven consensus system
- `SYNC.md` - Unified sync backbone and gossip protocol
- `STASH.md` - Distributed encrypted storage system
- `WORLD.md` - "Around the world" journey feature
- `COORDINATES.md` - Vivaldi network coordinate system

## Troubleshooting Protocol

- Do not guess the root cause. Confirm with failing tests, reproduction steps, or concrete error output before proposing or applying fixes.
- If evidence is missing, ask the user for the minimal additional detail (logs, exact command/URL, steps) instead of assuming.
- Keep changes minimal and reversible; avoid broad edits while narrowing down an issue.
