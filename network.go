package nara

import (
	"encoding/json"
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/pbnjay/clustering"
	"github.com/sirupsen/logrus"
	"math/rand"
	"sort"
	"strings"
	"time"
)

type Network struct {
	Neighbourhood  map[string]Nara
	LastHeyThere   int64
	skippingEvents bool
	local          *LocalNara
	Mqtt           mqtt.Client
}

func NewNetwork(localNara *LocalNara, host string, user string, pass string) *Network {
	network := &Network{local: localNara}
	network.Neighbourhood = make(map[string]Nara)
	network.skippingEvents = false
	network.Mqtt = initializeMQTT(network.mqttOnConnectHandler(), network.meName(), host, user, pass)
	return network
}

func (network *Network) Start() {
	go network.formOpinion()
	go network.observationMaintenance()
	go network.announceForever()
}

func (network *Network) meName() string {
	return network.local.Me.Name
}

func (network *Network) Connect() {
	if token := network.Mqtt.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}
}

func (network *Network) announce() {
	topic := fmt.Sprintf("%s/%s", "nara/newspaper", network.meName())
	logrus.Debug("posting on", topic)

	// update neighbour's opinion on us
	network.recordObservationOnlineNara(network.meName())

	payload, err := json.Marshal(network.local.Me.Status)
	if err != nil {
		fmt.Println(err)
		return
	}
	token := network.Mqtt.Publish(topic, 0, false, string(payload))
	token.Wait()
}

func (network *Network) announceForever() {
	for {
		ts := chattinessRate(*network.local.Me, 20, 30)
		time.Sleep(time.Duration(ts) * time.Second)

		network.announce()
	}
}

func chattinessRate(nara Nara, min int64, max int64) int64 {
	return min + ((max - min) * (100 - nara.Status.Chattiness) / 100)
}

func (network *Network) newspaperHandler(client mqtt.Client, msg mqtt.Message) {
	if network.local.Me.Status.Chattiness <= 10 && network.skippingEvents == false {
		logrus.Println("[warning] low chattiness, newspaper events may be dropped")
		network.skippingEvents = true
	} else if network.local.Me.Status.Chattiness > 10 && network.skippingEvents == true {
		logrus.Println("[recovered] chattiness is healthy again, not dropping events anymore")
		network.skippingEvents = false
	}
	if network.skippingEvents == true && rand.Intn(2) == 0 {
		return
	}
	if !strings.Contains(msg.Topic(), "nara/newspaper/") {
		return
	}
	var from = strings.Split(msg.Topic(), "nara/newspaper/")[1]

	if from == network.meName() {
		return
	}

	var status NaraStatus
	json.Unmarshal(msg.Payload(), &status)

	// logrus.Printf("newspaperHandler update from %s: %+v", from, status)

	other, present := network.Neighbourhood[from]
	if present {
		other.Status = status
		network.Neighbourhood[from] = other
	} else {
		logrus.Printf("%s posted a newspaper story (whodis?)", from)
		if network.local.Me.Status.Chattiness > 0 {
			network.heyThere()
		}
	}

	network.recordObservationOnlineNara(from)
	// inbox <- [2]string{msg.Topic(), string(msg.Payload())}
}

func (network *Network) heyThereHandler(client mqtt.Client, msg mqtt.Message) {
	var nara Nara
	json.Unmarshal(msg.Payload(), &nara)

	if nara.Name == network.meName() || nara.Name == "" {
		return
	}

	network.Neighbourhood[nara.Name] = nara
	logrus.Printf("%s says: hey there!", nara.Name)
	network.recordObservationOnlineNara(nara.Name)

	network.heyThere()
}

