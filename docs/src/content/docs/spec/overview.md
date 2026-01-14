# Overview

## Purpose

The Nara Network is a distributed system designed as a **creative medium**, not an optimization target.

It explores how computers can:
- gossip
- remember imperfectly
- disagree
- forget
- and form opinions

rather than maximizing correctness, durability, or performance.

Nara treats distributed-systems constraints (partial failure, clock skew, message loss) as **aesthetic features**, not problems to eliminate.

---

## Conceptual Model

- A **nara** is a long-running process with:
  - identity
  - memory
  - personality
- Naras form a **peer network**.
- No node is authoritative.
- No node has complete knowledge.
- Memory survives only if someone remembers.

Key invariants:
- No local persistence to disk.
- All durable knowledge is replicated socially.
- All shared facts are events.
- All state is derived.

---

## External Behavior

From the outside, the system appears as:
- a set of semi-independent agents
- reporting on each other’s presence
- participating in social interactions
- producing a shared but incomplete narrative

The system never guarantees:
- perfect accuracy
- complete history
- global agreement

Instead, it guarantees:
- authenticity (signed events)
- eventual convergence where possible
- explicit uncertainty where not

---

## Interfaces

At a high level, Nara exposes:
- a peer protocol (MQTT + HTTP)
- an HTTP API for observability
- a web UI that visualizes the network

No interface is considered authoritative; all are views over events.

---

## Failure Modes

Expected and acceptable:
- Naras forgetting history
- Conflicting opinions
- Divergent views of uptime
- Memory loss if all replicas disappear

Unacceptable:
- Silent data corruption
- Unsigned or unauthenticated events
- Hidden persistence

---

## Security / Trust Model

- Identity is cryptographic.
- Authenticity is verifiable.
- Truth is not absolute.
- Trust is emergent.

---

## Test Oracle

The system is correct if:
- Killing all naras loses all memory.
- Restarting a nara requires rebuilding state from peers.
- Two naras with the same events but different personalities disagree.

---

## Open Questions / TODO

- Explicit modeling of uncertainty decay.
- Long-term memory compaction strategies.
