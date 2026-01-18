package nara

import (
	"math"
	"testing"

	"github.com/eljojo/nara/types"
)

func TestNetworkCoordinate_NewCoordinate(t *testing.T) {
	t.Parallel()
	// rand.Seed is no longer needed in Go 1.20+; global rand is automatically seeded
	coord := NewNetworkCoordinate()

	// Should start with small random position
	if math.Abs(coord.X) > 0.01 {
		t.Errorf("Initial X should be near origin, got %f", coord.X)
	}
	if math.Abs(coord.Y) > 0.01 {
		t.Errorf("Initial Y should be near origin, got %f", coord.Y)
	}

	// Should have minimum height
	if coord.Height < DefaultVivaldiConfig().MinHeight {
		t.Errorf("Initial height should be >= MinHeight, got %f", coord.Height)
	}

	// Should have initial error
	if coord.Error != DefaultVivaldiConfig().InitialError {
		t.Errorf("Initial error should be %f, got %f", DefaultVivaldiConfig().InitialError, coord.Error)
	}
}

func TestNetworkCoordinate_Distance(t *testing.T) {
	t.Parallel()
	config := DefaultVivaldiConfig()

	// Two coordinates at known positions
	c1 := &NetworkCoordinate{X: 0, Y: 0, Height: config.MinHeight, Error: 0.5}
	c2 := &NetworkCoordinate{X: 3, Y: 4, Height: config.MinHeight, Error: 0.5}

	// Distance should be Euclidean + heights
	// sqrt(3² + 4²) = 5, plus 2 * MinHeight
	expected := 5.0 + 2*config.MinHeight
	dist := c1.DistanceTo(c2)

	if math.Abs(dist-expected) > 0.0001 {
		t.Errorf("Distance should be %f, got %f", expected, dist)
	}

	// Distance should be symmetric
	dist2 := c2.DistanceTo(c1)
	if math.Abs(dist-dist2) > 0.0001 {
		t.Errorf("Distance should be symmetric: %f vs %f", dist, dist2)
	}
}

func TestNetworkCoordinate_DistanceWithHeight(t *testing.T) {
	t.Parallel()
	// Height component adds to distance (handles asymmetric latency)
	c1 := &NetworkCoordinate{X: 0, Y: 0, Height: 10, Error: 0.5}
	c2 := &NetworkCoordinate{X: 0, Y: 0, Height: 5, Error: 0.5}

	// Same position but different heights
	// Distance = 0 + 10 + 5 = 15
	dist := c1.DistanceTo(c2)
	if math.Abs(dist-15.0) > 0.0001 {
		t.Errorf("Distance with heights should be 15, got %f", dist)
	}
}

func TestNetworkCoordinate_Update_MovesTowardPeer(t *testing.T) {
	t.Parallel()
	config := DefaultVivaldiConfig()

	// Start at origin
	c1 := &NetworkCoordinate{X: 0, Y: 0, Height: config.MinHeight, Error: 1.0}
	// Peer at (10, 0)
	c2 := &NetworkCoordinate{X: 10, Y: 0, Height: config.MinHeight, Error: 1.0}

	// Predicted distance is ~10
	// If measured RTT is 20 (farther than predicted), we should move away
	// If measured RTT is 5 (closer than predicted), we should move toward
	initialX := c1.X

	// Measure RTT of 5 (closer than predicted 10)
	c1.Update(c2, 5, config)

	// Should have moved toward peer (positive X direction)
	if c1.X <= initialX {
		t.Errorf("Should move toward peer when RTT < predicted, X was %f now %f", initialX, c1.X)
	}
}

func TestNetworkCoordinate_Update_MovesAwayFromPeer(t *testing.T) {
	t.Parallel()
	config := DefaultVivaldiConfig()

	// Start at (5, 0)
	c1 := &NetworkCoordinate{X: 5, Y: 0, Height: config.MinHeight, Error: 1.0}
	// Peer at (10, 0)
	c2 := &NetworkCoordinate{X: 10, Y: 0, Height: config.MinHeight, Error: 1.0}

	// Predicted distance is ~5
	initialX := c1.X

	// Measure RTT of 20 (farther than predicted 5)
	c1.Update(c2, 20, config)

	// Should have moved away from peer (negative X direction)
	if c1.X >= initialX {
		t.Errorf("Should move away from peer when RTT > predicted, X was %f now %f", initialX, c1.X)
	}
}

