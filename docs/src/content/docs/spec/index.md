---
title: Nara Network Specification
description: Authoritative specification for the Nara Network Go implementation.
---

# Nara Network Specification
*A distributed system with hazy memory.*

This folder contains the **authoritative** specification for the Nara Network. It is designed to be **re-implementation-grade**: an engineer or agent should be able to rebuild Nara using only these documents.

## Core Principles
- **Memory is Social**: No disk persistence; state is replicated to peers.
- **Derived State**: Events are the only facts; all state (opinions/status) is derived.
- **Subjective Truth**: No global state; opinions are shaped by personality and local history.
- **Tolerance for Loss**: Forgetting and "hazy memory" are features, not bugs.

---

## Spec Index

### 1. Identity & Being
- **[Overview](./overview.md)**: Core principles and the network myth.
- **[Identity](./identity.md)**: Names, souls, Ed25519 bonds, and portability.
- **[Personality](./personality.md)**: Agreeableness, Sociability, and Chill.
- **[Aura & Avatar](./aura-and-avatar.md)**: Deterministic visual identity.

### 2. Event & Memory Model
- **[Events](./events.md)**: Types, immutability, signing, and ledger structure.
- **[Projections](./projections.md)**: Deriving state and opinions from event streams.
- **[Memory Model](./memory-model.md)**: Hazy memory, forgetting, and recovery.

### 3. Transport & Sync
- **[Plaza (MQTT)](./plaza-mqtt.md)**: Public square broadcasts and heartbeats.
- **[Mesh (HTTP)](./mesh-http.md)**: P2P gossip and sync over WireGuard.
- **[Zines](./zines.md)**: Curated gossip bundles and editorial memory.
- **[Sync Protocol](./sync-protocol.md)**: Historical reconciliation and sampling.

### 4. Presence & Consensus
- **[Presence](./presence.md)**: Hey-there / Howdy / Chau and liveness signals.
- **[Observations](./observations.md)**: Monitoring uptime and restarts.
- **[Checkpoints](./checkpoints.md)**: Multi-sig anchors and historical consensus.

### 5. Social & World
- **[Stash](./stash.md)**: Distributed encrypted storage and redundancy.
- **[Social Events](./social-events.md)**: Teasing, trends, and interactions.
- **[Clout](./clout.md)**: Subjective reputation and ranking.
- **[World Postcards](./world-postcards.md)**: Journey messages and signature chains.
- **[Coordinates](./coordinates.md)**: Vivaldi network mapping and latency.

### 6. Operations & UI
- **[HTTP API](./http-api.md)**: Public and private endpoints.
- **[Boot Sequence](./boot-sequence.md)**: Startup, recovery, and steady-state loops.
- **[Configuration](./configuration.md)**: Flags, environment variables, and defaults.
- **[Deployment](./deployment.md)**: Production environments and runtime requirements.

---

## Maintenance Protocol
This is a **living specification**. It must remain in sync with the source code and test suite. Behavior in the code is the source of truth; discrepancies are bugs in the spec.
