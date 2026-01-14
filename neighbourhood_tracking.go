package nara

// neighbourhood_tracking.go
// Extracted from network.go
// Contains neighbourhood map access and tracking methods

// getNara returns a copy of the nara with the given name, or an empty Nara if not found.
func (network *Network) getNara(name string) *Nara {
	network.local.mu.Lock()
	nara, present := network.Neighbourhood[name]
	network.local.mu.Unlock()
	if present {
		return nara
	}
	return nil
}

// importNara imports a nara into the neighbourhood.
// If the nara already exists, it updates its values.
func (network *Network) importNara(nara *Nara) {
	nara.mu.Lock()
	defer nara.mu.Unlock()

	// deadlock prevention: ensure we always lock in the same order
	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	n, present := network.Neighbourhood[nara.Name]
	if present {
		n.setValuesFrom(nara)
	} else {
		network.Neighbourhood[nara.Name] = nara
	}
}
