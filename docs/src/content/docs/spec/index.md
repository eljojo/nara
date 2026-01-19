---
title: Nara Network Specification
description: Authoritative specification for the nara network Go implementation.
---

*A distributed system with hazy memory.*

This document is the **authoritative, re-implementation-grade** specification for the nara network.

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
- **[Identity](/docs/spec/runtime/identity/)**: Names, souls, and Ed25519 bonds.
- **[Personality](/docs/spec/personality/)**: Deterministic character traits.
- **[Aura & Avatar](/docs/spec/aura-and-avatar/)**: Visual representation.

### 2. Runtime Architecture
- **[Runtime & Primitives](/docs/spec/runtime/runtime/)**: Message primitives, nara OS, and adapters.
- **[Pipelines & Stages](/docs/spec/runtime/pipelines/)**: Composable message processing.
- **[Behaviours & Patterns](/docs/spec/runtime/behaviours/)**: Declarative Kind configuration.

### 3. Event & Memory Model
- **[Events](/docs/spec/developer/events/)**: Immutable signed facts and ledger.
- **[Projections](/docs/spec/projections/)**: Deriving state from event streams.
- **[Memory Model](/docs/spec/memory-model/)**: Pruning, recovery, and forgetting.

### 4. Transport & Sync
- **[Plaza (MQTT)](/docs/spec/developer/plaza-mqtt/)**: Public broadcasts and discovery.
- **[Mesh (HTTP)](/docs/spec/developer/mesh-http/)**: P2P authenticated transport.
- **[Sync Protocol](/docs/spec/developer/sync/)**: Historical reconciliation.

### 5. Features
- **[Zines](/docs/spec/features/zines/)**: Hand-to-hand gossip bundles.
- **[Stash](/docs/spec/features/stash/)**: Distributed encrypted storage.
- **[World Postcards](/docs/spec/features/world-postcards/)**: Signature-chained messages.
- **[Web UI](/docs/spec/features/web-ui/)**: Real-time dashboard and inspection.

### 6. Services
- **[Presence](/docs/spec/presence/)**: Discovery and liveness.
- **[Observations](/docs/spec/services/observations/)**: Distributed state monitoring.
- **[Checkpoints](/docs/spec/services/checkpoints/)**: Multi-sig historical anchors.
- **[Social Events](/docs/spec/services/social/)**: Teasing, trends, and buzz.
- **[Clout](/docs/spec/clout/)**: Subjective reputation ranking.
- **[Network Coordinates](/docs/spec/services/coordinates/)**: Vivaldi latency mapping.

### 7. Operations & UI
- **[HTTP API](/docs/spec/http-api/)**: Public and Inspector endpoints.
- **[Boot Sequence](/docs/spec/boot-sequence/)**: Transition to steady-state.
- **[Configuration](/docs/spec/configuration/)**: Flags and environment variables.
- **[Deployment](/docs/spec/deployment/)**: Build and orchestration.

### 8. Developer Section
- **[Developer Guide](/docs/spec/developer-guide/)**: Vision and building services.
- **[Events Reference](/docs/spec/developer/events/)**: Event types and structure.
- **[Sync Protocol](/docs/spec/developer/sync/)**: Sync internals.
- **[Plaza MQTT](/docs/spec/developer/plaza-mqtt/)**: MQTT implementation details.
- **[Mesh HTTP](/docs/spec/developer/mesh-http/)**: HTTP mesh details.
- **[Cryptography (Keyring)](/docs/spec/developer/cryptography/)**: Identity, signing, and self-encryption.
- **[Mesh Client](/docs/spec/developer/mesh-client/)**: Authenticated P2P HTTP communication.
- **[Sample Service (Stash)](/docs/spec/developer/sample-service/)**: Reference implementation deep-dive.

---

## Maintenance
Discrepancies between code and spec are bugs. Code behavior remains the source of truth.
