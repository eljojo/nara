package nara

import (
	"testing"
	"time"

	"github.com/eljojo/nara/types"
)

// Test creating a restart observation event
func TestObservationEvent_Restart(t *testing.T) {
	observer := types.NaraName("nara-a")
	subject := types.NaraName("nara-b")
	startTime := int64(1234567890)
	restartNum := int64(42)

	event := NewRestartObservationEvent(observer, subject, startTime, restartNum)

	if event.Service != ServiceObservation {
		t.Errorf("Expected service='observation', got '%s'", event.Service)
	}

	if event.Observation == nil {
		t.Fatal("Expected Observation payload, got nil")
	}

	if event.Observation.Observer != observer {
		t.Errorf("Expected observer='%s', got '%s'", observer, event.Observation.Observer)
	}

	if event.Observation.Subject != subject {
		t.Errorf("Expected subject='%s', got '%s'", subject, event.Observation.Subject)
	}

	if event.Observation.Type != "restart" {
		t.Errorf("Expected type='restart', got '%s'", event.Observation.Type)
	}

	if event.Observation.Importance != ImportanceCritical {
		t.Errorf("Expected importance=Critical (3), got %d", event.Observation.Importance)
	}

	if event.Observation.StartTime != startTime {
		t.Errorf("Expected start_time=%d, got %d", startTime, event.Observation.StartTime)
	}

	if event.Observation.RestartNum != restartNum {
		t.Errorf("Expected restart_num=%d, got %d", restartNum, event.Observation.RestartNum)
	}

	if event.Timestamp == 0 {
		t.Error("Expected non-zero timestamp")
	}
}

// Test creating a first-seen observation event
func TestObservationEvent_FirstSeen(t *testing.T) {
	observer := types.NaraName("nara-a")
	subject := types.NaraName("nara-b")
	startTime := int64(1234567890)

	event := NewFirstSeenObservationEvent(observer, subject, startTime)

	if event.Service != ServiceObservation {
		t.Errorf("Expected service='observation', got '%s'", event.Service)
	}

	if event.Observation == nil {
		t.Fatal("Expected Observation payload, got nil")
	}

	if event.Observation.Type != "first-seen" {
		t.Errorf("Expected type='first-seen', got '%s'", event.Observation.Type)
	}

	if event.Observation.Importance != ImportanceCritical {
		t.Errorf("Expected importance=Critical (3), got %d", event.Observation.Importance)
	}

	if event.Observation.StartTime != startTime {
		t.Errorf("Expected start_time=%d, got %d", startTime, event.Observation.StartTime)
	}
}

// Test creating a status-change observation event
func TestObservationEvent_StatusChange(t *testing.T) {
	observer := types.NaraName("nara-a")
	subject := types.NaraName("nara-b")
	onlineState := "OFFLINE"

	event := NewStatusChangeObservationEvent(observer, subject, onlineState)

	if event.Service != ServiceObservation {
		t.Errorf("Expected service='observation', got '%s'", event.Service)
	}

	if event.Observation == nil {
		t.Fatal("Expected Observation payload, got nil")
	}

	if event.Observation.Type != "status-change" {
		t.Errorf("Expected type='status-change', got '%s'", event.Observation.Type)
	}

	if event.Observation.Importance != ImportanceNormal {
		t.Errorf("Expected importance=Normal (2), got %d", event.Observation.Importance)
	}

	if event.Observation.OnlineState != onlineState {
		t.Errorf("Expected online_state='%s', got '%s'", onlineState, event.Observation.OnlineState)
	}
}

// Test creating a backfill observation event
func TestObservationEvent_Backfill(t *testing.T) {
	observer := types.NaraName("nara-a")
	subject := types.NaraName("nara-b")
	startTime := int64(1234567890)
	restartNum := int64(1137)

	event := NewBackfillObservationEvent(observer, subject, startTime, restartNum, startTime)

	if event.Service != ServiceObservation {
		t.Errorf("Expected service='observation', got '%s'", event.Service)
	}

	if event.Observation == nil {
		t.Fatal("Expected Observation payload, got nil")
	}

	if event.Observation.Type != "restart" {
		t.Errorf("Expected type='restart' for backfill, got '%s'", event.Observation.Type)
	}

	if event.Observation.Importance != ImportanceCritical {
		t.Errorf("Expected importance=Critical (3), got %d", event.Observation.Importance)
	}

	if !event.Observation.IsBackfill {
		t.Error("Expected IsBackfill=true, got false")
	}

	if event.Observation.RestartNum != restartNum {
		t.Errorf("Expected restart_num=%d, got %d", restartNum, event.Observation.RestartNum)
	}
}

