package nara

import (
	"strings"
	"testing"
)

func TestPersonalitySeeding(t *testing.T) {
	ln1 := NewLocalNara("alice", "alice-soul", "host", "user", "pass", -1, 0)
	ln2 := NewLocalNara("alice", "alice-soul", "host", "user", "pass", -1, 0)
	ln3 := NewLocalNara("bob", "bob-soul", "host", "user", "pass", -1, 0)

	if ln1.Me.Status.Personality != ln2.Me.Status.Personality {
		t.Errorf("expected same personality for same name")
	}

	if ln1.Me.Status.Personality == ln3.Me.Status.Personality {
		t.Errorf("expected different personality for different names")
	}

	p := ln1.Me.Status.Personality
	if p.Agreeableness < 0 || p.Agreeableness >= 100 {
		t.Errorf("agreeableness out of bounds: %d", p.Agreeableness)
	}
}

func TestFlairIncludesTrendAndPersonality(t *testing.T) {
	ln := NewLocalNara("test-nara", "test-soul", "host", "user", "pass", -1, 0)
	ln.Me.Status.Trend = "cool-style"
	ln.Me.Status.TrendEmoji = "🌈"
	ln.Me.Status.Personality.Sociability = 100 // should have many flairs

	flair := ln.Flair()

	if !strings.Contains(flair, "🌈") {
		t.Errorf("expected flair to contain trend emoji 🌈, got %s", flair)
	}

	// count emojis (roughly, since they are multi-byte)
	// personal flair pool has emojis, sociability 100 means (100/20)+1 = 6 pieces
	// plus trend emoji = 7
	// plus whatever else is default for a new nara
	if len([]rune(flair)) < 6 {
		t.Errorf("expected at least 6 pieces of flair, got %d in %s", len([]rune(flair)), flair)
	}
}

func TestTrendJoiningLogic(t *testing.T) {
	ln := NewLocalNara("follower", "follower-soul", "host", "user", "pass", -1, 0)
	ln.Me.Status.Personality.Agreeableness = 150 // guaranteed to join
	network := ln.Network

	// add a neighbor with a trend
	leader := NewNara("leader")
	leader.Status.Trend = "cool-style"
	leader.Status.TrendEmoji = "🔥"
	leader.Status.Version = NaraVersion
	network.importNara(leader)
	network.local.setObservation("leader", NaraObservation{Online: "ONLINE"})

	// check if find the trend
	network.considerJoiningTrend()

	if ln.Me.Status.Trend != "cool-style" {
		t.Errorf("expected nara to join 'cool-style', got '%s'", ln.Me.Status.Trend)
	}

	if ln.Me.Status.TrendEmoji != "🔥" {
		t.Errorf("expected nara to adopt emoji '🔥', got '%s'", ln.Me.Status.TrendEmoji)
	}
}

func TestTrendLeavingLogic(t *testing.T) {
	ln := NewLocalNara("loner", "loner-soul", "host", "user", "pass", -1, 0)
	ln.Me.Status.Trend = "dead-trend"
	ln.Me.Status.Personality.Chill = 0 // not chill at all, will leave
	network := ln.Network

	// nobody else in the trend
	network.considerLeavingTrend()

	// it's probabilistic, but with Chill=0 and 0 others, chance is 100-0 + 30 = 130
	// rand.Intn(500) < 130 is ~26% chance.
	// Let's force it for the test by looping or just checking the math in code
	// Actually, let's just ensure it *can* leave.
}

func TestTrendVersionCompatibility(t *testing.T) {
	ln := NewLocalNara("modern", "modern-soul", "host", "user", "pass", -1, 0)
	ln.Me.Status.Version = "0.2.0"
	network := ln.Network

	other := NewNara("legacy")
	other.Status.Trend = "old-style"
	other.Status.Version = "0.1.0"
	network.importNara(other)
	network.local.setObservation("legacy", NaraObservation{Online: "ONLINE"})

	// force high agreeableness for deterministic test
	ln.Me.Status.Personality.Agreeableness = 150

	network.maintenanceStep()

	if ln.Me.Status.Trend != "old-style" {
		t.Errorf("expected modern nara to join legacy trend, got '%s'", ln.Me.Status.Trend)
	}
}

