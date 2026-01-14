package nara

import (
	"os"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// TestDelayedMissingReporting validates that multiple observers don't all report MISSING events
// when they detect the same nara going offline - uses "if no one says anything" algorithm
func TestDelayedMissingReporting(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow test in short mode (requires delays)")
	}

	logrus.SetLevel(logrus.ErrorLevel)

	// Enable observation events
	os.Setenv("USE_OBSERVATION_EVENTS", "true")
	defer os.Unsetenv("USE_OBSERVATION_EVENTS")

	// Create shared ledger for all observers
	sharedLedger := NewSyncLedger(1000)

	// Create 5 observers
	observers := make([]*LocalNara, 5)
	for i := 0; i < 5; i++ {
		name := "observer-" + string(rune('a'+i))
		ln := testLocalNaraWithParams(t, name, 50, 1000)
		ln.SyncLedger = sharedLedger // Share the same ledger
		delay := time.Duration(i) * 50 * time.Millisecond
		ln.Network.testObservationDelay = &delay

		// Fake older start time to not be in booting mode
		me := ln.getMeObservation()
		me.LastRestart = time.Now().Unix() - 200
		me.LastSeen = time.Now().Unix()
		ln.setMeObservation(me)

		observers[i] = ln
	}

	subject := "target-nara"

	// All observers detect the subject going MISSING at roughly the same time
	// Using goroutines to simulate concurrent detection
	for i := 0; i < 5; i++ {
		network := observers[i].Network
		go network.reportMissingWithDelay(subject)
	}

	// Wait for delayed reporting to complete
	time.Sleep(1 * time.Second)

	// Check how many MISSING events were emitted
	events := sharedLedger.GetObservationEventsAbout(subject)
	missingCount := 0
	observers_reported := make(map[string]bool)

	for _, e := range events {
		if e.Observation != nil &&
			e.Observation.Type == "status-change" &&
			e.Observation.OnlineState == "MISSING" {
			missingCount++
			observers_reported[e.Observation.Observer] = true
		}
	}

	// We expect significantly fewer events than the 5 observers
	// Due to the delayed reporting, most observers should have stayed silent
	// Ideally we'd have 1-2 events, but allow up to 3 due to timing variations
	if missingCount > 3 {
		t.Errorf("Expected at most 3 MISSING events (delayed reporting), got %d from observers: %v",
			missingCount, observers_reported)
	}

	if missingCount == 0 {
		t.Error("Expected at least 1 MISSING event to be reported")
	}

	t.Logf("✅ Delayed reporting working: %d/%d observers reported (expected 1-3)", missingCount, 5)
}

// TestDelayedMissingReporting_NoRedundancy validates that if one observer reports first,
// others stay silent
func TestDelayedMissingReporting_NoRedundancy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow test in short mode (requires delays)")
	}

	logrus.SetLevel(logrus.DebugLevel)

	// Enable observation events
	os.Setenv("USE_OBSERVATION_EVENTS", "true")
	defer os.Unsetenv("USE_OBSERVATION_EVENTS")

	sharedLedger := NewSyncLedger(1000)

	// Create first observer who reports immediately
	ln1 := testLocalNaraWithParams(t, "observer-fast", 50, 1000)
	ln1.SyncLedger = sharedLedger
	fastDelay := time.Duration(0)
	ln1.Network.testObservationDelay = &fastDelay
	me1 := ln1.getMeObservation()
	me1.LastRestart = time.Now().Unix() - 200
	me1.LastSeen = time.Now().Unix()
	ln1.setMeObservation(me1)

	// Create second observer who will check later
	ln2 := testLocalNaraWithParams(t, "observer-slow", 50, 1000)
	ln2.SyncLedger = sharedLedger
	slowDelay := 100 * time.Millisecond
	ln2.Network.testObservationDelay = &slowDelay
	me2 := ln2.getMeObservation()
	me2.LastRestart = time.Now().Unix() - 200
	me2.LastSeen = time.Now().Unix()
	ln2.setMeObservation(me2)

	subject := "target-nara"

	// First observer reports immediately (simulating being the quickest)
	event := NewStatusChangeObservationEvent("observer-fast", subject, "MISSING")
	sharedLedger.AddEventWithDedup(event)

	// Small delay to ensure the event is in the ledger
	time.Sleep(100 * time.Millisecond)

	// Second observer starts delayed reporting
	go ln2.Network.reportMissingWithDelay(subject)

	// Wait for delayed reporting to complete
	time.Sleep(1 * time.Second)

	// Check that we only have 1 MISSING event (from fast observer)
	events := sharedLedger.GetObservationEventsAbout(subject)
	missingCount := 0
	lastObserver := ""

	for _, e := range events {
		if e.Observation != nil &&
			e.Observation.Type == "status-change" &&
			e.Observation.OnlineState == "MISSING" {
			missingCount++
			lastObserver = e.Observation.Observer
		}
	}

	if missingCount != 1 {
		t.Errorf("Expected exactly 1 MISSING event (slow observer should stay silent), got %d", missingCount)
	}

	if lastObserver != "observer-fast" {
		t.Errorf("Expected event from observer-fast, got %s", lastObserver)
	}

	t.Logf("✅ Redundancy prevention working: observer-slow stayed silent when observer-fast reported first")
}

