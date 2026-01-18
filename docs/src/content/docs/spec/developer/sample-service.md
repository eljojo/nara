---
title: Sample Service (Stash)
description: A deep dive into the reference implementation of a nara runtime service.
---

The `stash` service is the primary reference for how to build applications on the nara runtime. It demonstrates behavior registration, local message passing, and use of OS primitives.

## 1. Purpose
- Provide distributed, encrypted storage for nara state.
- Showcase a fully decoupled, message-driven service architecture.

## 2. Conceptual Model
- **The Stash**: An encrypted blob of data stored on a peer (Confidant).
- **The Recovery**: A boot-time sequence that uses broadcast and P2P messages to rebuild local state.

## 3. Service Logic & Implementation

### Initialization
```go
func (s *StashService) Init(rt RuntimeInterface) error {
    s.rt = rt
    s.log = rt.Log("stash")
    s.stored = make(map[types.NaraID]*EncryptedStash)
    return nil
}
```

### Defining Behaviors
The service implements `BehaviorRegistrar` to declare its protocol:
```go
func (s *StashService) RegisterBehaviors(rt RuntimeInterface) {
    // 1. External Protocol
    rt.Register(MeshRequest("stash:store", "Store data").
        WithPayload[StashStorePayload]().
        WithHandler(1, s.handleStoreV1))

    // 2. Local Notification
    rt.Register(Local("stash:recovered", "Recovery complete").
        WithPayload[StashRecoveredPayload]())
}
```

### Processing Logic
Handlers focus purely on the "Business Logic". Verification and transport are already handled by the runtime pipelines.
```go
func (s *StashService) handleStoreV1(msg *Message, p *StashStorePayload) {
    s.log.Info("storing stash", "from", msg.FromID)
    s.stored[msg.FromID] = &EncryptedStash{
        Data: p.Ciphertext,
        Nonce: p.Nonce,
    }
}
```

## 4. Local Message Passing
Stash uses `Local` messages to notify other services (like `presence`) when its state is back.
```go
func (s *StashService) finishRecovery() {
    s.rt.Emit(&Message{
        Kind: "stash:recovered",
        Payload: &StashRecoveredPayload{Count: len(s.stored)},
    })
}
```
**Why?** This decouples the stash service from whatever services might need its data. Other services just register a handler for `stash:recovered`.

## 5. Using OS Primitives

### Scoped Logging
Instead of global loggers, stash uses `s.log.Info`. This ensures all logs are automatically tagged with `service=stash` and the nara's identity.

### Self-Encryption
Stash uses `rt.Seal()` and `rt.Open()`.
- The runtime handles the HKDF key derivation from the Nara's soul.
- The service only sees `plaintext` and `ciphertext`.
- This ensures that if the service code is ever reused, it doesn't need to know how Nara manages keys.

## 6. Testing the Service
Stash tests use the `MockRuntime` to verify the protocol without needing a real network or real crypto.
```go
func TestStore(t *testing.T) {
    rt := runtime.NewMockRuntime(t)
    svc := NewStashService()
    svc.Init(rt)

    // Simulate an incoming store request
    rt.Deliver(syntheticStoreMsg)

    // Check if the service actually saved it
    assert.NotNil(t, svc.stored[senderID])
}
```

## 7. Failure Modes
- **Encryption Failure**: If `rt.Open` fails during recovery, the service logs an error and emits a `stash:recovered` message with `Count: 0`.
- **Corrupt Stash**: If a peer returns a malformed response, the `Receive` pipeline drops it, and the handler is never triggered.

## 8. Test Oracle
- A `stash:store` message MUST result in a record in the `s.stored` map.
- Emitting a `stash:recovered` message MUST trigger any other service that registered a local handler for that kind.
- Decrypting a stash with a different Nara ID MUST fail.
