package nara

import (
	"crypto/sha256"
	"encoding/binary"
	"time"
)

var clusterNames = []string{"martini", "sand", "ocean", "basil", "watermelon", "sorbet", "wizard", "bohemian", "pizza", "moai", "ufo", "gem", "fish", "surf", "peach", "sandwich"}
var BarrioEmoji = []string{"🍸", "🏖", "🌊", "🌿", "🍉", "🍧", "🧙", "👽", "🍕", "🗿", "🛸", "💎", "🐠", "🏄", "🍑", "🥪"}

// neighbourhoodMaintenance assigns each nara to a neighborhood based on Vivaldi proximity.
// Your neighborhood is determined by your closest neighbor - naras that cluster together
// will naturally end up in the same neighborhood without explicit coordination.
func (network *Network) neighbourhoodMaintenance() {
	names := network.NeighbourhoodNames()
	names = append(names, network.meName())

	for _, name := range names {
		observation := network.local.getObservation(name)

		if len(clusterNames) == 0 {
			continue
		}

		// Try to determine neighborhood from Vivaldi proximity
		clusterIndex := network.getProximityBasedCluster(name)
		if clusterIndex < 0 {
			// Fallback to hash-based vibe if no coordinates available
			vibe := calculateVibe(name, time.Now())
			clusterIndex = int(vibe % uint64(len(clusterNames)))
		}

		observation.ClusterName = clusterNames[clusterIndex]
		observation.ClusterEmoji = BarrioEmoji[clusterIndex]
		network.local.setObservation(name, observation)
	}
}

// getProximityBasedCluster determines which cluster a nara belongs to based on Vivaldi.
// Your neighborhood is named after your closest neighbor (or yourself if you're isolated).
// Returns -1 if coordinates aren't available.
func (network *Network) getProximityBasedCluster(name string) int {
	// Get coordinates for this nara
	var coords *NetworkCoordinate

	if name == network.meName() {
		network.local.Me.mu.Lock()
		coords = network.local.Me.Status.Coordinates
		network.local.Me.mu.Unlock()
	} else {
		nara, exists := network.Neighbourhood[name]
		if !exists {
			return -1
		}
		nara.mu.Lock()
		coords = nara.Status.Coordinates
		nara.mu.Unlock()
	}

	if coords == nil || !coords.IsValid() {
		return -1
	}

	// Find the closest neighbor
	closestName := ""
	closestDist := float64(999999)

	// Check distance to all other naras
	for otherName, otherNara := range network.Neighbourhood {
		if otherName == name {
			continue
		}

		otherNara.mu.Lock()
		otherCoords := otherNara.Status.Coordinates
		otherNara.mu.Unlock()

		if otherCoords == nil || !otherCoords.IsValid() {
			continue
		}

		dist := coords.DistanceTo(otherCoords)
		if dist < closestDist {
			closestDist = dist
			closestName = otherName
		}
	}

	// Also check distance to self (for neighbors looking at us)
	if name != network.meName() {
		network.local.Me.mu.Lock()
		myCoords := network.local.Me.Status.Coordinates
		network.local.Me.mu.Unlock()

		if myCoords != nil && myCoords.IsValid() {
			dist := coords.DistanceTo(myCoords)
			if dist < closestDist {
				closestDist = dist
				closestName = network.meName()
			}
		}
	}

	// If no closest found, use own name
	if closestName == "" {
		closestName = name
	}

	// Hash the closest neighbor's name to get a cluster index
	// This means naras with the same "closest" will be in the same cluster
	return nameToClusterIndex(closestName)
}

// nameToClusterIndex deterministically maps a name to a cluster index
func nameToClusterIndex(name string) int {
	hasher := sha256.New()
	hasher.Write([]byte(name))
	hash := hasher.Sum(nil)
	idx := binary.BigEndian.Uint64(hash[:8]) % uint64(len(clusterNames))
	return int(idx)
}

func (network *Network) calculateVibe(name string) uint64 {
	return calculateVibe(name, time.Now())
}

// calculateVibe is the legacy hash-based cluster assignment (used as fallback)
func calculateVibe(name string, t time.Time) uint64 {
	// vibe is based on the name and the current month
	// so neighbourhoods shift over time but stay consistent across the network
	hasher := sha256.New()
	hasher.Write([]byte(name))

	year, month, _ := t.Date()
	hasher.Write([]byte(string(rune(year))))
	hasher.Write([]byte(string(rune(month))))

	hash := hasher.Sum(nil)
	// use the first 8 bytes of hash as a uint64
	vibe := binary.BigEndian.Uint64(hash[:8])

	return vibe
}
