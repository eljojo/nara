---
title: Nara Network — Living Specification
description: Re-implementation-grade English specification for the Nara Network.
---

# The Nara Network  
*A distributed system with hazy memory.*

This folder contains the **authoritative English specification** for the Nara Network.

These documents are not explanatory blog posts and not marketing copy.  
They are **re-implementation-grade specs**: another engineer or AI should be able to rebuild Nara using only the contents of this folder.

The spec is **living** and must be kept in sync with the source code and test suite.
When behavior changes, the spec must change with it.

---

## How to Read This Spec

- Each file describes **one feature or domain**.
- Every feature file follows a fixed structure (see the protocol).
- If something is ambiguous, the code and tests decide.
- If something is weird, it is probably intentional—document the weirdness.

---

## Core Ideas (High-Level)

Before diving into individual features, the system rests on a few foundational ideas:

- Naras are **stateful but not persistent**.
- All memory is **social**: replicated to peers, never written to disk.
- Events are immutable facts; **state is always derived**.
- No node has the whole truth.
- Opinions are subjective and shaped by personality.
- Forgetting is expected and acceptable.
- Reliability emerges from overlap, not authority.

These ideas should be reflected consistently across all spec files.

---

## Spec Map

### 1. Overview & Philosophy

- **[overview.md](./overview.md)**  
  What Nara is, what it is not, and the guiding principles behind the system.

---

### 2. Identity & Being

- **[identity.md](./identity.md)**  
  Names, hardware identity, souls, cryptographic bonds, portability, and authenticity.

- **[personality.md](./personality.md)**  
  Agreeableness, Sociability, Chill, and how personality shapes memory and opinion.

- **[aura-and-avatar.md](./aura-and-avatar.md)**  
  Deterministic visual identity derived from soul and personality.

---

### 3. Event & Memory Model

- **[events.md](./events.md)**  
  Event types, immutability, signing, and the event ledger.

- **[projections.md](./projections.md)**  
  How naras derive state and opinions from events.

- **[memory-model.md](./memory-model.md)**  
  Hazy memory, forgetting, replay, and recovery on boot.

---

### 4. Transport & Sync

- **[plaza-mqtt.md](./plaza-mqtt.md)**  
  The public square: MQTT broadcast, announcements, and shared visibility.

- **[mesh-http.md](./mesh-http.md)**  
  Peer-to-peer gossip and sync over HTTP + WireGuard.

- **[zines.md](./zines.md)**  
  Curated gossip bundles and editorial memory.

- **[sync-protocol.md](./sync-protocol.md)**  
  How naras ask “what did I miss?” and merge partial histories.

---

### 5. Presence, Observation & Consensus

- **[presence.md](./presence.md)**  
  Hey-there / howdy / chau, liveness, and restarts.

- **[observations.md](./observations.md)**  
  Observing uptime, offline periods, and restarts as signed events.

- **[checkpoints.md](./checkpoints.md)**  
  Trimmed-mean voting, multi-signature anchors, and historical truth.

---

### 6. Persistence Without Disks

- **[stash.md](./stash.md)**  
  Distributed encrypted storage, confidants, redundancy, and recovery.

---

### 7. Social Layer

- **[social-events.md](./social-events.md)**  
  Teasing, trends, buzz, and other social interactions.

- **[clout.md](./clout.md)**  
  Subjective reputation, disagreement, and non-global rankings.

---

### 8. World Features

- **[world-postcards.md](./world-postcards.md)**  
  Journey messages, signature chains, and “around the world” hops.

- **[coordinates.md](./coordinates.md)**  
  Latency measurement, Vivaldi coordinates, and the network map.

---

### 9. Interfaces & UI

- **[http-api.md](./http-api.md)**  
  HTTP endpoints exposed by naras and the observatory.

- **[web-ui.md](./web-ui.md)**  
  The field guide: dashboards, timelines, maps, and visualizations.

---

### 10. Runtime & Operations

- **[boot-sequence.md](./boot-sequence.md)**  
  Startup, recovery, background loops, and steady-state behavior.

- **[configuration.md](./configuration.md)**  
  CLI flags, environment variables, and runtime options.

- **[deployment.md](./deployment.md)**  
  Running naras in the real world (VMs, clouds, home labs).

---

## Spec Status

This spec is **actively maintained** by an automated agent following the
**Nara Spec Maintenance Protocol**.

At any given time:
- Some files may be more complete than others.
- Missing details should be filled by reading code and tests.
- Ambiguities should be resolved in favor of actual behavior.

If a feature exists in code but not here, **that is a bug in the spec**.

---

## For Builders

If you are here to:
- re-implement Nara
- reason about its behavior
- or teach another system how it works

You should be able to do so using only this folder.

If you cannot, the spec is incomplete—please fix it.

Welcome.  
The machines are already gossiping.
