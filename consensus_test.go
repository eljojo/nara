package nara

import (
	"fmt"
	"testing"
)

func TestConsensus_FormOpinions(t *testing.T) {
	ln := NewLocalNara("me", "me-soul", "host", "user", "pass", -1)
	network := ln.Network
	target := "target"
	network.importNara(NewNara(target))

	// In a network of 4 voters, we need more than 1 vote (threshold = 4/4 = 1)
	// So 2 votes should be enough.

	// Vote 1
	n1 := NewNara("n1")
	n1.setObservation(target, NaraObservation{StartTime: 100})
	network.importNara(n1)

	// Vote 2 (Agreement)
	n2 := NewNara("n2")
	n2.setObservation(target, NaraObservation{StartTime: 100})
	network.importNara(n2)

	// Vote 3 (Dissent)
	n3 := NewNara("n3")
	n3.setObservation(target, NaraObservation{StartTime: 200})
	network.importNara(n3)

	// Vote 4 (Dissent)
	n4 := NewNara("n4")
	n4.setObservation(target, NaraObservation{StartTime: 300})
	network.importNara(n4)

	startTime := network.findStartingTimeFromNeighbourhoodForNara(target)
	if startTime != 100 {
		t.Errorf("expected consensus StartTime 100 with 2/4 votes, got %d", startTime)
	}

	// Now check if only 1 vote fails (1/4 is not > 1)
	n2.setObservation(target, NaraObservation{StartTime: 400}) // break consensus
	startTime = network.findStartingTimeFromNeighbourhoodForNara(target)
	if startTime != 0 {
		t.Errorf("expected no consensus (0) with 1/4 votes, got %d", startTime)
	}
}

func TestDissentAndChangeOfMind(t *testing.T) {
	ln := NewLocalNara("me", "me-soul", "host", "user", "pass", -1)
	network := ln.Network

	target := "target"
	network.importNara(NewNara(target))

	// Start with a consensus for StartTime 100
	n1 := NewNara("n1")
	n1.setObservation(target, NaraObservation{StartTime: 100})
	network.importNara(n1)

	n2 := NewNara("n2")
	n2.setObservation(target, NaraObservation{StartTime: 100})
	network.importNara(n2)

	// Verify consensus
	startTime := network.findStartingTimeFromNeighbourhoodForNara(target)
	if startTime != 100 {
		t.Errorf("expected initial consensus 100, got %d", startTime)
	}

	// Introduce a large number of dissenters with a different opinion (200)
	for i := 0; i < 10; i++ {
		dn := NewNara(fmt.Sprintf("d%d", i))
		dn.setObservation(target, NaraObservation{StartTime: 200})
		network.importNara(dn)
	}

	// Now 200 should be the new consensus
	startTime = network.findStartingTimeFromNeighbourhoodForNara(target)
	if startTime != 200 {
		t.Errorf("expected consensus to shift to 200, got %d", startTime)
	}

	// Now check what happens with complete chaos (no agreement > 25%)
	// Total voters is 13 (me + n1 + n2 + 10 dissenters). Threshold is 13/4 = 3.25 -> 3
	// We have ten '200's. Let's change 8 of them to unique values.
	// Result: two '200's, two '100's, and 8 unique values.
	// No value has > 3 votes.
	for i := 0; i < 8; i++ {
		dn := network.getNara(fmt.Sprintf("d%d", i))
		dn.setObservation(target, NaraObservation{StartTime: int64(1000 + i)})
		network.importNara(&dn)
	}

	startTime = network.findStartingTimeFromNeighbourhoodForNara(target)
	if startTime != 0 {
		t.Errorf("expected consensus to fail (0) in chaotic network, got %d", startTime)
	}
}

func TestObservations_OnlineTransitions(t *testing.T) {
	ln := NewLocalNara("me", "me-soul", "host", "user", "pass", -1)
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
