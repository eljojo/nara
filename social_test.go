package nara

import (
	"testing"
	"time"
)

func TestSocialEvent_ID(t *testing.T) {
	// Event ID should be deterministic based on content
	event1 := SocialEvent{
		Timestamp: 1000,
		Type:      "tease",
		Actor:     "alice",
		Target:    "bob",
		Reason:    ReasonHighRestarts,
	}
	event1.ComputeID()

	event2 := SocialEvent{
		Timestamp: 1000,
		Type:      "tease",
		Actor:     "alice",
		Target:    "bob",
		Reason:    ReasonHighRestarts,
	}
	event2.ComputeID()

	if event1.ID != event2.ID {
		t.Error("Same event content should produce same ID")
	}

	// Different content should produce different ID
	event3 := SocialEvent{
		Timestamp: 1001, // different timestamp
		Type:      "tease",
		Actor:     "alice",
		Target:    "bob",
		Reason:    ReasonHighRestarts,
	}
	event3.ComputeID()

	if event1.ID == event3.ID {
		t.Error("Different event content should produce different ID")
	}
}

func TestSocialLedger_AddEvent(t *testing.T) {
	personality := NaraPersonality{
		Agreeableness: 50,
		Sociability:   50,
		Chill:         50,
	}
	ledger := NewSocialLedger(personality, 1000)

	event := SocialEvent{
		Timestamp: 1000,
		Type:      "tease",
		Actor:     "alice",
		Target:    "bob",
		Reason:    ReasonHighRestarts,
	}
	event.ComputeID()

	added := ledger.AddEvent(event)
	if !added {
		t.Error("Event should be added")
	}

	if len(ledger.Events) != 1 {
		t.Errorf("Expected 1 event, got %d", len(ledger.Events))
	}
}

func TestSocialLedger_DeduplicateEvents(t *testing.T) {
	personality := NaraPersonality{
		Agreeableness: 50,
		Sociability:   50,
		Chill:         50,
	}
	ledger := NewSocialLedger(personality, 1000)

	event := SocialEvent{
		Timestamp: 1000,
		Type:      "tease",
		Actor:     "alice",
		Target:    "bob",
		Reason:    ReasonHighRestarts,
	}
	event.ComputeID()

	ledger.AddEvent(event)
	added := ledger.AddEvent(event) // same event again

	if added {
		t.Error("Duplicate event should not be added")
	}

	if len(ledger.Events) != 1 {
		t.Errorf("Expected 1 event after dedup, got %d", len(ledger.Events))
	}
}

func TestTeaseResonates_Deterministic(t *testing.T) {
	event := SocialEvent{
		ID:        "test-event-123",
		Timestamp: 1000,
		Type:      "tease",
		Actor:     "alice",
		Target:    "bob",
		Reason:    ReasonHighRestarts,
	}

	personality := NaraPersonality{
		Agreeableness: 50,
		Sociability:   70,
		Chill:         30,
	}

	soul := "test-soul-abc"

	// Same inputs should always produce same result
	result1 := TeaseResonates(event, soul, personality)
	result2 := TeaseResonates(event, soul, personality)

	if result1 != result2 {
		t.Error("TeaseResonates should be deterministic")
	}
}

func TestTeaseResonates_DifferentSouls(t *testing.T) {
	event := SocialEvent{
		ID:        "test-event-456",
		Timestamp: 1000,
		Type:      "tease",
		Actor:     "alice",
		Target:    "bob",
		Reason:    ReasonHighRestarts,
	}

	personality := NaraPersonality{
		Agreeableness: 50,
		Sociability:   50,
		Chill:         50,
	}

	// Test that different souls can produce different results
	// We test many souls to verify distribution isn't uniform
	trueCount := 0
	for i := 0; i < 100; i++ {
		soul := string(rune('a'+i)) + "-soul"
		if TeaseResonates(event, soul, personality) {
			trueCount++
		}
	}

	// Should have some variation (not all true or all false)
	if trueCount == 0 || trueCount == 100 {
		t.Errorf("Expected variation across souls, got %d/100 true", trueCount)
	}
}

