# Nara Roadmap & TODO

## 🚀 Current Focus: Runtime Migration
Ref: `DESIGN_NARA_RUNTIME.md`, `PLAN_CHECKPOINTS_V2.md`

Goal: Finish the Runtimes implementation by migrating everything into it. Currently stash is halfway done.

### Core Runtime
- [ ] **Feature Flags for Tests**: Make runtime support turning features on/off to speed up tests (e.g. stash).
- [ ] **Hibernation**: Add nara hibernation managed by runtime.
    - [ ] Go to hibernation when computer busy, come back when idle (debounce).
    - [ ] Catch up on missed events.
    - [ ] Suspend apps.
- [ ] **Transport Abstraction ("Ports")**:
    - [ ] Mesh port (http+tailscale logic).
    - [ ] MQTT port.
    - [ ] Abstract so nothing outside port is aware of transport details.
    - [ ] Allow adding future ports (Kafka, Meshtastic).
    - [ ] Support public URL reachable path (e.g. Heroku hosted without mesh/mqtt).
    - [ ] Env var to change transport mode.
- [ ] **Mesh Auth**: Extract ping-based retry logic into the resolve function.

### Refactoring & Types
- [ ] **Soul Type**: Add `Soul` type (like `NaraName`, `NaraID`) to avoid primitive obsession.
- [ ] **ID-based Indexing**: Passively rewrite to index by ID instead of Name.
    - [ ] Ask AI to help identify opportunities as we work.

### Service Migration & Definition
- [ ] **Stash**: Finish migration.
    - [ ] Stash redistribution on shutdown on behalf of offline confidants.
- [ ] **Nara Registry**:
    - [ ] Internal in-memory database/service.
    - [ ] Lookup by ID, Name, IP.
    - [ ] Collaborate with Observation service for status tracking.
- [ ] **App Definitions** (Internal Maintenance Apps):
    - [ ] **Observations**: Watch events, internal maintenance routines, insert events.
    - [ ] **Zines**: Interact with event store and outside world via mesh.

## 🎨 Web UI
- [ ] **Event Viewer**:
    - [ ] Explain what events mean to the user.
    - [ ] Visualize events like GitHub commits.
- [ ] **Homepage / Dashboard**:
    - [ ] Table view: Add restart count.
    - [ ] Table view: Add uptime since last restart.
    - [ ] Table view: Add last restart date.
- [ ] **Social Features**:
    - [ ] Display "thoughts" (text nodes) in UI.
    - [ ] "Follow" functionality visualization.

## ✨ New Features / Apps
- [ ] **Social Network** (Mastodon-like):
    - [ ] Share thoughts (text nodes).
    - [ ] Follow other naras.
- [ ] **Cachipun (Rock Paper Scissors)**:
    - [ ] App with 3rd party decision maker.
    - [ ] Secure random generation.
    - [ ] Logic: A generates key, B & C bet, A unlocks & decides.
- [ ] **Nara for Mac**:
    - [ ] Desktop version (like Syncthing for Mac).

## 🛠 DevOps & Infrastructure
- [ ] **Hosted/PaaS Mode**:
    - [ ] Support env-vars to determine location ("Nara in Amsterdam").
    - [ ] Container support (Fly.io) without needing custom bash scripts (solve in Go).
    - [ ] Env-var driven configuration.
- [ ] **Rewrite in Rust**: (Long term) Use Golang test suite as spec.

## 🧹 Cleanup & Maintenance
- [ ] **Validation**:
    - [ ] Ensure Nara Name cannot be empty.
    - [ ] Validate Nara Name characters (no emojis, spaces, etc).
- [ ] **Tests**:
    - [ ] Refactor to use helpers for common patterns.
- [ ] **Documentation**:
    - [ ] Create "Apps" folder in docs specification.
    - [ ] Detail each app (Zines, Observations, etc).
- [ ] **Tech Debt**:
    - [ ] Address `TODO` comments in code (`grep TODO`).
