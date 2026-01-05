package nara

import (
	"time"
	"testing"
)

func TestScenario_Interaction(t *testing.T) {
	// 1. Initialize our local Nara (A)
	ln := NewLocalNara("blue-jay", "host", "user", "pass", -1) // use blue-jay to skip fetch
	network := ln.Network
	network.ReadOnly = true // avoid MQTT publish in tests

	// 2. Simulate Nara B joining (Hey There)
	network.handleHeyThereEvent(HeyThereEvent{From: "B"})

	// 3. Verify B is known and ONLINE
	naraB := network.getNara("B")
	if naraB.Name != "B" {
		t.Errorf("expected nara B to be known, got %v", naraB.Name)
	}
	obsB := network.local.getObservation("B")
	if obsB.Online != "ONLINE" {
		t.Errorf("expected B to be ONLINE, got %s", obsB.Online)
	}

	// 4. Simulate Nara B sending a Newspaper with status
	statusB := NaraStatus{
		Flair:      "🌊",
		Chattiness: 50,
		Observations: map[string]NaraObservation{
			"B":        {StartTime: 2000, Online: "ONLINE"},
			"blue-jay": {StartTime: 1000, Online: "ONLINE"},
		},
	}
	network.handleNewspaperEvent(NewspaperEvent{From: "B", Status: statusB})

	// 5. Verify B's status was imported
	naraB = network.getNara("B")
	if naraB.Status.Flair != "🌊" {
		t.Errorf("expected B flair to be 🌊, got %s", naraB.Status.Flair)
	}

	// 6. Simulate Nara C joining and providing a conflicting opinion about B's start time
	network.handleHeyThereEvent(HeyThereEvent{From: "C"})
	statusC := NaraStatus{
		Observations: map[string]NaraObservation{
			"B": {StartTime: 2000, Online: "ONLINE"}, // Consensus
		},
	}
	network.handleNewspaperEvent(NewspaperEvent{From: "C", Status: statusC})

	OpinionDelayOverride = 1 * time.Millisecond
	defer func() { OpinionDelayOverride = 0 }()

	// 7. Form opinion and check consensus for B
	network.formOpinion()
	obsB = network.local.getObservation("B")
	if obsB.StartTime != 2000 {
		t.Errorf("expected consensus StartTime for B to be 2000, got %d", obsB.StartTime)
	}

	// 8. Simulate Nara B leaving (Chau)
	network.handleChauEvent(Nara{Name: "B"})
	obsB = network.local.getObservation("B")
	if obsB.Online != "OFFLINE" {
		t.Errorf("expected B to be OFFLINE, got %s", obsB.Online)
	}
}
