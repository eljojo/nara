---
title: Presence
description: Discovery, liveness, and identity announcements in the nara network.
---

Presence is how naras discover each other, prove they are alive, and maintain a shared view of who is currently participating in the network.

## 1. Purpose
- Enable autonomous discovery of peers without a central registry.
- Provide a mechanism for naras to announce their cryptographic identity (public keys).
- Track liveness and distinguish between graceful exits and unexpected downtime.

## 2. Conceptual Model
- **Presence**: The state of being reachable and active in the network.
- **Heartbeat**: Periodic signals (Newspapers or Pings) that prove a nara is still online.
- **The neighbourhood**: A local map of all peers a nara is currently aware of.
- **Status States**: `ONLINE`, `OFFLINE` (graceful), and `MISSING` (timed out).

### Invariants
1. **Event-Driven**: All presence state MUST be derivable from events in the ledger.
2. **Self-Announcement**: naras are responsible for announcing their own arrival (`hey-there`) and departure (`chau`).
3. **Consensus**: Liveness is not a single fact but a projection based on the most recent activity seen by multiple observers.

## 3. External Behavior
- Upon startup, a nara broadcasts its identity and metadata on the plaza.
- It periodically shares a "Newspaper" status update containing its current opinions and clout.
- If a nara does not hear from a peer for a long period, it marks them as `MISSING`.
- A gracefully departing nara broadcasts a `chau` event to inform others it is going offline intentionally.

## 4. Interfaces
- `hey-there`: Event carrying name, public key, and mesh IP.
- `chau`: Event carrying name and a "goodbye" signal.
- `Newspaper`: Status payload broadcast on the plaza.
- `OnlineStatusProjection`: The read-model that computes the current status of all peers.

## 5. Event Types & Schemas
### `hey-there` (SyncEvent)
- `From`: Nara name.
- `PublicKey`: Ed25519 public key.
- `MeshIP`: Stable mesh network address.
- `ID`: Nara ID (soul/name bond hash).

### `chau` (SyncEvent)
- `From`: Nara name.
- `ID`: Nara ID.

## 6. Algorithms

### Online Status Projection
The status of a peer is determined by the "most recent event wins" rule:
- **ONLINE**: Triggered by `hey-there`, `Seen`, `Ping`, `Social`, or any observation.
- **OFFLINE**: Triggered by a `chau` event.
- **MISSING**: Triggered when a nara is currently marked `ONLINE` but its `LastSeen` timestamp is older than a threshold:
    - **Plaza/MQTT**: 5 minutes.
    - **Gossip/Mesh**: 1 hour.

### Discovery Jitter
To prevent network spikes, naras apply a random delay (0-5s) before their initial `hey-there` announcement and subsequent status updates.

### Coordination (Howdy)
When many naras discover a new peer simultaneously, they use a "Howdy" coordination mechanism:
1. naras wait for a jittered delay.
2. They check if others have already welcomed the newcomer.
3. Only a limited number of "welcoming" events are sent to avoid flooding.

## 7. Failure Modes
- **Identity Spoofing**: Prevented by requiring all presence events to be signed and verified against the claimed public key.
- **Ghosting**: If a nara's mesh connection is broken but its plaza connection is fine, it may appear `ONLINE` but be unreachable for gossip.
- **Clock Drift**: Differences in local clocks can lead to minor discrepancies in "Last Seen" timeouts across different observers.

## 8. Security / Trust Model
- **TOFU (Trust On First Use)**: naras typically trust the first public key they see for a name. Subsequent changes to a public key for the same name are rejected unless signed by the original key.
- **Proof of Work/Stake**: Not used; presence is lightweight and based on cryptographic identity bonds.

## 9. Test Oracle
- `TestHeyThereBootstrap`: Verifies that a `hey-there` event correctly populates the neighbourhood with identity and mesh data.
- `TestOnlineStatusProjection`: Validates the transition between `ONLINE`, `OFFLINE`, and `MISSING` based on event streams.
- `TestChauEvent`: Ensures that graceful departures are correctly recognized and projected.
- `TestHowdyCoordination`: Confirms that multiple naras don't flood a newcomer with duplicate welcome messages.

## 10. Open Questions / TODO
- Implement "Presence Attestations" where peers vouch for each other's liveness.
- Add support for "Stealth Mode" where a nara listens but does not announce its own presence.
