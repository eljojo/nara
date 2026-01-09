package nara

import (
	"testing"
)

// Legacy consensus tests have been removed - the neighbourhood-based consensus
// functions have been replaced by projection-based consensus using the event store.
// See consensus_events_test.go for the projection-based consensus tests.

func TestObservations_OnlineTransitions(t *testing.T) {
	t.Parallel()
	ln := NewLocalNara("me", testSoul("me"), "host", "user", "pass", -1, 0)
	network := ln.Network
	name := "target"

	network.importNara(NewNara(name))

	obs := network.local.getObservation(name)
	if obs.Online != "" {
		t.Errorf("expected initial state to be empty, got %s", obs.Online)
	}

	network.recordObservationOnlineNara(name)
	obs = network.local.getObservation(name)
	if obs.Online != "ONLINE" {
		t.Errorf("expected state ONLINE, got %s", obs.Online)
	}
}
