package nara

import (
	"fmt"
	"math/rand"

	"github.com/sirupsen/logrus"

	"github.com/eljojo/nara/types"
)

// startTimeVote represents a vote for our start time from another nara
type startTimeVote struct {
	value  int64
	uptime uint64
}

// collectStartTimeVote collects a vote about our start time from a howdy response
func (network *Network) collectStartTimeVote(obs NaraObservation, senderUptime uint64) {
	// Skip if no start time info
	if obs.StartTime == 0 {
		return
	}

	network.startTimeVotesMu.Lock()
	defer network.startTimeVotesMu.Unlock()

	network.startTimeVotes = append(network.startTimeVotes, startTimeVote{
		value:  obs.StartTime,
		uptime: senderUptime,
	})

	// Don't apply consensus early - let formOpinion() handle it with more data
	// after boot recovery completes (~3+ minutes)
}

// applyStartTimeConsensus applies consensus to determine our start time
// Must be called with startTimeVotesMu held
func (network *Network) applyStartTimeConsensus() {
	if len(network.startTimeVotes) == 0 {
		return
	}

	// Convert to consensusValue format
	var values []consensusValue
	for _, vote := range network.startTimeVotes {
		values = append(values, consensusValue{
			value:  vote.value,
			weight: vote.uptime,
		})
	}

	// Use existing consensus algorithm
	const tolerance int64 = 60 // seconds, handles clock drift
	result := consensusByUptime(values, tolerance, false)

	if result > 0 {
		obs := network.local.getMeObservation()
		if obs.StartTime != result {
			obs.StartTime = result
			network.local.setMeObservation(obs)
			logrus.Infof("🕰️ Recovered start time via howdy consensus: %d (from %d votes)", result, len(network.startTimeVotes))
		}
	}
}

// recoverSelfStartTimeFromMesh attempts to recover this nara's start time from mesh neighbors
func (network *Network) recoverSelfStartTimeFromMesh() {
	if network.local.SyncLedger == nil || network.tsnetMesh == nil || network.local.isBooting() {
		return
	}

	obs := network.local.getMeObservation()
	if obs.StartTime > 0 {
		return
	}

	online := network.NeighbourhoodOnlineNames()
	if len(online) == 0 {
		return
	}

	// Ask a few neighbors for their opinions about our start time.
	rand.Shuffle(len(online), func(i, j int) {
		online[i], online[j] = online[j], online[i]
	})

	targetCount := len(online)
	if targetCount > 3 {
		targetCount = 3
	}

	subjects := []types.NaraName{network.meName()}
	totalAdded := 0

	for _, neighbor := range online[:targetCount] {
		ip, naraID := network.getMeshInfoForNara(neighbor)
		if ip == "" || naraID == "" {
			continue
		}

		// Register peer for mesh client lookups
		network.meshClient.RegisterPeerIP(naraID, ip)

		events, respVerified := network.fetchSyncEventsFromMesh(naraID, neighbor, subjects, 0, 1, 500)
		if len(events) == 0 {
			continue
		}

		filtered := make([]SyncEvent, 0, len(events))
		for _, event := range events {
			if event.Service != ServiceObservation || event.Observation == nil {
				continue
			}
			if event.Observation.Subject != network.meName() {
				continue
			}
			filtered = append(filtered, event)
		}

		if len(filtered) == 0 {
			continue
		}

		added, warned := network.MergeSyncEventsWithVerification(filtered)
		totalAdded += added
		verifiedStr := ""
		if respVerified && warned == 0 {
			verifiedStr = " ✓"
		} else if warned > 0 {
			verifiedStr = fmt.Sprintf(" ⚠%d", warned)
		}
		logrus.Printf("📦 start time recovery from %s: received %d events, merged %d%s", neighbor, len(filtered), added, verifiedStr)
	}

	if totalAdded == 0 {
		return
	}

	// Try to recover start time from event-based consensus
	if network.local.Projections != nil {
		updated := network.local.getMeObservation()
		before := updated.StartTime
		if _, err := network.local.Projections.Opinion().RunOnce(); err != nil {
			logrus.WithError(err).Warn("Failed to run opinion projection")
		}
		opinion := network.local.Projections.Opinion().DeriveOpinionWithValidation(network.meName())
		if updated.StartTime == 0 && opinion.StartTime > 0 {
			updated.StartTime = opinion.StartTime
		}
		if updated.StartTime > 0 && updated.StartTime != before {
			network.local.setObservation(network.meName(), updated)
			logrus.Printf("🕰️ recovered start time for %s via event consensus: %d", network.meName(), updated.StartTime)
		}
	}
}
