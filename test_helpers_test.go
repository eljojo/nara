package nara

import (
	"crypto/sha256"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/eljojo/nara/identity"
	"github.com/eljojo/nara/types"
	mqttserver "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/sirupsen/logrus"
)

// testSoul generates a valid soul for testing given a name.
// This ensures all test naras have valid keypairs for signing.
func testSoul(name string) string {
	hw := hashTestBytes([]byte("test-hardware-" + name))
	soul := identity.NativeSoulCustom(hw, types.NaraName(name))
	return identity.FormatSoul(soul)
}

func hashTestBytes(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}

// TestMain runs before all tests to set up global test configuration
func TestMain(m *testing.M) {
	OpinionRepeatOverride = 1
	OpinionIntervalOverride = 0

	// Set default log level to warnings and above for cleaner test output
	// This still shows warnings and errors, but suppresses info/debug logs
	// Individual tests can override this with logrus.SetLevel(logrus.DebugLevel)
	logrus.SetLevel(logrus.WarnLevel)

	// Explicitly initialize logrus to prevent nil pointer panics in parallel tests
	// This ensures the logger is fully set up before any parallel tests start
	logrus.SetOutput(os.Stderr)
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	// Run tests
	exitCode := m.Run()

	os.Exit(exitCode)
}

// TestNaraOptions configures test nara creation
type TestNaraOptions struct {
	mqttHost              string
	mqttPort              int
	chattiness            int
	ledgerCapacity        int
	soul                  string
	skipJitter            bool
	skipBootRecovery      bool
	skipHeyThereRateLimit bool
}

// TestNaraOption is a functional option for configuring test naras
type TestNaraOption func(*TestNaraOptions)

// WithMQTT configures MQTT connection
func WithMQTT(port int) TestNaraOption {
	return func(o *TestNaraOptions) {
		o.mqttHost = fmt.Sprintf("tcp://127.0.0.1:%d", port)
		o.mqttPort = port
		o.skipJitter = true
		o.skipBootRecovery = true
	}
}

// WithParams sets custom chattiness and ledger capacity
func WithParams(chattiness, ledgerCapacity int) TestNaraOption {
	return func(o *TestNaraOptions) {
		o.chattiness = chattiness
		o.ledgerCapacity = ledgerCapacity
	}
}

// WithSoul sets a custom soul string
func WithSoul(soul string) TestNaraOption {
	return func(o *TestNaraOptions) {
		o.soul = soul
	}
}

// WithHowdyTestConfig enables all flags needed for howdy tests
func WithHowdyTestConfig() TestNaraOption {
	return func(o *TestNaraOptions) {
		o.skipJitter = true
		o.skipHeyThereRateLimit = true
	}
}

// testNara creates a LocalNara for testing with optional configuration.
// Automatically registers cleanup via t.Cleanup() to ensure proper shutdown.
// This is the unified test helper - use this for all test nara creation.
func testNara(t *testing.T, name string, opts ...TestNaraOption) *LocalNara {
	t.Helper()

	// Apply options
	config := &TestNaraOptions{
		mqttHost:       "host",
		chattiness:     -1,
		ledgerCapacity: 0,
	}
	for _, opt := range opts {
		opt(config)
	}

	// Create identity
	var identityResult identity.IdentityResult
	if config.soul != "" {
		parsed, _ := identity.ParseSoul(config.soul)
		id, _ := identity.ComputeNaraID(config.soul, types.NaraName(name))
		identityResult = identity.IdentityResult{
			Name:        types.NaraName(name),
			Soul:        parsed,
			ID:          id,
			IsValidBond: true,
			IsNative:    true,
		}
	} else {
		identityResult = testIdentity(name)
	}

	// Create memory profile
	profile := DefaultMemoryProfile()
	if config.ledgerCapacity > 0 {
		profile.Mode = MemoryModeCustom
		profile.MaxEvents = config.ledgerCapacity
	}

	// Create LocalNara
	ln, err := NewLocalNara(identityResult, config.mqttHost, "", "", config.chattiness, profile)
	if err != nil {
		panic(err)
	}

	// Apply test configurations
	delay := time.Duration(0)
	ln.Network.testObservationDelay = &delay
	ln.Network.testSkipCoordinateWait = true // Skip 30s wait in coordinateMaintenance for faster tests
	if config.skipJitter {
		ln.Network.testSkipJitter = true
	}
	if config.skipBootRecovery {
		ln.Network.testSkipBootRecovery = true
	}
	if config.skipHeyThereRateLimit {
		ln.Network.testSkipHeyThereSleep = true
		ln.Network.testSkipHeyThereRateLimit = true
	}

	// Automatic cleanup
	t.Cleanup(func() {
		ln.Network.Shutdown()
		if config.mqttPort > 0 {
			ln.Network.disconnectMQTT()
		}
	})

	return ln
}

