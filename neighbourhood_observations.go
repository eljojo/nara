package nara

import "github.com/eljojo/nara/types"

// neighbourhood_observations.go
// Extracted from observations.go
// Contains NaraObservation type and observation access methods

// NaraObservation - pure state data about a nara
// Identity (who this is about) is external - stored as map key or in containing struct
type NaraObservation struct {
	// The Trinity - core checkpoint data
	Restarts    int64 `json:"restarts,omitempty"`     // Total restart count
	TotalUptime int64 `json:"total_uptime,omitempty"` // Total seconds online
	StartTime   int64 `json:"start_time,omitempty"`   // Unix timestamp when first observed (FirstSeen)

	// Current state
	Online      string `json:"online,omitempty"`       // "ONLINE", "OFFLINE", "MISSING"
	LastSeen    int64  `json:"last_seen,omitempty"`    // Unix timestamp last seen
	LastRestart int64  `json:"last_restart,omitempty"` // Unix timestamp of last restart

	// Cluster info
	ClusterName  string `json:"cluster_name,omitempty"`
	ClusterEmoji string `json:"cluster_emoji,omitempty"`

	// Latency measurement fields (for local tracking only, not synced)
	LastPingRTT  float64 `json:"last_ping_rtt,omitempty"`  // Last measured RTT in milliseconds
	AvgPingRTT   float64 `json:"avg_ping_rtt,omitempty"`   // Exponential moving average of RTT
	LastPingTime int64   `json:"last_ping_time,omitempty"` // Unix timestamp of last ping
}

// isOnline returns true if the nara is currently online.
func (obs NaraObservation) isOnline() bool {
	return obs.Online == "ONLINE"
}

// getMeObservation returns the observation about ourselves.
func (localNara *LocalNara) getMeObservation() NaraObservation {
	return localNara.getObservation(localNara.Me.Name)
}

// setMeObservation sets the observation about ourselves.
func (localNara *LocalNara) setMeObservation(observation NaraObservation) {
	localNara.setObservation(localNara.Me.Name, observation)
}

// getObservation returns the observation about the named nara.
func (localNara *LocalNara) getObservation(name types.NaraName) NaraObservation {
	observation := localNara.Me.getObservation(name)
	return observation
}

// getObservationLocked returns the observation about the named nara.
// This is called when localNara.mu is already held, but we still need to lock localNara.Me.mu.
func (localNara *LocalNara) getObservationLocked(name types.NaraName) NaraObservation {
	// this is called when localNara.mu is already held
	// but we still need to lock localNara.Me.mu
	localNara.Me.mu.Lock()
	observation := localNara.Me.Status.Observations[name]
	localNara.Me.mu.Unlock()
	return observation
}

// setObservation sets the observation about the named nara.
func (localNara *LocalNara) setObservation(name types.NaraName, observation NaraObservation) {
	localNara.mu.Lock()
	localNara.Me.setObservation(name, observation)
	localNara.mu.Unlock()
}

// getObservation returns the observation about the named nara.
func (nara *Nara) getObservation(name types.NaraName) NaraObservation {
	nara.mu.Lock()
	observation := nara.Status.Observations[name]
	nara.mu.Unlock()
	return observation
}

// setObservation sets the observation about the named nara.
func (nara *Nara) setObservation(name types.NaraName, observation NaraObservation) {
	nara.mu.Lock()
	nara.setObservationLocked(name, observation)
	nara.mu.Unlock()
}

// setObservationLocked sets an observation without acquiring the lock.
// Caller must hold nara.mu.
func (nara *Nara) setObservationLocked(name types.NaraName, observation NaraObservation) {
	nara.Status.Observations[name] = observation
}
