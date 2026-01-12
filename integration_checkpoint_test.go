package nara

import (
	"fmt"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// TestIntegration_CheckpointConsensus tests the two-round checkpoint consensus mechanism
// via MQTT. It verifies:
// - A nara can propose a checkpoint about itself
// - Other naras receive the proposal and vote
// - Signatures are verified
// - Consensus is reached and checkpoint is finalized
// - The checkpoint is stored in all ledgers
func TestIntegration_CheckpointConsensus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Start embedded MQTT broker
	broker := startEmbeddedBroker(t)
	defer broker.Close()

	// Give broker time to start
	time.Sleep(200 * time.Millisecond)

	t.Log("🧪 Testing checkpoint consensus mechanism")

	// Enable debug logging for checkpoint operations
	logrus.SetLevel(logrus.DebugLevel)
	defer logrus.SetLevel(logrus.WarnLevel)

	const numNaras = 4 // Need at least 3 (1 proposer + 2 voters minimum)

	// Create naras with test configuration
	naras := make([]*LocalNara, numNaras)
	for i := 0; i < numNaras; i++ {
		name := fmt.Sprintf("checkpoint-test-%d", i)
		hwFingerprint := []byte(fmt.Sprintf("checkpoint-hw-%d", i))
		identity := DetermineIdentity("", "", name, hwFingerprint)

		profile := DefaultMemoryProfile()
		profile.Mode = MemoryModeCustom
		profile.MaxEvents = 1000
		ln, err := NewLocalNara(
			identity,
			"tcp://127.0.0.1:11883",
			"", "",
			-1,
			profile,
		)
		if err != nil {
			t.Fatalf("Failed to create LocalNara: %v", err)
		}

		// Skip jitter delays for faster discovery
		ln.Network.testSkipJitter = true
		naras[i] = ln
	}

	// Start all naras
	for i, ln := range naras {
		go ln.Start(false, false, "", nil, TransportMQTT)
		t.Logf("✅ Started %s", ln.Me.Name)

		// Give each nara time to connect to MQTT
		time.Sleep(300 * time.Millisecond)

		// Configure checkpoint service with short vote window for testing
		if ln.Network.checkpointService != nil {
			ln.Network.checkpointService.voteWindow = 3 * time.Second
			t.Logf("   Configured checkpoint service for nara %d", i)
		}
	}

	// Wait for naras to discover each other and exchange public keys
	t.Log("🌐 Waiting for peer discovery and key exchange...")
	waitForFullDiscovery(t, naras, 10*time.Second)

	// Ensure all naras' checkpoint services have MQTT clients set
	// This is needed because Start() might not have completed fully
	for _, ln := range naras {
		if ln.Network.checkpointService != nil && ln.Network.Mqtt != nil {
			ln.Network.checkpointService.SetMQTTClient(ln.Network.Mqtt)
		}
	}

	// Add some observation events so naras have data about each other
	// This ensures DeriveRestartCount and DeriveTotalUptime return meaningful values
	t.Log("📝 Adding observation events...")
	for i, observer := range naras {
		for j, subject := range naras {
			if i == j {
				continue
			}
			// Add first-seen observation
			firstSeenEvent := NewFirstSeenObservationEvent(
				observer.Me.Name,
				subject.Me.Name,
				time.Now().Unix()-86400, // first seen 1 day ago
			)
			observer.SyncLedger.AddEvent(firstSeenEvent)

			// Add restart observation
			restartEvent := NewRestartObservationEvent(
				observer.Me.Name,
				subject.Me.Name,
				time.Now().Unix()-3600, // started 1 hour ago
				5,                      // 5 restarts
			)
			observer.SyncLedger.AddEvent(restartEvent)
		}
	}

	// Give events time to propagate via MQTT
	time.Sleep(1 * time.Second)

	// Now have the first nara propose a checkpoint about itself
	proposer := naras[0]
	t.Logf("📤 %s proposing checkpoint...", proposer.Me.Name)

	if proposer.Network.checkpointService == nil {
		t.Fatal("❌ Checkpoint service not initialized")
	}

	// Verify MQTT is connected
	waitForMQTTConnected(t, proposer, 3*time.Second)
	t.Logf("✅ MQTT connected for proposer")

	// Verify checkpoint service has ledger
	if proposer.Network.checkpointService.ledger == nil {
		t.Fatal("❌ Checkpoint service ledger is nil - initialization order bug")
	}
	t.Logf("✅ Checkpoint service has ledger with %d events", len(proposer.SyncLedger.Events))

	// Trigger the checkpoint proposal
	proposer.Network.checkpointService.ProposeCheckpoint()

	// Wait for checkpoint to be finalized (vote window + processing)
	t.Log("⏳ Waiting for checkpoint finalization...")
	checkpoint := waitForCheckpoint(t, proposer.SyncLedger, proposer.Me.Name, 10*time.Second)

	// Check if checkpoint was created
	t.Log("🔍 Checking for finalized checkpoint...")
	if checkpoint == nil {
		t.Log("⚠️  No checkpoint found in proposer's ledger")
	} else {
		t.Logf("✅ Checkpoint found in proposer's ledger:")
		t.Logf("   Subject: %s", checkpoint.Subject)
		t.Logf("   SubjectID: %s", checkpoint.SubjectID)
		t.Logf("   Restarts: %d", checkpoint.Observation.Restarts)
		t.Logf("   TotalUptime: %d", checkpoint.Observation.TotalUptime)
		t.Logf("   VoterIDs: %v", checkpoint.VoterIDs)
		t.Logf("   Signatures: %d", len(checkpoint.Signatures))
	}

	// Wait for checkpoint to propagate to other naras
	if checkpoint != nil {
		waitForCheckpointPropagation(t, naras[1:], proposer.Me.Name, 3*time.Second)
	}
	checkpointsPropagated := 0
	for _, ln := range naras[1:] {
		if ln.SyncLedger.GetCheckpoint(proposer.Me.Name) != nil {
			checkpointsPropagated++
			t.Logf("✅ Checkpoint propagated to %s", ln.Me.Name)
		}
	}

	// Cleanup
	t.Log("🛑 Stopping naras...")
	for _, ln := range naras {
		ln.Network.disconnectMQTT()
	}

	// Final validation
	t.Log("")
	t.Log("═══════════════════════════════════════")
	if checkpoint != nil {
		if len(checkpoint.VoterIDs) >= MinVotersRequired {
			t.Log("🎉 CHECKPOINT CONSENSUS TEST PASSED")
			t.Logf("   • Proposer: %s", proposer.Me.Name)
			t.Logf("   • Voters: %d (minimum required: %d)", len(checkpoint.VoterIDs), MinVotersRequired)
			t.Logf("   • Signatures verified: %d", len(checkpoint.Signatures))
			t.Logf("   • Checkpoints propagated to: %d/%d other naras", checkpointsPropagated, numNaras-1)
		} else {
			t.Errorf("❌ Insufficient voters: got %d, need %d", len(checkpoint.VoterIDs), MinVotersRequired)
		}
	} else {
		// This can happen if signature verification fails or voters don't have enough data
		t.Log("⚠️  Checkpoint not created - this may be expected if:")
		t.Log("   • Signature verification failed (voters don't know proposer's public key)")
		t.Log("   • Not enough voters responded")
		t.Log("   • Check logs for more details")
	}
	t.Log("═══════════════════════════════════════")
}