func (network *Network) recordObservationOnlineNara(name string) {
	observation, _ := network.local.Me.Status.Observations[name]

	if observation.StartTime == 0 || name == network.meName() {
		if name != network.meName() {
			logrus.Printf("observation: seen %s for the first time", name)
		}

		observation.Restarts = network.findRestartCountFromNeighbourhoodForNara(name)
		observation.StartTime = network.findStartingTimeFromNeighbourhoodForNara(name)
		observation.LastRestart = network.findLastRestartFromNeighbourhoodForNara(name)

		if observation.StartTime == 0 && name == network.meName() {
			observation.StartTime = time.Now().Unix()
		}

		if observation.LastRestart == 0 && name == network.meName() {
			observation.LastRestart = time.Now().Unix()
		}
	}

	if observation.Online != "ONLINE" && observation.Online != "" {
		observation.Restarts += 1
		observation.LastRestart = time.Now().Unix()
		logrus.Printf("observation: %s came back online", name)
	}

	observation.Online = "ONLINE"
	observation.LastSeen = time.Now().Unix()
	network.local.Me.Status.Observations[name] = observation
}

func (network *Network) heyThere() {
	ts := chattinessRate(*network.local.Me, 10, 20)
	if (time.Now().Unix() - network.LastHeyThere) <= ts {
		return
	}

	network.LastHeyThere = time.Now().Unix()

	topic := "nara/plaza/hey_there"
	logrus.Printf("posting to %s", topic)

	payload, err := json.Marshal(network.local.Me)
	if err != nil {
		fmt.Println(err)
		return
	}
	token := network.Mqtt.Publish(topic, 0, false, string(payload))
	token.Wait()
}

func (network *Network) chauHandler(client mqtt.Client, msg mqtt.Message) {
	var nara Nara
	json.Unmarshal(msg.Payload(), &nara)

	if nara.Name == network.meName() || nara.Name == "" {
		return
	}

	observation, _ := network.local.Me.Status.Observations[nara.Name]
	observation.Online = "OFFLINE"
	observation.LastSeen = time.Now().Unix()
	network.local.Me.Status.Observations[nara.Name] = observation
	network.Neighbourhood[nara.Name] = nara

	_, present := network.local.Me.Status.PingStats[nara.Name]
	if present {
		delete(network.local.Me.Status.PingStats, nara.Name)
	}

	logrus.Printf("%s: chau!", nara.Name)
}

func (network *Network) Chau() {
	topic := "nara/plaza/chau"
	logrus.Printf("posting to %s", topic)

	observation, _ := network.local.Me.Status.Observations[network.meName()]
	observation.Online = "OFFLINE"
	observation.LastSeen = time.Now().Unix()
	network.local.Me.Status.Observations[network.meName()] = observation

	payload, err := json.Marshal(network.local.Me)
	if err != nil {
		fmt.Println(err)
		return
	}
	token := network.Mqtt.Publish(topic, 0, false, string(payload))
	token.Wait()
}

func (network *Network) formOpinion() {
	time.Sleep(40 * time.Second)
	logrus.Printf("🕵️  forming opinions...")
	for name, _ := range network.Neighbourhood {
		observation, _ := network.local.Me.Status.Observations[name]
		startTime := network.findStartingTimeFromNeighbourhoodForNara(name)
		if startTime > 0 {
			observation.StartTime = startTime
		} else {
			logrus.Printf("couldn't adjust startTime for %s based on neighbour disagreement", name)
		}
		restarts := network.findRestartCountFromNeighbourhoodForNara(name)
		if restarts > 0 {
			observation.Restarts = restarts
		} else {
			logrus.Printf("couldn't adjust restart count for %s based on neighbour disagreement", name)
		}
		lastRestart := network.findLastRestartFromNeighbourhoodForNara(name)
		if lastRestart > 0 {
			observation.LastRestart = lastRestart
		} else {
			logrus.Printf("couldn't adjust last restart date for %s based on neighbour disagreement", name)
		}
		network.local.Me.Status.Observations[name] = observation
	}
}

func (network *Network) findStartingTimeFromNeighbourhoodForNara(name string) int64 {
	times := make(map[int64]int)

	for _, nara := range network.Neighbourhood {
		observed_start_time := nara.Status.Observations[name].StartTime
		if observed_start_time > 0 {
			times[observed_start_time] += 1
		}
	}

	var startTime int64
	maxSeen := 0
	one_third := len(times) / 3

	for time, count := range times {
		if count > maxSeen && count > one_third {
			maxSeen = count
			startTime = time
		}
	}

	return startTime
}

