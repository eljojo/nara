---
title: Nara Network Specification
description: Authoritative specification for the Nara Network Go implementation.
---

# Nara Network Specification
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
- **[Overview](./overview.md)**: Network myth and principles.
- **[Styleguide](./styleguide.md)**: Terminology and aesthetics.
- **[Identity](./identity.md)**: Names, souls, and Ed25519 bonds.
- **[Personality](./personality.md)**: Deterministic character traits.
- **[Aura & Avatar](./aura-and-avatar.md)**: Visual representation.

### 2. Event & Memory Model
- **[Events](./events.md)**: Immutable signed facts and ledger.
- **[Projections](./projections.md)**: Deriving state from event streams.
- **[Memory Model](./memory-model.md)**: Pruning, recovery, and forgetting.

### 3. Transport & Sync
- **[Plaza (MQTT)](./plaza-mqtt.md)**: Public broadcasts.
- **[Mesh (HTTP)](./mesh-http.md)**: P2P WireGuard transport.
- **[Zines](./zines.md)**: Hand-to-hand gossip bundles.
- **[Sync Protocol](./sync-protocol.md)**: Historical reconciliation.

### 4. Presence & Consensus
- **[Presence](./presence.md)**: Discovery and liveness.
- **[Observations](./observations.md)**: Uptime/restart monitoring.
- **[Checkpoints](./checkpoints.md)**: Multi-sig historical anchors.

### 5. Social & World
- **[Stash](./stash.md)**: Distributed encrypted storage.
- **[Social Events](./social-events.md)**: Teasing, trends, and buzz.
- **[Clout](./clout.md)**: Subjective reputation ranking.
- **[World Postcards](./world-postcards.md)**: Signature-chained messages.
- **[Coordinates](./coordinates.md)**: Vivaldi network latency mapping.

### 6. Operations & UI
- **[HTTP API](./http-api.md)**: Public and Inspector endpoints.
- **[Boot Sequence](./boot-sequence.md)**: Transition to steady-state.
- **[Configuration](./configuration.md)**: Flags and environment variables.
- **[Deployment](./deployment.md)**: Production requirements.

---

## Maintenance
Discrepancies between code and spec are bugs. Code behavior remains the source of truth.