// Test importance-based filtering with personality
func TestPersonalityFiltering_Importance(t *testing.T) {
	// Very chill personality (>85)
	chillPersonality := NaraPersonality{Chill: 90, Sociability: 50, Agreeableness: 50}

	ledger := NewSyncLedger(100)

	// Critical event (restart) - should NEVER be filtered
	restartEvent := NewRestartObservationEvent(types.NaraName("a"), types.NaraName("b"), time.Now().Unix(), 10)
	added := ledger.AddEventFiltered(restartEvent, chillPersonality)
	if !added {
		t.Error("Critical events (restart) must never be filtered by personality")
	}

	// Critical event (first-seen) - should NEVER be filtered
	firstSeenEvent := NewFirstSeenObservationEvent(types.NaraName("a"), types.NaraName("c"), time.Now().Unix())
	added = ledger.AddEventFiltered(firstSeenEvent, chillPersonality)
	if !added {
		t.Error("Critical events (first-seen) must never be filtered by personality")
	}

	// Critical event (backfill) - should NEVER be filtered
	backfillEvent := NewBackfillObservationEvent(types.NaraName("a"), types.NaraName("d"), time.Now().Unix(), 5, time.Now().Unix())
	added = ledger.AddEventFiltered(backfillEvent, chillPersonality)
	if !added {
		t.Error("Critical events (backfill) must never be filtered by personality")
	}

	// Normal event (status change) - may be filtered by very chill (>85)
	statusEvent := NewStatusChangeObservationEvent(types.NaraName("a"), types.NaraName("e"), "OFFLINE")
	// Note: Implementation will determine if this gets filtered
	// For now just verify it can be added
	ledger.AddEventFiltered(statusEvent, chillPersonality)
}

// Test observation event validation
func TestObservationEvent_Validation(t *testing.T) {
	// Valid restart event
	validEvent := NewRestartObservationEvent(types.NaraName("a"), types.NaraName("b"), time.Now().Unix(), 5)
	if !validEvent.IsValid() {
		t.Error("Valid restart event should pass validation")
	}

	// Invalid: empty observer
	invalidEvent := SyncEvent{
		Timestamp: time.Now().UnixNano(),
		Service:   ServiceObservation,
		Observation: &ObservationEventPayload{
			Observer:   types.NaraName(""), // Invalid
			Subject:    types.NaraName("b"),
			Type:       "restart",
			Importance: ImportanceCritical,
		},
	}
	if invalidEvent.IsValid() {
		t.Error("Event with empty observer should fail validation")
	}

	// Invalid: empty subject
	invalidEvent2 := SyncEvent{
		Timestamp: time.Now().UnixNano(),
		Service:   ServiceObservation,
		Observation: &ObservationEventPayload{
			Observer:   types.NaraName("a"),
			Subject:    types.NaraName(""), // Invalid
			Type:       "restart",
			Importance: ImportanceCritical,
		},
	}
	if invalidEvent2.IsValid() {
		t.Error("Event with empty subject should fail validation")
	}

	// Invalid: unknown type
	invalidEvent3 := SyncEvent{
		Timestamp: time.Now().UnixNano(),
		Service:   ServiceObservation,
		Observation: &ObservationEventPayload{
			Observer:   types.NaraName("a"),
			Subject:    types.NaraName("b"),
			Type:       "unknown-type", // Invalid
			Importance: ImportanceCritical,
		},
	}
	if invalidEvent3.IsValid() {
		t.Error("Event with unknown type should fail validation")
	}
}

// Test ID computation for observation events
func TestObservationEvent_IDComputation(t *testing.T) {
	event1 := NewRestartObservationEvent(types.NaraName("a"), types.NaraName("b"), 1234567890, 5)
	event1.ComputeID()

	if event1.ID == "" {
		t.Error("Event ID should be computed")
	}

	if len(event1.ID) != 32 {
		t.Errorf("Expected ID length 32 (16 bytes as hex), got %d", len(event1.ID))
	}

	// Same event should generate same ID
	event2 := NewRestartObservationEvent(types.NaraName("a"), types.NaraName("b"), 1234567890, 5)
	event2.Timestamp = event1.Timestamp // Use same timestamp
	event2.ComputeID()

	if event1.ID != event2.ID {
		t.Error("Same event data should generate same ID")
	}

	// Different event should generate different ID
	event3 := NewRestartObservationEvent(types.NaraName("a"), types.NaraName("b"), 1234567890, 6) // Different restart num
	event3.Timestamp = event1.Timestamp
	event3.ComputeID()

	if event1.ID == event3.ID {
		t.Error("Different event data should generate different ID")
	}
}

// Test payload interface implementation
func TestObservationEvent_PayloadInterface(t *testing.T) {
	event := NewRestartObservationEvent(types.NaraName("observer-a"), types.NaraName("subject-b"), time.Now().Unix(), 10)

	payload := event.Payload()
	if payload == nil {
		t.Fatal("Expected payload, got nil")
	}

	// Test GetActor (should return Observer)
	actor := event.Observation.GetActor()
	if actor != "observer-a" {
		t.Errorf("Expected GetActor='observer-a', got '%s'", actor)
	}

	// Test GetTarget (should return Subject)
	target := event.Observation.GetTarget()
	if target != "subject-b" {
		t.Errorf("Expected GetTarget='subject-b', got '%s'", target)
	}

	// Test IsValid
	if !event.Observation.IsValid() {
		t.Error("Valid observation payload should pass IsValid()")
	}

	// Test ContentString (for hashing)
	contentStr := event.Observation.ContentString()
	if contentStr == "" {
		t.Error("ContentString should not be empty")
	}
}
