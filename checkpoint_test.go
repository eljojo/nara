package nara

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"
	"time"
)

// Helper to generate a keypair for testing
func generateTestKeypair() NaraKeypair {
	pub, priv, _ := ed25519.GenerateKey(nil)
	return NaraKeypair{PrivateKey: priv, PublicKey: pub}
}

// Helper to convert public key to base64 string for verification maps
func pubKeyToBase64(pub ed25519.PublicKey) string {
	return base64.StdEncoding.EncodeToString(pub)
}

// Test creating a basic checkpoint event
func TestCheckpoint_Create(t *testing.T) {
	subject := "lisa"
	asOfTime := time.Now().Unix()
	firstSeen := int64(1624066568)
	restarts := int64(47)
	totalUptime := int64(23456789)

	event := NewCheckpointEvent(subject, asOfTime, firstSeen, restarts, totalUptime)

	if event.Service != ServiceCheckpoint {
		t.Errorf("Expected service=%s, got %s", ServiceCheckpoint, event.Service)
	}

	if event.Checkpoint == nil {
		t.Fatal("Expected checkpoint payload, got nil")
	}

	if event.Checkpoint.Subject != subject {
		t.Errorf("Expected subject=%s, got %s", subject, event.Checkpoint.Subject)
	}

	if event.Checkpoint.AsOfTime != asOfTime {
		t.Errorf("Expected as_of_time=%d, got %d", asOfTime, event.Checkpoint.AsOfTime)
	}

	if event.Checkpoint.Observation.StartTime != firstSeen {
		t.Errorf("Expected first_seen=%d, got %d", firstSeen, event.Checkpoint.Observation.StartTime)
	}

	if event.Checkpoint.Observation.Restarts != restarts {
		t.Errorf("Expected restarts=%d, got %d", restarts, event.Checkpoint.Observation.Restarts)
	}

	if event.Checkpoint.Observation.TotalUptime != totalUptime {
		t.Errorf("Expected total_uptime=%d, got %d", totalUptime, event.Checkpoint.Observation.TotalUptime)
	}

	// Note: Checkpoint events are always critical importance (no need to check)
}

// Test that checkpoint events are valid
func TestCheckpoint_Validation(t *testing.T) {
	// Valid checkpoint
	valid := &CheckpointEventPayload{
		Subject:  "lisa",
		AsOfTime: time.Now().Unix(),
		Observation: NaraObservation{
			StartTime:   1624066568,
			Restarts:    47,
			TotalUptime: 23456789,
		},
	}
	if !valid.IsValid() {
		t.Error("Expected valid checkpoint to pass validation")
	}

	// Invalid: missing subject
	invalid1 := &CheckpointEventPayload{
		AsOfTime: time.Now().Unix(),
		Observation: NaraObservation{
			StartTime: 1624066568,
			Restarts:  47,
		},
	}
	if invalid1.IsValid() {
		t.Error("Expected checkpoint without subject to fail validation")
	}

	// Invalid: missing AsOfTime
	invalid2 := &CheckpointEventPayload{
		Subject: "lisa",
		Observation: NaraObservation{
			StartTime: 1624066568,
			Restarts:  47,
		},
	}
	if invalid2.IsValid() {
		t.Error("Expected checkpoint without as_of_time to fail validation")
	}

	// Invalid: negative restarts
	invalid3 := &CheckpointEventPayload{
		Subject:  "lisa",
		AsOfTime: time.Now().Unix(),
		Observation: NaraObservation{
			Restarts: -1,
		},
	}
	if invalid3.IsValid() {
		t.Error("Expected checkpoint with negative restarts to fail validation")
	}
}

// Test adding checkpoint to ledger
func TestCheckpoint_AddToLedger(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := "lisa"

	event := NewCheckpointEvent(subject, time.Now().Unix(), 1624066568, 47, 23456789)

	added := ledger.AddEvent(event)
	if !added {
		t.Error("Expected checkpoint event to be added to ledger")
	}

	// Should be able to retrieve it
	checkpoint := ledger.GetCheckpoint(subject)
	if checkpoint == nil {
		t.Fatal("Expected to retrieve checkpoint, got nil")
	}

	if checkpoint.Observation.Restarts != 47 {
		t.Errorf("Expected restarts=47, got %d", checkpoint.Observation.Restarts)
	}
}