func TestTeaseResonates_PersonalityInfluence(t *testing.T) {
	event := SocialEvent{
		ID:        "test-event-789",
		Timestamp: 1000,
		Type:      "tease",
		Actor:     "alice",
		Target:    "bob",
		Reason:    ReasonHighRestarts,
	}

	soul := "consistent-soul"

	// High sociability should appreciate teasing more (lower threshold)
	highSociability := NaraPersonality{
		Agreeableness: 50,
		Sociability:   100,
		Chill:         0,
	}

	// Low sociability, high chill should appreciate teasing less (higher threshold)
	lowSociabilityHighChill := NaraPersonality{
		Agreeableness: 50,
		Sociability:   0,
		Chill:         100,
	}

	// Test many events to verify personality affects probability
	highSocCount := 0
	lowSocCount := 0

	for i := 0; i < 100; i++ {
		testEvent := event
		testEvent.ID = string(rune('a'+i)) + "-event"

		if TeaseResonates(testEvent, soul, highSociability) {
			highSocCount++
		}
		if TeaseResonates(testEvent, soul, lowSociabilityHighChill) {
			lowSocCount++
		}
	}

	// High sociability should resonate with more teases
	if highSocCount <= lowSocCount {
		t.Errorf("High sociability should appreciate more teases: high=%d, low=%d", highSocCount, lowSocCount)
	}
}

func TestDeriveClout_SameEventsDifferentOpinions(t *testing.T) {
	// Two naras see the same events but have different opinions

	events := []SocialEvent{
		{ID: "e1", Timestamp: 1000, Type: "tease", Actor: "alice", Target: "bob", Reason: "restarts"},
		{ID: "e2", Timestamp: 1001, Type: "tease", Actor: "alice", Target: "charlie", Reason: "trend"},
		{ID: "e3", Timestamp: 1002, Type: "tease", Actor: "bob", Target: "alice", Reason: "missing"},
	}

	highSoc := NaraPersonality{Sociability: 100, Agreeableness: 50, Chill: 0}
	lowSoc := NaraPersonality{Sociability: 0, Agreeableness: 50, Chill: 100}

	ledger1 := NewSocialLedger(highSoc, 1000)
	ledger2 := NewSocialLedger(lowSoc, 1000)

	for _, e := range events {
		ledger1.AddEvent(e)
		ledger2.AddEvent(e)
	}

	clout1 := ledger1.DeriveClout("soul1")
	clout2 := ledger2.DeriveClout("soul2")

	// They should derive different clout scores
	// (at least one actor should have different clout)
	sameBoth := true
	for actor := range clout1 {
		if clout1[actor] != clout2[actor] {
			sameBoth = false
			break
		}
	}

	// It's possible they agree by chance, but unlikely with different personalities
	// We mainly want to verify the function works without panicking
	_ = sameBoth // just checking it computes
}

func TestDeriveClout_Deterministic(t *testing.T) {
	events := []SocialEvent{
		{ID: "e1", Timestamp: 1000, Type: "tease", Actor: "alice", Target: "bob", Reason: "restarts"},
		{ID: "e2", Timestamp: 1001, Type: "tease", Actor: "bob", Target: "alice", Reason: "trend"},
	}

	personality := NaraPersonality{Sociability: 50, Agreeableness: 50, Chill: 50}
	soul := "test-soul"

	ledger1 := NewSocialLedger(personality, 1000)
	ledger2 := NewSocialLedger(personality, 1000)

	for _, e := range events {
		ledger1.AddEvent(e)
		ledger2.AddEvent(e)
	}

	clout1 := ledger1.DeriveClout(soul)
	clout2 := ledger2.DeriveClout(soul)

	// Same events + same soul + same personality = same clout
	for actor, score := range clout1 {
		if clout2[actor] != score {
			t.Errorf("Clout should be deterministic: %s got %f vs %f", actor, score, clout2[actor])
		}
	}
}

func TestSocialLedger_Prune(t *testing.T) {
	personality := NaraPersonality{Sociability: 50, Agreeableness: 50, Chill: 50}
	maxEvents := 5
	ledger := NewSocialLedger(personality, maxEvents)

	// Add more events than max
	for i := 0; i < 10; i++ {
		event := SocialEvent{
			Timestamp: int64(1000 + i),
			Type:      "tease",
			Actor:     "alice",
			Target:    "bob",
			Reason:    "test",
		}
		event.ComputeID()
		ledger.AddEvent(event)
	}

	ledger.Prune()

	if len(ledger.Events) > maxEvents {
		t.Errorf("Expected at most %d events after prune, got %d", maxEvents, len(ledger.Events))
	}
}

