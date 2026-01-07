package nara

import (
	"testing"
	"time"
)

func TestCalculateVibeConsistency(t *testing.T) {
	name := "test-nara"
	now := time.Now()
	vibe1 := calculateVibe(name, now)
	vibe2 := calculateVibe(name, now)

	if vibe1 != vibe2 {
		t.Errorf("calculateVibe should be consistent for same input and time, got %d and %d", vibe1, vibe2)
	}
}

func TestCalculateVibeDifferentNames(t *testing.T) {
	now := time.Now()
	vibe1 := calculateVibe("nara-1", now)
	vibe2 := calculateVibe("nara-2", now)

	if vibe1 == vibe2 {
		t.Errorf("calculateVibe should likely be different for different names, both got %d", vibe1)
	}
}

func TestCalculateVibeChangesOverTime(t *testing.T) {
	name := "test-nara"
	t1 := time.Date(2023, time.January, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2023, time.February, 1, 0, 0, 0, 0, time.UTC)

	vibe1 := calculateVibe(name, t1)
	vibe2 := calculateVibe(name, t2)

	if vibe1 == vibe2 {
		t.Errorf("vibe should change between January and February, got %d for both", vibe1)
	}
}

func TestCalculateVibeSameMonthDifferentDays(t *testing.T) {
	name := "test-nara"
	t1 := time.Date(2023, time.January, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2023, time.January, 15, 0, 0, 0, 0, time.UTC)

	vibe1 := calculateVibe(name, t1)
	vibe2 := calculateVibe(name, t2)

	if vibe1 != vibe2 {
		t.Errorf("vibe should be the same within the same month, got %d and %d", vibe1, vibe2)
	}
}

func TestCalculateVibeDifferentYears(t *testing.T) {
	name := "test-nara"
	t1 := time.Date(2023, time.January, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)

	vibe1 := calculateVibe(name, t1)
	vibe2 := calculateVibe(name, t2)

	if vibe1 == vibe2 {
		t.Errorf("vibe should change between different years, got %d for both", vibe1)
	}
}

func TestNeighbourhoodMaintenance(t *testing.T) {
	localNara := &LocalNara{
		Me:   NewNara("me"),
		Soul: "me-soul",
	}
	network := &Network{
		local:         localNara,
		Neighbourhood: make(map[string]*Nara),
	}

	neighborName := "neighbor"
	network.Neighbourhood[neighborName] = NewNara(neighborName)

	// Ensure the neighbor has an observation
	network.local.setObservation(neighborName, NaraObservation{})

	network.neighbourhoodMaintenance()

	observation := network.local.getObservation(neighborName)

	if observation.ClusterName == "" {
		t.Error("ClusterName should be set after neighbourhoodMaintenance")
	}

	if observation.ClusterEmoji == "" {
		t.Error("ClusterEmoji should be set after neighbourhoodMaintenance")
	}

	vibe := calculateVibe(neighborName, time.Now())
	expectedIndex := vibe % uint64(len(clusterNames))
	if observation.ClusterName != clusterNames[expectedIndex] {
		t.Errorf("Expected ClusterName %s, got %s", clusterNames[expectedIndex], observation.ClusterName)
	}
}

func TestNeighbourhoodMaintenance_IncludesMe(t *testing.T) {
	localNara := &LocalNara{
		Me:   NewNara("me"),
		Soul: "me-soul",
	}
	network := &Network{
		local:         localNara,
		Neighbourhood: make(map[string]*Nara),
	}

	// Ensure "me" has an observation
	network.local.setMeObservation(NaraObservation{})

	network.neighbourhoodMaintenance()

	observation := network.local.getMeObservation()
	if observation.ClusterName == "" {
		t.Error("ClusterName for 'me' should be set after neighbourhoodMaintenance")
	}
	if observation.ClusterEmoji == "" {
		t.Error("ClusterEmoji for 'me' should be set after neighbourhoodMaintenance")
	}
}