// TestDelayedRestartReporting validates that multiple observers don't all report restart events
// when they detect the same nara restarting - uses "if no one says anything" algorithm
func TestDelayedRestartReporting(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow test in short mode (requires delays)")
	}

	logrus.SetLevel(logrus.ErrorLevel)

	sharedLedger := NewSyncLedger(1000)

	observers := make([]*LocalNara, 5)
	for i := 0; i < 5; i++ {
		name := "observer-" + string(rune('a'+i))
		ln := testLocalNaraWithParams(t, name, 50, 1000)
		ln.SyncLedger = sharedLedger
		delay := time.Duration(i) * 50 * time.Millisecond
		ln.Network.testObservationDelay = &delay
		observers[i] = ln
	}

	subject := "target-nara"
	startTime := time.Now().Unix() - 300
	restartNum := int64(10)

	for i := 0; i < 5; i++ {
		network := observers[i].Network
		go network.reportRestartWithDelay(subject, startTime, restartNum)
	}

	time.Sleep(1 * time.Second)

	events := sharedLedger.GetObservationEventsAbout(subject)
	restartCount := 0
	observersReported := make(map[string]bool)

	for _, e := range events {
		if e.Observation != nil && e.Observation.Type == "restart" {
			restartCount++
			observersReported[e.Observation.Observer] = true
		}
	}

	if restartCount > 3 {
		t.Errorf("Expected at most 3 restart events (delayed reporting), got %d from observers: %v",
			restartCount, observersReported)
	}

	if restartCount == 0 {
		t.Error("Expected at least 1 restart event to be reported")
	}

	t.Logf("✅ Delayed restart reporting working: %d/%d observers reported (expected 1-3)", restartCount, 5)
}

// TestDelayedRestartReporting_NoRedundancy validates that if one observer reports first,
// others stay silent
func TestDelayedRestartReporting_NoRedundancy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow test in short mode (requires delays)")
	}

	logrus.SetLevel(logrus.DebugLevel)

	sharedLedger := NewSyncLedger(1000)

	ln1 := testLocalNaraWithParams(t, "observer-fast", 50, 1000)
	ln1.SyncLedger = sharedLedger
	fastDelay := time.Duration(0)
	ln1.Network.testObservationDelay = &fastDelay

	ln2 := testLocalNaraWithParams(t, "observer-slow", 50, 1000)
	ln2.SyncLedger = sharedLedger
	slowDelay := 100 * time.Millisecond
	ln2.Network.testObservationDelay = &slowDelay

	subject := "target-nara"
	startTime := time.Now().Unix() - 300
	restartNum := int64(10)

	event := NewRestartObservationEvent("observer-fast", subject, startTime, restartNum)
	sharedLedger.AddEventWithDedup(event)

	time.Sleep(50 * time.Millisecond)

	go ln2.Network.reportRestartWithDelay(subject, startTime, restartNum)

	time.Sleep(1 * time.Second)

	events := sharedLedger.GetObservationEventsAbout(subject)
	restartCount := 0
	lastObserver := ""

	for _, e := range events {
		if e.Observation != nil && e.Observation.Type == "restart" {
			restartCount++
			lastObserver = e.Observation.Observer
		}
	}

	if restartCount != 1 {
		t.Errorf("Expected exactly 1 restart event (slow observer should stay silent), got %d", restartCount)
	}

	if lastObserver != "observer-fast" {
		t.Errorf("Expected event from observer-fast, got %s", lastObserver)
	}

	t.Logf("✅ Redundancy prevention working: observer-slow stayed silent when observer-fast reported first")
}
