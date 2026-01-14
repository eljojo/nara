package nara

// neighbourhood_queries.go
// Extracted from network.go
// Contains neighbourhood query methods

// NeighbourhoodNames returns the names of all naras in the neighbourhood.
func (network *Network) NeighbourhoodNames() []string {
	var result []string
	network.local.mu.Lock()
	defer network.local.mu.Unlock()
	for _, nara := range network.Neighbourhood {
		result = append(result, nara.Name)
	}
	return result
}

// NeighbourhoodOnlineNames returns the names of all online naras in the neighbourhood.
func (network *Network) NeighbourhoodOnlineNames() []string {
	var result []string
	network.local.mu.Lock()
	defer network.local.mu.Unlock()
	for _, nara := range network.Neighbourhood {
		obs := network.local.getObservationLocked(nara.Name)
		if !obs.isOnline() {
			continue
		}
		result = append(result, nara.Name)
	}
	return result
}

// oldestNara returns the oldest online nara in the neighbourhood.
func (network *Network) oldestNara() *Nara {
	result := network.local.Me
	oldest := int64(network.local.getMeObservation().StartTime)

	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	for name, nara := range network.Neighbourhood {
		obs := network.local.getObservationLocked(name)
		if obs.Online != "ONLINE" {
			continue
		}
		if oldest <= obs.StartTime && name > result.Name {
			continue
		}
		if obs.StartTime > 0 && obs.StartTime <= oldest {
			oldest = obs.StartTime
			result = nara
		}
	}
	return result
}

// youngestNara returns the youngest online nara in the neighbourhood.
func (network *Network) youngestNara() *Nara {
	result := network.local.Me
	youngest := int64(network.local.getMeObservation().StartTime)

	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	for name, nara := range network.Neighbourhood {
		obs := network.local.getObservationLocked(name)
		if obs.Online != "ONLINE" {
			continue
		}
		if youngest >= obs.StartTime && name < result.Name {
			continue
		}
		if obs.StartTime > 0 && obs.StartTime >= youngest {
			youngest = obs.StartTime
			result = nara
		}
	}
	return result
}

// mostRestarts returns the online nara with the most restarts.
func (network *Network) mostRestarts() *Nara {
	result := network.local.Me
	most_restarts := network.local.getMeObservation().Restarts

	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	for name, nara := range network.Neighbourhood {
		obs := network.local.getObservationLocked(name)
		if obs.Online != "ONLINE" {
			continue
		}
		if most_restarts >= obs.Restarts && name > result.Name {
			continue
		}
		if obs.Restarts > 0 && obs.Restarts >= most_restarts {
			most_restarts = obs.Restarts
			result = nara
		}
	}
	return result
}

// oldestNaraBarrio returns the oldest online nara in our barrio (cluster).
func (network *Network) oldestNaraBarrio() *Nara {
	result := network.local.Me
	oldest := int64(network.local.getMeObservation().StartTime)
	myClusterName := network.local.getMeObservation().ClusterName
	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	for name, nara := range network.Neighbourhood {
		// question: do we follow our opinion of their neighbourhood or their opinion?
		obs := network.local.getObservationLocked(name)
		if obs.Online != "ONLINE" {
			continue
		}
		if obs.ClusterName != myClusterName {
			continue
		}
		if oldest <= obs.StartTime && name > result.Name {
			continue
		}
		if obs.StartTime > 0 && obs.StartTime <= oldest {
			oldest = obs.StartTime
			result = nara
		}
	}
	return result
}

// youngestNaraBarrio returns the youngest online nara in our barrio (cluster).
func (network *Network) youngestNaraBarrio() *Nara {
	result := network.local.Me
	youngest := int64(network.local.getMeObservation().StartTime)
	myClusterName := network.local.getMeObservation().ClusterName

	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	for name, nara := range network.Neighbourhood {
		obs := network.local.getObservationLocked(name)
		if obs.Online != "ONLINE" {
			continue
		}
		if obs.ClusterName != myClusterName {
			continue
		}
		if youngest >= obs.StartTime && name < result.Name {
			continue
		}
		if obs.StartTime > 0 && obs.StartTime >= youngest {
			youngest = obs.StartTime
			result = nara
		}
	}
	return result
}
