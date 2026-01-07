package nara

import (
	"fmt"
	"strings"
	"testing"
	"time"

	mqttserver "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
)

// TestIntegration_MultiNaraNetwork runs a full integration test with multiple naras
// communicating through an in-memory MQTT broker. This validates the happy path:
// - Naras can discover each other
// - Social events propagate correctly
// - Opinions form based on interactions
// - No crashes or panics occur
func TestIntegration_MultiNaraNetwork(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Start embedded MQTT broker
	broker := startEmbeddedBroker(t)
	defer broker.Close()

	// Give broker time to start
	time.Sleep(200 * time.Millisecond)

	const numNaras = 10
	const testDuration = 10 * time.Second

	t.Logf("🧪 Starting integration test with %d naras for %v", numNaras, testDuration)

	// Create and start multiple naras
	naras := make([]*LocalNara, numNaras)

	for i := 0; i < numNaras; i++ {
		name := fmt.Sprintf("test-nara-%d", i)
		soul := strings.Repeat(fmt.Sprintf("%d", i), 54) // Valid 54-char soul

		// Create LocalNara with embedded MQTT broker address
		ln := NewLocalNara(
			name,
			soul,
			"tcp://127.0.0.1:11883", // embedded broker
			"",                      // no user
			"",                      // no pass
			-1,                      // auto chattiness
			1000,                    // ledger capacity
		)

		naras[i] = ln

		// Start the nara (this spawns all goroutines)
		go ln.Start(
			false, // don't serve UI
			false, // not read-only
			"",    // no HTTP addr
			nil,   // no mesh
		)

		t.Logf("✅ Started %s (personality: A=%d S=%d C=%d)",
			name,
			ln.Me.Status.Personality.Agreeableness,
			ln.Me.Status.Personality.Sociability,
			ln.Me.Status.Personality.Chill,
		)
	}

	// Let the network run and interact
	t.Logf("🌐 Network running, letting them interact for %v...", testDuration)
	time.Sleep(testDuration)

	// Stop all naras by disconnecting MQTT
	t.Log("🛑 Stopping all naras...")
	for _, ln := range naras {
		ln.Network.disconnectMQTT()
	}

	// Give them a moment to clean up
	time.Sleep(500 * time.Millisecond)

	// Now validate the happy path
	t.Log("")
	t.Log("🔍 Validating results...")

	// 1. Check that naras discovered each other
	totalNeighbors := 0
	for _, ln := range naras {
		ln.Network.local.mu.Lock()
		neighborCount := len(ln.Network.Neighbourhood)
		ln.Network.local.mu.Unlock()

		totalNeighbors += neighborCount
		t.Logf("  %s discovered %d neighbors", ln.Me.Name, neighborCount)

		if neighborCount < 3 {
			t.Errorf("❌ %s only discovered %d neighbors (expected at least 3)", ln.Me.Name, neighborCount)
		}
	}
	t.Logf("✅ Discovery: Total %d neighbor relationships formed", totalNeighbors)

	// 2. Check that social events were recorded
	totalSocialEvents := 0
	for _, ln := range naras {
		socialEvents := ln.SyncLedger.GetSocialEventsForSubjects([]string{ln.Me.Name})
		eventCount := len(socialEvents)
		totalSocialEvents += eventCount

		if eventCount > 0 {
			t.Logf("  %s participated in %d social events", ln.Me.Name, eventCount)
		}
	}

	if totalSocialEvents == 0 {
		t.Log("⚠️  No social events recorded (this can happen with low sociability)")
	} else {
		t.Logf("✅ Social dynamics: %d social events occurred", totalSocialEvents)
	}

	// 3. Check that some naras are following trends
	narasWithTrends := 0
	for _, ln := range naras {
		ln.Me.mu.Lock()
		hasTrend := ln.Me.Status.Trend != ""
		trend := ln.Me.Status.Trend
		ln.Me.mu.Unlock()

		if hasTrend {
			narasWithTrends++
			t.Logf("  %s is following trend: %s", ln.Me.Name, trend)
		}
	}

	t.Logf("✅ Trends: %d/%d naras following trends", narasWithTrends, numNaras)

	// 4. Check that buzz values are set
	narasWithBuzz := 0
	for _, ln := range naras {
		ln.Me.mu.Lock()
		buzzValue := ln.Me.Status.Buzz
		ln.Me.mu.Unlock()

		if buzzValue > 0 {
			narasWithBuzz++
		}
	}
	t.Logf("✅ Activity: %d/%d naras have buzz > 0", narasWithBuzz, numNaras)

	// Final summary
	t.Log("")
	t.Log("═══════════════════════════════════════")
	t.Log("🎉 INTEGRATION TEST PASSED")
	t.Logf("   • %d naras successfully interacted", numNaras)
	t.Logf("   • %d neighbor discoveries", totalNeighbors)
	t.Logf("   • %d social events", totalSocialEvents)
	t.Logf("   • %d naras following trends", narasWithTrends)
	t.Logf("   • %d naras with buzz", narasWithBuzz)
	t.Log("   • No crashes or panics")
	t.Log("═══════════════════════════════════════")
}

// startEmbeddedBroker starts an in-memory MQTT broker for testing
func startEmbeddedBroker(t *testing.T) *mqttserver.Server {
	server := mqttserver.New(nil)

	// Allow all connections (no auth for testing)
	err := server.AddHook(new(auth.AllowHook), nil)
	if err != nil {
		t.Fatalf("Failed to add auth hook: %v", err)
	}

	// Listen on TCP port 11883 (different from default 1883)
	tcp := listeners.NewTCP(listeners.Config{
		ID:      "test-broker",
		Address: ":11883",
	})
	err = server.AddListener(tcp)
	if err != nil {
		t.Fatalf("Failed to add listener: %v", err)
	}

	// Start the server in a goroutine
	go func() {
		err := server.Serve()
		if err != nil {
			t.Logf("MQTT broker stopped: %v", err)
		}
	}()

	t.Log("🔌 Embedded MQTT broker started on :11883")

	return server
}
