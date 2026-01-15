---
title: Mesh HTTP
---

# Mesh HTTP

The authenticated, point-to-point transport for gossip, sync, direct messaging, and stash exchange over tsnet/Headscale.

## Conceptual Model

| Concept | Rule |
| :--- | :--- |
| **Authentication** | All requests (except `/ping`) require Ed25519 signature headers. |
| **Integrity** | Responses are signed; verification is optional but recommended. |
| **Discovery** | `/ping` is unauthenticated to allow initial identity discovery. |
| **Transport** | WireGuard (Tailscale/Headscale) ensures encrypted point-to-point links. |

## Interfaces (Mesh Port: 7433)

### Auth Headers
Required for all endpoints except `/ping`:
- `X-Nara-Name`: Sender's name.
- `X-Nara-Timestamp`: Unix milliseconds.
- `X-Nara-Signature`: Base64 signature of `name + timestamp + method + path`.

### Core Endpoints
| Endpoint | Method | Payload | Purpose |
| :--- | :--- | :--- | :--- |
| `/ping` | `GET` | - | Latency & public key discovery. |
| `/events/sync` | `POST` | `SyncRequest` | Historical event reconciliation. |
| `/gossip/zine` | `POST` | `Zine` | P2P gossip exchange. |
| `/dm` | `POST` | `SyncEvent` | Direct message delivery. |
| `/stash/*` | `POST/DEL` | `StashPayload`| Distributed storage operations. |
| `/coordinates` | `GET` | - | Fetch peer's Vivaldi coordinates. |

## Algorithms

### 1. Request Verification
1. **Freshness**: Reject if timestamp skew > ±30s.
2. **Signature**: Verify `X-Nara-Signature` using sender's `PublicKey`.
3. **Discovery Fallback**: If sender is unknown, call `/ping` on the requester's IP to fetch the key, then retry verification.

### 2. Response Signing
Response header `X-Nara-Signature` covers: `name + timestamp + Base64(SHA256(body))`.

### 3. Direct Messages (DM)
1. Request must pass Mesh Auth.
2. Body must contain a valid, signed `SyncEvent`.
3. Recipient verifies `SyncEvent` signature and merges it into the ledger.

## Failure Modes
- **Auth Rejection**: Triggered by missing headers, expired timestamps, or invalid signatures.
- **Clock Skew**: Strict 30s window requires NTP synchronization.
- **Verification Loop**: If a sender can't be discovered via `/ping`, all their mesh requests are rejected.

## Security
- **Mesh Auth**: Ensures request origin and prevents replay (within 30s).
- **Payload Auth**: `SyncEvent` signatures ensure end-to-end integrity regardless of transport.
- **Transport**: Secured at the network layer by WireGuard.

## Test Oracle
- **Auth Protocol**: Header signing and verification logic. (`transport_mesh_auth_test.go`)
- **Integration**: Sync and DM delivery via mesh. (`integration_events_test.go`)
- **Gossip**: Bidirectional zine exchange. (`integration_gossip_test.go`)