// testLocalNara creates a basic LocalNara for testing.
// Deprecated: Use testNara(t, name) instead.
func testLocalNara(t *testing.T, name string) *LocalNara {
	return testNara(t, name)
}

// testLocalNaraWithParams creates a LocalNara for testing with specific chattiness and ledger capacity.
// Deprecated: Use testNara(t, name, WithParams(chattiness, ledgerCapacity)) instead.
func testLocalNaraWithParams(t *testing.T, name string, chattiness int, ledgerCapacity int) *LocalNara {
	return testNara(t, name, WithParams(chattiness, ledgerCapacity))
}

// testLocalNaraWithSoul creates a LocalNara for testing with a specific soul string.
// Deprecated: Use testNara(t, name, WithSoul(soul)) instead.
func testLocalNaraWithSoul(t *testing.T, name string, soul string) *LocalNara {
	return testNara(t, name, WithSoul(soul))
}

func testIdentity(name string) identity.IdentityResult {
	soulStr := testSoul(name)
	parsed, _ := identity.ParseSoul(soulStr)
	id, _ := identity.ComputeNaraID(soulStr, types.NaraName(name))
	return identity.IdentityResult{
		Name:        types.NaraName(name),
		Soul:        parsed,
		ID:          id,
		IsValidBond: true,
		IsNative:    true,
	}
}

// testLocalNaraWithSoulAndParams creates a LocalNara for testing with a specific soul and parameters.
// Deprecated: Use testNara(t, name, WithSoul(soul), WithParams(chattiness, ledgerCapacity)) instead.
func testLocalNaraWithSoulAndParams(t *testing.T, name string, soul string, chattiness int, ledgerCapacity int) *LocalNara {
	return testNara(t, name, WithSoul(soul), WithParams(chattiness, ledgerCapacity))
}

// getFreePort returns an available TCP port by binding to :0 and releasing it.
// This allows tests to run in parallel without port conflicts.
func getFreePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to get free port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port
}

// startTestMQTTBroker starts an embedded MQTT broker for testing on the given port.
// Returns the broker server which should be closed with defer broker.Close() after the test.
func startTestMQTTBroker(t *testing.T, port int) *mqttserver.Server {
	t.Helper()
	server := mqttserver.New(nil)

	err := server.AddHook(new(auth.AllowHook), nil)
	if err != nil {
		t.Fatalf("Failed to add auth hook to MQTT broker: %v", err)
	}

	tcp := listeners.NewTCP(listeners.Config{
		ID:      fmt.Sprintf("test-broker-%d", port),
		Address: fmt.Sprintf(":%d", port),
	})
	err = server.AddListener(tcp)
	if err != nil {
		t.Fatalf("Failed to add listener to MQTT broker: %v", err)
	}

	go func() {
		err := server.Serve()
		if err != nil {
			t.Logf("MQTT broker stopped: %v", err)
		}
	}()

	// Register cleanup
	t.Cleanup(func() {
		server.Close()
	})

	return server
}

// startTestMQTTBrokerDynamic starts an embedded MQTT broker on a dynamically allocated port.
// Returns the broker server and the port it's listening on.
// This is preferred for parallel tests to avoid port conflicts.
func startTestMQTTBrokerDynamic(t *testing.T) (*mqttserver.Server, int) {
	t.Helper()
	port := getFreePort(t)
	broker := startTestMQTTBroker(t, port)
	// Small delay to ensure broker is ready
	time.Sleep(50 * time.Millisecond)
	return broker, port
}

