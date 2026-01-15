package nara

import "github.com/sirupsen/logrus"

// neighbourhood_tracking.go
// Extracted from network.go
// Contains neighbourhood map access and tracking methods

// getNara returns a pointer to the nara with the given name, or nil if not found.
//
// ⚠️ WARNING - RACE CONDITION:
// The nara may be removed from the Neighbourhood map between the time you call this
// function and the time you use the returned pointer. This means:
//
//  1. ALWAYS check for nil before using the returned pointer
//  2. Even after nil check, fields may be stale/inconsistent
//  3. For iteration, use snapshot methods instead (see below)
//
// ❌ UNSAFE - Will panic if nara is removed:
//
//	nara := network.getNara(name)
//	trend := nara.Status.Trend  // PANIC if nara is nil!
//
// ⚠️ RISKY - Race between check and use:
//
//	nara := network.getNara(name)
//	if nara != nil {
//	    trend := nara.Status.Trend  // Might read inconsistent data
//	}
//
// ✅ PREFER - Use snapshots for iteration:
//
//	for _, nara := range network.getOnlineNarasSnapshot() {
//	    trend := nara.Status.Trend  // Safe - no race
//	}
//
// Only use getNara() for:
//   - Quick existence checks where nil is acceptable
//   - Single field reads where you can handle nil safely
//   - Test code where race conditions are acceptable
func (network *Network) getNara(name string) *Nara {
	network.local.mu.Lock()
	nara, present := network.Neighbourhood[name]
	network.local.mu.Unlock()
	if present {
		return nara
	}
	return nil
}

// getNaraByID returns a pointer to the nara with the given ID, or nil if not found.
// This is the preferred lookup method - IDs are stable identifiers that don't change
// even if a nara changes name.
//
// Uses O(1) lookup via the NeighbourhoodByID index when available,
// falls back to O(N) search for backwards compatibility with tests.
//
// ⚠️ Same race condition warnings as getNara() apply - see that function's docs.
func (network *Network) getNaraByID(id string) *Nara {
	if id == "" {
		return nil
	}
	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	// Try O(1) lookup via NeighbourhoodByID index if available
	if network.NeighbourhoodByID != nil {
		if nara, ok := network.NeighbourhoodByID[id]; ok {
			return nara
		}
	}

	// Fall back to O(N) search of Neighbourhood for backwards compatibility
	for _, nara := range network.Neighbourhood {
		nara.mu.Lock()
		if nara.Status.ID == id || nara.ID == id {
			nara.mu.Unlock()
			return nara
		}
		nara.mu.Unlock()
	}
	return nil
}

// getAllNarasSnapshot returns a snapshot of all naras in the neighbourhood.
//
// ✅ SAFE for iteration - The slice is built atomically while holding the lock,
// so there's no race condition between listing and accessing naras.
//
// Use this when:
//   - Iterating over all naras (online or offline)
//   - Building reports/metrics that need consistent view
//   - HTTP handlers returning nara lists
//
// Example:
//
//	for _, nara := range network.getAllNarasSnapshot() {
//	    // Safe to access nara.Status fields without nil checks
//	    doSomething(nara.Status.Trend)
//	}
func (network *Network) getAllNarasSnapshot() []*Nara {
	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	naras := make([]*Nara, 0, len(network.Neighbourhood))
	for _, nara := range network.Neighbourhood {
		naras = append(naras, nara)
	}
	return naras
}

// getOnlineNarasSnapshot returns a snapshot of all online naras in the neighbourhood.
//
// ✅ SAFE for iteration - The slice is built atomically while holding the lock,
// filtering to only online naras at the moment the snapshot is taken.
//
// Use this when:
//   - Iterating over active/online naras only
//   - Calculating stats that should only include online naras
//   - Broadcasting messages to online naras
//
// Example:
//
//	for _, nara := range network.getOnlineNarasSnapshot() {
//	    // Safe to access nara.Status fields without nil checks
//	    if nara.Status.Trend == "vibes" {
//	        followingVibes++
//	    }
//	}
func (network *Network) getOnlineNarasSnapshot() []*Nara {
	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	naras := make([]*Nara, 0, len(network.Neighbourhood))
	for _, nara := range network.Neighbourhood {
		obs := network.local.getObservationLocked(nara.Name)
		if obs.isOnline() {
			naras = append(naras, nara)
		}
	}
	return naras
}

// importNara imports a nara into the neighbourhood.
// If the nara already exists, it updates its values.
// Maintains all three indexes: Neighbourhood (by name), NeighbourhoodByID, and nameToID.
func (network *Network) importNara(nara *Nara) {
	nara.mu.Lock()
	defer nara.mu.Unlock()

	// deadlock prevention: ensure we always lock in the same order
	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	naraID := nara.Status.ID
	if naraID == "" {
		naraID = nara.ID // Fallback to top-level ID field
	}

	n, present := network.Neighbourhood[nara.Name]
	if present {
		n.setValuesFrom(nara)
	} else {
		network.Neighbourhood[nara.Name] = nara
	}

	// Maintain ID-based indexes (even for updates, in case ID was added)
	if naraID != "" {
		// Warn if two different naras have the same name (ID collision)
		if network.nameToID != nil {
			existingID, exists := network.nameToID[nara.Name]
			if exists && existingID != naraID {
				logrus.Warnf("⚠️ Name collision: nara %q has two different IDs: %s (existing) vs %s (new) - overwriting index", nara.Name, existingID, naraID)
			}
		}
		if network.NeighbourhoodByID != nil {
			network.NeighbourhoodByID[naraID] = nara
		}
		if network.nameToID != nil {
			network.nameToID[nara.Name] = naraID
		}
	}
}

// getNaraIDByName returns the nara ID for a given name, or empty string if not found.
// This is a quick O(1) lookup using the nameToID index.
func (network *Network) getNaraIDByName(name string) string {
	if name == "" {
		return ""
	}
	// Check if it's self first
	if name == network.meName() {
		return network.local.ID
	}
	network.local.mu.Lock()
	id := network.nameToID[name]
	network.local.mu.Unlock()
	return id
}
