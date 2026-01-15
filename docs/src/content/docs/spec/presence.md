# Presence

## Purpose

Presence provides real-time liveness signals: who is here, who left, and who can vouch for whom.
It enables peer discovery, online status tracking, and graceful shutdowns.

## Conceptual Model

Presence is communicated through four primary mechanisms:
1. **`hey_there`**: "I just arrived" (with identity proof).
2. **`howdy`**: "Welcome! Here's what I know about you and others" (discovery & recovery).
3. **`chau`**: "I'm leaving now" (graceful shutdown).
4. **`newspaper`**: "I'm still here" (periodic status heartbeat).

Key invariants:
- **Signed Signals**: All presence messages are cryptographically signed.
- **Evidence-Based**: Presence events are evidence, not absolute truth. A `chau` doesn't mean offline if a newer `hey_there` exists.
- **Soft State**: Online status expires if not refreshed by events or pings.

## External Behavior

- **On Connect**: Naras broadcast `hey_there` via MQTT and create a SyncEvent for gossip.
- **On Discovery**: Peers respond to `hey_there` with `howdy` messages (up to 10 responses, randomized delay).
- **On Shutdown**: Naras broadcast `chau` via MQTT and create a SyncEvent for gossip.
- **Steady State**: Naras periodically broadcast `newspaper` updates (every 30-300s based on chattiness).

## Interfaces

### MQTT Topics
- `nara/plaza/hey_there` -> `SyncEvent` (`svc: hey-there`)
- `nara/plaza/howdy` -> `HowdyEvent` (Direct response, not synced)
- `nara/plaza/chau` -> `SyncEvent` (`svc: chau`)
- `nara/newspaper/{name}` -> `NewspaperEvent`

### HowdyEvent (MQTT Only)
Used for bootstrapping and neighbor discovery.
```json
{
  "From": "sender-name",
  "To": "target-name",
  "Seq": 1, // Response sequence number (1-10)
  "You": { ... }, // NaraObservation of the target
  "Neighbors": [
    {
      "Name": "neighbor-name",
      "PublicKey": "base64-key",
      "MeshIP": "100.x.y.z",
      "ID": "nara-id",
      "Observation": { ... }
    },
    ...
  ],
  "Me": { ... }, // Sender's NaraStatus
  "Signature": "base64-sig"
}
```
*Signature covers: `howdy:{from}:{to}:{seq}`*

### NewspaperEvent (MQTT Only)
Periodic status update.
```json
{
  "status": { ... }, // NaraStatus
  "signature": "base64-sig"
}
```
*Signature covers the raw JSON of `status`.*

## Event Types & Schemas

### HeyThere & Chau (SyncEvents)
Wrapped in `SyncEvent` for persistence and gossip propagation.
- **HeyThere**: `{From, PublicKey, MeshIP, ID}`
- **Chau**: `{From, PublicKey, ID}`
- *Note*: Both contain legacy inner signatures but rely on the outer `SyncEvent` signature for authentication in the modern protocol.

## Algorithms

### Hey-There Broadcasting
- Emitted on startup after connecting to MQTT.
- Rate-limited to once every 5 seconds to prevent storms.
- Jitter: 0-5s random delay before sending.

### Howdy Coordination (Discovery)
When a node sees `hey_there` from `Target`:
1. Start a random timer (0-3s).
2. Listen for other `howdy` responses to `Target`.
3. If `> 10` responses seen before timer fires, **abort** (silence).
4. Else, send `howdy` containing:
   - `You`: Observations about `Target` (helps them recover `StartTime`).
   - `Neighbors`: Up to 10 peers `Target` might not know (prioritizing online peers).
   - `Me`: Own status.

### Neighbor Selection Strategy
For `howdy` neighbor list:
1. Exclude `Target`, `Self`, and peers already mentioned in other `howdy`s for this session.
2. Filter for valid peers.
3. Sort:
   - **Online** peers first.
   - Then by **Least Recently Active** (helping `Target` discover stale/quiet peers).
4. Take top 10 (or 5 in low-memory mode).

### Chau Processing
- **Graceful Shutdown**: Emitted when the process receives SIGINT/SIGTERM.
- **Conflict Resolution**: A `chau` event is ignored if a `hey_there` or other activity exists with a later timestamp.
- **Boot Safety**: During boot recovery, `chau` events are ignored until projections are fully synced to prevent marking peers offline based on old data.

### StartTime Recovery
Naras forget their `StartTime` on restart. They recover it from `howdy` responses:
1. Collect `You.StartTime` votes from `howdy` messages.
2. Weigh votes by the sender's uptime (longer uptime = more trustworthy).
3. Apply consensus if votes agree within a 60s tolerance window.

## Failure Modes

- **Lost Howdys**: If all `howdy` responses are lost, the new node may be isolated until it receives a newspaper or zine.
- **Stale Chau**: A node crashing and restarting quickly might have its `hey_there` arrive before the `chau` from the previous run. Timestamps resolve this.
- **Split Brain**: If MQTT is down, `hey_there` propagates via gossip (slower), potentially delaying discovery.

## Security / Trust Model

- **Identity Bootstrap**: `hey_there` and `chau` contain the public key, acting as a self-signed certificate.
- **Observation Trust**: Neighbors reported in `howdy` are "hearsay" until directly verified (e.g., via ping or their own signed events).
- **TOFU**: Trust On First Use applies to public keys seen in `hey_there`.

## Test Oracle

- **Signature Verification**: `howdy`, `newspaper`, `hey_there`, and `chau` signatures must verify. (`identity_crypto_test.go`)
- **Howdy Limiting**: No more than 10 responses per `hey_there`. (`presence_howdy_test.go`)
- **Neighbor Selection**: `howdy` includes correct neighbors (online first). (`presence_howdy_test.go`)
- **Offline Transition**: `chau` marks sender offline, but new activity revives them. (`integration_events_test.go`)

## Open Questions / TODO

- **Replay Protection**: `howdy` and `newspaper` lack nonces or timestamps in their signature payloads, making them theoretically replayable (though impact is low as they just refresh status).
