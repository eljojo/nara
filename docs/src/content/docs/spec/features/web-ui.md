---
title: Web UI
description: Real-time visualization and inspection dashboard for the nara network.
---

The Web UI is an embedded Single Page App (SPA) that provides a human-friendly view of a nara's internal state and its perception of the network. It is the primary tool for observing the "social network for computers" in action.

## 1. Purpose
- Visualize real-time network activity (Buzz, Teasing, Presence).
- Provide a "Network Radar" map based on estimated latency.
- Allow users to inspect the ledger and the derived opinions (The Trinity).
- Display a nara's unique visual identity (Aura and Avatar).

## 2. Conceptual Model
- **Embedded SPA**: The UI is built using Preact and esbuild, and is embedded directly into the Go binary.
- **Reactive Feed**: The UI reacts to real-time events streamed from the nara via Server-Sent Events (SSE).
- **Radar Map**: A 2D visualization of the network topology based on [Network Coordinates](/docs/spec/coordinates/).
- **The Timeline**: A chronological feed of social and system events.

### Invariants
1. **Local Access**: By default, the UI is served on a local port (8080) and is intended for the nara's owner.
2. **Subjective Rendering**: The UI displays the world *exactly* as the local nara sees it, including any hazy memories or divergent opinions.
3. **No Local State**: The UI is stateless; all data is fetched or streamed from the nara at runtime.

## 3. External Behavior
- Users access the UI by navigating to the nara's HTTP address in a browser.
- The "Shooting Stars" animation provides immediate visual feedback for social interactions.
- The "Radar" map shows naras moving in virtual space as latency measurements are refined.
- The "Profile" view shows detailed stats, personality traits, and aura for individual peers.

## 4. Interfaces
- **Main Entrypoint**: `GET /`
- **Data Source**: `GET /api.json` and `GET /narae.json`
- **Real-time Stream**: `GET /events` (SSE)

## 5. Event Types & Schemas
The UI consumes UI-formatted events from the SSE stream. See the [HTTP API Spec](/docs/spec/http-api/) for the schema.

## 6. Algorithms
- **Radar Projection**: Maps `(x, y)` coordinates from the Vivaldi algorithm onto a circular radar display.
- **D3.js Visualization**: Uses D3 to manage the positioning and transitions of naras on the map.
- **Event Mapping**: Translates raw `SyncEvent` types into icons and human-readable strings (e.g., `tease` with reason `nice-number` → 😈 "Alice teased Bob about the number 69").

## 7. Failure Modes
- **SSE Connection Loss**: The UI should display a "Disconnected" state and attempt to reconnect automatically.
- **Asset Inconsistency**: If the binary is built without the latest web assets, the UI may be broken or outdated.
- **Browser Compatibility**: The UI relies on modern browser features like SSE, ES modules, and CSS Grid.

## 8. Security / Trust Model
- **Read-Only by Default**: Most UI elements are for observation only.
- **Triggering Actions**: Actions like "Start Journey" or "Update Stash" require the user to be able to reach the nara's local API.

## 9. Test Oracle
- `TestWebUI_Bundling`: Ensures that frontend assets are correctly embedded in the Go binary.
- `TestWebUI_API_Integration`: Verifies that the UI can successfully fetch and parse the neighbourhood snapshot.
- `TestWebUI_SSE_Rendering`: Confirms that events received via SSE are correctly rendered in the timeline.

## 10. Open Questions / TODO
- Implement "Global View" where the UI can aggregate data from multiple naras.
- Add "Admin Mode" with protected endpoints for identity migration and manual checkpointing.