// TestProximityBasedBarrios verifies that naras with the same closest neighbor
// end up in the same cluster - this is the key property of proximity-based clustering
func TestProximityBasedBarrios(t *testing.T) {
	ln := NewLocalNara("me", "test-soul", "host", "user", "pass", -1)
	network := ln.Network

	// Give ourselves coordinates at origin
	ln.Me.Status.Coordinates = NewNetworkCoordinate()
	ln.Me.Status.Coordinates.X = 0
	ln.Me.Status.Coordinates.Y = 0
	ln.Me.Status.Coordinates.Error = 0.1

	// Create a "hub" nara that will be closest to several others
	hub := NewNara("hub")
	hub.Status.Coordinates = NewNetworkCoordinate()
	hub.Status.Coordinates.X = 10
	hub.Status.Coordinates.Y = 0
	hub.Status.Coordinates.Error = 0.1
	network.Neighbourhood["hub"] = hub

	// Create two naras that are both closest to "hub"
	alice := NewNara("alice")
	alice.Status.Coordinates = NewNetworkCoordinate()
	alice.Status.Coordinates.X = 11 // Very close to hub
	alice.Status.Coordinates.Y = 1
	alice.Status.Coordinates.Error = 0.1
	network.Neighbourhood["alice"] = alice

	bob := NewNara("bob")
	bob.Status.Coordinates = NewNetworkCoordinate()
	bob.Status.Coordinates.X = 9 // Also very close to hub
	bob.Status.Coordinates.Y = -1
	bob.Status.Coordinates.Error = 0.1
	network.Neighbourhood["bob"] = bob

	// Initialize observations
	ln.setObservation("hub", NaraObservation{})
	ln.setObservation("alice", NaraObservation{})
	ln.setObservation("bob", NaraObservation{})

	// Run maintenance
	network.neighbourhoodMaintenance()

	// Alice and Bob should both have "hub" as their closest neighbor,
	// so they should be in the same cluster
	aliceObs := ln.getObservation("alice")
	bobObs := ln.getObservation("bob")

	if aliceObs.ClusterName == "" || bobObs.ClusterName == "" {
		t.Fatal("cluster names should be set")
	}

	// Both should be in the cluster named after "hub"
	expectedCluster := clusterNames[nameToClusterIndex("hub")]
	if aliceObs.ClusterName != expectedCluster {
		t.Errorf("expected alice to be in cluster '%s' (hub's cluster), got '%s'", expectedCluster, aliceObs.ClusterName)
	}
	if bobObs.ClusterName != expectedCluster {
		t.Errorf("expected bob to be in cluster '%s' (hub's cluster), got '%s'", expectedCluster, bobObs.ClusterName)
	}

	// They should have the same emoji too
	if aliceObs.ClusterEmoji != bobObs.ClusterEmoji {
		t.Errorf("alice and bob should have same emoji since they're both closest to hub, got %s and %s",
			aliceObs.ClusterEmoji, bobObs.ClusterEmoji)
	}
}

// TestProximityBarriosFallback verifies fallback to hash-based when no coordinates
func TestProximityBarriosFallback(t *testing.T) {
	ln := NewLocalNara("me", "test-soul", "host", "user", "pass", -1)
	network := ln.Network

	// No coordinates for anyone
	ln.Me.Status.Coordinates = nil

	noCoords := NewNara("no-coords")
	noCoords.Status.Coordinates = nil
	network.Neighbourhood["no-coords"] = noCoords
	ln.setObservation("no-coords", NaraObservation{})

	network.neighbourhoodMaintenance()

	obs := ln.getObservation("no-coords")

	// Should still get a cluster (via fallback)
	if obs.ClusterName == "" {
		t.Error("should fall back to hash-based cluster when no coordinates")
	}

	// Should match the hash-based calculation
	vibe := calculateVibe("no-coords", time.Now())
	expectedIdx := int(vibe % uint64(len(clusterNames)))
	if obs.ClusterName != clusterNames[expectedIdx] {
		t.Errorf("expected fallback cluster '%s', got '%s'", clusterNames[expectedIdx], obs.ClusterName)
	}
}
