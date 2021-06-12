package nara

import (
	"github.com/sirupsen/logrus"
	"time"
)

func (localNara *LocalNara) getMeObservation() NaraObservation {
	return localNara.getObservation(localNara.Me.Name)
}

func (localNara *LocalNara) setMeObservation(observation NaraObservation) {
	localNara.setObservation(localNara.Me.Name, observation)
}

func (localNara *LocalNara) getObservation(name string) NaraObservation {
	return localNara.Me.getObservation(name)
}

func (localNara *LocalNara) setObservation(name string, observation NaraObservation) {
	localNara.Me.setObservation(name, observation)
}

func (nara *Nara) getObservation(name string) NaraObservation {
	observation, _ := nara.Status.Observations[name]
	return observation
}

func (nara *Nara) setObservation(name string, observation NaraObservation) {
	nara.Status.Observations[name] = observation
}

func (network *Network) formOpinion() {
	time.Sleep(40 * time.Second)
	logrus.Printf("🕵️  forming opinions...")
	for name, _ := range network.Neighbourhood {
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
		lastRestart := network.findLastRestartFromNeighbourhoodForNara(name)
		if lastRestart > 0 {
			observation.LastRestart = lastRestart
		} else {
			logrus.Printf("couldn't adjust last restart date for %s based on neighbour disagreement", name)
		}
		network.local.setObservation(name, observation)
	}
	logrus.Printf("👀  opinions formed")
}

func (network *Network) findStartingTimeFromNeighbourhoodForNara(name string) int64 {
	times := make(map[int64]int)

	network.local.mu.Lock()
	for _, nara := range network.Neighbourhood {
		observed_start_time := nara.getObservation(name).StartTime
		if observed_start_time > 0 {
			times[observed_start_time] += 1
		}
	}
	network.local.mu.Unlock()

	var startTime int64
	maxSeen := 0
	one_third := len(times) / 3

	for time, count := range times {
		if count > maxSeen && count > one_third {
			maxSeen = count
			startTime = time
		}
	}

	return startTime
}

func (network *Network) recordObservationOnlineNara(name string) {
	observation := network.local.getObservation(name)

	if observation.StartTime == 0 || name == network.meName() {
		if name != network.meName() {
			logrus.Printf("observation: seen %s for the first time", name)
		}

		observation.Restarts = network.findRestartCountFromNeighbourhoodForNara(name)
		observation.StartTime = network.findStartingTimeFromNeighbourhoodForNara(name)
		observation.LastRestart = network.findLastRestartFromNeighbourhoodForNara(name)

		if observation.StartTime == 0 && name == network.meName() {
			observation.StartTime = time.Now().Unix()
		}

		if observation.LastRestart == 0 && name == network.meName() {
			observation.LastRestart = time.Now().Unix()
		}
	}

	if observation.Online != "ONLINE" && observation.Online != "" {
		observation.Restarts += 1
		observation.LastRestart = time.Now().Unix()
		logrus.Printf("observation: %s came back online", name)
	}

	observation.Online = "ONLINE"
	observation.LastSeen = time.Now().Unix()
	network.local.setObservation(name, observation)
}

func (network *Network) findRestartCountFromNeighbourhoodForNara(name string) int64 {
	values := make(map[int64]int)

	network.local.mu.Lock()
	for _, nara := range network.Neighbourhood {
		restarts := nara.getObservation(name).Restarts
		values[restarts] += 1
	}
	network.local.mu.Unlock()

	var result int64
	maxSeen := 0

	for restarts, count := range values {
		if count > maxSeen && restarts > 0 {
			maxSeen = count
			result = restarts
		}
	}

	return result
}

func (network *Network) findLastRestartFromNeighbourhoodForNara(name string) int64 {
	values := make(map[int64]int)

	network.local.mu.Lock()
	for _, nara := range network.Neighbourhood {
		last_restart := nara.getObservation(name).LastRestart
		if last_restart > 0 {
			values[last_restart] += 1
		}
	}
	network.local.mu.Unlock()

	var result int64
	maxSeen := 0
	one_third := len(values) / 3

	for last_restart, count := range values {
		if count > maxSeen && count > one_third {
			maxSeen = count
			result = last_restart
		}
	}

	return result
}

func (network *Network) observationMaintenance() {
	for {
		now := time.Now().Unix()

		for name, observation := range network.local.Me.Status.Observations {
			// only do maintenance on naras that are online
			if observation.Online != "ONLINE" {
				if observation.ClusterName != "" {
					// reset cluster for offline naras
					observation.ClusterName = ""
					network.local.setObservation(name, observation)
				}

				continue
			}

			// mark missing after 100 seconds of no updates
			if (now-observation.LastSeen) > 100 && !network.skippingEvents {
				observation.Online = "MISSING"
				network.local.setObservation(name, observation)
				logrus.Printf("observation: %s has disappeared", name)
			}
		}

		network.calculateClusters()

		time.Sleep(1 * time.Second)
	}
}
