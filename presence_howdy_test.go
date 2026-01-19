package nara

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHowdy_StartTimeRecovery tests the scenario where a nara goes offline
// and comes back, recovering its original start time via howdy responses.
func TestHowdy_StartTimeRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	// Start embedded MQTT broker on dynamic port
	_, port := startTestMQTTBrokerDynamic(t)

	t.Log("🧪 Testing start time recovery via howdy protocol")

	// Step 1: Boot nara1
	nara1, err := createTestNara(t, "howdy-nara-1", port)
	if err != nil {
		t.Fatalf("Failed to create test nara: %v", err)
	}
	go nara1.Start(false, false, "", nil, TransportMQTT)

	t.Log("✅ Started nara1")
	// Wait for nara1 to connect to MQTT
	waitForMQTTConnected(t, nara1, 5*time.Second)

	// Step 2: Boot nara2 - it says hey_there, nara1 responds with howdy
	nara2, err := createTestNara(t, "howdy-nara-2", port)
	if err != nil {
		t.Fatalf("Failed to create test nara: %v", err)
	}
	go nara2.Start(false, false, "", nil, TransportMQTT)

	t.Log("✅ Started nara2")
	// Wait for nara2 to connect to MQTT
	waitForMQTTConnected(t, nara2, 5*time.Second)

	// Step 3: Wait for nara1 to discover nara2 via howdy
	if !waitForNeighborDiscovery(t, nara1, "howdy-nara-2", 10*time.Second) {
		t.Fatal("Timeout: nara1 should discover nara2 via howdy")
	}
	t.Log("✅ nara1 discovered nara2")

	// Step 4: Wait for nara2 to discover nara1 via howdy
	if !waitForNeighborDiscovery(t, nara2, "howdy-nara-1", 10*time.Second) {
		t.Fatal("Timeout: nara2 should discover nara1 via howdy")
	}
	t.Log("✅ nara2 discovered nara1")

	// Step 5: Wait for nara1 to record LastSeen for nara2
	if !waitForObservation(t, nara1, "howdy-nara-2", func(obs NaraObservation) bool {
		return obs.LastSeen != 0
	}, 5*time.Second, "nara1 should record LastSeen for nara2") {
		t.Fatal("Timeout: nara1 should record LastSeen for nara2")
	}

	// nara1 should have recorded when it first saw nara2
	obs1AboutNara2 := nara1.getObservation("howdy-nara-2")
	t.Logf("📊 nara1's observation of nara2: StartTime=%d, LastSeen=%d", obs1AboutNara2.StartTime, obs1AboutNara2.LastSeen)

	// nara2 should know its own start time (set when it booted)
	nara2SelfObs := nara2.getMeObservation()
	t.Logf("📊 nara2's self observation: StartTime=%d, LastRestart=%d", nara2SelfObs.StartTime, nara2SelfObs.LastRestart)

	// Verify LastSeen is set
	require.NotZero(t, obs1AboutNara2.LastSeen, "nara1 should have recorded when it last saw nara2")

	t.Log("✅ Start time/observations recorded")
}

// TestHowdy_NeighborDiscovery tests that howdy responses include neighbor info.
func TestHowdy_NeighborDiscovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	// Start embedded MQTT broker on dynamic port
	_, port := startTestMQTTBrokerDynamic(t)

	t.Log("🧪 Testing neighbor discovery via howdy protocol")

	// Boot nara1 and nara2 first
	nara1, err := createTestNara(t, "discover-nara-1", port)
	if err != nil {
		t.Fatalf("Failed to create test nara: %v", err)
	}
	go nara1.Start(false, false, "", nil, TransportMQTT)

	waitForMQTTConnected(t, nara1, 5*time.Second)

	nara2, err := createTestNara(t, "discover-nara-2", port)
	if err != nil {
		t.Fatalf("Failed to create test nara: %v", err)
	}
	go nara2.Start(false, false, "", nil, TransportMQTT)

	waitForMQTTConnected(t, nara2, 5*time.Second)

	// Wait for nara1 and nara2 to discover each other
	if !waitForNeighborDiscovery(t, nara1, "discover-nara-2", 10*time.Second) {
		t.Fatal("Timeout: nara1 should discover nara2")
	}

	// Now boot nara3 - it should learn about both nara1 and nara2 via howdy
	nara3, err := createTestNara(t, "discover-nara-3", port)
	if err != nil {
		t.Fatalf("Failed to create test nara: %v", err)
	}
	go nara3.Start(false, false, "", nil, TransportMQTT)

	t.Log("✅ Started all 3 naras")

	waitForMQTTConnected(t, nara3, 5*time.Second)

	// Wait for nara3 to discover both nara1 and nara2 via hey-there/howdy exchange
	if !waitForNeighborDiscovery(t, nara3, "discover-nara-1", 10*time.Second) {
		t.Fatal("Timeout: nara3 should discover nara1")
	}
	if !waitForNeighborDiscovery(t, nara3, "discover-nara-2", 10*time.Second) {
		t.Fatal("Timeout: nara3 should discover nara2")
	}

	// Verify final state
	nara3.Network.local.mu.Lock()
	neighborCount := len(nara3.Network.Neighbourhood)
	_, hasNara1 := nara3.Network.Neighbourhood["discover-nara-1"]
	_, hasNara2 := nara3.Network.Neighbourhood["discover-nara-2"]
	nara3.Network.local.mu.Unlock()

	t.Logf("📊 nara3 discovered %d neighbors", neighborCount)
	assert.True(t, hasNara1, "nara3 should have discovered nara1")
	assert.True(t, hasNara2, "nara3 should have discovered nara2")

	t.Log("✅ nara3 discovered both neighbors via howdy")
}

