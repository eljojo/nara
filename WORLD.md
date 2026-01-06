# Going Around the World

A message that travels from nara to nara, collecting signatures and stamps along the way, until it returns home.

## Overview

"Going Around the World" is a collaborative feature where a nara sends a message that hops through the mesh network, visiting other naras one by one based on social affinity (clout), until it completes a full circle back to the originator.

## How It Works

### 1. Starting the Journey

A nara initiates a world journey with a message:
```
"Hello from the other side!"
```

The originator creates a `WorldMessage` containing:
- The original message
- Their name as originator
- A unique journey ID
- An empty list of hops (to be filled as it travels)

### 2. Routing by Vibes

At each stop, the current nara chooses the next destination based on **clout** (social affinity):
- Look at all known online naras
- Exclude naras that have already been visited (check the hops list)
- Pick the one with the highest clout score
- If no unvisited naras remain, return to the originator

### 3. Signing and Stamping

When a nara receives the world message, they:
1. **Verify** all existing signatures in the chain
2. **Sign** the current state of the message with their Ed25519 private key
3. **Add a stamp** - an emoji as a token of appreciation and acknowledgement
4. **Record the timestamp** of their participation

Each hop entry contains:
```go
type WorldHop struct {
    Nara      string  // Name of the nara
    Timestamp int64   // When they received it
    Signature string  // Ed25519 signature (Base64)
    Stamp     string  // Emoji stamp
}
```

### 4. Verification

Every nara in the chain verifies:
- All previous signatures are valid
- The chain is unbroken (each signature covers all previous hops)
- No hop has been tampered with

If verification fails, the message is rejected.

### 5. Completion

When the message returns to the originator:
1. The originator verifies the complete chain
2. Posts the final `WorldMessage` to MQTT for everyone to see
3. Clout rewards are distributed

### 6. Rewards

Completing a world journey awards clout:
- **Originator**: +10 clout for successfully completing the journey
- **Each participant**: +2 clout for being part of the chain

## Message Format

```go
type WorldMessage struct {
    ID              string     `json:"id"`        // Unique journey identifier
    OriginalMessage string     `json:"message"`   // The message being carried
    Originator      string     `json:"originator"` // Who started the journey
    Hops            []WorldHop `json:"hops"`      // Chain of signatures and stamps
}

type WorldHop struct {
    Nara      string `json:"nara"`       // Nara name
    Timestamp int64  `json:"timestamp"`  // Unix timestamp
    Signature string `json:"signature"`  // Base64 Ed25519 signature
    Stamp     string `json:"stamp"`      // Emoji stamp
}
```

## Cryptographic Details

### Ed25519 Keypair

Each nara derives an Ed25519 keypair deterministically from their soul:
- The soul's 32-byte seed is used directly as the Ed25519 seed
- Same soul = same keypair (portable identity)
- Public keys are shared as part of `NaraStatus`

### What Gets Signed

Each nara signs:
```
SHA256(message_id + original_message + originator + serialized_previous_hops)
```

This ensures:
- The message hasn't been altered
- The hop chain is intact
- The order is preserved

## Transport

World messages travel over the **mesh network** (tsnet/Headscale), not MQTT:
- Direct nara-to-nara connections
- More reliable for chain-of-custody
- MQTT only used for the final broadcast

## Example Journey

```
1. Alice starts: "Hello world!"
   Hops: []

2. Alice -> Bob (highest clout)
   Hops: [{Bob, 1234567890, "sig...", "🌟"}]

3. Bob -> Carol (highest unvisited clout)
   Hops: [{Bob, ..., "🌟"}, {Carol, 1234567895, "sig...", "🎉"}]

4. Carol -> Dave
   Hops: [{Bob, ..., "🌟"}, {Carol, ..., "🎉"}, {Dave, 1234567900, "sig...", "🚀"}]

5. Dave -> Alice (only unvisited is originator = complete!)
   Final hop added, journey complete

6. Alice posts to MQTT:
   "World journey complete! 🌍"
   Shows full chain with all stamps and timing
```

## Failure Cases

- **Nara offline**: Skip and try next highest clout
- **Signature invalid**: Reject message, log warning
- **No path back**: Journey fails if all naras visited but can't reach originator
- **Timeout**: Journey expires after 24 hours

## Future Enhancements

- Minimum hop count requirement
- Theme journeys (message must relate to a topic)
- Journey racing (multiple concurrent journeys)
- Visual map of the journey path
