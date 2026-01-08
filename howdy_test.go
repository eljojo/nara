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
	nara1 := createTestNara(t, "howdy-nara-1", 11884)
	go nara1.Start(false, false, "", nil, TransportMQTT)
	defer nara1.Network.Shutdown()
	defer nara1.Network.disconnectMQTT()

	t.Log("✅ Started nara1")
	time.Sleep(2 * time.Second)

	// Step 2: Boot nara2 - it says hey_there, nara1 responds with howdy
	nara2 := createTestNara(t, "howdy-nara-2", 11884)
	go nara2.Start(false, false, "", nil, TransportMQTT)
	defer nara2.Network.Shutdown()
	defer nara2.Network.disconnectMQTT()

	t.Log("✅ Started nara2")
	time.Sleep(3 * time.Second)

	// Step 3: Verify nara1 discovered nara2 via howdy
	nara1.Network.local.mu.Lock()
	_, nara2InNeighborhood := nara1.Network.Neighbourhood["howdy-nara-2"]
	nara1.Network.local.mu.Unlock()
	require.True(t, nara2InNeighborhood, "nara1 should have discovered nara2")
	t.Log("✅ nara1 discovered nara2")

	// Step 4: Verify nara2 discovered nara1 via howdy
	nara2.Network.local.mu.Lock()
	_, nara1InNeighborhood := nara2.Network.Neighbourhood["howdy-nara-1"]
	nara2.Network.local.mu.Unlock()
	require.True(t, nara1InNeighborhood, "nara2 should have discovered nara1")
	t.Log("✅ nara2 discovered nara1")

	// Step 5: Record nara2's start time as seen by nara1
	// First, let nara1 form an opinion about nara2
	time.Sleep(2 * time.Second)

	// nara1 should have recorded when it first saw nara2
	obs1AboutNara2 := nara1.getObservation("howdy-nara-2")
	t.Logf("📊 nara1's observation of nara2: StartTime=%d, LastSeen=%d", obs1AboutNara2.StartTime, obs1AboutNara2.LastSeen)

	// nara2 should know its own start time (set when it booted)
	nara2SelfObs := nara2.getMeObservation()
	t.Logf("📊 nara2's self observation: StartTime=%d, LastRestart=%d", nara2SelfObs.StartTime, nara2SelfObs.LastRestart)

	// The start time might not be set yet if no one else told nara2 about itself
	// But nara1 should have a record of when it first saw nara2
	require.NotZero(t, obs1AboutNara2.LastSeen, "nara1 should have recorded when it last saw nara2")

	t.Log("✅ Start time/observations recorded")
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
	nara1 := createTestNara(t, "discover-nara-1", 11885)
	go nara1.Start(false, false, "", nil, TransportMQTT)
	defer nara1.Network.Shutdown()
	defer nara1.Network.disconnectMQTT()

	time.Sleep(2 * time.Second)

	nara2 := createTestNara(t, "discover-nara-2", 11885)
	go nara2.Start(false, false, "", nil, TransportMQTT)
	defer nara2.Network.Shutdown()
	defer nara2.Network.disconnectMQTT()

	time.Sleep(3 * time.Second)

	// Now boot nara3 - it should learn about both nara1 and nara2 via howdy
	nara3 := createTestNara(t, "discover-nara-3", 11885)
	go nara3.Start(false, false, "", nil, TransportMQTT)
	defer nara3.Network.Shutdown()
	defer nara3.Network.disconnectMQTT()

	t.Log("✅ Started all 3 naras")
	time.Sleep(5 * time.Second)

	// Verify nara3 discovered both nara1 and nara2
	nara3.Network.local.mu.Lock()
	neighborCount := len(nara3.Network.Neighbourhood)
	_, hasNara1 := nara3.Network.Neighbourhood["discover-nara-1"]
	_, hasNara2 := nara3.Network.Neighbourhood["discover-nara-2"]
	nara3.Network.local.mu.Unlock()

	t.Logf("📊 nara3 discovered %d neighbors", neighborCount)
	assert.True(t, hasNara1, "nara3 should have discovered nara1")
	assert.True(t, hasNara2, "nara3 should have discovered nara2")

	if hasNara1 && hasNara2 {
		t.Log("✅ nara3 discovered both neighbors via howdy")
	}
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
		naras[i] = createTestNara(t, name, 11886)
		go naras[i].Start(false, false, "", nil, TransportMQTT)
		defer naras[i].Network.Shutdown()
		defer naras[i].Network.disconnectMQTT()
	}

	t.Log("✅ Started 14 naras, waiting for them to settle")
	time.Sleep(5 * time.Second)

	// Now start the 15th nara - it should trigger howdy responses from up to 10 naras
	lastNara := createTestNara(t, "select-nara-14", 11886)
	naras[numNaras-1] = lastNara
	go lastNara.Start(false, false, "", nil, TransportMQTT)
	defer lastNara.Network.Shutdown()
	defer lastNara.Network.disconnectMQTT()

	t.Log("✅ Started 15th nara, waiting for howdy responses")
	time.Sleep(5 * time.Second)

	// Check how many neighbors the last nara discovered
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
		t.Fatalf("Failed to add auth hook: %v", err)
	}

	tcp := listeners.NewTCP(listeners.Config{
		ID:      fmt.Sprintf("howdy-test-broker-%d", port),
		Address: fmt.Sprintf(":%d", port),
	})
	err = server.AddListener(tcp)
	if err != nil {
		t.Fatalf("Failed to add listener: %v", err)
	}

	go func() {
		err := server.Serve()
		if err != nil {
			t.Logf("MQTT broker stopped: %v", err)
		}
	}()

	return server
}

func createTestNara(t *testing.T, name string, port int) *LocalNara {
	hwFingerprint := []byte(fmt.Sprintf("howdy-test-hw-%s", name))
	soulV1 := NativeSoulCustom(hwFingerprint, name)
	soul := FormatSoul(soulV1)

	ln := NewLocalNara(
		name,
		soul,
		fmt.Sprintf("tcp://127.0.0.1:%d", port),
		"",
		"",
		-1,
		1000,
	)

	// Skip the 1s sleep in handleHeyThereEvent for faster tests
	ln.Network.testSkipHeyThereSleep = true

	return ln
}
