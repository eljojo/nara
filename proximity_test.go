package nara

import (
	"testing"
)

func TestGetProximityGroup_RanksbyDistance(t *testing.T) {
	ln := NewLocalNara("me", "test-soul", "host", "user", "pass", -1)
	network := ln.Network

	// Give ourselves coordinates
	ln.Me.Status.Coordinates = NewNetworkCoordinate()
	ln.Me.Status.Coordinates.X = 0
	ln.Me.Status.Coordinates.Y = 0
	ln.Me.Status.Coordinates.Error = 0.1

	// Add neighbors at different distances
	close := NewNara("close")
	close.Status.Coordinates = NewNetworkCoordinate()
	close.Status.Coordinates.X = 1 // Close
	close.Status.Coordinates.Y = 0
	close.Status.Coordinates.Error = 0.1
	network.Neighbourhood["close"] = close

	medium := NewNara("medium")
	medium.Status.Coordinates = NewNetworkCoordinate()
	medium.Status.Coordinates.X = 5 // Medium distance
	medium.Status.Coordinates.Y = 0
	medium.Status.Coordinates.Error = 0.1
	network.Neighbourhood["medium"] = medium

	far := NewNara("far")
	far.Status.Coordinates = NewNetworkCoordinate()
	far.Status.Coordinates.X = 20 // Far
	far.Status.Coordinates.Y = 0
	far.Status.Coordinates.Error = 0.1
	network.Neighbourhood["far"] = far

	// Get proximity group
	group := network.GetProximityGroup(3)

	if len(group) != 3 {
		t.Fatalf("expected 3 neighbors, got %d", len(group))
	}

	// Should be ordered by distance
	if group[0].Name != "close" {
		t.Errorf("expected closest to be 'close', got '%s'", group[0].Name)
	}
	if group[1].Name != "medium" {
		t.Errorf("expected second to be 'medium', got '%s'", group[1].Name)
	}
	if group[2].Name != "far" {
		t.Errorf("expected third to be 'far', got '%s'", group[2].Name)
	}

	// Distances should increase
	if group[0].EstimatedRTT >= group[1].EstimatedRTT {
		t.Error("expected close to have lower RTT than medium")
	}
	if group[1].EstimatedRTT >= group[2].EstimatedRTT {
		t.Error("expected medium to have lower RTT than far")
	}
}

func TestGetProximityGroup_HandlesNoCoordinates(t *testing.T) {
	ln := NewLocalNara("me", "test-soul", "host", "user", "pass", -1)
	network := ln.Network

	// Give ourselves coordinates
	ln.Me.Status.Coordinates = NewNetworkCoordinate()

	// Add neighbor without coordinates
	noCoords := NewNara("no-coords")
	noCoords.Status.Coordinates = nil
	network.Neighbourhood["no-coords"] = noCoords

	// Add neighbor with coordinates
	hasCoords := NewNara("has-coords")
	hasCoords.Status.Coordinates = NewNetworkCoordinate()
	hasCoords.Status.Coordinates.X = 5
	network.Neighbourhood["has-coords"] = hasCoords

	group := network.GetProximityGroup(2)

	if len(group) != 2 {
		t.Fatalf("expected 2 neighbors, got %d", len(group))
	}

	// The one with coordinates should be first (lower RTT)
	if group[0].Name != "has-coords" {
		t.Errorf("expected 'has-coords' to be first, got '%s'", group[0].Name)
	}
	if group[0].HasCoordinates != true {
		t.Error("expected 'has-coords' to have HasCoordinates=true")
	}

	// The one without should be last
	if group[1].Name != "no-coords" {
		t.Errorf("expected 'no-coords' to be second, got '%s'", group[1].Name)
	}
	if group[1].HasCoordinates != false {
		t.Error("expected 'no-coords' to have HasCoordinates=false")
	}
}

func TestIsInMyProximityGroup(t *testing.T) {
	ln := NewLocalNara("me", "test-soul", "host", "user", "pass", -1)
	network := ln.Network

	ln.Me.Status.Coordinates = NewNetworkCoordinate()

	// Add 3 neighbors at increasing distances
	for i, name := range []string{"alice", "bob", "carol"} {
		n := NewNara(name)
		n.Status.Coordinates = NewNetworkCoordinate()
		n.Status.Coordinates.X = float64((i + 1) * 10)
		network.Neighbourhood[name] = n
	}

	// With group size 2, only alice and bob should be in group
	if !network.IsInMyProximityGroup("alice", 2) {
		t.Error("expected alice to be in proximity group of 2")
	}
	if !network.IsInMyProximityGroup("bob", 2) {
		t.Error("expected bob to be in proximity group of 2")
	}
	if network.IsInMyProximityGroup("carol", 2) {
		t.Error("expected carol NOT to be in proximity group of 2")
	}

	// With group size 3, all should be in
	if !network.IsInMyProximityGroup("carol", 3) {
		t.Error("expected carol to be in proximity group of 3")
	}
}

func TestEstimateRTTTo(t *testing.T) {
	ln := NewLocalNara("me", "test-soul", "host", "user", "pass", -1)
	network := ln.Network

	ln.Me.Status.Coordinates = NewNetworkCoordinate()
	ln.Me.Status.Coordinates.X = 0
	ln.Me.Status.Coordinates.Y = 0

	other := NewNara("other")
	other.Status.Coordinates = NewNetworkCoordinate()
	other.Status.Coordinates.X = 10
	other.Status.Coordinates.Y = 0
	network.Neighbourhood["other"] = other

	rtt := network.EstimateRTTTo("other")
	if rtt < 0 {
		t.Error("expected valid RTT estimate")
	}

	// Should be roughly 10 (plus heights)
	if rtt < 10 || rtt > 12 {
		t.Errorf("expected RTT around 10, got %.2f", rtt)
	}

	// Non-existent nara should return -1
	if network.EstimateRTTTo("nobody") != -1 {
		t.Error("expected -1 for non-existent nara")
	}
}