// createTestNaraForMQTT creates a LocalNara for MQTT testing with proper test flags.
// Includes WithHowdyTestConfig() to bypass rate limits for faster tests.
// Deprecated: Use testNara(t, name, WithMQTT(port), WithParams(-1, 1000), WithHowdyTestConfig()) instead.
func createTestNaraForMQTT(t *testing.T, name string, port int) *LocalNara {
	return testNara(t, name, WithMQTT(port), WithParams(-1, 1000), WithHowdyTestConfig())
}

// startTestNaras creates and starts multiple naras, ensuring MQTT connection and full discovery.
// Automatically registers cleanup for all naras via t.Cleanup().
func startTestNaras(t *testing.T, port int, names []string, ensureDiscovery bool) []*LocalNara {
	t.Helper()
	naras := make([]*LocalNara, len(names))

	// Create all naras first
	// Note: cleanup is automatically registered by createTestNaraForMQTT() via testNara()
	for i, name := range names {
		naras[i] = createTestNaraForMQTT(t, name, port)
	}

	// Start all naras concurrently
	for _, ln := range naras {
		go ln.Start(false, false, "", nil, TransportMQTT)
	}

	// Wait for all to connect
	waitForAllMQTTConnected(t, naras, 15*time.Second)

	if ensureDiscovery {
		// Rate limit is bypassed via WithHowdyTestConfig() in createTestNaraForMQTT,
		// so we can trigger hey-there immediately without waiting 5 seconds.

		// Trigger hey-there to ensure all naras discover everyone
		for _, ln := range naras {
			ln.Network.heyThere()
		}

		// Small delay to allow hey-there messages to propagate via MQTT
		time.Sleep(200 * time.Millisecond)

		// Wait for full discovery with public keys
		waitForFullDiscovery(t, naras, 20*time.Second)
	}

	return naras
}

// waitForCondition polls until condition returns true or timeout expires.
// This is the base helper for all wait functions.
func waitForCondition(t *testing.T, condition func() bool, timeout time.Duration, description string) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// waitForMQTTConnected blocks until the nara's MQTT client is connected, or times out.
func waitForMQTTConnected(t *testing.T, ln *LocalNara, timeout time.Duration) {
	t.Helper()
	waitForAllMQTTConnected(t, []*LocalNara{ln}, timeout)
}

// waitForAllMQTTConnected blocks until all naras have MQTT clients connected, or times out.
func waitForAllMQTTConnected(t *testing.T, naras []*LocalNara, timeout time.Duration) {
	t.Helper()
	ok := waitForCondition(t, func() bool {
		for _, ln := range naras {
			if ln.Network.Mqtt == nil || !ln.Network.Mqtt.IsConnected() {
				return false
			}
		}
		return true
	}, timeout, "all MQTT connections established")
	if !ok {
		if len(naras) == 1 {
			t.Fatalf("Timed out waiting for %s MQTT to connect", naras[0].Me.Name)
		}
		t.Fatal("Timed out waiting for all naras to connect to MQTT")
	}
}

// waitForCheckpoint blocks until a checkpoint exists for the subject in the ledger, or times out.
// Returns the checkpoint if found, nil if timed out.
func waitForCheckpoint(t *testing.T, ledger *SyncLedger, subject types.NaraName, timeout time.Duration) *CheckpointEventPayload {
	t.Helper()
	var checkpoint *CheckpointEventPayload
	ok := waitForCondition(t, func() bool {
		checkpoint = ledger.GetCheckpoint(subject)
		return checkpoint != nil
	}, timeout, "checkpoint for "+subject.String())
	if ok {
		return checkpoint
	}
	return nil
}

// waitForCheckpointPropagation blocks until all naras have the checkpoint for a subject.
func waitForCheckpointPropagation(t *testing.T, naras []*LocalNara, subject types.NaraName, timeout time.Duration) bool {
	t.Helper()
	return waitForCondition(t, func() bool {
		for _, ln := range naras {
			if ln.SyncLedger.GetCheckpoint(subject) == nil {
				return false
			}
		}
		return true
	}, timeout, "checkpoint propagation")
}

