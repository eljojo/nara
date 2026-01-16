package nara

import (
	"testing"
	"time"

	"github.com/eljojo/nara/types"
)

func TestCalculateVibeConsistency(t *testing.T) {
	name := types.NaraName("test-nara")
	now := time.Now()
	vibe1 := calculateVibe(name, now)
	vibe2 := calculateVibe(name, now)

	if vibe1 != vibe2 {
		t.Errorf("calculateVibe should be consistent for same input and time, got %d and %d", vibe1, vibe2)
	}
}

func TestCalculateVibeDifferentNames(t *testing.T) {
	now := time.Now()
	vibe1 := calculateVibe(types.NaraName("nara-1"), now)
	vibe2 := calculateVibe(types.NaraName("nara-2"), now)

	if vibe1 == vibe2 {
		t.Errorf("calculateVibe should likely be different for different names, both got %d", vibe1)
	}
}

func TestCalculateVibeChangesOverTime(t *testing.T) {
	name := types.NaraName("test-nara")
	t1 := time.Date(2023, time.January, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2023, time.February, 1, 0, 0, 0, 0, time.UTC)

	vibe1 := calculateVibe(name, t1)
	vibe2 := calculateVibe(name, t2)

	if vibe1 == vibe2 {
		t.Errorf("vibe should change between January and February, got %d for both", vibe1)
	}
}

func TestCalculateVibeSameMonthDifferentDays(t *testing.T) {
	name := types.NaraName("test-nara")
	t1 := time.Date(2023, time.January, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2023, time.January, 15, 0, 0, 0, 0, time.UTC)

	vibe1 := calculateVibe(name, t1)
	vibe2 := calculateVibe(name, t2)

	if vibe1 != vibe2 {
		t.Errorf("vibe should be the same within the same month, got %d and %d", vibe1, vibe2)
	}
}

func TestCalculateVibeDifferentYears(t *testing.T) {
	name := types.NaraName("test-nara")
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
		Me:   NewNara(types.NaraName("me")),
		Soul: "me-soul",
	}
	network := &Network{
		local:         localNara,
		Neighbourhood: make(map[types.NaraName]*Nara),
	}

	neighborName := types.NaraName("neighbor")
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

	// Without coordinates, should fall back to vibe-based
	vibe := calculateVibe(neighborName, time.Now())
	expectedIndex := vibe % uint64(len(clusterNames))
	if observation.ClusterName != clusterNames[expectedIndex] {
		t.Errorf("Expected ClusterName %s, got %s", clusterNames[expectedIndex], observation.ClusterName)
	}
}

