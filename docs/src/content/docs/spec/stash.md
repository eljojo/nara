---
title: Stash
---

# Stash

## Purpose

Stash provides distributed, encrypted, memory-only persistence. It enables naras
to recover state after restarts by storing encrypted blobs with confidants.

## Conceptual Model

- Owners encrypt their stash and send it to confidants.
- Confidants store opaque ciphertext only.
- Recovery is owner-only: only the soul-derived key can decrypt.

Key invariants:
- **Encryption**: Stash payloads are encrypted with XChaCha20-Poly1305.
- **Privacy**: Confidants do not verify contents and cannot decrypt.
- **Volatility**: Storage is in-memory only (no disk persistence).

## External Behavior

- **Boot Recovery**: Naras attempt to recover stash from confidants on startup.
- **Push Recovery**: Confidants push stash to owners when they see `hey_there` (boot) or `stash_refresh`.
- **Maintenance**: Owners maintain a target number of confidants in the background, replacing offline ones.

## Interfaces

### Data Types
- **StashData**: `{Timestamp, Data (json), Version}`. The cleartext payload.
- **StashPayload**: `{Owner, Nonce, Ciphertext}`. The encrypted blob sent over the wire.

### Mesh HTTP Endpoints
Authenticated via mesh headers (`X-Nara-Mesh-Auth`).
- `POST /stash/store`: Store a stash payload. Returns `{accepted, reason}`.
- `DELETE /stash/store`: Delete a stored stash. Returns `{deleted}`.
- `POST /stash/retrieve`: Retrieve a stored stash. Returns `{found, stash}`.
- `POST /stash/push`: Push a stash to its owner (recovery). Returns `{accepted, reason}`.

### MQTT Topics
- `nara/plaza/stash_refresh`: `{from, timestamp}`. Requests confidants to push stash immediately.

## Event Types & Schemas

- **SyncEvents**: Stash does NOT emit SyncEvents for data transfer.
- **SocialEvents**: Emitted for observability (e.g., "stored stash for X").
  - Type: `service`
  - Reason: `stash_stored`

## Algorithms

### Encryption
1. Serialize `StashData` to JSON.
2. Gzip-compress the JSON.
3. Encrypt with XChaCha20-Poly1305 using a 24-byte random nonce.
4. Key Derivation: `HKDF-SHA256(PrivateKey.Seed, salt="nara:stash:v1", info="symmetric")`.

### Payload Limits
- **Max Size**: 10KB (enforced on receipt).
- **Capacity**: Confidant storage capacity depends on memory mode:
  - `short`: 5 stashes
  - `medium`: 20 stashes
  - `hog`: 50 stashes

### Confidant Selection
- **Target**: 3 confidants by default.
- **Strategy**:
  - First candidate: Best score (Highest uptime + High/Hog memory).
  - Subsequent candidates: Random selection (to distribute load).
- **Timeout**: Pending requests time out after 60s.
- **Retry**: Failed confidants are ignored for 5 minutes before retrying.

### Recovery
- **Trigger**: Owner sends `hey_there` on boot.
- **Push**: Confidants receiving `hey_there` wait 2s (jitter) then push the stash to the owner via `/stash/push`.
- **Conflict Resolution**: Owners decrypt all received stashes and keep the one with the newest timestamp.

### Eviction
- **Ghost Pruning**: Confidants remove stashes for owners who have been offline (MISSING/OFFLINE) for > 7 days.
- **Capacity Eviction**: If full, new requests are rejected (LRU eviction is not currently implemented; rejection is preferred to maintain promises).

## Failure Modes

- **No Confidants**: If all confidants are lost, stash is unrecoverable.
- **Decryption Failure**: If keys change (hardware change w/o soul migration), stash is unreadable.
- **Capacity**: If the network is full, new nodes may fail to find confidants.

## Security / Trust Model

- **Confidentiality**: End-to-end encrypted. Confidants see only blob size and owner name.
- **Integrity**: Poly1305 tag ensures ciphertext hasn't been tampered with.
- **Authentication**: Mesh auth headers (`X-Nara-Mesh-Auth`) verify the sender's identity for storage/retrieval.

## Test Oracle

- **Encryption**: Round-trip encryption/decryption works. (`stash_test.go`)
- **Capacity**: Confidants reject stashes when full based on memory mode. (`stash_test.go`)
- **Recovery**: Old stashes are discarded in favor of newer timestamps. (`stash_test.go`)
- **Ghost Pruning**: Offline owners are evicted after 7 days. (`stash_test.go`)

## Open Questions / TODO

- **Legacy MQTT**: `StashRequest`/`StashResponse` structs exist but are deprecated in favor of Mesh HTTP.
- **Resharding**: No mechanism to migrate stashes when a confidant shuts down gracefully (currently they just vanish).
