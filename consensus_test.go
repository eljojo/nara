package nara

import (
	"fmt"
	"testing"
)

// helper to create a nara with uptime set
func newNaraWithUptime(name string, uptime uint64) *Nara {
	n := NewNara(name)
	n.Status.HostStats.Uptime = uptime
	return n
}

func TestConsensus_ClusteringWithUptime(t *testing.T) {
	ln := NewLocalNara("me", "me-soul", "host", "user", "pass", -1)
	network := ln.Network
	target := "target"
	network.importNara(NewNara(target))

	// Two clusters: [100, 100] and [200]
	// Cluster 1 has higher total uptime (1000+1000=2000 vs 500)
	n1 := newNaraWithUptime("n1", 1000)
	n1.setObservation(target, NaraObservation{StartTime: 100})
	network.importNara(n1)

	n2 := newNaraWithUptime("n2", 1000)
	n2.setObservation(target, NaraObservation{StartTime: 100})
	network.importNara(n2)

	n3 := newNaraWithUptime("n3", 500)
	n3.setObservation(target, NaraObservation{StartTime: 200})
	network.importNara(n3)

	startTime := network.findStartingTimeFromNeighbourhoodForNara(target)
	if startTime != 100 {
		t.Errorf("expected cluster with higher uptime (100) to win, got %d", startTime)
	}
}

func TestConsensus_WeakStrategyWhenNoAgreement(t *testing.T) {
	ln := NewLocalNara("me", "me-soul", "host", "user", "pass", -1)
	network := ln.Network
	target := "target"
	network.importNara(NewNara(target))

	// Three single-observer clusters with different uptimes
	// No cluster has 2+ observers, so falls to Strategy 2 (highest uptime)
	n1 := newNaraWithUptime("n1", 100)
	n1.setObservation(target, NaraObservation{StartTime: 100})
	network.importNara(n1)

	n2 := newNaraWithUptime("n2", 500)
	n2.setObservation(target, NaraObservation{StartTime: 200})
	network.importNara(n2)

	n3 := newNaraWithUptime("n3", 10000) // highest uptime wins
	n3.setObservation(target, NaraObservation{StartTime: 300})
	network.importNara(n3)

	startTime := network.findStartingTimeFromNeighbourhoodForNara(target)
	if startTime != 300 {
		t.Errorf("expected highest uptime (300) to win when no agreement, got %d", startTime)
	}
}

func TestConsensus_SmallDisagreement(t *testing.T) {
	ln := NewLocalNara("me", "me-soul", "host", "user", "pass", -1)
	network := ln.Network
	target := "target"
	network.importNara(NewNara(target))

	// Naras disagree by small amounts (clock drift, etc)
	// Values: 100, 101, 102, 103, 104 - all within 60s tolerance = ONE cluster
	for i := 0; i < 5; i++ {
		n := newNaraWithUptime(fmt.Sprintf("n%d", i), 1000)
		n.setObservation(target, NaraObservation{StartTime: int64(100 + i)})
		network.importNara(n)
	}

	// Should cluster together and return median (102)
	startTime := network.findStartingTimeFromNeighbourhoodForNara(target)
	if startTime != 102 {
		t.Errorf("expected median 102 from single cluster, got %d", startTime)
	}
}

func TestConsensus_OutlierIgnored(t *testing.T) {
	ln := NewLocalNara("me", "me-soul", "host", "user", "pass", -1)
	network := ln.Network
	target := "target"
	network.importNara(NewNara(target))

	// Most agree around 100, but one outlier at 9999
	// Cluster 1: [100, 101, 102, 103] with total uptime 4000
	// Cluster 2: [9999] with uptime 1000
	times := []int64{100, 101, 102, 103, 9999}
	for i, time := range times {
		n := newNaraWithUptime(fmt.Sprintf("n%d", i), 1000)
		n.setObservation(target, NaraObservation{StartTime: time})
		network.importNara(n)
	}

	// Main cluster wins, median of [100,101,102,103] is 102 (index 4/2=2)
	startTime := network.findStartingTimeFromNeighbourhoodForNara(target)
	if startTime != 102 {
		t.Errorf("expected median 102 from main cluster (outlier ignored), got %d", startTime)
	}
}

func TestConsensus_AgreementBeatsElder(t *testing.T) {
	ln := NewLocalNara("me", "me-soul", "host", "user", "pass", -1)
	network := ln.Network
	target := "target"
	network.importNara(NewNara(target))

	// Main cluster has 4 observers (all within tolerance = one cluster)
	// Elder has higher uptime but is alone
	// Strategy 1 (Strong) prefers the 4-observer cluster
	times := []int64{100, 101, 102, 103}
	for i, time := range times {
		n := newNaraWithUptime(fmt.Sprintf("n%d", i), 100) // low uptime
		n.setObservation(target, NaraObservation{StartTime: time})
		network.importNara(n)
	}

	// Add an "elder" with very high uptime but alone
	elder := newNaraWithUptime("elder", 100000)
	elder.setObservation(target, NaraObservation{StartTime: 9999})
	network.importNara(elder)

	// Agreement (4 observers) beats the lone elder
	startTime := network.findStartingTimeFromNeighbourhoodForNara(target)
	if startTime == 9999 {
		t.Error("lone elder should not beat 4 agreeing observers")
	}
	if startTime != 102 {
		t.Errorf("expected median 102 from agreeing cluster, got %d", startTime)
	}
}

