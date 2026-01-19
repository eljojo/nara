//go:build !short
// +build !short

package nara

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// TestCheckpointV2DivergentReferencePointsNoConsensus tests that when naras have different
// LastSeenCheckpointIDs, they can't reach consensus (votes don't group together)
func TestCheckpointV2DivergentReferencePointsNoConsensus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Start embedded MQTT broker
	port := 11887
	broker := startTestMQTTBroker(t, port)
	defer broker.Close()

	// Give broker time to start
	time.Sleep(200 * time.Millisecond)

	naraNames := []string{"test-proposer", "voter1", "voter2", "voter3", "voter4", "voter5"}

	// Start all naras
	naras := startTestNaras(t, port, naraNames, true)

	// Set shorter vote window for faster testing
	for _, ln := range naras {
		ln.Network.checkpointService.voteWindow = 3 * time.Second
	}

	// Create different checkpoint histories for each nara by manually adding checkpoints
	// Each nara will have seen a different previous checkpoint
	// Use NewCheckpointEvent (not NewTestCheckpointEvent) with realistic timestamps
	// since our filter only affects r2d2's checkpoints, not these test ones
	for i, ln := range naras {
		// Create fake previous checkpoint with unique ID for each nara
		prevCheckpoint := NewCheckpointEvent("test-proposer", time.Now().Unix()-100000, time.Now().Unix()-200000, 10, 50000)
		// Modify the event ID to make it unique per nara (simulate different checkpoint history)
		prevCheckpoint.ID = prevCheckpoint.ID + "-nara-" + string(rune('a'+i))
		ln.SyncLedger.AddEvent(prevCheckpoint)
		logrus.Infof("Added fake checkpoint %s to %s's ledger", prevCheckpoint.ID, ln.Me.Name)
	}

	// Give time for checkpoint histories to settle
	time.Sleep(200 * time.Millisecond)

	// Trigger checkpoint proposal from first nara
	proposer := naras[0]
	proposer.Network.checkpointService.ProposeCheckpoint()

	// Wait for vote window + processing
	time.Sleep(2 * time.Second)

	// Verify NO checkpoint was created (no consensus reached)
	checkpoint := proposer.SyncLedger.GetCheckpoint("test-proposer")

	// The old checkpoint should still be there, but no NEW checkpoint
	// We can tell by checking the AsOfTime - new one would be much more recent
	if checkpoint != nil && checkpoint.AsOfTime > time.Now().Unix()-50 {
		t.Error("Expected no new checkpoint due to divergent reference points, but found one")
	}

	logrus.Info("✅ Divergent reference points correctly prevented consensus")

	// Cleanup
	for _, ln := range naras {
		ln.Network.Shutdown()
	}
}

// TestCheckpointV2NetworkDisagreesWithProposer tests that when the proposer has a different
// reference point than all voters, but voters agree among themselves, checkpoint is still emitted.
func TestCheckpointV2NetworkDisagreesWithProposer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Start embedded MQTT broker
	port := 11888
	broker := startTestMQTTBroker(t, port)
	defer broker.Close()

	// Give broker time to start
	time.Sleep(200 * time.Millisecond)

	naraNames := []string{"test-proposer", "voter1", "voter2", "voter3", "voter4", "voter5"}

	// Start all naras
	naras := startTestNaras(t, port, naraNames, true)

	proposer := naras[0]
	voters := naras[1:]

	// Set shorter vote window for faster testing
	for _, ln := range naras {
		ln.Network.checkpointService.voteWindow = 3 * time.Second
	}

	// Give proposer a unique previous checkpoint
	// Use NewCheckpointEvent (not NewTestCheckpointEvent) with realistic timestamps
	// since our filter only affects r2d2's checkpoints, not these test ones
	proposerPrevCheckpoint := NewCheckpointEvent("test-proposer", time.Now().Unix()-100000, time.Now().Unix()-200000, 10, 50000)
	proposerPrevCheckpoint.ID = proposerPrevCheckpoint.ID + "-proposer-unique"
	proposer.SyncLedger.AddEvent(proposerPrevCheckpoint)
	logrus.Infof("Added proposer checkpoint %s", proposerPrevCheckpoint.ID)

	// Give all voters the SAME previous checkpoint (different from proposer)
	voterPrevCheckpoint := NewCheckpointEvent("test-proposer", time.Now().Unix()-100000, time.Now().Unix()-200000, 10, 50000)
	voterPrevCheckpoint.ID = voterPrevCheckpoint.ID + "-voter-shared"
	for _, voter := range voters {
		voter.SyncLedger.AddEvent(voterPrevCheckpoint)
	}
	logrus.Infof("Added shared checkpoint %s to all voters", voterPrevCheckpoint.ID)

	// Give time for checkpoint histories to settle
	time.Sleep(200 * time.Millisecond)

	// Enable debug logging to see what's happening
	logrus.SetLevel(logrus.DebugLevel)
	defer logrus.SetLevel(logrus.WarnLevel)

	// Trigger checkpoint proposal
	proposer.Network.checkpointService.ProposeCheckpoint()

	// Wait for a recent v2 checkpoint to be created (voters formed consensus)
	checkpoint := waitForRecentCheckpoint(t, proposer.SyncLedger, "test-proposer", 10*time.Second, 15*time.Second)
	if checkpoint == nil {
		t.Fatal("Expected checkpoint to be created with voter consensus")
	}

	// Verify it has v2 format with PreviousCheckpointID
	if checkpoint.Version != 2 {
		t.Errorf("Expected v2 checkpoint, got version %d", checkpoint.Version)
	}

	// The previous checkpoint ID should be the voters' shared ID (not proposer's)
	if checkpoint.PreviousCheckpointID != voterPrevCheckpoint.ID {
		t.Errorf("Expected checkpoint to use voters' reference point %s, got %s",
			voterPrevCheckpoint.ID, checkpoint.PreviousCheckpointID)
	}

	// Verify proposer's signature is NOT included (different reference point)
	proposerIncluded := false
	for _, voterID := range checkpoint.VoterIDs {
		if voterID == proposer.Me.Status.ID {
			proposerIncluded = true
			break
		}
	}
	if proposerIncluded {
		t.Error("Proposer's signature should NOT be included (different reference point)")
	}

	logrus.Info("✅ Network consensus worked despite proposer disagreement")

	// Cleanup
	for _, ln := range naras {
		ln.Network.Shutdown()
	}
}

