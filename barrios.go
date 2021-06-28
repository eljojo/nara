package nara

import (
	"github.com/pbnjay/clustering"
	"sort"
)

var clusterNames = []string{"martini", "sand", "ocean", "basil", "watermelon", "sorbet", "wizard", "bohemian", "pizza", "moai", "ufo", "gem", "fish", "surf", "peach", "sandwich"}
var BarrioEmoji = []string{"🍸", "🏖", "🌊", "🌿", "🍉", "🍧", "🧙", "👽", "🍕", "🗿", "🛸", "💎", "🐠", "🏄", "🍑", "🥪"}

func (network *Network) neighbourhoodMaintenance() {
	distanceMap := network.prepareClusteringDistanceMap()
	clusters := clustering.NewDistanceMapClusterSet(distanceMap)

	// the Threshold defines how mini ms between nodes to consider as one cluster
	clustering.Cluster(clusters, clustering.Threshold(50), clustering.CompleteLinkage())
	sortedClusters := network.sortClusters(clusters)

	for clusterIndex, cluster := range sortedClusters {
		for _, name := range cluster {
			observation := network.local.getObservation(name)
			observation.ClusterName = clusterNames[clusterIndex]
			observation.ClusterEmoji = BarrioEmoji[clusterIndex]
			network.local.setObservation(name, observation)
		}
	}
}

func (network Network) prepareClusteringDistanceMap() clustering.DistanceMap {
	distanceMap := make(clustering.DistanceMap)

	network.local.mu.Lock()
	defer network.local.mu.Unlock()
	for _, nara := range network.Neighbourhood {
		// first create distance map with all pings from the perspective of each neighbour
		distanceMap[nara.Name] = nara.pingMap()
	}

	distanceMap[network.meName()] = network.local.Me.pingMap()

	return distanceMap
}

func (network Network) sortClusters(clusters clustering.ClusterSet) [][]string {
	res := make([][]string, 0)

	clusters.EachCluster(-1, func(clusterIndex int) {
		cl := make([]string, 0)
		clusters.EachItem(clusterIndex, func(nameInterface clustering.ClusterItem) {
			name := nameInterface.(string)
			cl = append(cl, name)
		})
		res = append(res, cl)
	})

	sort.SliceStable(res, func(i, j int) bool {
		a := network.sortingKeyForCluster(res[i])
		b := network.sortingKeyForCluster(res[j])

		return a < b
	})

	return res
}

func (network Network) sortingKeyForCluster(cluster []string) string {
	oldest := cluster[0]
	for _, name := range cluster {
		if name < oldest {
			oldest = name
		}
	}
	return oldest
}
