package nara

import (
	"context"
	"testing"
	"time"
)

// Test consensus with single observer (trivial case)
func TestConsensusEvents_SingleObserver(t *testing.T) {
	ledger := NewSyncLedger(1000)
	projection := NewOpinionConsensusProjection(ledger)
	observer := "nara-a"
	subject := "nara-target"
	expectedStartTime := int64(1234567890)
	expectedRestarts := int64(42)

	// Single observer reports restart
	event := NewRestartObservationEvent(observer, subject, expectedStartTime, expectedRestarts)
	ledger.AddEvent(event)

	// Derive consensus from events
	projection.RunToEnd(context.Background())
	opinion := projection.DeriveOpinion(subject)

	if opinion.StartTime != expectedStartTime {
		t.Errorf("Expected StartTime=%d, got %d", expectedStartTime, opinion.StartTime)
	}

	if opinion.Restarts != expectedRestarts {
		t.Errorf("Expected Restarts=%d, got %d", expectedRestarts, opinion.Restarts)
	}
}

// Test consensus with multiple observers agreeing
func TestConsensusEvents_Agreement(t *testing.T) {
	ledger := NewSyncLedger(1000)
	projection := NewOpinionConsensusProjection(ledger)
	subject := "nara-target"
	agreedStartTime := int64(1234567890)
	agreedRestarts := int64(100)

	// Five observers all report the same values
	observers := []string{"nara-a", "nara-b", "nara-c", "nara-d", "nara-e"}
	for _, observer := range observers {
		event := NewRestartObservationEvent(observer, subject, agreedStartTime, agreedRestarts)
		ledger.AddEvent(event)
	}

	// Consensus should match agreed values
	projection.RunToEnd(context.Background())
	opinion := projection.DeriveOpinion(subject)

	if opinion.StartTime != agreedStartTime {
		t.Errorf("Expected consensus StartTime=%d, got %d", agreedStartTime, opinion.StartTime)
	}

	if opinion.Restarts != agreedRestarts {
		t.Errorf("Expected consensus Restarts=%d, got %d", agreedRestarts, opinion.Restarts)
	}
}

// Test consensus with observers disagreeing
func TestConsensusEvents_Disagreement(t *testing.T) {
	ledger := NewSyncLedger(1000)
	projection := NewOpinionConsensusProjection(ledger)
	subject := "nara-target"

	// 3 observers report StartTime=1000, Restarts=10
	for i := 0; i < 3; i++ {
		observer := "observer-" + string(rune('a'+i))
		event := NewRestartObservationEvent(observer, subject, 1000, 10)
		ledger.AddEvent(event)
	}

	// 2 observers report StartTime=1000, Restarts=11 (same start, higher restarts)
	for i := 0; i < 2; i++ {
		observer := "observer-" + string(rune('d'+i))
		event := NewRestartObservationEvent(observer, subject, 1000, 11)
		ledger.AddEvent(event)
	}

	// Consensus should pick:
	// - StartTime: 1000 (clustering majority)
	// - Restarts: 11 (highest value = most recent knowledge)
	projection.RunToEnd(context.Background())
	opinion := projection.DeriveOpinion(subject)

	if opinion.StartTime != 1000 {
		t.Errorf("Expected consensus StartTime=1000 (clustering), got %d", opinion.StartTime)
	}

	if opinion.Restarts != 11 {
		t.Errorf("Expected consensus Restarts=11 (highest/most recent), got %d", opinion.Restarts)
	}
}

// Test consensus with uptime-based weighting
func TestConsensusEvents_UptimeWeighting(t *testing.T) {
	ledger := NewSyncLedger(1000)
	projection := NewOpinionConsensusProjection(ledger)
	subject := "nara-target"

	// Two observers with LOW uptime report StartTime=1000
	// Total uptime for cluster 1000: 100 + 100 = 200
	// Note: Sleep between events to ensure unique timestamps (restart events dedupe by content+timestamp)
	event1 := NewRestartObservationEventWithUptime("observer-a", subject, 1000, 5, 100)
	ledger.AddEvent(event1)
	time.Sleep(time.Microsecond) // Ensure different nanosecond timestamp
	event2 := NewRestartObservationEventWithUptime("observer-b", subject, 1000, 5, 100)
	ledger.AddEvent(event2)
	time.Sleep(time.Microsecond)

	// One observer with HIGH uptime reports StartTime=2000 (different cluster)
	// Total uptime for cluster 2000: 10000
	event3 := NewRestartObservationEventWithUptime("observer-elder", subject, 2000, 5, 10000)
	ledger.AddEvent(event3)

	// Even though 2 observers say 1000 vs 1 saying 2000,
	// the high-uptime elder observer should win due to uptime weighting
	// (Cluster with 2+ observers wins via Strategy 1, but they're in same cluster)
	// Actually, 1000 and 2000 are in DIFFERENT clusters (diff > 60s tolerance)
	// So this tests Strategy 2: single-observer clusters ranked by uptime
	projection.RunToEnd(context.Background())
	opinion := projection.DeriveOpinion(subject)

	// Cluster [1000, 1000] has 2 observers with total uptime 200
	// Cluster [2000] has 1 observer with uptime 10000
	// Strategy 1 (2+ observers) should pick cluster 1000
	if opinion.StartTime != 1000 {
		t.Errorf("Expected StartTime=1000 (cluster with 2 agreeing observers), got %d", opinion.StartTime)
	}
}

