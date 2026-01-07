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
		ln := NewLocalNara(name, testSoul(name), "host", "user", "pass", 50, 1000)
		ln.SyncLedger = sharedLedger // Share the same ledger

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

	// Wait for delayed reporting to complete (max delay is 10s, so wait 12s to be safe)
	time.Sleep(12 * time.Second)

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
	logrus.SetLevel(logrus.DebugLevel)

	// Enable observation events
	os.Setenv("USE_OBSERVATION_EVENTS", "true")
	defer os.Unsetenv("USE_OBSERVATION_EVENTS")

	sharedLedger := NewSyncLedger(1000)

	// Create first observer who reports immediately
	ln1 := NewLocalNara("observer-fast", testSoul("observer-fast"), "host", "user", "pass", 50, 1000)
	ln1.SyncLedger = sharedLedger
	me1 := ln1.getMeObservation()
	me1.LastRestart = time.Now().Unix() - 200
	me1.LastSeen = time.Now().Unix()
	ln1.setMeObservation(me1)

	// Create second observer who will check later
	ln2 := NewLocalNara("observer-slow", testSoul("observer-slow"), "host", "user", "pass", 50, 1000)
	ln2.SyncLedger = sharedLedger
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
	time.Sleep(11 * time.Second)

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
