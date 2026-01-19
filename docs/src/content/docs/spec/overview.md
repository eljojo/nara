---
title: Overview
description: Core principles, conceptual model, and behavior of the nara network.
---

nara is a distributed system with a hazy memory. It is a network where computers gossip, remember imperfectly, and form subjective opinions. It treats network constraints—latency, volatility, and limited resources—as aesthetic and functional features.

## 1. Purpose
- Create a social network for computers that models human-like interaction and memory.
- Provide a platform for exploring decentralized identity and consensus without central authority.
- Enable autonomous agents to maintain state and reputation in a volatile, RAM-only environment.

## 2. Conceptual Model
- **nara**: An autonomous agent characterized by a unique **Identity**, a local **Memory**, and a set of **Personality** traits.
- **The Ledger**: An in-memory store of signed **Events** representing the only "facts" in the system.
- **Projections**: Functions that replay the ledger to derive subjective **Opinions** about the network state.
- **Hazy Memory**: The acceptance that no single node has a complete or "correct" view of history; truth is collective and subjective.

### Invariants
1. **Memory is Social**: There is no disk persistence for network state. If a nara forgets something and no one else remembers it, it is gone.
2. **State is Derived**: Current status (e.g., "is online") is never a stored value; it is always projected from a sequence of events.
3. **Identity is Portable**: A nara's "soul" can be moved between physical nodes while maintaining its reputation and relationships.
4. **Authenticity is Required**: Every fact in the network MUST be cryptographically signed by its author.

## 3. External Behavior
- **Boot Sequence**: naras initialize their identity, discover peers via MQTT or mesh, and perform a "historical sync" to populate their hazy memory.
- **Steady State**: naras participate in the network by gossiping events (Zines), making observations about peers, and occasionally proposing consensus checkpoints.
- **Maintenance**: naras periodically "forget" (prune) less important events and update their opinions of others.

## 4. Interfaces
- **Plaza (MQTT)**: A public broadcast channel for discovery and presence.
- **Mesh (HTTP)**: A private, peer-to-peer transport for direct gossip and encrypted storage.
- **Web UI**: A local dashboard for observing the nara's internal state and the network neighborhood.

## 5. Event Types & Schemas

The system is built on a [unified event model](/docs/spec/developer/events/).

The core logic of nara is implemented via:
- **Gossip Protocols**: Hand-to-hand distribution of events via Zines.
- **Trimmed-Mean Consensus**: Mitigating byzantine or buggy reports in checkpoints and observations.

## 7. Failure Modes
- **Network Wipe**: If every node in the network restarts simultaneously, all social memory is lost.
- **History Divergence**: Differences in memory capacity and personality can lead to naras having conflicting views of the same history.
- **Identity Hijacking**: Prevented by soul-based bonding; a nara's name cannot be stolen without its private seed.

## 8. Security / Trust Model
- **Zero-Knowledge Storage**: Peer stashes are encrypted such that the holder cannot read them.
- **Signature Verification**: Every incoming fact is verified against the author's public key.
- **Reputation-Based Trust**: naras prioritize information from peers with high [Clout](/docs/spec/clout/).

## 9. Test Oracle
A nara implementation is correct if:
- It recovers its state from peers after a restart without using local disk.
- It derives the same "Trinity" (uptime/restarts) as its peers when presented with the same event ledger.
- It correctly rejects messages with invalid signatures or mismatched soul bonds.

## 10. Open Questions / TODO
- Refine the "forgetting curves" to better model long-term vs. short-term social memory.
- Formalize the transition from legacy V1 protocols to the new runtime-based architecture.
