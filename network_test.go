package nara

import (
	"testing"
)

func TestNetwork_ImportNara(t *testing.T) {
	ln := NewLocalNara("me", "host", "user", "pass", -1)
	network := ln.Network

	other := NewNara("other")
	other.Status.Flair = "🌈"

	network.importNara(other)

	if len(network.Neighbourhood) != 1 {
		t.Errorf("expected 1 nara in neighbourhood, got %d", len(network.Neighbourhood))
	}

	imported := network.getNara("other")
	if imported.Name != "other" {
		t.Errorf("expected imported nara name to be 'other', got %s", imported.Name)
	}
	if imported.Status.Flair != "🌈" {
		t.Errorf("expected imported nara flair to be '🌈', got %s", imported.Status.Flair)
	}
}

func TestNetwork_NaraOrdering(t *testing.T) {
	ln := NewLocalNara("me", "host", "user", "pass", -1)
	network := ln.Network

	// Set me observation
	obsMe := network.local.getMeObservation()
	obsMe.StartTime = 1000
	obsMe.Online = "ONLINE"
	network.local.setMeObservation(obsMe)

	// Add an older nara
	older := NewNara("older")
	network.importNara(older)
	obsOlder := NaraObservation{StartTime: 500, Online: "ONLINE"}
	network.local.setObservation("older", obsOlder)

	// Add a younger nara
	younger := NewNara("younger")
	network.importNara(younger)
	obsYounger := NaraObservation{StartTime: 1500, Online: "ONLINE"}
	network.local.setObservation("younger", obsYounger)

	oldest := network.oldestNara()
	if oldest.Name != "older" {
		t.Errorf("expected oldest nara to be 'older', got %s", oldest.Name)
	}

	youngest := network.youngestNara()
	if youngest.Name != "younger" {
		t.Errorf("expected youngest nara to be 'younger', got %s", youngest.Name)
	}
}

func TestNetwork_NeighbourhoodNames(t *testing.T) {
	ln := NewLocalNara("me", "host", "user", "pass", -1)
	network := ln.Network

	network.importNara(NewNara("a"))
	network.importNara(NewNara("b"))

	names := network.NeighbourhoodNames()
	if len(names) != 2 {
		t.Errorf("expected 2 names, got %d", len(names))
	}

	foundA := false
	foundB := false
	for _, n := range names {
		if n == "a" {
			foundA = true
		}
		if n == "b" {
			foundB = true
		}
	}

	if !foundA || !foundB {
		t.Errorf("did not find all expected names: foundA=%v, foundB=%v", foundA, foundB)
	}
}
