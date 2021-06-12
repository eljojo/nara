package nara

import (
	"fmt"
	"github.com/go-ping/ping"
	"github.com/pbnjay/clustering"
	"github.com/sirupsen/logrus"
	"time"
)

func (nara *Nara) getPing(name string) float64 {
	ping, _ := nara.Status.PingStats[name]
	return ping
}

func (nara *Nara) setPing(name string, ping float64) {
	nara.Status.PingStats[name] = ping
}

func (nara *Nara) forgetPing(name string) {
	_, present := nara.Status.PingStats[name]
	if present {
		delete(nara.Status.PingStats, name)
	}
}

func (nara *Nara) pingMap() map[clustering.ClusterItem]float64 {
	pingMap := make(map[clustering.ClusterItem]float64)
	for otherNara, ping := range nara.Status.PingStats {
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
			if ln.Me.getObservation(nara.Name).Online != "ONLINE" {
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
	if err == nil && ping > 0 {
		ln.Me.setPing(name, ping)
	} else {
		// logrus.Println("problem when pinging", dest, err)
		ln.Me.forgetPing(name)
	}
}

func measurePing(name string, dest string) (float64, error) {
	logrus.Debug("pinging", name, dest)
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
	ping, present := nara.Status.PingStats[other.Name]
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
