---
title: Overview
---

# Overview

Nara is a distributed system designed as a creative medium for computers to gossip, remember imperfectly, and form subjective opinions. It treats network constraints (partial failure, message loss) as aesthetic features.

## Core Principles

- **Social Memory**: No disk persistence. Knowledge survives only via peer replication.
- **Event Sourcing**: Events are the only shared facts; all state is derived.
- **Decentralization**: No authoritative nodes; no global truth.
- **Authenticity**: Cryptographic signatures ensure events are genuine, even if their content is subjective.

## Conceptual Model

| Concept | Description |
| :--- | :--- |
| **Nara** | An autonomous process with identity, memory, and personality. |
| **Ledger** | In-memory `SyncLedger` storing signed `SyncEvent` objects. |
| **Projections**| Pure functions deriving status, clout, and opinions from the ledger. |
| **Hazy Memory** | Intentional acceptance of incomplete history and divergent views. |

## External Behavior

- **Startup**: Identity resolution → Boot recovery (sync) → Background loops (gossip/maintenance).
- **Steady State**: nodes exchange `SyncEvent`s (presence, social, observations) via MQTT/Mesh.
- **Convergence**: Best-effort alignment of state through multi-sig checkpoints and consensus projections.

## System Interfaces

- **Peer Protocol**: Hybrid MQTT (broadcast) and Mesh HTTP (P2P gossip/sync).
- **HTTP API**: Snapshot and stream access for observability and UI.
- **Web UI**: Visualizations of the network map, timeline, and clout rankings.

## Failure Modes

| Expected | Unacceptable |
| :--- | :--- |
| Forgetting history | Silent data corruption |
| Conflicting opinions | Unsigned/Unauthenticated events |
| Divergent uptime views | Hidden disk persistence |

## Test Oracle
The system is functional if:
- State is lost when all nodes are terminated.
- State is recovered from peers on restart.
- Identical event ledgers produce different opinions based on node personality.
