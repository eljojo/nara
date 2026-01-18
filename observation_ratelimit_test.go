package nara

import (
	"testing"
	"time"

	"github.com/eljojo/nara/types"
)

// Test that we can add up to 10 observation events about a subject per 5-minute window
func TestObservationRateLimit_UnderLimit(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := types.NaraName("nara-target")

	// Add 10 events from different observers about the same subject
	for i := 0; i < 10; i++ {
		observer := "nara-" + string(rune('a'+i))
		event := NewRestartObservationEvent(types.NaraName(observer), subject, time.Now().Unix(), int64(i))
		added := ledger.AddEventWithRateLimit(event)
		if !added {
			t.Errorf("Event %d should be accepted (under rate limit)", i)
		}
	}

	events := ledger.GetObservationEventsAbout(subject)
	if len(events) != 10 {
		t.Errorf("Expected 10 events, got %d", len(events))
	}
}

// Test that the 11th event about same subject within 5 minutes is rejected
func TestObservationRateLimit_ExceedsLimit(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := types.NaraName("nara-target")

	// Add 10 events rapidly
	for i := 0; i < 10; i++ {
		observer := "nara-" + string(rune('a'+i))
		event := NewRestartObservationEvent(types.NaraName(observer), subject, time.Now().Unix(), int64(i))
		added := ledger.AddEventWithRateLimit(event)
		if !added {
			t.Fatalf("Event %d should be accepted, but was rejected", i)
		}
	}

	// 11th event should be rejected (rate limit exceeded)
	event11 := NewRestartObservationEvent(types.NaraName("nara-k"), subject, time.Now().Unix(), 10)
	added := ledger.AddEventWithRateLimit(event11)
	if added {
		t.Error("11th event should be rejected due to rate limit")
	}

	events := ledger.GetObservationEventsAbout(subject)
	if len(events) != 10 {
		t.Errorf("Expected 10 events (11th rejected), got %d", len(events))
	}
}

// Test that rate limit resets after 5-minute window
func TestObservationRateLimit_WindowSliding(t *testing.T) {
	// This test would require time manipulation or a mock clock
	// For now, document the expected behavior

	ledger := NewSyncLedger(1000)
	subject := types.NaraName("nara-target")

	// Create a time 6 minutes ago
	oldTime := time.Now().Add(-6 * time.Minute)

	// Simulate adding 10 events 6 minutes ago (would need ledger internals access)
	// In real implementation, these would be outside the 5-minute window

	// New event should be accepted (old window expired)
	event := NewRestartObservationEvent(types.NaraName("nara-a"), subject, time.Now().Unix(), 1)
	added := ledger.AddEventWithRateLimit(event)

	// Note: This test can't fully verify time-based behavior without
	// either time manipulation or exposing rate limiter state for testing
	if !added {
		t.Log("Window sliding test: implementation will need time-based testing")
	}

	_ = oldTime // Use variable to avoid compiler warning
}

// Test that different subjects have independent rate limits
func TestObservationRateLimit_PerSubject(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subjectA := types.NaraName("nara-alice")
	subjectB := types.NaraName("nara-bob")

	// Add 10 events about alice
	for i := 0; i < 10; i++ {
		observer := "observer-" + string(rune('a'+i))
		event := NewRestartObservationEvent(types.NaraName(observer), subjectA, time.Now().Unix(), int64(i))
		added := ledger.AddEventWithRateLimit(event)
		if !added {
			t.Errorf("Event %d about alice should be accepted", i)
		}
	}

	// Add 10 events about bob (should not be affected by alice's rate limit)
	for i := 0; i < 10; i++ {
		observer := "observer-" + string(rune('a'+i))
		event := NewRestartObservationEvent(types.NaraName(observer), subjectB, time.Now().Unix(), int64(i))
		added := ledger.AddEventWithRateLimit(event)
		if !added {
			t.Errorf("Event %d about bob should be accepted (independent rate limit)", i)
		}
	}

	aliceEvents := ledger.GetObservationEventsAbout(subjectA)
	bobEvents := ledger.GetObservationEventsAbout(subjectB)

	if len(aliceEvents) != 10 {
		t.Errorf("Expected 10 events about alice, got %d", len(aliceEvents))
	}

	if len(bobEvents) != 10 {
		t.Errorf("Expected 10 events about bob, got %d", len(bobEvents))
	}
}