// Test that only one checkpoint per subject is kept (latest wins)
func TestCheckpoint_OnlyOnePerSubject(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := "lisa"

	// Add first checkpoint
	event1 := NewCheckpointEvent(subject, 1000, 1624066568, 47, 23456789)
	ledger.AddEvent(event1)

	// Add second checkpoint with later AsOfTime
	event2 := NewCheckpointEvent(subject, 2000, 1624066568, 50, 24000000)
	ledger.AddEvent(event2)

	// Should get the latest checkpoint
	checkpoint := ledger.GetCheckpoint(subject)
	if checkpoint == nil {
		t.Fatal("Expected to retrieve checkpoint, got nil")
	}

	if checkpoint.Observation.Restarts != 50 {
		t.Errorf("Expected latest checkpoint restarts=50, got %d", checkpoint.Observation.Restarts)
	}

	if checkpoint.AsOfTime != 2000 {
		t.Errorf("Expected latest checkpoint as_of_time=2000, got %d", checkpoint.AsOfTime)
	}
}

// Test that checkpoints are never pruned (critical importance)
func TestCheckpoint_NeverPruned(t *testing.T) {
	// Small ledger to trigger pruning
	ledger := NewSyncLedger(50)
	subject := "lisa"

	// Add checkpoint
	checkpointEvent := NewCheckpointEvent(subject, time.Now().Unix(), 1624066568, 47, 23456789)
	ledger.AddEvent(checkpointEvent)

	// Flood with other events to trigger pruning
	for i := 0; i < 100; i++ {
		event := NewPingSyncEvent("observer", "target", float64(i))
		ledger.AddEvent(event)
	}

	// Checkpoint should still exist
	checkpoint := ledger.GetCheckpoint(subject)
	if checkpoint == nil {
		t.Fatal("Checkpoint was pruned but should never be pruned")
	}

	if checkpoint.Observation.Restarts != 47 {
		t.Errorf("Expected checkpoint restarts=47, got %d", checkpoint.Observation.Restarts)
	}
}

// Test checkpoint with single voter (basic case)
func TestCheckpoint_SingleVoter(t *testing.T) {
	subject := "lisa"
	voterID := "homer-id-123"
	keypair := generateTestKeypair()

	event := NewCheckpointEvent(subject, time.Now().Unix(), 1624066568, 47, 23456789)
	event.AddCheckpointVoter(voterID, keypair)

	if len(event.Checkpoint.VoterIDs) != 1 {
		t.Errorf("Expected 1 voter, got %d", len(event.Checkpoint.VoterIDs))
	}

	if event.Checkpoint.VoterIDs[0] != voterID {
		t.Errorf("Expected voterID=%s, got %s", voterID, event.Checkpoint.VoterIDs[0])
	}

	if len(event.Checkpoint.Signatures) != 1 {
		t.Errorf("Expected 1 signature, got %d", len(event.Checkpoint.Signatures))
	}
}

// Test checkpoint with multiple voters
func TestCheckpoint_MultipleVoters(t *testing.T) {
	subject := "lisa"
	voterIDs := []string{"homer-id", "marge-id", "bart-id"}
	keypairs := make([]NaraKeypair, 3)
	for i := range keypairs {
		keypairs[i] = generateTestKeypair()
	}

	event := NewCheckpointEvent(subject, time.Now().Unix(), 1624066568, 47, 23456789)

	for i, voterID := range voterIDs {
		event.AddCheckpointVoter(voterID, keypairs[i])
	}

	if len(event.Checkpoint.VoterIDs) != 3 {
		t.Errorf("Expected 3 voters, got %d", len(event.Checkpoint.VoterIDs))
	}

	if len(event.Checkpoint.Signatures) != 3 {
		t.Errorf("Expected 3 signatures, got %d", len(event.Checkpoint.Signatures))
	}
}

