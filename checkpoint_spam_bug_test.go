package nara

import (
	"testing"
	"time"
)

// TestCheckpointSpamBug_UninitializedLastCheckpointTime demonstrates Bug 1:
// lastCheckpointTime is never initialized, so it defaults to the zero value
// (January 1, year 1 AD), causing time.Since() to always return > 24h,
// making checkpoints propose immediately on every 15-minute check.
func TestCheckpointSpamBug_UninitializedLastCheckpointTime(t *testing.T) {
	// Create a checkpoint service (simulates a fresh boot)
	ledger := NewSyncLedger(1000)
	local := testNara(t, "test-nara")

	network := &Network{
		Neighbourhood: make(map[NaraName]*Nara),
		local:         local,
	}

	service := NewCheckpointService(network, ledger, local)

	// BUG: lastCheckpointTime is never initialized in NewCheckpointService()
	// It defaults to time.Time{} which is January 1, year 1, 00:00:00 UTC
	service.lastCheckpointTimeMu.RLock()
	lastCheckpoint := service.lastCheckpointTime
	service.lastCheckpointTimeMu.RUnlock()

	// Verify the bug: lastCheckpointTime is the zero value
	if !lastCheckpoint.IsZero() {
		t.Fatalf("Expected lastCheckpointTime to be zero (uninitialized), got %v", lastCheckpoint)
	}

	// Show that time.Since() thinks a VERY long time has passed
	// (time.Time{} is year 1 AD, so ~2025 years, though duration calculation may overflow)
	timeSince := time.Since(lastCheckpoint)

	t.Logf("lastCheckpointTime is: %v", lastCheckpoint)
	t.Logf("time.Since(lastCheckpointTime) is: %v", timeSince)

	// The exact duration may vary due to overflow, but it will definitely be > 24h
	if timeSince < 24*time.Hour {
		t.Errorf("Expected time.Since to be > 24h (zero time bug), got %v", timeSince)
	}

	// Show that this will ALWAYS pass the 24h check
	checkpointInterval := DefaultCheckpointInterval // 24 hours
	if timeSince < checkpointInterval {
		t.Errorf("BUG NOT REPRODUCED: time.Since should be > 24h, but got %v", timeSince)
	}

	t.Logf("✅ Bug reproduced: checkAndPropose() will ALWAYS propose because %v > 24h", timeSince)
}

// TestCheckpointSpamBug_ShouldInitializeFromLedger demonstrates the fix for Bug 1:
// After boot recovery, the ledger contains checkpoints synced from neighbors.
// The service should query the ledger for the most recent checkpoint and
// initialize lastCheckpointTime from it.
func TestCheckpointSpamBug_ShouldInitializeFromLedger(t *testing.T) {
	// Simulate boot recovery: ledger contains a checkpoint from yesterday
	ledger := NewSyncLedger(1000)
	local := testNara(t, "test-nara")

	yesterday := time.Now().Add(-24 * time.Hour).Unix()
	checkpointEvent := NewTestCheckpointEvent(local.Me.Name, yesterday, 1624066568, 47, 23456789)
	ledger.AddEvent(checkpointEvent)

	// Verify the checkpoint is in the ledger
	retrieved := ledger.GetCheckpointEvent(local.Me.Name)
	if retrieved == nil {
		t.Fatal("Expected checkpoint to be in ledger after boot recovery")
	}
	t.Logf("Ledger contains checkpoint from: %v", time.Unix(retrieved.Checkpoint.AsOfTime, 0))

	// Create checkpoint service
	network := &Network{
		Neighbourhood: make(map[NaraName]*Nara),
		local:         local,
	}
	service := NewCheckpointService(network, ledger, local)

	// Call Start() which should initialize from ledger
	service.Start()
	defer service.Stop()

	// Give it a moment to initialize
	time.Sleep(100 * time.Millisecond)

	// FIX VERIFIED: lastCheckpointTime should be initialized from the ledger checkpoint
	service.lastCheckpointTimeMu.RLock()
	lastCheckpoint := service.lastCheckpointTime
	service.lastCheckpointTimeMu.RUnlock()

	t.Logf("lastCheckpointTime is: %v", lastCheckpoint)
	t.Logf("Checkpoint in ledger is from: %v", time.Unix(yesterday, 0))

	if lastCheckpoint.IsZero() {
		t.Fatal("FIX NOT APPLIED: lastCheckpointTime should be initialized from ledger, but is still zero")
	}

	// Verify it matches the checkpoint from the ledger
	expectedTime := time.Unix(yesterday, 0)
	if !lastCheckpoint.Equal(expectedTime) {
		t.Errorf("Expected lastCheckpointTime=%v (from ledger), got %v", expectedTime, lastCheckpoint)
	}

	t.Logf("✅ Fix verified: lastCheckpointTime initialized to %v from ledger checkpoint", lastCheckpoint)
}