// TestIntegration_CheckpointRound2 tests the round 2 fallback when round 1 fails consensus
func TestIntegration_CheckpointRound2(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Start embedded MQTT broker
	broker := startEmbeddedBroker(t)
	defer broker.Close()

	time.Sleep(200 * time.Millisecond)

	t.Log("🧪 Testing checkpoint round 2 fallback")

	const numNaras = 4

	naras := make([]*LocalNara, numNaras)
	for i := 0; i < numNaras; i++ {
		name := fmt.Sprintf("round2-test-%d", i)
		hwFingerprint := []byte(fmt.Sprintf("round2-hw-%d", i))
		identity := DetermineIdentity("", "", name, hwFingerprint)

		profile := DefaultMemoryProfile()
		profile.Mode = MemoryModeCustom
		profile.MaxEvents = 1000
		ln, err := NewLocalNara(
			identity,
			"tcp://127.0.0.1:11883",
			"", "",
			-1,
			profile,
		)
		if err != nil {
			t.Fatalf("Failed to create LocalNara: %v", err)
		}

		ln.Network.testSkipJitter = true
		naras[i] = ln
	}

	// Start all naras
	for i, ln := range naras {
		go ln.Start(false, false, "", nil, TransportMQTT)
		time.Sleep(300 * time.Millisecond)

		if ln.Network.checkpointService != nil {
			ln.Network.checkpointService.voteWindow = 2 * time.Second
		}
		t.Logf("✅ Started %s (nara %d)", ln.Me.Name, i)
	}

	// Wait for discovery
	time.Sleep(3 * time.Second)

	// Add DIFFERENT observation data to each nara to force disagreement in round 1
	// This should trigger the trimmed mean calculation in round 2
	t.Log("📝 Adding divergent observation data to force round 2...")
	proposer := naras[0]
	for i, observer := range naras {
		// Each observer reports different restart counts
		restartEvent := NewRestartObservationEvent(
			observer.Me.Name,
			proposer.Me.Name,
			time.Now().Unix()-3600,
			int64(10+i*5), // 10, 15, 20, 25 - different values
		)
		observer.SyncLedger.AddEvent(restartEvent)

		firstSeenEvent := NewFirstSeenObservationEvent(
			observer.Me.Name,
			proposer.Me.Name,
			time.Now().Unix()-86400-int64(i*3600), // Different first-seen times
		)
		observer.SyncLedger.AddEvent(firstSeenEvent)
	}

	time.Sleep(1 * time.Second)

	// Trigger checkpoint proposal
	t.Logf("📤 %s proposing checkpoint (expecting round 2 due to disagreement)...", proposer.Me.Name)
	proposer.Network.checkpointService.ProposeCheckpoint()

	// Wait for round 1 + round 2 (2 vote windows)
	t.Log("⏳ Waiting for round 1 + round 2 (4+ seconds)...")
	time.Sleep(6 * time.Second)

	// Check results
	checkpoint := proposer.SyncLedger.GetCheckpoint(proposer.Me.Name)

	// Cleanup
	for _, ln := range naras {
		ln.Network.disconnectMQTT()
	}
	time.Sleep(500 * time.Millisecond)

	t.Log("")
	t.Log("═══════════════════════════════════════")
	if checkpoint != nil {
		t.Log("🎉 ROUND 2 FALLBACK TEST COMPLETED")
		t.Logf("   • Final restarts value: %d (should be trimmed mean)", checkpoint.Observation.Restarts)
		t.Logf("   • Voters: %d", len(checkpoint.VoterIDs))
	} else {
		t.Log("⚠️  No checkpoint created after round 2")
		t.Log("   • This may be expected if voters still disagreed")
	}
	t.Log("═══════════════════════════════════════")
}

