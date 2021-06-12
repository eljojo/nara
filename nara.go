package nara

import (
	"fmt"
	//	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/shirou/gopsutil/host"
	"github.com/sirupsen/logrus"

	"os"
	"os/signal"
	"sync"
	"syscall"
)

type LocalNara struct {
	Me              *Nara
	Network         *Network
	forceChattiness int
	mu              sync.Mutex
}

type Nara struct {
	Name      string
	Hostname  string
	Ip        string
	Status    NaraStatus
	PingStats map[string]float64
	mu        sync.Mutex
}

type NaraStatus struct {
	Barrio       string
	HostStats    HostStats
	Chattiness   int64
	Observations map[string]NaraObservation
}

type NaraObservation struct {
	Online      string
	StartTime   int64
	Restarts    int64
	LastSeen    int64
	LastRestart int64
	ClusterName string
}

func NewLocalNara(name string, mqtt_host string, mqtt_user string, mqtt_pass string, forceChattiness int) *LocalNara {
	ln := &LocalNara{
		Me:              NewNara(name),
		forceChattiness: forceChattiness,
	}
	ln.Network = NewNetwork(ln, mqtt_host, mqtt_user, mqtt_pass)

	ln.updateHostStats()

	ip, err := externalIP()
	if err == nil {
		ln.Me.Ip = ip
		logrus.Println("local ip", ip)
	} else {
		logrus.Panic(err)
	}

	hostinfo, _ := host.Info()
	ln.Me.Hostname = hostinfo.Hostname

	return ln
}

func NewNara(name string) *Nara {
	nara := &Nara{Name: name}
	nara.PingStats = make(map[string]float64)
	nara.Status.Observations = make(map[string]NaraObservation)
	return nara
}

func (ln *LocalNara) Start() {
	go ln.updateHostStatsForever()
	ln.Network.Start()
	go ln.measurePingForever()
}

func (ln *LocalNara) SetupCloseHandler() {
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		fmt.Println("babaayyy")
		ln.Network.Chau()
		os.Exit(0)
	}()
}

func (localNara *LocalNara) getMeObservation() NaraObservation {
	return localNara.getObservation(localNara.Me.Name)
}

func (localNara *LocalNara) setMeObservation(observation NaraObservation) {
	localNara.setObservation(localNara.Me.Name, observation)
}

func (localNara *LocalNara) getObservation(name string) NaraObservation {
	return localNara.Me.getObservation(name)
}

func (localNara *LocalNara) setObservation(name string, observation NaraObservation) {
	localNara.Me.setObservation(name, observation)
}

func (nara *Nara) getObservation(name string) NaraObservation {
	observation, _ := nara.Status.Observations[name]
	return observation
}

func (nara *Nara) setObservation(name string, observation NaraObservation) {
	nara.Status.Observations[name] = observation
}

func (nara *Nara) chattinessRate(min int64, max int64) int64 {
	return min + ((max - min) * (100 - nara.Status.Chattiness) / 100)
}
