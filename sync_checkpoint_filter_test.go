package nara

import (
	"testing"
	"time"

	"github.com/eljojo/nara/types"
)

// TestCheckpointIngestionFilter verifies that old buggy checkpoints are filtered out during ingestion
func TestCheckpointIngestionFilter(t *testing.T) {
	ledger := NewSyncLedger(1000)

	// Create an old checkpoint (before cutoff time) from r2d2
	oldCheckpoint := SyncEvent{
		Service:   ServiceCheckpoint,
		Timestamp: time.Now().UnixNano(),
		Emitter:   "r2d2", // Only r2d2's old checkpoints are filtered
		Checkpoint: &CheckpointEventPayload{
			Version:   1,
			Subject:   "alice",
			SubjectID: "test-alice-id",
			AsOfTime:  CheckpointCutoffTime - 1000, // 1000 seconds before cutoff
			Observation: NaraObservation{
				Restarts:    5,
				TotalUptime: 86400,
				StartTime:   1639996062,
			},
			VoterIDs:   []types.NaraID{types.NaraID("voter1"), types.NaraID("voter2")},
			Signatures: []string{"sig1", "sig2"},
			Round:      1,
		},
	}
	oldCheckpoint.ComputeID()

	// Create a new checkpoint (after cutoff time)
	newCheckpoint := SyncEvent{
		Service:   ServiceCheckpoint,
		Timestamp: time.Now().UnixNano(),
		Checkpoint: &CheckpointEventPayload{
			Version:   1,
			Subject:   "bob",
			SubjectID: "test-bob-id",
			AsOfTime:  CheckpointCutoffTime + 1000, // 1000 seconds after cutoff
			Observation: NaraObservation{
				Restarts:    10,
				TotalUptime: 172800,
				StartTime:   1639996062,
				LastRestart: time.Now().Unix(), // New field that old checkpoints don't have
			},
			VoterIDs:   []types.NaraID{types.NaraID("voter1"), types.NaraID("voter2")},
			Signatures: []string{"sig1", "sig2"},
			Round:      1,
		},
	}
	newCheckpoint.ComputeID()

	// Test AddEvent - old checkpoint should be filtered
	if ledger.AddEvent(oldCheckpoint) {
		t.Error("old checkpoint should be filtered out by AddEvent")
	}

	// New checkpoint should be added
	if !ledger.AddEvent(newCheckpoint) {
		t.Error("new checkpoint should be added")
	}

	// Verify only new checkpoint exists
	if ledger.EventCount() != 1 {
		t.Errorf("expected 1 event (new checkpoint), got %d", ledger.EventCount())
	}

	checkpoint := ledger.GetCheckpoint("bob")
	if checkpoint == nil {
		t.Fatal("new checkpoint should be retrievable")
	}
	if checkpoint.Subject != "bob" {
		t.Errorf("expected checkpoint for bob, got %s", checkpoint.Subject)
	}

	// Test MergeEvents - batch ingestion with mix of old and new
	ledger2 := NewSyncLedger(1000)

	// Create another old checkpoint from r2d2
	oldCheckpoint2 := SyncEvent{
		Service:   ServiceCheckpoint,
		Timestamp: time.Now().UnixNano(),
		Emitter:   "r2d2", // Only r2d2's old checkpoints are filtered
		Checkpoint: &CheckpointEventPayload{
			Version:   1,
			Subject:   "charlie",
			SubjectID: "test-charlie-id",
			AsOfTime:  CheckpointCutoffTime - 5000, // Way before cutoff
			Observation: NaraObservation{
				Restarts:    3,
				TotalUptime: 43200,
				StartTime:   1639996062,
			},
			VoterIDs:   []types.NaraID{types.NaraID("voter1")},
			Signatures: []string{"sig1"},
			Round:      1,
		},
	}
	oldCheckpoint2.ComputeID()

	// Merge array with 2 old checkpoints and 1 new checkpoint
	events := []SyncEvent{oldCheckpoint, oldCheckpoint2, newCheckpoint}
	added := ledger2.MergeEvents(events)

	// Only 1 should be added (the new one)
	if added != 1 {
		t.Errorf("expected 1 event added (filtered 2 old checkpoints), got %d", added)
	}

	if ledger2.EventCount() != 1 {
		t.Errorf("expected 1 event in ledger after merge, got %d", ledger2.EventCount())
	}

	// Verify it's the new checkpoint
	checkpoint2 := ledger2.GetCheckpoint("bob")
	if checkpoint2 == nil {
		t.Fatal("new checkpoint should be in ledger after merge")
	}

	// Old checkpoints should not be in ledger
	if ledger2.GetCheckpoint("alice") != nil {
		t.Error("old checkpoint for alice should be filtered")
	}
	if ledger2.GetCheckpoint("charlie") != nil {
		t.Error("old checkpoint for charlie should be filtered")
	}
}

// TestCheckpointCutoffBoundary verifies that the cutoff time is applied correctly
func TestCheckpointCutoffBoundary(t *testing.T) {
	ledger := NewSyncLedger(1000)

	// Checkpoint exactly at cutoff time from r2d2 (should be filtered)
	atCutoff := SyncEvent{
		Service:   ServiceCheckpoint,
		Timestamp: time.Now().UnixNano(),
		Emitter:   "r2d2", // Only r2d2's old checkpoints are filtered
		Checkpoint: &CheckpointEventPayload{
			Version:   1,
			Subject:   "boundary-test",
			SubjectID: "test-id",
			AsOfTime:  CheckpointCutoffTime, // Exactly at cutoff
			Observation: NaraObservation{
				Restarts:    1,
				TotalUptime: 1000,
				StartTime:   time.Now().Unix(),
			},
			VoterIDs:   []types.NaraID{types.NaraID("voter1")},
			Signatures: []string{"sig1"},
			Round:      1,
		},
	}
	atCutoff.ComputeID()

	// Should be filtered (< not <=)
	if ledger.AddEvent(atCutoff) {
		t.Error("checkpoint at exact cutoff time should be filtered")
	}

	// Checkpoint 1 second after cutoff (should NOT be filtered)
	afterCutoff := atCutoff
	afterCutoff.Checkpoint.AsOfTime = CheckpointCutoffTime + 1
	afterCutoff.ComputeID()

	if !ledger.AddEvent(afterCutoff) {
		t.Error("checkpoint after cutoff time should be accepted")
	}
}

// TestNonCheckpointEventsUnaffected verifies that filtering only affects checkpoints
func TestNonCheckpointEventsUnaffected(t *testing.T) {
	ledger := NewSyncLedger(1000)

	// Create events of other types (should not be filtered)
	socialEvent := NewSocialSyncEvent("tease", "alice", "bob", "high-restarts", "")
	pingEvent := SyncEvent{
		Service:   ServicePing,
		Timestamp: time.Now().UnixNano(),
		Ping: &PingObservation{
			Observer: "alice",
			Target:   "bob",
			RTT:      42.5,
		},
	}
	pingEvent.ComputeID()

	observationEvent := NewRestartObservationEvent("alice", "bob", time.Now().Unix(), 5)

	// All should be added regardless of timestamp
	if !ledger.AddEvent(socialEvent) {
		t.Error("social event should be added")
	}
	if !ledger.AddEvent(pingEvent) {
		t.Error("ping event should be added")
	}
	if !ledger.AddEvent(observationEvent) {
		t.Error("observation event should be added")
	}

	if ledger.EventCount() != 3 {
		t.Errorf("expected 3 events, got %d", ledger.EventCount())
	}
}
