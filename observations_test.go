package nara

import (
	"testing"
	"time"
)

func TestObservations_Record(t *testing.T) {
	ln := NewLocalNara("me", "me-soul", "host", "user", "pass", -1, 0)

	obs := ln.getMeObservation()
	obs.LastSeen = 100
	ln.setMeObservation(obs)

	savedObs := ln.getMeObservation()
	if savedObs.LastSeen != 100 {
		t.Errorf("expected LastSeen 100, got %d", savedObs.LastSeen)
	}
}

func TestObservations_Online(t *testing.T) {
	ln := NewLocalNara("me", "me-soul", "host", "user", "pass", -1, 0)
	network := ln.Network
	name := "other"
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

	// 2. Transition to OFFLINE
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
