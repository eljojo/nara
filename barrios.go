package nara

import (
	"crypto/sha256"
	"encoding/binary"
	"time"
)

var clusterNames = []string{"martini", "sand", "ocean", "basil", "watermelon", "sorbet", "wizard", "bohemian", "pizza", "moai", "ufo", "gem", "fish", "surf", "peach", "sandwich"}
var BarrioEmoji = []string{"🍸", "🏖", "🌊", "🌿", "🍉", "🍧", "🧙", "👽", "🍕", "🗿", "🛸", "💎", "🐠", "🏄", "🍑", "🥪"}

func (network *Network) neighbourhoodMaintenance() {
	for _, name := range network.NeighbourhoodNames() {
		observation := network.local.getObservation(name)
		vibe := network.calculateVibe(name)

		clusterIndex := vibe % len(clusterNames)
		observation.ClusterName = clusterNames[clusterIndex]
		observation.ClusterEmoji = BarrioEmoji[clusterIndex]
		network.local.setObservation(name, observation)
	}
}

func (network *Network) calculateVibe(name string) int {
	// vibe is based on the name and the current month
	// so neighbourhoods shift over time but stay consistent across the network
	hasher := sha256.New()
	hasher.Write([]byte(name))

	year, month, _ := time.Now().Date()
	hasher.Write([]byte(string(rune(year))))
	hasher.Write([]byte(string(rune(month))))

	hash := hasher.Sum(nil)
	// use the first 8 bytes of hash as a uint64
	vibe := binary.BigEndian.Uint64(hash[:8])

	return int(vibe)
}