func TestNetworkCoordinate_Update_ErrorDecreases(t *testing.T) {
	t.Parallel()
	config := DefaultVivaldiConfig()

	c1 := &NetworkCoordinate{X: 0, Y: 0, Height: config.MinHeight, Error: 1.0}
	c2 := &NetworkCoordinate{X: 10, Y: 0, Height: config.MinHeight, Error: 0.5}

	initialError := c1.Error

	// Update with accurate measurement
	predicted := c1.DistanceTo(c2)
	c1.Update(c2, predicted, config) // perfect measurement

	// Error should decrease (become more confident)
	if c1.Error >= initialError {
		t.Errorf("Error should decrease with accurate measurement, was %f now %f", initialError, c1.Error)
	}
}

func TestNetworkCoordinate_Update_RespectsMinHeight(t *testing.T) {
	t.Parallel()
	config := DefaultVivaldiConfig()

	c1 := &NetworkCoordinate{X: 0, Y: 0, Height: config.MinHeight, Error: 1.0}
	c2 := &NetworkCoordinate{X: 1, Y: 0, Height: config.MinHeight, Error: 1.0}

	// Even after many updates, height should never go below minimum
	for i := 0; i < 100; i++ {
		c1.Update(c2, 0.5, config)
	}

	if c1.Height < config.MinHeight {
		t.Errorf("Height should never go below MinHeight, got %f", c1.Height)
	}
}

func TestNetworkCoordinate_Clone(t *testing.T) {
	t.Parallel()
	original := &NetworkCoordinate{X: 1.5, Y: 2.5, Height: 0.1, Error: 0.3}
	clone := original.Clone()

	// Should have same values
	if clone.X != original.X || clone.Y != original.Y {
		t.Error("Clone should have same X, Y values")
	}
	if clone.Height != original.Height || clone.Error != original.Error {
		t.Error("Clone should have same Height, Error values")
	}

	// Modifying clone should not affect original
	clone.X = 100
	if original.X == 100 {
		t.Error("Modifying clone should not affect original")
	}
}

// NOTE: This test is flaky when run with other tests but passes in isolation.
// The convergence algorithm uses random jitter which can cause intermittent failures.
func TestVivaldi_Convergence(t *testing.T) {
	t.Parallel()
	// rand.Seed is no longer needed in Go 1.20+; global rand is automatically seeded
	// Simulate a 4-node network with known latencies
	// A -- 10ms -- B
	// |           |
	// 10ms       10ms
	// |           |
	// C -- 10ms -- D
	//
	// Diagonal A-D and B-C should be ~14ms (sqrt(2) * 10)

	config := DefaultVivaldiConfig()

	coords := map[string]*NetworkCoordinate{
		"A": NewNetworkCoordinate(),
		"B": NewNetworkCoordinate(),
		"C": NewNetworkCoordinate(),
		"D": NewNetworkCoordinate(),
	}

	// Simulated latencies (in ms)
	latencies := map[string]map[string]float64{
		"A": {"B": 10, "C": 10, "D": 14.14},
		"B": {"A": 10, "C": 14.14, "D": 10},
		"C": {"A": 10, "B": 14.14, "D": 10},
		"D": {"A": 14.14, "B": 10, "C": 10},
	}

	// Run many rounds of updates
	for round := 0; round < 100; round++ {
		for name, coord := range coords {
			for peerName, rtt := range latencies[name] {
				peer := coords[peerName]
				coord.Update(peer, rtt, config)
			}
		}
	}

	// Check that predicted distances roughly match actual latencies
	tolerance := 3.0 // Allow 3ms tolerance

	for name, coord := range coords {
		for peerName, actualRTT := range latencies[name] {
			peer := coords[peerName]
			predicted := coord.DistanceTo(peer)
			diff := math.Abs(predicted - actualRTT)

			if diff > tolerance {
				t.Errorf("%s->%s: predicted %f, actual %f, diff %f (> tolerance %f)",
					name, peerName, predicted, actualRTT, diff, tolerance)
			}
		}
	}
}