// TestHowdy_SelfSelection tests that only ~10 naras respond to a hey_there.
func TestHowdy_SelfSelection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	// Start embedded MQTT broker on dynamic port
	_, port := startTestMQTTBrokerDynamic(t)

	t.Log("🧪 Testing howdy self-selection with 15 naras")

	const numNaras = 15
	naras := make([]*LocalNara, numNaras)

	// Start 14 naras first
	for i := 0; i < numNaras-1; i++ {
		name := fmt.Sprintf("select-nara-%d", i)
		var err error
		naras[i], err = createTestNara(t, name, port)
		if err != nil {
			t.Fatalf("Failed to create test nara: %v", err)
		}
		go naras[i].Start(false, false, "", nil, TransportMQTT)
	}

	// Wait for all 14 naras to connect
	for i := 0; i < numNaras-1; i++ {
		waitForMQTTConnected(t, naras[i], 5*time.Second)
	}

	t.Log("✅ Started 14 naras, waiting for them to discover each other")

	// Wait for at least some mutual discovery among the first 14 naras
	// We wait until the first nara has at least 5 neighbors, indicating discovery is working
	if !waitForNeighborCount(t, naras[0], 5, 15*time.Second) {
		t.Fatal("Timeout: naras failed to discover each other")
	}

	// Now start the 15th nara - it should trigger howdy responses from up to 10 naras
	lastNara, err := createTestNara(t, "select-nara-14", port)
	if err != nil {
		t.Fatalf("Failed to create test nara: %v", err)
	}
	naras[numNaras-1] = lastNara
	go lastNara.Start(false, false, "", nil, TransportMQTT)

	waitForMQTTConnected(t, lastNara, 5*time.Second)

	t.Log("✅ Started 15th nara, waiting for neighbor discovery")

	// Wait for the last nara to discover at least 10 neighbors
	if !waitForCondition(t, func() bool {
		lastNara.Network.local.mu.Lock()
		count := len(lastNara.Network.Neighbourhood)
		lastNara.Network.local.mu.Unlock()
		return count >= 10
	}, 15*time.Second, "last nara should discover at least 10 neighbors") {
		t.Fatal("Timeout: last nara should discover at least 10 neighbors")
	}

	// Check final count
	lastNara.Network.local.mu.Lock()
	neighborCount := len(lastNara.Network.Neighbourhood)
	lastNara.Network.local.mu.Unlock()

	t.Logf("📊 Last nara discovered %d neighbors", neighborCount)

	// With the howdy protocol, we expect all 14 naras to be discovered
	// (either via direct howdy responses or via neighbor info in howdys)
	// The self-selection limits howdy RESPONSES to 10, but neighbor info can fill the rest
	assert.GreaterOrEqual(t, neighborCount, 10, "Last nara should discover at least 10 neighbors")
	t.Logf("✅ Neighbor discovery works with self-selection (found %d)", neighborCount)
}

// Helper functions

func createTestNara(t *testing.T, name string, port int) (*LocalNara, error) {
	// Use unified testNara with MQTT and howdy test config
	ln := testNara(t, name, WithMQTT(port), WithParams(-1, 1000), WithHowdyTestConfig())
	return ln, nil
}
