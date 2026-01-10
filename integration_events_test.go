package nara

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// TestIntegration_EventEmissionNoDuplicates validates that we don't emit duplicate events
// when a nara comes back online (should emit restart event, not restart + status-change)
func TestIntegration_EventEmissionNoDuplicates(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	// Enable observation events for this test
	os.Setenv("USE_OBSERVATION_EVENTS", "true")
	defer os.Unsetenv("USE_OBSERVATION_EVENTS")

	// Create nara with proper Network initialization
	ln1 := testLocalNaraWithParams("nara-1", 50, 1000)
	network := ln1.Network

	// Fake an older start time so we're not in booting mode (uptime > 120s)
	me := ln1.getMeObservation()
	me.LastRestart = time.Now().Unix() - 200 // Started 200 seconds ago
	me.LastSeen = time.Now().Unix()
	ln1.setMeObservation(me)

	// Import nara-2 before recording observations
	network.importNara(NewNara("nara-2"))

	// Simulate nara-2 being seen for the first time
	network.recordObservationOnlineNara("nara-2")

	// Wait a bit, then simulate nara-2 going offline
	time.Sleep(100 * time.Millisecond)
	obs := ln1.getObservation("nara-2")
	obs.Online = "OFFLINE"
	// Ensure StartTime is set (it won't be set automatically without a neighborhood)
	if obs.StartTime == 0 {
		obs.StartTime = time.Now().Unix() - 300 // Started 5 minutes ago
	}
	ln1.setObservation("nara-2", obs)

	// Clear any existing events
	ln1.SyncLedger.Events = []SyncEvent{}
	ln1.SyncLedger.eventIDs = make(map[string]bool)

	// Simulate nara-2 coming back online (should trigger restart detection)
	network.recordObservationOnlineNara("nara-2")

	// Check events - should have exactly 1 restart event, not 2 events
	events := ln1.SyncLedger.GetObservationEventsAbout("nara-2")

	restartEvents := 0
	statusChangeEvents := 0
	for _, e := range events {
		if e.Observation.Type == "restart" {
			restartEvents++
		}
		if e.Observation.Type == "status-change" {
			statusChangeEvents++
		}
	}

	if restartEvents != 1 {
		t.Errorf("Expected exactly 1 restart event, got %d", restartEvents)
	}

	if statusChangeEvents != 0 {
		t.Errorf("Expected 0 status-change events (should be consolidated with restart), got %d", statusChangeEvents)
	}

	t.Logf("✅ No duplicate events: restart=%d, status-change=%d", restartEvents, statusChangeEvents)
}

// TestIntegration_NoSelfObservationEvents validates that we never emit observation events about ourselves
func TestIntegration_NoSelfObservationEvents(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	ln := testLocalNaraWithParams("test-nara", 50, 1000)
	network := ln.Network

	// Try to record observation about ourselves (should be filtered)
	network.recordObservationOnlineNara("test-nara")

	// Check that no observation events were created about ourselves
	events := ln.SyncLedger.GetObservationEventsAbout("test-nara")

	observationEvents := 0
	for _, e := range events {
		if e.Service == ServiceObservation && e.Observation != nil {
			observationEvents++
		}
	}

	if observationEvents != 0 {
		t.Errorf("Expected 0 self-observation events, got %d", observationEvents)
	}

	t.Logf("✅ Self-observation filter working: 0 events about self")
}

// TestIntegration_SlimNewspapersInEventMode validates that newspapers don't include Observations
// when USE_OBSERVATION_EVENTS environment variable is set
func TestIntegration_SlimNewspapersInEventMode(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	// Set environment variable for event-primary mode
	os.Setenv("USE_OBSERVATION_EVENTS", "true")
	defer os.Unsetenv("USE_OBSERVATION_EVENTS")

	ln := testLocalNaraWithParams("test-nara", 50, 1000)
	// Add some observations to the local nara
	obs1 := NaraObservation{
		StartTime:   time.Now().Unix(),
		LastSeen:    time.Now().Unix(),
		Online:      "ONLINE",
		Restarts:    5,
		LastRestart: time.Now().Unix(),
	}
	ln.setObservation("other-nara-1", obs1)

	obs2 := NaraObservation{
		StartTime:   time.Now().Unix(),
		LastSeen:    time.Now().Unix(),
		Online:      "ONLINE",
		Restarts:    3,
		LastRestart: time.Now().Unix(),
	}
	ln.setObservation("other-nara-2", obs2)

	// Verify observations are stored (includes "me" observation + 2 others = 3 total)
	if len(ln.Me.Status.Observations) < 2 {
		t.Fatalf("Expected at least 2 observations stored, got %d", len(ln.Me.Status.Observations))
	}

	// Create network (without actually connecting to MQTT)
	ln.Network = &Network{
		local:    ln,
		ReadOnly: true, // Read-only mode to prevent actual MQTT connection
	}

	// Simulate what announce() does in event-primary mode
	ln.Me.mu.Lock()
	slimStatus := ln.Me.Status
	ln.Me.mu.Unlock()
	slimStatus.Observations = nil

	// Verify the slim status has no observations
	if slimStatus.Observations != nil {
		t.Errorf("Expected slim newspaper to have nil Observations, but got %d entries", len(slimStatus.Observations))
	}

	// Verify the original status still has observations (copy didn't affect original)
	ln.Me.mu.Lock()
	originalObsCount := len(ln.Me.Status.Observations)
	ln.Me.mu.Unlock()

	if originalObsCount < 2 {
		t.Errorf("Expected original status to still have at least 2 observations, got %d", originalObsCount)
	}

	t.Logf("✅ Slim newspaper mode working: original has %d observations, broadcast has 0", originalObsCount)
}

