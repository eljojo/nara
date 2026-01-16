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
- **[Identity](/docs/spec/identity/)**: Names, souls, and Ed25519 bonds.
- **[Personality](/docs/spec/personality/)**: Deterministic character traits.
- **[Aura & Avatar](/docs/spec/aura-and-avatar/)**: Visual representation.

### 2. Runtime & Services
- **[Runtime & Primitives](/docs/spec/developer/runtime/)**: Message primitives, Nara OS, and adapters.
- **[Stash Service](/docs/spec/stash/)**: Reference implementation for encrypted distributed storage.

### 3. Event & Memory Model
- **[Events](/docs/spec/events/)**: Immutable signed facts and ledger.
- **[Projections](/docs/spec/projections/)**: Deriving state from event streams.
- **[Memory Model](/docs/spec/memory-model/)**: Pruning, recovery, and forgetting.

### 4. Transport & Sync
- **[Plaza (MQTT)](/docs/spec/plaza-mqtt/)**: Public broadcasts and discovery.
- **[Mesh (HTTP)](/docs/spec/mesh-http/)**: P2P authenticated transport.
- **[Zines](/docs/spec/zines/)**: Hand-to-hand gossip bundles.
- **[Sync Protocol](/docs/spec/sync-protocol/)**: Historical reconciliation.

### 5. Presence & Consensus
- **[Presence](/docs/spec/presence/)**: Discovery and liveness.
- **[Observations](/docs/spec/observations/)**: Distributed state monitoring.
- **[Checkpoints](/docs/spec/checkpoints/)**: Multi-sig historical anchors.

### 6. Social & World
- **[Social Events](/docs/spec/social-events/)**: Teasing, trends, and buzz.
- **[Clout](/docs/spec/clout/)**: Subjective reputation ranking.
- **[World Postcards](/docs/spec/world-postcards/)**: Signature-chained messages.
- **[Network Coordinates](/docs/spec/coordinates/)**: Vivaldi latency mapping.

### 7. Operations & UI
- **[Web UI](/docs/spec/web-ui/)**: Real-time dashboard and inspection.
- **[HTTP API](/docs/spec/http-api/)**: Public and Inspector endpoints.
- **[Boot Sequence](/docs/spec/boot-sequence/)**: Transition to steady-state.
- **[Configuration](/docs/spec/configuration/)**: Flags and environment variables.
- **[Deployment](/docs/spec/deployment/)**: Build and orchestration.

### 8. Developer Section
- **[Developer Guide](/docs/spec/developer-guide/)**: Vision and building services.
- **[Runtime & Test Helpers](/docs/spec/developer/runtime/)**: The Nara OS and MockRuntime.
- **[Pipelines & Stages](/docs/spec/developer/pipelines/)**: Composable message processing.
- **[Behaviors & Patterns](/docs/spec/developer/behaviors/)**: Declarative Kind configuration.
- **[Cryptography (Keyring)](/docs/spec/developer/cryptography/)**: Identity, signing, and self-encryption.
- **[Mesh Client](/docs/spec/developer/mesh-client/)**: Authenticated P2P HTTP communication.
- **[Sample Service (Stash)](/docs/spec/developer/sample-service/)**: Reference implementation deep-dive.

---

## Maintenance
Discrepancies between code and spec are bugs. Code behavior remains the source of truth.