// Test checkpoint signature verification
func TestCheckpoint_VerifySignatures(t *testing.T) {
	subject := "lisa"
	voterIDs := []string{"homer-id", "marge-id", "bart-id"}
	keypairs := make([]NaraKeypair, 3)
	publicKeys := make(map[string]string)

	for i, voterID := range voterIDs {
		keypairs[i] = generateTestKeypair()
		publicKeys[voterID] = pubKeyToBase64(keypairs[i].PublicKey)
	}

	event := NewCheckpointEvent(subject, time.Now().Unix(), 1624066568, 47, 23456789)
	for i, voterID := range voterIDs {
		event.AddCheckpointVoter(voterID, keypairs[i])
	}

	// Verify with correct public keys
	validCount := event.VerifyCheckpointSignatures(publicKeys)
	if validCount != 3 {
		t.Errorf("Expected 3 valid signatures, got %d", validCount)
	}
}

// Test checkpoint signature verification with wrong keys
func TestCheckpoint_VerifySignatures_WrongKeys(t *testing.T) {
	subject := "lisa"
	voterID := "homer-id"
	keypair := generateTestKeypair()
	wrongKeypair := generateTestKeypair()

	event := NewCheckpointEvent(subject, time.Now().Unix(), 1624066568, 47, 23456789)
	event.AddCheckpointVoter(voterID, keypair)

	// Verify with wrong public key
	publicKeys := map[string]string{
		voterID: pubKeyToBase64(wrongKeypair.PublicKey), // Wrong key!
	}

	validCount := event.VerifyCheckpointSignatures(publicKeys)
	if validCount != 0 {
		t.Errorf("Expected 0 valid signatures with wrong key, got %d", validCount)
	}
}

// Test deriving restart count from checkpoint + new events
func TestCheckpoint_DeriveRestartCount(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := "lisa"
	checkpointTime := time.Now().Unix() - 3600 // 1 hour ago

	// Add checkpoint: lisa had 47 restarts as of 1 hour ago
	checkpointEvent := NewCheckpointEvent(subject, checkpointTime, 1624066568, 47, 23456789)
	ledger.AddEvent(checkpointEvent)

	// Add 3 new restart events after the checkpoint
	for i := 0; i < 3; i++ {
		startTime := checkpointTime + int64(i+1)*100 // Unique start times after checkpoint
		event := NewRestartObservationEvent("observer", subject, startTime, int64(48+i))
		ledger.AddEvent(event)
	}

	// Total should be 47 (checkpoint) + 3 (new) = 50
	total := ledger.DeriveRestartCount(subject)
	if total != 50 {
		t.Errorf("Expected total restarts=50 (47+3), got %d", total)
	}
}

// Test deriving restart count without checkpoint (just count events)
func TestCheckpoint_DeriveRestartCount_NoCheckpoint(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := "lisa"

	// Add 5 restart events with unique start times (no checkpoint)
	for i := 0; i < 5; i++ {
		startTime := int64(1000 + i*100) // Unique start times
		event := NewRestartObservationEvent("observer", subject, startTime, int64(i+1))
		ledger.AddEvent(event)
	}

	// Total should just be 5 (count of unique start times)
	total := ledger.DeriveRestartCount(subject)
	if total != 5 {
		t.Errorf("Expected total restarts=5, got %d", total)
	}
}

// Test deriving restart count with backfill fallback (no checkpoint)
func TestCheckpoint_DeriveRestartCount_BackfillFallback(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := "lisa"

	// Add backfill event (old system)
	backfillEvent := NewBackfillObservationEvent("observer", subject, 1624066568, 47, 1704067200)
	ledger.AddEvent(backfillEvent)

	// Add 2 new restart events with different start times
	event1 := NewRestartObservationEvent("observer", subject, 1704067300, 48)
	ledger.AddEvent(event1)
	event2 := NewRestartObservationEvent("observer", subject, 1704067400, 49)
	ledger.AddEvent(event2)

	// Total should be 47 (backfill baseline) + 2 (new unique start times) = 49
	total := ledger.DeriveRestartCount(subject)
	if total != 49 {
		t.Errorf("Expected total restarts=49 (47+2), got %d", total)
	}
}