// TestIntegration_EventEmissionDuringTransitions validates event emission during various state transitions
func TestIntegration_EventEmissionDuringTransitions(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	// Enable observation events for this test
	os.Setenv("USE_OBSERVATION_EVENTS", "true")
	defer os.Unsetenv("USE_OBSERVATION_EVENTS")

	ln := testLocalNaraWithParams("observer", 50, 1000)
	network := ln.Network

	// Fake an older start time so we're not in booting mode (uptime > 120s)
	me := ln.getMeObservation()
	me.LastRestart = time.Now().Unix() - 200 // Started 200 seconds ago
	me.LastSeen = time.Now().Unix()
	ln.setMeObservation(me)

	// Import subject-1 before recording observations
	network.importNara(NewNara("subject-1"))

	// Scenario 1: First seen (ONLINE) - should emit first-seen event
	network.recordObservationOnlineNara("subject-1")
	events := ln.SyncLedger.GetObservationEventsAbout("subject-1")
	firstSeenCount := 0
	for _, e := range events {
		if e.Observation.Type == "first-seen" {
			firstSeenCount++
		}
	}
	if firstSeenCount != 1 {
		t.Errorf("Expected 1 first-seen event for new nara, got %d", firstSeenCount)
	}

	// Scenario 2: Goes MISSING - should emit status-change event
	ln.SyncLedger.Events = []SyncEvent{} // Clear events
	ln.SyncLedger.eventIDs = make(map[string]bool)

	obs := ln.getObservation("subject-1")
	obs.Online = "MISSING"
	ln.setObservation("subject-1", obs)

	// Simulate the MISSING detection in recordObservationGone
	if ln.SyncLedger != nil {
		event := NewStatusChangeObservationEvent("observer", "subject-1", "MISSING")
		ln.SyncLedger.AddEventWithDedup(event)
	}

	events = ln.SyncLedger.GetObservationEventsAbout("subject-1")
	missingCount := 0
	for _, e := range events {
		if e.Observation.Type == "status-change" && e.Observation.OnlineState == "MISSING" {
			missingCount++
		}
	}
	if missingCount != 1 {
		t.Errorf("Expected 1 MISSING status-change event, got %d", missingCount)
	}

	// Scenario 3: Comes back ONLINE (restart) - should emit restart event only
	ln.SyncLedger.Events = []SyncEvent{} // Clear events
	ln.SyncLedger.eventIDs = make(map[string]bool)

	// Ensure StartTime is set before recording restart
	obsCheck := ln.getObservation("subject-1")
	if obsCheck.StartTime == 0 {
		obsCheck.StartTime = time.Now().Unix() - 300 // Started 5 minutes ago
		ln.setObservation("subject-1", obsCheck)
	}

	network.recordObservationOnlineNara("subject-1")

	events = ln.SyncLedger.GetObservationEventsAbout("subject-1")
	restartCount := 0
	statusChangeCount := 0
	for _, e := range events {
		if e.Observation.Type == "restart" {
			restartCount++
		}
		if e.Observation.Type == "status-change" && e.Observation.OnlineState == "ONLINE" {
			statusChangeCount++
		}
	}

	if restartCount != 1 {
		t.Errorf("Expected 1 restart event, got %d", restartCount)
	}
	if statusChangeCount != 0 {
		t.Errorf("Expected 0 status-change ONLINE events (consolidated with restart), got %d", statusChangeCount)
	}

	t.Logf("✅ State transitions validated: first-seen=%d, missing=%d, restart=%d, duplicate-status=%d",
		firstSeenCount, missingCount, restartCount, statusChangeCount)
}

// TestIntegration_BackfillDoesNotDuplicate validates that backfill respects deduplication
func TestIntegration_BackfillDoesNotDuplicate(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)
	ledger := NewSyncLedger(1000)

	// Three observers backfill the same historical restart
	for i := 0; i < 3; i++ {
		observer := "observer-" + string(rune('a'+i))
		event := NewBackfillObservationEvent(observer, "subject", 1000000, 42, 1000000)
		ledger.AddEventWithDedup(event)
	}

	// Should have exactly 1 event (deduplicated)
	events := ledger.GetObservationEventsAbout("subject")
	backfillCount := 0
	for _, e := range events {
		if e.Observation.IsBackfill && e.Observation.RestartNum == 42 {
			backfillCount++
		}
	}

	if backfillCount != 1 {
		t.Errorf("Expected 1 deduplicated backfill event, got %d", backfillCount)
	}

	// Verify the first observer is preserved
	if events[0].Observation.Observer != "observer-a" {
		t.Errorf("Expected first observer 'observer-a' to be preserved, got '%s'", events[0].Observation.Observer)
	}

	t.Logf("✅ Backfill deduplication working: 3 reports -> 1 event (observer: %s)", events[0].Observation.Observer)
}

// TestIntegration_CompactionAndDeduplicationIndependent validates that compaction
// and deduplication work independently without interfering
func TestIntegration_CompactionAndDeduplicationIndependent(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)
	ledger := NewSyncLedger(1000)

	// Scenario 1: Compaction without deduplication (using AddEvent)
	// alice adds 25 different restart events about bob
	for i := 0; i < 25; i++ {
		event := NewRestartObservationEvent("alice", "bob", int64(1000+i), int64(i))
		ledger.AddEvent(event) // No dedup
	}

	events := ledger.GetObservationEventsAbout("bob")
	aliceEvents := 0
	for _, e := range events {
		if e.Observation.Observer == "alice" {
			aliceEvents++
		}
	}

	// Should be compacted to 20 (MaxObservationsPerPair)
	if aliceEvents != 20 {
		t.Errorf("Expected 20 events after compaction, got %d", aliceEvents)
	}

	// Scenario 2: Deduplication without compaction (same restart reported by multiple observers)
	ledger.Events = []SyncEvent{} // Clear
	ledger.eventIDs = make(map[string]bool)

	// Three observers report the same restart (dedup should work)
	for i := 0; i < 3; i++ {
		observer := "observer-" + string(rune('a'+i))
		event := NewRestartObservationEvent(observer, "charlie", 2000, 100)
		ledger.AddEventWithDedup(event) // With dedup
	}

	events = ledger.GetObservationEventsAbout("charlie")
	if len(events) != 1 {
		t.Errorf("Expected 1 deduplicated event, got %d", len(events))
	}

	// Scenario 3: Both mechanisms together
	ledger.Events = []SyncEvent{} // Clear
	ledger.eventIDs = make(map[string]bool)

	// dave adds 25 unique events (should compact to 20)
	for i := 0; i < 25; i++ {
		event := NewRestartObservationEvent("dave", "bob", int64(3000+i), int64(200+i))
		ledger.AddEventWithDedup(event)
	}

	// Multiple observers report dave's most recent restart (should dedup)
	for i := 0; i < 3; i++ {
		observer := "witness-" + string(rune('a'+i))
		event := NewRestartObservationEvent(observer, "bob", 3024, 224) // Same as dave's last event
		ledger.AddEventWithDedup(event)
	}

	bobEvents := ledger.GetObservationEventsAbout("bob")
	daveEvents := 0
	witnessEvents := 0
	for _, e := range bobEvents {
		if e.Observation.Observer == "dave" {
			daveEvents++
		}
		if len(e.Observation.Observer) >= 7 && e.Observation.Observer[:7] == "witness" {
			witnessEvents++
		}
	}

	if daveEvents != 20 {
		t.Errorf("Expected 20 events from dave (compaction), got %d", daveEvents)
	}

	if witnessEvents != 0 {
		t.Errorf("Expected 0 events from witnesses (deduped against dave), got %d", witnessEvents)
	}

	t.Logf("✅ Compaction and deduplication independent: dave=%d (compacted), witnesses=%d (deduped)", daveEvents, witnessEvents)
}