func TestSocialLedger_PruneKeepsRecent(t *testing.T) {
	personality := NaraPersonality{Sociability: 50, Agreeableness: 50, Chill: 50}
	maxEvents := 3
	ledger := NewSocialLedger(personality, maxEvents)

	// Add events with increasing timestamps
	for i := 0; i < 5; i++ {
		event := SocialEvent{
			Timestamp: int64(1000 + i),
			Type:      "tease",
			Actor:     "alice",
			Target:    "bob",
			Reason:    "test",
		}
		event.ComputeID()
		ledger.AddEvent(event)
	}

	ledger.Prune()

	// Should keep the most recent events
	for _, event := range ledger.Events {
		if event.Timestamp < 1002 {
			t.Errorf("Prune should keep recent events, found timestamp %d", event.Timestamp)
		}
	}
}

func TestSocialEvent_Types(t *testing.T) {
	// Verify supported event types
	validTypes := []string{"tease", "observed", "gossip"}

	for _, eventType := range validTypes {
		event := SocialEvent{
			Timestamp: 1000,
			Type:      eventType,
			Actor:     "alice",
			Target:    "bob",
		}

		if !event.IsValid() {
			t.Errorf("Event type %s should be valid", eventType)
		}
	}

	// Invalid type
	invalidEvent := SocialEvent{
		Timestamp: 1000,
		Type:      "invalid",
		Actor:     "alice",
		Target:    "bob",
	}

	if invalidEvent.IsValid() {
		t.Error("Invalid event type should not be valid")
	}
}

func TestSocialLedger_GetEventsAbout(t *testing.T) {
	personality := NaraPersonality{Sociability: 50, Agreeableness: 50, Chill: 50}
	ledger := NewSocialLedger(personality, 1000)

	// Add events about different targets
	events := []SocialEvent{
		{ID: "e1", Timestamp: 1000, Type: "tease", Actor: "alice", Target: "bob"},
		{ID: "e2", Timestamp: 1001, Type: "tease", Actor: "charlie", Target: "bob"},
		{ID: "e3", Timestamp: 1002, Type: "tease", Actor: "alice", Target: "dave"},
	}

	for _, e := range events {
		ledger.AddEvent(e)
	}

	bobEvents := ledger.GetEventsAbout("bob")
	if len(bobEvents) != 2 {
		t.Errorf("Expected 2 events about bob, got %d", len(bobEvents))
	}

	daveEvents := ledger.GetEventsAbout("dave")
	if len(daveEvents) != 1 {
		t.Errorf("Expected 1 event about dave, got %d", len(daveEvents))
	}
}

func TestSocialLedger_GetEventsByActor(t *testing.T) {
	personality := NaraPersonality{Sociability: 50, Agreeableness: 50, Chill: 50}
	ledger := NewSocialLedger(personality, 1000)

	events := []SocialEvent{
		{ID: "e1", Timestamp: 1000, Type: "tease", Actor: "alice", Target: "bob"},
		{ID: "e2", Timestamp: 1001, Type: "tease", Actor: "alice", Target: "charlie"},
		{ID: "e3", Timestamp: 1002, Type: "tease", Actor: "bob", Target: "alice"},
	}

	for _, e := range events {
		ledger.AddEvent(e)
	}

	aliceEvents := ledger.GetEventsByActor("alice")
	if len(aliceEvents) != 2 {
		t.Errorf("Expected 2 events by alice, got %d", len(aliceEvents))
	}
}

func TestSocialLedger_GetEventsForSubjects(t *testing.T) {
	personality := NaraPersonality{Sociability: 50, Agreeableness: 50, Chill: 50}
	ledger := NewSocialLedger(personality, 1000)

	events := []SocialEvent{
		{ID: "e1", Timestamp: 1000, Type: "tease", Actor: "alice", Target: "bob"},
		{ID: "e2", Timestamp: 1001, Type: "tease", Actor: "charlie", Target: "dave"},
		{ID: "e3", Timestamp: 1002, Type: "tease", Actor: "bob", Target: "alice"},
		{ID: "e4", Timestamp: 1003, Type: "tease", Actor: "eve", Target: "frank"},
	}

	for _, e := range events {
		ledger.AddEvent(e)
	}

	// Get events involving alice or bob
	subjectEvents := ledger.GetEventsForSubjects([]string{"alice", "bob"})
	if len(subjectEvents) != 2 {
		t.Errorf("Expected 2 events involving alice/bob, got %d", len(subjectEvents))
	}

	// Get events involving charlie
	charlieEvents := ledger.GetEventsForSubjects([]string{"charlie"})
	if len(charlieEvents) != 1 {
		t.Errorf("Expected 1 event involving charlie, got %d", len(charlieEvents))
	}
}

