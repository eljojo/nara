package nara

import (
	"github.com/enescakir/emoji"
	"github.com/pbnjay/clustering"
	"github.com/sirupsen/logrus"
	"sort"
)

var clusterNames = []string{"olive", "peach", "sand", "ocean", "basil", "watermelon", "brunch", "sorbet", "margarita", "bohemian", "pizza"}
var BarrioEmoji = []string{"🍸", "🍑", "🏖", "🌊", "🌿", "🍉", "🥪", "🍧", "🧙", "👽", "🍕"}

func (ln LocalNara) Flair() string {
	networkSize := len(ln.Network.Neighbourhood)
	awards := ""
	if networkSize > 2 {
		if ln.Me.Name == ln.Network.oldestNara().Name {
			awards = awards + "🧓"
		}
		if ln.Me.Name == ln.Network.oldestNaraBarrio().Name {
			awards = awards + "👑"
		}
		if ln.Me.Name == ln.Network.youngestNara().Name {
			awards = awards + "🐤"
		}
		if ln.Me.Name == ln.Network.youngestNaraBarrio().Name {
			awards = awards + "🐦"
		}
		if ln.Me.Name == ln.Network.mostRestarts().Name {
			awards = awards + "🔁"
		}
		if ln.Me.Status.Chattiness >= 80 {
			awards = awards + "💬"
		}
		if ln.Me.Status.HostStats.LoadAvg >= 0.8 {
			awards = awards + "📈"
		} else if ln.Me.Status.HostStats.LoadAvg <= 0.1 {
			awards = awards + "🆒"
		}
		if ln.uptime() < (3600 * 6) {
			awards = awards + "👶"
		}
		if ln.isBooting() {
			awards = awards + "📡"
		}
	}
	return awards
}

func (ln LocalNara) LicensePlate() string {
	barrio := ln.getMeObservation().ClusterEmoji
	country, err := emoji.CountryFlag(ln.Me.IRL.CountryCode)
	if err != nil {
		logrus.Panic("lol failed to get country emoji lmao", ln.Me.IRL.CountryCode, err)
	}
	return country.String() + " " + barrio
}

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

func (network Network) oldestStarTimeForCluster(cluster []string) int64 {
	oldest := int64(0)
	for _, name := range cluster {
		obs := network.local.getObservation(name)
		if (obs.StartTime > 0 && obs.StartTime < oldest) || oldest == 0 {
			oldest = obs.StartTime
		}
	}
	return oldest
}
