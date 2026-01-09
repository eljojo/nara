package nara

import (
	"bytes"
	"context"
	"encoding/json"
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
)

// TsnetPeer represents a peer discovered from the tsnet Status API
type TsnetPeer struct {
	Name string // Hostname (e.g., "blue-jay")
	IP   string // Tailscale IP (e.g., "100.64.0.93")
}

// MeshTransport defines the interface for mesh network communication
type MeshTransport interface {
	// Send sends a message to a specific nara
	Send(target string, msg *WorldMessage) error
	// Receive returns a channel for incoming world messages
	// Note: With HTTP transport, messages come via HTTP handler instead
	Receive() <-chan *WorldMessage
	// Close shuts down the transport
	Close() error
}

// HTTPMeshTransport implements MeshTransport using HTTP over tsnet
// This is the preferred transport - unified with other mesh HTTP endpoints
type HTTPMeshTransport struct {
	tsnetServer *tsnet.Server
	network     *Network // For auth headers
	port        int
	inbox       chan *WorldMessage // Not used with HTTP (handler calls HandleIncoming directly)
	closed      bool
	mu          sync.Mutex
}

// NewHTTPMeshTransport creates a new HTTP-based mesh transport
func NewHTTPMeshTransport(tsnetServer *tsnet.Server, network *Network, port int) *HTTPMeshTransport {
	return &HTTPMeshTransport{
		tsnetServer: tsnetServer,
		network:     network,
		port:        port,
		inbox:       make(chan *WorldMessage, 10), // Small buffer, not really used
	}
}

// Send sends a world message to another nara via HTTP POST
func (t *HTTPMeshTransport) Send(target string, msg *WorldMessage) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return errors.New("transport closed")
	}
	t.mu.Unlock()

	// Marshal the world message
	jsonBody, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal world message: %w", err)
	}

	// Build request
	url := fmt.Sprintf("http://%s:%d/world/relay", target, t.port)
	req, err := http.NewRequest("POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Add mesh authentication headers
	if t.network != nil {
		t.network.AddMeshAuthHeaders(req)
	}

	// Use tsnet HTTP client for routing through Tailscale
	client := t.tsnetServer.HTTPClient()
	client.Timeout = 30 * time.Second

	logrus.Infof("🌍 Sending world message to %s via HTTP", target)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send world message to %s: %w", target, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("world relay to %s failed with status %d: %s", target, resp.StatusCode, string(body))
	}

	logrus.Infof("🌍 World message sent to %s successfully", target)
	return nil
}

// Receive returns the channel for incoming world messages
// Note: With HTTP transport, messages are handled directly by httpWorldRelayHandler
// This channel is kept for interface compatibility but not actively used
func (t *HTTPMeshTransport) Receive() <-chan *WorldMessage {
	return t.inbox
}

// Close shuts down the transport
func (t *HTTPMeshTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.closed {
		t.closed = true
		close(t.inbox)
	}
	return nil
}

// MockMeshNetwork simulates a mesh network for testing
// All MockMeshTransports connected to the same MockMeshNetwork can communicate
type MockMeshNetwork struct {
	mu         sync.RWMutex
	transports map[string]*MockMeshTransport
}

// NewMockMeshNetwork creates a new mock mesh network
func NewMockMeshNetwork() *MockMeshNetwork {
	return &MockMeshNetwork{
		transports: make(map[string]*MockMeshTransport),
	}
}

// Register adds a transport to the network
func (n *MockMeshNetwork) Register(name string, t *MockMeshTransport) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.transports[name] = t
	t.network = n
	t.name = name
}

// Unregister removes a transport from the network
func (n *MockMeshNetwork) Unregister(name string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.transports, name)
}

// GetTransport returns a transport by name
func (n *MockMeshNetwork) GetTransport(name string) (*MockMeshTransport, bool) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	t, ok := n.transports[name]
	return t, ok
}

// MockMeshTransport is a mock implementation of MeshTransport for testing
type MockMeshTransport struct {
	name    string
	network *MockMeshNetwork
	inbox   chan *WorldMessage
	closed  bool
	mu      sync.Mutex
}