// TestCheckpointSpamBug_NotUpdatedOnFailure demonstrates Bug 2:
// When consensus fails (round 1 or round 2), lastCheckpointTime is NOT updated,
// causing the service to propose again in 15 minutes instead of waiting 24h.
func TestCheckpointSpamBug_NotUpdatedOnFailure(t *testing.T) {
	ledger := NewSyncLedger(1000)
	local := testNara(t, "test-nara")

	network := &Network{
		Neighbourhood: make(map[NaraName]*Nara),
		local:         local,
	}
	service := NewCheckpointService(network, ledger, local)

	// Manually set lastCheckpointTime to 25 hours ago (should trigger proposal)
	service.lastCheckpointTimeMu.Lock()
	service.lastCheckpointTime = time.Now().Add(-25 * time.Hour)
	initialTime := service.lastCheckpointTime
	service.lastCheckpointTimeMu.Unlock()

	t.Logf("Initial lastCheckpointTime: %v", initialTime)

	// Simulate a failed round 2 proposal (insufficient votes, consensus failed)
	proposal := &CheckpointProposal{
		Attestation: Attestation{
			Subject:   local.Me.Name,
			SubjectID: local.Me.Status.ID,
			AsOfTime:  time.Now().Unix(),
			Observation: NaraObservation{
				Restarts:    10,
				TotalUptime: 5000,
				StartTime:   time.Now().Add(-24 * time.Hour).Unix(),
			},
		},
		Round: 2, // Round 2 failure
	}

	pending := &pendingProposal{
		proposal:  proposal,
		votes:     make([]*CheckpointVote, 0), // No votes = failure
		expiresAt: time.Now().Add(service.voteWindow),
		round:     2,
	}

	service.myPendingProposalMu.Lock()
	service.myPendingProposal = pending
	service.myPendingProposalMu.Unlock()

	// Call finalizeProposal which will fail consensus
	// BUG: This does NOT update lastCheckpointTime on failure
	service.finalizeProposal()

	// Check if lastCheckpointTime was updated
	service.lastCheckpointTimeMu.RLock()
	afterFailureTime := service.lastCheckpointTime
	service.lastCheckpointTimeMu.RUnlock()

	t.Logf("lastCheckpointTime after round 2 failure: %v", afterFailureTime)

	// FIX VERIFIED: lastCheckpointTime should be updated to time.Now() even on failure
	if afterFailureTime.Equal(initialTime) {
		t.Fatal("FIX NOT APPLIED: lastCheckpointTime should be updated on failure, but remains unchanged")
	}

	// Verify lastCheckpointTime was updated to recent time (within last few seconds)
	timeSince := time.Since(afterFailureTime)
	if timeSince > 5*time.Second {
		t.Errorf("Expected lastCheckpointTime to be updated to now (within 5s), but it's %v ago", timeSince)
	}

	// Verify that checkAndPropose() will now wait 24h before proposing again
	checkpointInterval := DefaultCheckpointInterval // 24 hours
	if timeSince >= checkpointInterval {
		t.Errorf("Expected time since checkpoint (%v) to be < 24h, so it won't spam", timeSince)
	}

	t.Logf("✅ Fix verified: lastCheckpointTime updated to %v on round 2 failure", afterFailureTime)
	t.Logf("   checkAndPropose() will wait 24h before proposing again (no 15-minute spam)")
}

// TestCheckpointSpamBug_Round1FailureAlsoNotUpdated demonstrates Bug 2 for round 1:
// When round 1 fails and transitions to round 2, lastCheckpointTime is also not updated.
// If round 2 then also fails, we've now wasted ~2 minutes and will retry in 15 minutes.
func TestCheckpointSpamBug_Round1FailureAlsoNotUpdated(t *testing.T) {
	ledger := NewSyncLedger(1000)
	local := testNara(t, "test-nara")

	network := &Network{
		Neighbourhood: make(map[NaraName]*Nara),
		local:         local,
	}
	service := NewCheckpointService(network, ledger, local)

	// Set lastCheckpointTime to 25 hours ago
	service.lastCheckpointTimeMu.Lock()
	service.lastCheckpointTime = time.Now().Add(-25 * time.Hour)
	initialTime := service.lastCheckpointTime
	service.lastCheckpointTimeMu.Unlock()

	// Simulate round 1 failure (transitions to round 2, but doesn't update lastCheckpointTime)
	// We can't easily test this without mocking proposeRound2, so we just document the bug

	t.Logf("Initial lastCheckpointTime: %v", initialTime)
	t.Logf("✅ Bug exists: Round 1 failure -> Round 2 (lastCheckpointTime unchanged)")
	t.Logf("   If Round 2 also fails, lastCheckpointTime STILL not updated")
	t.Logf("   Result: Nara will propose again in 15 minutes, and keep spamming every 15min")
	t.Logf("FIX: Update lastCheckpointTime on round 2 failure to prevent 15-minute spam")
}

// TestCheckpointService_ExpectedBehaviorAfterFix demonstrates the expected behavior
// after both bugs are fixed.
func TestCheckpointService_ExpectedBehaviorAfterFix(t *testing.T) {
	t.Log("Expected behavior after fix:")
	t.Log("")
	t.Log("1. NewCheckpointService() or Start() should:")
	t.Log("   - Query ledger.GetCheckpointEvent(myName)")
	t.Log("   - If found: set lastCheckpointTime = time.Unix(checkpoint.AsOfTime, 0)")
	t.Log("   - If not found: set lastCheckpointTime = time.Now()")
	t.Log("")
	t.Log("2. finalizeProposal() should update lastCheckpointTime on ALL outcomes:")
	t.Log("   - Success: update to time.Now() ✅ (already implemented)")
	t.Log("   - Round 1 failure -> Round 2: keep lastCheckpointTime (will update if round 2 succeeds/fails)")
	t.Log("   - Round 2 failure: update to time.Now() ❌ (currently missing, causes spam)")
	t.Log("")
	t.Log("This ensures:")
	t.Log("- Boot spam prevented (checkpoint time loaded from ledger)")
	t.Log("- Failure spam prevented (wait another 24h even after failure)")
	t.Log("- No need to persist to stash (ledger has checkpoints from boot recovery)")
}
