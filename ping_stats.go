package nara

import (
	"fmt"
	"github.com/go-ping/ping"
	"github.com/pbnjay/clustering"
	"github.com/sirupsen/logrus"
	"time"
)

func (nara Nara) getPing(name string) float64 {
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
		nara.mu.Lock()
		nara.PingStats[name] = ping
		nara.mu.Unlock()
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
		network.local.mu.Lock()
		nara, present := network.Neighbourhood[pingEvent.From]
		network.local.mu.Unlock()
		if present {
			nara.setPing(pingEvent.To, pingEvent.TimeMs)
			network.recordObservationOnlineNara(pingEvent.From)
		}
	}
}

func (nara Nara) pingMap() map[clustering.ClusterItem]float64 {
	pingMap := make(map[clustering.ClusterItem]float64)
	for otherNara, ping := range nara.PingStats {
		if otherNara == "google" || ping == 0 {
			continue
		}
		pingMap[otherNara] = ping
	}
	return pingMap
}

func (ln *LocalNara) measurePingForever() {
	for {
		ts := int64(0)
		logrus.Debugf("time between pings = %d", ts)

		ln.measureAndStorePing("google", "8.8.8.8")
		time.Sleep(time.Duration(ts) * time.Second)

		for name, nara := range ln.Network.Neighbourhood {
			if nara.Ip == "" {
				continue
			}

			if !ln.getObservation(nara.Name).isOnline() {
				continue
			}

			ln.measureAndStorePing(name, nara.Ip)

			ts = ln.chattinessRate(0, 60)
			time.Sleep(time.Duration(ts) * time.Second)
		}
	}
}

func (ln *LocalNara) measureAndStorePing(name string, dest string) {
	ping, err := measurePing(name, dest)
	if err == nil && ping > 0 {
		pingEvent := PingEvent{From: ln.Me.Name, To: name, TimeMs: ping}
		ln.Network.postPing(pingEvent)
	} else {
		time.Sleep(1 * time.Second)
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

func (nara Nara) pingTo(other Nara) (float64, bool) {
	ping, present := nara.PingStats[other.Name]
	return ping, present
}

func (nara Nara) pingBetween(other Nara) float64 {
	our_ping, has_our_ping := nara.pingTo(other)
	other_ping, has_other_ping := other.pingTo(nara)

	if has_our_ping && has_other_ping {
		return (our_ping + other_ping) / 2
	} else if has_our_ping {
		return our_ping
	}
	return other_ping
}

func (nara Nara) pingBetweenMs(other Nara) string {
	ping := nara.pingBetween(other)
	if ping == 0 {
		return ""
	}
	return fmt.Sprintf("%.2fms", ping)
}

func (network *Network) pingEvents() []PingEvent {
	var result []PingEvent

	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	for name, nara := range network.Neighbourhood {
		obs := network.local.getObservationLocked(name)
		if !obs.isOnline() {
			continue
		}
		nara.mu.Lock()
		for other, timeMs := range nara.PingStats {
			pingEvent := PingEvent{From: name, To: other, TimeMs: timeMs}
			result = append(result, pingEvent)
		}
		nara.mu.Unlock()
	}

	return result
}

func (network *Network) fetchPingEventsFromNeighbouringNara() error {
	var events []PingEvent

	naraApi, err := network.anyNaraApiUrl()
	if err != nil {
		return fmt.Errorf("failed to get ping events from nara api: %w", err)
	}

	url := fmt.Sprintf("%s/ping_events", naraApi)
	logrus.Debugf("fetching ping events from %s", url)

	err = httpFetchJson(url, &events)
	if err != nil {
		return fmt.Errorf("failed to get ping events from nara api: %w", err)
	}

	logrus.Debugf("found %d ping events from API", len(events))

	for _, pingEvent := range events {
		network.pingInbox <- pingEvent
	}

	return nil
}