// waitForFullDiscovery blocks until all naras have discovered each other with public keys,
// or times out. Each nara should know (numNaras - 1) neighbors, all with public keys.
func waitForFullDiscovery(t *testing.T, naras []*LocalNara, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	expectedNeighbors := len(naras) - 1

	for time.Now().Before(deadline) {
		allReady := true
		for _, ln := range naras {
			ln.Network.local.mu.Lock()
			neighborCount := len(ln.Network.Neighbourhood)
			keysKnown := 0
			for _, neighbor := range ln.Network.Neighbourhood {
				neighbor.mu.Lock()
				if neighbor.Status.PublicKey != "" {
					keysKnown++
				}
				neighbor.mu.Unlock()
			}
			ln.Network.local.mu.Unlock()

			if neighborCount < expectedNeighbors || keysKnown < expectedNeighbors {
				allReady = false
				break
			}
		}

		if allReady {
			// Log final state
			for _, ln := range naras {
				ln.Network.local.mu.Lock()
				neighborCount := len(ln.Network.Neighbourhood)
				keysKnown := 0
				for _, neighbor := range ln.Network.Neighbourhood {
					neighbor.mu.Lock()
					if neighbor.Status.PublicKey != "" {
						keysKnown++
					}
					neighbor.mu.Unlock()
				}
				ln.Network.local.mu.Unlock()
				t.Logf("  %s knows %d neighbors (%d with public keys)", ln.Me.Name, neighborCount, keysKnown)
			}
			return
		}

		time.Sleep(100 * time.Millisecond)
	}

	// Timeout - log current state and fail
	t.Log("⚠️  Discovery timed out, current state:")
	for _, ln := range naras {
		ln.Network.local.mu.Lock()
		neighborCount := len(ln.Network.Neighbourhood)
		keysKnown := 0
		for _, neighbor := range ln.Network.Neighbourhood {
			neighbor.mu.Lock()
			if neighbor.Status.PublicKey != "" {
				keysKnown++
			}
			neighbor.mu.Unlock()
		}
		ln.Network.local.mu.Unlock()
		t.Logf("  %s knows %d neighbors (%d with public keys)", ln.Me.Name, neighborCount, keysKnown)
	}
	t.Fatalf("Timed out waiting for full discovery (expected %d neighbors with keys)", expectedNeighbors)
}

// testCheckpointEvent creates a fully signed checkpoint event for testing.
// subject: the nara this checkpoint is about
// attester: the nara creating/signing this checkpoint
// attesterKeypair: the keypair to sign with
// observation: the checkpoint data (restarts, uptime, start_time)
func testCheckpointEvent(subject types.NaraName, attester types.NaraName, attesterKeypair identity.NaraKeypair, observation NaraObservation) SyncEvent {
	now := time.Now()

	checkpoint := &CheckpointEventPayload{
		Version:     1,
		Subject:     subject,
		SubjectID:   types.NaraID("test-id-" + subject.String()),
		Observation: observation,
		AsOfTime:    now.Unix(),
		Round:       1,
		VoterIDs:    []types.NaraID{types.NaraID("test-id-" + attester.String())},
	}

	// Create and sign attestation
	attestation := Attestation{
		Version:     checkpoint.Version,
		Subject:     checkpoint.Subject,
		SubjectID:   checkpoint.SubjectID,
		Observation: checkpoint.Observation,
		Attester:    attester,
		AttesterID:  types.NaraID("test-id-" + attester.String()),
		AsOfTime:    checkpoint.AsOfTime,
	}
	attestation.Signature = identity.SignContent(&attestation, attesterKeypair)
	checkpoint.Signatures = []string{attestation.Signature}

	// Create sync event
	event := SyncEvent{
		Timestamp:  now.UnixNano(),
		Service:    ServiceCheckpoint,
		Emitter:    attester,
		EmitterID:  types.NaraID("test-id-" + attester.String()),
		Checkpoint: checkpoint,
	}
	event.ComputeID()
	event.Sign(attester, attesterKeypair)

	return event
}

