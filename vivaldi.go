package nara

import (
	"math"
	"math/rand"
)

// NetworkCoordinate represents a node's position in network space
// Uses Vivaldi algorithm: 2D Euclidean + height for non-Euclidean reality
type NetworkCoordinate struct {
	X      float64 `json:"x"`      // X coordinate in virtual space
	Y      float64 `json:"y"`      // Y coordinate in virtual space
	Height float64 `json:"height"` // Height component (handles triangle inequality violations)
	Error  float64 `json:"error"`  // Estimated error (confidence in position, lower = more confident)
}

// VivaldiConfig holds tuning parameters for coordinate updates
type VivaldiConfig struct {
	Ce           float64 // Error update constant (typical: 0.25)
	Cc           float64 // Coordinate update constant (typical: 0.25)
	MinHeight    float64 // Minimum height value (typical: 1e-5)
	InitialError float64 // Starting error for new nodes (typical: 1.0)
}

// DefaultVivaldiConfig returns sensible defaults for the Nara network
func DefaultVivaldiConfig() VivaldiConfig {
	return VivaldiConfig{
		Ce:           0.25,
		Cc:           0.25,
		MinHeight:    1e-5,
		InitialError: 1.0,
	}
}

// NewNetworkCoordinate creates coordinates at a small random offset from origin
// with high initial error (low confidence)
func NewNetworkCoordinate() *NetworkCoordinate {
	config := DefaultVivaldiConfig()

	// Start at random position in small circle to prevent all nodes
	// starting at exact same point (which would cause division issues)
	angle := rand.Float64() * 2 * math.Pi
	radius := rand.Float64() * 0.001 // Small jitter

	return &NetworkCoordinate{
		X:      radius * math.Cos(angle),
		Y:      radius * math.Sin(angle),
		Height: config.MinHeight,
		Error:  config.InitialError,
	}
}

// DistanceTo calculates the expected RTT (latency) between two coordinates
// Distance = Euclidean distance + sum of heights
func (c *NetworkCoordinate) DistanceTo(other *NetworkCoordinate) float64 {
	if other == nil {
		return math.MaxFloat64
	}

	dx := c.X - other.X
	dy := c.Y - other.Y
	euclidean := math.Sqrt(dx*dx + dy*dy)

	// Height adds to distance (absorbs non-Euclidean network effects)
	return euclidean + c.Height + other.Height
}

// Update adjusts coordinates based on measured RTT to a peer
// Returns the new error estimate
func (c *NetworkCoordinate) Update(peer *NetworkCoordinate, rtt float64, config VivaldiConfig) float64 {
	if peer == nil || rtt <= 0 {
		return c.Error
	}

	// 1. Calculate predicted distance
	predicted := c.DistanceTo(peer)

	// 2. Calculate error (how wrong we were)
	err := rtt - predicted

	// 3. Calculate relative weight (who do we trust more?)
	// Higher error = less confident = willing to move more
	totalError := c.Error + peer.Error
	if totalError == 0 {
		totalError = 0.001 // Prevent division by zero
	}
	w := c.Error / totalError

	// 4. Update error estimate
	// Blend between current error and absolute error of this measurement
	absErr := math.Abs(err)
	c.Error = c.Error*config.Ce*w + absErr*(1-config.Ce*w)

	// Clamp error to reasonable bounds
	if c.Error < 0.001 {
		c.Error = 0.001
	}
	if c.Error > 2.0 {
		c.Error = 2.0
	}

	// 5. Calculate unit vector from peer to us (for Vivaldi update direction)
	dx := c.X - peer.X
	dy := c.Y - peer.Y
	dist := math.Sqrt(dx*dx + dy*dy)

	var unitX, unitY float64
	if dist > 0.0001 {
		unitX = dx / dist
		unitY = dy / dist
	} else {
		// If too close, use random direction
		angle := rand.Float64() * 2 * math.Pi
		unitX = math.Cos(angle)
		unitY = math.Sin(angle)
	}

	// 6. Move coordinates
	// Positive error (measured > predicted) means we're farther than predicted
	// So we should move AWAY from the peer (in direction of unit vector)
	// Negative error means we're closer, so move TOWARD peer (against unit vector)
	delta := config.Cc * w * err
	c.X += unitX * delta
	c.Y += unitY * delta

	// 7. Update height
	// Height absorbs the non-Euclidean component
	c.Height += config.Cc * w * err * 0.1 // Smaller adjustment for height
	if c.Height < config.MinHeight {
		c.Height = config.MinHeight
	}

	return c.Error
}

// Clone returns a deep copy of the coordinate
func (c *NetworkCoordinate) Clone() NetworkCoordinate {
	return NetworkCoordinate{
		X:      c.X,
		Y:      c.Y,
		Height: c.Height,
		Error:  c.Error,
	}
}

// IsValid returns true if the coordinate has reasonable values
func (c *NetworkCoordinate) IsValid() bool {
	if c == nil {
		return false
	}
	if math.IsNaN(c.X) || math.IsNaN(c.Y) || math.IsNaN(c.Height) || math.IsNaN(c.Error) {
		return false
	}
	if math.IsInf(c.X, 0) || math.IsInf(c.Y, 0) || math.IsInf(c.Height, 0) || math.IsInf(c.Error, 0) {
		return false
	}
	return true
}

// ApplyProximityToClout adjusts clout scores based on network distance
// Nearby naras have stronger influence on opinions
func ApplyProximityToClout(baseClout map[string]float64, myCoords *NetworkCoordinate, getCoords func(string) *NetworkCoordinate) map[string]float64 {
	if myCoords == nil || getCoords == nil {
		return baseClout
	}

	result := make(map[string]float64)
	for name, clout := range baseClout {
		result[name] = clout

		peerCoords := getCoords(name)
		if peerCoords == nil || !peerCoords.IsValid() {
			continue
		}

		distance := myCoords.DistanceTo(peerCoords)

		// Proximity modifier: nearby peers have more influence
		// Uses exponential decay: e^(-distance/scale)
		// Scale of 100ms means 100ms distance = ~37% weight
		proximityWeight := math.Exp(-distance / 100.0)

		// Blend original clout with proximity modifier
		// 70% clout, 30% proximity influence
		result[name] = clout*0.7 + clout*proximityWeight*0.3
	}

	return result
}

// ProximityBonus calculates the routing bonus for a candidate based on network distance
// Used in ChooseNextNara to prefer nearby nodes
func ProximityBonus(myCoords *NetworkCoordinate, peerCoords *NetworkCoordinate) float64 {
	if myCoords == nil || peerCoords == nil {
		return 0
	}

	distance := myCoords.DistanceTo(peerCoords)

	// Bonus = 100 / max(1, distance)
	// Close peers (1ms) get bonus of 100
	// Far peers (100ms) get bonus of 1
	// Very far peers (1000ms) get bonus of 0.1
	if distance < 1 {
		distance = 1
	}
	return 100.0 / distance
}