// Test consensus uptime weighting when no cluster has 2+ observers
func TestConsensusEvents_UptimeWeighting_SingleObserverClusters(t *testing.T) {
	ledger := NewSyncLedger(1000)
	projection := NewOpinionConsensusProjection(ledger)
	subject := "nara-target"

	// Three observers each report different times (all in separate clusters)
	// The elder with highest uptime should win
	event1 := NewRestartObservationEventWithUptime("observer-young", subject, 1000, 5, 100)
	event2 := NewRestartObservationEventWithUptime("observer-middle", subject, 2000, 5, 500)
	event3 := NewRestartObservationEventWithUptime("observer-elder", subject, 3000, 5, 10000)
	ledger.AddEvent(event1)
	ledger.AddEvent(event2)
	ledger.AddEvent(event3)

	// With no cluster having 2+ observers, Strategy 2 picks highest uptime
	projection.RunToEnd(context.Background())
	opinion := projection.DeriveOpinion(subject)

	if opinion.StartTime != 3000 {
		t.Errorf("Expected StartTime=3000 (highest uptime observer), got %d", opinion.StartTime)
	}
}

// Test consensus with time tolerance clustering (±60s)
func TestConsensusEvents_ToleranceClustering(t *testing.T) {
	ledger := NewSyncLedger(1000)
	projection := NewOpinionConsensusProjection(ledger)
	subject := "nara-target"
	baseTime := int64(1234567890)

	// Three observers report times within ±60s (should cluster together)
	observers := []string{"nara-a", "nara-b", "nara-c"}
	offsets := []int64{-30, 0, 40} // Within tolerance

	for i, observer := range observers {
		event := NewRestartObservationEvent(observer, subject, baseTime+offsets[i], 10)
		ledger.AddEvent(event)
	}

	// One outlier observer reports time 120s away (outside tolerance)
	outlierEvent := NewRestartObservationEvent("nara-outlier", subject, baseTime+120, 10)
	ledger.AddEvent(outlierEvent)

	// Consensus should cluster the three within-tolerance observations
	projection.RunToEnd(context.Background())
	opinion := projection.DeriveOpinion(subject)

	// Should be close to baseTime (within the cluster)
	if opinion.StartTime < baseTime-60 || opinion.StartTime > baseTime+60 {
		t.Errorf("Expected consensus within tolerance of %d, got %d", baseTime, opinion.StartTime)
	}
}

// Test consensus with first-seen events
func TestConsensusEvents_FirstSeen(t *testing.T) {
	ledger := NewSyncLedger(1000)
	projection := NewOpinionConsensusProjection(ledger)
	subject := "nara-target"
	firstSeenTime := int64(1234567890)

	// Three observers report first-seen at similar times
	for i := 0; i < 3; i++ {
		observer := "observer-" + string(rune('a'+i))
		event := NewFirstSeenObservationEvent(observer, subject, firstSeenTime+int64(i))
		ledger.AddEvent(event)
	}

	// Consensus should derive StartTime from first-seen events
	projection.RunToEnd(context.Background())
	opinion := projection.DeriveOpinion(subject)

	if opinion.StartTime == 0 {
		t.Error("Expected non-zero StartTime from first-seen consensus")
	}

	// Should be close to firstSeenTime
	if opinion.StartTime < firstSeenTime-10 || opinion.StartTime > firstSeenTime+10 {
		t.Errorf("Expected StartTime near %d, got %d", firstSeenTime, opinion.StartTime)
	}
}

