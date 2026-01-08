package nara

import (
	"testing"
)

func TestSocialEvent_ID(t *testing.T) {
	// Event ID should be deterministic based on content
	event1 := SocialEvent{
		Timestamp: 1000,
		Type:      "tease",
		Actor:     "alice",
		Target:    "bob",
		Reason:    ReasonHighRestarts,
	}
	event1.ComputeID()

	event2 := SocialEvent{
		Timestamp: 1000,
		Type:      "tease",
		Actor:     "alice",
		Target:    "bob",
		Reason:    ReasonHighRestarts,
	}
	event2.ComputeID()

	if event1.ID != event2.ID {
		t.Error("Same event content should produce same ID")
	}

	// Different content should produce different ID
	event3 := SocialEvent{
		Timestamp: 1001, // different timestamp
		Type:      "tease",
		Actor:     "alice",
		Target:    "bob",
		Reason:    ReasonHighRestarts,
	}
	event3.ComputeID()

	if event1.ID == event3.ID {
		t.Error("Different event content should produce different ID")
	}
}

func TestTeaseResonates_Deterministic(t *testing.T) {
	event := SocialEvent{
		ID:        "test-event-123",
		Timestamp: 1000,
		Type:      "tease",
		Actor:     "alice",
		Target:    "bob",
		Reason:    ReasonHighRestarts,
	}

	personality := NaraPersonality{
		Agreeableness: 50,
		Sociability:   70,
		Chill:         30,
	}

	soul := "test-soul-abc"

	// Same inputs should always produce same result
	result1 := TeaseResonates(event, soul, personality)
	result2 := TeaseResonates(event, soul, personality)

	if result1 != result2 {
		t.Error("TeaseResonates should be deterministic")
	}
}

func TestTeaseResonates_DifferentSouls(t *testing.T) {
	event := SocialEvent{
		ID:        "test-event-456",
		Timestamp: 1000,
		Type:      "tease",
		Actor:     "alice",
		Target:    "bob",
		Reason:    ReasonHighRestarts,
	}

	personality := NaraPersonality{
		Agreeableness: 50,
		Sociability:   50,
		Chill:         50,
	}

	// Test that different souls can produce different results
	// We test many souls to verify distribution isn't uniform
	trueCount := 0
	for i := 0; i < 100; i++ {
		soul := string(rune('a'+i)) + "-soul"
		if TeaseResonates(event, soul, personality) {
			trueCount++
		}
	}

	// Should have some variation (not all true or all false)
	if trueCount == 0 || trueCount == 100 {
		t.Errorf("Expected variation across souls, got %d/100 true", trueCount)
	}
}

func TestTeaseResonates_PersonalityInfluence(t *testing.T) {
	event := SocialEvent{
		ID:        "test-event-789",
		Timestamp: 1000,
		Type:      "tease",
		Actor:     "alice",
		Target:    "bob",
		Reason:    ReasonHighRestarts,
	}

	soul := "consistent-soul"

	// High sociability should appreciate teasing more (lower threshold)
	highSociability := NaraPersonality{
		Agreeableness: 50,
		Sociability:   100,
		Chill:         0,
	}

	// Low sociability, high chill should appreciate teasing less (higher threshold)
	lowSociabilityHighChill := NaraPersonality{
		Agreeableness: 50,
		Sociability:   0,
		Chill:         100,
	}

	// Test many events to verify personality affects probability
	highSocCount := 0
	lowSocCount := 0

	for i := 0; i < 100; i++ {
		testEvent := event
		testEvent.ID = string(rune('a'+i)) + "-event"

		if TeaseResonates(testEvent, soul, highSociability) {
			highSocCount++
		}
		if TeaseResonates(testEvent, soul, lowSociabilityHighChill) {
			lowSocCount++
		}
	}

	// High sociability should resonate with more teases
	if highSocCount <= lowSocCount {
		t.Errorf("High sociability should appreciate more teases: high=%d, low=%d", highSocCount, lowSocCount)
	}
}

func TestSocialEvent_Types(t *testing.T) {
	// Verify supported event types
	validTypes := []string{"tease", "observed", "gossip", "observation"}

	for _, eventType := range validTypes {
		event := SocialEvent{
			Timestamp: 1000,
			Type:      eventType,
			Actor:     "alice",
			Target:    "bob",
		}

		if !event.IsValid() {
			t.Errorf("Event type %s should be valid", eventType)
		}
	}

	// Invalid type
	invalidEvent := SocialEvent{
		Timestamp: 1000,
		Type:      "invalid",
		Actor:     "alice",
		Target:    "bob",
	}

	if invalidEvent.IsValid() {
		t.Error("Invalid event type should not be valid")
	}
}