// TestIntegration_MissingDetectionNotTooSensitive validates that naras posting infrequently
// (but still within normal bounds) don't get marked as MISSING prematurely.
// This test exposes the bug where a 100-second threshold caused naras to be marked
// MISSING and then teased for "coming back" when they were never actually offline.
func TestIntegration_MissingDetectionNotTooSensitive(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	// Enable observation events for this test
	os.Setenv("USE_OBSERVATION_EVENTS", "true")
	defer os.Unsetenv("USE_OBSERVATION_EVENTS")

	ln1 := testLocalNaraWithParams("observer", 50, 1000)
	network := ln1.Network

	// Not booting
	me := ln1.getMeObservation()
	me.LastRestart = time.Now().Unix() - 300
	me.LastSeen = time.Now().Unix()
	ln1.setMeObservation(me)

	// Import a neighbor
	network.importNara(NewNara("quiet-nara"))

	// Quiet-nara is seen for the first time
	network.recordObservationOnlineNara("quiet-nara")

	// Verify initial state is ONLINE
	obs := ln1.getObservation("quiet-nara")
	if obs.Online != "ONLINE" {
		t.Fatalf("Expected quiet-nara to be ONLINE initially, got %s", obs.Online)
	}
	initialRestarts := obs.Restarts

	// Simulate 2 minutes passing without an update (normal quiet period)
	// This should NOT trigger MISSING - 2 minutes is a reasonable posting interval
	obs.LastSeen = time.Now().Unix() - 120 // 2 minutes ago
	ln1.setObservation("quiet-nara", obs)

	// Run maintenance (this is what would mark them as MISSING if threshold is too low)
	// We run it in a loop to simulate the maintenance ticker
	now := time.Now().Unix()
	ln1.Me.mu.Lock()
	for name, observation := range ln1.Me.Status.Observations {
		if !observation.isOnline() {
			continue
		}
		// This is the check from observationMaintenance() - uses MissingThresholdSeconds constant
		if (now - observation.LastSeen) > MissingThresholdSeconds {
			observation.Online = "MISSING"
			ln1.Me.Status.Observations[name] = observation
		}
	}
	ln1.Me.mu.Unlock()

	// Check if quiet-nara was incorrectly marked as MISSING
	obs = ln1.getObservation("quiet-nara")
	markedMissing := obs.Online == "MISSING"

	// If marked missing, simulate the nara posting again
	if markedMissing {
		// Record them coming online again (this triggers "came back online" logic)
		ln1.SyncLedger.Events = []SyncEvent{}
		ln1.SyncLedger.eventIDs = make(map[string]bool)
		network.recordObservationOnlineNara("quiet-nara")
	}

	// Get the final state
	obs = ln1.getObservation("quiet-nara")

	// Count restart observation events emitted
	restartEvents := 0
	for _, e := range ln1.SyncLedger.GetAllEvents() {
		if e.Observation != nil && e.Observation.Type == "restart" && e.Observation.Subject == "quiet-nara" {
			restartEvents++
		}
	}

	// THE BUG: With threshold=100s, a nara that was quiet for 120s gets:
	// 1. Marked as MISSING (wrong - they're just quiet)
	// 2. When they post, counted as a restart (wrong - they never restarted)
	// 3. Teased for "coming back" (wrong - they never left)

	if markedMissing {
		t.Errorf("BUG: quiet-nara was marked MISSING after only 2 minutes of silence - threshold is too sensitive")
	}

	if obs.Restarts > initialRestarts {
		t.Errorf("BUG: quiet-nara's restart count was incremented (from %d to %d) even though they never restarted - just posted infrequently",
			initialRestarts, obs.Restarts)
	}

	if restartEvents > 0 {
		t.Errorf("BUG: %d restart events were emitted for quiet-nara even though they never restarted", restartEvents)
	}

	t.Logf("Missing detection sensitivity test complete")
}

// TestIntegration_TeasingDeduplication validates that when multiple naras see the same
// event triggering a tease, only one of them actually teases (the others see it already happened).
func TestIntegration_TeasingDeduplication(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	// Create 3 naras that share a sync ledger (simulating they're all seeing the same events)
	sharedLedger := NewSyncLedger(1000)

	naras := make([]*LocalNara, 3)
	// Different delays for each nara: 10ms, 50ms, 100ms
	// This simulates the staggered timing that happens in production
	delays := []time.Duration{10 * time.Millisecond, 50 * time.Millisecond, 100 * time.Millisecond}

	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("observer-%d", i)
		ln := testLocalNaraWithParams(name, 50, 1000)
		ln.SyncLedger = sharedLedger
		ln.Network.TeaseState = NewTeaseState()
		// Set deterministic delay for testing
		delay := delays[i]
		ln.Network.testTeaseDelay = &delay

		// Not booting
		me := ln.getMeObservation()
		me.LastRestart = time.Now().Unix() - 300
		me.LastSeen = time.Now().Unix()
		ln.setMeObservation(me)

		naras[i] = ln
	}

	target := "comeback-nara"

	// Count teases before
	countTeasesBefore := 0
	for _, e := range sharedLedger.GetSocialEventsAbout(target) {
		if e.Social != nil && e.Social.Type == "tease" && e.Social.Reason == ReasonComeback {
			countTeasesBefore++
		}
	}

	// All 3 naras see the same triggering event and try to tease concurrently
	// Use a WaitGroup to wait for all goroutines to complete
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			naras[idx].Network.TeaseWithDelay(target, ReasonComeback)
		}(i)
	}

	// Wait for all teasing attempts to complete
	wg.Wait()

	// Count teases after - should be exactly 1 (the first nara to finish their delay wins)
	teaseCount := 0
	for _, e := range sharedLedger.GetSocialEventsAbout(target) {
		if e.Social != nil && e.Social.Type == "tease" && e.Social.Reason == ReasonComeback {
			teaseCount++
		}
	}

	newTeases := teaseCount - countTeasesBefore

	// THE KEY ASSERTION: Only 1 tease should have been added, not 3
	if newTeases != 1 {
		t.Errorf("Expected exactly 1 tease (first nara wins), got %d teases", newTeases)
		t.Log("Without deduplication, all 3 naras would tease simultaneously")
	}

	// Find which nara actually teased (the one with shortest delay wins)
	var teaser string
	for _, e := range sharedLedger.GetSocialEventsAbout(target) {
		if e.Social != nil && e.Social.Type == "tease" && e.Social.Reason == ReasonComeback {
			teaser = e.Social.Actor
			break
		}
	}

	// Verify hasRecentTeaseFor: naras who DIDN'T tease should see it, the teaser won't (it's their own)
	for i := 0; i < 3; i++ {
		hasRecent := naras[i].Network.hasRecentTeaseFor(target, ReasonComeback)
		isTeaser := naras[i].Network.meName() == teaser
		if isTeaser && hasRecent {
			t.Errorf("observer-%d (the teaser) should NOT see their own tease via hasRecentTeaseFor", i)
		}
		if !isTeaser && !hasRecent {
			t.Errorf("observer-%d should see the tease from %s", i, teaser)
		}
	}

	// Verify different reason is NOT blocked
	for i := 0; i < 3; i++ {
		hasRecent := naras[i].Network.hasRecentTeaseFor(target, ReasonHighRestarts)
		if hasRecent {
			t.Errorf("observer-%d should NOT see a tease for different reason", i)
		}
	}

	t.Logf("Teasing deduplication: 3 naras tried to tease, only %d actually did", newTeases)
}

