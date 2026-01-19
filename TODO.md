# Nara Roadmap & TODO

> **Current Focus**: Finish the Runtime migration by migrating all services into it.
> Status: Stash service is halfway done (Chapter 1 nearly complete).
> Refs: `DESIGN_NARA_RUNTIME.md`, `PLAN_CHECKPOINTS_V2.md`

---

## Priority Legend

- **[P0]** Critical / Blocking other work
- **[P1]** High impact / Should do soon
- **[P2]** Medium impact / Nice to have
- **[P3]** Low priority / Future

---

## 1. Runtime Migration (Current Focus)

The runtime is the foundation. Everything else builds on it.

### Chapter 1: Stash Service [P0]
- [ ] Finish stash migration (nearly complete)
- [ ] Stash redistribution on shutdown on behalf of offline confidants
- [ ] Wire runtime to existing Network lifecycle

### Chapter 2: Remaining Services [P0]
- [ ] Migrate presence service (hey-there, chau, newspaper, howdy)
- [ ] Migrate social service
- [ ] Migrate checkpoint service
- [ ] Migrate neighbourhood/observations
- [ ] Migrate gossip service (zines)
- [ ] Complete receive pipeline with all stages

### Feature Flags for Testing [P1]
> *Speeds up development cycle significantly*
- [ ] Make runtime support turning features on/off for tests
- [ ] Example: disable stash in tests that don't need it
- [ ] Pattern: `runtime.Config{DisabledServices: []string{"stash"}}`

### Ports Abstraction (Transport) [P1]
> *Clean architecture, enables future transports*
- [ ] Define Port interface abstracting transport logic
- [ ] Mesh port (http+tailscale logic) — nothing outside should know about tailscale
- [ ] MQTT port — nothing outside should know about MQTT topics
- [ ] Env var to switch transport mode (`NARA_TRANSPORT=mqtt|mesh|hybrid`)
- [ ] Support "reachable path" for public URL naras (Heroku without mesh/mqtt)
- [ ] Future: Kafka port, Meshtastic port

### Hibernation [P2]
> *Better resource usage, especially for desktop*
- [ ] Add hibernation mode managed by runtime
- [ ] Trigger: go to hibernation when computer busy
- [ ] Trigger: come back online when idle (with debounce)
- [ ] On wake: catch up on missed events
- [ ] Apps/services are suspended during hibernation

---

## 2. Architecture & Types

### Soul Type [P1]
> *Avoid primitive obsession, like we did for NaraName and NaraID*
- [ ] Create `Soul` type wrapping the ~54 char Base58 string
- [ ] Add validation methods
- [ ] Migrate existing `string` soul references

### ID-based Indexing [P1]
> *Ongoing: prefer NaraID over NaraName as primary key*
- [ ] Passively migrate to index by ID instead of Name as we touch code
- [ ] Identify opportunities during feature work
- [ ] Code TODOs to address:
  - [ ] `boot_backfill.go`: ping.Target should be NaraID not string
  - [ ] `gossip_exchange.go`: fix casting of targets to strings

### Nara Registry [P1]
> *Evolve neighbourhood tracking into a proper registry — like the Contacts app on Mac*
- [ ] Merge with/evolve from `neighbourhood_*.go` code
- [ ] In-memory database of known naras
- [ ] Lookup by: ID, Name, IP
- [ ] Collaborate with Observation service for status tracking
- [ ] Single source of truth for "who do we know"

### Name Validation [P2]
- [ ] Ensure Nara name cannot be empty (nil safety)
- [ ] Validate characters: no emojis, no spaces, etc.
- [ ] Consistent validation across entry points

### Mesh Auth Refactor [P2]
- [ ] Extract ping-based retry logic into resolve function
- [ ] Clean up retry/backoff patterns in `transport_mesh_auth.go`

---

## 3. Web UI

### Event Viewer [P1]
> *Make events understandable and browsable*
- [ ] Explain to user what each event means (tooltips/descriptions)
- [ ] Visualize events like GitHub commits (timeline, diffs)
- [ ] Filter/search events

### Dashboard / Homepage [P2]
- [ ] Table view: add restart count column
- [ ] Table view: add uptime since last restart
- [ ] Table view: add last restart date
- [ ] Better visualization of nara status

### Social UI [P2]
> *Once social network feature exists*
- [ ] Display "thoughts" (text posts) in the UI
- [ ] Show follow relationships
- [ ] Social feed view

---

## 4. New Features / Apps

### Social Network [P2]
> *New feature, separate from existing teasing system*
- [ ] Naras can post "thoughts" (text nodes)
- [ ] Naras can follow other naras
- [ ] Thoughts propagate through gossip
- [ ] Mastodon-like but for naras

### Cachipun (Rock Paper Scissors) [P3]
> *Fun game with third-party decision maker*
- [ ] Player A generates secret key
- [ ] Players B & C submit bets (secure random generation)
- [ ] Player A unlocks and determines winner
- [ ] Event-sourced game state

### App Definitions (Internal Maintenance) [P2]
> *Formalize the concept of "apps" in docs*
- [ ] **Observations app**: watches events, runs internal maintenance, inserts events
- [ ] **Zines app**: interacts with event store, communicates via mesh
- [ ] Create "Apps" folder in docs specification
- [ ] Document each app's responsibilities

