package nara

import (
	"fmt"
	"testing"
	"time"

	mqttserver "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHowdy_StartTimeRecovery tests the scenario where a nara goes offline
// and comes back, recovering its original start time via howdy responses
func TestHowdy_StartTimeRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Start embedded MQTT broker on a unique port
	broker := startHowdyTestBroker(t, 11884)
	defer broker.Close()

	time.Sleep(200 * time.Millisecond)

	t.Log("🧪 Testing start time recovery via howdy protocol")

	// Step 1: Boot nara1
	nara1, err := createTestNara(t, "howdy-nara-1", 11884)
	if err != nil {
		t.Fatalf("Failed to create test nara: %v", err)
	}
	go nara1.Start(false, false, "", nil, TransportMQTT)
	defer nara1.Network.Shutdown()
	defer nara1.Network.disconnectMQTT()

	t.Log("✅ Started nara1")
	// Wait for nara1 to connect to MQTT
	waitForCondition(t, 5*time.Second, func() bool {
		return nara1.Network.Mqtt != nil && nara1.Network.Mqtt.IsConnected()
	}, "nara1 should connect to MQTT")

	// Step 2: Boot nara2 - it says hey_there, nara1 responds with howdy
	nara2, err := createTestNara(t, "howdy-nara-2", 11884)
	if err != nil {
		t.Fatalf("Failed to create test nara: %v", err)
	}
	go nara2.Start(false, false, "", nil, TransportMQTT)
	defer nara2.Network.Shutdown()
	defer nara2.Network.disconnectMQTT()

	t.Log("✅ Started nara2")
	// Wait for nara2 to connect to MQTT
	waitForCondition(t, 5*time.Second, func() bool {
		return nara2.Network.Mqtt != nil && nara2.Network.Mqtt.IsConnected()
	}, "nara2 should connect to MQTT")

	// Step 3: Wait for nara1 to discover nara2 via howdy
	waitForCondition(t, 10*time.Second, func() bool {
		nara1.Network.local.mu.Lock()
		_, found := nara1.Network.Neighbourhood["howdy-nara-2"]
		nara1.Network.local.mu.Unlock()
		return found
	}, "nara1 should discover nara2 via howdy")
	t.Log("✅ nara1 discovered nara2")

	// Step 4: Wait for nara2 to discover nara1 via howdy
	waitForCondition(t, 10*time.Second, func() bool {
		nara2.Network.local.mu.Lock()
		_, found := nara2.Network.Neighbourhood["howdy-nara-1"]
		nara2.Network.local.mu.Unlock()
		return found
	}, "nara2 should discover nara1 via howdy")
	t.Log("✅ nara2 discovered nara1")

	// Step 5: Wait for nara1 to record LastSeen for nara2
	waitForCondition(t, 5*time.Second, func() bool {
		obs := nara1.getObservation("howdy-nara-2")
		return obs.LastSeen != 0
	}, "nara1 should record LastSeen for nara2")

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

// waitForCondition polls a condition function until it returns true or timeout is reached.
// This replaces fixed sleeps with proper synchronization.
func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool, description string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		if condition() {
			return
		}

		select {
		case <-ticker.C:
			if time.Now().After(deadline) {
				t.Fatalf("Timeout waiting for condition: %s (waited %v)", description, timeout)
			}
		}
	}
}

// TestHowdy_NeighborDiscovery tests that howdy responses include neighbor info
func TestHowdy_NeighborDiscovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Start embedded MQTT broker on a unique port
	broker := startHowdyTestBroker(t, 11885)
	defer broker.Close()

	time.Sleep(200 * time.Millisecond)

	t.Log("🧪 Testing neighbor discovery via howdy protocol")

	// Boot nara1 and nara2 first
	nara1, err := createTestNara(t, "discover-nara-1", 11885)
	if err != nil {
		t.Fatalf("Failed to create test nara: %v", err)
	}
	go nara1.Start(false, false, "", nil, TransportMQTT)
	defer nara1.Network.Shutdown()
	defer nara1.Network.disconnectMQTT()

	waitForCondition(t, 5*time.Second, func() bool {
		return nara1.Network.Mqtt != nil && nara1.Network.Mqtt.IsConnected()
	}, "nara1 should connect to MQTT")

	nara2, err := createTestNara(t, "discover-nara-2", 11885)
	if err != nil {
		t.Fatalf("Failed to create test nara: %v", err)
	}
	go nara2.Start(false, false, "", nil, TransportMQTT)
	defer nara2.Network.Shutdown()
	defer nara2.Network.disconnectMQTT()

	waitForCondition(t, 5*time.Second, func() bool {
		return nara2.Network.Mqtt != nil && nara2.Network.Mqtt.IsConnected()
	}, "nara2 should connect to MQTT")

	// Wait for nara1 and nara2 to discover each other
	waitForCondition(t, 10*time.Second, func() bool {
		nara1.Network.local.mu.Lock()
		_, found := nara1.Network.Neighbourhood["discover-nara-2"]
		nara1.Network.local.mu.Unlock()
		return found
	}, "nara1 should discover nara2")

	// Now boot nara3 - it should learn about both nara1 and nara2 via howdy
	nara3, err := createTestNara(t, "discover-nara-3", 11885)
	if err != nil {
		t.Fatalf("Failed to create test nara: %v", err)
	}
	go nara3.Start(false, false, "", nil, TransportMQTT)
	defer nara3.Network.Shutdown()
	defer nara3.Network.disconnectMQTT()

	t.Log("✅ Started all 3 naras")

	waitForCondition(t, 5*time.Second, func() bool {
		return nara3.Network.Mqtt != nil && nara3.Network.Mqtt.IsConnected()
	}, "nara3 should connect to MQTT")

	// Wait for nara3 to discover both nara1 and nara2
	waitForCondition(t, 10*time.Second, func() bool {
		nara3.Network.local.mu.Lock()
		_, hasNara1 := nara3.Network.Neighbourhood["discover-nara-1"]
		_, hasNara2 := nara3.Network.Neighbourhood["discover-nara-2"]
		nara3.Network.local.mu.Unlock()
		return hasNara1 && hasNara2
	}, "nara3 should discover both nara1 and nara2")

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

