package main

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
}

func NewNetwork() *Network {
	network := &Network{}
	network.Neighbourhood = make(map[string]Nara)
	network.skippingEvents = false
	return network
}

func (ln *LocalNara) announce() {
	topic := fmt.Sprintf("%s/%s", "nara/newspaper", ln.Me.Name)
	logrus.Debug("posting on", topic)

	// update neighbour's opinion on us
	ln.recordObservationOnlineNara(ln.Me.Name)

	payload, err := json.Marshal(ln.Me.Status)
	if err != nil {
		fmt.Println(err)
		return
	}
	token := ln.Mqtt.Publish(topic, 0, false, string(payload))
	token.Wait()
}

func (ln *LocalNara) announceForever() {
	for {
		ts := chattinessRate(*ln.Me, 20, 30)
		time.Sleep(time.Duration(ts) * time.Second)

		ln.announce()
	}
}

func chattinessRate(nara Nara, min int64, max int64) int64 {
	return min + ((max - min) * (100 - nara.Status.Chattiness) / 100)
}

func (ln *LocalNara) newspaperHandler(client mqtt.Client, msg mqtt.Message) {
	if ln.Me.Status.Chattiness <= 10 && ln.Network.skippingEvents == false {
		logrus.Println("[warning] low chattiness, newspaper events may be dropped")
		ln.Network.skippingEvents = true
	} else if ln.Me.Status.Chattiness > 10 && ln.Network.skippingEvents == true {
		logrus.Println("[recovered] chattiness is healthy again, not dropping events anymore")
		ln.Network.skippingEvents = false
	}
	if ln.Network.skippingEvents == true && rand.Intn(2) == 0 {
		return
	}
	if !strings.Contains(msg.Topic(), "nara/newspaper/") {
		return
	}
	var from = strings.Split(msg.Topic(), "nara/newspaper/")[1]

	if from == ln.Me.Name {
		return
	}

	var status NaraStatus
	json.Unmarshal(msg.Payload(), &status)

	// logrus.Printf("newspaperHandler update from %s: %+v", from, status)

	other, present := ln.Network.Neighbourhood[from]
	if present {
		other.Status = status
		ln.Network.Neighbourhood[from] = other
	} else {
		logrus.Printf("%s posted a newspaper story (whodis?)", from)
		if ln.Me.Status.Chattiness > 0 {
			ln.heyThere()
		}
	}

	ln.recordObservationOnlineNara(from)
	// inbox <- [2]string{msg.Topic(), string(msg.Payload())}
}

func (ln *LocalNara) heyThereHandler(client mqtt.Client, msg mqtt.Message) {
	var nara Nara
	json.Unmarshal(msg.Payload(), &nara)

	if nara.Name == ln.Me.Name || nara.Name == "" {
		return
	}

	ln.Network.Neighbourhood[nara.Name] = nara
	logrus.Printf("%s says: hey there!", nara.Name)
	ln.recordObservationOnlineNara(nara.Name)

	ln.heyThere()
}