func TestVivaldi_Stability(t *testing.T) {
	t.Parallel()
	// After convergence, coordinates should remain stable with consistent measurements
	config := DefaultVivaldiConfig()

	c1 := &NetworkCoordinate{X: 0, Y: 0, Height: config.MinHeight, Error: 0.1}
	c2 := &NetworkCoordinate{X: 10, Y: 0, Height: config.MinHeight, Error: 0.1}

	// Let them stabilize
	for i := 0; i < 50; i++ {
		c1.Update(c2, 10, config) // consistent 10ms measurement
	}

	// Record position after stabilization
	stabilizedX := c1.X
	stabilizedY := c1.Y

	// Continue with same measurements
	for i := 0; i < 50; i++ {
		c1.Update(c2, 10, config)
	}

	// Position should not have moved much
	drift := math.Sqrt(math.Pow(c1.X-stabilizedX, 2) + math.Pow(c1.Y-stabilizedY, 2))
	if drift > 0.5 {
		t.Errorf("Stable coordinates should not drift much, drifted %f", drift)
	}
}

func TestVivaldi_WeightsByError(t *testing.T) {
	t.Parallel()
	config := DefaultVivaldiConfig()

	// Node with high error (uncertain)
	uncertain := &NetworkCoordinate{X: 0, Y: 0, Height: config.MinHeight, Error: 1.0}
	// Node with low error (confident)
	confident := &NetworkCoordinate{X: 10, Y: 0, Height: config.MinHeight, Error: 0.1}

	uncertainInitialX := uncertain.X
	confidentInitialX := confident.X

	// Both measure 5ms to each other (should pull them together)
	uncertain.Update(confident, 5, config)
	confident.Update(uncertain, 5, config)

	// Uncertain node should move MORE than confident node
	uncertainMove := math.Abs(uncertain.X - uncertainInitialX)
	confidentMove := math.Abs(confident.X - confidentInitialX)

	if uncertainMove <= confidentMove {
		t.Errorf("Uncertain node should move more: uncertain moved %f, confident moved %f",
			uncertainMove, confidentMove)
	}
}

func TestDefaultVivaldiConfig(t *testing.T) {
	t.Parallel()
	config := DefaultVivaldiConfig()

	// Verify sensible defaults
	if config.Ce <= 0 || config.Ce > 1 {
		t.Errorf("Ce should be in (0, 1], got %f", config.Ce)
	}
	if config.Cc <= 0 || config.Cc > 1 {
		t.Errorf("Cc should be in (0, 1], got %f", config.Cc)
	}
	if config.MinHeight <= 0 {
		t.Errorf("MinHeight should be > 0, got %f", config.MinHeight)
	}
	if config.InitialError <= 0 {
		t.Errorf("InitialError should be > 0, got %f", config.InitialError)
	}
}

func TestApplyProximityToClout(t *testing.T) {
	t.Parallel()
	// Test that proximity weighting works correctly
	myCoords := &NetworkCoordinate{X: 0, Y: 0, Height: 0.01, Error: 0.1}

	baseClout := map[types.NaraName]float64{
		types.NaraName("near"):    5.0,
		types.NaraName("far"):     5.0,
		types.NaraName("nocoord"): 5.0,
	}

	getCoords := func(name types.NaraName) *NetworkCoordinate {
		switch name {
		case "near":
			return &NetworkCoordinate{X: 10, Y: 0, Height: 0.01, Error: 0.1} // ~10ms away
		case "far":
			return &NetworkCoordinate{X: 200, Y: 0, Height: 0.01, Error: 0.1} // ~200ms away
		case "nocoord":
			return nil // No coordinates
		default:
			return nil
		}
	}

	result := ApplyProximityToClout(baseClout, myCoords, getCoords)

	// Near peer should have higher final clout than far peer
	if result[types.NaraName("near")] <= result[types.NaraName("far")] {
		t.Errorf("Near peer should have higher clout than far: near=%f, far=%f",
			result[types.NaraName("near")], result[types.NaraName("far")])
	}

	// Peer without coordinates should keep original clout
	if result[types.NaraName("nocoord")] != baseClout[types.NaraName("nocoord")] {
		t.Errorf("Peer without coords should keep original clout: got %f, want %f",
			result[types.NaraName("nocoord")], baseClout[types.NaraName("nocoord")])
	}
}