func TestConsensus_ChangeOfMind(t *testing.T) {
	ln := NewLocalNara("me", "me-soul", "host", "user", "pass", -1)
	network := ln.Network
	target := "target"
	network.importNara(NewNara(target))

	// Start with consensus at 100
	n1 := newNaraWithUptime("n1", 1000)
	n1.setObservation(target, NaraObservation{StartTime: 100})
	network.importNara(n1)

	n2 := newNaraWithUptime("n2", 1000)
	n2.setObservation(target, NaraObservation{StartTime: 100})
	network.importNara(n2)

	startTime := network.findStartingTimeFromNeighbourhoodForNara(target)
	if startTime != 100 {
		t.Errorf("expected initial consensus 100, got %d", startTime)
	}

	// New naras join with different opinion and higher uptimes
	for i := 0; i < 5; i++ {
		n := newNaraWithUptime(fmt.Sprintf("new%d", i), 5000)
		n.setObservation(target, NaraObservation{StartTime: 200})
		network.importNara(n)
	}

	// New cluster (200) has total uptime 25000 vs old cluster (100) with 2000
	startTime = network.findStartingTimeFromNeighbourhoodForNara(target)
	if startTime != 200 {
		t.Errorf("expected consensus to shift to 200, got %d", startTime)
	}
}

func TestConsensus_CoinFlipWhenClose(t *testing.T) {
	// When no cluster has 2+ observers and top 2 are within 20% uptime,
	// we flip a coin. This test just verifies it doesn't crash and returns
	// one of the two values.
	ln := NewLocalNara("me", "me-soul", "host", "user", "pass", -1)
	network := ln.Network
	target := "target"
	network.importNara(NewNara(target))

	// Two single-observer clusters with similar uptimes
	n1 := newNaraWithUptime("n1", 1000)
	n1.setObservation(target, NaraObservation{StartTime: 100})
	network.importNara(n1)

	n2 := newNaraWithUptime("n2", 950) // within 20% of 1000
	n2.setObservation(target, NaraObservation{StartTime: 200})
	network.importNara(n2)

	startTime := network.findStartingTimeFromNeighbourhoodForNara(target)
	if startTime != 100 && startTime != 200 {
		t.Errorf("expected coin flip to return 100 or 200, got %d", startTime)
	}
}

func TestConsensus_StrongStrategyPreferred(t *testing.T) {
	// Strategy 1 (Strong) should be preferred over single-observer clusters
	ln := NewLocalNara("me", "me-soul", "host", "user", "pass", -1)
	network := ln.Network
	target := "target"
	network.importNara(NewNara(target))

	// Cluster with 2 observers but lower total uptime
	n1 := newNaraWithUptime("n1", 100)
	n1.setObservation(target, NaraObservation{StartTime: 100})
	network.importNara(n1)

	n2 := newNaraWithUptime("n2", 100)
	n2.setObservation(target, NaraObservation{StartTime: 100})
	network.importNara(n2)

	// Single observer with much higher uptime
	n3 := newNaraWithUptime("n3", 10000)
	n3.setObservation(target, NaraObservation{StartTime: 999})
	network.importNara(n3)

	// Should prefer the 2-observer cluster despite lower uptime
	startTime := network.findStartingTimeFromNeighbourhoodForNara(target)
	if startTime != 100 {
		t.Errorf("expected Strong strategy (2 observers) to win, got %d", startTime)
	}
}

func TestConsensus_NoChainingBug(t *testing.T) {
	ln := NewLocalNara("me", "me-soul", "host", "user", "pass", -1)
	network := ln.Network
	target := "target"
	network.importNara(NewNara(target))

	// Values that would chain incorrectly if comparing to last value:
	// [100, 159, 218] - each pair is within 60, but 218-100 > 60
	// Should create TWO clusters: [100, 159] and [218]
	// (using values > 0 because StartTime=0 is skipped)
	times := []int64{100, 159, 218}
	for i, time := range times {
		n := newNaraWithUptime(fmt.Sprintf("n%d", i), 1000)
		n.setObservation(target, NaraObservation{StartTime: time})
		network.importNara(n)
	}

	// Cluster [100, 159] has uptime 2000, cluster [218] has uptime 1000
	// First cluster wins, median of [100, 159] is 159 (index 2/2=1)
	startTime := network.findStartingTimeFromNeighbourhoodForNara(target)
	if startTime == 218 {
		t.Error("chaining bug: 218 should not be in the same cluster as 100")
	}
	if startTime != 159 {
		t.Errorf("expected median 159 from cluster [100, 159], got %d", startTime)
	}
}

func TestObservations_OnlineTransitions(t *testing.T) {
	ln := NewLocalNara("me", "me-soul", "host", "user", "pass", -1)
	network := ln.Network
	name := "target"

	network.importNara(NewNara(name))

	obs := network.local.getObservation(name)
	if obs.Online != "" {
		t.Errorf("expected initial state to be empty, got %s", obs.Online)
	}

	network.recordObservationOnlineNara(name)
	obs = network.local.getObservation(name)
	if obs.Online != "ONLINE" {
		t.Errorf("expected state ONLINE, got %s", obs.Online)
	}
}
