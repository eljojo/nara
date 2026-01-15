---
title: Mesh HTTP
description: Authenticated point-to-point communication over WireGuard.
---

# Mesh HTTP

Mesh HTTP is the primary transport for sensitive and bulk data exchange in the Nara Network. It runs over an encrypted WireGuard-based mesh (using `tsnet` and `Headscale`), providing authenticated point-to-point links for gossip, storage, and direct messaging.

## Purpose
- Provide an encrypted and authenticated alternative to public MQTT.
- Facilitate large data transfers like [Zines](./zines.md) and [Sync Protocol](./sync-protocol.md) batches.
- Enable distributed storage through [Stash](./stash.md).
- Allow direct, low-latency communication for [World Postcards](./world-postcards.md).

## Conceptual Model
- **Point-to-Point**: Communication is direct between two naras.
- **Mesh Port**: All naras listen on port `7433` (DefaultMeshPort).
- **Authentication**: All requests (except `/ping`) must be signed by the sender's soul.
- **Middleware**: A centralized `meshAuthMiddleware` handles signature verification and discovery.

### Invariants
- **Identity Headers**: Every authenticated request must include `X-Nara-Name`, `X-Nara-Timestamp`, and `X-Nara-Signature`.
- **Clock Tolerance**: Request timestamps must be within ±30 seconds of the receiver's clock.
- **Signed Responses**: Responses are also signed, allowing the requester to verify the responder's identity.

## External Behavior
- **Latency Measurement**: Naras perform `/ping` requests to calculate network RTT for [Vivaldi Coordinates](./coordinates.md).
- **Zine Exchange**: Naras periodically `POST` their zines to random peers and receive the peer's zine in response.
- **Stash Management**: Owners use Mesh HTTP to store and retrieve their encrypted state on [Confidants](./stash.md#confidant-selection).

## Interfaces

### Authenticated Request Headers
- `X-Nara-Name`: Sender's human-readable name.
- `X-Nara-Timestamp`: Unix milliseconds when the request was sent.
- `X-Nara-Signature`: Ed25519 signature of the string `{name}{timestamp}{method}{path}`.

### Authenticated Response Headers
- `X-Nara-Name`: Responder's name.
- `X-Nara-Timestamp`: Unix milliseconds when the response was sent.
- `X-Nara-Signature`: Ed25519 signature of the string `{name}{timestamp}{base64(sha256(body))}`.

### Core Endpoints
| Endpoint | Method | Purpose |
| :--- | :--- | :--- |
| `/ping` | `GET` | Latency measurement and public key discovery (Unauthenticated). |
| `/gossip/zine` | `POST` | Bidirectional exchange of recent events. |
| `/dm` | `POST` | Immediate delivery of a single `SyncEvent`. |
| `/events/sync` | `POST` | Reconciliation of historical ledger data. |
| `/world/relay` | `POST` | Forwarding a [World Postcard](./world-postcards.md). |
| `/stash/*` | `POST/DEL`| [Stash](./stash.md) operations (store, retrieve, push, delete). |

## Algorithms

### 1. Mesh Authentication (Middleware)
For every incoming request:
1. **Freshness Check**: Reject if `abs(now - timestamp) > 30s`.
2. **Signature Verification**: 
    - Fetch the public key for the claimed `X-Nara-Name`.
    - If unknown, attempt **On-Demand Discovery**: call `/ping` on the sender's IP.
    - Verify signature.
3. **Verified Sender**: Store the verified name in the request context for the handler to use.

### 2. Mesh Discovery Fallback
If a Nara receives a request from a name it doesn't recognize:
1. Extract the IP from the remote address (must be in the `100.64.x.x` mesh range).
2. Perform an unauthenticated `GET /ping` to that IP.
3. If successful, import the returned `PublicKey` and `Nara ID` into the neighborhood.
4. Retry the original request's signature verification.

## Failure Modes
- **Auth Failure**: If keys are out of sync or clocks drift > 30s, mesh communication between two nodes fails entirely.
- **Looping Discovery**: If discovery fails to find a key, or finds the wrong one, requests are rejected with `401 Unauthorized`.
- **Mesh Partition**: If a Nara cannot reach the mesh controller (Headscale), it loses all mesh connectivity but may still function via MQTT.

## Security / Trust Model
- **Network Layer**: WireGuard provides transport-level encryption and IP-based identity.
- **Application Layer**: Ed25519 signatures provide end-to-end authentication and integrity.
- **Privacy**: Only the `/ping` endpoint is public; all other data is restricted to verified peers.

## Test Oracle
- `TestMeshAuth_SignVerify`: Validates the header generation and verification logic.
- `TestMeshAuth_ClockSkew`: Ensures that requests outside the 30s window are rejected.
- `TestMeshAuth_UnknownSenderDiscovery`: Verifies that on-demand discovery works for new peers.
- `TestMeshAuth_ResponseSigning`: Checks that response bodies are correctly signed by the server.
