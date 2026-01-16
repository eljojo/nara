package nara

import (
	"bytes"
	"testing"
	"time"

	"github.com/eljojo/nara/types"
	"github.com/sirupsen/logrus"
)

// TestCheckpointFilterLogging verifies that filtering logs messages
func TestCheckpointFilterLogging(t *testing.T) {
	// Set up log capture
	var buf bytes.Buffer
	logrus.SetOutput(&buf)
	logrus.SetLevel(logrus.DebugLevel)
	defer func() {
		logrus.SetLevel(logrus.WarnLevel)
		logrus.SetOutput(nil)
	}()

	ledger := NewSyncLedger(1000)

	// Create an old checkpoint
	oldCheckpoint := SyncEvent{
		Service:   ServiceCheckpoint,
		Timestamp: time.Now().UnixNano(),
		Checkpoint: &CheckpointEventPayload{
			Version:   1,
			Subject:   "alice",
			SubjectID: "test-alice-id",
			AsOfTime:  CheckpointCutoffTime - 1000,
			Observation: NaraObservation{
				Restarts:    5,
				TotalUptime: 86400,
				StartTime:   1639996062,
			},
			VoterIDs:   []types.NaraID{types.NaraID("voter1")},
			Signatures: []string{"sig1"},
			Round:      1,
		},
	}
	oldCheckpoint.ComputeID()

	// Try to add it
	ledger.AddEvent(oldCheckpoint)

	// Check that a log message was produced
	logOutput := buf.String()
	if logOutput == "" {
		t.Error("expected log output for filtered checkpoint")
	}
	if !bytes.Contains(buf.Bytes(), []byte("Filtered old checkpoint")) {
		t.Errorf("expected 'Filtered old checkpoint' in log, got: %s", logOutput)
	}
	if !bytes.Contains(buf.Bytes(), []byte("alice")) {
		t.Errorf("expected subject name 'alice' in log, got: %s", logOutput)
	}

	// Test batch filtering
	buf.Reset()

	oldCheckpoint2 := oldCheckpoint
	oldCheckpoint2.Checkpoint.Subject = "bob"
	oldCheckpoint2.ComputeID()

	oldCheckpoint3 := oldCheckpoint
	oldCheckpoint3.Checkpoint.Subject = "charlie"
	oldCheckpoint3.ComputeID()

	events := []SyncEvent{oldCheckpoint, oldCheckpoint2, oldCheckpoint3}
	ledger.MergeEvents(events)

	// Should see batch filter log
	logOutput = buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("Filtered 3 old checkpoint")) {
		t.Errorf("expected 'Filtered 3 old checkpoint' in log for batch, got: %s", logOutput)
	}
}