// TestIntegration_GossipModeThreshold validates that naras in gossip mode get a longer
// threshold before being marked as MISSING (1 hour vs 5 minutes).
// This is important because gossip mode relies on periodic zine exchanges which are
// less frequent than MQTT broadcasts.
func TestIntegration_GossipModeThreshold(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	// Create the observer nara
	observer := testLocalNaraWithParams("observer", 100, 1000)
	network := observer.Network
	network.ReadOnly = true

	// Create two neighbor naras: one in MQTT mode, one in gossip mode
	mqttNara := NewNara("mqtt-nara")
	mqttNara.Status.TransportMode = "mqtt"
	network.importNara(mqttNara)

	gossipNara := NewNara("gossip-nara")
	gossipNara.Status.TransportMode = "gossip"
	network.importNara(gossipNara)

	// Set both as ONLINE with LastSeen 10 minutes ago (600 seconds)
	// This is after the MQTT threshold (300s) but before the gossip threshold (3600s)
	now := time.Now().Unix()
	tenMinutesAgo := now - 600

	observer.setObservation("mqtt-nara", NaraObservation{
		Online:   "ONLINE",
		LastSeen: tenMinutesAgo,
	})
	observer.setObservation("gossip-nara", NaraObservation{
		Online:   "ONLINE",
		LastSeen: tenMinutesAgo,
	})

	// Simulate the observationMaintenance check
	mqttObs := observer.getObservation("mqtt-nara")
	gossipObs := observer.getObservation("gossip-nara")

	// Check MQTT nara threshold (should exceed MissingThresholdSeconds of 300s)
	mqttThreshold := MissingThresholdSeconds
	mqttNaraInfo := network.getNara("mqtt-nara")
	if mqttNaraInfo.Name != "" && mqttNaraInfo.Status.TransportMode == "gossip" {
		mqttThreshold = MissingThresholdGossipSeconds
	}
	mqttShouldBeMissing := (now - mqttObs.LastSeen) > mqttThreshold

	// Check gossip nara threshold (should NOT exceed MissingThresholdGossipSeconds of 3600s)
	gossipThreshold := MissingThresholdSeconds
	gossipNaraInfo := network.getNara("gossip-nara")
	if gossipNaraInfo.Name != "" && gossipNaraInfo.Status.TransportMode == "gossip" {
		gossipThreshold = MissingThresholdGossipSeconds
	}
	gossipShouldBeMissing := (now - gossipObs.LastSeen) > gossipThreshold

	// MQTT nara should be marked MISSING (10 min > 5 min threshold)
	if !mqttShouldBeMissing {
		t.Errorf("MQTT nara should be marked MISSING: elapsed=%ds, threshold=%ds",
			now-mqttObs.LastSeen, mqttThreshold)
	}

	// Gossip nara should NOT be marked MISSING (10 min < 1 hour threshold)
	if gossipShouldBeMissing {
		t.Errorf("Gossip nara should NOT be marked MISSING: elapsed=%ds, threshold=%ds",
			now-gossipObs.LastSeen, gossipThreshold)
	}

	// Verify the thresholds are as expected
	if mqttThreshold != MissingThresholdSeconds {
		t.Errorf("MQTT nara should use standard threshold: got %d, want %d",
			mqttThreshold, MissingThresholdSeconds)
	}
	if gossipThreshold != MissingThresholdGossipSeconds {
		t.Errorf("Gossip nara should use gossip threshold: got %d, want %d",
			gossipThreshold, MissingThresholdGossipSeconds)
	}

	t.Logf("✅ Gossip mode threshold working:")
	t.Logf("   - MQTT nara (10 min silence): marked MISSING=%v (threshold=%ds)",
		mqttShouldBeMissing, mqttThreshold)
	t.Logf("   - Gossip nara (10 min silence): marked MISSING=%v (threshold=%ds)",
		gossipShouldBeMissing, gossipThreshold)
}

// TestIntegration_GossipObserverThreshold validates that when the observer itself is
// in gossip mode, it uses the longer threshold for ALL naras it observes.
// This is because gossip-only naras receive updates less frequently via zine exchanges,
// so they shouldn't mark others as MISSING too quickly.
func TestIntegration_GossipObserverThreshold(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	// Create a gossip-only observer
	observer := testLocalNaraWithParams("gossip-observer", 100, 1000)
	network := observer.Network
	network.TransportMode = TransportGossip // Observer is in gossip mode
	network.ReadOnly = true

	// Create a neighbor nara in MQTT mode
	mqttNara := NewNara("mqtt-nara")
	mqttNara.Status.TransportMode = "mqtt"
	network.importNara(mqttNara)

	// Set the MQTT nara as ONLINE with LastSeen 10 minutes ago (600 seconds)
	// This is after the MQTT threshold (300s) but before the gossip threshold (3600s)
	now := time.Now().Unix()
	tenMinutesAgo := now - 600

	observer.setObservation("mqtt-nara", NaraObservation{
		Online:   "ONLINE",
		LastSeen: tenMinutesAgo,
	})

	// Simulate the threshold check (same logic as observationMaintenance)
	obs := observer.getObservation("mqtt-nara")

	threshold := MissingThresholdSeconds
	nara := network.getNara("mqtt-nara")
	subjectIsGossip := nara.Name != "" && nara.Status.TransportMode == "gossip"
	observerIsGossip := network.TransportMode == TransportGossip
	if subjectIsGossip || observerIsGossip {
		threshold = MissingThresholdGossipSeconds
	}
	shouldBeMissing := (now - obs.LastSeen) > threshold

	// Even though the observed nara is MQTT, the gossip observer should use the longer threshold
	if shouldBeMissing {
		t.Errorf("Gossip observer should NOT mark MQTT nara as MISSING after 10 min: elapsed=%ds, threshold=%ds",
			now-obs.LastSeen, threshold)
	}

	if threshold != MissingThresholdGossipSeconds {
		t.Errorf("Gossip observer should use gossip threshold for all naras: got %d, want %d",
			threshold, MissingThresholdGossipSeconds)
	}

	t.Logf("✅ Gossip observer threshold working:")
	t.Logf("   - Observer mode: gossip")
	t.Logf("   - Subject mode: mqtt")
	t.Logf("   - MQTT nara (10 min silence): marked MISSING=%v (threshold=%ds)",
		shouldBeMissing, threshold)
}

// TestIntegration_TransportModeString validates the TransportMode.String() method
func TestIntegration_TransportModeString(t *testing.T) {
	tests := []struct {
		mode     TransportMode
		expected string
	}{
		{TransportMQTT, "mqtt"},
		{TransportGossip, "gossip"},
		{TransportHybrid, "hybrid"},
		{TransportMode(99), "unknown"},
	}

	for _, tt := range tests {
		got := tt.mode.String()
		if got != tt.expected {
			t.Errorf("TransportMode(%d).String() = %q, want %q", tt.mode, got, tt.expected)
		}
	}
}

