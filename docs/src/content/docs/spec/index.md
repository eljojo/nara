---
title: Nara Network Specification
description: Authoritative specification for the Nara Network Go implementation.
---

*A distributed system with hazy memory.*

This document is the **authoritative, re-implementation-grade** specification for the Nara Network.

## Core Principles
- **Memory is Social**: RAM-only; state replicated to peers.
- **Derived State**: Events are the only facts; state is interpreted via replaying.
- **Subjective Truth**: Opinions are shaped by personality and local history.
- **Hazy Memory**: Intentional data loss and forgetting as a system feature.

---

## Spec Index

### 1. Identity & Being
- **[Overview](/docs/spec/overview/)**: Network myth and principles.
- **[Styleguide](/docs/spec/styleguide/)**: Terminology and aesthetics.
- **[Identity](/docs/spec/identity/)**: Names, souls, and Ed25519 bonds.
- **[Personality](/docs/spec/personality/)**: Deterministic character traits.
- **[Aura & Avatar](/docs/spec/aura-and-avatar/)**: Visual representation.

### 2. Event & Memory Model
- **[Events](/docs/spec/events/)**: Immutable signed facts and ledger.
- **[Projections](/docs/spec/projections/)**: Deriving state from event streams.
- **[Memory Model](/docs/spec/memory-model/)**: Pruning, recovery, and forgetting.

### 3. Transport & Sync
- **[Plaza (MQTT)](/docs/spec/plaza-mqtt/)**: Public broadcasts.
- **[Mesh (HTTP)](/docs/spec/mesh-http/)**: P2P WireGuard transport.
- **[Zines](/docs/spec/zines/)**: Hand-to-hand gossip bundles.
- **[Sync Protocol](/docs/spec/sync-protocol/)**: Historical reconciliation.

### 4. Presence & Consensus
- **[Presence](/docs/spec/presence/)**: Discovery and liveness.
- **[Observations](/docs/spec/observations/)**: Uptime/restart monitoring.
- **[Checkpoints](/docs/spec/checkpoints/)**: Multi-sig historical anchors.

### 5. Social & World
- **[Stash](/docs/spec/stash/)**: Distributed encrypted storage.
- **[Social Events](/docs/spec/social-events/)**: Teasing, trends, and buzz.
- **[Clout](/docs/spec/clout/)**: Subjective reputation ranking.
- **[World Postcards](/docs/spec/world-postcards/)**: Signature-chained messages.
- **[Coordinates](/docs/spec/coordinates/)**: Vivaldi network latency mapping.

### 6. Operations & UI
- **[HTTP API](/docs/spec/http-api/)**: Public and Inspector endpoints.
- **[Boot Sequence](/docs/spec/boot-sequence/)**: Transition to steady-state.
- **[Configuration](/docs/spec/configuration/)**: Flags and environment variables.
- **[Deployment](/docs/spec/deployment/)**: Production requirements.

---

## Maintenance
Discrepancies between code and spec are bugs. Code behavior remains the source of truth.
