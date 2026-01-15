---
title: Plaza MQTT
description: Public broadcast and heartbeat layer for the Nara Network.
---

Plaza MQTT is the public square of the network, providing best-effort broadcast for presence, heartbeats, and real-time social events.

## 1. Purpose
- Low-latency discovery via `hey_there`.
- Periodic status heartbeats ([Newspapers](#newspaper-heartbeats)).
- Real-time social signaling (teasing, buzz).
- Coordination for [Checkpoint](/docs/spec/checkpoints/) consensus.

## 2. Conceptual Model
- **Pub/Sub**: Standard MQTT (default `tls://mqtt.nara.network:8883`).
- **Best-Effort**: QoS 0 only; no delivery guarantees.
- **Coordination**: Used for jittered responses (e.g., `howdy`) to avoid thundering herds.

### Invariants
- **Signed Payloads**: Every message must be signed with the sender's soul.
- **Jittered Join**: 0-5s delay before initial `hey_there`.
- **Hybrid-Resilient**: Falls back to [Mesh HTTP](/docs/spec/mesh-http/) gossip if MQTT is down.

## 3. Interfaces

### Topic Reference
| Topic | Purpose | Payload |
| :--- | :--- | :--- |
| `nara/plaza/hey_there` | Join announcement | `SyncEvent(hey-there)` |
| `nara/plaza/chau` | Graceful departure | `SyncEvent(chau)` |
| `nara/plaza/howdy` | Discovery response | `HowdyEvent` |
| `nara/plaza/social` | Social interactions | `SyncEvent(social)` |
| `nara/newspaper/{name}` | Status heartbeat | `NewspaperEvent` |
| `nara/checkpoint/*` | Checkpoint consensus | `Proposal`, `Vote`, `Final` |

### Newspaper Heartbeats
Published every 10-300s (per `Chattiness`).
- **Signature**: Covers raw JSON of the `Status` field.
- **Content**: [Identity](/docs/spec/identity/), [Personality](/docs/spec/personality/), [Observations](/docs/spec/observations/), and [Buzz](/docs/spec/social-events/#buzz-calculation).

## 4. Algorithms

### Connection & Jitter
1. TLS Connect.
2. Jitter: `rand(0..5)s`.
3. Subscribe to `nara/#`.
4. Broadcast `hey_there`.

### Reconnect Loop
Randomized backoff (5-35s) on connection loss.

## 5. Failure Modes
- **Broker Outage**: Loss of real-time heartbeats; fallback to [Zines](/docs/spec/zines/).
- **Spoofing**: Unprotected topics require **mandatory** signature verification by subscribers.
- **Message Loss**: State derivation must be resilient to dropped QoS 0 broadcasts.

## 6. Security
- **Transport**: TLS.
- **Application**: Ed25519 self-authentication.
- **Anonymity**: Identity tied to soul, not IP (from Nara perspective).

## 7. Test Oracle
- `TestMQTT_ConnectJitter`: Delay verification.
- `TestMQTT_SignedSyncEvents`: Subscriber verification check.
- `TestMQTT_NewspaperSignature`: Status payload integrity.
- `TestMQTT_ReconnectLoop`: Backoff behavior.