// Test checkpoint takes precedence over backfill
func TestCheckpoint_PrecedenceOverBackfill(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := "lisa"
	checkpointTime := time.Now().Unix() - 3600

	// Add backfill event with old data
	backfillEvent := NewBackfillObservationEvent("observer", subject, 1624066568, 40, 1704067200)
	ledger.AddEvent(backfillEvent)

	// Add checkpoint with newer data (should take precedence)
	checkpointEvent := NewCheckpointEvent(subject, checkpointTime, 1624066568, 47, 23456789)
	ledger.AddEvent(checkpointEvent)

	// Add 1 new restart event
	newEvent := NewRestartObservationEvent("observer", subject, checkpointTime+100, 48)
	ledger.AddEvent(newEvent)

	// Total should be 47 (checkpoint) + 1 (new) = 48, NOT 40 (backfill) + something
	total := ledger.DeriveRestartCount(subject)
	if total != 48 {
		t.Errorf("Expected total restarts=48 (checkpoint 47 + 1 new), got %d", total)
	}
}

// Test counting unique start times (deduplication)
func TestCheckpoint_UniqueStartTimes(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := "lisa"

	startTime := int64(1704067200)

	// Multiple observers report the same restart (same start time)
	observers := []string{"homer", "marge", "bart"}
	for _, obs := range observers {
		event := NewRestartObservationEvent(obs, subject, startTime, 1)
		ledger.AddEvent(event)
	}

	// Should count as 1 restart (same start time)
	total := ledger.DeriveRestartCount(subject)
	if total != 1 {
		t.Errorf("Expected total restarts=1 (same start_time), got %d", total)
	}
}

// Test deriving total uptime from checkpoint + status events
func TestCheckpoint_DeriveTotalUptime(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := "lisa"
	checkpointTime := time.Now().Unix() - 3600 // 1 hour ago

	// Add checkpoint: lisa had 1000 seconds of uptime as of 1 hour ago
	checkpointEvent := NewCheckpointEvent(subject, checkpointTime, 1624066568, 10, 1000)
	ledger.AddEvent(checkpointEvent)

	// Add status change events after checkpoint
	// ONLINE at checkpoint+100, OFFLINE at checkpoint+600 = 500 seconds
	onlineEvent := NewStatusChangeObservationEvent("observer", subject, "ONLINE")
	onlineEvent.Timestamp = (checkpointTime + 100) * 1e9 // Convert to nanoseconds
	ledger.AddEvent(onlineEvent)

	offlineEvent := NewStatusChangeObservationEvent("observer", subject, "OFFLINE")
	offlineEvent.Timestamp = (checkpointTime + 600) * 1e9
	ledger.AddEvent(offlineEvent)

	// Total uptime should be 1000 (checkpoint) + 500 (new period) = 1500
	total := ledger.DeriveTotalUptime(subject)
	if total != 1500 {
		t.Errorf("Expected total uptime=1500 (1000+500), got %d", total)
	}
}

// Test deriving total uptime from backfill (no checkpoint)
// For backfill events, we assume the nara has been online since StartTime
func TestCheckpoint_DeriveTotalUptime_Backfill(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := "lisa"

	// Backfill: lisa has been around since startTime
	startTime := time.Now().Unix() - 7200 // 2 hours ago
	backfillEvent := NewBackfillObservationEvent("observer", subject, startTime, 10, time.Now().Unix())
	ledger.AddEvent(backfillEvent)

	// With no status-change events, uptime should be time since startTime
	total := ledger.DeriveTotalUptime(subject)
	expectedUptime := time.Now().Unix() - startTime

	// Allow 2 second tolerance for timing
	if total < expectedUptime-2 || total > expectedUptime+2 {
		t.Errorf("Expected uptime ~%d (since startTime), got %d", expectedUptime, total)
	}
}

