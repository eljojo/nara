package nara

import (
	"fmt"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
)

type Network struct {
	Neighbourhood    map[string]*Nara
	Buzz             *Buzz
	LastHeyThere     int64
	LastSelfie       int64
	skippingEvents   bool
	local            *LocalNara
	Mqtt             mqtt.Client
	heyThereInbox    chan HeyThereEvent
	newspaperInbox   chan NewspaperEvent
	chauInbox        chan Nara
	selfieInbox      chan Nara
	ReadOnly         bool
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
	network.heyThereInbox = make(chan HeyThereEvent)
	network.chauInbox = make(chan Nara)
	network.selfieInbox = make(chan Nara)
	network.newspaperInbox = make(chan NewspaperEvent)
	network.skippingEvents = false
	network.Buzz = newBuzz()
	network.Mqtt = initializeMQTT(network.mqttOnConnectHandler(), network.meName(), host, user, pass)
	return network
}

func (network *Network) Start(serveUI bool, httpAddr string) {
	if serveUI {
		err := network.startHttpServer(httpAddr)
		if err != nil {
			logrus.Panic(err)
		}
	}

	if token := network.Mqtt.Connect(); token.Wait() && token.Error() != nil {
		logrus.Fatalf("MQTT connection error: %v", token.Error())
	}

	if !network.ReadOnly {
		network.heyThere()
		network.announce()
	}

	time.Sleep(1 * time.Second)

	time.Sleep(4 * time.Second) // wait for others to announce
	go network.formOpinion()
	go network.observationMaintenance()
	if !network.ReadOnly {
		go network.announceForever()
	}
	go network.processHeyThereEvents()
	go network.processSelfieEvents()
	go network.processChauEvents()
	go network.processNewspaperEvents()
	go network.maintenanceBuzz()
}

func (network *Network) meName() string {
	return network.local.Me.Name
}

func (network *Network) announce() {
	if network.ReadOnly {
		return
	}
	topic := fmt.Sprintf("%s/%s", "nara/newspaper", network.meName())
	network.recordObservationOnlineNara(network.meName())
	network.postEvent(topic, network.local.Me.Status)
}

func (network *Network) announceForever() {
	for {
		ts := network.local.chattinessRate(5, 55)
		// logrus.Debugf("time between announces = %d", ts)
		time.Sleep(time.Duration(ts) * time.Second)

		network.announce()
	}
}

func (network *Network) processNewspaperEvents() {
	for {
		network.handleNewspaperEvent(<-network.newspaperInbox)
	}
}

func (network *Network) handleNewspaperEvent(event NewspaperEvent) {
	logrus.Debugf("newspaperHandler update from %s", event.From)

	network.local.mu.Lock()
	nara, present := network.Neighbourhood[event.From]
	network.local.mu.Unlock()
	if present {
		nara.mu.Lock()
		nara.Status.setValuesFrom(event.Status)
		nara.mu.Unlock()
	} else {
		logrus.Printf("%s posted a newspaper story (whodis?)", event.From)
		nara = NewNara(event.From)
		nara.Status.setValuesFrom(event.Status)
		if network.local.Me.Status.Chattiness > 0 && !network.ReadOnly {
			network.heyThere()
		}
		network.importNara(nara)
	}

	network.recordObservationOnlineNara(event.From)
}

func (network *Network) processSelfieEvents() {
	for {
		network.handleSelfieEvent(<-network.selfieInbox)
	}
}

func (network *Network) handleSelfieEvent(nara Nara) {
	network.importNara(&nara)
	logrus.Debugf("%s just took a selfie", nara.Name)
	network.recordObservationOnlineNara(nara.Name)

	network.Buzz.increase(1)
}

func (network *Network) processHeyThereEvents() {
	for {
		network.handleHeyThereEvent(<-network.heyThereInbox)
	}
}

func (network *Network) handleHeyThereEvent(heyThere HeyThereEvent) {
	logrus.Printf("%s says: hey there!", heyThere.From)
	network.recordObservationOnlineNara(heyThere.From)

	// artificially slow down so if two naras boot at the same time they both get the message
	if !network.ReadOnly {
		time.Sleep(1 * time.Second)
		network.selfie()
	}
	network.Buzz.increase(1)
}

