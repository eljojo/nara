package nara

import (
	"fmt"
	//	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/shirou/gopsutil/host"
	"github.com/sirupsen/logrus"

	"os"
	"os/signal"
	"syscall"
)

type LocalNara struct {
	Me              *Nara
	Network         *Network
	forceChattiness int
}

type Nara struct {
	Name     string
	Hostname string
	Ip       string
	Status   NaraStatus
}

type NaraStatus struct {
	PingStats    map[string]float64
	Barrio       string
	HostStats    HostStats
	Chattiness   int64
	Observations map[string]NaraObservation
}

type HostStats struct {
	Uptime  uint64
	LoadAvg float64
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
	nara.Status.PingStats = make(map[string]float64)
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
