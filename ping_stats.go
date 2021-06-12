package nara

import (
	"fmt"
	"github.com/go-ping/ping"
	"github.com/sirupsen/logrus"
	"time"
)

func (ln *LocalNara) measurePingForever() {
	for {
		ln.measureAndStorePing("google", "8.8.8.8")

		for name, nara := range ln.Network.Neighbourhood {
			if ln.Me.Status.Observations[nara.Name].Online != "ONLINE" {
				delete(ln.Me.Status.PingStats, name)
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
		ln.Me.Status.PingStats[name] = ping
	} else {
		// logrus.Println("problem when pinging", dest, err)
		delete(ln.Me.Status.PingStats, name)
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

func pingBetween(a Nara, b Nara) float64 {
	a_ping, a_ping_present := a.Status.PingStats[b.Name]
	b_ping, b_ping_present := b.Status.PingStats[a.Name]
	if a_ping_present && b_ping_present {
		return (a_ping + b_ping) / 2
	} else if a_ping_present {
		return a_ping
	}
	return b_ping
}

func pingBetweenMs(a Nara, b Nara) string {
	ping := pingBetween(a, b)
	if ping == 0 {
		return ""
	}
	return fmt.Sprintf("%.2fms", ping)
}