func (network *Network) heyThere() {
	if network.ReadOnly {
		return
	}
	ts := int64(5) // seconds
	network.recordObservationOnlineNara(network.meName())
	if (time.Now().Unix() - network.LastHeyThere) <= ts {
		return
	}
	network.LastHeyThere = time.Now().Unix()

	topic := "nara/plaza/hey_there"
	heyThere := &HeyThereEvent{From: network.meName()}
	network.postEvent(topic, heyThere)
	network.selfie()
	logrus.Printf("%s: 👋", heyThere.From)

	network.Buzz.increase(2)
}

func (network *Network) selfie() {
	if network.ReadOnly {
		return
	}
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
		network.handleChauEvent(<-network.chauInbox)
	}
}

func (network *Network) handleChauEvent(nara Nara) {
	if nara.Name == network.meName() || nara.Name == "" {
		return
	}

	network.local.mu.Lock()
	existingNara, present := network.Neighbourhood[nara.Name]
	network.local.mu.Unlock()
	if present {
		existingNara.setValuesFrom(nara)
	}

	observation := network.local.getObservation(nara.Name)
	observation.Online = "OFFLINE"
	observation.LastSeen = time.Now().Unix()
	network.local.setObservation(nara.Name, observation)

	logrus.Printf("%s: chau!", nara.Name)
	network.Buzz.increase(2)
}

func (network *Network) Chau() {
	if network.ReadOnly {
		return
	}
	topic := "nara/plaza/chau"
	logrus.Printf("posting to %s", topic)

	observation := network.local.getMeObservation()
	observation.Online = "OFFLINE"
	observation.LastSeen = time.Now().Unix()
	network.local.setMeObservation(observation)

	network.postEvent(topic, network.local.Me)
}

func (network *Network) oldestNaraBarrio() Nara {
	result := *network.local.Me
	oldest := int64(network.local.getMeObservation().StartTime)
	myClusterName := network.local.getMeObservation().ClusterName
	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	for name, nara := range network.Neighbourhood {
		// question: do we follow our opinion of their neighbourhood or their opinion?
		obs := network.local.getObservationLocked(name)
		if obs.Online != "ONLINE" {
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

func (network *Network) oldestNara() Nara {
	result := *network.local.Me
	oldest := int64(network.local.getMeObservation().StartTime)

	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	for name, nara := range network.Neighbourhood {
		obs := network.local.getObservationLocked(name)
		if obs.Online != "ONLINE" {
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

func (network *Network) youngestNaraBarrio() Nara {
	result := *network.local.Me
	youngest := int64(network.local.getMeObservation().StartTime)
	myClusterName := network.local.getMeObservation().ClusterName

	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	for name, nara := range network.Neighbourhood {
		obs := network.local.getObservationLocked(name)
		if obs.Online != "ONLINE" {
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

func (network *Network) youngestNara() Nara {
	result := *network.local.Me
	youngest := int64(network.local.getMeObservation().StartTime)

	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	for name, nara := range network.Neighbourhood {
		obs := network.local.getObservationLocked(name)
		if obs.Online != "ONLINE" {
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

func (network *Network) mostRestarts() Nara {
	result := *network.local.Me
	most_restarts := network.local.getMeObservation().Restarts

	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	for name, nara := range network.Neighbourhood {
		obs := network.local.getObservationLocked(name)
		if obs.Online != "ONLINE" {
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

func (network *Network) NeighbourhoodNames() []string {
	var result []string
	network.local.mu.Lock()
	defer network.local.mu.Unlock()
	for _, nara := range network.Neighbourhood {
		result = append(result, nara.Name)
	}
	return result
}

func (network *Network) NeighbourhoodOnlineNames() []string {
	var result []string
	network.local.mu.Lock()
	defer network.local.mu.Unlock()
	for _, nara := range network.Neighbourhood {
		obs := network.local.getObservationLocked(nara.Name)
		if !obs.isOnline() {
			continue
		}
		result = append(result, nara.Name)
	}
	return result
}

func (network *Network) getNara(name string) Nara {
	network.local.mu.Lock()
	nara, present := network.Neighbourhood[name]
	network.local.mu.Unlock()
	if present {
		return *nara
	}
	return Nara{}
}

func (network *Network) importNara(nara *Nara) {
	nara.mu.Lock()
	defer nara.mu.Unlock()

	// deadlock prevention: ensure we always lock in the same order
	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	n, present := network.Neighbourhood[nara.Name]
	if present {
		n.setValuesFrom(*nara)
	} else {
		network.Neighbourhood[nara.Name] = nara
	}
}