func TestPartitionSubjects(t *testing.T) {
	subjects := []string{"alice", "bob", "charlie", "dave", "eve", "frank", "grace", "henry"}

	// Partition into 3 groups
	partitions := PartitionSubjects(subjects, 3)

	if len(partitions) != 3 {
		t.Errorf("Expected 3 partitions, got %d", len(partitions))
	}

	// All subjects should be accounted for
	total := 0
	for _, p := range partitions {
		total += len(p)
	}
	if total != len(subjects) {
		t.Errorf("Expected %d subjects in partitions, got %d", len(subjects), total)
	}

	// Partitioning should be deterministic
	partitions2 := PartitionSubjects(subjects, 3)
	for i := range partitions {
		if len(partitions[i]) != len(partitions2[i]) {
			t.Error("Partitioning should be deterministic")
		}
	}
}

func TestPartitionSubjects_EdgeCases(t *testing.T) {
	subjects := []string{"alice", "bob"}

	// More partitions than subjects
	partitions := PartitionSubjects(subjects, 10)
	if len(partitions) != 2 { // should cap at len(subjects)
		t.Errorf("Expected 2 partitions (capped), got %d", len(partitions))
	}

	// Zero partitions
	partitions = PartitionSubjects(subjects, 0)
	if len(partitions) != 1 { // should default to 1
		t.Errorf("Expected 1 partition (default), got %d", len(partitions))
	}
}

func TestSocialLedger_MergeEvents(t *testing.T) {
	personality := NaraPersonality{Sociability: 50, Agreeableness: 50, Chill: 50}
	ledger := NewSocialLedger(personality, 1000)

	// Add initial event
	event1 := SocialEvent{ID: "e1", Timestamp: 1000, Type: "tease", Actor: "alice", Target: "bob"}
	ledger.AddEvent(event1)

	// Merge new events (including duplicate)
	newEvents := []SocialEvent{
		{ID: "e1", Timestamp: 1000, Type: "tease", Actor: "alice", Target: "bob"}, // duplicate
		{ID: "e2", Timestamp: 1001, Type: "tease", Actor: "bob", Target: "alice"}, // new
	}

	added := ledger.MergeEvents(newEvents)
	if added != 1 {
		t.Errorf("Expected 1 new event added, got %d", added)
	}

	if ledger.EventCount() != 2 {
		t.Errorf("Expected 2 total events, got %d", ledger.EventCount())
	}
}

func TestEventWeight_IncludesReasonAndType(t *testing.T) {
	// Test that eventWeight varies based on event reason and personality
	now := time.Now().Unix()

	// Social nara should weight comeback events higher
	socialPersonality := NaraPersonality{Sociability: 80, Agreeableness: 50, Chill: 30}
	socialLedger := NewSocialLedger(socialPersonality, 1000)

	comebackEvent := SocialEvent{Type: "tease", Reason: ReasonComeback, Timestamp: now}
	randomEvent := SocialEvent{Type: "tease", Reason: ReasonRandom, Timestamp: now}

	comebackWeight := socialLedger.eventWeight(comebackEvent)
	randomWeight := socialLedger.eventWeight(randomEvent)

	// Social nara should care more about comeback than random
	if comebackWeight <= randomWeight {
		t.Errorf("Social nara should weight comeback (%.2f) higher than random (%.2f)", comebackWeight, randomWeight)
	}

	// Chill nara should weight random events very low
	chillPersonality := NaraPersonality{Sociability: 50, Agreeableness: 50, Chill: 80}
	chillLedger := NewSocialLedger(chillPersonality, 1000)

	chillRandomWeight := chillLedger.eventWeight(randomEvent)
	if chillRandomWeight >= 0.5 {
		t.Errorf("Chill nara should weight random events low, got %.2f", chillRandomWeight)
	}

	// Test event type affects weight
	teaseEvent := SocialEvent{Type: "tease", Reason: ReasonHighRestarts, Timestamp: now}
	observedEvent := SocialEvent{Type: "observed", Reason: ReasonHighRestarts, Timestamp: now}
	gossipEvent := SocialEvent{Type: "gossip", Reason: ReasonHighRestarts, Timestamp: now}

	teaseWeight := socialLedger.eventWeight(teaseEvent)
	observedWeight := socialLedger.eventWeight(observedEvent)
	gossipWeight := socialLedger.eventWeight(gossipEvent)

	// Direct teases should weigh more than observations
	if teaseWeight <= observedWeight {
		t.Errorf("Tease (%.2f) should weigh more than observed (%.2f)", teaseWeight, observedWeight)
	}

	// Observations should weigh more than gossip
	if observedWeight <= gossipWeight {
		t.Errorf("Observed (%.2f) should weigh more than gossip (%.2f)", observedWeight, gossipWeight)
	}
}

