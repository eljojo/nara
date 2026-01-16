package nara

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/eljojo/nara/identity"
	"github.com/eljojo/nara/types"
	"tailscale.com/ipn/store/mem"
	"tailscale.com/tsnet"
)

// MeshConnectionConfig holds configuration for connecting to the mesh
type MeshConnectionConfig struct {
	Hostname   string
	ControlURL string
	AuthKey    string
	Ephemeral  bool
	Verbose    bool
}

// ConnectToMesh creates a tsnet connection and returns the server and HTTP client
func ConnectToMesh(ctx context.Context, config MeshConnectionConfig) (*tsnet.Server, *http.Client, error) {
	server := &tsnet.Server{
		Hostname:   config.Hostname,
		ControlURL: config.ControlURL,
		AuthKey:    config.AuthKey,
		Store:      new(mem.Store),
		Ephemeral:  config.Ephemeral,
	}

	if !config.Verbose {
		server.Logf = func(format string, args ...interface{}) {} // Suppress logs
	}

	// Start and wait for connection
	if _, err := server.Up(ctx); err != nil {
		return nil, nil, fmt.Errorf("failed to connect to mesh: %w", err)
	}

	return server, server.HTTPClient(), nil
}

// NewMeshHTTPClient creates a properly configured HTTP client for mesh communication.
// Uses aggressive timeouts for fast failure when peers are offline:
// - 5s connection timeout (fails fast on down peers)
// - 5s default request timeout (can be overridden per-request with context)
// - Connection pooling for efficiency
func NewMeshHTTPClient(server *tsnet.Server) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				// 5s connection timeout for fast failure on down peers
				dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				return server.Dial(dialCtx, network, addr)
			},
			MaxIdleConns:        100, // used to be 200!
			MaxIdleConnsPerHost: 20,
			IdleConnTimeout:     90 * time.Second,
		},
		Timeout: 5 * time.Second, // Default request timeout (override per-request if needed)
	}
}

// MeshClient handles authenticated HTTP communication over the Tailscale mesh.
// It encapsulates the HTTP client and identity used for signing requests.
// This allows both the main nara app and external tools (like nara-backup)
// to share the same mesh communication logic.
//
// MeshClient works exclusively with Nara IDs and internally resolves them to
// mesh URLs. It supports test mode for unit tests via optional URL overrides.
type MeshClient struct {
	httpClient *http.Client
	name       types.NaraName       // Who we are (for request signing)
	keypair    identity.NaraKeypair // For signing requests

	peers map[types.NaraID]string // naraID -> baseURL (e.g., "http://100.64.0.1:9632")

	// Test mode support
	testURLs map[types.NaraID]string // Override URLs for tests (bypasses IP resolution)
}

// NewMeshClient creates a new mesh client with the given identity
func NewMeshClient(httpClient *http.Client, name types.NaraName, keypair identity.NaraKeypair) *MeshClient {
	return &MeshClient{
		httpClient: httpClient,
		name:       name,
		keypair:    keypair,
		peers:      make(map[types.NaraID]string),
		testURLs:   make(map[types.NaraID]string),
	}
}

// EnableTestMode enables test mode with optional URL overrides.
// When test URLs are provided, they bypass normal IP resolution.
func (m *MeshClient) EnableTestMode(testURLs map[types.NaraID]string) {
	if testURLs != nil {
		m.testURLs = testURLs
	}
}

// UpdateHTTPClient updates the HTTP client (used when tsnet becomes available)
func (m *MeshClient) UpdateHTTPClient(client *http.Client) {
	m.httpClient = client
}

// RegisterPeer registers a peer's base URL by nara ID.
// For production mesh peers, use RegisterPeerIP which builds the standard mesh URL.
func (m *MeshClient) RegisterPeer(naraID types.NaraID, baseURL string) {
	m.peers[naraID] = baseURL
}

// RegisterPeerIP registers a peer by nara ID and mesh IP.
// Builds the standard mesh URL (http://<ip>:9632).
// Handles both production IPs (adds port) and test IPs (with port already included).
func (m *MeshClient) RegisterPeerIP(naraID types.NaraID, ip string) {
	m.peers[naraID] = buildMeshURLFromIP(ip)
}

