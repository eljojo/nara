package nara

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"tailscale.com/tsnet"
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
func (network *Network) buildMeshURL(name string, path string) string {
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
func (network *Network) getMeshURLForNara(name string) string {
	return network.buildMeshURL(name, "")
}

// hasMeshConnectivity returns true if a nara is reachable via mesh
// Checks both test URLs and production mesh IPs
func (network *Network) hasMeshConnectivity(name string) bool {
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

// initMeshHTTPClients initializes shared mesh HTTP clients and transport.
func (network *Network) initMeshHTTPClients(tsnetServer *tsnet.Server) {
	if tsnetServer == nil {
		return
	}
	if network.meshHTTPTransport == nil {
		network.meshHTTPTransport = &http.Transport{
			DialContext:         tsnetServer.Dial,
			MaxIdleConns:        200,
			MaxIdleConnsPerHost: 20,
			IdleConnTimeout:     30 * time.Second,
		}
	}
	if network.meshHTTPClient == nil {
		network.meshHTTPClient = &http.Client{
			Transport: network.meshHTTPTransport,
			Timeout:   5 * time.Second,
		}
	}
}
