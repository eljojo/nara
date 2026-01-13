package nara

import (
	"strings"
	"testing"
)

// TestStartTimeConsensus_DeferredToFormOpinion verifies that:
// 1. Collecting howdy votes does NOT immediately apply consensus
// 2. Waiting for more votes produces a DIFFERENT (correct) result than early consensus
//
// This test is designed so that:
// - First 3 votes: scattered outliers, would pick 9000 (highest weight single-observer)
// - All 10 votes: correct cluster around 2000 wins (5 observers, high total weight)
func TestStartTimeConsensus_DeferredToFormOpinion(t *testing.T) {
	t.Parallel()
	ln := testLocalNara(t,"me")
	network := ln.Network

	obsMe := network.local.getMeObservation()
	obsMe.StartTime = 0
	network.local.setMeObservation(obsMe)

	// First 3 votes: scattered outliers (each in own cluster, far apart)
	// If we triggered at 3 votes, we'd get 9000 (highest uptime single-observer fallback)
	earlyVotes := []struct {
		startTime int64
		uptime    uint64
	}{
		{1000, 50},  // Outlier cluster A
		{5000, 50},  // Outlier cluster B
		{9000, 100}, // Outlier cluster C - highest uptime, would "win" with 3 votes
	}

	// Later votes: the correct cluster around 2000
	// These arrive after boot recovery completes
	lateVotes := []struct {
		startTime int64
		uptime    uint64
	}{
		{2000, 200},
		{2010, 200},
		{2020, 200},
		{2030, 200},
		{2040, 200}, // 5 observers, total weight 1000
	}

	// Collect early votes
	for _, v := range earlyVotes {
		network.collectStartTimeVote(NaraObservation{StartTime: v.startTime}, v.uptime)
	}

	// Verify: StartTime should still be 0 (early trigger removed)
	obsAfterEarly := network.local.getMeObservation()
	if obsAfterEarly.StartTime != 0 {
		t.Fatalf("Early trigger fired! StartTime=%d after 3 votes (expected 0)", obsAfterEarly.StartTime)
	}

	// Collect late votes (simulating boot recovery bringing more data)
	for _, v := range lateVotes {
		network.collectStartTimeVote(NaraObservation{StartTime: v.startTime}, v.uptime)
	}

	// Now apply consensus (what formOpinion does after waiting)
	network.startTimeVotesMu.Lock()
	network.applyStartTimeConsensus()
	network.startTimeVotesMu.Unlock()

	// With all 8 votes:
	// - Outlier clusters: 1 observer each
	// - Cluster around 2000: 5 observers, weight 1000
	// Algorithm prefers ≥2 observers, so 2000-cluster wins
	// Median of [2000, 2010, 2020, 2030, 2040] = index 2 = 2020
	obsFinal := network.local.getMeObservation()
	expectedStartTime := int64(2020)

	if obsFinal.StartTime != expectedStartTime {
		t.Errorf("Expected StartTime=%d (correct cluster median), got=%d", expectedStartTime, obsFinal.StartTime)
		t.Errorf("If this is 9000, the early 3-vote trigger is still active!")
	}
}

// TestStartTimeConsensus_PreferMultiObserverCluster verifies that clusters with
// ≥2 observers are preferred over single-observer clusters, even if the single
// observer has higher uptime.
func TestStartTimeConsensus_PreferMultiObserverCluster(t *testing.T) {
	t.Parallel()
	ln := testLocalNara(t,"me")
	network := ln.Network

	obsMe := network.local.getMeObservation()
	obsMe.StartTime = 0
	network.local.setMeObservation(obsMe)

	// Scenario:
	// Cluster A: single observer with massive uptime (9999)
	// Cluster B: two observers with small uptime (50 each = 100 total)
	//
	// Algorithm prefers ≥2 observer clusters first, so Cluster B wins despite lower weight.

	votes := []struct {
		startTime int64
		uptime    uint64
	}{
		{1000, 9999}, // Cluster A: single observer, huge uptime
		{5000, 50},   // Cluster B: first observer
		{5030, 50},   // Cluster B: second observer (within 60s tolerance)
	}

	for _, v := range votes {
		network.collectStartTimeVote(NaraObservation{StartTime: v.startTime}, v.uptime)
	}

	network.startTimeVotesMu.Lock()
	network.applyStartTimeConsensus()
	network.startTimeVotesMu.Unlock()

	obsFinal := network.local.getMeObservation()
	// Cluster B wins: values [5000, 5030], median index = 1, value = 5030
	expectedStartTime := int64(5030)
	if obsFinal.StartTime != expectedStartTime {
		t.Errorf("Expected StartTime %d (multi-observer cluster), got: %d", expectedStartTime, obsFinal.StartTime)
	}
}

