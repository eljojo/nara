package nara

import (
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
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
	newspaperInbox chan NewspaperEvent
	chauInbox      chan Nara
}

type NewspaperEvent struct {
	From   string
	Status NaraStatus
}

func NewNetwork(localNara *LocalNara, host string, user string, pass string) *Network {
	network := &Network{local: localNara}
	network.Neighbourhood = make(map[string]*Nara)
	network.pingInbox = make(chan PingEvent)
	network.heyThereInbox = make(chan Nara)
	network.chauInbox = make(chan Nara)
	network.newspaperInbox = make(chan NewspaperEvent)
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
	go network.processNewspaperEvents()
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

func (network *Network) processNewspaperEvents() {
	for {
		newspaperEvent := <-network.newspaperInbox
		logrus.Debugf("newspaperHandler update from %s", newspaperEvent.From)

		network.local.mu.Lock()
		other, present := network.Neighbourhood[newspaperEvent.From]
		network.local.mu.Unlock()
		if present {
			other.Status = newspaperEvent.Status
			network.Neighbourhood[newspaperEvent.From] = other
		} else {
			logrus.Printf("%s posted a newspaper story (whodis?)", newspaperEvent.From)
			if network.local.Me.Status.Chattiness > 0 {
				network.heyThere()
			}
		}

		network.recordObservationOnlineNara(newspaperEvent.From)
	}
}

func (network *Network) processHeyThereEvents() {
	for {
		nara := <-network.heyThereInbox

		network.local.mu.Lock()
		network.Neighbourhood[nara.Name] = &nara
		network.local.mu.Unlock()
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

		network.local.mu.Lock()
		network.Neighbourhood[nara.Name] = &nara
		network.local.mu.Unlock()

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
