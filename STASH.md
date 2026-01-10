# Stash - Distributed Encrypted Storage

Naras store their encrypted identity data (emojis) on other naras ("confidents") instead of local disk. This enables stateless operation where a nara can reboot on any machine and recover its identity sequence.

## How It Works

1. **Owner** picks 2-3 random naras as **confidents**
2. Owner encrypts their tsnet state with their own key (derived from soul)
3. Owner sends encrypted stash to confidents via DM
4. Confidents store in **memory only** (lost on restart)
5. On boot, owner broadcasts "who has my stash?" and recovers from responses

## Key Properties

- **Self-encrypted**: Only the owner can decrypt their stash (key derived from soul)
- **Random selection**: Confidents are chosen randomly to spread load
- **Memory-only**: Confidents don't persist stash to disk
- **Tamper-proof**: Timestamp is inside the encrypted payload
- **Social reward**: Storing stash for someone gives you clout

## Data Flow

### Storing Stash (Owner -> Confidents)

```
Owner                           Confident
  |                                 |
  |-- StashStore (encrypted) ------>|
  |                                 | stores in memory
  |<-------- StashStoreAck ---------|
```

### Recovering Stash (Boot)

```
Owner                           All Naras
  |                                 |
  |-- StashRequest (broadcast) ---->|
  |                                 |
  |<------- StashResponse ----------| (only confidents with stash respond)
  |                                 |
  | decrypt, pick newest timestamp  |
  | write to tsnet state dir        |
  | start tsnet                     |
```

### Managing Confidents

- **Target count**: 2-3 confidents
- **Too few**: Pick random new confident, send StashStore
- **Too many**: DM extras with StashDelete
- **Confident offline**: Pick new random confident
- **Re-send triggers**: Confident reboots, or stash data changes

## MQTT Topics

| Topic | Direction | Message |
|-------|-----------|---------|
| `nara/stash/{target}/store` | Owner -> Confident | StashStore |
| `nara/stash/{owner}/ack` | Confident -> Owner | StashStoreAck |
| `nara/plaza/stash_request` | Owner -> Broadcast | StashRequest |
| `nara/stash/{owner}/response` | Confident -> Owner | StashResponse |
| `nara/stash/{target}/delete` | Owner -> Confident | StashDelete |

## Encryption

Uses XChaCha20-Poly1305 with a symmetric key derived from the owner's Ed25519 private key:

```
Ed25519 seed -> HKDF("nara:stash:v1") -> 32-byte symmetric key
```

The encrypted payload contains:
- `timestamp`: When the stash was created (inside encrypted blob for tamper-proofing)
- `state`: tar.gz of tsnet state directory (~3KB)

## Security

- **10KB size limit** on stash payload
- **Signature verification** on StashStore and StashRequest
- **Path traversal protection** when extracting tar
- **Memory-only** on confidents (no disk writes)
- **Random selection** spreads load, avoids "hot keys"

## Boot Sequence

```
1. MQTT connects
2. Wait 5s for neighbor discovery
3. Broadcast StashRequest
4. Wait up to 2 minutes for StashResponse
5. If received:
   - Decrypt all responses
   - Pick newest by timestamp
   - Write to tsnet state dir
   - Start tsnet
6. If timeout + authkey:
   - Register fresh with Headscale
7. If timeout + no authkey:
   - Mesh disabled
```

## Social Reward

When a confident stores stash for an owner, they receive clout:

```go
SocialEvent{
    Type:   "stash_stored",
    Actor:  confidentName,
    Target: ownerName,
    Reason: ReasonStashStored,
}
```

This encourages naras to participate in the stash network.
