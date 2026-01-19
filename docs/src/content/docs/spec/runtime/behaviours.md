---
title: Behaviors & Patterns
description: How to define the character and processing rules for different message kinds.
---

Behaviors are the declarative configuration for message kinds. They define the "physics" of a message—how it's signed, where it's stored, and who handles it.

## 1. Purpose
- Move protocol complexity out of code and into configuration.
- Standardize common message patterns (e.g., "ephemeral broadcast" vs "persistent fact").
- Enable version-safe schema evolution through explicit mapping.

## 2. Conceptual Model
- **The Kind**: A unique string key (e.g., `observation:restart`).
- **The Behavior**: A struct containing the pipelines (Emit/Receive) and handlers for that Kind.
- **The Registry**: A central runtime component where all behaviors are registered.

### Invariants
1. Every message emitted MUST have a corresponding registered behavior.
2. A behavior MUST define a `PayloadType` to enable JSON deserialization.
3. Handlers in a behavior MUST be version-specific.

## 3. Interfaces

### Behavior Struct
```go
type Behavior struct {
    Kind           string
    CurrentVersion int
    MinVersion     int
    PayloadTypes   map[int]reflect.Type
    Handlers       map[int]any
    Emit           EmitBehavior
    Receive        ReceiveBehavior
}
```

### Emit & Receive Configuration
- `EmitBehavior`: Configures `Sign`, `Store`, `Gossip`, `Transport`, and `OnError`.
- `ReceiveBehavior`: Configures `Verify`, `Dedupe`, `RateLimit`, `Filter`, `Store`, and `OnError`.

### ErrorStrategy
Defines what happens when a pipeline stage fails:
- `ErrorDrop`: Silent drop.
- `ErrorLog`: Log warning and drop.
- `ErrorRetry`: Retry with backoff.
- `ErrorQueue`: Send to dead letter queue.
- `ErrorPanic`: Fail loudly (critical only).

### Pattern Templates (The DSL)
The runtime provides helper functions to reduce boilerplate:
- `Ephemeral()`: MQTT only, not stored, not signed.
- `StoredEvent()`: Stored in ledger, gossiped hand-to-hand.
- `MeshRequest()`: Direct P2P (HTTP), signed, not broadcast.
- `Local()`: Internal service-to-service communication only.

## 4. Examples & Showcases

### Example 1: Ephemeral Ping
A ping is a casual message that shouldn't be saved or gossiped.
```go
rt.Register(Ephemeral("ping", "Latency check", "nara/plaza/ping").
    WithPayload[PingPayload]().
    WithHandler(1, s.handlePing))
```
**Effect**: Uses MQTT, skips signing, skips storage, and filters based on personality.

### Example 2: Persistent Observation
An observation is a critical fact that must be remembered and shared.
```go
rt.Register(StoredEvent("observation:restart", "Nara restart", 0).
    WithPayload[RestartPayload]().
    WithContentKey(restartKeyFunc).
    WithHandler(1, s.handleRestart))
```
**Effect**: Computes a semantic `ContentKey`, stores in ledger with priority 0 (never prune), and gossips to peers.

### Example 3: Private Mesh Request
A stash request is a direct P2P message that must be signed but never broadcast.
```go
rt.Register(MeshRequest("stash:request", "Get my data").
    WithPayload[StashRequestPayload]().
    WithHandler(1, s.handleRequest))
```
**Effect**: Forces direct HTTP mesh transport, requires a valid signature, and skips MQTT.

## 5. Versioning Algorithms
Behaviors manage schema changes using the `Version` field.

### Upgrade Path
1. Define a new payload struct (`PayloadV2`).
2. Add to behavior: `WithPayloadV2()`.
3. Add a new handler: `WithHandler(2, handleV2)`.
4. Update `CurrentVersion` to 2.
5. **Receive**: Runtime sees `version: 1`, picks `PayloadV1`, calls `handleV1`.
6. **Emit**: Runtime sets `version: 2`, picks `PayloadV2`, calls `handleV2`.

## 6. Failure Modes
- **Unknown Kind**: Receiving a message with a kind that hasn't been registered results in an immediate error.
- **Schema Mismatch**: Messages with unsupported versions result in errors during `Receive`.

## 7. Security / Trust Model
- Behaviors define the security baseline. If a behavior uses `DefaultVerify()`, no message will reach the service handler without a valid cryptographic signature from the `FromID`.

## 8. Test Oracle
- `rt.Lookup(kind)` MUST return the registered behavior.
- Messages delivered via `rt.Receive` MUST pass through the registered pipelines.
- A handler registered for version 2 MUST NOT be called for a version 1 message.

## 9. Open Questions / TODO
- **Dynamic Unregistration**: Allowing services to "uninstall" behaviors at runtime.
- **Schema Validation**: Using JSON Schema to validate payloads.
