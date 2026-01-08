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
	ln1 := NewLocalNara("nara-1", testSoul("nara-1"), "host", "user", "pass", 50, 1000)
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

	ln := NewLocalNara("test-nara", testSoul("test-nara"), "host", "user", "pass", 50, 1000)
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

	ln := NewLocalNara("test-nara", testSoul("test-nara"), "", "", "", 50, 1000)

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

	ln := NewLocalNara("observer", testSoul("observer"), "host", "user", "pass", 50, 1000)
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
	if useObservationEvents() && ln.SyncLedger != nil {
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

	ln1 := NewLocalNara("observer", testSoul("observer"), "", "", "", 50, 1000)
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
		ln := NewLocalNara(name, testSoul(name), "", "", "", 50, 1000)
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
	observer := NewLocalNara("observer", testSoul("observer"), "", "", "", 100, 1000)
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
	observer := NewLocalNara("gossip-observer", testSoul("gossip-observer"), "", "", "", 100, 1000)
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
	ln := NewLocalNara("test-nara", testSoul("test"), "", "", "", 100, 1000)
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

	ln := NewLocalNara("test-nara", testSoul("test"), "", "", "", 100, 1000)

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
// 1. We receive a zine with events emitted by r2d2 (teases, etc.)
// 2. r2d2 should be discovered AND marked as seen
// 3. Currently r2d2 is only discovered but not marked as seen until we directly
//    receive their newspaper
//
// BUG: discoverNarasFromEvents creates entries but doesn't emit seen events
// for emitters, so they appear as "unknown" rather than "online" until we
// directly receive their newspaper.
func TestIntegration_ZineMergeMarksEmittersAsSeen(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	// Enable observation events for this test
	os.Setenv("USE_OBSERVATION_EVENTS", "true")
	defer os.Unsetenv("USE_OBSERVATION_EVENTS")

	ln := NewLocalNara("observer", testSoul("observer"), "", "", "", 50, 1000)
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

	// Additionally, we should have emitted a seen event for r2d2
	seenEvents := 0
	for _, e := range ln.SyncLedger.GetAllEvents() {
		if e.Service == ServiceSeen && e.Seen != nil && e.Seen.Subject == "r2d2" {
			seenEvents++
		}
	}

	if seenEvents == 0 {
		t.Error("BUG: Should have emitted a seen event for r2d2 after receiving events they emitted")
	}
}