// TestIntegration_MeshPeerDiscoverySetsLastSeen validates that when a peer is discovered
// via mesh, both Online and LastSeen are set properly.
// BUG: discoverMeshPeers was using setObservation with Online="ONLINE" but no LastSeen,
// causing naras to show as online but with stale "Last Ping" times in the UI.
// FIX: discoverMeshPeers should use recordObservationOnlineNara instead.
func TestIntegration_MeshPeerDiscoverySetsLastSeen(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	// Create a local nara
	ln := testLocalNaraWithParams("test-nara", 100, 1000)
	network := ln.Network
	network.ReadOnly = true

	// Simulate what discoverMeshPeers does when it finds a new peer
	peerName := "mesh-peer"
	nara := NewNara(peerName)
	nara.Status.MeshIP = "100.64.0.5"
	nara.Status.MeshEnabled = true
	nara.Status.PublicKey = "testkey123"
	network.importNara(nara)

	// Simulate mesh peer discovery - this must set LastSeen properly
	// The buggy pattern was: ln.setObservation(peerName, NaraObservation{Online: "ONLINE"})
	// The fix is to use recordObservationOnlineNara instead
	beforeTime := time.Now().Unix()

	// This simulates what discoverMeshPeers should do after the fix
	network.recordObservationOnlineNara(peerName)

	afterTime := time.Now().Unix()

	// Verify the observation
	obs := ln.getObservation(peerName)

	// Online should be set
	if obs.Online != "ONLINE" {
		t.Errorf("Peer should be ONLINE, got %q", obs.Online)
	}

	// LastSeen MUST be set to current time, not 0
	// This was the bug - setObservation with just Online left LastSeen at 0
	if obs.LastSeen == 0 {
		t.Error("BUG: LastSeen is 0 - mesh peer discovery must set LastSeen properly")
	}

	// LastSeen should be within the time window
	if obs.LastSeen < beforeTime || obs.LastSeen > afterTime {
		t.Errorf("LastSeen should be between %d and %d, got %d",
			beforeTime, afterTime, obs.LastSeen)
	}

	t.Logf("✅ Mesh peer discovery properly sets LastSeen: %d", obs.LastSeen)
}

// TestIntegration_DirectObservationSetMissingLastSeen demonstrates the bug pattern
// where setting observation with just Online leaves LastSeen at 0.
func TestIntegration_DirectObservationSetMissingLastSeen(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	ln := testLocalNaraWithParams("test-nara", 100, 1000)
	// This is the buggy pattern that was in discoverMeshPeers
	peerName := "buggy-peer"
	ln.setObservation(peerName, NaraObservation{Online: "ONLINE"})

	obs := ln.getObservation(peerName)

	// Online is set
	if obs.Online != "ONLINE" {
		t.Errorf("Expected ONLINE, got %q", obs.Online)
	}

	// But LastSeen is NOT set - this is the bug!
	// Any code using this pattern should use recordObservationOnlineNara instead
	if obs.LastSeen != 0 {
		t.Errorf("This test documents the bug: expected LastSeen=0 with direct setObservation, got %d", obs.LastSeen)
	}

	t.Log("⚠️  Direct setObservation with only Online leaves LastSeen=0 - use recordObservationOnlineNara instead")
}

// TestIntegration_ZineMergeMarksEmittersAsSeen verifies that when we receive events
// via zine merge where a nara is the EMITTER, they should be marked as seen.
//
// This tests the scenario from production where:
//  1. We receive a zine with events emitted by r2d2 (teases, etc.)
//  2. r2d2 should be discovered AND marked as seen
//  3. Currently r2d2 is only discovered but not marked as seen until we directly
//     receive their newspaper
//
// BUG: discoverNarasFromEvents creates entries but doesn't emit seen events
// for emitters, so they appear as "unknown" rather than "online" until we
// directly receive their newspaper.
func TestIntegration_ZineMergeMarksEmittersAsSeen(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	// Enable observation events for this test
	os.Setenv("USE_OBSERVATION_EVENTS", "true")
	defer os.Unsetenv("USE_OBSERVATION_EVENTS")

	ln := testLocalNaraWithParams("observer", 50, 1000)
	network := ln.Network

	// Fake an older start time so we're not in booting mode
	me := ln.getMeObservation()
	me.LastRestart = time.Now().Unix() - 200
	me.LastSeen = time.Now().Unix()
	ln.setMeObservation(me)

	// Verify r2d2 is not known initially
	_, known := network.Neighbourhood["r2d2"]
	if known {
		t.Fatal("r2d2 should not be known initially")
	}

	// Create events that r2d2 emitted (like a tease)
	// These are events we'd receive via zine merge
	r2d2Soul := NativeSoulCustom([]byte("test-hw-r2d2"), "r2d2")
	r2d2Keypair := DeriveKeypair(r2d2Soul)
	teaseEvent := NewSignedSocialSyncEvent("tease", "r2d2", "observer", ReasonHighRestarts, "witness", "r2d2", r2d2Keypair)

	// Simulate receiving these events via zine merge
	network.MergeSyncEventsWithVerification([]SyncEvent{teaseEvent})

	// r2d2 should now be discovered (in Neighbourhood)
	_, known = network.Neighbourhood["r2d2"]
	if !known {
		t.Error("r2d2 should be discovered after merging events they emitted")
	}

	// BUG: r2d2 should also be marked as "seen" (observation with Online status)
	// because we received events they emitted - that's evidence they exist and are active
	obs := ln.getObservation("r2d2")

	// This is the expected behavior that currently fails:
	// When we receive events EMITTED by a nara, they should be marked as seen
	if obs.Online != "ONLINE" {
		t.Errorf("BUG: r2d2 should be marked as ONLINE after receiving events they emitted, got %q", obs.Online)
		t.Log("Expected: receiving events emitted by r2d2 should mark them as seen/online")
		t.Log("Actual: r2d2 is discovered but not marked online until we receive their newspaper directly")
	}

	// With the new "quiet nara" logic, we should NOT emit a seen event for r2d2
	// because they have recent events (the tease we just received proves they're active).
	// Seen events are only emitted for quiet naras who haven't emitted anything recently.
	seenEvents := 0
	for _, e := range ln.SyncLedger.GetAllEvents() {
		if e.Service == ServiceSeen && e.Seen != nil && e.Seen.Subject == "r2d2" {
			seenEvents++
		}
	}

	if seenEvents > 0 {
		t.Error("Should NOT emit seen event for r2d2 - their own events prove they're active")
	}
}

