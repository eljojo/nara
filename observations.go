package nara

import (
	"time"

	"github.com/sirupsen/logrus"
)

type NaraObservation struct {
	Online       string
	StartTime    int64
	Restarts     int64
	LastSeen     int64
	LastRestart  int64
	ClusterName  string
	ClusterEmoji string
}

func (localNara *LocalNara) getMeObservation() NaraObservation {
	return localNara.getObservation(localNara.Me.Name)
}

func (localNara *LocalNara) setMeObservation(observation NaraObservation) {
	localNara.setObservation(localNara.Me.Name, observation)
}

func (localNara *LocalNara) getObservation(name string) NaraObservation {
	observation := localNara.Me.getObservation(name)
	return observation
}

func (localNara *LocalNara) getObservationLocked(name string) NaraObservation {
	// this is called when localNara.mu is already held
	// but we still need to lock localNara.Me.mu
	localNara.Me.mu.Lock()
	observation, _ := localNara.Me.Status.Observations[name]
	localNara.Me.mu.Unlock()
	return observation
}

func (localNara *LocalNara) setObservation(name string, observation NaraObservation) {
	localNara.mu.Lock()
	localNara.Me.setObservation(name, observation)
	localNara.mu.Unlock()
}

func (nara *Nara) getObservation(name string) NaraObservation {
	nara.mu.Lock()
	observation, _ := nara.Status.Observations[name]
	nara.mu.Unlock()
	return observation
}

func (nara *Nara) setObservation(name string, observation NaraObservation) {
	nara.mu.Lock()
	nara.Status.Observations[name] = observation
	nara.mu.Unlock()
}

func (network *Network) formOpinion() {
	time.Sleep(5 * time.Second)
	logrus.Printf("🕵️  forming opinions...")

	names := network.NeighbourhoodNames()

	for _, name := range names {
		observation := network.local.getObservation(name)
		startTime := network.findStartingTimeFromNeighbourhoodForNara(name)
		if startTime > 0 {
			observation.StartTime = startTime
		} else {
			logrus.Printf("couldn't adjust startTime for %s based on neighbour disagreement", name)
		}
		restarts := network.findRestartCountFromNeighbourhoodForNara(name)
		if restarts > 0 {
			observation.Restarts = restarts
		} else {
			logrus.Printf("couldn't adjust restart count for %s based on neighbour disagreement", name)
		}
		if name != network.meName() {
			lastRestart := network.findLastRestartFromNeighbourhoodForNara(name)
			if lastRestart > 0 {
				observation.LastRestart = lastRestart
			} else {
				logrus.Printf("couldn't adjust last restart date for %s based on neighbour disagreement", name)
			}
		}
		network.local.setObservation(name, observation)
	}
	logrus.Printf("👀  opinions formed")
}

func (network *Network) findStartingTimeFromNeighbourhoodForNara(name string) int64 {
	times := make(map[int64]int)
	network.local.mu.Lock()
	defer network.local.mu.Unlock()
	for _, nara := range network.Neighbourhood {
		observed_start_time := nara.getObservation(name).StartTime
		if observed_start_time > 0 {
			times[observed_start_time] += 1
		}
	}

	var startTime int64
	maxSeen := 0
	one_fourth := len(times) / 4

	for time, count := range times {
		if count > maxSeen && count > one_fourth {
			maxSeen = count
			startTime = time
		}
	}

	return startTime
}

func (network *Network) recordObservationOnlineNara(name string) {
	observation := network.local.getObservation(name)

	// "our" observation is mostly a mirror of what others think of us
	if observation.StartTime == 0 || name == network.meName() {
		if name != network.meName() {
			logrus.Printf("observation: seen %s for the first time", name)
			network.Buzz.increase(3)
		}

		restarts := network.findRestartCountFromNeighbourhoodForNara(name)
		startTime := network.findStartingTimeFromNeighbourhoodForNara(name)
		lastRestart := network.findLastRestartFromNeighbourhoodForNara(name)

		if restarts > 0 {
			observation.Restarts = restarts
		}
		if startTime > 0 {
			observation.StartTime = startTime
		}
		if lastRestart > 0 && name != network.meName() {
			observation.LastRestart = lastRestart
		}

		if observation.StartTime == 0 && name == network.meName() && !network.local.isBooting() {
			observation.StartTime = time.Now().Unix()
			logrus.Printf("⚠️ set StartTime to now")
		}
	}

	if !observation.isOnline() && observation.Online != "" {
		observation.LastRestart = time.Now().Unix()
		observation.Restarts = observation.Restarts + 1
		logrus.Printf("observation: %s came back online", name)
		network.Buzz.increase(3)
	}

	observation.Online = "ONLINE"
	observation.LastSeen = time.Now().Unix()
	network.local.setObservation(name, observation)
}

func (network *Network) findRestartCountFromNeighbourhoodForNara(name string) int64 {
	network.local.mu.Lock()
	defer network.local.mu.Unlock()
	var maxSeen int64 = 0

	for _, nara := range network.Neighbourhood {
		restarts := nara.getObservation(name).Restarts
		if restarts > maxSeen {
			maxSeen = restarts
		}
	}

	return maxSeen
}

func (network *Network) findLastRestartFromNeighbourhoodForNara(name string) int64 {
	values := make(map[int64]int)
	network.local.mu.Lock()
	defer network.local.mu.Unlock()
	for _, nara := range network.Neighbourhood {
		last_restart := nara.getObservation(name).LastRestart
		if last_restart > 0 {
			values[last_restart] += 1
		}
	}

	var result int64
	maxSeen := 0
	one_fourth := len(values) / 4

	for last_restart, count := range values {
		if count > maxSeen && count >= one_fourth {
			maxSeen = count
			result = last_restart
		}
	}

	return result
}

func (network *Network) observationMaintenance() {
	for {
		network.local.Me.mu.Lock()
		observations := make(map[string]NaraObservation)
		for name, obs := range network.local.Me.Status.Observations {
			observations[name] = obs
		}
		network.local.Me.mu.Unlock()

		now := time.Now().Unix()

		for name, observation := range observations {
			// only do maintenance on naras that are online
			if !observation.isOnline() {
				if observation.ClusterName != "" {
					// reset cluster for offline naras
					observation.ClusterName = ""
					observation.ClusterEmoji = ""
					network.local.setObservation(name, observation)
				}

				continue
			}

			// mark missing after 100 seconds of no updates
			if (now-observation.LastSeen) > 100 && !network.skippingEvents && !network.local.isBooting() {
				observation.Online = "MISSING"
				network.local.setObservation(name, observation)
				logrus.Printf("observation: %s has disappeared", name)
				network.Buzz.increase(10)
			}
		}

		network.neighbourhoodMaintenance()

		// set own Flair
		newFlair := network.local.Flair()
		if newFlair != network.local.Me.Status.Flair {
			network.Buzz.increase(2)
		}
		network.local.Me.Status.Flair = newFlair
		network.local.Me.Status.LicensePlate = network.local.LicensePlate()

		time.Sleep(1 * time.Second)
	}
}

func (obs NaraObservation) isOnline() bool {
	return obs.Online == "ONLINE"
}