// buildMeshURLFromIP builds a mesh base URL from an IP address.
// Handles both test URLs (with port already included) and production IPs (adds DefaultMeshPort).
// Examples:
//   - buildMeshURLFromIP("100.64.0.1") -> "http://100.64.0.1:9632"
//   - buildMeshURLFromIP("127.0.0.1:12345") -> "http://127.0.0.1:12345" (test)
func buildMeshURLFromIP(ip string) string {
	if ip == "" {
		return ""
	}

	// Check if port is already included (test mode)
	if strings.Contains(ip, ":") {
		return "http://" + ip
	}

	// Production IP without port - add DefaultMeshPort
	return fmt.Sprintf("http://%s:%d", ip, DefaultMeshPort)
}

// UnregisterPeer removes a peer from the registry.
func (m *MeshClient) UnregisterPeer(naraID types.NaraID) {
	delete(m.peers, naraID)
}

// HasPeer returns true if a peer is registered.
func (m *MeshClient) HasPeer(naraID types.NaraID) bool {
	_, ok := m.peers[naraID]
	return ok
}

// GetPeerBaseURL returns the base URL for a registered peer (for legacy code paths)
func (m *MeshClient) GetPeerBaseURL(naraID types.NaraID) (string, bool) {
	url, ok := m.peers[naraID]
	return url, ok
}

// buildURL constructs the full URL for a request to a peer.
// Checks testURLs first (for test mode), then peers registry.
func (m *MeshClient) buildURL(naraID types.NaraID, path string) (string, error) {
	var baseURL string
	var ok bool

	// Check test URLs first (test mode override)
	if len(m.testURLs) > 0 {
		baseURL, ok = m.testURLs[naraID]
	}

	// Fall back to peers registry
	if !ok {
		baseURL, ok = m.peers[naraID]
	}

	if !ok {
		return "", fmt.Errorf("peer not registered: %s", naraID)
	}

	return baseURL + path, nil
}

// signRequest adds mesh authentication headers to an HTTP request
func (m *MeshClient) signRequest(req *http.Request) {
	timestamp := time.Now().UnixMilli()
	message := fmt.Sprintf("%s%d%s%s", m.name, timestamp, req.Method, req.URL.Path)

	req.Header.Set(HeaderNaraName, m.name.String())
	req.Header.Set(HeaderNaraTimestamp, strconv.FormatInt(timestamp, 10))
	req.Header.Set(HeaderNaraSignature, m.keypair.SignBase64([]byte(message)))
}

// BuildMeshURL builds a mesh URL for a given IP and path
func BuildMeshURL(ip string, path string) string {
	return fmt.Sprintf("http://%s:%d%s", ip, DefaultMeshPort, path)
}

// DiscoverMeshPeers discovers peers using tsnet Status API
func DiscoverMeshPeers(ctx context.Context, server *tsnet.Server) ([]TsnetPeer, error) {
	lc, err := server.LocalClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get local client: %w", err)
	}

	status, err := lc.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get status: %w", err)
	}

	var peers []TsnetPeer
	for _, peer := range status.Peer {
		if len(peer.TailscaleIPs) == 0 || peer.HostName == "" {
			continue
		}
		// Skip backup tool peers
		if len(peer.HostName) >= 12 && peer.HostName[:12] == "nara-backup-" {
			continue
		}
		peers = append(peers, TsnetPeer{
			Name: peer.HostName,
			IP:   peer.TailscaleIPs[0].String(),
		})
	}

	return peers, nil
}

// FetchSyncEvents fetches events from a peer via POST /events/sync
// Returns just the events (for backward compatibility)
func (m *MeshClient) FetchSyncEvents(ctx context.Context, naraID types.NaraID, req SyncRequest) ([]SyncEvent, error) {
	resp, err := m.FetchSyncEventsWithCursor(ctx, naraID, req)
	if err != nil {
		return nil, err
	}
	return resp.Events, nil
}