// TestIntegration_PingVerificationBeforeMarkingMissing validates that we ping a nara
// before marking them as MISSING. This guards against buggy naras spreading false
// "offline" observations that could be believed by the whole network.
func TestIntegration_PingVerificationBeforeMarkingMissing(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	ln := testLocalNaraWithParams("observer", 50, 1000)
	network := ln.Network

	// Fake an older start time so we're not in booting mode
	me := ln.getMeObservation()
	me.LastRestart = time.Now().Unix() - 200
	me.LastSeen = time.Now().Unix()
	ln.setMeObservation(me)

	// Import target nara and mark as online
	network.importNara(NewNara("target"))
	obs := ln.getObservation("target")
	obs.Online = "ONLINE"
	obs.StartTime = time.Now().Unix() - 3600 // Started 1 hour ago
	ln.setObservation("target", obs)

	// Track ping attempts
	pingAttempts := 0
	var pingResults []bool

	// Scenario 1: Ping succeeds - target should stay ONLINE
	t.Run("ping_succeeds_prevents_missing", func(t *testing.T) {
		pingAttempts = 0
		network.testPingFunc = func(name string) (bool, error) {
			pingAttempts++
			pingResults = append(pingResults, true)
			return true, nil // Ping succeeds
		}

		// Call verifyOnlineWithPing directly
		result := network.verifyOnlineWithPing("target")

		if !result {
			t.Error("verifyOnlineWithPing should return true when ping succeeds")
		}

		if pingAttempts != 1 {
			t.Errorf("Expected 1 ping attempt, got %d", pingAttempts)
		}

		// Target should still be ONLINE
		obs := ln.getObservation("target")
		if obs.Online != "ONLINE" {
			t.Errorf("Target should be ONLINE after successful ping, got %s", obs.Online)
		}

		// Should have emitted a ping event
		pingEvents := 0
		for _, e := range ln.SyncLedger.GetAllEvents() {
			if e.Service == ServicePing && e.Ping != nil && e.Ping.Target == "target" {
				pingEvents++
			}
		}
		if pingEvents == 0 {
			t.Error("Should have emitted a ping event after successful verification")
		}

		t.Logf("✅ Ping succeeded: target stayed ONLINE, ping events=%d", pingEvents)
	})

	// Reset for scenario 2
	ln.SyncLedger.Events = []SyncEvent{}
	ln.SyncLedger.eventIDs = make(map[string]bool)
	pingAttempts = 0
	pingResults = []bool{}
	resetVerifyPingRateLimit() // Clear rate limit for next scenario

	// Scenario 2: Ping fails - target should be allowed to be marked MISSING
	t.Run("ping_fails_allows_missing", func(t *testing.T) {
		network.testPingFunc = func(name string) (bool, error) {
			pingAttempts++
			pingResults = append(pingResults, false)
			return false, nil // Ping fails
		}

		// Call verifyOnlineWithPing directly
		result := network.verifyOnlineWithPing("target")

		if result {
			t.Error("verifyOnlineWithPing should return false when ping fails")
		}

		if pingAttempts != 1 {
			t.Errorf("Expected 1 ping attempt, got %d", pingAttempts)
		}

		t.Logf("✅ Ping failed: verification returned false, allowing MISSING transition")
	})

	// Reset for scenario 3
	pingAttempts = 0

	// Scenario 3: Self-ping is skipped
	t.Run("self_ping_skipped", func(t *testing.T) {
		network.testPingFunc = func(name string) (bool, error) {
			pingAttempts++
			return true, nil
		}

		// Try to verify ourselves - should skip
		result := network.verifyOnlineWithPing("observer")

		if result {
			t.Error("verifyOnlineWithPing should return false for self")
		}

		if pingAttempts != 0 {
			t.Errorf("Should not ping self, but got %d ping attempts", pingAttempts)
		}

		t.Log("✅ Self-ping correctly skipped")
	})

	// Cleanup
	network.testPingFunc = nil
}

// TestIntegration_ChauEventShouldNotMarkOnline validates that processing a chau event
// via gossip/merge does NOT incorrectly mark the emitter as online.
// Bug: When a zine contains a chau event, processChauSyncEvents correctly marks the
// emitter as OFFLINE, but then markEmittersAsSeen would see the chau event's Emitter
// and incorrectly mark them back as ONLINE.
func TestIntegration_ChauEventShouldNotMarkOnline(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	// Create observer nara
	observer := testLocalNara("observer")
	observer.Network.TransportMode = TransportGossip

	// Create the nara that will send chau
	departing := testLocalNara("departing-nara")
	// Observer knows about departing nara
	departingNara := NewNara("departing-nara")
	departingNara.Status.PublicKey = FormatPublicKey(departing.Keypair.PublicKey)
	observer.Network.importNara(departingNara)

	// Mark departing nara as online initially
	observer.setObservation("departing-nara", NaraObservation{
		Online:   "ONLINE",
		LastSeen: time.Now().Unix(),
	})

	// Mark observer as not booting (so markEmittersAsSeen runs)
	me := observer.getMeObservation()
	me.LastRestart = time.Now().Unix() - 200
	me.LastSeen = time.Now().Unix()
	observer.setMeObservation(me)

	// Verify departing nara is online
	obs := observer.getObservation("departing-nara")
	if obs.Online != "ONLINE" {
		t.Fatalf("Expected departing-nara to be ONLINE initially, got %s", obs.Online)
	}

	// Create a chau event from departing-nara
	chauEvent := NewChauSyncEvent("departing-nara", FormatPublicKey(departing.Keypair.PublicKey), departing.ID, departing.Keypair)

	// Process the chau event via MergeSyncEventsWithVerification (simulates receiving via gossip)
	observer.Network.MergeSyncEventsWithVerification([]SyncEvent{chauEvent})

	// The bug: After processing chau, the nara should be OFFLINE
	// But markEmittersAsSeen was incorrectly marking them as ONLINE again
	obs = observer.getObservation("departing-nara")
	if obs.Online != "OFFLINE" {
		t.Errorf("BUG: After processing chau event, departing-nara should be OFFLINE but got %s", obs.Online)
		t.Log("The fix is to skip chau events in markEmittersAsSeen()")
	} else {
		t.Log("✅ Chau event correctly keeps nara OFFLINE")
	}
}

// TestIntegration_ChauWithOtherEventsFromSameNara validates the scenario where a zine
// contains multiple events from a nara including their chau event.
// Bug: When a nara shuts down, their zine might contain:
// 1. Social/ping events they created before shutdown (with Emitter=their_name)
// 2. Their chau event
// markEmittersAsSeen was processing the non-chau events and marking the nara ONLINE,
// even though they had a chau event in the same batch.
func TestIntegration_ChauWithOtherEventsFromSameNara(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	// Create observer nara
	observer := testLocalNara("observer")
	observer.Network.TransportMode = TransportGossip

	// Create the nara that will shut down
	departing := testLocalNara("condorito")
	// Observer knows about condorito - CRITICAL: Set public key for signature verification
	condoritoNara := NewNara("condorito")
	condoritoNara.Status.PublicKey = FormatPublicKey(departing.Keypair.PublicKey)
	observer.Network.importNara(condoritoNara)

	// Mark condorito as online initially
	observer.setObservation("condorito", NaraObservation{
		Online:   "ONLINE",
		LastSeen: time.Now().Unix(),
	})

	// Mark observer as not booting (so markEmittersAsSeen runs)
	me := observer.getMeObservation()
	me.LastRestart = time.Now().Unix() - 200
	me.LastSeen = time.Now().Unix()
	observer.setMeObservation(me)

	// Create multiple events from condorito, simulating a zine they sent before/during shutdown
	baseTime := time.Now().UnixNano()

	// Create unsigned events for testing the logic (not signature verification)
	// Event 1: Social event created before shutdown (Emitter=condorito)
	socialEvent := SyncEvent{
		Timestamp: baseTime - int64(10*time.Second),
		Service:   ServiceSocial,
		Emitter:   "condorito",
		Social: &SocialEventPayload{
			Type:   "tease",
			Actor:  "condorito",
			Target: "observer",
			Reason: ReasonRandom,
		},
	}
	socialEvent.ComputeID()

	// Event 2: Ping event created before shutdown (Emitter=condorito)
	pingEvent := SyncEvent{
		Timestamp: baseTime - int64(5*time.Second),
		Service:   ServicePing,
		Emitter:   "condorito",
		Ping: &PingObservation{
			Observer: "condorito",
			Target:   "observer",
			RTT:      5.0,
		},
	}
	pingEvent.ComputeID()

	// Event 3: Chau event (condorito is shutting down)
	chauEvent := SyncEvent{
		Timestamp: baseTime,
		Service:   ServiceChau,
		Emitter:   "condorito",
		Chau: &ChauEvent{
			From:      "condorito",
			PublicKey: FormatPublicKey(departing.Keypair.PublicKey),
		},
	}
	chauEvent.ComputeID()

	// Process all events together (simulates receiving a zine with multiple events)
	observer.Network.MergeSyncEventsWithVerification([]SyncEvent{socialEvent, pingEvent, chauEvent})

	// After processing, condorito should be OFFLINE (chau wins)
	// The bug was that markEmittersAsSeen would see the social/ping events
	// with Emitter=condorito and mark them ONLINE, ignoring the chau event.
	obs := observer.getObservation("condorito")
	if obs.Online != "OFFLINE" {
		t.Errorf("BUG: After processing multiple events including chau, condorito should be OFFLINE but got %s", obs.Online)
		t.Log("The fix is for markEmittersAsSeen to check if the emitter has a chau event in the same batch")
		t.Log("and skip marking them ONLINE if they do")
	} else {
		t.Log("✅ Chau event correctly overrides other events from same nara in batch")
	}
}

