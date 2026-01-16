package nara

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"sort"
	"time"

	"github.com/eljojo/nara/types"
)

var clusterNames = []string{"martini", "sand", "ocean", "basil", "watermelon", "sorbet", "wizard", "bohemian", "pizza", "moai", "ufo", "gem", "fish", "surf", "peach", "sandwich"}
var BarrioEmoji = []string{"🍸", "🏖", "🌊", "🌿", "🍉", "🍧", "🧙", "👽", "🍕", "🗿", "🛸", "💎", "🐠", "🏄", "🍑", "🥪"}

// Default grid size if no RTT data available (in coordinate units, roughly ~50ms RTT)
const DefaultGridSize = 50.0

// MinGridSize prevents clusters from being too small (at least ~10ms RTT)
const MinGridSize = 10.0

// MaxGridSize prevents clusters from being too large (at most ~200ms RTT)
const MaxGridSize = 200.0

// neighbourhoodMaintenance assigns each nara to a neighborhood based on grid-based clustering.
// Naras in the same grid cell share the same barrio - this is symmetric and location-based.
func (network *Network) neighbourhoodMaintenance() {
	names := network.NeighbourhoodNames()
	names = append(names, network.meName())

	// Calculate grid size from network RTT data (auto-tune)
	gridSize := network.calculateGridSize()

	for _, naraName := range names {
		observation := network.local.getObservation(naraName)
		oldCluster := observation.ClusterName

		if len(clusterNames) == 0 {
			continue
		}

		// Try to determine neighborhood from grid-based clustering
		clusterIndex := network.getGridBasedCluster(naraName, gridSize)
		usedGrid := clusterIndex >= 0
		if clusterIndex < 0 {
			// Fallback to hash-based vibe if no coordinates available
			vibe := calculateVibe(naraName, time.Now())
			clusterIndex = int(vibe % uint64(len(clusterNames)))
		}

		newCluster := clusterNames[clusterIndex]
		observation.ClusterName = newCluster
		observation.ClusterEmoji = BarrioEmoji[clusterIndex]
		network.local.setObservation(naraName, observation)

		// Log barrio changes
		if oldCluster != "" && oldCluster != newCluster && network.logService != nil {
			method := "vibe"
			if usedGrid {
				method = "grid"
			}
			network.logService.BatchBarrioMovement(naraName, oldCluster, newCluster, BarrioEmoji[clusterIndex], method, gridSize)
		}
	}
}

// calculateGridSize auto-tunes the grid size based on network RTT distribution.
// Uses the median RTT to determine what "nearby" means in this network.
func (network *Network) calculateGridSize() float64 {
	if network.local.SyncLedger == nil {
		return DefaultGridSize
	}

	pings := network.local.SyncLedger.GetPingObservations()
	if len(pings) == 0 {
		return DefaultGridSize
	}

	// Collect all RTT values
	rtts := make([]float64, 0, len(pings))
	for _, p := range pings {
		if p.RTT > 0 {
			rtts = append(rtts, p.RTT)
		}
	}

	if len(rtts) == 0 {
		return DefaultGridSize
	}

	// Sort and find median
	sort.Float64s(rtts)
	var median float64
	mid := len(rtts) / 2
	if len(rtts)%2 == 0 {
		median = (rtts[mid-1] + rtts[mid]) / 2
	} else {
		median = rtts[mid]
	}

	// Clamp to reasonable bounds
	gridSize := median
	if gridSize < MinGridSize {
		gridSize = MinGridSize
	}
	if gridSize > MaxGridSize {
		gridSize = MaxGridSize
	}

	return gridSize
}

// getGridBasedCluster determines which cluster a nara belongs to based on grid position.
// Naras in the same grid cell get the same cluster - this is symmetric by design.
// Returns -1 if coordinates aren't available.
func (network *Network) getGridBasedCluster(name types.NaraName, gridSize float64) int {
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

	// Floor coordinates to grid cell (more stable than Round at boundaries)
	cellX := int64(math.Floor(coords.X / gridSize))
	cellY := int64(math.Floor(coords.Y / gridSize))

	// Hash the grid cell position to get a cluster index
	return gridCellToClusterIndex(cellX, cellY)
}

// gridCellToClusterIndex deterministically maps a grid cell to a cluster index.
// Same cell coordinates always produce the same cluster.
func gridCellToClusterIndex(cellX, cellY int64) int {
	hasher := sha256.New()

	// Write cell coordinates as bytes
	buf := make([]byte, 16)
	binary.BigEndian.PutUint64(buf[0:8], uint64(cellX))
	binary.BigEndian.PutUint64(buf[8:16], uint64(cellY))
	hasher.Write(buf)

	hash := hasher.Sum(nil)
	idx := binary.BigEndian.Uint64(hash[:8]) % uint64(len(clusterNames))
	return int(idx)
}

// IsInMyBarrio returns true if the named nara is in the same barrio as me.
// Used for proximity-based teasing boost.
func (network *Network) IsInMyBarrio(name types.NaraName) bool {
	gridSize := network.calculateGridSize()

	myCluster := network.getGridBasedCluster(network.meName(), gridSize)
	theirCluster := network.getGridBasedCluster(name, gridSize)

	// If either doesn't have coordinates, fall back to vibe comparison
	if myCluster < 0 || theirCluster < 0 {
		myVibe := calculateVibe(network.meName(), time.Now()) % uint64(len(clusterNames))
		theirVibe := calculateVibe(name, time.Now()) % uint64(len(clusterNames))
		return myVibe == theirVibe
	}

	return myCluster == theirCluster
}

// GetMyBarrioEmoji returns the emoji for my current barrio
func (network *Network) GetMyBarrioEmoji() string {
	gridSize := network.calculateGridSize()
	clusterIndex := network.getGridBasedCluster(network.meName(), gridSize)

	if clusterIndex < 0 {
		vibe := calculateVibe(network.meName(), time.Now())
		clusterIndex = int(vibe % uint64(len(clusterNames)))
	}

	if clusterIndex >= 0 && clusterIndex < len(BarrioEmoji) {
		return BarrioEmoji[clusterIndex]
	}
	return ""
}

func (network *Network) calculateVibe(name types.NaraName) uint64 {
	return calculateVibe(name, time.Now())
}

// calculateVibe is the legacy hash-based cluster assignment (used as fallback)
func calculateVibe(name types.NaraName, t time.Time) uint64 {
	// vibe is based on the name and the current month
	// so neighbourhoods shift over time but stay consistent across the network
	hasher := sha256.New()
	hasher.Write([]byte(name.String()))

	year, month, _ := t.Date()
	hasher.Write([]byte(string(rune(year))))
	hasher.Write([]byte(string(rune(month))))

	hash := hasher.Sum(nil)
	// use the first 8 bytes of hash as a uint64
	vibe := binary.BigEndian.Uint64(hash[:8])

	return vibe
}