// Test deriving total uptime from backfill with status-change events
func TestCheckpoint_DeriveTotalUptime_BackfillWithOffline(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := "lisa"

	// Backfill: lisa has been around since time 1000
	backfillEvent := NewBackfillObservationEvent("observer", subject, 1000, 10, 2000)
	ledger.AddEvent(backfillEvent)

	// Status events: OFFLINE at 1500, back ONLINE at 1700, OFFLINE at 1900
	// Timeline: online 1000-1500 (500s), offline 1500-1700 (200s), online 1700-1900 (200s)
	// Total uptime: 500 + 200 = 700s

	offline1 := NewStatusChangeObservationEvent("observer", subject, "OFFLINE")
	offline1.Timestamp = 1500 * 1e9
	ledger.AddEvent(offline1)

	online1 := NewStatusChangeObservationEvent("observer", subject, "ONLINE")
	online1.Timestamp = 1700 * 1e9
	ledger.AddEvent(online1)

	offline2 := NewStatusChangeObservationEvent("observer", subject, "OFFLINE")
	offline2.Timestamp = 1900 * 1e9
	ledger.AddEvent(offline2)

	total := ledger.DeriveTotalUptime(subject)
	expectedUptime := int64(500 + 200) // 700
	if total != expectedUptime {
		t.Errorf("Expected uptime=%d (500+200 from backfill timeline), got %d", expectedUptime, total)
	}
}

// Test that restart events are NOT pruned when no checkpoint exists
// This ensures we don't lose historical restart data before it's checkpointed
func TestCheckpoint_RestartEventsPreservedWithoutCheckpoint(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := "lisa"
	observer := "homer"

	// Add 25 restart events (more than MaxObservationsPerPair = 20)
	for i := 0; i < 25; i++ {
		startTime := int64(1704067200 + i*100) // Unique start times
		event := NewRestartObservationEvent(observer, subject, startTime, int64(i+1))
		ledger.AddEvent(event)
	}

	// All 25 should still be there since no checkpoint exists
	restartCount := ledger.DeriveRestartCount(subject)
	if restartCount != 25 {
		t.Errorf("Expected 25 restarts to be preserved (no checkpoint), got %d", restartCount)
	}

	// Count actual restart events
	count := 0
	for _, e := range ledger.Events {
		if e.Service == ServiceObservation && e.Observation != nil &&
			e.Observation.Subject == subject && e.Observation.Type == "restart" {
			count++
		}
	}
	if count < 25 {
		t.Errorf("Expected at least 25 restart events preserved, got %d", count)
	}
}

// Test that restart events CAN be pruned AFTER a checkpoint exists
func TestCheckpoint_RestartEventsPrunedAfterCheckpoint(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := "lisa"
	observer := "homer"
	checkpointTime := int64(1704067200)

	// First add a checkpoint
	checkpoint := NewCheckpointEvent(subject, checkpointTime, 1624066568, 10, 1000)
	keypair := generateTestKeypair()
	checkpoint.AddCheckpointVoter("voter1-id", keypair)
	checkpoint.AddCheckpointVoter("voter2-id", generateTestKeypair())
	ledger.AddEvent(checkpoint)

	// Now add 25 restart events after the checkpoint
	for i := 0; i < 25; i++ {
		startTime := checkpointTime + int64(i+1)*100 // After checkpoint
		event := NewRestartObservationEvent(observer, subject, startTime, int64(11+i))
		ledger.AddEvent(event)
	}

	// With checkpoint existing, compaction should prune to 20
	count := 0
	for _, e := range ledger.Events {
		if e.Service == ServiceObservation && e.Observation != nil &&
			e.Observation.Observer == observer && e.Observation.Subject == subject {
			count++
		}
	}
	if count > MaxObservationsPerPair {
		t.Errorf("Expected at most %d observation events after compaction, got %d", MaxObservationsPerPair, count)
	}

	// But derived restart count should still be correct: 10 (checkpoint) + remaining unique StartTimes
	restartCount := ledger.DeriveRestartCount(subject)
	// We should have checkpoint(10) + some number of new restarts
	if restartCount < 10 {
		t.Errorf("Expected at least checkpoint restarts (10), got %d", restartCount)
	}
}