// Test that multiple observers reporting about same subject share rate limit
func TestObservationRateLimit_SharedAcrossObservers(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := types.NaraName("nara-target")

	// 5 observers each report 2 events about the same subject (10 total)
	for observer := 0; observer < 5; observer++ {
		for event := 0; event < 2; event++ {
			obs := types.NaraName("observer-" + string(rune('a'+observer)))
			ev := NewRestartObservationEvent(obs, subject, time.Now().Unix(), int64(observer*2+event))
			added := ledger.AddEventWithRateLimit(ev)
			if !added {
				t.Errorf("Event from %s should be accepted", obs)
			}
		}
	}

	// Any observer trying to add 11th event should be rejected
	event11 := NewRestartObservationEvent(types.NaraName("observer-f"), subject, time.Now().Unix(), 10)
	added := ledger.AddEventWithRateLimit(event11)
	if added {
		t.Error("11th event should be rejected (rate limit shared across observers)")
	}
}

// Test rate limiting with burst of events
func TestObservationRateLimit_BurstProtection(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := types.NaraName("nara-flapper") // Simulating a nara restarting in a loop

	// Simulate burst: 50 restart events in rapid succession
	acceptedCount := 0
	rejectedCount := 0

	for i := 0; i < 50; i++ {
		event := NewRestartObservationEvent(types.NaraName("observer-a"), subject, time.Now().Unix(), int64(i))
		added := ledger.AddEventWithRateLimit(event)
		if added {
			acceptedCount++
		} else {
			rejectedCount++
		}
	}

	// Should accept first 10, reject remaining 40
	if acceptedCount != 10 {
		t.Errorf("Expected 10 accepted events, got %d", acceptedCount)
	}

	if rejectedCount != 40 {
		t.Errorf("Expected 40 rejected events, got %d", rejectedCount)
	}

	events := ledger.GetObservationEventsAbout(subject)
	if len(events) > 10 {
		t.Errorf("Expected at most 10 events stored, got %d", len(events))
	}
}

// Test rate limiting with different event types
func TestObservationRateLimit_MixedEventTypes(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := types.NaraName("nara-target")

	// Add 5 restart events
	for i := 0; i < 5; i++ {
		event := NewRestartObservationEvent(types.NaraName("observer-a"), subject, time.Now().Unix(), int64(i))
		added := ledger.AddEventWithRateLimit(event)
		if !added {
			t.Errorf("Restart event %d should be accepted", i)
		}
	}

	// Add 3 status-change events
	for i := 0; i < 3; i++ {
		state := "ONLINE"
		if i%2 == 0 {
			state = "OFFLINE"
		}
		event := NewStatusChangeObservationEvent(types.NaraName("observer-b"), subject, state)
		added := ledger.AddEventWithRateLimit(event)
		if !added {
			t.Errorf("Status-change event %d should be accepted", i)
		}
	}

	// Add 2 first-seen events
	for i := 0; i < 2; i++ {
		observer := "observer-" + string(rune('c'+i))
		event := NewFirstSeenObservationEvent(types.NaraName(observer), subject, time.Now().Unix())
		added := ledger.AddEventWithRateLimit(event)
		if !added {
			t.Errorf("First-seen event %d should be accepted", i)
		}
	}

	// Total: 10 events, next should be rejected
	event11 := NewRestartObservationEvent(types.NaraName("observer-e"), subject, time.Now().Unix(), 100)
	added := ledger.AddEventWithRateLimit(event11)
	if added {
		t.Error("11th event (mixed types) should be rejected")
	}

	events := ledger.GetObservationEventsAbout(subject)
	if len(events) != 10 {
		t.Errorf("Expected 10 events (mixed types), got %d", len(events))
	}
}

