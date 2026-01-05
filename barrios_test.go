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
		Me: NewNara("me"),
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