// Test that round 2 checkpoint signatures can be verified
// This tests the bug where verifyCheckpointSignatures hardcodes round=1
func TestCheckpoint_Round2SignatureVerification(t *testing.T) {
	// Create test naras with keypairs
	proposerKeypair := generateTestKeypair()
	voter1Keypair := generateTestKeypair()
	voter2Keypair := generateTestKeypair()

	proposerID := "proposer-id"
	voter1ID := "voter1-id"
	voter2ID := "voter2-id"

	subject := "proposer"
	asOfTime := time.Now().Unix()
	restarts := int64(100)
	totalUptime := int64(50000)
	firstSeen := int64(1624066568)
	round := 2 // Round 2!

	// Create signatures using attestation format
	// Proposer creates self-attestation
	proposerAttestation := Attestation{
		Version:   1,
		Subject:   subject,
		SubjectID: proposerID,
		Observation: NaraObservation{
			Restarts:    restarts,
			TotalUptime: totalUptime,
			StartTime:   firstSeen,
		},
		Attester:   subject,
		AttesterID: proposerID,
		AsOfTime:   asOfTime,
	}
	proposerSig := SignContent(&proposerAttestation, proposerKeypair)

	// Voters create third-party attestations
	voter1Attestation := Attestation{
		Version:   1,
		Subject:   subject,
		SubjectID: proposerID,
		Observation: NaraObservation{
			Restarts:    restarts,
			TotalUptime: totalUptime,
			StartTime:   firstSeen,
		},
		Attester:   "voter1",
		AttesterID: voter1ID,
		AsOfTime:   asOfTime,
	}
	voter1Sig := SignContent(&voter1Attestation, voter1Keypair)

	voter2Attestation := Attestation{
		Version:   1,
		Subject:   subject,
		SubjectID: proposerID,
		Observation: NaraObservation{
			Restarts:    restarts,
			TotalUptime: totalUptime,
			StartTime:   firstSeen,
		},
		Attester:   "voter2",
		AttesterID: voter2ID,
		AsOfTime:   asOfTime,
	}
	voter2Sig := SignContent(&voter2Attestation, voter2Keypair)

	// Create checkpoint with round 2 signatures
	checkpoint := &CheckpointEventPayload{
		Subject:   subject,
		SubjectID: proposerID,
		AsOfTime:  asOfTime,
		Observation: NaraObservation{
			Restarts:    restarts,
			TotalUptime: totalUptime,
			StartTime:   firstSeen,
		},
		VoterIDs:   []string{proposerID, voter1ID, voter2ID},
		Signatures: []string{proposerSig, voter1Sig, voter2Sig},
		Round:      round, // Must match what was signed
	}

	// Create a CheckpointService with a mock network that returns public keys
	ledger := NewSyncLedger(1000)
	local := testLocalNara("verifier")

	// Create a minimal network with public key lookup
	network := &Network{
		Neighbourhood: make(map[string]*Nara),
		local:         local,
	}
	network.Neighbourhood["proposer"] = &Nara{
		Name: "proposer",
		Status: NaraStatus{
			ID:        proposerID,
			PublicKey: pubKeyToBase64(proposerKeypair.PublicKey),
		},
	}
	network.Neighbourhood["voter1"] = &Nara{
		Name: "voter1",
		Status: NaraStatus{
			ID:        voter1ID,
			PublicKey: pubKeyToBase64(voter1Keypair.PublicKey),
		},
	}
	network.Neighbourhood["voter2"] = &Nara{
		Name: "voter2",
		Status: NaraStatus{
			ID:        voter2ID,
			PublicKey: pubKeyToBase64(voter2Keypair.PublicKey),
		},
	}

	service := NewCheckpointService(network, ledger, local)

	// Verify signatures - should succeed with attestation format
	validCount := service.verifyCheckpointSignatures(checkpoint)

	// All 3 signatures should be valid
	if validCount != 3 {
		t.Errorf("Expected 3 valid signatures for round 2 checkpoint, got %d", validCount)
	}
}

