---
title: Runtime & Test Helpers
description: The core operating system of nara and the tools for verifying service behavior.
---

The runtime is the foundational layer of nara. It acts as an Operating System for distributed agents, providing identity, storage, transport, and security primitives to modular services. It enables **Nara-as-a-Platform**, where applications run as services consuming shared primitives.

## 1. Purpose
- Provide an orchestrator for services (`Init`/`Start`/`Stop`), so adding new capabilities is a matter of declaring behaviors.
- Define `Message` as the universal primitive for all communication (facts, requests, responses, and internal messages).
- Decouple business logic (Services) from protocol details (Pipelines) and transport details.
- Enable high-fidelity testing through deterministic simulation and mockable interfaces.

## 2. Conceptual Model
- **Nara OS**: The runtime instance providing shared resources (Identity, Keypair, Ledger, Transport).
- **Service (App)**: A modular unit of logic that implements the `Service` interface. Services are the "citizens" or "guests" of the runtime.
- **Message**: The universal envelope with `ID`/`ContentKey`, `Kind`/`Version`, identity, timestamp, payload, and signature.
- **Test Fidelity**: Because the runtime abstracts transport and signing, tests can swap a "Real Runtime" for a "Mock Runtime" without changing service code.

### Invariants
1. Services MUST NOT interact with the disk or network directly.
2. The runtime is the sole authority for generating message IDs and signatures.
3. Every emitted message MUST pass through the emit pipeline before any transport call.
4. Test helpers MUST support automatic cleanup of goroutines and state.

## 3. External Behavior
- **Orchestration**: The runtime calls `Init`, `Start`, and `Stop` on all registered services.
- **Message Handling**: Services call `Runtime.Emit(msg)`. The runtime populates defaults, computes `ID`, and signs before execution.
- **Self-Encryption**: The runtime provides `Seal`/`Open` primitives for XChaCha20-Poly1305 encryption using the nara's local keypair.
- **Identity Resolution**: The runtime resolves Nara IDs to public keys and mesh addresses.

## 4. Interfaces

### Message Structure
- `ID`: Unique 16-char base58 string.
- `ContentKey`: Semantic key for cross-observer deduplication.
- `Kind`: Fully qualified type (e.g., `stash:store`).
- `Version`: Schema version.
- `FromID` / `From`: Sender identity.
- `ToID` / `To`: Optional direct target.
- `Payload`: Kind-specific data (Go struct).
- `Signature`: Ed25519 signature over `SignableContent`.
- `InReplyTo`: Reference to a previous message `ID`.

### RuntimeInterface
The primary interface used by all services.
- `Me() *Nara` / `MeID() types.NaraID`: Returns the local nara's identity.
- `Emit(msg *Message) error`: Sends a message into the emit pipeline.
- `Receive(raw []byte) error`: Processes an incoming raw message.
- `Log(service) *ServiceLog`: Returns a structured logger scoped to the service.
- `Seal(plaintext) / Open(nonce, ciphertext)`: Symmetrically encrypt/decrypt data using the nara's seed.
- `LookupPublicKey(id types.NaraID) []byte`: Resolves a Nara ID to its Ed25519 public key.
- `Env() Environment`: Returns `Production`, `Development`, or `Test` mode.

### MockRuntime (Test Helper)
A specialized implementation of `RuntimeInterface` for unit testing.
- `EmittedMessages`: A slice of all messages emitted by the service during the test.
- `Deliver(msg)`: Simulates receiving a message from the network, triggering the receive pipeline and handlers.
- `RegisterPublicKey(id, key)`: Seeds the mock identity registry for verification tests.

## 5. Algorithms

### Runtime Initialization
1. Load identity (Soul) from hardware or configuration.
2. Initialize the `SyncLedger` and `TransportAdapters`.
3. Register services via `AddService`.
4. Run `Init` on all services to provide the `RuntimeInterface` reference.
5. Run `Start` on all services to begin background processing.

### Testing a Service (The Oracle Pattern)
1. Initialize `rt := runtime.NewMockRuntime(t)`.
2. Instantiate the service and call `svc.Init(rt)`.
3. Call a service method that should emit a message.
4. **Oracle**: Assert that `len(rt.EmittedMessages)` is correct and the payload matches expectations.
5. Send a message to the service via `rt.Receive(rawBytes)`.
6. **Oracle**: Assert that the service's internal state (or its next emission) reflects the received message.

## 6. Failure Modes
- **Leaked Goroutines**: Services that do not respect the `Stop` signal or context cancellation will cause test failures.
- **Identity Mismatch**: If the runtime cannot resolve a public key for a `FromID`, the message is dropped.
- **Schema Mismatch**: Messages with unsupported versions or unregistered kinds return errors during `Receive`.

## 7. Security / Trust Model
- **Isolation**: Services are logically isolated within the runtime, communicating only via messages.
- **Self-Encryption**: `Seal`/`Open` uses XChaCha20-Poly1305. The symmetric key is derived from the nara's seed.

## 8. Test Oracle
- `runtime.NewMockRuntime` MUST capture every call to `Emit`.
- Messages delivered via `rt.Receive` MUST pass through the same pipeline logic (Verify, Dedupe) as real network messages.
- `rt.Seal` followed by `rt.Open` MUST return the original plaintext.

## 9. Open Questions / TODO
- **WASM Isolation**: Moving beyond Go-level isolation to true WASM sandboxing for services.
- **Resource Quotas**: Enforcing CPU/Memory limits per service.
