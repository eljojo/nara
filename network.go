package nara

import (
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
	"time"
)

type Network struct {
	Neighbourhood  map[string]*Nara
	Buzz           *Buzz
	LastHeyThere   int64
	LastSelfie     int64
	skippingEvents bool
	local          *LocalNara
	Mqtt           mqtt.Client
	pingInbox      chan PingEvent
	heyThereInbox  chan HeyThereEvent
	newspaperInbox chan NewspaperEvent
	chauInbox      chan Nara
	selfieInbox    chan Nara
}

type NewspaperEvent struct {
	From   string
	Status NaraStatus
}

type HeyThereEvent struct {
	From string
}

func NewNetwork(localNara *LocalNara, host string, user string, pass string) *Network {
	network := &Network{local: localNara}
	network.Neighbourhood = make(map[string]*Nara)
	network.pingInbox = make(chan PingEvent)
	network.heyThereInbox = make(chan HeyThereEvent)
	network.chauInbox = make(chan Nara)
	network.selfieInbox = make(chan Nara)
	network.newspaperInbox = make(chan NewspaperEvent)
	network.skippingEvents = false
	network.Buzz = newBuzz()
	network.Mqtt = initializeMQTT(network.mqttOnConnectHandler(), network.meName(), host, user, pass)
	return network
}

func (network *Network) Start() {
	err := network.startHttpServer()
	if err != nil {
		logrus.Panic(err)
	}

	if token := network.Mqtt.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	network.heyThere()
	network.announce()

	time.Sleep(1 * time.Second)

	go network.formOpinion()
	go network.observationMaintenance()
	go network.announceForever()
	go network.processPingEvents()
	go network.processHeyThereEvents()
	go network.processSelfieEvents()
	go network.processChauEvents()
	go network.processNewspaperEvents()
	go network.maintenanceBuzz()
}

func (network Network) meName() string {
	return network.local.Me.Name
}

func (network *Network) announce() {
	topic := fmt.Sprintf("%s/%s", "nara/newspaper", network.meName())
	network.recordObservationOnlineNara(network.meName())
	network.postEvent(topic, network.local.Me.Status)
}

func (network *Network) announceForever() {
	for {
		ts := network.local.chattinessRate(5, 60)
		logrus.Debugf("time between announces = %d", ts)
		time.Sleep(time.Duration(ts) * time.Second)

		network.announce()
	}
}

func (network *Network) processNewspaperEvents() {
	for {
		newspaperEvent := <-network.newspaperInbox
		logrus.Debugf("newspaperHandler update from %s", newspaperEvent.From)

		network.local.mu.Lock()
		nara, present := network.Neighbourhood[newspaperEvent.From]
		network.local.mu.Unlock()
		if !present {
			logrus.Printf("%s posted a newspaper story (whodis?)", newspaperEvent.From)
			nara = NewNara(newspaperEvent.From)
			if network.local.Me.Status.Chattiness > 0 {
				network.heyThere()
			}
		}
		nara.Status = newspaperEvent.Status
		network.importNara(nara)

		network.recordObservationOnlineNara(newspaperEvent.From)
	}
}

func (network *Network) importNara(nara *Nara) {
	network.local.mu.Lock()
	network.Neighbourhood[nara.Name] = nara
	network.local.mu.Unlock()
}

func (network *Network) processSelfieEvents() {
	for {
		nara := <-network.selfieInbox

		network.importNara(&nara)
		logrus.Debugf("%s just took a selfie", nara.Name)
		network.recordObservationOnlineNara(nara.Name)

		network.Buzz.increase(1)
	}
}

func (network *Network) processHeyThereEvents() {
	for {
		heyThere := <-network.heyThereInbox

		logrus.Printf("%s says: hey there!", heyThere.From)
		network.recordObservationOnlineNara(heyThere.From)

		network.selfie()
		network.Buzz.increase(1)
	}
}

func (network *Network) heyThere() {
	ts := int64(5) // seconds
	network.recordObservationOnlineNara(network.meName())
	if (time.Now().Unix() - network.LastHeyThere) <= ts {
		return
	}
	logrus.Debugf("time between hey there = %d", ts)
	network.LastHeyThere = time.Now().Unix()

	topic := "nara/plaza/hey_there"
	heyThere := &HeyThereEvent{From: network.meName()}
	network.postEvent(topic, heyThere)
	network.selfie()
	logrus.Printf("%s: 👋", heyThere.From)

	network.Buzz.increase(2)
}

