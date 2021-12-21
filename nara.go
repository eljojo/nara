package nara

import (
	"fmt"

	"github.com/shirou/gopsutil/host"
	"github.com/sirupsen/logrus"

	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type LocalNara struct {
	Me              *Nara
	Network         *Network
	forceChattiness int
	isRaspberryPi   bool
	isNixOs         bool
	isKubernetes    bool
	mu              sync.Mutex
}

type Nara struct {
	Name      string
	Hostname  string
	Ip        string
	ApiUrl    string
	IRL       IRL
	Status    NaraStatus
	pingStats map[string]float64
	mu        sync.Mutex
	// remember to sync with setValuesFrom
}

type NaraStatus struct {
	LicensePlate string
	Flair        string
	HostStats    HostStats
	Chattiness   int64
	Buzz         int
	Observations map[string]NaraObservation
	// remember to sync with setValuesFrom
}

func NewLocalNara(name string, mqtt_host string, mqtt_user string, mqtt_pass string, forceChattiness int) *LocalNara {
	logrus.Printf("📟 Booting nara: %s", name)

	ln := &LocalNara{
		Me:              NewNara(name),
		forceChattiness: forceChattiness,
		isRaspberryPi:   isRaspberryPi(),
		isNixOs:         isNixOs(),
		isKubernetes:    isKubernetes(),
	}
	ln.Network = NewNetwork(ln, mqtt_host, mqtt_user, mqtt_pass)

	ln.updateHostStats()

	irl, err := fetchIRL()
	if (err == nil && irl != IRL{}) {
		ln.Me.IRL = irl
		logrus.Printf("🪐 Hello from %s, %s", irl.City, irl.CountryName)
	} else {
		logrus.Panic("couldn't find IRL data", err)
	}

	ip, err := externalIP()
	if err == nil {
		ln.Me.Ip = ip
	} else {
		logrus.Panic("couldn't find internal IP", err)
	}

	hostinfo, _ := host.Info()
	ln.Me.Hostname = hostinfo.Hostname

	previousStatus, err := fetchStatusFromApi(name)
	if err != nil { // lol
		previousStatus, err = fetchStatusFromApi(name)
	}
	if err != nil { // lol
		previousStatus, err = fetchStatusFromApi(name)
	}
	if err != nil {
		logrus.Debugf("failed to fetch status from API: %v", err)
	} else {
		logrus.Print("fetched last status from nara-web API")
		// logrus.Debugf("%v", previousStatus)
		ln.Me.Status.setValuesFrom(previousStatus)
	}

	observation := ln.getMeObservation()
	observation.LastRestart = time.Now().Unix()
	ln.setMeObservation(observation)

	return ln
}

func NewNara(name string) *Nara {
	nara := &Nara{Name: name}
	nara.pingStats = make(map[string]float64)
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

		ln.Network.disconnectMQTT()

		time.Sleep(50 * time.Millisecond) // sleep a bit to ensure message is sent
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
	err := httpFetchJson(url, status)
	if err != nil {
		return *status, fmt.Errorf("failed to get status from api: %w", err)
	}

	return *status, nil
}

func (nara *Nara) setValuesFrom(other Nara) {
	nara.mu.Lock()
	defer nara.mu.Unlock()

	if other.Name == "" || other.Name != nara.Name {
		logrus.Printf("warning: fed incorrect Nara to setValuesFrom: %v", other)
		return
	}

	if other.Hostname != "" {
		nara.Hostname = other.Hostname
	}
	if other.Ip != "" {
		nara.Ip = other.Ip
	}
	if other.ApiUrl != "" {
		nara.ApiUrl = other.ApiUrl
	}
	if (other.IRL != IRL{}) {
		nara.IRL = other.IRL
	}
	nara.Status.setValuesFrom(other.Status)
}

func (ns *NaraStatus) setValuesFrom(other NaraStatus) {
	if other.LicensePlate != "" {
		ns.LicensePlate = other.LicensePlate
	}
	if other.Flair != "" {
		ns.Flair = other.Flair
	}
	if (other.HostStats != HostStats{}) {
		ns.HostStats = other.HostStats
	}
	ns.Chattiness = other.Chattiness
	ns.Buzz = other.Buzz
	if other.Observations != nil {
		for name, nara := range other.Observations {
			ns.Observations[name] = nara
		}
	}
}

func (nara Nara) ApiGatewayUrl() string {
	return fmt.Sprintf("https://%s.nara.network", nara.Name)
}

func (nara Nara) BestApiUrl() string {
	if nara.ApiUrl != "" {
		return nara.ApiUrl
	}
	return nara.ApiGatewayUrl()
}
