package nara

import (
	"testing"
	"time"
)

// Test that we can add up to 20 observation events per observer→subject pair
func TestObservationCompaction_UnderLimit(t *testing.T) {
	ledger := NewSyncLedger(1000)
	observer := "nara-a"
	subject := "nara-b"

	// Add 20 events for the same observer→subject pair
	for i := 0; i < 20; i++ {
		event := NewRestartObservationEvent(observer, subject, time.Now().Unix(), int64(i))
		added := ledger.AddEvent(event)
		if !added {
			t.Errorf("Event %d should be added (under limit)", i)
		}
	}

	// Verify all 20 events are present
	events := ledger.GetObservationEventsAbout(subject)
	count := 0
	for _, e := range events {
		if e.Observation.Observer == observer && e.Observation.Subject == subject {
			count++
		}
	}

	if count != 20 {
		t.Errorf("Expected 20 events for %s→%s, got %d", observer, subject, count)
	}
}

// Test that adding the 21st event evicts the oldest
func TestObservationCompaction_OverLimit(t *testing.T) {
	ledger := NewSyncLedger(1000)
	observer := "nara-a"
	subject := "nara-b"

	// Add 21 events with distinct restart numbers
	for i := 0; i < 21; i++ {
		event := NewRestartObservationEvent(observer, subject, time.Now().Unix(), int64(i))
		time.Sleep(1 * time.Millisecond) // Ensure different timestamps
		ledger.AddEvent(event)
	}

	// Should have exactly 20 events
	events := ledger.GetObservationEventsAbout(subject)
	count := 0
	for _, e := range events {
		if e.Observation.Observer == observer && e.Observation.Subject == subject {
			count++
		}
	}

	if count != 20 {
		t.Errorf("Expected exactly 20 events after compaction, got %d", count)
	}

	// The oldest event (restart_num=0) should be evicted
	hasOldest := false
	hasNewest := false
	for _, e := range events {
		if e.Observation.Observer == observer && e.Observation.Subject == subject {
			if e.Observation.RestartNum == 0 {
				hasOldest = true
			}
			if e.Observation.RestartNum == 20 {
				hasNewest = true
			}
		}
	}

	if hasOldest {
		t.Error("Oldest event (restart_num=0) should have been evicted")
	}

	if !hasNewest {
		t.Error("Newest event (restart_num=20) should be present")
	}
}

// Test that multiple observer→subject pairs don't interfere with each other
func TestObservationCompaction_MultiplePairs(t *testing.T) {
	ledger := NewSyncLedger(1000)

	// Add 20 events for alice→bob
	for i := 0; i < 20; i++ {
		event := NewRestartObservationEvent("alice", "bob", time.Now().Unix(), int64(i))
		ledger.AddEvent(event)
	}

	// Add 20 events for alice→charlie
	for i := 0; i < 20; i++ {
		event := NewRestartObservationEvent("alice", "charlie", time.Now().Unix(), int64(i))
		ledger.AddEvent(event)
	}

	// Add 20 events for dave→bob
	for i := 0; i < 20; i++ {
		event := NewRestartObservationEvent("dave", "bob", time.Now().Unix(), int64(i))
		ledger.AddEvent(event)
	}

	// Each pair should have 20 events
	bobEvents := ledger.GetObservationEventsAbout("bob")
	aliceToBobCount := 0
	daveToBobCount := 0
	for _, e := range bobEvents {
		if e.Observation.Observer == "alice" {
			aliceToBobCount++
		}
		if e.Observation.Observer == "dave" {
			daveToBobCount++
		}
	}

	if aliceToBobCount != 20 {
		t.Errorf("Expected 20 events for alice→bob, got %d", aliceToBobCount)
	}

	if daveToBobCount != 20 {
		t.Errorf("Expected 20 events for dave→bob, got %d", daveToBobCount)
	}

	charlieEvents := ledger.GetObservationEventsAbout("charlie")
	aliceToCharlieCount := 0
	for _, e := range charlieEvents {
		if e.Observation.Observer == "alice" {
			aliceToCharlieCount++
		}
	}

	if aliceToCharlieCount != 20 {
		t.Errorf("Expected 20 events for alice→charlie, got %d", aliceToCharlieCount)
	}
}

// Test compaction with different observation types
func TestObservationCompaction_MixedTypes(t *testing.T) {
	ledger := NewSyncLedger(1000)
	observer := "nara-a"
	subject := "nara-b"

	// Add 10 restart events
	for i := 0; i < 10; i++ {
		event := NewRestartObservationEvent(observer, subject, time.Now().Unix(), int64(i))
		ledger.AddEvent(event)
	}

	// Add 10 status-change events
	for i := 0; i < 10; i++ {
		var state string
		if i%2 == 0 {
			state = "ONLINE"
		} else {
			state = "OFFLINE"
		}
		event := NewStatusChangeObservationEvent(observer, subject, state)
		ledger.AddEvent(event)
	}

	// Should have exactly 20 events total
	events := ledger.GetObservationEventsAbout(subject)
	count := 0
	for _, e := range events {
		if e.Observation.Observer == observer && e.Observation.Subject == subject {
			count++
		}
	}

	if count != 20 {
		t.Errorf("Expected 20 events total (mixed types), got %d", count)
	}

	// Add 5 more restart events
	for i := 10; i < 15; i++ {
		event := NewRestartObservationEvent(observer, subject, time.Now().Unix(), int64(i))
		ledger.AddEvent(event)
	}

	// Should still have exactly 20 events (compaction should evict oldest 5)
	events = ledger.GetObservationEventsAbout(subject)
	count = 0
	for _, e := range events {
		if e.Observation.Observer == observer && e.Observation.Subject == subject {
			count++
		}
	}

	if count != 20 {
		t.Errorf("Expected 20 events after adding more (should compact), got %d", count)
	}
}

