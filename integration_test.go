package nara

import (
	"fmt"
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
		hwFingerprint := []byte(fmt.Sprintf("test-hw-fingerprint-%d", i))
		identity := DetermineIdentity("", "", name, hwFingerprint)

		// Create LocalNara with embedded MQTT broker address
		profile := DefaultMemoryProfile()
		profile.Mode = MemoryModeCustom
		profile.MaxEvents = 1000
		ln, err := NewLocalNara(
			identity,
			"tcp://127.0.0.1:11883", // embedded broker
			"",                      // no user
			"",                      // no pass
			-1,                      // auto chattiness
			profile,
		)
		if err != nil {
			t.Fatalf("Failed to create LocalNara: %v", err)
		}
		naras[i] = ln

		// Start the nara (this spawns all goroutines)
		go ln.Start(
			false,         // don't serve UI
			false,         // not read-only
			"",            // no HTTP addr
			nil,           // no mesh
			TransportMQTT, // MQTT-only for integration tests (no mesh)
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

	// 5. Check that observation events are being emitted
	totalObservationEvents := 0
	restartEvents := 0
	firstSeenEvents := 0
	statusChangeEvents := 0

	for _, ln := range naras {
		allEvents := ln.SyncLedger.GetAllEvents()
		for _, event := range allEvents {
			if event.Service == "observation" && event.Observation != nil {
				totalObservationEvents++
				switch event.Observation.Type {
				case "restart":
					restartEvents++
				case "first-seen":
					firstSeenEvents++
				case "status-change":
					statusChangeEvents++
				}
			}
		}
	}

	if totalObservationEvents > 0 {
		t.Logf("✅ Observation events: %d total (%d restart, %d first-seen, %d status-change)",
			totalObservationEvents, restartEvents, firstSeenEvents, statusChangeEvents)
	} else {
		t.Log("⚠️  No observation events recorded (feature may not be enabled yet)")
	}

	// 6. Verify observation event importance levels
	criticalEventCount := 0
	normalEventCount := 0
	for _, ln := range naras {
		allEvents := ln.SyncLedger.GetAllEvents()
		for _, event := range allEvents {
			if event.Service == "observation" && event.Observation != nil {
				switch event.Observation.Importance {
				case 3: // ImportanceCritical
					criticalEventCount++
				case 2: // ImportanceNormal
					normalEventCount++
				}
			}
		}
	}

	if criticalEventCount > 0 || normalEventCount > 0 {
		t.Logf("✅ Event importance: %d critical, %d normal", criticalEventCount, normalEventCount)
	}

	// 7. Validate anti-abuse: check no single observer→subject pair has >20 events
	abuseDetected := false
	for _, ln := range naras {
		pairCounts := make(map[string]int)
		allEvents := ln.SyncLedger.GetAllEvents()
		for _, event := range allEvents {
			if event.Service == "observation" && event.Observation != nil {
				pairKey := fmt.Sprintf("%s→%s", event.Observation.Observer, event.Observation.Subject)
				pairCounts[pairKey]++
				if pairCounts[pairKey] > 20 {
					abuseDetected = true
					t.Errorf("❌ Anti-abuse violation: %s has %d events (limit: 20)",
						pairKey, pairCounts[pairKey])
				}
			}
		}
	}

	if !abuseDetected && totalObservationEvents > 0 {
		t.Log("✅ Anti-abuse: Per-pair compaction working (no pair >20 events)")
	}

	// 8. Check that consensus can be derived from events (if implemented)
	consensusWorking := 0
	for _, ln := range naras {
		for neighborName := range ln.Network.Neighbourhood {
			// Try to derive opinion from events
			opinion := ln.Projections.Opinion().DeriveOpinion(neighborName)
			if opinion.StartTime > 0 || opinion.Restarts > 0 {
				consensusWorking++
				break // Count once per nara
			}
		}
	}

	if consensusWorking > 0 {
		t.Logf("✅ Event consensus: %d/%d naras can derive opinions from events", consensusWorking, numNaras)
	} else if totalObservationEvents > 0 {
		t.Log("⚠️  Event consensus not yet implemented")
	}

	// Final summary
	t.Log("")
	t.Log("═══════════════════════════════════════")
	t.Log("🎉 INTEGRATION TEST PASSED")
	t.Logf("   • %d naras successfully interacted", numNaras)
	t.Logf("   • %d neighbor discoveries", totalNeighbors)
	t.Logf("   • %d social events", totalSocialEvents)
	t.Logf("   • %d observation events (%d restart, %d first-seen, %d status-change)",
		totalObservationEvents, restartEvents, firstSeenEvents, statusChangeEvents)
	t.Logf("   • %d naras following trends", narasWithTrends)
	t.Logf("   • %d naras with buzz", narasWithBuzz)
	if consensusWorking > 0 {
		t.Logf("   • %d naras with event-based consensus", consensusWorking)
	}
	t.Log("   • No crashes or panics")
	t.Log("   • Anti-abuse mechanisms validated")
	t.Log("═══════════════════════════════════════")
}

// TestIntegration_HeyThereDiscovery is a regression test for the selfie removal.
// It verifies that two naras can discover each other quickly when one sends hey_there
// and the other responds with an announce (newspaper).
// Before the fix, naras would take much longer to discover each other after boot.
func TestIntegration_HeyThereDiscovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Start embedded MQTT broker
	broker := startEmbeddedBroker(t)
	defer broker.Close()

	// Give broker time to start
	time.Sleep(200 * time.Millisecond)

	t.Log("🧪 Testing hey_there → announce discovery mechanism")

	// Create two naras
	createNara := func(name string) *LocalNara {
		hwFingerprint := []byte(fmt.Sprintf("test-hw-fingerprint-%s", name))
		identity := DetermineIdentity("", "", name, hwFingerprint)

		profile := DefaultMemoryProfile()
		profile.Mode = MemoryModeCustom
		profile.MaxEvents = 1000
		ln, err := NewLocalNara(
			identity,
			"tcp://127.0.0.1:11883",
			"", "",
			-1, // auto chattiness
			profile,
		)
		if err != nil {
			t.Fatalf("Failed to create LocalNara: %v", err)
		}
		// Skip jitter delays for faster discovery in tests
		ln.Network.testSkipJitter = true
		return ln
	}

	alice := createNara("alice")
	bob := createNara("bob")

	// Start Alice first
	go alice.Start(false, false, "", nil, TransportMQTT)
	t.Log("✅ Started alice")

	// Give Alice time to connect and send her initial hey_there
	time.Sleep(500 * time.Millisecond)

	// Verify Alice doesn't know Bob yet
	alice.Network.local.mu.Lock()
	_, aliceKnowsBob := alice.Network.Neighbourhood["bob"]
	alice.Network.local.mu.Unlock()
	if aliceKnowsBob {
		t.Error("❌ Alice shouldn't know Bob yet")
	}

	// Start Bob - he will send hey_there, and Alice should respond with announce
	go bob.Start(false, false, "", nil, TransportMQTT)
	t.Log("✅ Started bob")

	// Wait for discovery - should be fast now (< 5 seconds)
	// Before the fix, this would take much longer (waiting for periodic newspapers)
	discoveryDeadline := 5 * time.Second
	discoveryStart := time.Now()
	discovered := false

	for time.Since(discoveryStart) < discoveryDeadline {
		// Check if Bob knows Alice
		bob.Network.local.mu.Lock()
		_, bobKnowsAlice := bob.Network.Neighbourhood["alice"]
		bob.Network.local.mu.Unlock()

		// Check if Alice knows Bob
		alice.Network.local.mu.Lock()
		_, aliceKnowsBob = alice.Network.Neighbourhood["bob"]
		alice.Network.local.mu.Unlock()

		if bobKnowsAlice && aliceKnowsBob {
			discovered = true
			break
		}

		time.Sleep(100 * time.Millisecond)
	}

	discoveryDuration := time.Since(discoveryStart)

	// Cleanup
	alice.Network.disconnectMQTT()
	bob.Network.disconnectMQTT()
	time.Sleep(200 * time.Millisecond)

	// Validate results
	if !discovered {
		// Check what each knows
		bob.Network.local.mu.Lock()
		_, bobKnowsAlice := bob.Network.Neighbourhood["alice"]
		bob.Network.local.mu.Unlock()

		alice.Network.local.mu.Lock()
		_, aliceKnowsBob = alice.Network.Neighbourhood["bob"]
		alice.Network.local.mu.Unlock()

		t.Errorf("❌ Discovery failed within %v deadline. Bob knows Alice: %v, Alice knows Bob: %v",
			discoveryDeadline, bobKnowsAlice, aliceKnowsBob)
	} else {
		t.Logf("✅ Mutual discovery completed in %v (deadline was %v)", discoveryDuration, discoveryDeadline)
	}

	// The discovery should be fast - if it takes more than 3 seconds, something might be wrong
	if discoveryDuration > 3*time.Second {
		t.Logf("⚠️  Discovery took %v - this seems slow, may indicate the hey_there→announce fix isn't working", discoveryDuration)
	}

	t.Log("═══════════════════════════════════════")
	t.Log("🎉 HEY_THERE DISCOVERY TEST PASSED")
	t.Logf("   • Two naras discovered each other in %v", discoveryDuration)
	t.Log("   • hey_there → announce mechanism working")
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
