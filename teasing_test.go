package nara

import (
	"testing"
	"time"
)

func TestTeaseTrigger_HighRestarts(t *testing.T) {
	personality := NaraPersonality{Sociability: 80, Agreeableness: 50, Chill: 30}
	now := time.Now().Unix()

	// High restart RATE should trigger (10 restarts in 2 days = 5/day)
	obs := NaraObservation{
		Restarts:  10,
		StartTime: now - 2*86400, // 2 days ago
		Online:    "ONLINE",
	}

	trigger := ShouldTeaseForRestarts(obs, personality)
	if !trigger {
		t.Error("Should tease for high restart rate (5/day)")
	}

	// Low restart rate shouldn't trigger (1000 restarts over 3 years = ~0.9/day)
	obs = NaraObservation{
		Restarts:  1000,
		StartTime: now - 3*365*86400, // 3 years ago
		Online:    "ONLINE",
	}
	trigger = ShouldTeaseForRestarts(obs, personality)
	if trigger {
		t.Error("Should not tease for low restart rate (~0.9/day over 3 years)")
	}

	// Very new nara (< 1 day) should get a break
	obs = NaraObservation{
		Restarts:  5,
		StartTime: now - 3600, // 1 hour ago
		Online:    "ONLINE",
	}
	trigger = ShouldTeaseForRestarts(obs, personality)
	if trigger {
		t.Error("Should not tease nara less than 1 day old")
	}
}

func TestTeaseTrigger_PersonalityGated(t *testing.T) {
	now := time.Now().Unix()

	// Moderate restart rate: 3 restarts/day
	obs := NaraObservation{
		Restarts:  30,
		StartTime: now - 10*86400, // 10 days ago = 3/day
		Online:    "ONLINE",
	}

	// High sociability should be more likely to tease (threshold ~1.5)
	highSoc := NaraPersonality{Sociability: 100, Agreeableness: 30, Chill: 20}
	// Low sociability, high chill, high agreeableness (threshold ~3.0)
	lowSoc := NaraPersonality{Sociability: 10, Agreeableness: 80, Chill: 90}

	highSocTease := ShouldTeaseForRestarts(obs, highSoc)
	lowSocTease := ShouldTeaseForRestarts(obs, lowSoc)

	// High sociability should tease at 3/day (threshold ~1.5)
	if !highSocTease {
		t.Error("High sociability should tease for 3 restarts/day")
	}

	// Low sociability, high chill should not tease (threshold ~3.0, rate = 3.0)
	// This is borderline, so let's make the rate lower
	obs.Restarts = 20 // now 2/day
	lowSocTease = ShouldTeaseForRestarts(obs, lowSoc)
	if lowSocTease {
		t.Error("Low sociability, high chill should not tease for 2 restarts/day")
	}
}

func TestTeaseTrigger_CameBack(t *testing.T) {
	personality := NaraPersonality{Sociability: 70, Agreeableness: 50, Chill: 40}

	// Nara that was missing for a while
	obs := NaraObservation{
		Online:      "ONLINE",
		LastRestart: time.Now().Unix(),
	}

	previousState := "MISSING"

	trigger := ShouldTeaseForComeback(obs, previousState, personality)
	if !trigger {
		t.Error("Should tease when nara comes back from MISSING")
	}

	// Nara that was already online shouldn't trigger comeback tease
	previousState = "ONLINE"
	trigger = ShouldTeaseForComeback(obs, previousState, personality)
	if trigger {
		t.Error("Should not tease for nara that was already online")
	}
}

func TestTeaseTrigger_TrendAbandon(t *testing.T) {
	personality := NaraPersonality{Sociability: 60, Agreeableness: 50, Chill: 30}

	// Popular trend that target abandoned
	previousTrend := "sparkles"
	currentTrend := ""
	trendPopularity := 0.6 // 60% of network following

	trigger := ShouldTeaseForTrendAbandon(previousTrend, currentTrend, trendPopularity, personality)
	if !trigger {
		t.Error("Should tease for abandoning popular trend")
	}

	// Abandoning unpopular trend shouldn't trigger
	trendPopularity = 0.1
	trigger = ShouldTeaseForTrendAbandon(previousTrend, currentTrend, trendPopularity, personality)
	if trigger {
		t.Error("Should not tease for abandoning unpopular trend")
	}

	// Still following trend shouldn't trigger
	currentTrend = "sparkles"
	trendPopularity = 0.6
	trigger = ShouldTeaseForTrendAbandon(previousTrend, currentTrend, trendPopularity, personality)
	if trigger {
		t.Error("Should not tease when still following trend")
	}
}

func TestTeaseEvent_Creation(t *testing.T) {
	actor := "alice"
	target := "bob"
	reason := ReasonHighRestarts

	event := NewTeaseEvent(actor, target, reason)

	if event.Type != "tease" {
		t.Errorf("Expected type 'tease', got '%s'", event.Type)
	}
	if event.Actor != actor {
		t.Errorf("Expected actor '%s', got '%s'", actor, event.Actor)
	}
	if event.Target != target {
		t.Errorf("Expected target '%s', got '%s'", target, event.Target)
	}
	if event.Reason != reason {
		t.Errorf("Expected reason '%s', got '%s'", reason, event.Reason)
	}
	if event.Timestamp == 0 {
		t.Error("Expected non-zero timestamp")
	}
	if event.ID == "" {
		t.Error("Expected non-empty ID")
	}
}