func TestPartitionSubjects(t *testing.T) {
	subjects := []string{"alice", "bob", "charlie", "dave", "eve", "frank", "grace", "henry"}

	// Partition into 3 groups
	partitions := PartitionSubjects(subjects, 3)

	if len(partitions) != 3 {
		t.Errorf("Expected 3 partitions, got %d", len(partitions))
	}

	// All subjects should be accounted for
	total := 0
	for _, p := range partitions {
		total += len(p)
	}
	if total != len(subjects) {
		t.Errorf("Expected %d subjects in partitions, got %d", len(subjects), total)
	}

	// Partitioning should be deterministic
	partitions2 := PartitionSubjects(subjects, 3)
	for i := range partitions {
		if len(partitions[i]) != len(partitions2[i]) {
			t.Error("Partitioning should be deterministic")
		}
	}
}

func TestPartitionSubjects_EdgeCases(t *testing.T) {
	subjects := []string{"alice", "bob"}

	// More partitions than subjects
	partitions := PartitionSubjects(subjects, 10)
	if len(partitions) != 2 { // should cap at len(subjects)
		t.Errorf("Expected 2 partitions (capped), got %d", len(partitions))
	}

	// Zero partitions
	partitions = PartitionSubjects(subjects, 0)
	if len(partitions) != 1 { // should default to 1
		t.Errorf("Expected 1 partition (default), got %d", len(partitions))
	}
}

// ============================================================
// Tests for observation event types
// ============================================================

func TestObservationEventReasons_Defined(t *testing.T) {
	// Verify new observation reasons are defined
	reasons := []string{
		ReasonOnline,
		ReasonOffline,
		ReasonJourneyPass,
		ReasonJourneyComplete,
		ReasonJourneyTimeout,
	}

	for _, reason := range reasons {
		if reason == "" {
			t.Errorf("Reason constant should not be empty")
		}
	}
}

func TestObservationEventType_Valid(t *testing.T) {
	// The new "observation" type should be valid
	event := SocialEvent{
		Timestamp: 1000,
		Type:      "observation",
		Actor:     "alice",
		Target:    "bob",
		Reason:    ReasonOnline,
	}

	if !event.IsValid() {
		t.Error("Event type 'observation' should be valid")
	}
}

func TestNewObservationEvent(t *testing.T) {
	event := NewObservationEvent("alice", "bob", ReasonOnline)

	if event.Type != "observation" {
		t.Errorf("Expected type 'observation', got %s", event.Type)
	}
	if event.Actor != "alice" {
		t.Errorf("Expected actor 'alice', got %s", event.Actor)
	}
	if event.Target != "bob" {
		t.Errorf("Expected target 'bob', got %s", event.Target)
	}
	if event.Reason != ReasonOnline {
		t.Errorf("Expected reason '%s', got %s", ReasonOnline, event.Reason)
	}
	if event.Timestamp == 0 {
		t.Error("Timestamp should be set")
	}
	if event.ID == "" {
		t.Error("ID should be computed")
	}
}

func TestNewJourneyObservationEvent(t *testing.T) {
	journeyID := "journey-123"
	event := NewJourneyObservationEvent("alice", "bob", ReasonJourneyPass, journeyID)

	if event.Type != "observation" {
		t.Errorf("Expected type 'observation', got %s", event.Type)
	}
	if event.Reason != ReasonJourneyPass {
		t.Errorf("Expected reason '%s', got %s", ReasonJourneyPass, event.Reason)
	}
	if event.Witness != journeyID {
		t.Errorf("Expected witness (journey ID) '%s', got %s", journeyID, event.Witness)
	}
}

// Tests for teasing mechanics (IsNiceNumber, ShouldTeaseFor*, etc.)
// are in teasing_test.go to avoid duplication

// --- Signature Tests ---

func TestSocialEvent_SignAndVerify(t *testing.T) {
	// Create keypairs from test souls
	soul := NativeSoulCustom([]byte("test-hw-fingerprint-1"), "alice")
	keypair := DeriveKeypair(soul)

	// Create and sign an event
	event := NewTeaseEvent("alice", "bob", ReasonHighRestarts)
	event.Sign(keypair)

	if event.Signature == "" {
		t.Error("Expected signature to be set after signing")
	}

	// Verify with correct public key
	if !event.Verify(keypair.PublicKey) {
		t.Error("Expected signature to verify with correct public key")
	}

	// Verify fails with wrong public key
	wrongSoul := NativeSoulCustom([]byte("different-hw-fingerprint"), "bob")
	wrongKeypair := DeriveKeypair(wrongSoul)
	if event.Verify(wrongKeypair.PublicKey) {
		t.Error("Expected signature to fail with wrong public key")
	}

	// Verify fails if event is tampered
	tamperedEvent := event
	tamperedEvent.Target = "charlie"
	if tamperedEvent.Verify(keypair.PublicKey) {
		t.Error("Expected signature to fail with tampered event")
	}
}

func TestSocialEvent_VerifyUnsigned(t *testing.T) {
	event := NewTeaseEvent("alice", "bob", ReasonHighRestarts)
	// Don't sign it

	soul := NativeSoulCustom([]byte("test-hw-fingerprint-2"), "alice")
	keypair := DeriveKeypair(soul)

	// Verify should return false for unsigned event
	if event.Verify(keypair.PublicKey) {
		t.Error("Expected Verify to return false for unsigned event")
	}
}
