package nara

import (
	"sort"
)

// ProximityNeighbor represents a nara and their estimated distance
type ProximityNeighbor struct {
	Name            string  // Nara name
	EstimatedRTT    float64 // Estimated RTT in ms from Vivaldi coordinates
	HasCoordinates  bool    // Whether they have valid coordinates
}

// ProximityGroupSize is the default number of "close friends" each nara identifies
const ProximityGroupSize = 5

// GetProximityGroup returns the N closest naras by estimated Vivaldi distance.
// Each nara computes this independently, but because coordinates converge,
// they'll naturally agree on who's close to whom - without explicit coordination.
func (network *Network) GetProximityGroup(n int) []ProximityNeighbor {
	network.local.mu.Lock()
	myCoords := network.local.Me.Status.Coordinates
	network.local.mu.Unlock()

	if myCoords == nil || !myCoords.IsValid() {
		return nil
	}

	var neighbors []ProximityNeighbor

	// Calculate estimated distance to all known naras
	for name, nara := range network.Neighbourhood {
		nara.mu.Lock()
		coords := nara.Status.Coordinates
		nara.mu.Unlock()

		neighbor := ProximityNeighbor{
			Name:           name,
			HasCoordinates: coords != nil && coords.IsValid(),
		}

		if neighbor.HasCoordinates {
			neighbor.EstimatedRTT = myCoords.DistanceTo(coords)
		} else {
			// No coordinates = infinite distance (unknown)
			neighbor.EstimatedRTT = 999999
		}

		neighbors = append(neighbors, neighbor)
	}

	// Sort by estimated RTT (closest first)
	sort.Slice(neighbors, func(i, j int) bool {
		return neighbors[i].EstimatedRTT < neighbors[j].EstimatedRTT
	})

	// Return top N
	if n > len(neighbors) {
		n = len(neighbors)
	}

	return neighbors[:n]
}

// GetAllProximities returns all naras ranked by estimated distance
func (network *Network) GetAllProximities() []ProximityNeighbor {
	return network.GetProximityGroup(len(network.Neighbourhood))
}

// IsInMyProximityGroup checks if a nara is in our closest N friends
func (network *Network) IsInMyProximityGroup(name string, groupSize int) bool {
	group := network.GetProximityGroup(groupSize)
	for _, n := range group {
		if n.Name == name {
			return true
		}
	}
	return false
}

// EstimateRTTTo returns the estimated RTT to a specific nara using Vivaldi
func (network *Network) EstimateRTTTo(name string) float64 {
	network.local.mu.Lock()
	myCoords := network.local.Me.Status.Coordinates
	network.local.mu.Unlock()

	if myCoords == nil {
		return -1
	}

	nara, exists := network.Neighbourhood[name]
	if !exists {
		return -1
	}

	nara.mu.Lock()
	theirCoords := nara.Status.Coordinates
	nara.mu.Unlock()

	if theirCoords == nil || !theirCoords.IsValid() {
		return -1
	}

	return myCoords.DistanceTo(theirCoords)
}