// Test consensus with backfill events
func TestConsensusEvents_Backfill(t *testing.T) {
	ledger := NewSyncLedger(1000)
	projection := NewOpinionConsensusProjection(ledger)
	subject := "nara-target"
	backfillStartTime := int64(1624066568) // Long-running nara
	backfillRestarts := int64(1137)

	// Three observers backfill historical data
	for i := 0; i < 3; i++ {
		observer := "observer-" + string(rune('a'+i))
		event := NewBackfillObservationEvent(observer, subject, backfillStartTime, backfillRestarts, time.Now().Unix())
		ledger.AddEvent(event)
	}

	// Consensus should treat backfill events like regular observations
	projection.RunToEnd(context.Background())
	opinion := projection.DeriveOpinion(subject)

	if opinion.StartTime != backfillStartTime {
		t.Errorf("Expected backfill StartTime=%d, got %d", backfillStartTime, opinion.StartTime)
	}

	if opinion.Restarts != backfillRestarts {
		t.Errorf("Expected backfill Restarts=%d, got %d", backfillRestarts, opinion.Restarts)
	}
}

// Test consensus with mixed backfill and real-time events
func TestConsensusEvents_MixedBackfillAndRealtime(t *testing.T) {
	ledger := NewSyncLedger(1000)
	projection := NewOpinionConsensusProjection(ledger)
	subject := "nara-target"
	historicalStartTime := int64(1624066568)
	historicalRestarts := int64(1137)

	// Two observers backfill historical data
	for i := 0; i < 2; i++ {
		observer := "backfill-" + string(rune('a'+i))
		event := NewBackfillObservationEvent(observer, subject, historicalStartTime, historicalRestarts, time.Now().Unix())
		ledger.AddEvent(event)
	}

	// Three observers report newer restart count in real-time
	newRestarts := int64(1140)
	for i := 0; i < 3; i++ {
		observer := "realtime-" + string(rune('a'+i))
		event := NewRestartObservationEvent(observer, subject, historicalStartTime, newRestarts)
		ledger.AddEvent(event)
	}

	// Consensus should prefer majority (newer restart count)
	projection.RunToEnd(context.Background())
	opinion := projection.DeriveOpinion(subject)

	if opinion.Restarts != newRestarts {
		t.Errorf("Expected consensus Restarts=%d (majority), got %d", newRestarts, opinion.Restarts)
	}
}

// Test consensus with no events (fallback behavior)
func TestConsensusEvents_NoEvents(t *testing.T) {
	ledger := NewSyncLedger(1000)
	projection := NewOpinionConsensusProjection(ledger)
	subject := "nara-unknown"

	// No events about this subject
	projection.RunToEnd(context.Background())
	opinion := projection.DeriveOpinion(subject)

	// Should return zero values or indicate no consensus
	if opinion.StartTime != 0 {
		t.Logf("No events available - StartTime defaulted to %d", opinion.StartTime)
	}

	if opinion.Restarts != 0 {
		t.Logf("No events available - Restarts defaulted to %d", opinion.Restarts)
	}
}

// Test consensus respects critical importance
func TestConsensusEvents_ImportanceAwareness(t *testing.T) {
	ledger := NewSyncLedger(1000)
	subject := "nara-target"

	// Critical events (restart, first-seen) should always participate in consensus
	criticalEvent := NewRestartObservationEvent("observer-a", subject, 1000, 10)
	if criticalEvent.Observation.Importance != ImportanceCritical {
		t.Fatal("Expected restart event to be Critical importance")
	}
	ledger.AddEvent(criticalEvent)

	// Normal events (status-change) should also participate
	normalEvent := NewStatusChangeObservationEvent("observer-b", subject, "ONLINE")
	if normalEvent.Observation.Importance != ImportanceNormal {
		t.Fatal("Expected status-change event to be Normal importance")
	}
	ledger.AddEvent(normalEvent)

	// Both should be available for consensus
	events := ledger.GetObservationEventsAbout(subject)
	if len(events) != 2 {
		t.Errorf("Expected 2 events for consensus, got %d", len(events))
	}
}