// testAddCheckpointToLedger creates and adds a checkpoint event to a ledger
func testAddCheckpointToLedger(ledger *SyncLedger, subject types.NaraName, attester types.NaraName, attesterKeypair identity.NaraKeypair, observation NaraObservation) SyncEvent {
	event := testCheckpointEvent(subject, attester, attesterKeypair, observation)
	// Manually add to ledger to avoid deduplication issues in tests
	ledger.mu.Lock()
	ledger.Events = append(ledger.Events, event)
	ledger.eventIDs[event.ID] = true
	ledger.mu.Unlock()
	return event
}

// =============================================================================
// Network Mesh Setup Helpers
// =============================================================================

// testMeshNetwork holds a mesh of interconnected test naras with HTTP servers.
type testMeshNetwork struct {
	Naras    []*LocalNara
	Servers  []*httptest.Server
	Client   *http.Client
	t        *testing.T
	MqttPort int
}

// testCreateMeshNetwork creates N naras in a full mesh topology with HTTP servers.
// Each nara knows all others and has testMeshURLs configured for HTTP communication.
// TODO: this api of having to pass mqttPortNumber is not good, the system should take care of that for us, should be a boolean true/false
func testCreateMeshNetwork(t *testing.T, names []string, chattiness, ledgerCapacity int, mqttPortNumber int) *testMeshNetwork {
	t.Helper()
	count := len(names)

	mesh := &testMeshNetwork{
		Naras:    make([]*LocalNara, count),
		Servers:  make([]*httptest.Server, count),
		Client:   &http.Client{Timeout: 5 * time.Second},
		t:        t,
		MqttPort: mqttPortNumber,
	}

	if mqttPortNumber > 0 {
		// Start a shared MQTT broker for the mesh
		startTestMQTTBroker(t, mqttPortNumber)
	}

	// Create naras and servers
	for i, name := range names {
		var ln *LocalNara
		if mqttPortNumber > 0 {
			ln = testNara(t, name, WithParams(chattiness, ledgerCapacity), WithMQTT(mqttPortNumber))
		} else {
			ln = testNara(t, name, WithParams(chattiness, ledgerCapacity))
		}

		// Mark not booting (common integration test requirement)
		me := ln.getMeObservation()
		me.LastRestart = time.Now().Unix() - 200
		me.LastSeen = time.Now().Unix()
		ln.setMeObservation(me)

		// Create HTTP server with common endpoints
		mux := ln.Network.createHTTPMux(false) // Use production mux logic
		server := httptest.NewServer(mux)

		mesh.Naras[i] = ln
		mesh.Servers[i] = server

		// Configure test hooks
		ln.Network.testHTTPClient = mesh.Client
		ln.Network.testMeshURLs = make(map[types.NaraName]string)
		if mqttPortNumber > 0 {
			ln.Network.TransportMode = TransportHybrid
		} else {
			ln.Network.TransportMode = TransportGossip
		}
	}

	// Connect everyone to everyone
	for i := 0; i < count; i++ {
		mesh.connectNodeToPeers(i)
	}

	// Register cleanup
	t.Cleanup(func() {
		for _, s := range mesh.Servers {
			s.Close()
		}
	})

	return mesh
}

// connectNodeToPeers wires the nara at the given index to all other naras in the mesh.
// It imports neighbor identities, sets initial online observations, and configures the mesh client.
func (m *testMeshNetwork) connectNodeToPeers(index int) {
	targetNara := m.Naras[index]
	testURLsForMeshClient := make(map[types.NaraID]string)

	for j := 0; j < len(m.Naras); j++ {
		if index != j {
			peer := m.Naras[j]
			peerServer := m.Servers[j]

			// Import peer identity and set connection info
			neighbor := NewNara(peer.Me.Name)
			neighbor.Status.ID = peer.Me.Status.ID
			neighbor.ID = peer.Me.Status.ID // Important: set top-level ID too
			neighbor.Status.PublicKey = identity.FormatPublicKey(peer.Keypair.PublicKey)

			targetNara.Network.importNara(neighbor)
			targetNara.setObservation(peer.Me.Name, NaraObservation{Online: "ONLINE"})
			targetNara.Network.testMeshURLs[peer.Me.Name] = peerServer.URL

			// Register for meshClient (outgoing requests)
			testURLsForMeshClient[peer.Me.Status.ID] = peerServer.URL
		}
	}

	targetNara.Network.meshClient.EnableTestMode(testURLsForMeshClient)
}

