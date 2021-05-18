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

var neighbourhood = make(map[string]Nara)
var lastHeyThere int64

func announce(client mqtt.Client) {
	topic := fmt.Sprintf("%s/%s", "nara/newspaper", me.Name)
	logrus.Debug("posting on", topic)

	// update neighbour's opinion on us
	recordObservationOnlineNara(me.Name)

	payload, err := json.Marshal(me.Status)
	if err != nil {
		fmt.Println(err)
		return
	}
	token := client.Publish(topic, 0, false, string(payload))
	token.Wait()
}

func announceForever(client mqtt.Client) {
	for {
		ts := chattinessRate(*me, 20, 30)
		time.Sleep(time.Duration(ts) * time.Second)

		announce(client)
	}
}

func chattinessRate(nara Nara, min int64, max int64) int64 {
	return min + ((max - min) * (100 - nara.Status.Chattiness) / 100)
}

var skippingEvents = false

func newspaperHandler(client mqtt.Client, msg mqtt.Message) {
	if me.Status.Chattiness <= 10 && skippingEvents == false {
		logrus.Println("[warning] low chattiness, newspaper events may be dropped")
		skippingEvents = true
	} else if me.Status.Chattiness > 10 && skippingEvents == true {
		logrus.Println("[recovered] chattiness is healthy again, not dropping events anymore")
		skippingEvents = false
	}
	if skippingEvents == true && rand.Intn(2) == 0 {
		return
	}
	if !strings.Contains(msg.Topic(), "nara/newspaper/") {
		return
	}
	var from = strings.Split(msg.Topic(), "nara/newspaper/")[1]

	if from == me.Name {
		return
	}

	var status NaraStatus
	json.Unmarshal(msg.Payload(), &status)

	// logrus.Printf("newspaperHandler update from %s: %+v", from, status)

	other, present := neighbourhood[from]
	if present {
		other.Status = status
		neighbourhood[from] = other
	} else {
		logrus.Printf("%s posted a newspaper story (whodis?)", from)
		if me.Status.Chattiness > 0 {
			heyThere(client)
		}
	}

	recordObservationOnlineNara(from)
	// inbox <- [2]string{msg.Topic(), string(msg.Payload())}
}

func heyThereHandler(client mqtt.Client, msg mqtt.Message) {
	var nara Nara
	json.Unmarshal(msg.Payload(), &nara)

	if nara.Name == me.Name || nara.Name == "" {
		return
	}

	neighbourhood[nara.Name] = nara
	logrus.Printf("%s says: hey there!", nara.Name)
	recordObservationOnlineNara(nara.Name)

	heyThere(client)
}

