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

	if event.Checkpoint.FirstSeen != firstSeen {
		t.Errorf("Expected first_seen=%d, got %d", firstSeen, event.Checkpoint.FirstSeen)
	}

	if event.Checkpoint.Restarts != restarts {
		t.Errorf("Expected restarts=%d, got %d", restarts, event.Checkpoint.Restarts)
	}

	if event.Checkpoint.TotalUptime != totalUptime {
		t.Errorf("Expected total_uptime=%d, got %d", totalUptime, event.Checkpoint.TotalUptime)
	}

	// Checkpoint events should be critical importance
	if event.Checkpoint.Importance != ImportanceCritical {
		t.Errorf("Expected importance=%d (Critical), got %d", ImportanceCritical, event.Checkpoint.Importance)
	}
}

// Test that checkpoint events are valid
func TestCheckpoint_Validation(t *testing.T) {
	// Valid checkpoint
	valid := &CheckpointEventPayload{
		Subject:     "lisa",
		AsOfTime:    time.Now().Unix(),
		FirstSeen:   1624066568,
		Restarts:    47,
		TotalUptime: 23456789,
		Importance:  ImportanceCritical,
	}
	if !valid.IsValid() {
		t.Error("Expected valid checkpoint to pass validation")
	}

	// Invalid: missing subject
	invalid1 := &CheckpointEventPayload{
		AsOfTime:  time.Now().Unix(),
		FirstSeen: 1624066568,
		Restarts:  47,
	}
	if invalid1.IsValid() {
		t.Error("Expected checkpoint without subject to fail validation")
	}

	// Invalid: missing AsOfTime
	invalid2 := &CheckpointEventPayload{
		Subject:   "lisa",
		FirstSeen: 1624066568,
		Restarts:  47,
	}
	if invalid2.IsValid() {
		t.Error("Expected checkpoint without as_of_time to fail validation")
	}

	// Invalid: negative restarts
	invalid3 := &CheckpointEventPayload{
		Subject:  "lisa",
		AsOfTime: time.Now().Unix(),
		Restarts: -1,
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

	if checkpoint.Restarts != 47 {
		t.Errorf("Expected restarts=47, got %d", checkpoint.Restarts)
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

	if checkpoint.Restarts != 50 {
		t.Errorf("Expected latest checkpoint restarts=50, got %d", checkpoint.Restarts)
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

	if checkpoint.Restarts != 47 {
		t.Errorf("Expected checkpoint restarts=47, got %d", checkpoint.Restarts)
	}
}

// Test checkpoint with single attester (basic case)
func TestCheckpoint_SingleAttester(t *testing.T) {
	subject := "lisa"
	attester := "homer"
	keypair := generateTestKeypair()

	event := NewCheckpointEvent(subject, time.Now().Unix(), 1624066568, 47, 23456789)
	event.AddCheckpointAttester(attester, keypair)

	if len(event.Checkpoint.Attesters) != 1 {
		t.Errorf("Expected 1 attester, got %d", len(event.Checkpoint.Attesters))
	}

	if event.Checkpoint.Attesters[0] != attester {
		t.Errorf("Expected attester=%s, got %s", attester, event.Checkpoint.Attesters[0])
	}

	if len(event.Checkpoint.Signatures) != 1 {
		t.Errorf("Expected 1 signature, got %d", len(event.Checkpoint.Signatures))
	}
}

// Test checkpoint with multiple attesters
func TestCheckpoint_MultipleAttesters(t *testing.T) {
	subject := "lisa"
	attesters := []string{"homer", "marge", "bart"}
	keypairs := make([]NaraKeypair, 3)
	for i := range keypairs {
		keypairs[i] = generateTestKeypair()
	}

	event := NewCheckpointEvent(subject, time.Now().Unix(), 1624066568, 47, 23456789)

	for i, attester := range attesters {
		event.AddCheckpointAttester(attester, keypairs[i])
	}

	if len(event.Checkpoint.Attesters) != 3 {
		t.Errorf("Expected 3 attesters, got %d", len(event.Checkpoint.Attesters))
	}

	if len(event.Checkpoint.Signatures) != 3 {
		t.Errorf("Expected 3 signatures, got %d", len(event.Checkpoint.Signatures))
	}
}

// Test checkpoint signature verification
func TestCheckpoint_VerifySignatures(t *testing.T) {
	subject := "lisa"
	attesters := []string{"homer", "marge", "bart"}
	keypairs := make([]NaraKeypair, 3)
	publicKeys := make(map[string]string)

	for i, attester := range attesters {
		keypairs[i] = generateTestKeypair()
		publicKeys[attester] = pubKeyToBase64(keypairs[i].PublicKey)
	}

	event := NewCheckpointEvent(subject, time.Now().Unix(), 1624066568, 47, 23456789)
	for i, attester := range attesters {
		event.AddCheckpointAttester(attester, keypairs[i])
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
	attester := "homer"
	keypair := generateTestKeypair()
	wrongKeypair := generateTestKeypair()

	event := NewCheckpointEvent(subject, time.Now().Unix(), 1624066568, 47, 23456789)
	event.AddCheckpointAttester(attester, keypair)

	// Verify with wrong public key
	publicKeys := map[string]string{
		attester: pubKeyToBase64(wrongKeypair.PublicKey), // Wrong key!
	}

	validCount := event.VerifyCheckpointSignatures(publicKeys)
	if validCount != 0 {
		t.Errorf("Expected 0 valid signatures with wrong key, got %d", validCount)
	}
}

// Test checkpoint minimum attesters requirement
func TestCheckpoint_MinimumAttesters(t *testing.T) {
	if !useCheckpoints() {
		t.Skip("Checkpoints not enabled")
	}

	ledger := NewSyncLedger(1000)
	subject := "lisa"

	// Create checkpoint with only 1 attester (below minimum)
	event := NewCheckpointEvent(subject, time.Now().Unix(), 1624066568, 47, 23456789)
	keypair := generateTestKeypair()
	event.AddCheckpointAttester("homer", keypair)

	// Should still be added (validation happens at consensus time, not add time)
	added := ledger.AddEvent(event)
	if !added {
		t.Error("Expected checkpoint to be added even with few attesters")
	}

	// But it shouldn't be considered valid for consensus without enough attesters
	minAttesters := getMinCheckpointAttesters()
	if minAttesters > 1 && len(event.Checkpoint.Attesters) < minAttesters {
		// This is expected - checkpoint exists but isn't trusted yet
		t.Logf("Checkpoint has %d attesters, needs %d for consensus", len(event.Checkpoint.Attesters), minAttesters)
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

// Test feature flag for checkpoints
func TestCheckpoint_FeatureFlag(t *testing.T) {
	// This tests that the feature flag function exists and returns a boolean
	enabled := useCheckpoints()
	t.Logf("Checkpoints enabled: %v", enabled)
	// The actual value depends on environment, but function should exist
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

// --- Checkpoint Creation Tests ---

// Test that high-uptime naras are identified correctly
func TestCheckpoint_IsHighUptime(t *testing.T) {
	// Create test observations with various uptimes
	observations := map[string]NaraObservation{
		"homer":  {StartTime: time.Now().Unix() - 86400*30, Online: "ONLINE"},  // 30 days (high)
		"marge":  {StartTime: time.Now().Unix() - 86400*14, Online: "ONLINE"},  // 14 days (high)
		"bart":   {StartTime: time.Now().Unix() - 86400*7, Online: "ONLINE"},   // 7 days (medium)
		"lisa":   {StartTime: time.Now().Unix() - 86400*3, Online: "ONLINE"},   // 3 days (low)
		"maggie": {StartTime: time.Now().Unix() - 3600, Online: "ONLINE"},      // 1 hour (very low)
		"patty":  {StartTime: time.Now().Unix() - 86400*60, Online: "OFFLINE"}, // 60 days but offline
	}

	// Test with minimum 7 days uptime threshold
	minUptimeSeconds := int64(7 * 24 * 60 * 60) // 7 days

	// Homer (30 days) should be high uptime
	if !IsHighUptime(observations["homer"], minUptimeSeconds) {
		t.Error("Expected homer (30 days) to be high uptime")
	}

	// Marge (14 days) should be high uptime
	if !IsHighUptime(observations["marge"], minUptimeSeconds) {
		t.Error("Expected marge (14 days) to be high uptime")
	}

	// Bart (7 days) should be high uptime (exactly at threshold)
	if !IsHighUptime(observations["bart"], minUptimeSeconds) {
		t.Error("Expected bart (7 days) to be high uptime")
	}

	// Lisa (3 days) should NOT be high uptime
	if IsHighUptime(observations["lisa"], minUptimeSeconds) {
		t.Error("Expected lisa (3 days) to NOT be high uptime")
	}

	// Maggie (1 hour) should NOT be high uptime
	if IsHighUptime(observations["maggie"], minUptimeSeconds) {
		t.Error("Expected maggie (1 hour) to NOT be high uptime")
	}

	// Patty is offline, so not considered high uptime for checkpoint purposes
	if IsHighUptime(observations["patty"], minUptimeSeconds) {
		t.Error("Expected offline nara to NOT be considered high uptime")
	}
}

// Test that checkpoint eligibility considers top N% uptime
func TestCheckpoint_GetHighUptimeNaras(t *testing.T) {
	observations := map[string]NaraObservation{
		"homer":  {StartTime: time.Now().Unix() - 86400*30, Online: "ONLINE"},
		"marge":  {StartTime: time.Now().Unix() - 86400*20, Online: "ONLINE"},
		"bart":   {StartTime: time.Now().Unix() - 86400*10, Online: "ONLINE"},
		"lisa":   {StartTime: time.Now().Unix() - 86400*5, Online: "ONLINE"},
		"maggie": {StartTime: time.Now().Unix() - 86400*1, Online: "ONLINE"},
	}

	// Get top 40% (2 out of 5 naras)
	topNaras := GetHighUptimeNaras(observations, 0.4)

	if len(topNaras) != 2 {
		t.Errorf("Expected 2 high-uptime naras (top 40%%), got %d", len(topNaras))
	}

	// Should include homer and marge (highest uptime)
	found := make(map[string]bool)
	for _, name := range topNaras {
		found[name] = true
	}

	if !found["homer"] {
		t.Error("Expected homer in high-uptime list")
	}
	if !found["marge"] {
		t.Error("Expected marge in high-uptime list")
	}
}

// Test determining when to propose a checkpoint for a subject
func TestCheckpoint_ShouldProposeCheckpoint(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := "lisa"

	// With no data at all, should not propose
	if ShouldProposeCheckpoint(ledger, subject) {
		t.Error("Should not propose checkpoint when no data exists")
	}

	// Add backfill event (indicates we have historical data to checkpoint)
	backfill := NewBackfillObservationEvent("homer", subject, 1624066568, 47, 1704067200)
	ledger.AddEvent(backfill)

	// Now should propose (have backfill, no checkpoint)
	if !ShouldProposeCheckpoint(ledger, subject) {
		t.Error("Should propose checkpoint when backfill exists but no checkpoint")
	}

	// Add a checkpoint - now should NOT propose
	checkpoint := NewCheckpointEvent(subject, time.Now().Unix(), 1624066568, 47, 23456789)
	keypair := generateTestKeypair()
	checkpoint.AddCheckpointAttester("homer", keypair)
	checkpoint.AddCheckpointAttester("marge", generateTestKeypair())
	ledger.AddEvent(checkpoint)

	if ShouldProposeCheckpoint(ledger, subject) {
		t.Error("Should not propose checkpoint when valid checkpoint already exists")
	}
}

// Test preparing a checkpoint proposal from existing data
func TestCheckpoint_PrepareCheckpointProposal(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := "lisa"

	// Add backfill event with historical data
	backfill := NewBackfillObservationEvent("homer", subject, 1624066568, 47, 1704067200)
	ledger.AddEvent(backfill)

	// Add some restart events
	restart1 := NewRestartObservationEvent("homer", subject, 1704100000, 48)
	ledger.AddEvent(restart1)

	restart2 := NewRestartObservationEvent("marge", subject, 1704200000, 49)
	ledger.AddEvent(restart2)

	// Prepare proposal
	proposal := PrepareCheckpointProposal(ledger, subject)

	if proposal == nil {
		t.Fatal("Expected proposal to be created")
	}

	if proposal.Subject != subject {
		t.Errorf("Expected subject=%s, got %s", subject, proposal.Subject)
	}

	// Should have derived restart count (backfill 47 + 2 new = 49)
	if proposal.Restarts != 49 {
		t.Errorf("Expected restarts=49, got %d", proposal.Restarts)
	}

	// FirstSeen should come from backfill
	if proposal.FirstSeen != 1624066568 {
		t.Errorf("Expected first_seen=1624066568, got %d", proposal.FirstSeen)
	}
}

// Test signing a checkpoint proposal
func TestCheckpoint_SignProposal(t *testing.T) {
	subject := "lisa"
	asOfTime := time.Now().Unix()

	// Create proposal
	proposal := &CheckpointEventPayload{
		Subject:     subject,
		AsOfTime:    asOfTime,
		FirstSeen:   1624066568,
		Restarts:    47,
		TotalUptime: 23456789,
		Importance:  ImportanceCritical,
	}

	// Sign it
	attester := "homer"
	keypair := generateTestKeypair()

	signature := SignCheckpointProposal(proposal, keypair)

	if signature == "" {
		t.Error("Expected signature to be generated")
	}

	// Verify signature
	pubKeys := map[string]string{
		attester: pubKeyToBase64(keypair.PublicKey),
	}

	// Create event with signature to verify
	event := NewCheckpointEvent(subject, asOfTime, 1624066568, 47, 23456789)
	event.Checkpoint.Attesters = []string{attester}
	event.Checkpoint.Signatures = []string{signature}

	validCount := event.VerifyCheckpointSignatures(pubKeys)
	if validCount != 1 {
		t.Errorf("Expected 1 valid signature, got %d", validCount)
	}
}

// Test checkpoint signature request structure
func TestCheckpoint_SignatureRequest(t *testing.T) {
	proposal := &CheckpointEventPayload{
		Subject:     "lisa",
		AsOfTime:    time.Now().Unix(),
		FirstSeen:   1624066568,
		Restarts:    47,
		TotalUptime: 23456789,
		Importance:  ImportanceCritical,
	}

	// Create request
	request := CheckpointSignatureRequest{
		Proposal:  proposal,
		Requester: "homer",
	}

	if request.Proposal.Subject != "lisa" {
		t.Errorf("Expected subject=lisa, got %s", request.Proposal.Subject)
	}
	if request.Requester != "homer" {
		t.Errorf("Expected requester=homer, got %s", request.Requester)
	}
}

// Test checkpoint signature response structure
func TestCheckpoint_SignatureResponse(t *testing.T) {
	keypair := generateTestKeypair()
	proposal := &CheckpointEventPayload{
		Subject:     "lisa",
		AsOfTime:    time.Now().Unix(),
		FirstSeen:   1624066568,
		Restarts:    47,
		TotalUptime: 23456789,
		Importance:  ImportanceCritical,
	}

	signature := SignCheckpointProposal(proposal, keypair)

	// Create response
	response := CheckpointSignatureResponse{
		Attester:  "marge",
		Signature: signature,
		Approved:  true,
	}

	if response.Attester != "marge" {
		t.Errorf("Expected attester=marge, got %s", response.Attester)
	}
	if response.Signature == "" {
		t.Error("Expected signature to be set")
	}
	if !response.Approved {
		t.Error("Expected approved=true")
	}
}

// Test declining to sign a checkpoint (disagreement on data)
func TestCheckpoint_DeclineSignature(t *testing.T) {
	// Response when declining
	response := CheckpointSignatureResponse{
		Attester:  "marge",
		Signature: "",
		Approved:  false,
		Reason:    "restart count mismatch",
	}

	if response.Approved {
		t.Error("Expected approved=false")
	}
	if response.Reason == "" {
		t.Error("Expected reason to be set when declining")
	}
}

// Test validating a checkpoint proposal before signing
func TestCheckpoint_ValidateProposalBeforeSigning(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := "lisa"

	// Add data to ledger that matches proposal
	backfill := NewBackfillObservationEvent("homer", subject, 1624066568, 47, 1704067200)
	ledger.AddEvent(backfill)

	restart := NewRestartObservationEvent("homer", subject, 1704100000, 48)
	ledger.AddEvent(restart)

	// Proposal that matches our data
	goodProposal := &CheckpointEventPayload{
		Subject:     subject,
		AsOfTime:    time.Now().Unix(),
		FirstSeen:   1624066568,
		Restarts:    48, // matches: 47 + 1
		TotalUptime: 0,
		Importance:  ImportanceCritical,
	}

	// Should validate successfully
	err := ValidateCheckpointProposal(ledger, goodProposal)
	if err != nil {
		t.Errorf("Expected good proposal to validate, got error: %v", err)
	}

	// Proposal with wrong restart count
	badProposal := &CheckpointEventPayload{
		Subject:     subject,
		AsOfTime:    time.Now().Unix(),
		FirstSeen:   1624066568,
		Restarts:    100, // Wrong!
		TotalUptime: 0,
		Importance:  ImportanceCritical,
	}

	// Should fail validation
	err = ValidateCheckpointProposal(ledger, badProposal)
	if err == nil {
		t.Error("Expected bad proposal to fail validation")
	}
}