func (network *Network) findRestartCountFromNeighbourhoodForNara(name string) int64 {
	values := make(map[int64]int)

	for _, nara := range network.Neighbourhood {
		restarts := nara.Status.Observations[name].Restarts
		values[restarts] += 1
	}

	var result int64
	maxSeen := 0

	for restarts, count := range values {
		if count > maxSeen && restarts > 0 {
			maxSeen = count
			result = restarts
		}
	}

	return result
}

func (network *Network) findLastRestartFromNeighbourhoodForNara(name string) int64 {
	values := make(map[int64]int)

	for _, nara := range network.Neighbourhood {
		last_restart := nara.Status.Observations[name].LastRestart
		if last_restart > 0 {
			values[last_restart] += 1
		}
	}

	var result int64
	maxSeen := 0
	one_third := len(values) / 3

	for last_restart, count := range values {
		if count > maxSeen && count > one_third {
			maxSeen = count
			result = last_restart
		}
	}

	return result
}

func (network *Network) observationMaintenance() {
	for {
		now := time.Now().Unix()

		for name, observation := range network.local.Me.Status.Observations {
			// only do maintenance on naras that are online
			if observation.Online != "ONLINE" {
				continue
			}

			// mark missing after 100 seconds of no updates
			if (now-observation.LastSeen) > 100 && !network.skippingEvents {
				observation.Online = "MISSING"
				network.local.Me.Status.Observations[name] = observation
				logrus.Printf("observation: %s has disappeared", name)
			}
		}

		network.calculateClusters()

		time.Sleep(1 * time.Second)
	}
}

var clusterNames = []string{"olive", "peach", "sand", "ocean", "basil", "papaya", "brunch", "sorbet", "margarita", "bohemian", "terracotta"}

func (network *Network) calculateClusters() {

	distanceMap := network.prepareClusteringDistanceMap()
	clusters := clustering.NewDistanceMapClusterSet(distanceMap)

	// the Threshold defines how mini ms between nodes to consider as one cluster
	clustering.Cluster(clusters, clustering.Threshold(50), clustering.CompleteLinkage())
	sortedClusters := network.sortClusters(clusters)

	for clusterIndex, cluster := range sortedClusters {
		for _, name := range cluster {
			observation, _ := network.local.Me.Status.Observations[name]
			observation.ClusterName = clusterNames[clusterIndex]
			network.local.Me.Status.Observations[name] = observation
		}
	}

	observation, _ := network.local.Me.Status.Observations[network.local.Me.Name]
	network.local.Me.Status.Barrio = observation.ClusterName
}

func (network *Network) prepareClusteringDistanceMap() clustering.DistanceMap {
	distanceMap := make(clustering.DistanceMap)

	// first create distance map with all pings from the perspective of each neighbour
	for _, nara := range network.Neighbourhood {
		distanceMap[nara.Name] = make(map[clustering.ClusterItem]float64)
		for otherNara, ping := range nara.Status.PingStats {
			if otherNara == "google" {
				continue
			}
			distanceMap[nara.Name][otherNara] = ping
		}

		// reset observations for offline naras
		observation, _ := network.local.Me.Status.Observations[nara.Name]
		observation.ClusterName = ""
		network.local.Me.Status.Observations[nara.Name] = observation
	}

	// add own ping stats
	distanceMap[network.meName()] = make(map[clustering.ClusterItem]float64)
	for otherNara, ping := range network.local.Me.Status.PingStats {
		if otherNara == "google" {
			continue
		}
		distanceMap[network.meName()][otherNara] = ping
	}

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
		obs, _ := network.local.Me.Status.Observations[name]
		if (obs.StartTime > 0 && obs.StartTime < oldest) || oldest == 0 {
			oldest = obs.StartTime
		}
	}
	return oldest
}