// Get returns the nara at index i.
func (m *testMeshNetwork) Get(i int) *LocalNara {
	return m.Naras[i]
}

// GetByName finds a nara by name.
func (m *testMeshNetwork) GetByName(name string) *LocalNara {
	for _, ln := range m.Naras {
		if ln.Me.Name == types.NaraName(name) {
			return ln
		}
	}
	m.t.Fatalf("No nara named %s in mesh", name)
	return nil
}

// ServerURL returns the HTTP server URL for the nara at index i.
func (m *testMeshNetwork) ServerURL(i int) string {
	return m.Servers[i].URL
}

// RestartNara simulates a nara restart by shutting down and recreating it with the same identity.
// This is useful for testing recovery scenarios where a nara loses all local state but keeps its soul.
// The restarted nara will have the same soul/identity but empty local state (no stash, no ledger).
// All mesh connections to other naras are automatically re-wired.
func (m *testMeshNetwork) RestartNara(index int) *LocalNara {
	m.t.Helper()

	if index < 0 || index >= len(m.Naras) {
		m.t.Fatalf("Invalid index %d, mesh has %d naras", index, len(m.Naras))
	}

	old := m.Naras[index]
	originalSoul := old.Soul
	originalName := string(old.Me.Name)

	// Shutdown the old nara
	old.Network.Shutdown()
	m.t.Logf("Stopped nara %s (index %d) for restart simulation", originalName, index)

	// Wait for graceful shutdown
	time.Sleep(200 * time.Millisecond)

	// Create fresh nara with same identity (simulates reboot with no local state)
	chattiness := old.forceChattiness
	if chattiness == 0 {
		chattiness = 50
	}
	ledgerCap := len(old.SyncLedger.Events)
	if ledgerCap == 0 {
		ledgerCap = 1000
	}

	fresh := testNara(m.t, originalName, WithSoul(originalSoul), WithParams(chattiness, ledgerCap), WithMQTT(m.MqttPort))

	// Mark as not booting (consistent with mesh setup)
	me := fresh.getMeObservation()
	me.LastRestart = time.Now().Unix() - 200
	me.LastSeen = time.Now().Unix()
	fresh.setMeObservation(me)

	// Configure test hooks for mesh
	fresh.Network.testHTTPClient = m.Client
	fresh.Network.testMeshURLs = make(map[types.NaraName]string)
	fresh.Network.TransportMode = TransportHybrid

	// Update mesh tracking - must do this BEFORE wiring peers so the loop sees the fresh nara at m.Naras[index]
	m.Naras[index] = fresh

	// Wire the fresh nara to all existing peers
	m.connectNodeToPeers(index)

	// Update HTTP server to use fresh nara's handlers
	// Note: We reuse the same httptest.Server, just update the handler
	if m.Servers[index].Config.Handler != nil {
		mux := fresh.Network.createHTTPMux(false) // Use production mux logic
		m.Servers[index].Config.Handler = mux
	}

	// Update other naras' view of this nara (they see it as same peer, just restarted)
	// We need to tell them about the "restart" event (LastRestart update)
	for j := 0; j < len(m.Naras); j++ {
		if j != index {
			// They already have this nara in their neighbourhood from initial mesh setup
			// Just update their observation to reflect the restart
			m.Naras[j].setObservation(fresh.Me.Name, NaraObservation{
				Online:      "ONLINE",
				LastRestart: me.LastRestart,
				LastSeen:    me.LastSeen,
			})
		}
	}

	m.t.Logf("✅ Restarted nara %s (index %d) with fresh state", originalName, index)
	return fresh
}

