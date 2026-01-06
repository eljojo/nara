package nara

import (
	"fmt"

	"github.com/shirou/gopsutil/v3/host"
	"github.com/sirupsen/logrus"

	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

const NaraVersion = "0.2.0"

type LocalNara struct {
	Me              *Nara
	Network         *Network
	Soul            string
	SocialLedger    *SocialLedger
	forceChattiness int
	isRaspberryPi   bool
	isNixOs         bool
	isKubernetes    bool
	mu              sync.Mutex
}

type Nara struct {
	Name     string
	Hostname string `json:"-"`
	Version  string
	Status   NaraStatus
	mu       sync.Mutex
	// remember to sync with setValuesFrom
}

type NaraPersonality struct {
	Agreeableness int // 0-100, high = more likely to join a clique
	Sociability   int // 0-100, high = more likely to start a clique
	Chill         int // 0-100, high = less likely to leave a clique
}

type NaraStatus struct {
	LicensePlate string
	Flair        string
	HostStats    HostStats
	Chattiness   int64
	Buzz         int
	Observations map[string]NaraObservation
	Trend        string
	TrendEmoji   string
	Personality  NaraPersonality
	Soul         string
	Version      string
	PublicUrl    string
	// remember to sync with setValuesFrom
}

func NewLocalNara(name string, soul string, mqtt_host string, mqtt_user string, mqtt_pass string, forceChattiness int) *LocalNara {
	logrus.Printf("📟 Booting nara: %s", name)

	ln := &LocalNara{
		Me:              NewNara(name),
		Soul:            soul,
		forceChattiness: forceChattiness,
		isRaspberryPi:   isRaspberryPi(),
		isNixOs:         isNixOs(),
		isKubernetes:    isKubernetes(),
	}
	ln.Me.Version = NaraVersion
	ln.Me.Status.Version = NaraVersion
	ln.Me.Status.Soul = soul

	ln.Network = NewNetwork(ln, mqtt_host, mqtt_user, mqtt_pass)

	ln.seedPersonality()

	// Initialize social ledger with personality (max 30,000 events ~= 10MB)
	ln.SocialLedger = NewSocialLedger(ln.Me.Status.Personality, 30000)

	ln.updateHostStats()

	hostinfo, _ := host.Info()
	ln.Me.Hostname = hostinfo.Hostname

	observation := ln.getMeObservation()
	observation.LastRestart = time.Now().Unix()
	ln.setMeObservation(observation)

	return ln
}

func NewNara(name string) *Nara {
	nara := &Nara{Name: name}
	nara.Status.Observations = make(map[string]NaraObservation)
	return nara
}

func (ln *LocalNara) Start(serveUI bool, readOnly bool, httpAddr string) {
	ln.Network.ReadOnly = readOnly
	if serveUI {
		logrus.Printf("💻 Serving UI")
	}

	go ln.updateHostStatsForever()
	ln.Network.Start(serveUI, httpAddr)

	if readOnly {
		logrus.Printf("🤫 Read-only mode: not pinging or announcing")
	}
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

func (ln *LocalNara) chattinessRate(min int64, max int64) int64 {
	return min + ((max - min) * (100 - ln.Me.Status.Chattiness) / 100)
}

func (ln *LocalNara) uptime() int64 {
	me := ln.getMeObservation()
	return me.LastSeen - me.LastRestart
}

func (ln *LocalNara) isBooting() bool {
	return ln.uptime() < 120
}

func (nara *Nara) setValuesFrom(other Nara) {
	nara.mu.Lock() // this protects Status
	defer nara.mu.Unlock()

	if other.Name == "" || other.Name != nara.Name {
		logrus.Printf("warning: fed incorrect Nara to setValuesFrom: %v", other)
		return
	}

	if other.Version != "" {
		nara.Version = other.Version
	}
	nara.Status.setValuesFrom(other.Status)
}

func (ns *NaraStatus) setValuesFrom(other NaraStatus) {
	ns.LicensePlate = other.LicensePlate
	if other.Flair != "" {
		ns.Flair = other.Flair
	}
	if (other.HostStats != HostStats{}) {
		ns.HostStats = other.HostStats
	}
	if other.Version != "" {
		ns.Version = other.Version
	}
	ns.Trend = other.Trend
	ns.TrendEmoji = other.TrendEmoji
	ns.Personality = other.Personality
	ns.Soul = other.Soul
	if other.LicensePlate != "" {
		ns.LicensePlate = other.LicensePlate
	}
	ns.Chattiness = other.Chattiness
	ns.Buzz = other.Buzz
	if other.Observations != nil {
		for name, nara := range other.Observations {
			ns.Observations[name] = nara
		}
	}
	if other.PublicUrl != "" {
		ns.PublicUrl = other.PublicUrl
	}
}
