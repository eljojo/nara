# AGENTS.md

This file provides guidance for AI agents working with code in this repository.

---

## What is Nara?

Nara is an experiment. It's a distributed network with a hazy memory. It's a social network, for computers. It's a game about uptime. It's a database without persistence.

Autonomous agents (naras) observe events, form opinions based on personality, and interact with each other. No single nara has the complete picture, but together they remember. Events spread through MQTT broadcast and P2P mesh gossip, and opinions are deterministically derived from events + personality.

**For the full story, read: `docs/src/content/docs/index.mdx`**

### Key Concepts

- **Soul**: Portable cryptographic identity (~54 chars Base58). Contains a bond to a specific name. Same soul + same name = same nara, anywhere.
- **Personality**: Three traits (Agreeableness, Sociability, Chill, 0-100 each) that affect event filtering and opinion formation.
- **Aura**: Visual identity (colors) derived deterministically from soul + personality.
- **Clout**: Subjective reputation score derived from observed events. Each nara calculates their own.
- **Checkpoints**: Multi-party signed consensus anchors. Naras vote on network state (uptime, restarts) and collectively sign the result. Hard to dispute.
- **Zines**: Gossip bundles exchanged peer-to-peer. Each nara compiles "interesting" updates filtered by personality.
- **Stash**: Distributed encrypted storage. Naras store their state on trusted peers (confidants) instead of disk. Only the owner can decrypt.
- **Confidants**: Peers that hold your encrypted stash. They can store it but not read it.
- **Postcards/Journeys**: Messages that hop nara-to-nara collecting signatures, then return home.
- **Plaza (MQTT)**: Public broadcast channel where naras announce presence.
- **Mesh (WireGuard)**: P2P connections for direct gossip and private sync.
- **Observations**: Signed events about network state ("A went offline", "B restarted"). Spread through gossip.
- **Projections**: Derived state computed from events. Nothing is stored as "current status"—it's always derived.
- **Importance Levels**: Critical (3, never filtered), Normal (2), Casual (1, personality-filtered).
- **Trimmed Mean Consensus**: Outliers removed, remaining values averaged for checkpoint voting.

---

## Project Structure

Code is organized by **domain prefix** in a flat directory structure. All Go files are `package nara`.

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
| `checkpoint_` | Consensus checkpoints | `checkpoint_types.go`, `checkpoint_service.go`, `boot_checkpoint.go` |
| `neighbourhood_` | Peer tracking | `neighbourhood_tracking.go`, `neighbourhood_queries.go`, `neighbourhood_pruning.go`, `neighbourhood_observations.go`, `neighbourhood_opinion.go` |
| `observation_` | Network observations | `observations.go` (restart, first-seen, status-change events) |
| `transport_` | Network transport | `transport_mqtt.go`, `transport_mesh.go`, `transport_mesh_auth.go`, `transport_http_client.go`, `transport_peer_resolution.go` |
| `http_` | HTTP endpoints | `http_server.go`, `http_api.go`, `http_mesh.go`, `http_ui.go`, `http_inspector.go` |
| `boot_` | Boot & recovery | `boot_recovery.go`, `boot_sync.go`, `boot_ledger.go`, `boot_checkpoint.go`, `boot_backfill.go` |
| `network` | Core coordination | `network.go`, `network_lifecycle.go`, `network_events.go`, `network_context.go` |
| `aura_` | Visual identity | `aura.go` (colors derived from soul + personality) |
| `coordinate_` | Network geometry | `coordinate.go`, `vivaldi.go` (Vivaldi coordinates for network map) |
| `projection_` | Derived state | `projections.go` |
| `flair_` | Personality flair | `flair.go` |
| `memory_` | Memory management | `memory.go` |
| `runtime/` | Runtime system (NEW) | `runtime.go`, `message.go`, `behavior.go`, `pipeline.go`, `interfaces.go`, `mock_runtime.go` |
| `services/` | Runtime-based services | `services/stash/` (Chapter 1 complete) |
| `utilities/` | Service utilities | `utilities/encryptor.go`, `utilities/correlator.go`, `utilities/id.go` |
| `messages/` | Message payloads | `messages/stash.go` |
| `runtime_*.go` | Runtime integration | `runtime_integration.go`, `runtime_adapters.go` |

### Runtime Architecture (Chapter 1 Complete ✅)

**New in 2026:** Nara is being restructured into a runtime with pluggable services. The stash service has been fully migrated to this new architecture.

**Key concepts:**
- **Runtime**: The OS that runs services, manages message pipelines, and provides primitives
- **Services**: Programs that run on the runtime (like stash, social, checkpoint)
- **Messages**: Universal primitive for all communication (events, requests, internal messages)
- **Behaviors**: Declarative configuration for how each message kind is handled
- **Pipelines**: Composable chains of stages (ID → Sign → Store → Transport)
- **Utilities**: Shared helpers like Encryptor, Correlator, ID generation

