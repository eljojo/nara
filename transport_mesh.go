package nara

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"tailscale.com/ipn/store/mem"
	"tailscale.com/tsnet"

	"github.com/eljojo/nara/types"
)

// TsnetPeer represents a peer discovered from the tsnet Status API
type TsnetPeer struct {
	Name string // Hostname (e.g., "blue-jay")
	IP   string // Tailscale IP (e.g., "100.64.0.93")
}

// TsnetMesh implements peer-to-peer communication using Tailscale's tsnet
// World messages are sent via MeshClient, not direct HTTP
type TsnetMesh struct {
	server     *tsnet.Server // Exported via Server() method for HTTP client access
	inbox      chan *WorldMessage
	closed     bool
	mu         sync.Mutex
	myName     string
	myIP       string // Our tailscale IP (for broadcasting to others)
	port       int
	stateStore *mem.Store // In-memory state store (no disk writes)
	httpClient *http.Client
}

// Server returns the underlying tsnet.Server for HTTP client access
func (t *TsnetMesh) Server() *tsnet.Server {
	return t.server
}

// SetHTTPClient allows sharing a single mesh HTTP client/transport across requests.
func (t *TsnetMesh) SetHTTPClient(client *http.Client) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.httpClient = client
}

// TsnetConfig holds configuration for creating a TsnetMesh
type TsnetConfig struct {
	Hostname   string // The nara's name (used as tsnet hostname)
	ControlURL string // Headscale server URL (e.g., https://vpn.nara.network)
	AuthKey    string // Pre-auth key for automatic registration
	StateDir   string // Directory for temp files like sockets (uses /tmp, no state written)
	Port       int    // Port to listen on for world messages (default: DefaultMeshPort)
	Verbose    bool   // Enable verbose Tailscale logging (use -vv flag)
}

// NewTsnetMesh creates a new tsnet-based mesh transport
func NewTsnetMesh(config TsnetConfig) (*TsnetMesh, error) {
	if config.Hostname == "" {
		return nil, errors.New("hostname is required")
	}
	if config.ControlURL == "" {
		return nil, errors.New("control URL is required")
	}

	// Append random suffix to hostname to avoid conflicts with stale registrations
	// This helps when restarting quickly or when previous instances didn't clean up
	suffix := randomSuffix(4) // keep this in sync with gossip_discovery.go
	tsnetHostname := config.Hostname + "-" + suffix
	logrus.Debugf("🌐 Tailscale hostname: %s (base: %s)", tsnetHostname, config.Hostname)

	// Default temp directory for sockets and other temp files
	// State itself is kept in memory via mem.Store
	if config.StateDir == "" {
		config.StateDir = filepath.Join(os.TempDir(), "nara-tsnet-"+tsnetHostname)
	}

	// Default port
	if config.Port == 0 {
		config.Port = DefaultMeshPort
	}

	// Create temp directory (needed for unix sockets, etc.)
	if err := os.MkdirAll(config.StateDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Use in-memory state store - no disk writes for state
	stateStore := new(mem.Store)

	// Tailscale logging: OFF by default, enabled only with -vv flag
	tsnetLogf := func(format string, args ...any) {} // no-op by default
	if config.Verbose {
		tsnetLogf = func(format string, args ...any) { logrus.Debugf("[tsnet] "+format, args...) }
	}

	server := &tsnet.Server{
		Hostname:   tsnetHostname,
		ControlURL: config.ControlURL,
		AuthKey:    config.AuthKey,
		Dir:        config.StateDir,
		Store:      stateStore, // In-memory state, no disk persistence
		Ephemeral:  true,       // Node removed from tailnet when it disconnects
		Logf:       tsnetLogf,
	}

	mesh := &TsnetMesh{
		server:     server,
		inbox:      make(chan *WorldMessage, 100),
		myName:     config.Hostname, // Keep the original name for nara identity
		port:       config.Port,
		stateStore: stateStore,
	}

	return mesh, nil
}

// randomSuffix generates a random alphanumeric suffix of the given length
func randomSuffix(length int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, length)
	for i := range result {
		result[i] = chars[rand.Intn(len(chars))]
	}
	return string(result)
}

// Start initializes the tsnet server and starts listening for connections
func (t *TsnetMesh) Start(ctx context.Context) error {
	logrus.Infof("Starting tsnet mesh for %s...", t.myName)

	// Start the tsnet server
	status, err := t.server.Up(ctx)
	if err != nil {
		return fmt.Errorf("failed to start tsnet: %w", err)
	}

	// Store our tailscale IP for broadcasting to others
	if len(status.TailscaleIPs) > 0 {
		t.myIP = status.TailscaleIPs[0].String()
	}
	logrus.Infof("Tsnet connected! IP: %s", t.myIP)

	// Note: HTTP server will be started separately via startMeshHttpServer()
	// No need to listen for raw TCP connections - world messages use HTTP now

	return nil
}

// IP returns the tailscale IP address of this mesh node
func (t *TsnetMesh) IP() string {
	return t.myIP
}

// Peers returns the list of peers from the tsnet Status API
// This is much faster than scanning IPs since tsnet already knows its peers
func (t *TsnetMesh) Peers(ctx context.Context) ([]TsnetPeer, error) {
	if t.server == nil {
		return nil, errors.New("tsnet server not initialized")
	}

	// Use the centralized DiscoverMeshPeers helper (defined in mesh_client.go)
	// This ensures consistent peer filtering across the codebase
	return DiscoverMeshPeers(ctx, t.server)
}

// Send is deprecated - world messages now use HTTP via HTTPMeshTransport
// This method returns an error as it's no longer supported
func (t *TsnetMesh) Send(target types.NaraName, msg *WorldMessage) error {
	return errors.New("deprecated: TsnetMesh.Send() no longer supported - use HTTPMeshTransport instead")
}

// Receive returns the channel for incoming world messages
func (t *TsnetMesh) Receive() <-chan *WorldMessage {
	return t.inbox
}

// Close shuts down the tsnet mesh
func (t *TsnetMesh) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}
	t.closed = true

	close(t.inbox)

	if t.server != nil {
		return t.server.Close()
	}

	return nil
}

// Ping measures RTT to a peer nara via the mesh network
// Uses HTTP GET to the /ping endpoint and returns the round-trip time
// Note: TCP handshake time is included, which is correct for Vivaldi coordinates
// (the handshake itself measures one network RTT)
// TODO: we need to unify this with MeshClient somehow
func (t *TsnetMesh) Ping(targetIP string, from types.NaraName, timeout time.Duration) (time.Duration, error) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return 0, errors.New("transport closed")
	}
	client := t.httpClient
	t.mu.Unlock()

	if targetIP == "" {
		return 0, errors.New("target IP is required")
	}

	// Use tsnet's HTTP client
	if client == nil {
		client = t.server.HTTPClient()
	}

	// Build the ping URL (using mesh IP and mesh port)
	url := fmt.Sprintf("http://%s:%d/ping", targetIP, DefaultMeshPort)

	// Create request with X-Nara-From header
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create ping request: %w", err)
	}
	req.Header.Set("X-Nara-From", from.String())

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("ping failed: %w", err)
	}
	rtt := time.Since(start)
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		logrus.WithError(err).Warn("Failed to discard response body")
	}
	resp.Body.Close()

	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("ping returned status %d", resp.StatusCode)
	}

	return rtt, nil
}
