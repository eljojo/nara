# World Postcards

## Purpose

World postcards are messages that travel through the network as signed hops.
They provide a shared ritual of participation and a source of social observations.

## Conceptual Model

Entities:
- WorldMessage: a journey with an ID, original message, originator, and hop list.
- WorldHop: a signed hop entry (nara, timestamp, signature, stamp).
- WorldJourneyHandler: handles routing, verification, and forwarding.
- PendingJourney: a locally tracked journey awaiting completion confirmation.

Key invariants:
- Each hop signs the entire journey state up to that hop.
- A journey is complete only when it returns to the originator.
- A nara is visited at most once per journey (no repeat hops).

## External Behavior

- Starting a journey selects a next hop and sends it over the mesh.
- Each recipient verifies the hop chain, adds its own signed hop, then forwards.
- Journeys produce observation social events (pass, complete, timeout).
- Completion is broadcast via MQTT to inform participants who did not receive the final hop.

## Interfaces

HTTP endpoints:
- `POST /world/start`: starts a new journey (body includes `message`).
- `POST /world/relay`: receives and forwards a world message.
- `GET /world/journeys`: returns up to the last 20 completed journeys.

MQTT topics:
- `nara/plaza/journey_complete`: notifies completion (JourneyCompletion payload).

Public structs:
- `WorldMessage`, `WorldHop`, `WorldJourneyHandler`, `JourneyCompletion`.

## Event Types & Schemas (if relevant)

World journeys emit social observation events (`svc="social"`, `type="observation"`):
- `reason="journey-pass"`: recorded when a journey passes through.
- `reason="journey-complete"`: recorded by originator and listeners on completion.
- `reason="journey-timeout"`: recorded when a pending journey times out.

The journey ID is stored in `SocialEventPayload.Witness`.

## Algorithms

Journey ID:
- `ID = Base64(sha256(originator + message + timestamp))` over 16 bytes.

Hop signing:
- Each hop signs `sha256(json({id, message, originator, previous_hops, current_nara}))`.
- `previous_hops` includes all hops before the current hop.

Routing (`ChooseNextNara`):
- Candidate set: online naras, not self, not originator, not already visited.
- Score by clout (higher is better); tie order is arbitrary.
- If no candidates remain, return to originator if they are online and we are not them.
- If originator is not available, the journey is stuck.

Handler flow:
1. Verify existing hop chain.
2. Add own hop with a deterministic stamp.
3. If complete, call `onComplete` and stop.
4. Otherwise, call `onJourneyPass` and forward to the next candidate.

Timeouts:
- Pending journeys time out after 5 minutes.
- Timeout check runs every 30 seconds.

## Failure Modes

- Invalid signature or unknown public key rejects the journey.
- If no next hop exists, the journey stops with an error.
- If mesh is not configured, journeys fall back to a local mock mesh.

## Security / Trust Model (if relevant)

- Each hop is individually signed and verified by public keys in the neighbourhood.
- Completion via MQTT is not cryptographically verified beyond the included hop chain.

## Test Oracle

- Hop chain signing and verification succeed for a complete journey. (`world_test.go`)
- Routing respects clout ordering and completion conditions. (`world_integration_test.go`)

## Open Questions / TODO

- None.