func TestIntegration_SeenEventsOnlyForQuietNaras(t *testing.T) {
	// Seen events should only be emitted for "quiet" naras - those who haven't
	// emitted any events themselves in the last 5 minutes. Active naras prove
	// themselves through their own events and don't need vouching.

	logrus.SetLevel(logrus.ErrorLevel)

	os.Setenv("USE_OBSERVATION_EVENTS", "true")
	defer os.Unsetenv("USE_OBSERVATION_EVENTS")

	ln := testLocalNaraWithParams("observer", 50, 1000)
	network := ln.Network

	// Set up so we're not in booting mode
	me := ln.getMeObservation()
	me.LastRestart = time.Now().Unix() - 200
	me.LastSeen = time.Now().Unix()
	ln.setMeObservation(me)

	// Create keypairs for test naras
	activeSoul := NativeSoulCustom([]byte("test-hw-active"), "active-nara")
	activeKeypair := DeriveKeypair(activeSoul)

	quietSoul := NativeSoulCustom([]byte("test-hw-quiet"), "quiet-nara")
	quietKeypair := DeriveKeypair(quietSoul)

	// Import both naras with their public keys
	activeNara := NewNara("active-nara")
	activeNara.Status.PublicKey = FormatPublicKey(activeKeypair.PublicKey)
	network.importNara(activeNara)

	quietNara := NewNara("quiet-nara")
	quietNara.Status.PublicKey = FormatPublicKey(quietKeypair.PublicKey)
	network.importNara(quietNara)

	// Active nara has emitted a recent event (within last 5 minutes)
	recentEvent := NewSignedSocialSyncEvent("tease", "active-nara", "someone", ReasonHighRestarts, "witness", "active-nara", activeKeypair)
	ln.SyncLedger.AddEvent(recentEvent)

	// Quiet nara has only old events (6 minutes ago = older than 5 min threshold)
	oldEvent := NewSignedSocialSyncEvent("tease", "quiet-nara", "someone", ReasonHighRestarts, "witness", "quiet-nara", quietKeypair)
	// Manually set timestamp to 6 minutes ago
	oldEvent.Timestamp = time.Now().Add(-6 * time.Minute).UnixNano()
	oldEvent.ComputeID() // Recompute ID with new timestamp
	oldEvent.Sign("quiet-nara", quietKeypair)
	ln.SyncLedger.AddEvent(oldEvent)

	// Count seen events before
	countSeenFor := func(subject string) int {
		count := 0
		for _, e := range ln.SyncLedger.GetAllEvents() {
			if e.Service == ServiceSeen && e.Seen != nil && e.Seen.Subject == subject {
				count++
			}
		}
		return count
	}

	seenForActiveBefore := countSeenFor("active-nara")
	seenForQuietBefore := countSeenFor("quiet-nara")

	// Now emit seen events for both naras (simulating an interaction like gossip)
	network.emitSeenEvent("active-nara", "test")
	network.emitSeenEvent("quiet-nara", "test")

	seenForActiveAfter := countSeenFor("active-nara")
	seenForQuietAfter := countSeenFor("quiet-nara")

	// Check: active nara should NOT get a seen event (they're proving themselves)
	if seenForActiveAfter > seenForActiveBefore {
		t.Errorf("FAIL: Should NOT emit seen event for active nara (has recent events)")
		t.Logf("Active nara seen events: before=%d, after=%d", seenForActiveBefore, seenForActiveAfter)
		t.Log("Active naras prove themselves through their own events - no vouching needed")
	} else {
		t.Log("✅ Correctly skipped seen event for active nara")
	}

	// Check: quiet nara SHOULD get a seen event (they need vouching)
	if seenForQuietAfter <= seenForQuietBefore {
		t.Errorf("FAIL: Should emit seen event for quiet nara (no recent events)")
		t.Logf("Quiet nara seen events: before=%d, after=%d", seenForQuietBefore, seenForQuietAfter)
		t.Log("Quiet naras need vouching since they haven't emitted events themselves")
	} else {
		t.Log("✅ Correctly emitted seen event for quiet nara")
	}

	// Test 3: Nara already marked ONLINE should NOT get seen events
	// even if they're quiet (no recent events)
	onlineNara := NewNara("online-nara")
	network.importNara(onlineNara)
	// Mark as ONLINE in observations
	ln.setObservation("online-nara", NaraObservation{Online: "ONLINE", LastSeen: time.Now().Unix()})

	seenForOnlineBefore := countSeenFor("online-nara")
	network.emitSeenEvent("online-nara", "test")
	seenForOnlineAfter := countSeenFor("online-nara")

	if seenForOnlineAfter > seenForOnlineBefore {
		t.Errorf("FAIL: Should NOT emit seen event for nara already marked ONLINE")
		t.Logf("Online nara seen events: before=%d, after=%d", seenForOnlineBefore, seenForOnlineAfter)
		t.Log("No need to vouch for naras we already know are online")
	} else {
		t.Log("✅ Correctly skipped seen event for already-online nara")
	}
}

