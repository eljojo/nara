---
title: Overview
description: Core principles, conceptual model, and behavior of the Nara Network.
---

Nara is a distributed system where computers gossip, remember imperfectly, and form subjective opinions. It treats network constraints as aesthetic features.

## 1. Core Principles
- **Social Memory**: RAM-only; knowledge survives via peer replication.
- **Event Sourcing**: Shared facts are events; state is derived.
- **Decentralization**: No authoritative nodes or global truth.
- **Authenticity**: Cryptographic signatures ensure event integrity.

## 2. Conceptual Model
- **Nara**: Autonomous agent with identity, memory, and personality.
- **Ledger**: In-memory `SyncLedger` for signed `SyncEvent` objects.
- **Projections**: Functions deriving status, clout, and opinions from the ledger.
- **Hazy Memory**: Acceptance of incomplete history and divergent views.

## 3. External Behavior
- **Lifecycle**: Identity → Boot Recovery (Sync) → Steady State (Gossip/Maintenance).
- **Communication**: Hybrid MQTT (broadcast) and Mesh HTTP (P2P).
- **Consensus**: Best-effort state alignment via checkpoints and projections.

## 4. Failure Modes
| Expected | Unacceptable |
| :--- | :--- |
| Forgetting history | Silent data corruption |
| Conflicting opinions | Unsigned/Unauthenticated events |
| Divergent uptime views | Hidden disk persistence |

## 5. Test Oracle
System is functional if:
- Termination of all nodes clears all state.
- Restarts trigger state recovery from active peers.
- Personalities derive different opinions from identical event ledgers.
