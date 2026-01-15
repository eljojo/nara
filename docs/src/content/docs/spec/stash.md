---
title: Stash
description: Distributed encrypted storage and redundancy in the Nara Network.
---

# Stash

Stash is Nara's distributed, encrypted, memory-only storage system. It allows naras to survive restarts and hardware migrations by storing small (max 10KB) encrypted blobs of state with "confidants"—other naras on the network.

## Purpose
- Provide persistence for essential state (e.g., personality, history) without using local disk.
- Enable identity migration: a nara can recover its state on new hardware if it has its `soul`.
- Maintain privacy: only the owner of a stash can decrypt its contents.
- Ensure availability: stashes are replicated across multiple confidants.

## Conceptual Model
- **Owner**: The Nara who creates and owns the stash data.
- **Confidant**: A peer who agrees to store an encrypted stash for an owner.
- **StashData**: The raw, sensitive JSON data (includes a timestamp for versioning).
- **StashPayload**: The encrypted and compressed blob (includes owner name, nonce, and ciphertext).
- **Redundancy**: By default, each Nara attempts to maintain 3 confidants.

### Invariants
- **Owner-Only Decryption**: Stashes are encrypted with a key derived from the owner's private soul; confidants cannot read the data.
- **Memory-Only**: Stashes are never written to disk by confidants; if a confidant restarts, the stashes it was holding are lost.
- **Size Limit**: Stash payloads are capped at 10KB to prevent network and memory exhaustion.
- **Authentication**: All stash operations (store, retrieve, delete) are gated by Mesh-authenticated identity.

## External Behavior
- **Distribution**: Owners periodically check their confidant count and search for new ones if below the target (3).
- **Recovery**: On startup, a Nara's neighbors who hold its stash will "push" it back to the owner upon seeing their `hey_there`.
- **Active Fetch**: A Nara can also explicitly poll neighbors for its stash using `/stash/retrieve`.
- **Storage Limits**: Confidants limit how many stashes they store based on their `MemoryMode` (Short: 5, Medium: 20, Hog: 50).

## Interfaces

### HTTP API: `/stash/store` (POST/DELETE)
Used by owners to manage their stash on a confidant.

### HTTP API: `/stash/retrieve` (POST)
Used by owners to fetch their stash from a confidant.

### HTTP API: `/stash/push` (POST)
Used by confidants to proactively return a stash to its owner (e.g., after the owner restarts).

## Algorithms

### 1. Encryption & Compression
1. **Compress**: The `StashData` JSON is Gzip-compressed.
2. **Key Derivation**: A symmetric key is derived from the owner's Ed25519 seed using HKDF-SHA256 (salt="nara:stash:v1", info="symmetric").
3. **Encrypt**: The compressed data is encrypted using **XChaCha20-Poly1305** with a random 24-byte nonce.

### 2. Confidant Selection
Owners score potential confidants based on:
- **Memory Mode**: Hogs (+300) > Medium (+200) > Short (+100).
- **Uptime**: Longer-running naras are considered more stable.
- **Jitter**: Small random factor to prevent tie-breaking hotspots.
- **Strategy**: The first confidant is the "best" (highest score); subsequent confidants are chosen randomly from available online peers to distribute load.

### 3. Ghost Pruning
Confidants periodically (every 5 minutes) check the status of owners they are storing stashes for.
- If an owner has been **OFFLINE** or **MISSING** for more than 7 days, their stash is evicted to free up space.

### 4. Recovery (Push)
1. Confidant observes a `hey_there` from Nara `X`.
2. Confidant checks if they hold a stash for `X`.
3. If yes, the confidant waits for a small random delay (to avoid thundering herd) and calls `POST /stash/push` on `X`.

## Failure Modes
- **Network Wipe**: If all naras in a cluster restart simultaneously, all stash data is lost (hazy memory).
- **Decryption Failure**: If a Nara loses its soul or uses the wrong one, it cannot decrypt its recovered stash.
- **Capacity Rejection**: A confidant may reject a `store` request if its memory-mode limit is reached.

## Security / Trust Model
- **Confidentiality**: XChaCha20-Poly1305 ensures only the owner (seed holder) can read the data.
- **Integrity**: The Poly1305 tag ensures the ciphertext hasn't been tampered with.
- **Replay Protection**: Timestamps in the encrypted payload allow the owner to reject old versions.
- **Access Control**: Mesh authentication headers (`X-Nara-Mesh-Auth`) ensure only the legitimate owner can store or delete their stash.

## Test Oracle
- `TestStashEncryption`: Verifies that data can be encrypted and then decrypted back to its original form.
- `TestStashStorageLimits`: Ensures that confidants reject stashes when their memory-mode limit is hit.
- `TestConfidantSelection`: Checks that the "best" naras (high uptime/memory) are prioritized.
- `TestGhostPruning`: Validates that stashes for long-offline owners are eventually deleted.
- `TestStashRecoveryPush`: Verifies the proactive push mechanism after an owner sends `hey_there`.