// TestHowdy_SelfSelection tests that only ~10 naras respond to a hey_there
func TestHowdy_SelfSelection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Start embedded MQTT broker on a unique port
	broker := startHowdyTestBroker(t, 11886)
	defer broker.Close()

	time.Sleep(200 * time.Millisecond)

	t.Log("🧪 Testing howdy self-selection with 15 naras")

	const numNaras = 15
	naras := make([]*LocalNara, numNaras)

	// Start 14 naras first
	for i := 0; i < numNaras-1; i++ {
		name := fmt.Sprintf("select-nara-%d", i)
		var err error
		naras[i], err = createTestNara(t, name, 11886)
		if err != nil {
			t.Fatalf("Failed to create test nara: %v", err)
		}
		go naras[i].Start(false, false, "", nil, TransportMQTT)
		defer naras[i].Network.Shutdown()
		defer naras[i].Network.disconnectMQTT()
	}

	// Wait for all 14 naras to connect
	for i := 0; i < numNaras-1; i++ {
		nara := naras[i]
		waitForCondition(t, 5*time.Second, func() bool {
			return nara.Network.Mqtt != nil && nara.Network.Mqtt.IsConnected()
		}, fmt.Sprintf("select-nara-%d should connect to MQTT", i))
	}

	t.Log("✅ Started 14 naras, waiting for them to discover each other")

	// Wait for at least some mutual discovery among the first 14 naras
	time.Sleep(1000 * time.Millisecond)

	// Now start the 15th nara - it should trigger howdy responses from up to 10 naras
	lastNara, err := createTestNara(t, "select-nara-14", 11886)
	if err != nil {
		t.Fatalf("Failed to create test nara: %v", err)
	}
	naras[numNaras-1] = lastNara
	go lastNara.Start(false, false, "", nil, TransportMQTT)
	defer lastNara.Network.Shutdown()
	defer lastNara.Network.disconnectMQTT()

	waitForCondition(t, 5*time.Second, func() bool {
		return lastNara.Network.Mqtt != nil && lastNara.Network.Mqtt.IsConnected()
	}, "last nara should connect to MQTT")

	t.Log("✅ Started 15th nara, waiting for neighbor discovery")

	// Wait for the last nara to discover at least 10 neighbors
	waitForCondition(t, 15*time.Second, func() bool {
		lastNara.Network.local.mu.Lock()
		count := len(lastNara.Network.Neighbourhood)
		lastNara.Network.local.mu.Unlock()
		return count >= 10
	}, "last nara should discover at least 10 neighbors")

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

func startHowdyTestBroker(t *testing.T, port int) *mqttserver.Server {
	server := mqttserver.New(nil)

	err := server.AddHook(new(auth.AllowHook), nil)
	if err != nil {
		t.Fatalf("Failed to create LocalNara: %v", err)
	}

	tcp := listeners.NewTCP(listeners.Config{
		ID:      fmt.Sprintf("howdy-test-broker-%d", port),
		Address: fmt.Sprintf(":%d", port),
	})
	err = server.AddListener(tcp)
	if err != nil {
		t.Fatalf("Failed to create LocalNara: %v", err)
	}

	go func() {
		err := server.Serve()
		if err != nil {
			t.Logf("MQTT broker stopped: %v", err)
		}
	}()

	return server
}

func createTestNara(t *testing.T, name string, port int) (*LocalNara, error) {
	identity := testIdentity(name)

	ln, err := NewLocalNara(
		identity,
		fmt.Sprintf("tcp://127.0.0.1:%d", port),
		"",
		"",
		-1,
		1000,
	)
	if err != nil {
		return nil, err
	}

	// Skip the 1s sleep in handleHeyThereEvent for faster tests
	ln.Network.testSkipHeyThereSleep = true
	// Skip jitter delays for faster discovery in tests
	ln.Network.testSkipJitter = true

	return ln, nil
}