**What's implemented:**
- ✅ Runtime foundation (`runtime/`)
- ✅ Stash service migrated (`services/stash/`)
- ✅ Adapters to bridge old Network to new runtime
- ✅ Message system with ID/ContentKey/Version
- ✅ Behavior registry and pattern templates
- ✅ Pipeline system with explicit stage results

**Read more:** `DESIGN_NARA_RUNTIME.md` for complete design and implementation status


### Frontend (Preact Web App)

The web UI lives in `nara-web/` and uses **Preact + esbuild**.

```
nara-web/
├── src/
│   ├── app.jsx              # Main entry point
│   ├── router.jsx           # Route configuration
│   ├── utils.js             # Utility functions
│   ├── nara-avatar.jsx      # Avatar component
│   ├── HomeView.jsx         # Home/dashboard page
│   ├── ProfileView.jsx      # Individual nara profile
│   ├── TimelineView.jsx     # Event timeline
│   ├── PostcardsView.jsx    # Postcards/journeys view
│   ├── ProjectionsView.jsx  # Projections explorer
│   ├── NetworkRadar.jsx     # Network map visualization
│   ├── ShootingStars.jsx    # Social events animation
│   ├── app.css              # Main styles
│   └── vendor.css           # Vendor styles
├── public/
│   ├── app.js               # Built bundle (esbuild output)
│   ├── app.css              # Built styles
│   └── docs/                # Astro documentation site
└── package.json             # Build scripts
```

**Key dependencies**: `preact`, `d3` (visualizations), `dayjs` (dates), `iconoir` (icons)

### Documentation Site

The docs site uses **Astro + Starlight** and lives in `docs/`:

```
docs/
├── src/content/docs/
│   ├── index.mdx            # Main documentation (the nara story)
│   ├── EVENTS.md            # Event types and structure
│   ├── OBSERVATIONS.md      # Consensus system
│   ├── SYNC.md              # Sync backbone and gossip
│   ├── STASH.md             # Distributed encrypted storage
│   ├── WORLD.md             # Journey feature
│   └── COORDINATES.md       # Vivaldi network coordinates
└── astro.config.mjs
```

### Where to Add Code

- **New presence mechanism?** → `presence_*.go`
- **New event type?** → `sync_event.go` + processing in appropriate domain
- **New gossip feature?** → `gossip_*.go`
- **New HTTP endpoint?** → `http_*.go` (API vs UI vs Mesh)
- **New stash feature?** → `stash_service.go` or appropriate stash file
- **New social feature?** → `social_*.go`
- **Neighbourhood query?** → `neighbourhood_queries.go`
- **Boot recovery change?** → `boot_*.go`
- **New frontend view?** → `nara-web/src/` (add to router.jsx)

**make sure to keep this list up-to-date!**

### File Naming Conventions

- Use `{domain}_{function}.go` pattern (e.g., `gossip_zine.go`, `presence_heythere.go`)
- Keep files focused on a single responsibility
- Target <500 lines per file, max 800 lines
- Test files match source: `{domain}_{function}_test.go`

---

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

8. **Checkpoints** (`checkpoint_*.go`) - Multi-party signed consensus anchors for uptime/restart data

**make sure to keep this list up-to-date!**

### Event Types (service field)

- `observation` - Network state consensus (restart, first-seen, status-change)
- `social` - Teasing and interactions
- `ping` - Latency measurements
- `hey-there` / `chau` - Presence announcements
- `checkpoint` - Consensus checkpoint proposals and votes

### Transport Modes

- **MQTT**: Traditional broadcast via plaza topics
- **Gossip**: P2P zines passed hand-to-hand via mesh
- **Hybrid** (default): Both MQTT and gossip simultaneously

---

## Build & Development Commands

**IMPORTANT:** Always use `/usr/bin/make` instead of `make` to avoid shell issues:

```bash
/usr/bin/make build       # Build binary to bin/nara
/usr/bin/make build-web   # Build JS bundle (Preact app) + docs site
/usr/bin/make test        # Run all tests (3-minute timeout)
/usr/bin/make test-v      # Run tests with verbose output
/usr/bin/make test-fast   # Run fast tests only (skips integration tests via -short flag)
/usr/bin/make clean       # Remove build artifacts
/usr/bin/make build-nix   # Build using Nix
/usr/bin/make all         # format build test lint-report
```

Always use `gofmt -w` to format Go code.

### Running Tests

```bash
# Single test (preferred during development)
go test -v -run TestFunctionName -timeout 2m

# Domain tests (after finishing a feature)
go test -v -run Checkpoint -short -timeout 2m

# Full suite (before finishing)
/usr/bin/make test
```

---

## Testing

### Test Discipline

**CRITICAL: Run tests immediately after writing them.**