// testNaraWithHTTP creates a test nara with HTTP server and returns the base URL.
// The server is automatically started on a test-specific port and cleaned up after the test.
//
// Example:
//
//	nara, baseURL := testNaraWithHTTP(t, "test-nara")
//	resp, err := http.Get(baseURL + "/api/stash/status")

// =============================================================================
// High-Level Condition Helpers
// =============================================================================
// These helpers replace fixed time.Sleep() calls with condition-based waits
// to reduce test flakiness in CI environments.

// waitForNeighborDiscovery waits until a specific neighbor is discovered by a nara.
// Returns true if the neighbor was discovered, false on timeout.
func waitForNeighborDiscovery(t *testing.T, ln *LocalNara, neighborName types.NaraName, timeout time.Duration) bool {
	t.Helper()
	return waitForCondition(t, func() bool {
		ln.Network.local.mu.Lock()
		_, found := ln.Network.Neighbourhood[neighborName]
		ln.Network.local.mu.Unlock()
		return found
	}, timeout, "discover neighbor "+neighborName.String())
}

// waitForNeighborCount waits until a nara has discovered at least minCount neighbors.
// Returns true if the count was reached, false on timeout.
func waitForNeighborCount(t *testing.T, ln *LocalNara, minCount int, timeout time.Duration) bool {
	t.Helper()
	return waitForCondition(t, func() bool {
		ln.Network.local.mu.Lock()
		count := len(ln.Network.Neighbourhood)
		ln.Network.local.mu.Unlock()
		return count >= minCount
	}, timeout, fmt.Sprintf("discover at least %d neighbors", minCount))
}

// waitForObservation waits until a nara has an observation for a specific neighbor
// where the checker function returns true.
func waitForObservation(t *testing.T, ln *LocalNara, neighborName types.NaraName, checker func(NaraObservation) bool, timeout time.Duration, description string) bool {
	t.Helper()
	return waitForCondition(t, func() bool {
		obs := ln.getObservation(neighborName)
		return checker(obs)
	}, timeout, description)
}

// waitForCheckpointV2 waits until a v2 checkpoint exists for the subject.
// This is more specific than waitForCheckpoint - it ensures the checkpoint has version 2.
func waitForCheckpointV2(t *testing.T, ledger *SyncLedger, subject types.NaraName, timeout time.Duration) *CheckpointEventPayload {
	t.Helper()
	var checkpoint *CheckpointEventPayload
	ok := waitForCondition(t, func() bool {
		checkpoint = ledger.GetCheckpoint(subject)
		return checkpoint != nil && checkpoint.Version == 2
	}, timeout, "v2 checkpoint for "+subject.String())
	if ok {
		return checkpoint
	}
	return nil
}

// waitForRecentCheckpoint waits until a checkpoint exists that was created within the last maxAge seconds.
// This is useful when you want to verify a NEW checkpoint was created, not just any checkpoint.
func waitForRecentCheckpoint(t *testing.T, ledger *SyncLedger, subject types.NaraName, maxAge time.Duration, timeout time.Duration) *CheckpointEventPayload {
	t.Helper()
	cutoff := time.Now().Add(-maxAge).Unix()
	var checkpoint *CheckpointEventPayload
	ok := waitForCondition(t, func() bool {
		checkpoint = ledger.GetCheckpoint(subject)
		return checkpoint != nil && checkpoint.AsOfTime > cutoff
	}, timeout, "recent checkpoint for "+subject.String())
	if ok {
		return checkpoint
	}
	return nil
}

// waitForHTTPReady waits until an HTTP endpoint responds successfully.
// Useful for waiting for HTTP servers to be ready after starting them in goroutines.
func waitForHTTPReady(t *testing.T, url string, timeout time.Duration) bool {
	t.Helper()
	client := &http.Client{Timeout: 500 * time.Millisecond}
	return waitForCondition(t, func() bool {
		resp, err := client.Get(url)
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode < 500 // Any non-5xx is "ready"
	}, timeout, "HTTP endpoint ready")
}
