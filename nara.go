package main

import (
	"flag"
	"fmt"
	"github.com/bugsnag/bugsnag-go"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/shirou/gopsutil/host"
	"github.com/sirupsen/logrus"
	"math/rand"
	// "strconv"
	"time"

	"os"
	"os/signal"
	"runtime"
	"syscall"
)

type LocalNara struct {
	Me              *Nara
	Network         *Network
	Mqtt            mqtt.Client
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

func main() {
	rand.Seed(time.Now().UnixNano())

	bugsnag.Configure(bugsnag.Configuration{
		APIKey:          "0bd8e595fccf5f1befe9151c3a32ea61",
		ProjectPackages: []string{"main"},
	})

	mqttHostPtr := flag.String("mqtt-host", "tcp://hass.eljojo.casa:1883", "mqtt server hostname")
	mqttUserPtr := flag.String("mqtt-user", "my_username", "mqtt server username")
	mqttPassPtr := flag.String("mqtt-pass", "my_password", "mqtt server password")
	naraIdPtr := flag.String("nara-id", "raspberry", "nara id")
	showNeighboursPtr := flag.Bool("show-neighbours", true, "show table with neighbourhood")
	showNeighboursSpeedPtr := flag.Int("refresh-rate", 60, "refresh rate in seconds for neighbourhood table")
	forceChattinessPtr := flag.Int("force-chattiness", -1, "specific chattiness to force, -1 for auto (default)")
	verbosePtr := flag.Bool("verbose", false, "log debug stuff")

	flag.Parse()
	localNara := NewLocalNara(*naraIdPtr, *mqttHostPtr, *mqttUserPtr, *mqttPassPtr, *forceChattinessPtr)

	go localNara.announceForever()
	go localNara.measurePingForever()
	go localNara.updateHostStatsForever()
	go localNara.formOpinion()
	go localNara.observationMaintenance()
	if *showNeighboursPtr {
		go localNara.printNeigbourhoodForever(*showNeighboursSpeedPtr)
	}

	if *verbosePtr {
		logrus.SetLevel(logrus.DebugLevel)
	}

	localNara.SetupCloseHandler()
	defer localNara.chau()

	// sleep forever while goroutines do their thing
	for {
		time.Sleep(10 * time.Millisecond)
		runtime.Gosched() // https://blog.container-solutions.com/surprise-golang-thread-scheduling
	}
}

func NewLocalNara(name string, mqtt_host string, mqtt_user string, mqtt_pass string, forceChattiness int) *LocalNara {
	ln := &LocalNara{
		Me:              NewNara(name),
		Network:         NewNetwork(),
		forceChattiness: forceChattiness,
	}

	ln.Mqtt = ln.initializeMQTT(mqtt_host, mqtt_user, mqtt_pass)

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

	if token := ln.Mqtt.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}
	return ln
}

func NewNara(name string) *Nara {
	nara := &Nara{Name: name}
	nara.Status.PingStats = make(map[string]float64)
	nara.Status.Observations = make(map[string]NaraObservation)
	return nara
}

func (ln *LocalNara) SetupCloseHandler() {
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		fmt.Println("babaayyy")
		ln.chau()
		os.Exit(0)
	}()
}
