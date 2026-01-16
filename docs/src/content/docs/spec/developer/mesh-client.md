---
title: Mesh Client Primitive
description: The authenticated HTTP client for direct peer-to-peer communication over the Tailscale mesh.
---

The `MeshClient` is the runtime primitive for direct P2P communication. While MQTT is used for broadcasts, the `MeshClient` handles targeted requests like syncing events, exchanging zines, or relaying world postcards.

## 1. Purpose
- Provide a signed and authenticated HTTP channel between two naras.
- Abstract IP resolution and mesh connectivity (Tailscale/tsnet) from services.
- Enable fast-failure and efficient connection pooling for mesh-connected peers.

## 2. Conceptual Model
- **The Client**: Encapsulates a `tsnet.Server` and a customized `http.Client`.
- **Authenticated Requests**: Every request includes headers (`X-Nara-Name`, `X-Nara-Signature`, `X-Nara-Timestamp`) that allow the receiver to verify the sender.
- **Peer Registry**: A local mapping of `NaraID` to mesh IP addresses and base URLs.

### Invariants
1. All mesh requests MUST be signed with the local `NaraKeypair`.
2. Connections MUST fail fast (5s timeout) if a peer is offline to prevent blocking the runtime.
3. Requests MUST use the mesh IP (100.x.y.z) and the dedicated Nara mesh port (9632).

## 3. External Behavior
- **Peer Resolution**: Services provide a `NaraID`; the `MeshClient` resolves this to an internal `http://100.x.y.z:9632` URL.
- **Request Signing**: The client automatically signs the method, path, and timestamp before sending.
- **Test Mode**: Supports URL overrides to allow unit tests to route mesh traffic to local test servers.

## 4. Interfaces

### Mesh Authentication Headers
- `X-Nara-Name`: The sender's `NaraName`.
- `X-Nara-Timestamp`: Unix milliseconds (prevents replay attacks).
- `X-Nara-Signature`: Ed25519 signature of the string: `Name + Timestamp + Method + Path`.

### Capabilities
- `PingNara(id)`: Measures RTT to a specific peer.
- `FetchSyncEvents(id, request)`: Executes a sync protocol request via `POST /events/sync`.
- `RelayWorldMessage(id, msg)`: Forwards a multi-hop postcard via `POST /world/relay`.
- `SendDM(id, msg)`: Sends a direct message to a peer.

## 5. Algorithms

### Mesh Request Signing
1. Capture `Method` (e.g., `POST`) and `Path` (e.g., `/events/sync`).
2. Generate current `Timestamp` (Unix ms).
3. Construct message string: `Name + Timestamp + Method + Path`.
4. Sign message string with Ed25519.
5. Set headers and execute HTTP request.

### Peer Discovery (tsnet)
1. Query `tsnet.LocalClient().Status()`.
2. Iterate through `Peer` list.
3. Filter out non-nara peers (e.g., backup tools).
4. Register `HostName` as `NaraName` and the first `TailscaleIP` in the peer registry.

## 6. Failure Modes
- **Offline Peer**: The client's `DialContext` uses a 5s timeout. If the mesh cannot route to the IP, the request fails immediately with a timeout error.
- **Signature Expiry**: Receivers check the `X-Nara-Timestamp`. If the request is too old (e.g., > 30s), it is rejected as a potential replay attack.

## 7. Security / Trust Model
- **Transport Security**: Tailscale (WireGuard) provides encrypted transport.
- **Application Authentication**: Nara mesh headers provide identity verification at the application layer.
- **Isolation**: Direct mesh communication is only possible between peers sharing the same Tailscale tailnet.

## 8. Test Oracle
- A request to a registered `NaraID` MUST be routed to the correct mesh IP.
- Tampering with the `X-Nara-Name` or `X-Nara-Timestamp` MUST result in an invalid signature on the receiving end.
- `PingNara` MUST return a valid `time.Duration` if the peer is online.

## 9. Open Questions / TODO
- **mTLS**: Exploring moving authentication from HTTP headers to a full mTLS handshake using the Nara's Ed25519 keys.
- **Streaming**: Implementing long-lived SSE streams over the mesh for real-time gossip.
