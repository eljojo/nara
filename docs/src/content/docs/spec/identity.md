# Identity

## Purpose

Identity answers: who is this nara, and can they prove it?
It binds a human-readable name to a cryptographic soul so peers can verify
claims without any central registry.

## Conceptual Model

Entities:
- Name: a human-readable string (explicit flag or hostname or generated).
- Soul: a 40-byte binary value (32-byte seed + 8-byte tag), Base58-encoded.
- Keypair: Ed25519 keypair deterministically derived from the soul seed.
- Nara ID: Base58(SHA256(soul_bytes || name_bytes)), used as a stable unique ID.

Key invariants:
- A soul is only valid for the name it was minted with (bond tag must match).
- Same hardware + same name => same soul (deterministic).
- A soul can travel to other machines and still be valid for its name.
- Invalid or mismatched souls are allowed to exist locally but marked inauthentic.
- Souls never appear in public status or over the network.

## External Behavior

On startup:
1. Resolve the effective name.
2. Parse or derive the soul.
3. Validate the name <-> soul bond.
4. Derive public keys and nara ID.

Name resolution:
- If the `-nara-id` flag is provided, that value is the name.
- Else, use hostname (short form, no domain suffix).
- If the hostname is generic (e.g., "nixos", "localhost", container IDs), the name is generated.

Soul resolution:
- If `-soul` is provided, parse it as Base58-encoded 40 bytes.
- If no soul is provided:
  - Generated-name mode: derive a soul from hardware only, then derive the name from the seed.
  - Custom-name mode: derive a soul from hardware and the explicit name.

Identity flags:
- `IsValidBond` is false when the soul tag does not match the resolved name.
- `IsNative` is true only when the soul is exactly what this hardware would derive.

## Interfaces

CLI / environment:
- `-nara-id` / `NARA_ID`: explicit name override.
- `-soul` / `NARA_SOUL`: Base58 soul string to inherit identity.

Public fields:
- `NaraStatus.PublicKey` (Base64 Ed25519 public key) is broadcast.
- `NaraStatus.ID` (Nara ID) is broadcast in hey_there/chau payloads.

## Event Types & Schemas (if relevant)

Identity shows up inside other payloads:
- `HeyThereEvent.PublicKey`, `HeyThereEvent.ID`
- `ChauEvent.PublicKey`, `ChauEvent.ID`
- `NewspaperEvent.Status.PublicKey`, `NewspaperEvent.Status.ID`

## Algorithms

Soul format:
- Seed: 32 bytes.
- Tag: first 8 bytes of HMAC-SHA256(seed, "nara:name:v2:" + name).
- Soul string: Base58(seed || tag), 40 bytes total.

Soul derivation:
- HKDF-SHA256 over the hardware fingerprint.
- Custom name: info = "seed:custom:" + name.
- Generated name: info = "seed:generated" and name = GenerateName(hex(seed)).

Nara ID derivation:
- Decode soul Base58 to 40 bytes.
- ID = Base58(SHA256(soul_bytes || name_bytes)).

Key derivation:
- Ed25519 private key is derived from the soul seed.
- Public key is the Ed25519 public key.
- Stash encryption key is derived via HKDF-SHA256 over the private key seed
  using salt "nara:stash:v1" and info "symmetric".

## Failure Modes

- Invalid soul string (bad Base58 or wrong length) -> IsValidBond=false, ID empty.
- Name/soul mismatch -> IsValidBond=false, but node still runs and is marked inauthentic.
- Hardware changes (HostID or MACs) -> different native soul, IsNative=false for old soul.

## Security / Trust Model

- Souls are secret and never serialized.
- Public keys are deterministic from souls, so signatures verify identity.
- Identity authenticity is local verification of the soul/name bond (no global registry).

## Test Oracle

- Same hardware + same name => same soul. (`identity_soul_test.go`)
- Same soul + different name => invalid bond. (`identity_soul_test.go`)
- Generated-name mode derives name from soul seed. (`identity_soul_test.go`)
- HeyThere/Chau signatures verify with the embedded public key. (`identity_crypto_test.go`)

## Open Questions / TODO

- Stability of hardware fingerprint inputs across OS upgrades and VM migrations.
