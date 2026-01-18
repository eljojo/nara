package nara

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"tailscale.com/tsnet"

	"github.com/eljojo/nara/types"
)

// buildMeshURLFromIP builds a mesh URL from an IP address and optional path.
// Handles both test URLs (with port already included) and production IPs (adds DefaultMeshPort).
// Examples:
//   - buildMeshURLFromIP("100.64.0.1", "/ping") -> "http://100.64.0.1:7433/ping"
//   - buildMeshURLFromIP("127.0.0.1:12345", "/ping") -> "http://127.0.0.1:12345/ping" (test)
func (network *Network) buildMeshURLFromIP(ip string, path string) string {
	if ip == "" {
		return ""
	}

	var baseURL string
	if strings.Contains(ip, ":") {
		// IP already contains port (e.g., from tests)
		baseURL = "http://" + ip
	} else {
		// Production IP without port - add DefaultMeshPort
		baseURL = fmt.Sprintf("http://%s:%d", ip, DefaultMeshPort)
	}

	if path != "" {
		return baseURL + path
	}
	return baseURL
}

// buildMeshURL returns the full mesh URL for a nara with optional path
// Returns empty string if nara is not reachable via mesh
// Examples:
//   - buildMeshURL("alice", "") -> "http://100.64.0.1:5683"
//   - buildMeshURL("alice", "/stash/push") -> "http://100.64.0.1:5683/stash/push"
func (network *Network) buildMeshURL(name types.NaraName, path string) string {
	var baseURL string
	if network.testMeshURLs != nil {
		baseURL = network.testMeshURLs[name]
		if baseURL == "" {
			return ""
		}
	} else {
		meshIP := network.getMeshIPForNara(name)
		if meshIP == "" {
			return ""
		}
		baseURL = network.buildMeshURLFromIP(meshIP, "")
	}

	if path != "" {
		return baseURL + path
	}
	return baseURL
}

// getMeshURLForNara returns the base mesh URL for a nara (for backwards compatibility)
func (network *Network) getMeshURLForNara(name types.NaraName) string {
	return network.buildMeshURL(name, "")
}

// hasMeshConnectivity returns true if a nara is reachable via mesh
// Checks both test URLs and production mesh IPs
func (network *Network) hasMeshConnectivity(name types.NaraName) bool {
	if network.testMeshURLs != nil {
		return network.testMeshURLs[name] != ""
	}
	return network.getMeshIPForNara(name) != ""
}

// getHTTPClient returns the HTTP client to use (test override or default)
func (network *Network) getHTTPClient() *http.Client {
	if network.testHTTPClient != nil {
		return network.testHTTPClient
	}
	if network.httpClient == nil {
		network.httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	return network.httpClient
}

// getMeshHTTPClient returns the mesh HTTP client to use (test override or tsnet client).
func (network *Network) getMeshHTTPClient() *http.Client {
	if network.testHTTPClient != nil {
		return network.testHTTPClient
	}
	if network.meshHTTPClient != nil {
		return network.meshHTTPClient
	}
	if network.tsnetMesh == nil || network.tsnetMesh.Server() == nil {
		return nil
	}
	network.initMeshHTTPClients(network.tsnetMesh.Server())
	return network.meshHTTPClient
}

// initMeshHTTPClients initializes shared mesh HTTP clients using centralized config.
func (network *Network) initMeshHTTPClients(tsnetServer *tsnet.Server) {
	if tsnetServer == nil {
		return
	}
	if network.meshHTTPClient == nil {
		// Use centralized mesh HTTP client from mesh_client.go
		// Provides consistent timeout behavior (5s connection, 5s request) across nara app and backup tool
		network.meshHTTPClient = NewMeshHTTPClient(tsnetServer)

		// Extract transport for reuse elsewhere if needed
		if transport, ok := network.meshHTTPClient.Transport.(*http.Transport); ok {
			network.meshHTTPTransport = transport
		}
	}
}
