---
title: Plaza MQTT
---

# Plaza MQTT

The public broadcast layer for real-time presence, lightweight gossip, and checkpoint consensus. Delivery is best-effort (QoS 0) by design.

## Conceptual Model

| Concept | Rule |
| :--- | :--- |
| **Broadcast** | One-to-many delivery via topic subscription. |
| **Unreliable** | Best-effort (QoS 0). Messages are optional; sync fills gaps. |
| **Payloads** | Most messages are signed `SyncEvent` objects. |
| **Discovery** | Primary bootstrap mechanism for non-mesh nodes. |

## Topics Reference

| Topic Group | Payloads | Purpose |
| :--- | :--- | :--- |
| **`nara/plaza/hey_there`** | `SyncEvent` | Arrival announcements. |
| **`nara/plaza/chau`** | `SyncEvent` | Graceful departure. |
| **`nara/plaza/howdy`** | `HowdyEvent` | Discovery responses. |
| **`nara/newspaper/{name}`**| `NewspaperEvent`| Periodic status heartbeats. |
| **`nara/plaza/social`** | `SyncEvent` | Public social interactions. |
| **`nara/checkpoint/*`** | Proposals/Votes | Consensus coordination. |
| **`nara/plaza/stash_refresh`**| Metadata | Requesting stash push from confidants. |

## Algorithms

### 1. Connection Lifecycle
1. Connect to broker (Default: `tls://mqtt.nara.network:8883`).
2. Apply 0-5s random jitter.
3. Subscribe to all plaza topics.
4. Emit initial `hey_there`.

### 2. Rate Limiting
- `hey_there` is restricted to once every 5 seconds.
- Heartbeats (`newspaper`) follow the node's configured chattiness/refresh rate.

## Legacy Compatibility
- `nara/ledger/{name}/*`: Legacy social-only sync protocol. Deprecated in favor of Mesh HTTP.

## Security
- **Authentication**: Payloads are self-authenticating via Ed25519 signatures.
- **Encryption**: Transport layer security (TLS) is required for the public broker.
- **Integrity**: Every `SyncEvent` and `NewspaperEvent` is signed by the emitter's soul.

## Test Oracle
- **Signatures**: Verify `hey_there` and `howdy` authenticity. (`identity_crypto_test.go`)
- **Messaging**: Verify topic publication and subscription handling. (`presence_howdy_test.go`)
- **Checkpoints**: Verify multi-step consensus traffic. (`checkpoint_test.go`)
