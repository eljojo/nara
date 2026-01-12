package nara

import (
	"crypto/sha256"
	"fmt"
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
	ok := waitForCondition(t, func() bool {
		return ln.Network.Mqtt != nil && ln.Network.Mqtt.IsConnected()
	}, timeout, "MQTT connected")
	if !ok {
		t.Fatalf("Timed out waiting for %s MQTT to connect", ln.Me.Name)
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
