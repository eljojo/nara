package nara

import (
	"encoding/json"
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
	"math/rand"
	"strings"
	"time"
)

type Network struct {
	Neighbourhood  map[string]*Nara
	LastHeyThere   int64
	skippingEvents bool
	local          *LocalNara
	Mqtt           mqtt.Client
	pingInbox      chan PingEvent
	heyThereInbox  chan Nara
	chauInbox      chan Nara
}

func NewNetwork(localNara *LocalNara, host string, user string, pass string) *Network {
	network := &Network{local: localNara}
	network.Neighbourhood = make(map[string]*Nara)
	network.pingInbox = make(chan PingEvent)
	network.heyThereInbox = make(chan Nara)
	network.chauInbox = make(chan Nara)
	network.skippingEvents = false
	network.Mqtt = initializeMQTT(network.mqttOnConnectHandler(), network.meName(), host, user, pass)
	return network
}

func (network *Network) Start() {
	if token := network.Mqtt.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	go network.formOpinion()
	go network.observationMaintenance()
	go network.announceForever()
	go network.processPingEvents()
	go network.processHeyThereEvents()
	go network.processChauEvents()
}

func (network *Network) meName() string {
	return network.local.Me.Name
}

func (network *Network) announce() {
	topic := fmt.Sprintf("%s/%s", "nara/newspaper", network.meName())

	// update neighbour's opinion on us
	network.recordObservationOnlineNara(network.meName())

	network.postEvent(topic, network.local.Me.Status)
}

func (network *Network) announceForever() {
	for {
		ts := network.local.Me.chattinessRate(20, 30)
		time.Sleep(time.Duration(ts) * time.Second)

		network.announce()
	}
}

func (network *Network) newspaperHandler(client mqtt.Client, msg mqtt.Message) {
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
}

func (network *Network) processHeyThereEvents() {
	for {
		nara := <-network.heyThereInbox

		if nara.Name == network.meName() || nara.Name == "" {
			continue
		}

		network.Neighbourhood[nara.Name] = &nara
		logrus.Printf("%s says: hey there!", nara.Name)
		network.recordObservationOnlineNara(nara.Name)

		network.heyThere()
	}
}

func (network *Network) heyThere() {
	topic := "nara/plaza/hey_there"

	ts := network.local.Me.chattinessRate(10, 20)
	if (time.Now().Unix() - network.LastHeyThere) <= ts {
		return
	}

	network.LastHeyThere = time.Now().Unix()
	network.postEvent(topic, network.local.Me)
}

func (network *Network) processChauEvents() {
	for {
		nara := <-network.chauInbox

		if nara.Name == network.meName() || nara.Name == "" {
			continue
		}

		observation := network.local.getObservation(nara.Name)
		observation.Online = "OFFLINE"
		observation.LastSeen = time.Now().Unix()
		network.local.setObservation(nara.Name, observation)
		network.Neighbourhood[nara.Name] = &nara

		network.local.Me.forgetPing(nara.Name)

		logrus.Printf("%s: chau!", nara.Name)
	}
}

func (network *Network) Chau() {
	topic := "nara/plaza/chau"
	logrus.Printf("posting to %s", topic)

	observation := network.local.getMeObservation()
	observation.Online = "OFFLINE"
	observation.LastSeen = time.Now().Unix()
	network.local.setMeObservation(observation)

	network.postEvent(topic, network.local.Me)
}
