package nara

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"tailscale.com/ipn/store/mem"
	"tailscale.com/tsnet"
)

// MeshTransport defines the interface for mesh network communication
type MeshTransport interface {
	// Send sends a message to a specific nara
	Send(target string, msg *WorldMessage) error
	// Receive returns a channel for incoming world messages
	Receive() <-chan *WorldMessage
	// Close shuts down the transport
	Close() error
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

// MeshConnection wraps a net.Conn for mesh protocol communication
type MeshConnection struct {
	conn    net.Conn
	encoder *json.Encoder
	decoder *json.Decoder
}

// NewMeshConnection wraps a connection for mesh communication
func NewMeshConnection(conn net.Conn) *MeshConnection {
	return &MeshConnection{
		conn:    conn,
		encoder: json.NewEncoder(conn),
		decoder: json.NewDecoder(conn),
	}
}

// SendWorldMessage sends a world message over the connection
func (mc *MeshConnection) SendWorldMessage(msg *WorldMessage) error {
	return mc.encoder.Encode(msg)
}

// ReceiveWorldMessage receives a world message from the connection
func (mc *MeshConnection) ReceiveWorldMessage() (*WorldMessage, error) {
	var msg WorldMessage
	if err := mc.decoder.Decode(&msg); err != nil {
		if err == io.EOF {
			return nil, err
		}
		return nil, err
	}
	return &msg, nil
}

// Close closes the underlying connection
func (mc *MeshConnection) Close() error {
	return mc.conn.Close()
}

// TsnetMesh implements MeshTransport using Tailscale's tsnet for real peer-to-peer communication
type TsnetMesh struct {
	server     *tsnet.Server
	listener   net.Listener
	inbox      chan *WorldMessage
	closed     bool
	mu         sync.Mutex
	myName     string
	port       int
	stateStore *mem.Store // In-memory state store (no disk writes)
}

// TsnetConfig holds configuration for creating a TsnetMesh
type TsnetConfig struct {
	Hostname   string // The nara's name (used as tsnet hostname)
	ControlURL string // Headscale server URL (e.g., https://vpn.nara.network)
	AuthKey    string // Pre-auth key for automatic registration
	StateDir   string // Directory for temp files like sockets (uses /tmp, no state written)
	Port       int    // Port to listen on for world messages (default: 7433)
}

// NewTsnetMesh creates a new tsnet-based mesh transport
func NewTsnetMesh(config TsnetConfig) (*TsnetMesh, error) {
	if config.Hostname == "" {
		return nil, errors.New("hostname is required")
	}
	if config.ControlURL == "" {
		return nil, errors.New("control URL is required")
	}

	// Default temp directory for sockets and other temp files
	// State itself is kept in memory via mem.Store
	if config.StateDir == "" {
		config.StateDir = filepath.Join(os.TempDir(), "nara-tsnet-"+config.Hostname)
	}

	// Default port
	if config.Port == 0 {
		config.Port = 7433 // NARA on phone keypad :)
	}

	// Create temp directory (needed for unix sockets, etc.)
	if err := os.MkdirAll(config.StateDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Use in-memory state store - no disk writes for state
	stateStore := new(mem.Store)

	server := &tsnet.Server{
		Hostname:   config.Hostname,
		ControlURL: config.ControlURL,
		AuthKey:    config.AuthKey,
		Dir:        config.StateDir,
		Store:      stateStore, // In-memory state, no disk persistence
		Ephemeral:  true,       // Node removed from tailnet when it disconnects
		Logf:       func(format string, args ...any) { logrus.Debugf("[tsnet] "+format, args...) },
	}

	mesh := &TsnetMesh{
		server:     server,
		inbox:      make(chan *WorldMessage, 100),
		myName:     config.Hostname,
		port:       config.Port,
		stateStore: stateStore,
	}

	return mesh, nil
}

// Start initializes the tsnet server and starts listening for connections
func (t *TsnetMesh) Start(ctx context.Context) error {
	logrus.Infof("Starting tsnet mesh for %s...", t.myName)

	// Start the tsnet server
	status, err := t.server.Up(ctx)
	if err != nil {
		return fmt.Errorf("failed to start tsnet: %w", err)
	}

	logrus.Infof("Tsnet connected! IP: %v", status.TailscaleIPs)

	// Start listening for incoming connections
	listener, err := t.server.Listen("tcp", fmt.Sprintf(":%d", t.port))
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	t.listener = listener

	logrus.Infof("Listening for world messages on port %d", t.port)

	// Accept connections in background
	go t.acceptConnections()

	return nil
}

// acceptConnections handles incoming connections
func (t *TsnetMesh) acceptConnections() {
	for {
		conn, err := t.listener.Accept()
		if err != nil {
			t.mu.Lock()
			closed := t.closed
			t.mu.Unlock()
			if closed {
				return
			}
			logrus.Warnf("Accept error: %v", err)
			continue
		}

		go t.handleConnection(conn)
	}
}

// handleConnection processes an incoming connection
func (t *TsnetMesh) handleConnection(conn net.Conn) {
	defer conn.Close()

	mc := NewMeshConnection(conn)
	msg, err := mc.ReceiveWorldMessage()
	if err != nil {
		if err != io.EOF {
			logrus.Warnf("Failed to receive world message: %v", err)
		}
		return
	}

	t.mu.Lock()
	closed := t.closed
	t.mu.Unlock()

	if closed {
		return
	}

	select {
	case t.inbox <- msg:
	default:
		logrus.Warn("World message inbox full, dropping message")
	}
}

// Send sends a world message to another nara via tsnet
func (t *TsnetMesh) Send(target string, msg *WorldMessage) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return errors.New("transport closed")
	}
	t.mu.Unlock()

	// Dial the target nara
	// tsnet hostnames are just the hostname part, tsnet handles the domain
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := t.server.Dial(ctx, "tcp", fmt.Sprintf("%s:%d", target, t.port))
	if err != nil {
		return fmt.Errorf("failed to dial %s: %w", target, err)
	}
	defer conn.Close()

	mc := NewMeshConnection(conn)
	if err := mc.SendWorldMessage(msg); err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
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

	if t.listener != nil {
		t.listener.Close()
	}

	close(t.inbox)

	if t.server != nil {
		return t.server.Close()
	}

	return nil
}
