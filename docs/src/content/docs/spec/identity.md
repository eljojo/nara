# Identity

## Purpose

Identity answers: who is this nara, and can they prove it?
It binds a human-readable name to a cryptographic soul so peers can verify claims without any central registry.

## Conceptual Model

Entities:
- **Name**: a human-readable string (e.g., "jojo", "fuzzy-cat-123").
- **Soul**: a 40-byte binary value (32-byte seed + 8-byte tag), encoded as Base58.
- **Keypair**: Ed25519 keypair deterministically derived from the soul seed.
- **Nara ID**: A stable, unique identifier derived from `Base58(SHA256(soul_bytes || name_bytes))`.

Key Invariants:
- **Bonding**: A soul is only valid for the name it was minted with (the HMAC tag MUST match).
- **Determinism**: Same hardware + same name => same soul.
- **Portability**: A soul can travel to other hardware (via `-soul` flag) and remains valid.
- **Uniqueness**: A name can have many valid souls (minted on different hardware), but a soul is only valid for one name.
- **Authenticity**: Peers verify the bond `HMAC(seed, name) == tag`. If it fails, the identity is considered "inauthentic" but the node may still participate (with low trust).

## External Behavior

On startup, Nara resolves its identity:

1. **Name Resolution**:
   - If `-nara-id` flag is set -> use that name.
   - Else if hostname is "interesting" (not "localhost", "nixos", etc.) -> use hostname.
   - Else -> generate a name (e.g. "comfy-badger-42") from the soul.

2. **Soul Resolution**:
   - If `-soul` flag is set -> use that soul.
   - Else -> derive a "native" soul from the hardware fingerprint + name.

3. **Bond Validation**:
   - Check if `HMAC(soul.seed, name) == soul.tag`.
   - If valid: `IsValidBond = true`.
   - If invalid: `IsValidBond = false` (logs a warning, identity is suspect).

4. **ID Derivation**:
   - `ID = Base58(SHA256(soul_bytes || name_bytes))`.
   - This ID is used for sharding, DHT keys, and deduplication.

## Interfaces

### CLI / Environment
- `-nara-id` / `NARA_ID`: Explicit name override.
- `-soul` / `NARA_SOUL`: Base58 soul string to inherit identity.
- Hostname: Used as default name if not generic.

### Public Fields
- `NaraStatus.PublicKey`: Base64 Ed25519 public key (broadcast in presence events).
- `NaraStatus.ID`: Base58 Nara ID (broadcast in presence events).

### Event Payloads
Identity appears in all signed events:
- `HeyThereEvent` / `ChauEvent`: Include `PublicKey` and `ID`.
- `NewspaperEvent`: Includes `Status` with identity fields.
- Signatures: All events are signed by the soul's derived private key.

## Event Types & Schemas

### Identity Data Structure (`IdentityResult`)
```go
type IdentityResult struct {
    Name        string
    Soul        SoulV1
    ID          string // Nara ID
    IsValidBond bool
    IsNative    bool   // True if derived from current hardware
}
```

### Soul Structure (`SoulV1`)
- `Seed`: 32 bytes (Ed25519 seed).
- `Tag`: 8 bytes (HMAC tag).
- Serialized as: `Base58(Seed || Tag)` (approx 54 chars).

## Algorithms

### Soul Generation (Native)
```
Input: HardwareFingerprint, Name
Secret: "nara:soul:v2", "seed:custom:" + Name
Seed = HKDF-SHA256(Secret, HardwareFingerprint)
Tag  = HMAC-SHA256(Seed, "nara:name:v2:" + Name)[0..8]
Soul = Seed || Tag
```

### Soul Generation (Generated Name)
```
Input: HardwareFingerprint
Secret: "nara:soul:v2", "seed:generated"
Seed = HKDF-SHA256(Secret, HardwareFingerprint)
Name = GenerateName(Hex(Seed))
Tag  = HMAC-SHA256(Seed, "nara:name:v2:" + Name)[0..8]
Soul = Seed || Tag
```

### Bond Validation
```
Function ValidateBond(Soul, Name):
    ExpectedTag = HMAC-SHA256(Soul.Seed, "nara:name:v2:" + Name)[0..8]
    Return ConstantTimeCompare(Soul.Tag, ExpectedTag)
```

### ID Derivation
```
Function ComputeNaraID(SoulBytes, Name):
    Hash = SHA256(SoulBytes || NameBytes)
    Return Base58(Hash)
```

### Encryption Key Derivation
Symmetric keys for stash (self-storage) are derived from the private key:
```
Seed = PrivateKey.Seed()
SymmetricKey = HKDF-SHA256(Seed, Salt="nara:stash:v1", Info="symmetric")
```

## Failure Modes

- **Invalid Soul Format**:
  - If `-soul` is malformed (not Base58, wrong length), startup fails or falls back to native (depending on strictness).
- **Invalid Bond**:
  - If `-soul` and `-nara-id` don't match (HMAC failure), `IsValidBond` is false.
  - Node starts but peers may reject or mistrust it.
- **Hardware Change**:
  - If hardware changes, the "native" soul changes.
  - If using `-soul`, the identity persists (IsNative = false, IsValidBond = true).
  - If using auto-derived identity, the node effectively becomes a new person (new soul, new ID).

## Security / Trust Model

- **Self-Sovereign**: No central authority issues identities.
- **Trust on First Use (TOFU)**: Peers remember the first valid soul they see for a name.
- **Portability**: You can move your soul to a new machine.
- **Impersonation Protection**: You cannot forge a soul for someone else's name without their seed (because of the HMAC check). You *can* mint a new valid soul for their name on your hardware, but it will have a different ID and Public Key, so peers will see it as a different identity (or a conflict).

## Test Oracle

- **Determinism**: `NativeSoulCustom(hw1, "jojo")` always produces the same soul. (`identity_soul_test.go`)
- **Bonding**: `ValidateBond` returns true for matching name, false for others. (`identity_soul_test.go`)
- **Portability**: A soul generated on HW1 is valid for the same name on HW2. (`identity_soul_test.go`)
- **Generated Names**: `NativeSoulGenerated` derives the name from the seed, ensuring the bond holds. (`identity_soul_test.go`)
- **ID Stability**: Nara ID is derived from Soul+Name, not ephemeral keys. (`identity_detection_test.go`)
- **Signatures**: Messages signed by the identity verify against the public key. (`identity_crypto_test.go`)

## Open Questions / TODO

- **Hardware Fingerprint Stability**: How stable is the fingerprint across OS updates? Currently relies on `machine-id` or MAC addresses.
- **Identity Rotation**: No standard protocol for rotating keys while keeping the name/reputation.
