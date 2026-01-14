package nara

import (
	"testing"
)

func TestScenario_VibeShift(t *testing.T) {
	ln := testLocalNara(t, "blue-jay") // use blue-jay to skip fetch
	network := ln.Network

	// Mock observations
	network.ReadOnly = true // avoid MQTT publish in tests

	// 2. Simulate Nara B joining (Hey There)
	network.handleHeyThereEvent(SyncEvent{
		Service:  ServiceHeyThere,
		HeyThere: &HeyThereEvent{From: "B", PublicKey: "dummykey"},
	})

	// 3. Verify B is known and ONLINE
	naraB := network.getNara("B")
	if naraB.Name != "B" {
		t.Errorf("expected nara B to be known, got %v", naraB.Name)
	}
	obsB := network.local.getObservation("B")
	if obsB.Online != "ONLINE" {
		t.Errorf("expected B to be ONLINE, got %s", obsB.Online)
	}

	// 4. Simulate Nara B sending a Newspaper with status (slim - no Observations)
	statusB := NaraStatus{
		Flair:      "🌊",
		Chattiness: 50,
	}
	network.handleNewspaperEvent(NewspaperEvent{From: "B", Status: statusB})

	// 5. Verify B's status was imported
	naraB = network.getNara("B")
	if naraB.Status.Flair != "🌊" {
		t.Errorf("expected B flair to be 🌊, got %s", naraB.Status.Flair)
	}

	// 6. Simulate Nara C joining
	network.handleHeyThereEvent(SyncEvent{
		Service:  ServiceHeyThere,
		HeyThere: &HeyThereEvent{From: "C", PublicKey: "dummykey"},
	})

	// 7. Add observation events for consensus (projection-based)
	// B and C both report restart observations about B with StartTime=2000
	ln.SyncLedger.AddEvent(NewRestartObservationEvent("B", "B", 2000, 1))
	ln.SyncLedger.AddEvent(NewRestartObservationEvent("C", "B", 2000, 1))

	// Set initial observation for B so it's not considered a ghost
	// (ghost detection looks at local observation's StartTime/Restarts/LastRestart)
	obsB = network.local.getObservation("B")
	obsB.StartTime = 1 // Any non-zero value prevents ghost detection
	network.local.setObservation("B", obsB)

	prevRepeat := OpinionRepeatOverride
	prevInterval := OpinionIntervalOverride
	OpinionRepeatOverride = 1
	OpinionIntervalOverride = 0
	defer func() {
		OpinionRepeatOverride = prevRepeat
		OpinionIntervalOverride = prevInterval
	}()
	if network.bootRecoveryDone != nil {
		select {
		case <-network.bootRecoveryDone:
		default:
			close(network.bootRecoveryDone)
		}
	}

	// 8. Form opinion and check consensus for B (should update from projection)
	network.formOpinion()
	obsB = network.local.getObservation("B")
	if obsB.StartTime != 2000 {
		t.Errorf("expected consensus StartTime for B to be 2000, got %d", obsB.StartTime)
	}

	// 9. Simulate Nara B leaving (Chau)
	network.handleChauEvent(SyncEvent{
		Service: ServiceChau,
		Chau:    &ChauEvent{From: "B"},
	})
	obsB = network.local.getObservation("B")
	if obsB.Online != "OFFLINE" {
		t.Errorf("expected B to be OFFLINE, got %s", obsB.Online)
	}
}
