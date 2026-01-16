package nara

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eljojo/nara/types"
)

// TestCoordinator manages test infrastructure and nara connections.
// It replicates real-world mesh networking in a test environment.
type TestCoordinator struct {
	t          *testing.T
	naras      map[string]*LocalNara
	servers    map[string]*httptest.Server
	muxes      map[string]*http.ServeMux
	httpClient *http.Client
}

// NewTestCoordinator creates a new test coordinator.
func NewTestCoordinator(t *testing.T) *TestCoordinator {
	tc := &TestCoordinator{
		t:          t,
		naras:      make(map[string]*LocalNara),
		servers:    make(map[string]*httptest.Server),
		muxes:      make(map[string]*http.ServeMux),
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	t.Cleanup(func() {
		for _, server := range tc.servers {
			server.Close()
		}
	})

	return tc
}

// CoordinatorNaraOption configures a nara during creation by the coordinator.
type CoordinatorNaraOption func(*naraConfig)

type naraConfig struct {
	soul            string
	chattiness      int
	ledgerCapacity  int
	withServer      bool
	handlers        []string
	notBooting      bool // Mark as not booting (common test requirement)
}

// WithStandardHandlers adds all common handlers (gossip, dm, world, peer query, checkpoints).
func WithStandardHandlers() CoordinatorNaraOption {
	return func(c *naraConfig) {
		c.withServer = true
		c.handlers = []string{"gossip", "dm", "world", "peer", "checkpoints"}
	}
}

// WithHandlers adds specific handlers. Options: "gossip", "dm", "world", "peer", "checkpoints".
func WithHandlers(handlers ...string) CoordinatorNaraOption {
	return func(c *naraConfig) {
		c.withServer = true
		c.handlers = handlers
	}
}

// WithoutServer creates a nara without an HTTP server (for MQTT-only tests).
func WithoutServer() CoordinatorNaraOption {
	return func(c *naraConfig) {
		c.withServer = false
	}
}

// WithCoordinatorSoul sets a custom soul string.
func WithCoordinatorSoul(soul string) CoordinatorNaraOption {
	return func(c *naraConfig) {
		c.soul = soul
	}
}

// WithCoordinatorParams sets chattiness and ledger capacity.
func WithCoordinatorParams(chattiness, ledgerCapacity int) CoordinatorNaraOption {
	return func(c *naraConfig) {
		c.chattiness = chattiness
		c.ledgerCapacity = ledgerCapacity
	}
}

// NotBooting marks the nara as not booting (sets LastRestart in the past).
func NotBooting() CoordinatorNaraOption {
	return func(c *naraConfig) {
		c.notBooting = true
	}
}

// AddNara creates a nara with the given options and registers it with the coordinator.
func (tc *TestCoordinator) AddNara(name string, opts ...CoordinatorNaraOption) *LocalNara {
	config := &naraConfig{
		chattiness:     50,
		ledgerCapacity: 1000,
		withServer:     true, // default to server enabled
		handlers:       []string{"gossip", "dm", "world", "peer", "checkpoints"}, // default all
	}

	for _, opt := range opts {
		opt(config)
	}

	// Create nara
	var ln *LocalNara
	if config.soul != "" {
		ln = testLocalNaraWithSoulAndParams(tc.t, name, config.soul, config.chattiness, config.ledgerCapacity)
	} else {
		ln = testLocalNaraWithParams(tc.t, name, config.chattiness, config.ledgerCapacity)
	}

	// Mark as not booting if requested
	if config.notBooting {
		me := ln.getMeObservation()
		me.LastRestart = time.Now().Unix() - 200
		me.LastSeen = time.Now().Unix()
		ln.setMeObservation(me)
	}

	// Set up HTTP server if requested
	if config.withServer {
		mux := http.NewServeMux()

		// Register handlers based on config
		for _, handler := range config.handlers {
			switch handler {
			case "gossip":
				mux.HandleFunc("/gossip/zine", ln.Network.httpGossipZineHandler)
			case "dm":
				mux.HandleFunc("/dm", ln.Network.httpDMHandler)
			case "world":
				mux.HandleFunc("/world/relay", ln.Network.httpWorldRelayHandler)
			case "peer":
				mux.HandleFunc("/peer/query", ln.Network.httpPeerQueryHandler)
			case "checkpoints":
				mux.HandleFunc("/api/checkpoints/all", ln.Network.httpCheckpointsAllHandler)
			}
		}

		server := httptest.NewServer(mux)
		tc.servers[name] = server
		tc.muxes[name] = mux

		// Configure test HTTP client
		ln.Network.testHTTPClient = tc.httpClient
		ln.Network.testMeshURLs = make(map[types.NaraName]string)
	}

	tc.naras[name] = ln
	return ln
}

// Connect establishes a bidirectional connection between two naras.
// Each nara will know about the other and have routing configured.
func (tc *TestCoordinator) Connect(name1, name2 string) {
	nara1 := tc.naras[name1]
	nara2 := tc.naras[name2]

	if nara1 == nil || nara2 == nil {
		tc.t.Fatalf("Connect: both naras must exist (have: %s=%v, %s=%v)", name1, nara1 != nil, name2, nara2 != nil)
	}

	// nara1 -> nara2
	tc.connectOneWay(name1, name2)

	// nara2 -> nara1
	tc.connectOneWay(name2, name1)
}

// connectOneWay sets up one-directional routing from source to target.
func (tc *TestCoordinator) connectOneWay(sourceName, targetName string) {
	source := tc.naras[sourceName]
	target := tc.naras[targetName]

	if source == nil {
		tc.t.Fatalf("connectOneWay: source nara %s not found", sourceName)
	}
	if target == nil {
		tc.t.Fatalf("connectOneWay: target nara %s not found", targetName)
	}

	// Import target into source's neighbourhood
	targetNara := NewNara(types.NaraName(targetName))
	targetNara.ID = target.ID
	targetNara.Status.ID = target.ID
	targetNara.Status.PublicKey = FormatPublicKey(target.Keypair.PublicKey)
	source.Network.importNara(targetNara)

	// Set up routing if target has a server
	if server, ok := tc.servers[targetName]; ok {
		// Configure name-based routing (for legacy code)
		if source.Network.testMeshURLs == nil {
			source.Network.testMeshURLs = make(map[types.NaraName]string)
		}
		source.Network.testMeshURLs[types.NaraName(targetName)] = server.URL

		// Configure ID-based routing (for MeshClient)
		testURLs := make(map[types.NaraID]string)
		// Preserve existing test URLs
		if len(source.Network.meshClient.testURLs) > 0 {
			for id, url := range source.Network.meshClient.testURLs {
				testURLs[id] = url
			}
		}
		testURLs[target.ID] = server.URL
		source.Network.meshClient.EnableTestMode(testURLs)
	}
	// If target doesn't have a server, we still import them (they're known but unreachable)
}

// ConnectAll creates a full mesh where every nara knows every other nara.
func (tc *TestCoordinator) ConnectAll() {
	names := make([]string, 0, len(tc.naras))
	for name := range tc.naras {
		names = append(names, name)
	}

	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			tc.Connect(names[i], names[j])
		}
	}
}

// SetOnline marks a nara as online in another nara's observations.
func (tc *TestCoordinator) SetOnline(observerName, targetName string) {
	observer := tc.naras[observerName]
	if observer == nil {
		tc.t.Fatalf("SetOnline: observer %s not found", observerName)
	}
	observer.setObservation(types.NaraName(targetName), NaraObservation{Online: "ONLINE"})
}

// Get returns the LocalNara with the given name.
func (tc *TestCoordinator) Get(name string) *LocalNara {
	return tc.naras[name]
}

// GetMux returns the HTTP mux for a nara, allowing tests to add custom handlers.
func (tc *TestCoordinator) GetMux(name string) *http.ServeMux {
	return tc.muxes[name]
}

// GetServerURL returns the test server URL for a nara.
func (tc *TestCoordinator) GetServerURL(name string) string {
	if server, ok := tc.servers[name]; ok {
		return server.URL
	}
	return ""
}
