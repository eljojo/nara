package nara

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	mqttserver "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/sirupsen/logrus"
)

// testSoul generates a valid soul for testing given a name.
// This ensures all test naras have valid keypairs for signing.
func testSoul(name string) string {
	hw := hashTestBytes([]byte("test-hardware-" + name))
	soul := NativeSoulCustom(hw, name)
	return FormatSoul(soul)
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

// testLocalNara creates a LocalNara for testing with a valid identity bonded to the name.
func testLocalNara(name string) *LocalNara {
	identity := testIdentity(name)
	profile := DefaultMemoryProfile()
	ln, err := NewLocalNara(identity, "host", "user", "pass", -1, profile)
	if err != nil {
		panic(err)
	}
	delay := time.Duration(0)
	ln.Network.testObservationDelay = &delay
	return ln
}

// testLocalNaraWithParams creates a LocalNara for testing with specific chattiness and ledger capacity.
func testLocalNaraWithParams(name string, chattiness int, ledgerCapacity int) *LocalNara {
	identity := testIdentity(name)
	profile := DefaultMemoryProfile()
	if ledgerCapacity > 0 {
		profile.Mode = MemoryModeCustom
		profile.MaxEvents = ledgerCapacity
	}
	ln, err := NewLocalNara(identity, "", "", "", chattiness, profile)
	if err != nil {
		panic(err)
	}
	delay := time.Duration(0)
	ln.Network.testObservationDelay = &delay
	return ln
}

// testLocalNaraWithSoul creates a LocalNara for testing with a specific soul string.
func testLocalNaraWithSoul(name string, soul string) *LocalNara {
	parsed, _ := ParseSoul(soul)
	id, _ := ComputeNaraID(soul, name)
	identity := IdentityResult{
		Name:        name,
		Soul:        parsed,
		ID:          id,
		IsValidBond: true,
		IsNative:    true,
	}
	profile := DefaultMemoryProfile()
	ln, err := NewLocalNara(identity, "host", "user", "pass", -1, profile)
	if err != nil {
		panic(err)
	}
	return ln
}

func testIdentity(name string) IdentityResult {
	soulStr := testSoul(name)
	parsed, _ := ParseSoul(soulStr)
	id, _ := ComputeNaraID(soulStr, name)
	return IdentityResult{
		Name:        name,
		Soul:        parsed,
		ID:          id,
		IsValidBond: true,
		IsNative:    true,
	}
}

// testLocalNaraWithSoulAndParams creates a LocalNara for testing with a specific soul and parameters.
func testLocalNaraWithSoulAndParams(name string, soul string, chattiness int, ledgerCapacity int) *LocalNara {
	parsed, _ := ParseSoul(soul)
	id, _ := ComputeNaraID(soul, name)
	identity := IdentityResult{
		Name:        name,
		Soul:        parsed,
		ID:          id,
		IsValidBond: true,
		IsNative:    true,
	}
	profile := DefaultMemoryProfile()
	if ledgerCapacity > 0 {
		profile.Mode = MemoryModeCustom
		profile.MaxEvents = ledgerCapacity
	}
	ln, err := NewLocalNara(identity, "", "", "", chattiness, profile)
	if err != nil {
		panic(err)
	}
	return ln
}

// startTestMQTTBroker starts an embedded MQTT broker for testing on the given port.
// Returns the broker server which should be closed with defer broker.Close() after the test.
func startTestMQTTBroker(t *testing.T, port int) *mqttserver.Server {
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

	return server
}

// createTestNaraForMQTT creates a LocalNara for MQTT testing with proper test flags
func createTestNaraForMQTT(t *testing.T, name string, port int) *LocalNara {
	t.Helper()
	hwFingerprint := []byte(fmt.Sprintf("test-hw-%s", name))
	identity := DetermineIdentity("", "", name, hwFingerprint)

	profile := DefaultMemoryProfile()
	profile.Mode = MemoryModeCustom
	profile.MaxEvents = 1000
	ln, err := NewLocalNara(
		identity,
		fmt.Sprintf("tcp://127.0.0.1:%d", port),
		"", "",
		-1,
		profile,
	)
	if err != nil {
		t.Fatalf("Failed to create LocalNara: %v", err)
	}

	// Skip jitter delays for faster, more predictable test execution
	ln.Network.testSkipJitter = true
	// Skip boot recovery - tests will manually add observation data
	ln.Network.testSkipBootRecovery = true

	return ln
}

// startTestNaras creates and starts multiple naras, ensuring MQTT connection and full discovery
func startTestNaras(t *testing.T, port int, names []string, ensureDiscovery bool) []*LocalNara {
	t.Helper()
	naras := make([]*LocalNara, len(names))

	// Create and start all naras
	for i, name := range names {
		naras[i] = createTestNaraForMQTT(t, name, port)
		go naras[i].Start(false, false, "", nil, TransportMQTT)
		time.Sleep(100 * time.Millisecond) // Small delay between starts
	}

	// Wait for all to connect
	waitForAllMQTTConnected(t, naras, 15*time.Second)

	if ensureDiscovery {
		// Wait for initial hey-there cooldown (5s rate limit in heyThere())
		time.Sleep(5 * time.Second)

		// Trigger an extra round of hey-there to ensure late-joiners discover everyone
		for _, ln := range naras {
			ln.Network.heyThere()
		}
		time.Sleep(2 * time.Second) // Wait for hey-there/howdy exchanges
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
func waitForCheckpoint(t *testing.T, ledger *SyncLedger, subject string, timeout time.Duration) *CheckpointEventPayload {
	t.Helper()
	var checkpoint *CheckpointEventPayload
	ok := waitForCondition(t, func() bool {
		checkpoint = ledger.GetCheckpoint(subject)
		return checkpoint != nil
	}, timeout, "checkpoint for "+subject)
	if ok {
		return checkpoint
	}
	return nil
}

// waitForCheckpointPropagation blocks until all naras have the checkpoint for a subject.
func waitForCheckpointPropagation(t *testing.T, naras []*LocalNara, subject string, timeout time.Duration) bool {
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
func testCheckpointEvent(subject string, attester string, attesterKeypair NaraKeypair, observation NaraObservation) SyncEvent {
	now := time.Now()

	checkpoint := &CheckpointEventPayload{
		Version:     1,
		Subject:     subject,
		SubjectID:   "test-id-" + subject,
		Observation: observation,
		AsOfTime:    now.Unix(),
		Round:       1,
		VoterIDs:    []string{"test-id-" + attester},
	}

	// Create and sign attestation
	attestation := Attestation{
		Version:     checkpoint.Version,
		Subject:     checkpoint.Subject,
		SubjectID:   checkpoint.SubjectID,
		Observation: checkpoint.Observation,
		Attester:    attester,
		AttesterID:  "test-id-" + attester,
		AsOfTime:    checkpoint.AsOfTime,
	}
	attestation.Signature = SignContent(&attestation, attesterKeypair)
	checkpoint.Signatures = []string{attestation.Signature}

	// Create sync event
	event := SyncEvent{
		Timestamp:  now.UnixNano(),
		Service:    ServiceCheckpoint,
		Emitter:    attester,
		EmitterID:  "test-id-" + attester,
		Checkpoint: checkpoint,
	}
	event.ComputeID()
	event.Sign(attester, attesterKeypair)

	return event
}

// testCheckpointEventSimple creates a checkpoint event with default observation values
func testCheckpointEventSimple(subject string, attester string, attesterKeypair NaraKeypair) SyncEvent {
	observation := NaraObservation{
		Restarts:    5,
		TotalUptime: 3600,
		StartTime:   time.Now().Unix() - 86400,
	}
	return testCheckpointEvent(subject, attester, attesterKeypair, observation)
}

// testAddCheckpointToLedger creates and adds a checkpoint event to a ledger
func testAddCheckpointToLedger(ledger *SyncLedger, subject string, attester string, attesterKeypair NaraKeypair, observation NaraObservation) SyncEvent {
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
	Naras   []*LocalNara
	Servers []*httptest.Server
	Client  *http.Client
	t       *testing.T
}

// testCreateMeshNetwork creates N naras in a full mesh topology with HTTP servers.
// Each nara knows all others and has testMeshURLs configured for HTTP communication.
func testCreateMeshNetwork(t *testing.T, names []string, chattiness, ledgerCapacity int) *testMeshNetwork {
	t.Helper()
	count := len(names)

	mesh := &testMeshNetwork{
		Naras:   make([]*LocalNara, count),
		Servers: make([]*httptest.Server, count),
		Client:  &http.Client{Timeout: 5 * time.Second},
		t:       t,
	}

	// Create naras and servers
	for i, name := range names {
		ln := testLocalNaraWithParams(name, chattiness, ledgerCapacity)

		// Mark not booting (common integration test requirement)
		me := ln.getMeObservation()
		me.LastRestart = time.Now().Unix() - 200
		me.LastSeen = time.Now().Unix()
		ln.setMeObservation(me)

		// Create HTTP server with common endpoints
		mux := http.NewServeMux()
		mux.HandleFunc("/gossip/zine", ln.Network.httpGossipZineHandler)
		mux.HandleFunc("/api/checkpoints/all", ln.Network.httpCheckpointsAllHandler)
		server := httptest.NewServer(mux)

		mesh.Naras[i] = ln
		mesh.Servers[i] = server

		// Configure test hooks
		ln.Network.testHTTPClient = mesh.Client
		ln.Network.testMeshURLs = make(map[string]string)
		ln.Network.TransportMode = TransportGossip
	}

	// Create full mesh: each nara knows all others
	for i := 0; i < count; i++ {
		for j := 0; j < count; j++ {
			if i != j {
				neighbor := NewNara(mesh.Naras[j].Me.Name)
				neighbor.Status.ID = mesh.Naras[j].Me.Status.ID
				neighbor.Status.PublicKey = FormatPublicKey(mesh.Naras[j].Keypair.PublicKey)
				mesh.Naras[i].Network.importNara(neighbor)
				mesh.Naras[i].setObservation(mesh.Naras[j].Me.Name, NaraObservation{Online: "ONLINE"})
				mesh.Naras[i].Network.testMeshURLs[mesh.Naras[j].Me.Name] = mesh.Servers[j].URL
			}
		}
	}

	// Register cleanup
	t.Cleanup(func() {
		for _, s := range mesh.Servers {
			s.Close()
		}
	})

	return mesh
}

// Get returns the nara at index i.
func (m *testMeshNetwork) Get(i int) *LocalNara {
	return m.Naras[i]
}

// GetByName finds a nara by name.
func (m *testMeshNetwork) GetByName(name string) *LocalNara {
	for _, ln := range m.Naras {
		if ln.Me.Name == name {
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