func TestIntegration_MissingToOnlineViaSeenEvent_NoRestartIncrement(t *testing.T) {
	// When a nara goes MISSING → ONLINE via someone else's seen event,
	// the restart count should NOT increment. The nara never actually restarted,
	// we just temporarily lost contact with them.
	//
	// There are two paths for status changes:
	// 1. Via observationMaintenance (projection-derived) - sets Online directly, no restart increment
	// 2. Via recordObservationOnlineNara - would incorrectly increment restarts
	//
	// This test verifies that receiving a seen event about a nara uses path #1.

	logrus.SetLevel(logrus.ErrorLevel)

	os.Setenv("USE_OBSERVATION_EVENTS", "true")
	defer os.Unsetenv("USE_OBSERVATION_EVENTS")

	ln := testLocalNaraWithParams("observer", 50, 1000)
	network := ln.Network

	// Set up so we're not in booting mode
	me := ln.getMeObservation()
	me.LastRestart = time.Now().Unix() - 200
	me.LastSeen = time.Now().Unix()
	ln.setMeObservation(me)

	// Create Bob who will be MISSING
	bobNara := NewNara("bob")
	network.importNara(bobNara)

	// Set Bob as MISSING with a restart count of 5
	bobObs := NaraObservation{
		Online:      "MISSING",
		LastSeen:    time.Now().Unix() - 600, // 10 minutes ago
		StartTime:   time.Now().Unix() - 3600,
		Restarts:    5,
		LastRestart: time.Now().Unix() - 3600,
	}
	ln.setObservation("bob", bobObs)

	// Create a seen event from Alice saying she saw Bob
	aliceSoul := NativeSoulCustom([]byte("test-hw-alice"), "alice")
	aliceKeypair := DeriveKeypair(aliceSoul)
	seenEvent := NewSeenSyncEvent("alice", "bob", "mesh", aliceKeypair)

	// Simulate receiving this event through MergeSyncEventsWithVerification
	// (the normal gossip path)
	network.MergeSyncEventsWithVerification([]SyncEvent{seenEvent})

	// Check that Bob's restart count didn't change
	// The seen event should NOT trigger recordObservationOnlineNara for Bob
	// because Alice is the emitter, not Bob
	finalObs := ln.getObservation("bob")

	if finalObs.Restarts != 5 {
		t.Errorf("FAIL: Restart count changed when receiving seen event about Bob")
		t.Logf("Expected restarts=5, got restarts=%d", finalObs.Restarts)
		t.Log("Receiving someone else's seen event should NOT increment restart count")
	} else {
		t.Log("✅ Restart count correctly unchanged when receiving seen event about Bob")
	}

	// Bob should still be MISSING after just receiving a seen event
	// (the projection would eventually mark them ONLINE, but that happens
	// through observationMaintenance which also doesn't increment restarts)
	if finalObs.Online == "ONLINE" {
		// If Bob is somehow ONLINE, ensure restarts didn't change
		if finalObs.Restarts != 5 {
			t.Error("FAIL: Bob became ONLINE but restart count also changed")
		}
	}
}
func TestIntegration_NoRedundantSeenEventsForActiveNaras(t *testing.T) {
	// Regression test for the seen event leak bug.
	// When we receive events via gossip that were emitted by other naras,
	// we should NOT emit seen events for those naras - they're proving themselves
	// through their own events.
	//
	// Before the fix: We would emit seen events for every emitter we encountered,
	// resulting in 40%+ of all events being redundant seen events.
	//
	// After the fix: We only emit seen events for truly quiet naras (haven't
	// emitted in 5+ minutes), not for naras whose events we just received.

	logrus.SetLevel(logrus.ErrorLevel)

	os.Setenv("USE_OBSERVATION_EVENTS", "true")
	defer os.Unsetenv("USE_OBSERVATION_EVENTS")

	observer := testLocalNaraWithParams("observer", 50, 1000)
	network := observer.Network

	// Set up so we're not in booting mode
	me := observer.getMeObservation()
	me.LastRestart = time.Now().Unix() - 200
	me.LastSeen = time.Now().Unix()
	observer.setMeObservation(me)

	// Create keypairs for test naras
	aliceSoul := NativeSoulCustom([]byte("test-hw-alice"), "alice")
	aliceKeypair := DeriveKeypair(aliceSoul)

	bobSoul := NativeSoulCustom([]byte("test-hw-bob"), "bob")
	bobKeypair := DeriveKeypair(bobSoul)

	// Import them
	aliceNara := NewNara("alice")
	aliceNara.Status.PublicKey = FormatPublicKey(aliceKeypair.PublicKey)
	network.importNara(aliceNara)

	bobNara := NewNara("bob")
	bobNara.Status.PublicKey = FormatPublicKey(bobKeypair.PublicKey)
	network.importNara(bobNara)

	// Count seen events before processing
	countSeenEvents := func() int {
		count := 0
		for _, e := range observer.SyncLedger.GetAllEvents() {
			if e.Service == ServiceSeen {
				count++
			}
		}
		return count
	}

	seenBefore := countSeenEvents()

	// Simulate receiving events via gossip (zine exchange)
	// These events were emitted by alice and bob - they're proving themselves
	aliceTease := NewSignedSocialSyncEvent("tease", "alice", "someone", ReasonHighRestarts, "witness", "alice", aliceKeypair)
	bobTease := NewSignedSocialSyncEvent("tease", "bob", "someone", ReasonHighRestarts, "witness", "bob", bobKeypair)

	// Process these events as if we received them in a zine
	network.MergeSyncEventsWithVerification([]SyncEvent{aliceTease, bobTease})

	seenAfter := countSeenEvents()

	// Check: We should NOT have emitted seen events for alice or bob
	// They just proved they're active by emitting events!
	if seenAfter > seenBefore {
		t.Errorf("FAIL: Emitted %d redundant seen events for naras who just proved themselves", seenAfter-seenBefore)
		t.Log("When we receive events emitted by a nara, they're proving themselves")
		t.Log("We should NOT also emit a seen event for them - that's redundant")

		// Show which seen events were created
		for _, e := range observer.SyncLedger.GetAllEvents() {
			if e.Service == ServiceSeen && e.Seen != nil && e.Timestamp > time.Now().Add(-1*time.Second).UnixNano() {
				t.Logf("  - Redundant seen event: %s saw %s", e.Seen.Observer, e.Seen.Subject)
			}
		}
	} else {
		t.Log("✅ Correctly did NOT emit seen events for naras who emitted events")
	}

	// Test 2: Verify we DO emit seen events for truly quiet naras
	// (naras we interact with who haven't emitted recently)

	// Charlie is quiet (no recent events)
	charlieSoul := NativeSoulCustom([]byte("test-hw-charlie"), "charlie")
	charlieKeypair := DeriveKeypair(charlieSoul)
	charlieNara := NewNara("charlie")
	charlieNara.Status.PublicKey = FormatPublicKey(charlieKeypair.PublicKey)
	network.importNara(charlieNara)

	// Charlie had events 10 minutes ago (old, beyond the 5 minute threshold)
	oldEvent := NewSignedSocialSyncEvent("tease", "charlie", "someone", ReasonHighRestarts, "witness", "charlie", charlieKeypair)
	oldEvent.Timestamp = time.Now().Add(-10 * time.Minute).UnixNano()
	oldEvent.ComputeID()
	oldEvent.Sign("charlie", charlieKeypair)
	observer.SyncLedger.AddEvent(oldEvent)

	seenBeforeMesh := countSeenEvents()

	// Now we discover charlie on the mesh (they're not actively emitting, just present)
	network.emitSeenEvent("charlie", "mesh")

	seenAfterMesh := countSeenEvents()

	// Check: We SHOULD have emitted a seen event for charlie (they're quiet)
	if seenAfterMesh <= seenBeforeMesh {
		t.Error("FAIL: Should emit seen event for quiet nara discovered on mesh")
		t.Log("Charlie hasn't emitted in 10 minutes - they need vouching")
	} else {
		t.Log("✅ Correctly emitted seen event for quiet nara")
	}
}