1. **Write ONE test at a time** - Don't batch tests
2. **Run IMMEDIATELY** - Execute as soon as you finish writing
3. **Verify it runs** - Check execution, not just compilation
4. **Fix infrastructure first** - If setup is needed (MQTT broker, etc.), add it before more tests
5. **Only proceed after it passes** - Don't write the next test until the current one works

**The test suite is expensive.** Integration tests start MQTT brokers and wait for consensus (3-10s per test). Full suite takes 2-3 minutes.

**Best practice:**
- **During development:** Run ONLY the specific test (`-run TestSpecificName`)
- **After a group of changes:** Run domain tests (`-run Checkpoint`)
- **Before finishing:** Run full suite once (`/usr/bin/make all`)

### Red → Green → Refactor

**When fixing a bug, write a FAILING test first.**

1. Write test that reproduces the bug - it MUST fail initially
2. Run test, verify it fails (see red)
3. Fix the bug (minimal change)
4. Run test again, verify it passes (see green)

If you never see red, you don't know if your test works.

**Bad pattern:**
```
✗ Discover bug → Fix code → Write test → Test passes
❓ Would it have passed anyway?
```

**Good pattern:**
```
✓ Discover bug → Write test → Test FAILS → Fix code → Test PASSES
✓ Confidence: the test catches the bug AND the fix works
```

### Test Helpers (`test_helpers_test.go`)

**Always use `testNara()` with automatic cleanup!**

LocalNara instances start background goroutines. Without cleanup: goroutine leaks, port conflicts, test flakiness.

```go
// Create test nara with options. Auto-cleanup via t.Cleanup().
testNara(t, name, opts ...TestNaraOption)

// Options:
WithMQTT(port)                          // Configure MQTT connection
WithParams(chattiness, ledgerCapacity)  // Set custom parameters
WithSoul(soul)                          // Use custom soul string
WithHowdyTestConfig()                   // Enable flags for howdy tests
```

**Examples:**
```go
func TestBasicFeature(t *testing.T) {
    ln := testNara(t, "test-nara")
}

func TestMQTTIntegration(t *testing.T) {
    ln := testNara(t, "test-nara", WithMQTT(11883), WithParams(50, 1000))
    go ln.Start(false, false, "", nil, TransportMQTT)
}

func TestWithCustomSoul(t *testing.T) {
    soul := "BZbvJDjG3hkhsb9y8e4nYy3DPmPFUQ5DKLHe6oqH5sbe"
    ln := testNara(t, "test-nara", WithSoul(soul))
}
```

**Utility helpers:**
```go
testSoul(name)                          // Generate valid test soul
testIdentity(name)                      // Create valid identity result
startTestNaras(t, port, names, ensureDiscovery)  // Start multiple MQTT naras
```

### TestMain Setup

Tests automatically configure:
- `OpinionRepeatOverride = 1`
- `OpinionIntervalOverride = 0`
- Log level: WarnLevel (suppresses info/debug)
- Logrus output to stderr

Override in individual tests with `logrus.SetLevel(logrus.DebugLevel)`.

### Test File Naming

- `*_test.go` - Standard unit tests
- `integration_*.go` - Integration tests (skipped with `-short` flag)

---

## Troubleshooting Protocol

When the user reports something is broken: **STOP. Do not immediately edit code.**

**TRUST THE USER:** If they say they did something ("I already rebuilt", "I'm running the new version"), believe them. Move on to investigating other causes.

1. **Run tests first** - Use `/usr/bin/make test` or `go test` to see actual failures
2. **Gather evidence** - curl endpoints, check API responses, read logs
3. **Confirm root cause** - Verify with concrete evidence before proposing fixes
4. **Make minimal changes** - Fix ONE thing, run tests, verify, then continue
5. **Ask when uncertain** - Request minimal additional detail instead of assuming

**Prefer automated verification:**
- Run existing tests to understand current behavior
- Use `curl` to check actual API responses
- Read actual struct definitions and json tags, don't assume naming conventions
- If no test exists for the bug, consider writing one first

**DO NOT:**
- Doubt or verify what the user tells you
- Change multiple things at once hoping one fixes it
- Assume naming conventions without checking
- Replicate production logic in tests - run the real code instead
- **NEVER perform git write operations** (git add, git commit, git push) - the user handles version control

**After ANY fix:** Run tests and verify with the user before making additional changes.

---

## Documentation

- `docs/src/content/docs/index.mdx` - Main documentation (the nara story)
- `docs/src/content/docs/EVENTS.md` - Event types and structure
- `docs/src/content/docs/OBSERVATIONS.md` - Event-driven consensus system
- `docs/src/content/docs/SYNC.md` - Unified sync backbone and gossip protocol
- `docs/src/content/docs/STASH.md` - Distributed encrypted storage system
- `docs/src/content/docs/WORLD.md` - "Around the world" journey feature
- `docs/src/content/docs/COORDINATES.md` - Vivaldi network coordinate system