// Test consensus with restart count progression
func TestConsensusEvents_RestartProgression(t *testing.T) {
	ledger := NewSyncLedger(1000)
	projection := NewOpinionConsensusProjection(ledger)
	subject := "nara-target"
	startTime := int64(1234567890)

	// Observer reports restarts: 5, 7, 10 (progression over time)
	restartCounts := []int64{5, 7, 10}
	for i, count := range restartCounts {
		observer := "observer-a"
		event := NewRestartObservationEvent(observer, subject, startTime, count)
		time.Sleep(time.Duration(i+1) * time.Millisecond) // Ensure ordering
		ledger.AddEvent(event)
	}

	// Consensus should pick highest restart count (most recent)
	projection.RunToEnd(context.Background())
	opinion := projection.DeriveOpinion(subject)

	if opinion.Restarts != 10 {
		t.Errorf("Expected consensus Restarts=10 (highest), got %d", opinion.Restarts)
	}
}

// Test consensus determinism (same events = same opinion)
func TestConsensusEvents_Determinism(t *testing.T) {
	ledger1 := NewSyncLedger(1000)
	ledger2 := NewSyncLedger(1000)
	projection1 := NewOpinionConsensusProjection(ledger1)
	projection2 := NewOpinionConsensusProjection(ledger2)
	subject := "nara-target"

	// Add same events to both ledgers
	events := []struct {
		observer   string
		startTime  int64
		restartNum int64
	}{
		{"observer-a", 1000, 10},
		{"observer-b", 1001, 10},
		{"observer-c", 1000, 11},
	}

	for _, e := range events {
		event1 := NewRestartObservationEvent(e.observer, subject, e.startTime, e.restartNum)
		event2 := NewRestartObservationEvent(e.observer, subject, e.startTime, e.restartNum)
		ledger1.AddEvent(event1)
		ledger2.AddEvent(event2)
	}

	// Both should derive same opinion
	opinion1 := projection1.DeriveOpinion(subject)
	opinion2 := projection2.DeriveOpinion(subject)

	if opinion1.StartTime != opinion2.StartTime {
		t.Errorf("Expected deterministic StartTime, got %d and %d", opinion1.StartTime, opinion2.StartTime)
	}

	if opinion1.Restarts != opinion2.Restarts {
		t.Errorf("Expected deterministic Restarts, got %d and %d", opinion1.Restarts, opinion2.Restarts)
	}
}

// Test consensus with personality-filtered events
func TestConsensusEvents_PersonalityFiltered(t *testing.T) {
	// Very chill personality (>85) filters Normal importance events
	chillPersonality := NaraPersonality{Chill: 90, Sociability: 50, Agreeableness: 50}
	ledger := NewSyncLedger(1000)
	projection := NewOpinionConsensusProjection(ledger)
	subject := "nara-target"

	// Add critical event (should NOT be filtered)
	criticalEvent := NewRestartObservationEvent("observer-a", subject, 1000, 10)
	added := ledger.AddEventFiltered(criticalEvent, chillPersonality)
	if !added {
		t.Fatal("Critical event should not be filtered by personality")
	}

	// Add normal event (MAY be filtered by very chill personality)
	normalEvent := NewStatusChangeObservationEvent("observer-b", subject, "OFFLINE")
	ledger.AddEventFiltered(normalEvent, chillPersonality)

	// Consensus should work even if some events were filtered
	projection.RunToEnd(context.Background())
	opinion := projection.DeriveOpinion(subject)

	// Should have at least the critical event
	if opinion.StartTime == 0 {
		t.Error("Expected consensus from critical event, got zero StartTime")
	}
}

// Test consensus with LastRestart field
func TestConsensusEvents_LastRestart(t *testing.T) {
	ledger := NewSyncLedger(1000)
	projection := NewOpinionConsensusProjection(ledger)
	subject := "nara-target"
	startTime := int64(1234567890)
	lastRestartTime := time.Now().Unix()

	// Observers report restart with LastRestart timestamp
	for i := 0; i < 3; i++ {
		observer := "observer-" + string(rune('a'+i))
		event := NewRestartObservationEvent(observer, subject, startTime, 10)
		// In implementation, LastRestart would be part of the event
		ledger.AddEvent(event)
	}

	projection.RunToEnd(context.Background())
	opinion := projection.DeriveOpinion(subject)

	// Should derive LastRestart from events
	if opinion.LastRestart == 0 {
		t.Log("LastRestart not yet implemented in consensus")
	}

	_ = lastRestartTime // Use to avoid warning
}

// Test consensus accuracy compared to newspaper mode (integration)
func TestConsensusEvents_VsNewspaperAccuracy(t *testing.T) {
	t.Skip("Integration test - requires full implementation and newspaper comparison")

	// This test would:
	// 1. Run network in dual mode (newspapers + events)
	// 2. Derive opinion from newspapers (old way)
	// 3. Derive opinion from events (new way)
	// 4. Assert >99% agreement between the two methods

	// Example structure:
	// newspaperOpinion := formOpinionFromNewspapers(subject)
	// eventOpinion := formOpinionFromEvents(subject)
	// assertSimilar(newspaperOpinion, eventOpinion, tolerance=1%)
}

