---
title: Plaza MQTT
description: Public broadcast and heartbeat layer for the Nara Network.
---

# Plaza MQTT

Plaza MQTT is the public square of the Nara Network. It provides a broadcast-oriented transport for presence, heartbeats, and real-time social events. It is designed to be best-effort and unreliable, with the [Sync Protocol](./sync-protocol.md) filling the gaps.

## Purpose
- Provide a low-latency discovery mechanism for new naras.
- Broadcast periodic heartbeats ([Newspapers](#newspaper-heartbeats)) to keep the network alive.
- Facilitate real-time social interactions (teasing, buzz).
- Coordinate multi-party consensus for [Checkpoints](./checkpoints.md).

## Conceptual Model
- **Pub/Sub**: Nara uses a standard MQTT broker (default `tls://mqtt.nara.network:8883`) with anonymous but authenticated payloads.
- **Topics**: Organized under the `nara/` hierarchy.
- **Best-Effort**: All messages are sent with QoS 0; there are no delivery guarantees.
- **Self-Selection**: MQTT is used for coordination (like [Howdy](./presence.md#howdy-coordination)) to prevent network-wide thundering herds.

### Invariants
- **Signed Payloads**: Every meaningful MQTT message must be signed with the sender's Ed25519 soul.
- **Jittered Connection**: Naras wait 0-5s after connecting before sending their first `hey_there` to avoid broker overload.
- **Fallback-Safe**: If MQTT is down, the network continues to function via [Mesh HTTP](./mesh-http.md) gossip, albeit with higher discovery latency.

## External Behavior
- **Discovery**: When a Nara connects, it broadcasts `hey_there` on the plaza. Neighbors respond with `howdy` to help it boot.
- **Heartbeats**: Online naras periodically publish their full status to the `nara/newspaper/{name}` topic.
- **Social Real-time**: Teases and other social events are mirrored to MQTT for immediate UI feedback.

## Interfaces

### Topic Reference
| Topic | Purpose | Payload |
| :--- | :--- | :--- |
| `nara/plaza/hey_there` | Join announcement | `SyncEvent(hey-there)` |
| `nara/plaza/chau` | Graceful departure | `SyncEvent(chau)` |
| `nara/plaza/howdy` | Discovery response | `HowdyEvent` |
| `nara/plaza/social` | Social interactions | `SyncEvent(social)` |
| `nara/newspaper/{name}` | Status heartbeat | `NewspaperEvent` |
| `nara/checkpoint/*` | Checkpoint consensus | `Proposal`, `Vote`, `Final` |
| `nara/plaza/stash_refresh` | Requesting stash push | Metadata |

### Newspaper Heartbeats
Heartbeats are published every 10-300 seconds (determined by `Chattiness`).
- **Signature**: Covers the raw JSON of the `Status` field.
- **Content**: Includes [Identity](./identity.md), [Personality](./personality.md), [Observations](./observations.md), and [Buzz](./social-events.md#buzz-calculation).

## Algorithms

### 1. Connection & Jitter
1. Establish TLS connection to the MQTT broker.
2. Wait `rand(0..5)` seconds.
3. Subscribe to all relevant `nara/#` topics.
4. Broadcast initial `hey_there`.

### 2. MQTT Reconnect Loop
If the connection is lost, the Nara enters a randomized backoff loop (5-35 seconds) to attempt reconnection, preventing a "thundering herd" of naras from hitting the broker simultaneously.

## Failure Modes
- **Broker Downtime**: Naras lose real-time heartbeats but continue to exchange data via [Zines](./zines.md).
- **Topic Spoofing**: Since topics are not protected, any Nara can post to any topic. Verification of the signed payload is **mandatory** for all subscribers.
- **Missed Messages**: Due to QoS 0, messages may be lost. State derivation must not rely on receiving every MQTT broadcast.

## Security / Trust Model
- **Transport Security**: TLS is used for the connection to the public broker.
- **Application Security**: Every message is self-authenticating via Ed25519. Peers ignore any message with an invalid signature or an unknown `Nara ID`.
- **Anonymity**: The broker sees IP addresses, but Nara identity is tied only to the soul.

## Test Oracle
- `TestMQTT_ConnectJitter`: Verifies the 0-5s delay on join.
- `TestMQTT_SignedSyncEvents`: Ensures that SyncEvents sent over MQTT are correctly verified by subscribers.
- `TestMQTT_NewspaperSignature`: Checks that heartbeat signatures correctly cover the status payload.
- `TestMQTT_ReconnectLoop`: Validates the randomized backoff behavior when the broker is unavailable.