// TestCheckpointV1ToV2Chain tests that a new v2 checkpoint correctly points to an existing v1 checkpoint.
func TestCheckpointV1ToV2Chain(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Start embedded MQTT broker
	port := 11889
	broker := startTestMQTTBroker(t, port)
	defer broker.Close()

	// Give broker time to start
	time.Sleep(200 * time.Millisecond)

	naraNames := []string{"test-nara", "voter1", "voter2", "voter3", "voter4", "voter5"}

	// Start all naras
	naras := startTestNaras(t, port, naraNames, true)

	// Set shorter vote window for faster testing
	for _, ln := range naras {
		ln.Network.checkpointService.voteWindow = 3 * time.Second
	}

	testNara := naras[0]

	// Create a v1 checkpoint manually
	v1Checkpoint := NewTestCheckpointEvent("test-nara", time.Now().Unix()-100, time.Now().Unix()-200, 10, 50000)
	v1Checkpoint.Checkpoint.Version = 1
	v1Checkpoint.Checkpoint.PreviousCheckpointID = "" // v1 has no previous ID
	v1Checkpoint.ComputeID()                          // Recompute ID for v1 format

	// Add v1 checkpoint to all naras
	for _, ln := range naras {
		ln.SyncLedger.AddEvent(v1Checkpoint)
	}
	logrus.Infof("Added v1 checkpoint %s to all naras", v1Checkpoint.ID)

	// Give time for checkpoint to be processed
	time.Sleep(200 * time.Millisecond)

	// Verify v1 checkpoint exists
	existingCheckpoint := testNara.SyncLedger.GetCheckpoint("test-nara")
	if existingCheckpoint == nil {
		t.Fatal("V1 checkpoint not found")
	}
	if existingCheckpoint.Version != 1 {
		t.Errorf("Expected v1 checkpoint, got version %d", existingCheckpoint.Version)
	}

	// Trigger new checkpoint proposal (should create v2)
	testNara.Network.checkpointService.ProposeCheckpoint()

	// Wait for new v2 checkpoint to be created
	newCheckpoint := waitForCheckpointV2(t, testNara.SyncLedger, "test-nara", 15*time.Second)
	if newCheckpoint == nil {
		t.Fatal("Expected new v2 checkpoint to be created")
	}

	// Verify it points to the v1 checkpoint
	if newCheckpoint.PreviousCheckpointID != v1Checkpoint.ID {
		t.Errorf("Expected v2 checkpoint to point to v1 checkpoint %s, got %s",
			v1Checkpoint.ID, newCheckpoint.PreviousCheckpointID)
	}

	// Verify it's recent
	if newCheckpoint.AsOfTime < time.Now().Unix()-5 {
		t.Error("New checkpoint is too old")
	}

	logrus.Info("✅ V1 to V2 chain works correctly")

	// Cleanup
	for _, ln := range naras {
		ln.Network.Shutdown()
	}
}