---

## 5. Infrastructure & DevOps

### PaaS / Hosted Mode [P2]
> *Run nara on Fly.io, Railway, etc. with location awareness*
- [ ] Read location env vars (e.g., `FLY_REGION`) to know "this nara is in Amsterdam"
- [ ] Solve in Go (not bash scripts) since Nix container doesn't have bash
- [ ] Env-var driven configuration for cloud deployments
- [ ] Document supported env vars

### Nara for Mac [P3]
> *Desktop app like Syncthing for Mac*
- [ ] Menu bar app or full desktop app
- [ ] System tray integration
- [ ] Start on login option
- [ ] Native notifications

---

## 6. Cleanup & Maintenance

### Logging Consolidation [P1]
> *Two primitives: `runtime.Logger` for services, `LogService` for event batching*

**Goal**: All logging should use one of two patterns:
1. **Service logging** → `s.log.Info("message")` via `runtime.ServiceLog`
2. **Event presentation** → `LogService.Push()` / `BatchXXX()` for aggregated output

**Migrate to runtime.Logger**:
- [ ] Audit all `logrus.Infof/Debugf/Warnf/Errorf` calls
- [ ] Services should use `s.log.Info()` (injected via `Init()`)
- [ ] Runtime-internal code should use scoped loggers (e.g., `rt.Log("runtime")`)
- [ ] Network/transport code should use consistent service names

**Quiet tests by default**:
- [ ] Create shared test logger that disables noisy services
- [ ] Add to `TestMain` or test helpers: `logger.Disable("stash", "presence", ...)`
- [ ] Keep errors/warnings visible for debugging
- [ ] Document pattern in `CLAUDE.md` for AI assistants

**Clean up LogService**:
- [ ] Remove `LogService.Info/Warn/Error` convenience methods (they just call logrus)
- [ ] Keep only `Push()` and `BatchXXX()` for event aggregation
- [ ] Document the two-primitive pattern in code comments

### Test Improvements [P1]
- [ ] Refactor tests to use helpers for common patterns
- [ ] Fix flakey tests (see `TODO(flakey)` markers):
  - [ ] `presence_howdy_test.go` has multiple flakey tests
- [ ] Add missing integration tests:
  - [ ] `boot_recovery.go`: fetchSyncEventsFromMesh
  - [ ] `gossip_discovery.go`: fetchPublicKeysFromPeers
  - [ ] `transport_peer_resolution.go`: queryPeerAt

### Code TODOs [P2]
> *From `grep TODO` — technical debt to address*

**Signature Rollout** (`TODO(signature)`):
- [ ] `presence_chau.go`: Re-enable ID signature verification
- [ ] `presence_heythere.go`: Re-enable ID signature verification
- [ ] `presence_newspaper.go`: Enforce signature validation (currently warns)
- [ ] `network_signature_compat_test.go`: Add tests after rollout

**API Deprecation** (target: 2026-07):
- [ ] `boot_checkpoint.go`: Remove Mode: "page" fallback
- [ ] `boot_recovery.go`: Remove Mode: "sample" fallback
- [ ] `http_api.go`: Remove old sync endpoints

**Other**:
- [ ] `sync_event.go`: Fix journey storage in Witness field (likely breaking something)
- [ ] `transport_mqtt.go`: Remove local.mu.Lock hack
- [ ] `mesh_client.go`: Add PostGossipZine when Zine type signature confirmed
- [ ] `mesh_client.go`: Add SendDM when DirectMessage type confirmed
- [ ] `boot_recovery.go`: Move verification logic into MeshClient

---

## 7. Long-term / Aspirational

### Rewrite in Rust [P3]
> *Serious long-term goal (1-2 years)*
- [ ] Use Golang test suite as specification
- [ ] Incremental approach: start with core types
- [ ] Maintain Go version as reference implementation
- [ ] Benefits: memory safety, single binary, WASM target

---

## Connections & Dependencies

```
Runtime Migration (Ch 1 & 2)
    └─► Feature Flags for Testing
    └─► Ports Abstraction
            └─► PaaS/Hosted Mode
            └─► Reachable Path (public URL)
    └─► Hibernation (after services migrated)

Soul Type + ID-based Indexing
    └─► Nara Registry (uses IDs as primary key)
            └─► Observation Service integration
            └─► Neighbourhood merge

Name Validation ◄── Can do anytime

Social Network (new feature)
    └─► Social UI in Web

Rust Rewrite ◄── After runtime stabilizes, use Go tests as spec
```

---

## Suggested Order of Work

1. **Finish Stash** (current) — unblocks runtime Chapter 2
2. **Feature Flags** — speeds up all subsequent dev
3. **Ports Abstraction** — clean architecture before more services
4. **Remaining Service Migration** — complete runtime
5. **Soul Type + ID Indexing** — cleaner foundation
6. **Nara Registry** — merge neighbourhood, enables features
7. **Test Improvements** — reduce flakiness, increase confidence
8. **Name Validation** — quick win, prevents bugs
9. **Event Viewer UI** — user-facing improvement
10. **Code TODOs** — ongoing tech debt reduction
11. **Social Network** — new feature once foundation solid
12. **Everything else** — based on interest/need