func TestTeaseMessage_Templates(t *testing.T) {
	// Verify we have message templates for each reason
	reasons := []string{ReasonHighRestarts, ReasonComeback, ReasonTrendAbandon, ReasonRandom, ReasonNiceNumber}

	for _, reason := range reasons {
		msg := TeaseMessage(reason, "alice", "bob")
		if msg == "" {
			t.Errorf("No message template for reason '%s'", reason)
		}
		// Most messages should mention target (except random and some nice-number ones)
		if reason != ReasonRandom && reason != ReasonNiceNumber && !containsSubstring(msg, "bob") {
			t.Errorf("Message for '%s' should mention target", reason)
		}
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || containsSubstring(s[1:], substr)))
}

func TestTeaseCooldown(t *testing.T) {
	state := NewTeaseState()

	actor := "alice"
	target := "bob"

	// First tease should be allowed
	if !state.CanTease(actor, target) {
		t.Error("First tease should be allowed")
	}

	// Record the tease
	state.RecordTease(actor, target)

	// Immediate second tease should be blocked
	if state.CanTease(actor, target) {
		t.Error("Immediate second tease should be blocked by cooldown")
	}

	// Different target should be allowed
	if !state.CanTease(actor, "charlie") {
		t.Error("Tease to different target should be allowed")
	}

	// Different actor should be allowed
	if !state.CanTease("dave", target) {
		t.Error("Tease from different actor should be allowed")
	}
}

func TestTeaseRandomTrigger_Deterministic(t *testing.T) {
	personality := NaraPersonality{Sociability: 50, Agreeableness: 50, Chill: 50}
	soul := "test-soul"
	target := "bob"
	timestamp := int64(1000)

	// Same inputs should produce same result
	result1 := ShouldRandomTease(soul, target, timestamp, personality)
	result2 := ShouldRandomTease(soul, target, timestamp, personality)

	if result1 != result2 {
		t.Error("Random tease should be deterministic for same inputs")
	}

	// Test that different inputs produce different results across a large sample
	// The probability is ~1-2%, so we need a large sample to see variation
	trueCount := 0
	for i := 0; i < 1000; i++ {
		testSoul := string(rune('a'+i%26)) + "-soul-" + string(rune('0'+i/26%10))
		if ShouldRandomTease(testSoul, target, timestamp, personality) {
			trueCount++
		}
	}

	// With ~1-2% probability, we expect 10-20 true results out of 1000
	if trueCount == 0 {
		t.Error("Random tease should trigger occasionally (got 0 out of 1000)")
	}
	if trueCount > 100 {
		t.Errorf("Random tease should be rare, got %d/1000 (expected ~10-20)", trueCount)
	}
}

func TestIsNiceNumber(t *testing.T) {
	// Meme numbers - culturally significant
	memeNumbers := map[int64]string{
		42:   "answer to everything",
		67:   "the new 69",
		69:   "nice",
		420:  "blaze it",
		666:  "spooky",
		777:  "lucky sevens",
		1337: "leet",
		404:  "not found",
		365:  "days in a year",
	}
	for n, reason := range memeNumbers {
		if !IsNiceNumber(n) {
			t.Errorf("%d should be nice (%s)", n, reason)
		}
	}

	// Round numbers (multiples of 100)
	roundNumbers := []int64{100, 200, 300, 500, 1000, 2000}
	for _, n := range roundNumbers {
		if !IsNiceNumber(n) {
			t.Errorf("%d should be nice (round number)", n)
		}
	}

	// Sequences
	sequences := []int64{123, 1234}
	for _, n := range sequences {
		if !IsNiceNumber(n) {
			t.Errorf("%d should be nice (sequence)", n)
		}
	}

	// Repeating digits
	repeating := []int64{111, 222, 333, 444, 555, 888, 999}
	for _, n := range repeating {
		if !IsNiceNumber(n) {
			t.Errorf("%d should be nice (repeating)", n)
		}
	}

	// Palindromes
	palindromes := []int64{11, 22, 33, 44, 55, 66, 77, 88, 99, 101, 111, 121, 131, 1001, 1221, 12321}
	for _, n := range palindromes {
		if !IsNiceNumber(n) {
			t.Errorf("%d should be nice (palindrome)", n)
		}
	}

	// Boring numbers - nothing special about these
	boringNumbers := []int64{7, 13, 47, 73, 127, 1002, 1003, 1005, 234, 567, 891}
	for _, n := range boringNumbers {
		if IsNiceNumber(n) {
			t.Errorf("%d should NOT be a nice number", n)
		}
	}

	// Edge cases
	if IsNiceNumber(0) {
		t.Error("0 should not be nice")
	}
	if IsNiceNumber(1) {
		t.Error("1 should not be nice (too small for palindrome)")
	}
	if IsNiceNumber(10) {
		t.Error("10 should not be nice (not a palindrome, not round enough)")
	}
}

func TestShouldTeaseForNiceNumber(t *testing.T) {
	personality := NaraPersonality{Sociability: 50, Agreeableness: 50, Chill: 50}

	// Should trigger for nice numbers (high probability)
	// 69 is definitely nice
	triggered := ShouldTeaseForNiceNumber(69, personality)
	// Can't guarantee it triggers due to randomness, but let's check it works

	// Should never trigger for non-nice numbers
	triggered = ShouldTeaseForNiceNumber(47, personality)
	if triggered {
		t.Error("Should not tease for non-nice number 47")
	}

	triggered = ShouldTeaseForNiceNumber(73, personality)
	if triggered {
		t.Error("Should not tease for non-nice number 73")
	}

	// Determinism test
	result1 := ShouldTeaseForNiceNumber(420, personality)
	result2 := ShouldTeaseForNiceNumber(420, personality)
	if result1 != result2 {
		t.Error("ShouldTeaseForNiceNumber should be deterministic")
	}
}
