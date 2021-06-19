package nara

import (
	"encoding/json"
	"fmt"
	"github.com/shirou/gopsutil/host"
	"github.com/sirupsen/logrus"

	"os"
	"os/signal"
	"sync"
	"syscall"

	"io/ioutil"
	"net/http"
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
	ApiUrl    string
	IRL       IRL
	Status    NaraStatus
	PingStats map[string]float64
	mu        sync.Mutex
}

type NaraStatus struct {
	LicensePlate string
	Flair        string
	HostStats    HostStats
	Chattiness   int64
	Buzz         int
	Observations map[string]NaraObservation
}

func NewLocalNara(name string, mqtt_host string, mqtt_user string, mqtt_pass string, forceChattiness int) *LocalNara {
	logrus.Printf("booting nara %s", name)

	ln := &LocalNara{
		Me:              NewNara(name),
		forceChattiness: forceChattiness,
	}
	ln.Network = NewNetwork(ln, mqtt_host, mqtt_user, mqtt_pass)

	ln.updateHostStats()

	irl, err := fetchIRL()
	if (err == nil && irl != IRL{}) {
		ln.Me.IRL = irl
		logrus.Printf("hello from %s, %s", irl.City, irl.CountryName)
	} else {
		logrus.Panic("couldn't find IRL data", err)
	}

	ip, err := externalIP()
	if err == nil {
		ln.Me.Ip = ip
		logrus.Println("internal ip", ip)
	} else {
		logrus.Panic("couldn't find internal IP", err)
	}

	hostinfo, _ := host.Info()
	ln.Me.Hostname = hostinfo.Hostname

	previousStatus, err := fetchStatusFromApi(name)
	if err != nil {
		logrus.Debugf("failed to fetch status from API: %v", err)
	} else {
		logrus.Print("fetched last status from nara-web API")
		logrus.Debugf("%v", previousStatus)
		ln.Me.Status = previousStatus
	}

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

func (ln LocalNara) chattinessRate(min int64, max int64) int64 {
	return min + ((max - min) * (100 - ln.Me.Status.Chattiness) / 100)
}

func (ln LocalNara) uptime() int64 {
	me := ln.getMeObservation()
	return me.LastSeen - me.LastRestart
}

func (ln LocalNara) isBooting() bool {
	return ln.uptime() < 120
}

func fetchStatusFromApi(name string) (NaraStatus, error) {
	status := &NaraStatus{}

	logrus.Debugf("fetching status from API for %s", name)
	url := fmt.Sprintf("https://nara.eljojo.net/status/%s.json", name)
	resp, err := http.Get(url)

	if err != nil {
		return *status, err
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		return *status, err
	}

	err = json.Unmarshal(body, status)
	if err != nil {
		return *status, err
	}

	return *status, nil
}
