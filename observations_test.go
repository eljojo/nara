package nara

import (
	"testing"
	"time"
)

func TestObservation_OnlineTransitions(t *testing.T) {
	ln := NewLocalNara("me", "host", "user", "pass", -1)
	network := ln.Network

	name := "test-nara"
	network.importNara(NewNara(name))

	// 1. Initial observation via recording online
	network.recordObservationOnlineNara(name)
	obs := network.local.getObservation(name)
	if obs.Online != "ONLINE" {
		t.Errorf("expected state ONLINE, got %s", obs.Online)
	}
	if obs.LastSeen == 0 {
		t.Error("expected LastSeen to be set")
	}

	// 2. Transition to OFFLINE via Chau
	// Since we can't easily simulate receiving an MQTT chau message without the client,
	// we test the internal logic used when processing chau events
	obs.Online = "OFFLINE"
	obs.LastSeen = time.Now().Unix()
	network.local.setObservation(name, obs)

	obs = network.local.getObservation(name)
	if obs.isOnline() {
		t.Error("expected isOnline() to be false")
	}

	// 3. Transition back to ONLINE (should increment restarts)
	network.recordObservationOnlineNara(name)
	obs = network.local.getObservation(name)
	if obs.Online != "ONLINE" {
		t.Errorf("expected back to ONLINE, got %s", obs.Online)
	}
	if obs.Restarts != 1 {
		t.Errorf("expected 1 restart, got %d", obs.Restarts)
	}
}

func TestObservation_OpinionFormation(t *testing.T) {
	ln := NewLocalNara("me", "host", "user", "pass", -1)
	network := ln.Network

	target := "target"
	network.importNara(NewNara(target))

	// Setup neighbors with different opinions about target's start time
	n1 := NewNara("n1")
	n1.setObservation(target, NaraObservation{StartTime: 100})
	network.importNara(n1)

	n2 := NewNara("n2")
	n2.setObservation(target, NaraObservation{StartTime: 200})
	network.importNara(n2)

	n3 := NewNara("n3")
	n3.setObservation(target, NaraObservation{StartTime: 100})
	network.importNara(n3)

	// findStartingTimeFromNeighbourhoodForNara uses simple majority/plurality
	// 100 appears twice, 200 appears once.
	startTime := network.findStartingTimeFromNeighbourhoodForNara(target)
	if startTime != 100 {
		t.Errorf("expected consensus StartTime 100, got %d", startTime)
	}
}
