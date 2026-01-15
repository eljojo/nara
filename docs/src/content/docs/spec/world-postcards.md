# World Postcards

## Purpose

World postcards are messages that travel through the network as signed hops.
They provide a shared ritual of participation and a source of social observations.

## Conceptual Model

Entities:
- **`WorldMessage`**: A journey object containing an ID, original message, originator, and hop list.
- **`WorldHop`**: A signed hop entry `{nara, timestamp, signature, stamp}`.
- **`WorldJourneyHandler`**: Logic for routing, verification, and forwarding.

Key invariants:
- **Chain Integrity**: Each hop signs the entire journey state up to that point.
- **Completion**: A journey is complete only when it returns to the originator.
- **Uniqueness**: A nara is visited at most once per journey (no repeat hops).

## External Behavior

- **Start**: Originator creates a message and sends it to the first hop (selected by clout).
- **Relay**: Each recipient verifies the chain, appends a signed hop, and forwards to the next candidate.
- **Completion**: When it returns to the originator, a `journey-complete` event is emitted.
- **Events**: Journeys emit `social` observation events (pass, complete) as they travel.

## Interfaces

### HTTP Endpoints (Mesh)
- `POST /world/relay`: Receives and forwards a `WorldMessage`.
- `GET /world/journeys`: Returns recent completed journeys (UI).

### MQTT Topics
- `nara/plaza/journey_complete`: Broadcasts the final completed journey object.

### Data Structures

**`WorldMessage`** (JSON):
```json
{
  "id": "base64-id",
  "message": "Hello World",
  "originator": "nara-name",
  "hops": [
    {
      "nara": "hop-1",
      "timestamp": 1700000000,
      "signature": "base64-sig",
      "stamp": "🌟"
    },
    ...
  ]
}
```

## Event Types & Schemas

Journeys emit `SyncEvent` with `svc: social`, `type: observation`.
- `reason="journey-pass"`: Emitted by a node when it relays a message.
- `reason="journey-complete"`: Emitted by the originator upon return.
- `reason="journey-timeout"`: Emitted if a tracked journey fails to complete.

The `WorldMessage.ID` is stored in `SocialEventPayload.Witness`.

## Algorithms

### Journey ID
`ID = Base64(SHA256(originator + message + timestamp))[0..16]`

### Hop Signing
Each hop signs a canonical JSON representation of the current state:
```go
SHA256(JSON({
  "id": ...,
  "message": ...,
  "originator": ...,
  "previous_hops": [ ... ], // All prior hops
  "current_nara": "me"
}))
```
This ensures the history cannot be altered.

### Routing (`ChooseNextNara`)
1. **Candidates**: Online naras (mesh-enabled), excluding self, originator, and already visited.
2. **Scoring**: Sort by local Clout score (descending).
3. **Selection**: Pick the highest-clout candidate.
4. **Return**: If no candidates remain, return to originator (if online).
5. **Stuck**: If no candidates and originator unreachable, journey halts.

### Handler Logic
1. **Verify**: Check all signatures in `Hops`.
2. **Sign**: Append new `WorldHop` with signature and a deterministic emoji stamp.
3. **Check Completion**: If `LastHop == Originator`, trigger completion logic.
4. **Forward**: Select next hop and send via mesh HTTP.

## Failure Modes

- **Signature Failure**: If any hop signature is invalid, the journey is dropped.
- **Routing Failure**: If no next hop is found, the journey dies.
- **Timeout**: Pending journeys are cleaned up after 5 minutes if not completed.

## Security / Trust Model

- **Chain of Trust**: Each hop verifies the entire history before adding their signature.
- **Public Keys**: Verification relies on locally known public keys (from `hey_there` or directory).

## Test Oracle

- **Signing**: `VerifyChain` succeeds for valid journeys, fails for tampered ones. (`world_test.go`)
- **Routing**: `ChooseNextNara` respects constraints (no repeats, return to originator last). (`world_integration_test.go`)
- **Completion**: Originator detects return and fires completion event. (`world_integration_test.go`)

## Open Questions / TODO

- **Retries**: Currently no retry mechanism if a specific hop fails (HTTP error).
