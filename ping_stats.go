package nara

import (
	"fmt"
	"github.com/go-ping/ping"
	"github.com/pbnjay/clustering"
	"github.com/sirupsen/logrus"
	"time"
)

func (nara *Nara) getPing(name string) float64 {
	ping, _ := nara.PingStats[name]
	return ping
}

type PingEvent struct {
	From   string
	To     string
	TimeMs float64
}

func (nara *Nara) setPing(name string, ping float64) {
	if ping > 0 {
		nara.PingStats[name] = ping
	} else {
		nara.forgetPing(name)
	}
}

func (nara *Nara) forgetPing(name string) {
	_, present := nara.PingStats[name]
	if present {
		delete(nara.PingStats, name)
	}
}

func (network *Network) processPingEvents() {
	for {
		pingEvent := <-network.pingInbox
		logrus.Debugf("ping from %s to %s is %.2fms", pingEvent.From, pingEvent.To, pingEvent.TimeMs)
		network.storePingEvent(pingEvent)
	}
}

func (network *Network) storePingEvent(pingEvent PingEvent) {
	if pingEvent.From == network.meName() {
		network.local.Me.setPing(pingEvent.To, pingEvent.TimeMs)
	} else {
		neighbour, present := network.Neighbourhood[pingEvent.From]
		if present {
			neighbour.setPing(pingEvent.To, pingEvent.TimeMs)
		}
	}
}

func (nara *Nara) pingMap() map[clustering.ClusterItem]float64 {
	pingMap := make(map[clustering.ClusterItem]float64)
	for otherNara, ping := range nara.PingStats {
		if otherNara == "google" {
			continue
		}
		pingMap[otherNara] = ping
	}
	return pingMap
}

func (ln *LocalNara) measurePingForever() {
	for {
		ln.measureAndStorePing("google", "8.8.8.8")

		for name, nara := range ln.Network.Neighbourhood {
			if ln.getObservation(nara.Name).Online != "ONLINE" {
				ln.Me.forgetPing(name)
				continue
			}

			ln.measureAndStorePing(name, nara.Ip)
		}
		ts := ln.Me.chattinessRate(30, 120)
		time.Sleep(time.Duration(ts) * time.Second)
	}
}

func (ln *LocalNara) measureAndStorePing(name string, dest string) {
	ping, err := measurePing(name, dest)
	pingEvent := PingEvent{From: ln.Me.Name, To: name, TimeMs: ping}
	ln.Network.postPing(pingEvent)
	if err == nil && ping > 0 {
		// ln.Me.setPing(name, ping)
		// testing: pingHandler should receive the event sent above
	} else {
		// logrus.Println("problem when pinging", dest, err)
		ln.Me.forgetPing(name) // likely not necessary
	}
}

func measurePing(name string, dest string) (float64, error) {
	logrus.Debugf("pinging %s (%s)", name, dest)
	pinger, err := ping.NewPinger(dest)
	if err != nil {
		return 0, err
	}
	pinger.Count = 5
	pinger.Timeout = time.Second * 10
	pinger.SetPrivileged(true)
	err = pinger.Run() // blocks until finished
	if err != nil {
		return 0, err
	}
	stats := pinger.Statistics() // get send/receive/rtt stats
	return float64(stats.AvgRtt/time.Microsecond) / 1000, nil
}

func (nara *Nara) pingTo(other Nara) (float64, bool) {
	ping, present := nara.PingStats[other.Name]
	return ping, present
}

func (nara *Nara) pingBetween(other Nara) float64 {
	our_ping, has_our_ping := nara.pingTo(other)
	other_ping, has_other_ping := other.pingTo(*nara)

	if has_our_ping && has_other_ping {
		return (our_ping + other_ping) / 2
	} else if has_our_ping {
		return our_ping
	}
	return other_ping
}

func (nara *Nara) pingBetweenMs(other Nara) string {
	ping := nara.pingBetween(other)
	if ping == 0 {
		return ""
	}
	return fmt.Sprintf("%.2fms", ping)
}