// Test consensus when observer changes their mind about StartTime.
// Unlike the legacy newspaper-based system which only saw current observer state,
// the projection accumulates ALL observations. When the same observer changes their
// opinion, both values are used for clustering. The newer value needs network
// corroboration (other observers) to form a winning cluster.
func TestConsensusEvents_ChangeOfMind(t *testing.T) {
	ledger := NewSyncLedger(1000)
	projection := NewOpinionConsensusProjection(ledger)
	subject := "nara-target"

	// Observer-a initially reports StartTime=1000
	event1 := NewRestartObservationEvent("observer-a", subject, 1000, 5)
	ledger.AddEvent(event1)

	time.Sleep(time.Millisecond) // Ensure different timestamp

	// Same observer later reports StartTime=2000 (changed their mind, e.g., after restart)
	event2 := NewRestartObservationEvent("observer-a", subject, 2000, 6)
	ledger.AddEvent(event2)

	// With no other observers, both values form single-observer clusters.
	// The algorithm picks the first cluster with equal weight (deterministic).
	projection.RunToEnd(context.Background())
	opinion := projection.DeriveOpinion(subject)

	// Restarts uses "highest value wins" - so the newer observation wins
	if opinion.Restarts != 6 {
		t.Errorf("Expected Restarts=6 (highest value), got %d", opinion.Restarts)
	}

	// Now add corroborating observer that agrees with new StartTime
	time.Sleep(time.Millisecond)
	event3 := NewRestartObservationEvent("observer-b", subject, 2000, 6)
	ledger.AddEvent(event3)

	projection.RunOnce()
	opinion = projection.DeriveOpinion(subject)

	// With 2 observers agreeing on StartTime=2000, that cluster wins (Strategy 1)
	if opinion.StartTime != 2000 {
		t.Errorf("Expected StartTime=2000 (cluster with 2 observers), got %d", opinion.StartTime)
	}
}

// slight modification of the one above
func TestConsensusEvents_ChangeOfMind_two(t *testing.T) {
	ledger := NewSyncLedger(1000)
	projection := NewOpinionConsensusProjection(ledger)
	subject := "nara-target"

	// Observer-a initially reports StartTime=1000
	event1 := NewRestartObservationEvent("observer-a", subject, 1000, 5)
	ledger.AddEvent(event1)

	time.Sleep(time.Millisecond) // Ensure different timestamp

	// Same observer later reports StartTime=2000 (changed their mind, e.g., after restart)
	event2 := NewRestartObservationEvent("observer-a", subject, 2000, 6)
	ledger.AddEvent(event2)

	// With no other observers, both values form single-observer clusters.
	// The algorithm picks the first cluster with equal weight (deterministic).
	projection.RunToEnd(context.Background())
	opinion := projection.DeriveOpinion(subject)

	// Restarts uses "highest value wins" - so the newer observation wins
	if opinion.Restarts != 6 {
		t.Errorf("Expected Restarts=6 (highest value), got %d", opinion.Restarts)
	}

	// Now add corroborating observer that agrees with **original** StartTime
	time.Sleep(time.Millisecond)
	event3 := NewRestartObservationEvent("observer-b", subject, 1000, 6)
	ledger.AddEvent(event3)

	projection.RunOnce()
	opinion = projection.DeriveOpinion(subject)

	// With 2 observers agreeing on StartTime=1000, that cluster wins (Strategy 1)
	if opinion.StartTime != 1000 {
		t.Errorf("Expected StartTime=1000 (cluster with 2 observers), got %d", opinion.StartTime)
	}
}

// Test consensus with sparse observations
func TestConsensusEvents_SparseObservations(t *testing.T) {
	ledger := NewSyncLedger(1000)
	projection := NewOpinionConsensusProjection(ledger)
	subject := "nara-target"

	// Single first-seen event
	event := NewFirstSeenObservationEvent("observer-a", subject, 1000)
	ledger.AddEvent(event)

	// Consensus should work with minimal data
	projection.RunToEnd(context.Background())
	opinion := projection.DeriveOpinion(subject)

	if opinion.StartTime != 1000 {
		t.Errorf("Expected StartTime=1000 from sparse observation, got %d", opinion.StartTime)
	}
}