func (network *Network) selfie() {
	ts := int64(5) // seconds
	network.recordObservationOnlineNara(network.meName())
	if (time.Now().Unix() - network.LastSelfie) <= ts {
		return
	}
	network.LastSelfie = time.Now().Unix()

	topic := "nara/selfies/" + network.meName()
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

		logrus.Printf("%s: chau!", nara.Name)
	}
	network.Buzz.increase(2)
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

func (network Network) oldestNaraBarrio() Nara {
	result := *network.local.Me
	oldest := int64(network.local.getMeObservation().StartTime)
	myClusterName := network.local.getMeObservation().ClusterName

	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	for name, nara := range network.Neighbourhood {
		// question: do we follow our opinion of their neighbourhood or their opinion?
		obs := network.local.getObservationLocked(name)
		if !obs.isOnline() {
			continue
		}
		if obs.ClusterName != myClusterName {
			continue
		}
		if oldest <= obs.StartTime && name > result.Name {
			continue
		}
		if obs.StartTime > 0 && obs.StartTime <= oldest {
			oldest = obs.StartTime
			result = *nara
		}
	}
	return result
}

func (network Network) oldestNara() Nara {
	result := *network.local.Me
	oldest := int64(network.local.getMeObservation().StartTime)

	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	for name, nara := range network.Neighbourhood {
		obs := network.local.getObservationLocked(name)
		if !obs.isOnline() {
			continue
		}
		if oldest <= obs.StartTime && name > result.Name {
			continue
		}
		if obs.StartTime > 0 && obs.StartTime <= oldest {
			oldest = obs.StartTime
			result = *nara
		}
	}
	return result
}

func (network Network) youngestNaraBarrio() Nara {
	result := *network.local.Me
	youngest := int64(network.local.getMeObservation().StartTime)
	myClusterName := network.local.getMeObservation().ClusterName

	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	for name, nara := range network.Neighbourhood {
		obs := network.local.getObservationLocked(name)
		if !obs.isOnline() {
			continue
		}
		if obs.ClusterName != myClusterName {
			continue
		}
		if youngest >= obs.StartTime && name < result.Name {
			continue
		}
		if obs.StartTime > 0 && obs.StartTime >= youngest {
			youngest = obs.StartTime
			result = *nara
		}
	}
	return result
}

func (network Network) youngestNara() Nara {
	result := *network.local.Me
	youngest := int64(network.local.getMeObservation().StartTime)

	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	for name, nara := range network.Neighbourhood {
		obs := network.local.getObservationLocked(name)
		if !obs.isOnline() {
			continue
		}
		if youngest >= obs.StartTime && name < result.Name {
			continue
		}
		if obs.StartTime > 0 && obs.StartTime >= youngest {
			youngest = obs.StartTime
			result = *nara
		}
	}
	return result
}

func (network Network) mostRestarts() Nara {
	result := *network.local.Me
	most_restarts := network.local.getMeObservation().Restarts
	for name, nara := range network.Neighbourhood {
		obs := network.local.getObservationLocked(name)
		if !obs.isOnline() {
			continue
		}
		if most_restarts >= obs.Restarts && name > result.Name {
			continue
		}
		if obs.Restarts > 0 && obs.Restarts >= most_restarts {
			most_restarts = obs.Restarts
			result = *nara
		}
	}
	return result
}

func (network *Network) anyNaraApiUrl() (string, error) {
	apiUrl := ""
	startTime := int64(0)

	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	for name, nara := range network.Neighbourhood {
		if nara.ApiUrl == "" {
			continue
		}
		observation := network.local.getObservationLocked(name)
		if startTime == 0 || observation.LastRestart < startTime {
			startTime = observation.LastRestart
			apiUrl = nara.ApiUrl
		}
	}

	if apiUrl == "" {
		return "", fmt.Errorf("no neighbour nara with api available")
	}

	return apiUrl, nil
}