// Test compaction behavior at exact limit
func TestObservationCompaction_ExactLimit(t *testing.T) {
	ledger := NewSyncLedger(1000)
	observer := "nara-a"
	subject := "nara-b"

	// Add exactly 20 events
	for i := 0; i < 20; i++ {
		event := NewRestartObservationEvent(observer, subject, time.Now().Unix(), int64(i))
		ledger.AddEvent(event)
	}

	events := ledger.GetObservationEventsAbout(subject)
	count := 0
	for _, e := range events {
		if e.Observation.Observer == observer && e.Observation.Subject == subject {
			count++
		}
	}

	if count != 20 {
		t.Errorf("Expected exactly 20 events at limit, got %d", count)
	}

	// Verify no compaction happened yet (all original events should be present)
	for i := 0; i < 20; i++ {
		found := false
		for _, e := range events {
			if e.Observation.Observer == observer &&
				e.Observation.Subject == subject &&
				e.Observation.RestartNum == int64(i) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected to find restart_num=%d, but it's missing", i)
		}
	}
}

// Test heavy compaction (adding 100 events, keeping only 20)
func TestObservationCompaction_HeavyLoad(t *testing.T) {
	ledger := NewSyncLedger(2000)
	observer := "nara-a"
	subject := "nara-b"

	// Add 100 events
	for i := 0; i < 100; i++ {
		event := NewRestartObservationEvent(observer, subject, time.Now().Unix(), int64(i))
		ledger.AddEvent(event)
	}

	// Should have exactly 20 events (most recent)
	events := ledger.GetObservationEventsAbout(subject)
	count := 0
	for _, e := range events {
		if e.Observation.Observer == observer && e.Observation.Subject == subject {
			count++
		}
	}

	if count != 20 {
		t.Errorf("Expected 20 events after heavy compaction, got %d", count)
	}

	// Should have events 80-99 (most recent 20)
	for i := 80; i < 100; i++ {
		found := false
		for _, e := range events {
			if e.Observation.Observer == observer &&
				e.Observation.Subject == subject &&
				e.Observation.RestartNum == int64(i) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected to find recent restart_num=%d, but it's missing", i)
		}
	}

	// Should NOT have events 0-79
	for i := 0; i < 80; i++ {
		found := false
		for _, e := range events {
			if e.Observation.Observer == observer &&
				e.Observation.Subject == subject &&
				e.Observation.RestartNum == int64(i) {
				found = true
				break
			}
		}
		if found {
			t.Errorf("Old event restart_num=%d should have been compacted away", i)
		}
	}
}

// Test that compaction preserves critical importance events
func TestObservationCompaction_PreservesImportance(t *testing.T) {
	ledger := NewSyncLedger(1000)
	observer := "nara-a"
	subject := "nara-b"

	// Add 25 critical events (restarts and first-seen)
	event := NewFirstSeenObservationEvent(observer, subject, time.Now().Unix())
	ledger.AddEvent(event)

	for i := 0; i < 24; i++ {
		event := NewRestartObservationEvent(observer, subject, time.Now().Unix(), int64(i))
		ledger.AddEvent(event)
	}

	// Should compact to 20 events, but all should still be critical
	events := ledger.GetObservationEventsAbout(subject)
	for _, e := range events {
		if e.Observation.Observer == observer && e.Observation.Subject == subject {
			if e.Observation.Importance != ImportanceCritical {
				t.Errorf("Expected all compacted events to be Critical, got importance=%d", e.Observation.Importance)
			}
		}
	}
}

// Test compaction with backfill events
func TestObservationCompaction_WithBackfill(t *testing.T) {
	ledger := NewSyncLedger(1000)
	observer := "nara-a"
	subject := "nara-b"

	// Add 15 backfill events
	for i := 0; i < 15; i++ {
		event := NewBackfillObservationEvent(observer, subject, time.Now().Unix(), int64(i), time.Now().Unix())
		ledger.AddEvent(event)
	}

	// Add 10 regular restart events
	for i := 15; i < 25; i++ {
		event := NewRestartObservationEvent(observer, subject, time.Now().Unix(), int64(i))
		ledger.AddEvent(event)
	}

	// Should have exactly 20 events (5 oldest backfills evicted)
	events := ledger.GetObservationEventsAbout(subject)
	count := 0
	for _, e := range events {
		if e.Observation.Observer == observer && e.Observation.Subject == subject {
			count++
		}
	}

	if count != 20 {
		t.Errorf("Expected 20 events after backfill+regular compaction, got %d", count)
	}
}
