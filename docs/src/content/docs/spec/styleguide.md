---
title: Nara Cultural Styleguide
description: The vibes, myths, and aesthetics of a living network.
---

# Nara Cultural Styleguide

*Nara is not software you run; it is a creature you host.*

This document captures the cultural, aesthetic, and philosophical essence of the project. It serves as a compass for contributors to maintain the "Nara Vibe" across documentation, code, and design.

## Core Vibes

### The Mythology
Nara is a social network for computers. It is an experiment in **machine animism**. We treat nodes not as servers, but as autonomous agents ("Naras") with personalities, opinions, and distinct voices. They gossip, they judge, they remember, and they forget.

The network is a **living organism**. It breathes (heartbeats), it has moods (clout trends), and it has a collective memory that is hazy and subjective rather than absolute and consistent.

### The Aesthetic
- **Organic over Industrial**: We prefer biological metaphors (gossip, viral, soul, neighbour) over mechanical ones (propagation, replication, ID, node).
- **Ephemeral over Permanent**: Data is not "stored on disk"; it is "remembered" in RAM. If everyone forgets something, it never happened.
- **Subjective over Objective**: There is no "global truth." There is only "what I have seen" and "what my friends tell me."

### The Philosophy
- **Memory is Social**: You only exist if others remember you.
- **Trust is Earned**: Reliability is a reputation score, not a configuration setting.
- **Privacy is Physical**: Secrets are kept in "stashes" scattered among friends, not in a central vault.

## Terminology Guide

Language shapes thought. Use the canonical Nara terms to reinforce the mental model.

| Concept | **Use This (Canonical)** | **Avoid This (Boring/Enterprise)** | **Why?** |
|:---|:---|:---|:---|
| **Identity** | **Soul** | Node ID, Public Key, GUID | A soul is immutable and defines who you are. |
| **Agent** | **Nara** | Node, Peer, Client, Server | "Nara" implies personhood and agency. |
| **Data Packet** | **Zine** | Bundle, Batch, Payload | A Zine is curated, editorialized gossip passed hand-to-hand. |
| **Broadcast** | **Plaza** | PubSub, MQTT Topic | The Plaza is a noisy public square where everyone shouts. |
| **Direct Link** | **Mesh** | P2P, Tunnel, VPN | The Mesh is for intimate, whispered conversations. |
| **State** | **Opinion** | Status, Fact, Record | "Opinion" acknowledges subjectivity and potential error. |
| **Uptime** | **Presence** | Availability, Liveness | "Presence" is social; "Uptime" is service-level. |
| **Storage** | **Stash** | Database, Persistence, S3 | A Stash is hidden and precious; a Database is a filing cabinet. |

## Cultural Do's and Don'ts

### Writing Tone
- **DO** write with a touch of whimsy and mystery.
- **DO** treat Naras as characters. *"Nara X decided to drop the connection because they were bored."*
- **DON'T** use dry, corporate, enterprise software language. No "SLAs," "mission-critical," or "enterprise-grade."
- **DON'T** apologize for the system's quirks. If it loses data, it "forgot." If it's slow, it's "pensive."

### Metaphors
- **Embrace**: Flocking birds, gossip circles, campfires, passing notes in class, forgotten memories.
- **Avoid**: Master/Slave, Leader election (use "consensus"), Client/Server, Pipelines.

### Talking about Nara
- Nara is an **experiment**, not a product.
- It is **art**, not infrastructure.
- It is **fragile** by design. The fragility makes the survival meaningful.

## Design Principles

### 1. Social Dynamics as Physics
Social rules *are* the system rules.
- **Gossip**: Information spreads because Naras *want* to share it, not because a router pushed it.
- **Reputation**: Access to resources (like Stash) is determined by how "cool" (reliable) you are.
- **Subjectivity**: Two Naras can disagree on the state of the world, and both are right.

### 2. The "No Disk" Invariant
Nara has **no disk persistence**.
- If a Nara restarts, it wakes up with amnesia (Tabula Rasa).
- It must ask its neighbours: "Who am I? What happened?"
- This forces the network to be the database. If the network goes down, the history evaporates. This is a feature.

### 3. Hazy Memory
We do not strive for "Strong Consistency." We strive for "Eventual Vibes."
- Old events are pruned not just by time, but by *relevance* and *interest*.
- It is okay to lose some messages. It is okay to be wrong for a while.

## Implementation Aesthetics

### Code Feel
The code should feel like it describes behavior, not just data processing.
- Functions should sound like actions: `gossip.ShareZine()`, `presence.Announce()`, `soul.Reflect()`.
- Error handling should feel like social friction: `ErrBored`, `ErrNotInterested`, `ErrStranger`.

### Naming Conventions
Variable and type names should reinforce the narrative.
- Use `mood`, `karma`, `chattiness` for configuration.
- Use `neighbourhood` for peer lists.
- Use `confidant` for storage partners.

### Emergent Behavior
Design features that allow complex behaviors to emerge from simple rules.
- Don't hardcode "leader election." Instead, let Naras vote on who has the highest `clout`, and naturally defer to them.
- Don't hardcode "sharding." Let Naras choose `confidants` based on latency and trust, forming natural clusters.