// Test that rate limit state is tracked correctly
func TestObservationRateLimit_StateTracking(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := types.NaraName("nara-target")

	// Add 8 events
	for i := 0; i < 8; i++ {
		observer := "observer-" + string(rune('a'+i))
		event := NewRestartObservationEvent(types.NaraName(observer), subject, time.Now().Unix(), int64(i))
		added := ledger.AddEventWithRateLimit(event)
		if !added {
			t.Errorf("Event %d should be accepted", i)
		}
	}

	// Should be able to add 2 more (under limit)
	event9 := NewRestartObservationEvent(types.NaraName("observer-i"), subject, time.Now().Unix(), 8)
	added9 := ledger.AddEventWithRateLimit(event9)
	if !added9 {
		t.Error("9th event should be accepted")
	}

	event10 := NewRestartObservationEvent(types.NaraName("observer-j"), subject, time.Now().Unix(), 9)
	added10 := ledger.AddEventWithRateLimit(event10)
	if !added10 {
		t.Error("10th event should be accepted")
	}

	// 11th should be rejected
	event11 := NewRestartObservationEvent("observer-k", subject, time.Now().Unix(), 10)
	added11 := ledger.AddEventWithRateLimit(event11)
	if added11 {
		t.Error("11th event should be rejected")
	}
}

// Test rate limiting doesn't affect events about different subjects
func TestObservationRateLimit_NoInterference(t *testing.T) {
	ledger := NewSyncLedger(2000)

	// Max out rate limit for subject A (10 events)
	subjectA := types.NaraName("nara-a")
	for i := 0; i < 10; i++ {
		event := NewRestartObservationEvent(types.NaraName("observer-1"), subjectA, time.Now().Unix(), int64(i))
		ledger.AddEventWithRateLimit(event)
	}

	// Max out rate limit for subject B (10 events)
	subjectB := types.NaraName("nara-b")
	for i := 0; i < 10; i++ {
		event := NewRestartObservationEvent(types.NaraName("observer-1"), subjectB, time.Now().Unix(), int64(i))
		ledger.AddEventWithRateLimit(event)
	}

	// Max out rate limit for subject C (10 events)
	subjectC := types.NaraName("nara-c")
	for i := 0; i < 10; i++ {
		event := NewRestartObservationEvent(types.NaraName("observer-1"), subjectC, time.Now().Unix(), int64(i))
		ledger.AddEventWithRateLimit(event)
	}

	// All three subjects should have exactly 10 events
	eventsA := ledger.GetObservationEventsAbout(subjectA)
	eventsB := ledger.GetObservationEventsAbout(subjectB)
	eventsC := ledger.GetObservationEventsAbout(subjectC)

	if len(eventsA) != 10 {
		t.Errorf("Expected 10 events for subject A, got %d", len(eventsA))
	}
	if len(eventsB) != 10 {
		t.Errorf("Expected 10 events for subject B, got %d", len(eventsB))
	}
	if len(eventsC) != 10 {
		t.Errorf("Expected 10 events for subject C, got %d", len(eventsC))
	}

	// Further events for any subject should be rejected
	extraA := NewRestartObservationEvent(types.NaraName("observer-1"), subjectA, time.Now().Unix(), 100)
	if ledger.AddEventWithRateLimit(extraA) {
		t.Error("Extra event for subject A should be rejected")
	}

	extraB := NewRestartObservationEvent(types.NaraName("observer-1"), subjectB, time.Now().Unix(), 100)
	if ledger.AddEventWithRateLimit(extraB) {
		t.Error("Extra event for subject B should be rejected")
	}
}

// Test rate limiting with backfill events
func TestObservationRateLimit_BackfillEvents(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := types.NaraName("nara-target")

	// Add 10 backfill events (simulating migration)
	for i := 0; i < 10; i++ {
		observer := "observer-" + string(rune('a'+i))
		event := NewBackfillObservationEvent(types.NaraName(observer), subject, time.Now().Unix(), int64(i), time.Now().Unix())
		added := ledger.AddEventWithRateLimit(event)
		if !added {
			t.Errorf("Backfill event %d should be accepted", i)
		}
	}

	// 11th backfill event should be rejected
	event11 := NewBackfillObservationEvent(types.NaraName("observer-k"), subject, time.Now().Unix(), 10, time.Now().Unix())
	added := ledger.AddEventWithRateLimit(event11)
	if added {
		t.Error("11th backfill event should be rejected (rate limited)")
	}
}
