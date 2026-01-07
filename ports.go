package nara

// Default ports for nara services
//
// Nara runs two separate servers on different network interfaces:
//
// 1. Web UI Server (default :8080): Serves the web interface and local API
//   - Only starts if -serve-ui flag is set
//   - Defaults to port 8080, overridable via -http-addr flag or HTTP_ADDR env var
//   - Listens on local network interface
//   - Endpoints: /api.json, /metrics, /ping, web UI, etc.
//
// 2. Mesh Server (DefaultMeshPort = 7433): Handles world message relay via Tailscale
//   - Enabled by default, disable with --no-mesh flag
//   - Listens on Tailscale network interface only
//   - Accessible only to other naras on the mesh VPN
//   - Used for: world journey relay, event sync, mesh pings
//
// These are separate because they serve different networks and purposes.
const (
	// DefaultMeshPort is the default port for the mesh server (Tailscale interface)
	// Used for world message relay and mesh-to-mesh communication
	// 7433 = NARA on a phone keypad :)
	DefaultMeshPort = 7433
)