func TestNeighbourhoodMaintenance_IncludesMe(t *testing.T) {
	localNara := &LocalNara{
		Me:   NewNara(types.NaraName("me")),
		Soul: "me-soul",
	}
	network := &Network{
		local:         localNara,
		Neighbourhood: make(map[types.NaraName]*Nara),
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

// TestGridCellToClusterIndex verifies that grid cell hashing is consistent
func TestGridCellToClusterIndex(t *testing.T) {
	// Same cell should always produce same cluster
	idx1 := gridCellToClusterIndex(5, 10)
	idx2 := gridCellToClusterIndex(5, 10)
	if idx1 != idx2 {
		t.Errorf("same cell should produce same cluster, got %d and %d", idx1, idx2)
	}

	// Different cells should (usually) produce different clusters
	// This is probabilistic, so we just check the function doesn't crash
	idx3 := gridCellToClusterIndex(0, 0)
	idx4 := gridCellToClusterIndex(100, 100)
	if idx3 < 0 || idx3 >= len(clusterNames) {
		t.Errorf("cluster index out of range: %d", idx3)
	}
	if idx4 < 0 || idx4 >= len(clusterNames) {
		t.Errorf("cluster index out of range: %d", idx4)
	}
}

// TestGridBasedBarrios_SameCellSameCluster verifies that naras in the same grid cell
// get the same cluster - this is the key symmetric property
func TestGridBasedBarrios_SameCellSameCluster(t *testing.T) {
	ln := testLocalNara(t, "me")
	network := ln.Network

	// Give ourselves coordinates clearly in a cell
	// With default grid size of 50 and Floor, all coords in [50,100) map to cell (1,1)
	ln.Me.Status.Coordinates = NewNetworkCoordinate()
	ln.Me.Status.Coordinates.X = 60
	ln.Me.Status.Coordinates.Y = 60
	ln.Me.Status.Coordinates.Error = 0.1

	// Create two naras in the same grid cell (all within [50,100) range)
	alice := NewNara("alice")
	alice.Status.Coordinates = NewNetworkCoordinate()
	alice.Status.Coordinates.X = 70 // Same cell as me: Floor(70/50) = 1
	alice.Status.Coordinates.Y = 80 // Floor(80/50) = 1
	alice.Status.Coordinates.Error = 0.1
	network.Neighbourhood["alice"] = alice

	bob := NewNara("bob")
	bob.Status.Coordinates = NewNetworkCoordinate()
	bob.Status.Coordinates.X = 90 // Also same cell: Floor(90/50) = 1
	bob.Status.Coordinates.Y = 65 // Floor(65/50) = 1
	bob.Status.Coordinates.Error = 0.1
	network.Neighbourhood["bob"] = bob

	// Initialize observations
	ln.setMeObservation(NaraObservation{})
	ln.setObservation("alice", NaraObservation{})
	ln.setObservation("bob", NaraObservation{})

	// Run maintenance
	network.neighbourhoodMaintenance()

	// All three should be in the same cluster
	meObs := ln.getMeObservation()
	aliceObs := ln.getObservation("alice")
	bobObs := ln.getObservation("bob")

	if meObs.ClusterName == "" || aliceObs.ClusterName == "" || bobObs.ClusterName == "" {
		t.Fatal("cluster names should be set")
	}

	// All should have the same cluster since they're in the same grid cell
	if meObs.ClusterName != aliceObs.ClusterName {
		t.Errorf("me and alice should be in same cluster, got '%s' and '%s'",
			meObs.ClusterName, aliceObs.ClusterName)
	}
	if meObs.ClusterName != bobObs.ClusterName {
		t.Errorf("me and bob should be in same cluster, got '%s' and '%s'",
			meObs.ClusterName, bobObs.ClusterName)
	}

	// And same emoji
	if meObs.ClusterEmoji != aliceObs.ClusterEmoji || meObs.ClusterEmoji != bobObs.ClusterEmoji {
		t.Error("all three should have the same emoji")
	}
}

// TestGridBasedBarrios_DifferentCellsDifferentClusters verifies that naras in
// different grid cells get different clusters (usually - this is probabilistic)
func TestGridBasedBarrios_DifferentCellsDifferentClusters(t *testing.T) {
	ln := testLocalNara(t, "me")
	network := ln.Network

	// Me at origin
	ln.Me.Status.Coordinates = NewNetworkCoordinate()
	ln.Me.Status.Coordinates.X = 0
	ln.Me.Status.Coordinates.Y = 0
	ln.Me.Status.Coordinates.Error = 0.1

	// Far away nara - definitely in a different grid cell
	faraway := NewNara("faraway")
	faraway.Status.Coordinates = NewNetworkCoordinate()
	faraway.Status.Coordinates.X = 500 // Far from origin
	faraway.Status.Coordinates.Y = 500
	faraway.Status.Coordinates.Error = 0.1
	network.Neighbourhood["faraway"] = faraway

	ln.setMeObservation(NaraObservation{})
	ln.setObservation("faraway", NaraObservation{})

	network.neighbourhoodMaintenance()

	meObs := ln.getMeObservation()
	farawayObs := ln.getObservation("faraway")

	// They should have clusters set
	if meObs.ClusterName == "" || farawayObs.ClusterName == "" {
		t.Fatal("cluster names should be set")
	}

	// Note: we don't assert they're different because hash collisions are possible
	// The test just verifies the clustering works without crashing
	t.Logf("me cluster: %s, faraway cluster: %s", meObs.ClusterName, farawayObs.ClusterName)
}

// TestCalculateGridSize verifies auto-tuning based on RTT data
func TestCalculateGridSize(t *testing.T) {
	ln := testLocalNara(t, "me")
	network := ln.Network

	// No ping data - should return default
	gridSize := network.calculateGridSize()
	if gridSize != DefaultGridSize {
		t.Errorf("expected default grid size %f, got %f", DefaultGridSize, gridSize)
	}

	// Add some ping observations
	ln.SyncLedger.AddPingObservation("a", "b", 30.0)
	ln.SyncLedger.AddPingObservation("b", "c", 50.0)
	ln.SyncLedger.AddPingObservation("c", "d", 70.0)

	// Median of [30, 50, 70] is 50
	gridSize = network.calculateGridSize()
	if gridSize != 50.0 {
		t.Errorf("expected grid size 50.0 (median), got %f", gridSize)
	}
}

// TestCalculateGridSize_Clamping verifies grid size stays within bounds
func TestCalculateGridSize_Clamping(t *testing.T) {
	ln := testLocalNara(t, "me")
	network := ln.Network

	// Add very low RTT pings
	ln.SyncLedger.AddPingObservation("a", "b", 1.0)
	ln.SyncLedger.AddPingObservation("b", "c", 2.0)

	gridSize := network.calculateGridSize()
	if gridSize < MinGridSize {
		t.Errorf("grid size should be clamped to min %f, got %f", MinGridSize, gridSize)
	}

	// Clear and add very high RTT pings
	ln.SyncLedger = NewSyncLedger(1000)
	ln.SyncLedger.AddPingObservation("a", "b", 500.0)
	ln.SyncLedger.AddPingObservation("b", "c", 600.0)

	gridSize = network.calculateGridSize()
	if gridSize > MaxGridSize {
		t.Errorf("grid size should be clamped to max %f, got %f", MaxGridSize, gridSize)
	}
}

// TestIsInMyBarrio verifies the barrio membership check
func TestIsInMyBarrio(t *testing.T) {
	ln := testLocalNara(t, "me")
	network := ln.Network

	// Set coordinates for me
	ln.Me.Status.Coordinates = NewNetworkCoordinate()
	ln.Me.Status.Coordinates.X = 25
	ln.Me.Status.Coordinates.Y = 25
	ln.Me.Status.Coordinates.Error = 0.1

	// Nearby nara (same cell with default grid size)
	nearby := NewNara("nearby")
	nearby.Status.Coordinates = NewNetworkCoordinate()
	nearby.Status.Coordinates.X = 30
	nearby.Status.Coordinates.Y = 30
	nearby.Status.Coordinates.Error = 0.1
	network.Neighbourhood["nearby"] = nearby

	// Far nara (different cell)
	far := NewNara("far")
	far.Status.Coordinates = NewNetworkCoordinate()
	far.Status.Coordinates.X = 500
	far.Status.Coordinates.Y = 500
	far.Status.Coordinates.Error = 0.1
	network.Neighbourhood["far"] = far

	if !network.IsInMyBarrio("nearby") {
		t.Error("nearby should be in my barrio")
	}

	if network.IsInMyBarrio("far") {
		t.Error("far should NOT be in my barrio")
	}
}

// TestIsInMyBarrio_FallbackToVibe verifies fallback when no coordinates
func TestIsInMyBarrio_FallbackToVibe(t *testing.T) {
	ln := testLocalNara(t, "me")
	network := ln.Network

	// No coordinates for anyone
	ln.Me.Status.Coordinates = nil

	noCoords := NewNara("no-coords")
	noCoords.Status.Coordinates = nil
	network.Neighbourhood["no-coords"] = noCoords

	// Should not crash, falls back to vibe comparison
	result := network.IsInMyBarrio("no-coords")
	// Result depends on whether vibes match - just verify it doesn't crash
	t.Logf("IsInMyBarrio with no coords returned: %v", result)
}

// TestIsInMyBarrio_MixedRollout verifies behavior during rollout when some naras
// have coordinates (new code) and some don't (old code)
func TestIsInMyBarrio_MixedRollout(t *testing.T) {
	ln := testLocalNara(t, "me")
	network := ln.Network

	// I have coordinates (new nara)
	// With Floor and grid size 50, coords in [50,100) map to cell (1,1)
	ln.Me.Status.Coordinates = NewNetworkCoordinate()
	ln.Me.Status.Coordinates.X = 60
	ln.Me.Status.Coordinates.Y = 60
	ln.Me.Status.Coordinates.Error = 0.1

	// Old nara without coordinates
	oldNara := NewNara("old-nara")
	oldNara.Status.Coordinates = nil
	network.Neighbourhood["old-nara"] = oldNara

	// New nara with coordinates in same cell as me
	newNara := NewNara("new-nara")
	newNara.Status.Coordinates = NewNetworkCoordinate()
	newNara.Status.Coordinates.X = 75 // Same cell: Floor(75/50) = 1
	newNara.Status.Coordinates.Y = 85 // Floor(85/50) = 1
	newNara.Status.Coordinates.Error = 0.1
	network.Neighbourhood["new-nara"] = newNara

	// Old nara: should fall back to vibe comparison (not crash)
	oldResult := network.IsInMyBarrio("old-nara")
	t.Logf("IsInMyBarrio(old-nara) with mixed coords: %v (vibe fallback)", oldResult)

	// New nara: should use grid-based (same cell = same barrio)
	newResult := network.IsInMyBarrio("new-nara")
	if !newResult {
		t.Error("new-nara should be in my barrio (same grid cell)")
	}

	// Verify maintenance also handles mixed case
	ln.setMeObservation(NaraObservation{})
	ln.setObservation("old-nara", NaraObservation{})
	ln.setObservation("new-nara", NaraObservation{})

	network.neighbourhoodMaintenance()

	// All should get clusters assigned (grid-based or vibe fallback)
	meObs := ln.getMeObservation()
	oldObs := ln.getObservation("old-nara")
	newObs := ln.getObservation("new-nara")

	if meObs.ClusterName == "" || oldObs.ClusterName == "" || newObs.ClusterName == "" {
		t.Error("all naras should have clusters assigned during mixed rollout")
	}

	// Me and new-nara should have same cluster (both have coords, same cell)
	if meObs.ClusterName != newObs.ClusterName {
		t.Errorf("me and new-nara should be in same cluster, got '%s' and '%s'",
			meObs.ClusterName, newObs.ClusterName)
	}

	t.Logf("Mixed rollout clusters: me=%s, old-nara=%s (vibe), new-nara=%s",
		meObs.ClusterName, oldObs.ClusterName, newObs.ClusterName)
}

// TestBarriosFallback verifies fallback to hash-based when no coordinates
func TestBarriosFallback(t *testing.T) {
	ln := testLocalNara(t, "me")
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