func TestApplyProximityToClout_NilCoords(t *testing.T) {
	t.Parallel()
	baseClout := map[types.NaraName]float64{types.NaraName("peer"): 5.0}

	// Should return base clout when myCoords is nil
	result := ApplyProximityToClout(baseClout, nil, func(types.NaraName) *NetworkCoordinate {
		return &NetworkCoordinate{X: 0, Y: 0, Height: 0.01, Error: 0.1}
	})

	if result[types.NaraName("peer")] != baseClout[types.NaraName("peer")] {
		t.Errorf("Should return base clout when myCoords is nil")
	}
}

func TestProximityBonus(t *testing.T) {
	t.Parallel()
	myCoords := &NetworkCoordinate{X: 0, Y: 0, Height: 0, Error: 0.1}

	tests := []struct {
		name     string
		peerX    float64
		expected float64 // approximate
	}{
		{"very close (1ms)", 1, 100.0},   // 100 / 1 = 100
		{"moderate (10ms)", 10, 10.0},    // 100 / 10 = 10
		{"far (100ms)", 100, 1.0},        // 100 / 100 = 1
		{"very far (1000ms)", 1000, 0.1}, // 100 / 1000 = 0.1
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			peerCoords := &NetworkCoordinate{X: tt.peerX, Y: 0, Height: 0, Error: 0.1}
			bonus := ProximityBonus(myCoords, peerCoords)

			// Allow 10% tolerance due to height component
			tolerance := tt.expected * 0.1
			if bonus < tt.expected-tolerance || bonus > tt.expected+tolerance {
				t.Errorf("ProximityBonus = %f, want ~%f", bonus, tt.expected)
			}
		})
	}
}

func TestProximityBonus_NilCoords(t *testing.T) {
	t.Parallel()
	myCoords := &NetworkCoordinate{X: 0, Y: 0, Height: 0, Error: 0.1}

	// Should return 0 when either coord is nil
	if bonus := ProximityBonus(nil, myCoords); bonus != 0 {
		t.Errorf("Expected 0 when myCoords is nil, got %f", bonus)
	}
	if bonus := ProximityBonus(myCoords, nil); bonus != 0 {
		t.Errorf("Expected 0 when peerCoords is nil, got %f", bonus)
	}
}

func TestNetworkCoordinate_IsValid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		coord *NetworkCoordinate
		valid bool
	}{
		{"nil", nil, false},
		{"valid", &NetworkCoordinate{X: 1, Y: 2, Height: 0.01, Error: 0.5}, true},
		{"NaN X", &NetworkCoordinate{X: math.NaN(), Y: 2, Height: 0.01, Error: 0.5}, false},
		{"NaN Y", &NetworkCoordinate{X: 1, Y: math.NaN(), Height: 0.01, Error: 0.5}, false},
		{"Inf X", &NetworkCoordinate{X: math.Inf(1), Y: 2, Height: 0.01, Error: 0.5}, false},
		{"Inf Height", &NetworkCoordinate{X: 1, Y: 2, Height: math.Inf(-1), Error: 0.5}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.coord.IsValid(); got != tt.valid {
				t.Errorf("IsValid() = %v, want %v", got, tt.valid)
			}
		})
	}
}

func TestVivaldi_HighErrorNodeMovesMore(t *testing.T) {
	t.Parallel()
	config := DefaultVivaldiConfig()

	// Two nodes at same position but different confidence
	highError := &NetworkCoordinate{X: 0, Y: 0, Height: config.MinHeight, Error: 1.0}
	lowError := &NetworkCoordinate{X: 0, Y: 0, Height: config.MinHeight, Error: 0.1}

	// Peer at known position
	peer := &NetworkCoordinate{X: 10, Y: 0, Height: config.MinHeight, Error: 0.5}

	// Record initial positions
	highErrorInitialX := highError.X
	lowErrorInitialX := lowError.X

	// Same RTT measurement for both
	highError.Update(peer, 5, config)
	lowError.Update(peer, 5, config)

	// High error node should move more
	highErrorMove := math.Abs(highError.X - highErrorInitialX)
	lowErrorMove := math.Abs(lowError.X - lowErrorInitialX)

	if highErrorMove <= lowErrorMove {
		t.Errorf("High error node should move more: highError moved %f, lowError moved %f",
			highErrorMove, lowErrorMove)
	}
}