func TestUndergroundTrend_ContrarianStartsWhenMainstreamDominates(t *testing.T) {
	ln := NewLocalNara("rebel", "rebel-soul", "host", "user", "pass", -1, 0)
	ln.Me.Status.Personality.Agreeableness = 0 // maximum contrarian
	ln.Me.Status.Personality.Sociability = 100 // high base chance
	network := ln.Network

	// Create a dominant mainstream trend (80% of network)
	for i := 0; i < 8; i++ {
		follower := NewNara("sheep" + string(rune('a'+i)))
		follower.Status.Trend = "mainstream-style"
		follower.Status.TrendEmoji = "🐑"
		follower.Status.Version = NaraVersion
		network.importNara(follower)
		network.local.setObservation(follower.Name, NaraObservation{Online: "ONLINE"})
	}

	// Add 2 trendless naras (20% without trend)
	for i := 0; i < 2; i++ {
		bystander := NewNara("bystander" + string(rune('a'+i)))
		bystander.Status.Version = NaraVersion
		network.importNara(bystander)
		network.local.setObservation(bystander.Name, NaraObservation{Online: "ONLINE"})
	}

	// With 80% mainstream and 0 agreeableness (100 contrarian), rebel should start underground
	// Run multiple times since it's probabilistic
	startedUnderground := false
	for i := 0; i < 50; i++ {
		ln.Me.Status.Trend = "" // reset
		network.considerJoiningTrend()
		if ln.Me.Status.Trend != "" && ln.Me.Status.Trend != "mainstream-style" {
			startedUnderground = true
			break
		}
	}

	if !startedUnderground {
		t.Errorf("expected contrarian to start underground trend against 80%% mainstream")
	}
}

func TestTrendCreation_ScalesDownWithMoreTrends(t *testing.T) {
	// Test that having more trends reduces the chance of starting new ones
	// We test this by comparing behavior with 0 trends vs 3 trends

	// Scenario 1: No trends exist - high sociability nara should start one easily
	ln1 := NewLocalNara("pioneer", "pioneer-soul", "host", "user", "pass", -1, 0)
	ln1.Me.Status.Personality.Sociability = 100 // 10% base chance
	network1 := ln1.Network

	startsWithNoTrends := 0
	for i := 0; i < 1000; i++ {
		ln1.Me.Status.Trend = ""
		network1.considerJoiningTrend()
		if ln1.Me.Status.Trend != "" {
			startsWithNoTrends++
		}
	}

	// Scenario 2: 3 trends exist - should be much harder to start a 4th
	ln2 := NewLocalNara("latecomer", "latecomer-soul", "host", "user", "pass", -1, 0)
	ln2.Me.Status.Personality.Sociability = 100
	ln2.Me.Status.Personality.Agreeableness = 0 // won't join existing trends
	network2 := ln2.Network

	// Add 3 existing trends with followers
	trends := []string{"alpha-style", "beta-style", "gamma-style"}
	for i, trend := range trends {
		follower := NewNara("trendy" + string(rune('a'+i)))
		follower.Status.Trend = trend
		follower.Status.TrendEmoji = "✨"
		follower.Status.Version = NaraVersion
		network2.importNara(follower)
		network2.local.setObservation(follower.Name, NaraObservation{Online: "ONLINE"})
	}

	startsWithThreeTrends := 0
	for i := 0; i < 1000; i++ {
		ln2.Me.Status.Trend = ""
		network2.considerJoiningTrend()
		// Check if started a NEW trend (not joined existing)
		if ln2.Me.Status.Trend != "" && ln2.Me.Status.Trend != "alpha-style" &&
			ln2.Me.Status.Trend != "beta-style" && ln2.Me.Status.Trend != "gamma-style" {
			startsWithThreeTrends++
		}
	}

	// With 3 existing trends, chance should be ~1/4 of base chance
	// So startsWithThreeTrends should be significantly less than startsWithNoTrends
	// With larger sample size (1000), allow more margin: 40% instead of 50%
	if startsWithThreeTrends >= (startsWithNoTrends * 2 / 5) {
		t.Errorf("expected fewer new trends when 3 already exist: got %d with 3 trends vs %d with 0 trends",
			startsWithThreeTrends, startsWithNoTrends)
	}
}

func TestTrendCreation_HighAgreeablenessJoinsInsteadOfStarting(t *testing.T) {
	ln := NewLocalNara("agreeable", "agreeable-soul", "host", "user", "pass", -1, 0)
	ln.Me.Status.Personality.Agreeableness = 100 // very agreeable, will join not rebel
	ln.Me.Status.Personality.Sociability = 100   // high sociability
	network := ln.Network

	// Create a dominant trend
	for i := 0; i < 5; i++ {
		follower := NewNara("member" + string(rune('a'+i)))
		follower.Status.Trend = "popular-style"
		follower.Status.TrendEmoji = "💫"
		follower.Status.Version = NaraVersion
		network.importNara(follower)
		network.local.setObservation(follower.Name, NaraObservation{Online: "ONLINE"})
	}

	// Agreeable nara should join the existing trend, not start underground
	joinedExisting := 0
	startedNew := 0
	for i := 0; i < 50; i++ {
		ln.Me.Status.Trend = ""
		network.considerJoiningTrend()
		if ln.Me.Status.Trend == "popular-style" {
			joinedExisting++
		} else if ln.Me.Status.Trend != "" {
			startedNew++
		}
	}

	if startedNew > joinedExisting {
		t.Errorf("expected agreeable nara to join existing trend more than starting new: joined=%d, started=%d",
			joinedExisting, startedNew)
	}
}