// NewMockMeshTransport creates a new mock mesh transport
func NewMockMeshTransport() *MockMeshTransport {
	return &MockMeshTransport{
		inbox: make(chan *WorldMessage, 100),
	}
}

// Send sends a message to a target nara through the mock network
func (t *MockMeshTransport) Send(target string, msg *WorldMessage) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return errors.New("transport closed")
	}
	t.mu.Unlock()

	if t.network == nil {
		return errors.New("transport not connected to network")
	}

	targetTransport, ok := t.network.GetTransport(target)
	if !ok {
		return errors.New("target not found: " + target)
	}

	// Clone the message to simulate network serialization
	msgCopy := cloneWorldMessage(msg)

	select {
	case targetTransport.inbox <- msgCopy:
		return nil
	default:
		return errors.New("target inbox full")
	}
}

// Receive returns the channel for incoming messages
func (t *MockMeshTransport) Receive() <-chan *WorldMessage {
	return t.inbox
}

// Close shuts down the transport
func (t *MockMeshTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.closed {
		t.closed = true
		close(t.inbox)
	}
	return nil
}

// cloneWorldMessage creates a deep copy of a WorldMessage (simulates serialization)
func cloneWorldMessage(msg *WorldMessage) *WorldMessage {
	data, _ := json.Marshal(msg)
	var copy WorldMessage
	json.Unmarshal(data, &copy)
	return &copy
}

// TsnetMesh implements peer-to-peer communication using Tailscale's tsnet
// World messages are sent via HTTP (using HTTPMeshTransport), not raw TCP
type TsnetMesh struct {
	server     *tsnet.Server // Exported via Server() method for HTTP client access
	inbox      chan *WorldMessage
	closed     bool
	mu         sync.Mutex
	myName     string
	myIP       string // Our tailscale IP (for broadcasting to others)
	port       int
	stateStore *mem.Store // In-memory state store (no disk writes)
}

// Server returns the underlying tsnet.Server for HTTP client access
func (t *TsnetMesh) Server() *tsnet.Server {
	return t.server
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
	suffix := randomSuffix(4)
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

	lc, err := t.server.LocalClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get local client: %w", err)
	}

	status, err := lc.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get status: %w", err)
	}

	var peers []TsnetPeer
	for _, peer := range status.Peer {
		// Skip peers without IPs
		if len(peer.TailscaleIPs) == 0 {
			continue
		}
		// Use hostname as initial name - will be replaced with real name from /ping
		// The Tailscale hostname may have a random suffix, but that's fine since
		// fetchPublicKeysFromPeers will get the real name when it pings
		name := peer.HostName
		if name == "" {
			continue
		}
		peers = append(peers, TsnetPeer{
			Name: name,
			IP:   peer.TailscaleIPs[0].String(),
		})
	}

	logrus.Debugf("📡 Got %d peers from tsnet Status API", len(peers))
	return peers, nil
}

// Send is deprecated - world messages now use HTTP via HTTPMeshTransport
// This method returns an error as it's no longer supported
func (t *TsnetMesh) Send(target string, msg *WorldMessage) error {
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
func (t *TsnetMesh) Ping(targetIP string, from string, timeout time.Duration) (time.Duration, error) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return 0, errors.New("transport closed")
	}
	t.mu.Unlock()

	if targetIP == "" {
		return 0, errors.New("target IP is required")
	}

	// Use tsnet's HTTP client
	client := t.server.HTTPClient()
	client.Timeout = timeout

	// Build the ping URL (using mesh IP and mesh port)
	url := fmt.Sprintf("http://%s:%d/ping", targetIP, DefaultMeshPort)

	// Create request with X-Nara-From header
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create ping request: %w", err)
	}
	req.Header.Set("X-Nara-From", from)

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("ping failed: %w", err)
	}
	rtt := time.Since(start)
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("ping returned status %d", resp.StatusCode)
	}

	return rtt, nil
}