// Test that partial signature verification works (some naras unknown)
func TestCheckpoint_PartialSignatureVerification(t *testing.T) {
	// Create test naras with keypairs
	proposerKeypair := generateTestKeypair()
	voter1Keypair := generateTestKeypair()
	voter2Keypair := generateTestKeypair()

	proposerID := "proposer-id"
	voter1ID := "voter1-id"
	voter2ID := "voter2-id"

	subject := "proposer"
	asOfTime := time.Now().Unix()
	restarts := int64(100)
	totalUptime := int64(50000)
	firstSeen := int64(1624066568)
	round := 1

	// Create signatures using attestation format
	proposerAttestation := Attestation{
		Version:   1,
		Subject:   subject,
		SubjectID: proposerID,
		Observation: NaraObservation{
			Restarts:    restarts,
			TotalUptime: totalUptime,
			StartTime:   firstSeen,
		},
		Attester:   subject,
		AttesterID: proposerID,
		AsOfTime:   asOfTime,
	}
	proposerSig := SignContent(&proposerAttestation, proposerKeypair)

	voter1Attestation := Attestation{
		Version:   1,
		Subject:   subject,
		SubjectID: proposerID,
		Observation: NaraObservation{
			Restarts:    restarts,
			TotalUptime: totalUptime,
			StartTime:   firstSeen,
		},
		Attester:   "voter1",
		AttesterID: voter1ID,
		AsOfTime:   asOfTime,
	}
	voter1Sig := SignContent(&voter1Attestation, voter1Keypair)

	voter2Attestation := Attestation{
		Version:   1,
		Subject:   subject,
		SubjectID: proposerID,
		Observation: NaraObservation{
			Restarts:    restarts,
			TotalUptime: totalUptime,
			StartTime:   firstSeen,
		},
		Attester:   "voter2",
		AttesterID: voter2ID,
		AsOfTime:   asOfTime,
	}
	voter2Sig := SignContent(&voter2Attestation, voter2Keypair)

	// Create checkpoint with all 3 signatures
	checkpoint := &CheckpointEventPayload{
		Subject:   subject,
		SubjectID: proposerID,
		AsOfTime:  asOfTime,
		Observation: NaraObservation{
			Restarts:    restarts,
			TotalUptime: totalUptime,
			StartTime:   firstSeen,
		},
		VoterIDs:   []string{proposerID, voter1ID, voter2ID},
		Signatures: []string{proposerSig, voter1Sig, voter2Sig},
		Round:      round,
	}

	// Create a CheckpointService with a network that only knows voter1
	ledger := NewSyncLedger(1000)
	local := testLocalNara("verifier")

	network := &Network{
		Neighbourhood: make(map[string]*Nara),
		local:         local,
	}
	// Only add voter1 - proposer and voter2 are "unknown"
	network.Neighbourhood["voter1"] = &Nara{
		Name: "voter1",
		Status: NaraStatus{
			ID:        voter1ID,
			PublicKey: pubKeyToBase64(voter1Keypair.PublicKey),
		},
	}

	service := NewCheckpointService(network, ledger, local)

	// Should return 1 (only voter1 is verifiable)
	validCount := service.verifyCheckpointSignatures(checkpoint)

	if validCount != 1 {
		t.Errorf("Expected 1 valid signature (only voter1 known), got %d", validCount)
	}
}

// Test that status-change events are pruned first (before restart events)
func TestCheckpoint_StatusChangeEventsPrunedFirst(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := "lisa"
	observer := "homer"

	// Add 15 restart events
	for i := 0; i < 15; i++ {
		startTime := int64(1704067200 + i*100)
		event := NewRestartObservationEvent(observer, subject, startTime, int64(i+1))
		ledger.AddEvent(event)
	}

	// Add 10 status-change events (total 25, over limit of 20)
	for i := 0; i < 10; i++ {
		event := NewStatusChangeObservationEvent(observer, subject, "ONLINE")
		event.Timestamp = int64(1704067200+i*50) * 1e9
		ledger.AddEvent(event)
	}

	// All 15 restart events should be preserved
	restartCount := 0
	statusCount := 0
	for _, e := range ledger.Events {
		if e.Service == ServiceObservation && e.Observation != nil &&
			e.Observation.Observer == observer && e.Observation.Subject == subject {
			if e.Observation.Type == "restart" {
				restartCount++
			} else if e.Observation.Type == "status-change" {
				statusCount++
			}
		}
	}

	if restartCount != 15 {
		t.Errorf("Expected all 15 restart events preserved, got %d", restartCount)
	}

	// Status-change events should have been pruned to make room
	if statusCount > 5 {
		t.Errorf("Expected status-change events to be pruned (got %d), restarts should be preserved", statusCount)
	}
}
