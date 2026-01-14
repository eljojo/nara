# Identity

## Purpose

Identity answers the question:  
**“Who is this nara, really?”**

Nara identity is:
- deterministic
- cryptographic
- portable
- non-centralized

Identity is not granted by the network.  
It is *asserted* and *verified*.

---

## Conceptual Model

Each nara has:
- a **name** (human-readable)
- a **soul** (cryptographic root)
- a **keypair** (ed25519)
- a **personality**
- derived visual traits

Key invariants:
- A soul binds to exactly one name.
- Same soul + same name = same nara.
- A soul cannot validly claim another name.
- Losing the soul means losing the identity permanently.

---

## External Behavior

On startup, a nara:
1. Determines its name.
2. Derives or loads its soul.
3. Verifies the name ↔ soul bond.
4. Announces itself to peers.

Peers classify identity claims as:
- **authentic** (bond verified)
- **shadow** (bond mismatch or unverifiable)

---

## Interfaces

Relevant surfaces:
- CLI / config for explicit name and soul
- Identity fields exposed in HTTP API
- Signed events containing nara ID and public key

---

## Algorithms

### Name selection
1. If name is configured → use it
2. Else if hostname is non-generic and unique → use it
3. Else → generate quirky name from hardware ID

### Soul derivation
- Input: hardware fingerprint + name
- Output: deterministic Base58 string (~54 chars)
- Soul embeds a cryptographic bond to the name

### Bond verification

```
bond_tag = hash(seed, name)
verify(hash(seed, claimed_name) == bond_tag)
```

---

## Failure Modes

- Hardware changes may generate a different soul.
- Using a soul with a different name must fail verification.
- Two naras may share a name but never share a soul.

---

## Security / Trust Model

- Souls are secret.
- Public keys are derived from souls.
- Identity authenticity is local and verifiable.
- No global registry exists.

---

## Test Oracle

- Same hardware + same name → same soul.
- Same soul + different name → rejected.
- Copying a soul preserves identity across machines.

---

## Open Questions / TODO

- Formal definition of “hardware fingerprint” stability.
