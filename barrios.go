package nara

import (
	"github.com/pbnjay/clustering"
	"sort"
)

var clusterNames = []string{"olive", "peach", "sand", "ocean", "basil", "papaya", "brunch", "sorbet", "margarita", "bohemian", "terracotta"}

func (network *Network) calculateClusters() {
	distanceMap := network.prepareClusteringDistanceMap()
	clusters := clustering.NewDistanceMapClusterSet(distanceMap)

	// the Threshold defines how mini ms between nodes to consider as one cluster
	clustering.Cluster(clusters, clustering.Threshold(50), clustering.CompleteLinkage())
	sortedClusters := network.sortClusters(clusters)

	for clusterIndex, cluster := range sortedClusters {
		for _, name := range cluster {
			observation := network.local.getObservation(name)
			observation.ClusterName = clusterNames[clusterIndex]
			network.local.setObservation(name, observation)
		}
	}

	// set own neighbourhood
	observation := network.local.getMeObservation()
	network.local.Me.Status.Barrio = observation.ClusterName
}

func (network *Network) prepareClusteringDistanceMap() clustering.DistanceMap {
	distanceMap := make(clustering.DistanceMap)

	network.local.mu.Lock()
	for _, nara := range network.Neighbourhood {
		// first create distance map with all pings from the perspective of each neighbour
		distanceMap[nara.Name] = nara.pingMap()
	}
	network.local.mu.Unlock()

	distanceMap[network.meName()] = network.local.Me.pingMap()

	return distanceMap
}

func (network *Network) sortClusters(clusters clustering.ClusterSet) [][]string {
	res := make([][]string, 0)

	clusters.EachCluster(-1, func(clusterIndex int) {
		cl := make([]string, 0)
		clusters.EachItem(clusterIndex, func(nameInterface clustering.ClusterItem) {
			name := nameInterface.(string)
			cl = append(cl, name)
		})
		res = append(res, cl)
	})

	sort.Slice(res, func(i, j int) bool {
		oldestI := network.oldestStarTimeForCluster(res[i])
		oldestJ := network.oldestStarTimeForCluster(res[j])

		// tie-break by oldest start time when clusters are same size otherwise sort by size
		if len(res[i]) == len(res[j]) {
			return oldestI < oldestJ
		} else {
			return len(res[i]) > len(res[j])
		}
	})

	return res
}

func (network *Network) oldestStarTimeForCluster(cluster []string) int64 {
	oldest := int64(0)
	for _, name := range cluster {
		obs := network.local.getObservation(name)
		if (obs.StartTime > 0 && obs.StartTime < oldest) || oldest == 0 {
			oldest = obs.StartTime
		}
	}
	return oldest
}