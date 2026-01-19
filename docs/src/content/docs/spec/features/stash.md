---
title: Stash
description: Encrypted distributed storage service for nara.
---

Stash is the reference implementation of a nara runtime service. It provides encrypted, memory-only persistence by delegating storage to trusted "confidants". See the **[Stash Developer Guide](/docs/spec/developer/sample-service/)** for a deep-dive into the implementation.

## 1. Purpose
- Enable naras to survive restarts without local disk persistence by "stashing" their state on peers. See the **[Memory Model](/docs/spec/memory-model/)** for the "Hazy Memory" context.
- Provide a blueprint for runtime services using typed messages, Call() for request/response, and versioned handlers. See **[Behaviors & Patterns](/docs/spec/developer/behaviors/)**.
- Ensure that only the owner of a stash can ever decrypt it.

## 2. Conceptual Model
- **Owner**: The nara whose state is being stored.
- **Confidant**: A peer that holds an `EncryptedStash` for an owner.
- **EncryptedStash**: A record containing `OwnerID`, `Nonce`, `Ciphertext`, and a `StoredAt` timestamp.
- **Call**: Runtime primitive for request/response pairs (e.g., `store` -> `ack`). Services call `rt.Call(msg, timeout)` and receive responses via `InReplyTo` matching.
- **Encryptor**: Derived from the owner's seed; uses XChaCha20-Poly1305.

### Invariants
1. Confidants MUST NOT be able to read the plaintext of the stashes they hold.
2. Every stash operation (store/request) MUST be signed and verified via the runtime. See **[Identity](/docs/spec/identity/)**.
3. Storage is volatile (RAM-only). If all confidants of an owner restart, the stash is lost.
4. Symmetric keys MUST be derived deterministically from the owner's private seed.

## 3. External Behavior
- **Storage**: `StoreWith(confidantID, data)` encrypts data and waits for a `stash:ack`.
- **Retrieval**: `RequestFrom(confidantID)` fetches and decrypts the owner's stash.
- **Recovery**: `RecoverFromAny()` attempts retrieval from all configured confidants until success. See **[Boot Sequence](/docs/spec/boot-sequence/)**.
- **Confidant Duty**: When receiving a `stash:store`, a nara saves the blob in its local `stored` map.
- **Refresh**: Broadcasting `stash-refresh` via MQTT triggers confidants to send back their held stashes over mesh. See **[Plaza (MQTT)](/docs/spec/plaza-mqtt/)**.

## 4. Interfaces

### Service API
- `StoreWith(targetID, data) error`: Encrypt and send to specific peer.
- `RequestFrom(targetID) ([]byte, error)`: Request and decrypt from specific peer.
- `RecoverFromAny() ([]byte, error)`: Sequential recovery attempt.
- `SetConfidants([]ID)`: Configure the set of peers to use for stashing.
- `MarshalState()`: Returns the current `stored` map for debugging.

### Message Kinds
- `stash:store`: (MeshOnly) Carries encrypted blob to a confidant. Response: `stash:ack`.
- `stash:ack`: (MeshOnly) Confirmation of storage.
- `stash:request`: (MeshOnly) Retrieval request. Response: `stash:response`.
- `stash:response`: (MeshOnly) Carries the encrypted blob back to the owner.
- `stash-refresh`: (MQTT) Broadcast to `nara/plaza/stash_refresh`.

## 5. Event Types & Schemas

### `stash:store` (v1)
- `OwnerID`: Nara ID of the owner.
- `Nonce`: 24-byte random nonce.
- `Ciphertext`: XChaCha20-Poly1305 encrypted blob.
- `Timestamp`: When the stash was created.

### `stash:ack` (v1)
- `OwnerID`: Echoed owner ID.
- `Success`: Boolean indicating if stored.
- `StoredAt`: Confidant's local timestamp.

### `stash-refresh` (v1)
- `OwnerID`: ID of the nara looking for its stash.

## 6. Algorithms

### Encryption (HKDF + XChaCha20-Poly1305)
- **Key Derivation**: `HKDF-SHA256`
  - Salt: `nara:stash:v1`
  - Info: `symmetric`
  - Seed: 32-byte private seed (derived from Ed25519 key).
- **Encryption**: `Seal(plaintext)` generates a random 24-byte nonce and appends the Poly1305 tag to the ciphertext.

### Request/Response (Call)
- Services use `rt.Call(msg, timeout)` for request/response patterns.
- The runtime's `CallRegistry` tracks pending calls by `Message.ID`.
- The response MUST set `InReplyTo` to the request's `ID`.
- When a response arrives, the runtime matches `InReplyTo` and resolves the pending call.
- Default timeout: 30 seconds.

### Recovery Workflow
1. Emit `stash-refresh` on MQTT.
2. Confidants seeing their own ID in the refresh payload (or having a record for that `OwnerID`) emit `stash:response` via mesh.
3. Owner receives `stash:response`, matches via `InReplyTo` (or direct handling), and decrypts.

## 7. Failure Modes
- **Transport Error**: If the mesh target is unreachable, the Call times out.
- **Decryption Error**: If the owner's seed changes or ciphertext is corrupted, `Open` fails.
- **Missing Stash**: If a confidant doesn't have the requested record, it replies with `Found: false`.
- **Invalid Payload**: malformed store/request payloads result in failure acks or ignored messages.

## 8. Security / Trust Model
- **Confidentiality**: Guaranteed by AEAD (XChaCha20-Poly1305). Only the owner possesses the symmetric key.
- **Authenticity**: Every message is signed by the sender's nara identity. Confidants verify the owner's signature before accepting a `stash:store`.
- **Integrity**: Poly1305 protects against tampering of the encrypted blobs.

## 9. Test Oracle
- `TestStashStoreAndAck`: Verifies the full store -> ack flow with a mock runtime.
- `TestStashRequestAndResponse`: Verifies request -> response -> decryption.
- `TestStashEncryptionDecryption`: Validates that HKDF derivation and XChaCha20 round-trip correctly.
- `TestStashStateMarshaling`: Ensures `stored` stashes survive service state serialization.

## 10. Open Questions / TODO
- **Seed Derivation**: Currently uses a placeholder seed; must be linked to the Nara's soul (Ed25519 seed).
- **Auto-Selection**: Runtime should eventually provide "Best Confidants" based on uptime/memory heuristics.
