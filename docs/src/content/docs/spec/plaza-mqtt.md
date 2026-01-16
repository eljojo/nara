---
title: Plaza (MQTT)
description: Public broadcast and real-time presence via MQTT.
---

The Plaza is nara's public square—a global broadcast channel where naras announce their presence, share status updates, and discover peers. It uses MQTT as the transport for real-time, low-overhead communication.

## 1. Purpose
- Provide a discovery mechanism for new naras.
- Facilitate real-time status heartbeats (Newspapers).
- Enable network-wide broadcasts for presence and consensus.
- Serve as a fallback when mesh connectivity is unavailable.

## 2. Conceptual Model
- **Broker**: A central MQTT broker (e.g., `mosquitto`) that facilitates message passing.
- **Topics**: A hierarchical namespace for filtering messages.
- **Newspaper**: A periodic status update broadcast by a nara to tell the network about its current state.
- **Plaza Event**: Any event broadcast over MQTT, typically using QoS 0 (best-effort).

### Invariants
1. **Public by Default**: Everything on the plaza is visible to anyone connected to the broker.
2. **Mandatory Signatures**: To prevent spoofing, all meaningful plaza messages MUST be signed by the sender's soul.
3. **No Central State**: The broker does not store history (except for retained messages if specifically configured).

## 3. External Behavior
- naras connect to the configured broker at startup.
- They subscribe to `nara/#` to hear about everyone else.
- They broadcast a `hey-there` event immediately upon connection.
- They periodically broadcast their `Newspaper` status.

## 4. Interfaces

### MQTT Topics
- `nara/plaza/hey-there`: Presence announcements.
- `nara/plaza/chau`: Graceful exit announcements.
- `nara/plaza/newspaper/{name}`: Periodic status updates.
- `nara/plaza/stash_refresh`: Requests for stash recovery.
- `nara/checkpoint/{subject}/{op}`: Checkpoint proposals and votes.

### Config
- `MQTT_HOST`: Broker address.
- `MQTT_PORT`: Broker port.

## 5. Event Types & Schemas
- `hey-there`: [Identity](/docs/spec/identity/) and discovery.
- `chau`: Graceful shutdown.
- `Newspaper`: Status payload containing [Personality](/docs/spec/personality/), [Observations](/docs/spec/observations/), and [Clout](/docs/spec/clout/).

## 6. Algorithms

### Connection & Jitter
1. Connect to the broker using TLS if available.
2. Apply a random jitter delay (0-5s) before the first broadcast to prevent "thundering herd" issues.
3. Subscribe to the global topic prefix.
4. Broadcast initial presence.

### Reconnect Loop
- On connection loss, naras attempt to reconnect with a randomized exponential backoff (typically 5-35s).

## 7. Failure Modes
- **Broker Outage**: If the plaza broker is down, real-time heartbeats are lost. naras must rely on [Zines](/docs/spec/zines/) and mesh-based discovery.
- **Message Loss**: MQTT QoS 0 is used for efficiency; naras must be resilient to occasional missing status updates.

## 8. Security / Trust Model
- **Transport**: Secured via TLS at the broker level.
- **Authentication**: self-authenticating messages. Every subscriber MUST verify the signature of plaza events against the sender's public key.
- **Privacy**: Plaza messages are public. sensitive data MUST NOT be broadcast on the plaza (use [Mesh HTTP](/docs/spec/mesh-http/) or [Stash](/docs/spec/stash/)).

## 9. Test Oracle
- `TestMQTT_ConnectJitter`: Verifies that connection delays are applied correctly.
- `TestMQTT_SignedSyncEvents`: Ensures that subscribers correctly reject unsigned or invalidly signed plaza events.
- `TestMQTT_NewspaperSignature`: Validates the integrity of the periodic status broadcast.

## 10. Open Questions / TODO
- Implement MQTT authentication (username/password) for private plaza instances.
- Transition `newspaper` broadcasts to the new `runtime` message model.