func recordObservationOnlineNara(name string) {
	observation, _ := me.Status.Observations[name]

	if observation.StartTime == 0 || name == me.Name {
		if name != me.Name {
			logrus.Printf("observation: seen %s for the first time", name)
		}

		observation.Restarts = findRestartCountFromNeighbourhoodForNara(name)
		observation.StartTime = findStartingTimeFromNeighbourhoodForNara(name)
		observation.LastRestart = findLastRestartFromNeighbourhoodForNara(name)

		if observation.StartTime == 0 && name == me.Name {
			observation.StartTime = time.Now().Unix()
		}

		if observation.LastRestart == 0 && name == me.Name {
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
	me.Status.Observations[name] = observation
}

func heyThere(client mqtt.Client) {
	ts := chattinessRate(*me, 10, 20)
	if (time.Now().Unix() - lastHeyThere) <= ts {
		return
	}

	lastHeyThere = time.Now().Unix()

	topic := "nara/plaza/hey_there"
	logrus.Printf("posting to %s", topic)

	payload, err := json.Marshal(me)
	if err != nil {
		fmt.Println(err)
		return
	}
	token := client.Publish(topic, 0, false, string(payload))
	token.Wait()
}

func chauHandler(client mqtt.Client, msg mqtt.Message) {
	var nara Nara
	json.Unmarshal(msg.Payload(), &nara)

	if nara.Name == me.Name || nara.Name == "" {
		return
	}

	observation, _ := me.Status.Observations[nara.Name]
	observation.Online = "OFFLINE"
	observation.LastSeen = time.Now().Unix()
	me.Status.Observations[nara.Name] = observation
	neighbourhood[nara.Name] = nara

	_, present := me.Status.PingStats[nara.Name]
	if present {
		delete(me.Status.PingStats, nara.Name)
	}

	logrus.Printf("%s: chau!", nara.Name)
}

func chau(client mqtt.Client) {
	topic := "nara/plaza/chau"
	logrus.Printf("posting to %s", topic)

	payload, err := json.Marshal(me)
	if err != nil {
		fmt.Println(err)
		return
	}
	token := client.Publish(topic, 0, false, string(payload))
	token.Wait()
}

func formOpinion() {
	time.Sleep(40 * time.Second)
	logrus.Printf("forming opinions")
	for name, _ := range neighbourhood {
		observation, _ := me.Status.Observations[name]
		startTime := findStartingTimeFromNeighbourhoodForNara(name)
		if startTime > 0 {
			observation.StartTime = startTime
			logrus.Printf("adjusting start time for %s based on neighbours opinion", name)
		}
		restarts := findRestartCountFromNeighbourhoodForNara(name)
		if restarts > 0 {
			logrus.Printf("adjusting restart count to %d for %s based on neighbours opinion", restarts, name)
			observation.Restarts = restarts
		}
		lastRestart := findLastRestartFromNeighbourhoodForNara(name)
		if lastRestart > 0 {
			logrus.Printf("adjusting last restart date for %s based on neighbours opinion", name)
			observation.LastRestart = lastRestart
		}
		me.Status.Observations[name] = observation
	}
}

func findStartingTimeFromNeighbourhoodForNara(name string) int64 {
	times := make(map[int64]int)

	for _, nara := range neighbourhood {
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

func findRestartCountFromNeighbourhoodForNara(name string) int64 {
	values := make(map[int64]int)

	for _, nara := range neighbourhood {
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
func findLastRestartFromNeighbourhoodForNara(name string) int64 {
	values := make(map[int64]int)

	for _, nara := range neighbourhood {
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

func observationMaintenance() {
	for {
		now := time.Now().Unix()

		for name, observation := range me.Status.Observations {
			// only do maintenance on naras that are online
			if observation.Online != "ONLINE" {
				continue
			}

			// mark missing after 100 seconds of no updates
			if (now-observation.LastSeen) > 100 && !skippingEvents {
				observation.Online = "MISSING"
				me.Status.Observations[name] = observation
				logrus.Printf("observation: %s has disappeared", name)
			}
		}

		calculateClusters()

		time.Sleep(1 * time.Second)
	}
}

var clusterNames = []string{"olive", "peach", "sand", "ocean", "basil", "papaya", "brunch", "sorbet", "margarita", "bohemian", "terracotta"}

func calculateClusters() {

	distanceMap := prepareClusteringDistanceMap()
	clusters := clustering.NewDistanceMapClusterSet(distanceMap)
	clustering.Cluster(clusters, clustering.Threshold(10), clustering.CompleteLinkage())
	sortedClusters := sortClusters(clusters)

	for clusterIndex, cluster := range sortedClusters {
		for _, name := range cluster {
			observation, _ := me.Status.Observations[name]
			observation.ClusterName = clusterNames[clusterIndex]
			me.Status.Observations[name] = observation
		}
	}

	observation, _ := me.Status.Observations[me.Name]
	me.Status.Barrio = observation.ClusterName
}

func prepareClusteringDistanceMap() clustering.DistanceMap {
	distanceMap := make(clustering.DistanceMap)

	// first create distance map with all pings from the perspective of each neighbour
	for _, nara := range neighbourhood {
		distanceMap[nara.Name] = make(map[clustering.ClusterItem]float64)
		for otherNara, ping := range nara.Status.PingStats {
			if otherNara == "google" {
				continue
			}
			distanceMap[nara.Name][otherNara] = ping
		}

		// reset observations for offline naras
		observation, _ := me.Status.Observations[nara.Name]
		observation.ClusterName = ""
		me.Status.Observations[nara.Name] = observation
	}

	// add own ping stats
	distanceMap[me.Name] = make(map[clustering.ClusterItem]float64)
	for otherNara, ping := range me.Status.PingStats {
		if otherNara == "google" {
			continue
		}
		distanceMap[me.Name][otherNara] = ping
	}

	return distanceMap
}

func sortClusters(clusters clustering.ClusterSet) [][]string {
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
		oldestI := oldestStarTimeForCluster(res[i])
		oldestJ := oldestStarTimeForCluster(res[j])

		// tie-break by oldest start time when clusters are same size otherwise sort by size
		if len(res[i]) == len(res[j]) {
			return oldestI < oldestJ
		} else {
			return len(res[i]) > len(res[j])
		}
	})

	return res
}

func oldestStarTimeForCluster(cluster []string) int64 {
	oldest := int64(0)
	for _, name := range cluster {
		obs, _ := me.Status.Observations[name]
		if (obs.StartTime > 0 && obs.StartTime < oldest) || oldest == 0 {
			oldest = obs.StartTime
		}
	}
	return oldest
}