// FetchSyncEventsWithCursor fetches events from a peer via POST /events/sync
// Returns the full response including NextCursor for pagination
func (m *MeshClient) FetchSyncEventsWithCursor(ctx context.Context, naraID types.NaraID, req SyncRequest) (*SyncResponse, error) {
	// Ensure From is set to our identity
	req.From = m.name

	jsonBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url, err := m.buildURL(naraID, "/events/sync")
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	m.signRequest(httpReq)

	resp, err := m.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var response SyncResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &response, nil
}

// FetchCheckpoints fetches checkpoint events from a peer using the unified sync API
// Returns checkpoints in oldest-first order with cursor-based pagination
func (m *MeshClient) FetchCheckpoints(ctx context.Context, naraID types.NaraID, cursor string, pageSize int) (*SyncResponse, error) {
	if pageSize <= 0 || pageSize > 5000 {
		pageSize = 1000
	}

	return m.FetchSyncEventsWithCursor(ctx, naraID, SyncRequest{
		Mode:     "page",
		Services: []string{ServiceCheckpoint},
		Cursor:   cursor,
		PageSize: pageSize,
	})
}

// TODO: Add PostGossipZine when Zine type signature is confirmed
// TODO: Add SendDM when DirectMessage type is confirmed (currently uses SyncEvent)

// PingNara sends a ping to a peer by NaraID and returns the round-trip time.
// This is the preferred method for pinging peers in most cases.
func (m *MeshClient) PingNara(ctx context.Context, naraID types.NaraID) (time.Duration, error) {
	url, err := m.buildURL(naraID, "/ping")
	if err != nil {
		return 0, err
	}
	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("X-Nara-From", m.name.String())

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Drain body to reuse connection
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	return time.Since(start), nil
}

// RelayWorldMessage sends a world message to a peer via POST /world/relay
func (m *MeshClient) RelayWorldMessage(ctx context.Context, naraID types.NaraID, msg *WorldMessage) error {
	jsonBody, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal world message: %w", err)
	}

	url, err := m.buildURL(naraID, "/world/relay")
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	m.signRequest(req)

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}

	return nil
}

// SendDM sends a direct message to a peer via POST /dm
func (m *MeshClient) SendDM(ctx context.Context, naraID types.NaraID, dm interface{}) error {
	jsonBody, err := json.Marshal(dm)
	if err != nil {
		return fmt.Errorf("marshal DM: %w", err)
	}

	url, err := m.buildURL(naraID, "/dm")
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	m.signRequest(req)

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}

	return nil
}

// PostGossipZine sends a gossip zine to a peer via POST /gossip/zine
// Returns the response zine from the peer (for zine exchange)
func (m *MeshClient) PostGossipZine(ctx context.Context, naraID types.NaraID, zine interface{}) (*Zine, error) {
	jsonBody, err := json.Marshal(zine)
	if err != nil {
		return nil, fmt.Errorf("marshal zine: %w", err)
	}

	url, err := m.buildURL(naraID, "/gossip/zine")
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	m.signRequest(req)

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	// Decode response zine
	var responseZine Zine
	if err := json.NewDecoder(resp.Body).Decode(&responseZine); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &responseZine, nil
}

// PingIP sends a ping request to a nara via mesh IP and measures round-trip time.
// This is used for special cases like Vivaldi coordinate updates where we have the mesh IP directly.
// For general use, prefer PingNara which works with NaraIDs.
func (m *MeshClient) PingIP(ctx context.Context, meshIP string) (time.Duration, error) {
	if meshIP == "" {
		return 0, fmt.Errorf("mesh IP is required")
	}

	// Build the ping URL (using mesh IP and mesh port)
	url := fmt.Sprintf("http://%s:%d/ping", meshIP, DefaultMeshPort)

	// Create request with X-Nara-From header
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create ping request: %w", err)
	}
	req.Header.Set("X-Nara-From", m.name.String())

	start := time.Now()
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("ping failed: %w", err)
	}
	rtt := time.Since(start)
	defer resp.Body.Close()

	// Drain body to reuse connection
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("ping returned status %d", resp.StatusCode)
	}

	return rtt, nil
}

// Note: Additional mesh client methods can be added as needed.
// Currently implemented: FetchSyncEvents, FetchCheckpoints, Ping, RelayWorldMessage, SendDM, PostGossipZine
// Sufficient for boot recovery, checkpoint sync, backup tool, world journeys, and coordinates.