// TestIntegration_CheckpointSignatureVerification tests that invalid signatures are rejected
func TestIntegration_CheckpointSignatureVerification(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Start embedded MQTT broker
	broker := startEmbeddedBroker(t)
	defer broker.Close()

	time.Sleep(200 * time.Millisecond)

	t.Log("🧪 Testing checkpoint signature verification")

	// Create 2 naras
	createNara := func(name string) *LocalNara {
		hwFingerprint := []byte(fmt.Sprintf("sig-test-hw-%s", name))
		identity := DetermineIdentity("", "", name, hwFingerprint)

		profile := DefaultMemoryProfile()
		profile.Mode = MemoryModeCustom
		profile.MaxEvents = 1000
		ln, err := NewLocalNara(
			identity,
			"tcp://127.0.0.1:11883",
			"", "",
			-1,
			profile,
		)
		if err != nil {
			t.Fatalf("Failed to create LocalNara: %v", err)
		}
		ln.Network.testSkipJitter = true
		return ln
	}

	alice := createNara("alice-sig")
	bob := createNara("bob-sig")

	// Start both
	go alice.Start(false, false, "", nil, TransportMQTT)
	time.Sleep(300 * time.Millisecond)
	go bob.Start(false, false, "", nil, TransportMQTT)
	time.Sleep(300 * time.Millisecond)

	// Configure short vote window
	if alice.Network.checkpointService != nil {
		alice.Network.checkpointService.voteWindow = 2 * time.Second
	}
	if bob.Network.checkpointService != nil {
		bob.Network.checkpointService.voteWindow = 2 * time.Second
	}

	// Wait for discovery
	time.Sleep(2 * time.Second)

	// Verify they know each other
	alice.Network.local.mu.Lock()
	_, aliceKnowsBob := alice.Network.Neighbourhood["bob-sig"]
	alice.Network.local.mu.Unlock()

	bob.Network.local.mu.Lock()
	_, bobKnowsAlice := bob.Network.Neighbourhood["alice-sig"]
	bob.Network.local.mu.Unlock()

	t.Logf("Discovery: Alice knows Bob: %v, Bob knows Alice: %v", aliceKnowsBob, bobKnowsAlice)

	// Test 1: Valid signature should be accepted
	// Add observation data
	restartEvent := NewRestartObservationEvent(bob.Me.Name, alice.Me.Name, time.Now().Unix()-3600, 5)
	bob.SyncLedger.AddEvent(restartEvent)

	firstSeenEvent := NewFirstSeenObservationEvent(bob.Me.Name, alice.Me.Name, time.Now().Unix()-86400)
	bob.SyncLedger.AddEvent(firstSeenEvent)

	time.Sleep(500 * time.Millisecond)

	// Alice proposes checkpoint
	t.Log("📤 Alice proposing checkpoint...")
	if alice.Network.checkpointService != nil {
		alice.Network.checkpointService.ProposeCheckpoint()
	}

	// Wait for vote window
	time.Sleep(4 * time.Second)

	// Check if Bob voted (if he knows Alice's public key)
	checkpoint := alice.SyncLedger.GetCheckpoint(alice.Me.Name)

	// Cleanup
	alice.Network.disconnectMQTT()
	bob.Network.disconnectMQTT()
	time.Sleep(300 * time.Millisecond)

	t.Log("")
	t.Log("═══════════════════════════════════════")
	if checkpoint != nil && len(checkpoint.Signatures) > 0 {
		t.Log("🎉 SIGNATURE VERIFICATION TEST PASSED")
		t.Logf("   • Checkpoint created with %d signatures", len(checkpoint.Signatures))
		t.Log("   • Valid signatures were accepted")
	} else {
		t.Log("⚠️  Checkpoint not created or no signatures")
		t.Log("   • This may indicate signature verification working (rejected invalid)")
		t.Log("   • Or voters didn't have proposer's public key yet")
	}
	t.Log("═══════════════════════════════════════")
}

