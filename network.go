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

		observation, _ := me.Status.Observations[from]
		observation.LastSeen = time.Now().Unix()
		observation.Online = "ONLINE"
		me.Status.Observations[from] = observation
	} else {
		logrus.Println("whodis?", from)
		if me.Status.Chattiness > 0 {
			heyThere(client)
		}
	}
	// inbox <- [2]string{msg.Topic(), string(msg.Payload())}
}

func heyThereHandler(client mqtt.Client, msg mqtt.Message) {
	var nara Nara
	json.Unmarshal(msg.Payload(), &nara)

	if nara.Name == me.Name || nara.Name == "" {
		return
	}

	neighbourhood[nara.Name] = nara

	observation, seenBefore := me.Status.Observations[nara.Name]

	if !seenBefore {
		observation.StartTime = time.Now().Unix()
	}

	if observation.Online == "OFFLINE" {
		observation.Restarts += 1
		logrus.Printf("%s: hey there! (seen before)", nara.Name)
	} else {
		logrus.Printf("%s: hey there!", nara.Name)
	}

	observation.Online = "ONLINE"
	observation.LastSeen = time.Now().Unix()
	me.Status.Observations[nara.Name] = observation

	// logrus.Printf("neighbourhood: %+v", neighbourhood)

	// sleep some random amount to avoid ddosing new friends
	time.Sleep(time.Duration(rand.Intn(10)) * time.Second)

	heyThere(client)
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
