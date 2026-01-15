# Stash

## Purpose

Stash provides distributed, encrypted, memory-only persistence. It lets naras
recover state after restarts by storing encrypted blobs with confidants.

## Conceptual Model

- Owners encrypt their stash and send it to confidants.
- Confidants store opaque ciphertext only.
- Recovery is owner-only: only the soul-derived key can decrypt.

Key invariants:
- Stash payloads are encrypted with XChaCha20-Poly1305.
- Confidants do not verify contents and cannot decrypt.
- Storage is in-memory only (no disk persistence).

## External Behavior

- On boot, naras attempt to recover stash from confidants.
- Confidants may push stash to owners when they see `hey_there` or `stash_refresh`.
- Owners maintain a target number of confidants in the background.

## Interfaces

Data types:
- `StashData`: `{timestamp, data, version}` (data is arbitrary JSON)
- `StashPayload`: `{owner, nonce, ciphertext}`

Mesh HTTP endpoints (mesh auth required):
- `POST /stash/store` -> {accepted, reason}
- `DELETE /stash/store` -> {deleted}
- `POST /stash/retrieve` -> {found, stash}
- `POST /stash/push` -> {accepted, reason}

MQTT:
- `nara/plaza/stash_refresh` -> {from, timestamp}

Legacy request/response (used in tests):
- `StashRequest` signed with "stash_request:{from}"
- `StashResponse` with `StashPayload`

## Event Types & Schemas (if relevant)

Stash does not emit SyncEvents; it uses mesh HTTP and occasional MQTT requests.
Social events may be emitted for stash operations (observability only).

## Algorithms

Encryption:
1. Serialize StashData to JSON.
2. Gzip-compress the JSON.
3. Encrypt with XChaCha20-Poly1305 using a 24-byte random nonce.
4. Key is derived via HKDF-SHA256 from the soul-derived Ed25519 private key
   (salt: "nara:stash:v1", info: "symmetric").

Payload limits:
- Max stash payload size is 10KB.
- Confidant storage capacity by memory mode:
  - short: 5
  - medium: 20
  - hog: 50

Confidant selection:
- Owners target 3 confidants by default.
- First candidate is highest-uptime/high-memory peer; others are random.
- Pending confidants time out after 60s; failed ones are retried after 5 minutes.

Recovery:
- On `hey_there`, confidants push stash back to the owner (2s delay).
- On `stash_refresh`, confidants push after 500ms.
- Owners accept the newest stash timestamp and ignore older payloads.

Eviction:
- Confidants keep stashes unless the owner is a ghost (offline > 7 days).

## Failure Modes

- Missing confidants -> stash cannot be recovered.
- Stale or replayed stash payloads are rejected by timestamp validation.
- Over-capacity confidants reject new owners (updates to existing owners allowed).

## Security / Trust Model

- Only the owner can decrypt the stash.
- Confidant storage is blind and purely a commitment to keep bytes.
- Mesh auth provides transport-level identity, but stash content is end-to-end encrypted.

## Test Oracle

- Stash encryption/decryption round trips. (`stash_test.go`)
- Confidant capacity limits (short/medium/hog). (`stash_test.go`)
- Recovery picks newest timestamp. (`stash_test.go`)

## Open Questions / TODO

- StashRequest MQTT flow is currently unused in production.
