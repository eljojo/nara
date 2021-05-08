package main

import (
	"encoding/json"
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
	"math/rand"
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
		ts := chattinessRate(*me, 5, 60)
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

	// logrus.Printf("neighbourhood: %+v", neighbourhood)

	// sleep some random amount to avoid ddosing new friends
	time.Sleep(time.Duration(rand.Intn(10)) * time.Second)

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

		if observation.StartTime == 0 {
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
	ts := chattinessRate(*me, 45, 120)
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
	time.Sleep(60 * time.Second)
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

			if (now-observation.LastSeen) > 240 && !skippingEvents {
				observation.Online = "MISSING"
				me.Status.Observations[name] = observation
				logrus.Printf("observation: %s has disappeared", name)
			}
		}

		time.Sleep(5 * time.Second)
	}
}