// TestStartTimeConsensus_FallbackToHighestWeight verifies that when no cluster
// has ≥2 observers, the highest-weight single-observer cluster wins.
func TestStartTimeConsensus_FallbackToHighestWeight(t *testing.T) {
	t.Parallel()
	ln := testLocalNara(t,"me")
	network := ln.Network

	obsMe := network.local.getMeObservation()
	obsMe.StartTime = 0
	network.local.setMeObservation(obsMe)

	// Scenario: All clusters have single observers (values far apart)
	// The one with highest uptime should win.

	votes := []struct {
		startTime int64
		uptime    uint64
	}{
		{1000, 100},  // Cluster A: low uptime
		{5000, 500},  // Cluster B: medium uptime
		{9000, 1000}, // Cluster C: highest uptime - should win
	}

	for _, v := range votes {
		network.collectStartTimeVote(NaraObservation{StartTime: v.startTime}, v.uptime)
	}

	network.startTimeVotesMu.Lock()
	network.applyStartTimeConsensus()
	network.startTimeVotesMu.Unlock()

	obsFinal := network.local.getMeObservation()
	expectedStartTime := int64(9000)
	if obsFinal.StartTime != expectedStartTime {
		t.Errorf("Expected StartTime %d (highest weight fallback), got: %d", expectedStartTime, obsFinal.StartTime)
	}
}

// TestStartTimeConsensus_ToleranceGrouping verifies that values within 60 seconds
// are grouped into the same cluster.
func TestStartTimeConsensus_ToleranceGrouping(t *testing.T) {
	t.Parallel()
	ln := testLocalNara(t,"me")
	network := ln.Network

	obsMe := network.local.getMeObservation()
	obsMe.StartTime = 0
	network.local.setMeObservation(obsMe)

	// Scenario: Values spread within 60 second tolerance should cluster together
	// 1000, 1030, 1059 are all within 60s of the cluster start (1000)
	// 1061 would start a new cluster (61s from 1000)

	votes := []struct {
		startTime int64
		uptime    uint64
	}{
		{1000, 100},
		{1030, 100},
		{1059, 100}, // Still within 60s of 1000
		{1061, 100}, // New cluster (61s from 1000)
	}

	for _, v := range votes {
		network.collectStartTimeVote(NaraObservation{StartTime: v.startTime}, v.uptime)
	}

	network.startTimeVotesMu.Lock()
	network.applyStartTimeConsensus()
	network.startTimeVotesMu.Unlock()

	obsFinal := network.local.getMeObservation()
	// First cluster [1000, 1030, 1059] has 3 observers (weight 300)
	// Second cluster [1061] has 1 observer (weight 100)
	// First cluster wins, median index = 1, value = 1030
	expectedStartTime := int64(1030)
	if obsFinal.StartTime != expectedStartTime {
		t.Errorf("Expected StartTime %d (tolerance grouping), got: %d", expectedStartTime, obsFinal.StartTime)
	}
}

// TestFlair_ShowsThinkingEmoji_WhenStartTimeUnknown verifies UI behavior during boot
func TestFlair_ShowsThinkingEmoji_WhenStartTimeUnknown(t *testing.T) {
	t.Parallel()
	ln := testLocalNara(t,"me")
	network := ln.Network

	obsMe := network.local.getMeObservation()
	obsMe.StartTime = 0
	obsMe.Online = "ONLINE"
	network.local.setMeObservation(obsMe)

	// Add 3 neighbors so networkSize > 2
	for i := 0; i < 3; i++ {
		name := string(rune('a' + i))
		network.importNara(NewNara(name))
		network.local.setObservation(name, NaraObservation{StartTime: 1000 + int64(i*100), Online: "ONLINE"})
	}

	flair := ln.Flair()

	if !strings.Contains(flair, "🤔") {
		t.Errorf("Expected 🤔 when StartTime unknown, got: %s", flair)
	}
	if strings.Contains(flair, "🧓") || strings.Contains(flair, "🐤") {
		t.Errorf("Expected no age flair when StartTime unknown, got: %s", flair)
	}
}

// TestFlair_ShowsAgeFlair_WhenStartTimeKnown verifies normal flair after opinions form
func TestFlair_ShowsAgeFlair_WhenStartTimeKnown(t *testing.T) {
	t.Parallel()
	ln := testLocalNara(t,"me")
	network := ln.Network

	obsMe := network.local.getMeObservation()
	obsMe.StartTime = 500 // oldest
	obsMe.Online = "ONLINE"
	network.local.setMeObservation(obsMe)

	for i := 0; i < 3; i++ {
		name := string(rune('a' + i))
		network.importNara(NewNara(name))
		network.local.setObservation(name, NaraObservation{StartTime: 1000 + int64(i*100), Online: "ONLINE"})
	}

	flair := ln.Flair()

	if strings.Contains(flair, "🤔") {
		t.Errorf("Expected no 🤔 when StartTime known, got: %s", flair)
	}
	if !strings.Contains(flair, "🧓") {
		t.Errorf("Expected 🧓 when oldest, got: %s", flair)
	}
}