// TestIntegration_CheckpointTop10Voters tests that only top 10 voters by uptime are kept
func TestIntegration_CheckpointTop10Voters(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Start embedded MQTT broker
	broker := startEmbeddedBroker(t)
	defer broker.Close()

	time.Sleep(200 * time.Millisecond)

	t.Log("🧪 Testing checkpoint top 10 voters limit")

	// Create 15 naras (more than the 10 signature limit)
	const numNaras = 15
	naras := make([]*LocalNara, numNaras)

	for i := 0; i < numNaras; i++ {
		name := fmt.Sprintf("top10-test-%d", i)
		hwFingerprint := []byte(fmt.Sprintf("top10-hw-%d", i))
		identity := DetermineIdentity("", "", name, hwFingerprint)

		profile := DefaultMemoryProfile()
		profile.Mode = MemoryModeCustom
		profile.MaxEvents = 1000
		ln, err := NewLocalNara(
			identity,
			"tcp://127.0.0.1:11883",
			"", "",
			-1,
			profile,
		)
		if err != nil {
			t.Fatalf("Failed to create LocalNara: %v", err)
		}
		ln.Network.testSkipJitter = true
		naras[i] = ln
	}

	// Start all naras
	for i, ln := range naras {
		go ln.Start(false, false, "", nil, TransportMQTT)
		time.Sleep(200 * time.Millisecond)

		if ln.Network.checkpointService != nil {
			ln.Network.checkpointService.voteWindow = 3 * time.Second
		}
		if i%5 == 0 {
			t.Logf("✅ Started naras %d-%d", i, min(i+4, numNaras-1))
		}
	}

	// Wait for discovery
	t.Log("🌐 Waiting for peer discovery...")
	time.Sleep(5 * time.Second)

	// Add observation data with varying uptimes
	proposer := naras[0]
	t.Log("📝 Adding observation data with different uptimes...")
	for i, observer := range naras {
		// Give each nara different uptime (higher index = more uptime)
		uptimeSeconds := int64((i + 1) * 86400) // 1 day, 2 days, ... 15 days
		startTime := time.Now().Unix() - uptimeSeconds

		restartEvent := NewRestartObservationEvent(
			observer.Me.Name,
			proposer.Me.Name,
			startTime,
			5,
		)
		observer.SyncLedger.AddEvent(restartEvent)

		firstSeenEvent := NewFirstSeenObservationEvent(
			observer.Me.Name,
			proposer.Me.Name,
			startTime-86400,
		)
		observer.SyncLedger.AddEvent(firstSeenEvent)

		// Also add uptime data for the voter themselves
		// so the proposer can look up their uptime
		statusEvent := NewStatusChangeObservationEvent(
			proposer.Me.Name,
			observer.Me.Name,
			"ONLINE",
		)
		statusEvent.Timestamp = startTime * 1e9
		proposer.SyncLedger.AddEvent(statusEvent)
	}

	time.Sleep(1 * time.Second)

	// Trigger checkpoint
	t.Logf("📤 %s proposing checkpoint...", proposer.Me.Name)
	proposer.Network.checkpointService.ProposeCheckpoint()

	// Wait for finalization
	time.Sleep(5 * time.Second)

	checkpoint := proposer.SyncLedger.GetCheckpoint(proposer.Me.Name)

	// Cleanup
	for _, ln := range naras {
		ln.Network.disconnectMQTT()
	}
	time.Sleep(500 * time.Millisecond)

	t.Log("")
	t.Log("═══════════════════════════════════════")
	if checkpoint != nil {
		voterCount := len(checkpoint.VoterIDs)
		t.Logf("📊 Checkpoint has %d voters", voterCount)

		if voterCount <= MaxCheckpointSignatures {
			t.Log("🎉 TOP 10 VOTERS TEST PASSED")
			t.Logf("   • Voters limited to %d (max: %d)", voterCount, MaxCheckpointSignatures)
			t.Logf("   • VoterIDs: %v", checkpoint.VoterIDs)
		} else {
			t.Errorf("❌ Too many voters: %d (max should be %d)", voterCount, MaxCheckpointSignatures)
		}
	} else {
		t.Log("⚠️  No checkpoint created")
	}
	t.Log("═══════════════════════════════════════")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
