package nara

import (
	"testing"
	"time"
)

// TestCheckpoint_VoterIDsNotUsedAsNames verifies that checkpoint VoterIDs (which are
// nara IDs, not names) don't create ghost naras with IDs as names.
// This was a bug where GetActor() returned VoterIDs[0], causing the event processing
// loop to treat IDs as names and create ghost naras like "EGqUnthqW8bNDb5SzNzPkkyzJbQVnkqo2Z4hjL4nrTVg".
func TestCheckpoint_VoterIDsNotUsedAsNames(t *testing.T) {
	t.Parallel()

	ln := testLocalNaraWithParams(t, "test-nara", 50, 1000)
	network := ln.Network

	// Create a checkpoint event with a VoterID (nara ID, not name)
	voterID := NaraID("EGqUnthqW8bNDb5SzNzPkkyzJbQVnkqo2Z4hjL4nrTVg") // Example nara ID from production
	voterName := NaraName("alice")                                      // The actual nara name

	// Import the real nara with proper name
	alice := NewNara(voterName)
	alice.ID = voterID
	alice.Status.ID = voterID
	network.importNara(alice)

	// Create a checkpoint event with VoterID (ID, not name)
	checkpoint := &CheckpointEventPayload{
		Subject:   "subject-nara",
		SubjectID: NaraID("subject-id"),
		Observation: NaraObservation{
			Restarts:    5,
			TotalUptime: 86400,
			StartTime:   time.Now().Unix() - 86400,
		},
		VoterIDs:   []NaraID{voterID}, // This is an ID, not a name!
		Signatures: []string{"fake-signature"},
		AsOfTime:   time.Now().Unix(),
		Round:      1,
	}

	event := SyncEvent{
		Timestamp:  time.Now().UnixNano(),
		Service:    ServiceCheckpoint,
		Checkpoint: checkpoint,
		Emitter:    string(voterName), // Emitter should be the name
	}
	event.ComputeID()

	// Process the event through the discovery pipeline
	network.discoverNarasFromEvents([]SyncEvent{event})

	// Verify that we did NOT create a ghost nara with the VoterID as name
	network.local.mu.Lock()
	_, ghostExists := network.Neighbourhood[NaraName(voterID)]
	_, realExists := network.Neighbourhood[voterName]
	network.local.mu.Unlock()

	if ghostExists {
		t.Errorf("❌ BUG: Created ghost nara with ID as name: %s", voterID)
		t.Errorf("This means GetActor() is still returning VoterIDs instead of empty string")
	}

	if !realExists {
		t.Error("Real nara (alice) should still exist in neighbourhood")
	}

	// Also verify GetActor() returns empty string for checkpoint events
	if actor := checkpoint.GetActor(); actor != "" {
		t.Errorf("CheckpointEventPayload.GetActor() should return empty string, got: %s", actor)
	}
}

// TestCheckpoint_GetActorReturnsEmpty verifies the fix at the interface level
func TestCheckpoint_GetActorReturnsEmpty(t *testing.T) {
	t.Parallel()

	checkpoint := &CheckpointEventPayload{
		Subject:   "subject",
		SubjectID: NaraID("subject-id"),
		Observation: NaraObservation{
			Restarts:    5,
			TotalUptime: 86400,
			StartTime:   time.Now().Unix(),
		},
		VoterIDs:   []NaraID{NaraID("voter-id-1"), NaraID("voter-id-2")}, // IDs, not names
		Signatures: []string{"sig1", "sig2"},
		AsOfTime:   time.Now().Unix(),
		Round:      1,
	}

	// GetActor() should return empty string to prevent IDs from being used as names
	actor := checkpoint.GetActor()
	if actor != "" {
		t.Errorf("Expected GetActor() to return empty string, got: %s", actor)
		t.Error("This would cause VoterIDs (nara IDs) to be treated as names in event processing")
	}

	// GetTarget() should still work (returns Subject)
	target := checkpoint.GetTarget()
	if target != "subject" {
		t.Errorf("Expected GetTarget() to return 'subject', got: %s", target)
	}
}