func (ln *LocalNara) recordObservationOnlineNara(name string) {
	observation, _ := ln.Me.Status.Observations[name]

	if observation.StartTime == 0 || name == ln.Me.Name {
		if name != ln.Me.Name {
			logrus.Printf("observation: seen %s for the first time", name)
		}

		observation.Restarts = ln.findRestartCountFromNeighbourhoodForNara(name)
		observation.StartTime = ln.findStartingTimeFromNeighbourhoodForNara(name)
		observation.LastRestart = ln.findLastRestartFromNeighbourhoodForNara(name)

		if observation.StartTime == 0 && name == ln.Me.Name {
			observation.StartTime = time.Now().Unix()
		}

		if observation.LastRestart == 0 && name == ln.Me.Name {
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
	ln.Me.Status.Observations[name] = observation
}

func (ln *LocalNara) heyThere() {
	ts := chattinessRate(*ln.Me, 10, 20)
	if (time.Now().Unix() - ln.Network.LastHeyThere) <= ts {
		return
	}

	ln.Network.LastHeyThere = time.Now().Unix()

	topic := "nara/plaza/hey_there"
	logrus.Printf("posting to %s", topic)

	payload, err := json.Marshal(ln.Me)
	if err != nil {
		fmt.Println(err)
		return
	}
	token := ln.Mqtt.Publish(topic, 0, false, string(payload))
	token.Wait()
}

func (ln *LocalNara) chauHandler(client mqtt.Client, msg mqtt.Message) {
	var nara Nara
	json.Unmarshal(msg.Payload(), &nara)

	if nara.Name == ln.Me.Name || nara.Name == "" {
		return
	}

	observation, _ := ln.Me.Status.Observations[nara.Name]
	observation.Online = "OFFLINE"
	observation.LastSeen = time.Now().Unix()
	ln.Me.Status.Observations[nara.Name] = observation
	ln.Network.Neighbourhood[nara.Name] = nara

	_, present := ln.Me.Status.PingStats[nara.Name]
	if present {
		delete(ln.Me.Status.PingStats, nara.Name)
	}

	logrus.Printf("%s: chau!", nara.Name)
}

func (ln *LocalNara) chau() {
	topic := "nara/plaza/chau"
	logrus.Printf("posting to %s", topic)

	observation, _ := ln.Me.Status.Observations[ln.Me.Name]
	observation.Online = "OFFLINE"
	observation.LastSeen = time.Now().Unix()
	ln.Me.Status.Observations[ln.Me.Name] = observation

	payload, err := json.Marshal(ln.Me)
	if err != nil {
		fmt.Println(err)
		return
	}
	token := ln.Mqtt.Publish(topic, 0, false, string(payload))
	token.Wait()
}

func (ln *LocalNara) formOpinion() {
	time.Sleep(40 * time.Second)
	logrus.Printf("forming opinions")
	for name, _ := range ln.Network.Neighbourhood {
		observation, _ := ln.Me.Status.Observations[name]
		startTime := ln.findStartingTimeFromNeighbourhoodForNara(name)
		if startTime > 0 {
			observation.StartTime = startTime
			logrus.Printf("adjusting start time for %s based on neighbours opinion", name)
		}
		restarts := ln.findRestartCountFromNeighbourhoodForNara(name)
		if restarts > 0 {
			logrus.Printf("adjusting restart count to %d for %s based on neighbours opinion", restarts, name)
			observation.Restarts = restarts
		}
		lastRestart := ln.findLastRestartFromNeighbourhoodForNara(name)
		if lastRestart > 0 {
			logrus.Printf("adjusting last restart date for %s based on neighbours opinion", name)
			observation.LastRestart = lastRestart
		}
		ln.Me.Status.Observations[name] = observation
	}
}

func (ln *LocalNara) findStartingTimeFromNeighbourhoodForNara(name string) int64 {
	times := make(map[int64]int)

	for _, nara := range ln.Network.Neighbourhood {
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

func (ln *LocalNara) findRestartCountFromNeighbourhoodForNara(name string) int64 {
	values := make(map[int64]int)

	for _, nara := range ln.Network.Neighbourhood {
		restarts := nara.Status.Observations[name].Restarts
		values[restarts] += 1
	}

	var result int64
	maxSeen := 0
	one_third := len(values) / 3

	for restarts, count := range values {
		if count > maxSeen && count > one_third {
			maxSeen = count
			result = restarts
		}
	}

	return result
}
func (ln *LocalNara) findLastRestartFromNeighbourhoodForNara(name string) int64 {
	values := make(map[int64]int)

	for _, nara := range ln.Network.Neighbourhood {
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

func (ln *LocalNara) observationMaintenance() {
	for {
		now := time.Now().Unix()

		for name, observation := range ln.Me.Status.Observations {
			// only do maintenance on naras that are online
			if observation.Online != "ONLINE" {
				continue
			}

			// mark missing after 100 seconds of no updates
			if (now-observation.LastSeen) > 100 && !ln.Network.skippingEvents {
				observation.Online = "MISSING"
				ln.Me.Status.Observations[name] = observation
				logrus.Printf("observation: %s has disappeared", name)
			}
		}

		ln.calculateClusters()

		time.Sleep(1 * time.Second)
	}
}

var clusterNames = []string{"olive", "peach", "sand", "ocean", "basil", "papaya", "brunch", "sorbet", "margarita", "bohemian", "terracotta"}

func (ln *LocalNara) calculateClusters() {

	distanceMap := ln.prepareClusteringDistanceMap()
	clusters := clustering.NewDistanceMapClusterSet(distanceMap)

	// the Threshold defines how mini ms between nodes to consider as one cluster
	clustering.Cluster(clusters, clustering.Threshold(50), clustering.CompleteLinkage())
	sortedClusters := ln.sortClusters(clusters)

	for clusterIndex, cluster := range sortedClusters {
		for _, name := range cluster {
			observation, _ := ln.Me.Status.Observations[name]
			observation.ClusterName = clusterNames[clusterIndex]
			ln.Me.Status.Observations[name] = observation
		}
	}

	observation, _ := ln.Me.Status.Observations[ln.Me.Name]
	ln.Me.Status.Barrio = observation.ClusterName
}

func (ln *LocalNara) prepareClusteringDistanceMap() clustering.DistanceMap {
	distanceMap := make(clustering.DistanceMap)

	// first create distance map with all pings from the perspective of each neighbour
	for _, nara := range ln.Network.Neighbourhood {
		distanceMap[nara.Name] = make(map[clustering.ClusterItem]float64)
		for otherNara, ping := range nara.Status.PingStats {
			if otherNara == "google" {
				continue
			}
			distanceMap[nara.Name][otherNara] = ping
		}

		// reset observations for offline naras
		observation, _ := ln.Me.Status.Observations[nara.Name]
		observation.ClusterName = ""
		ln.Me.Status.Observations[nara.Name] = observation
	}

	// add own ping stats
	distanceMap[ln.Me.Name] = make(map[clustering.ClusterItem]float64)
	for otherNara, ping := range ln.Me.Status.PingStats {
		if otherNara == "google" {
			continue
		}
		distanceMap[ln.Me.Name][otherNara] = ping
	}

	return distanceMap
}

func (ln *LocalNara) sortClusters(clusters clustering.ClusterSet) [][]string {
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
		oldestI := ln.oldestStarTimeForCluster(res[i])
		oldestJ := ln.oldestStarTimeForCluster(res[j])

		// tie-break by oldest start time when clusters are same size otherwise sort by size
		if len(res[i]) == len(res[j]) {
			return oldestI < oldestJ
		} else {
			return len(res[i]) > len(res[j])
		}
	})

	return res
}

func (ln *LocalNara) oldestStarTimeForCluster(cluster []string) int64 {
	oldest := int64(0)
	for _, name := range cluster {
		obs, _ := ln.Me.Status.Observations[name]
		if (obs.StartTime > 0 && obs.StartTime < oldest) || oldest == 0 {
			oldest = obs.StartTime
		}
	}
	return oldest
}