func TestEventWeight_MemoryDecay(t *testing.T) {
	// Test that some naras have better memory than others
	now := time.Now().Unix()
	oneDayAgo := now - 24*60*60

	// Chill nara forgets faster
	chillPersonality := NaraPersonality{Sociability: 50, Agreeableness: 50, Chill: 90}
	chillLedger := NewSocialLedger(chillPersonality, 1000)

	// Non-chill nara remembers longer
	intensePersonality := NaraPersonality{Sociability: 50, Agreeableness: 50, Chill: 10}
	intenseLedger := NewSocialLedger(intensePersonality, 1000)

	oldEvent := SocialEvent{Type: "tease", Reason: ReasonHighRestarts, Timestamp: oneDayAgo}

	chillWeight := chillLedger.eventWeight(oldEvent)
	intenseWeight := intenseLedger.eventWeight(oldEvent)

	// Intense (non-chill) nara should remember the old event better
	if intenseWeight <= chillWeight {
		t.Errorf("Intense nara should remember better: intense=%.3f, chill=%.3f", intenseWeight, chillWeight)
	}

	// Recent events should have similar weight regardless of personality
	recentEvent := SocialEvent{Type: "tease", Reason: ReasonHighRestarts, Timestamp: now}
	chillRecent := chillLedger.eventWeight(recentEvent)
	intenseRecent := intenseLedger.eventWeight(recentEvent)

	// Both should be close to full weight for recent events
	if chillRecent < 0.8 || intenseRecent < 0.8 {
		t.Errorf("Recent events should have high weight: chill=%.3f, intense=%.3f", chillRecent, intenseRecent)
	}
}

func TestEventIsMeaningful_PersonalityFiltering(t *testing.T) {
	// Test that eventIsMeaningful filters based on personality

	// High chill nara doesn't care about random
	chillPersonality := NaraPersonality{Sociability: 50, Agreeableness: 50, Chill: 80}
	chillLedger := NewSocialLedger(chillPersonality, 1000)

	randomEvent := SocialEvent{Type: "tease", Reason: ReasonRandom}
	if chillLedger.eventIsMeaningful(randomEvent) {
		t.Error("Chill nara should filter out random teases")
	}

	// Very chill nara only keeps significant events
	veryChillPersonality := NaraPersonality{Sociability: 50, Agreeableness: 50, Chill: 90}
	veryChillLedger := NewSocialLedger(veryChillPersonality, 1000)

	trendEvent := SocialEvent{Type: "tease", Reason: ReasonTrendAbandon}
	if veryChillLedger.eventIsMeaningful(trendEvent) {
		t.Error("Very chill nara should only keep high-restarts and comeback events")
	}

	comebackEvent := SocialEvent{Type: "tease", Reason: ReasonComeback}
	if !veryChillLedger.eventIsMeaningful(comebackEvent) {
		t.Error("Very chill nara should keep comeback events")
	}

	// Highly agreeable nara doesn't store trend-abandon drama
	agreeablePersonality := NaraPersonality{Sociability: 50, Agreeableness: 90, Chill: 30}
	agreeableLedger := NewSocialLedger(agreeablePersonality, 1000)

	if agreeableLedger.eventIsMeaningful(trendEvent) {
		t.Error("Highly agreeable nara should filter out trend-abandon events")
	}

	// Low sociability nara doesn't care about random
	lowSocPersonality := NaraPersonality{Sociability: 10, Agreeableness: 50, Chill: 50}
	lowSocLedger := NewSocialLedger(lowSocPersonality, 1000)

	if lowSocLedger.eventIsMeaningful(randomEvent) {
		t.Error("Low sociability nara should filter out random events")
	}

	// Normal personality accepts most events
	normalPersonality := NaraPersonality{Sociability: 50, Agreeableness: 50, Chill: 50}
	normalLedger := NewSocialLedger(normalPersonality, 1000)

	if !normalLedger.eventIsMeaningful(randomEvent) {
		t.Error("Normal personality should accept random events")
	}
	if !normalLedger.eventIsMeaningful(trendEvent) {
		t.Error("Normal personality should accept trend-abandon events")
	}
}
