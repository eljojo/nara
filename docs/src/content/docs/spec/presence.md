---
title: Presence
description: Real-time liveness and discovery protocol in the Nara Network.
---

# Presence

Presence signals enable naras to discover each other, track liveness, and manage graceful departures. It uses a hybrid approach of direct broadcasts and gossip.

## Purpose
- Announce new nodes to the network (`hey_there`).
- Provide bootstrap information to joining nodes (`howdy`).
- Signal intentional departures to avoid "missing" timeouts (`chau`).
- Track derived online status based on any observed activity.

## Conceptual Model
- **`hey_there`**: A signed announcement sent on join (MQTT and Gossip). Includes identity (Public Key, ID) and connectivity (Mesh IP).
- **`howdy`**: A coordination response to `hey_there`. Peers help the newcomer by sharing what they know about the network and the newcomer's own history.
- **`chau`**: A signed announcement sent on graceful shutdown.
- **Derived Status**: Presence is not just about messages; any signed event (ping, social) proves a Nara is "ONLINE".
- **Thresholds**: If no events are seen for a Nara within a threshold (default 5m for Plaza, 1h for Gossip), they are marked "MISSING".

### Invariants
- **Evidence-Based**: Activity in the ledger is the source of truth for presence projections.
- **Graceful vs. Abrupt**: `chau` marks a node "OFFLINE"; silence marks it "MISSING".
- **Self-Selection**: Up to 10 peers respond with `howdy` using random delays to prevent thundering herds.

## External Behavior
- **Join**: New nodes broadcast `hey_there` and wait for `howdy` responses.
- **Bootstrap**: Nodes learn about neighbors through `howdy` before having to discover them manually.
- **Steady State**: Continuous activity (pings, heartbeats) maintains the "ONLINE" status.
- **Departure**: On shutdown, the node emits `chau` and local state marks it "OFFLINE".

## Interfaces

### HeyThereEvent (SyncEvent Payload)
- `From`: Nara name.
- `PublicKey`: Base64 Ed25519 public key.
- `MeshIP`: Tailscale/Mesh IP.
- `ID`: Stable Nara ID.
- `Signature`: Inner signature of identity fields (Legacy, typically verified via outer SyncEvent).

### ChauEvent (SyncEvent Payload)
- `From`: Nara name.
- `PublicKey`: Base64 Ed25519 public key.
- `ID`: Stable Nara ID.

### HowdyEvent (MQTT Only)
- `From`: Responder name.
- `To`: Newcomer name.
- `Seq`: Sequence number (1-10).
- `You`: `NaraObservation` of the newcomer (assists in StartTime recovery).
- `Neighbors`: Up to 10 known peers (Name, PublicKey, MeshIP, ID, Observation).
- `Me`: Responder's full `NaraStatus`.

## Algorithms

### 1. Discovery & Howdy Coordination
1. **Joiner** sends `hey_there`.
2. **Peers** start a `howdyCoordinator`:
   - Wait 0-3 seconds (random jitter).
   - Listen for other `howdy` messages for this joiner.
   - If `seen >= 10` before our timer fires, **abort**.
   - Otherwise, select neighbors and send `howdy`.

### 2. Neighbor Selection
Howdy responders pick up to 10 neighbors (5 in short-memory mode):
1. Prioritize **ONLINE** naras.
2. Sort by **least recently active** (oldest `LastSeen`) to help spread network knowledge.
3. Exclude the joiner and the responder themselves.

### 3. Online Status Projection
Derived status is computed using a "most recent event wins" rule:
- `hey_there` / `Social` / `Ping` / `Seen` / `Restart` → **ONLINE**.
- `chau` / `Observation(status-change: OFFLINE)` → **OFFLINE**.
- `Observation(status-change: MISSING)` → **MISSING**.
- **Timeout**: If current status is **ONLINE** but `now - LastEventTime > threshold`, status becomes **MISSING**.

## Failure Modes
- **Storms**: If jitter fails or many peers start simultaneously, `howdy` responses may spike.
- **Stale Chau**: A `chau` from an old session could mark a Nara offline. Projections protect against this by only accepting status changes newer than the current state.
- **Boot Race**: `chau` events are ignored during the initial boot phase to prevent backfilled history from clobbering live join events.

## Security / Trust Model
- **Identity Proof**: `hey_there` is the first time a Nara's public key is seen. Peers use TOFU (Trust On First Use) for the name-to-key binding.
- **Attestation**: `howdy` responses are signed by the responder.
- **Hearsay**: Neighbor info in `howdy` is considered unverified until the Nara is seen directly or via a signed event.

## Test Oracle
- `TestHowdyCoordination`: Verifies that no more than 10 responses are sent.
- `TestOnlineStatusProjection`: Ensures that pings/social events correctly mark nodes ONLINE and timeouts mark them MISSING.
- `TestChauEvent`: Confirms graceful shutdown correctly updates projection state to OFFLINE.
- `TestHeyThereBootstrap`: Checks that identity and mesh IP are correctly imported from join events.
