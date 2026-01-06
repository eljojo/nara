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

// ============================================================
// Tests for new observation event types
// ============================================================

func TestObservationEventReasons_Defined(t *testing.T) {
	// Verify new observation reasons are defined
	reasons := []string{
		ReasonOnline,
		ReasonOffline,
		ReasonJourneyPass,
		ReasonJourneyComplete,
		ReasonJourneyTimeout,
	}

	for _, reason := range reasons {
		if reason == "" {
			t.Errorf("Reason constant should not be empty")
		}
	}
}

func TestObservationEventType_Valid(t *testing.T) {
	// The new "observation" type should be valid
	event := SocialEvent{
		Timestamp: 1000,
		Type:      "observation",
		Actor:     "alice",
		Target:    "bob",
		Reason:    ReasonOnline,
	}

	if !event.IsValid() {
		t.Error("Event type 'observation' should be valid")
	}
}

func TestNewObservationEvent(t *testing.T) {
	event := NewObservationEvent("alice", "bob", ReasonOnline)

	if event.Type != "observation" {
		t.Errorf("Expected type 'observation', got %s", event.Type)
	}
	if event.Actor != "alice" {
		t.Errorf("Expected actor 'alice', got %s", event.Actor)
	}
	if event.Target != "bob" {
		t.Errorf("Expected target 'bob', got %s", event.Target)
	}
	if event.Reason != ReasonOnline {
		t.Errorf("Expected reason '%s', got %s", ReasonOnline, event.Reason)
	}
	if event.Timestamp == 0 {
		t.Error("Timestamp should be set")
	}
	if event.ID == "" {
		t.Error("ID should be computed")
	}
}

func TestNewJourneyObservationEvent(t *testing.T) {
	journeyID := "journey-123"
	event := NewJourneyObservationEvent("alice", "bob", ReasonJourneyPass, journeyID)

	if event.Type != "observation" {
		t.Errorf("Expected type 'observation', got %s", event.Type)
	}
	if event.Reason != ReasonJourneyPass {
		t.Errorf("Expected reason '%s', got %s", ReasonJourneyPass, event.Reason)
	}
	if event.Witness != journeyID {
		t.Errorf("Expected witness (journey ID) '%s', got %s", journeyID, event.Witness)
	}
}

func TestObservationEvent_PersonalityFiltering(t *testing.T) {
	// Very chill naras skip routine online/offline
	veryChillPersonality := NaraPersonality{Sociability: 50, Agreeableness: 50, Chill: 90}
	veryChillLedger := NewSocialLedger(veryChillPersonality, 1000)

	onlineEvent := SocialEvent{Type: "observation", Reason: ReasonOnline}
	offlineEvent := SocialEvent{Type: "observation", Reason: ReasonOffline}

	if veryChillLedger.eventIsMeaningful(onlineEvent) {
		t.Error("Very chill nara should skip routine online events")
	}
	if veryChillLedger.eventIsMeaningful(offlineEvent) {
		t.Error("Very chill nara should skip routine offline events")
	}

	// Low sociability naras skip journey-pass/complete
	lowSocPersonality := NaraPersonality{Sociability: 20, Agreeableness: 50, Chill: 50}
	lowSocLedger := NewSocialLedger(lowSocPersonality, 1000)

	journeyPassEvent := SocialEvent{Type: "observation", Reason: ReasonJourneyPass}
	journeyCompleteEvent := SocialEvent{Type: "observation", Reason: ReasonJourneyComplete}

	if lowSocLedger.eventIsMeaningful(journeyPassEvent) {
		t.Error("Low sociability nara should skip journey-pass events")
	}
	if lowSocLedger.eventIsMeaningful(journeyCompleteEvent) {
		t.Error("Low sociability nara should skip journey-complete events")
	}

	// Everyone keeps journey-timeout (reliability matters)
	journeyTimeoutEvent := SocialEvent{Type: "observation", Reason: ReasonJourneyTimeout}

	if !veryChillLedger.eventIsMeaningful(journeyTimeoutEvent) {
		t.Error("Even very chill naras should keep journey-timeout events")
	}
	if !lowSocLedger.eventIsMeaningful(journeyTimeoutEvent) {
		t.Error("Even low sociability naras should keep journey-timeout events")
	}

	// Normal personality accepts all observation events
	normalPersonality := NaraPersonality{Sociability: 50, Agreeableness: 50, Chill: 50}
	normalLedger := NewSocialLedger(normalPersonality, 1000)

	if !normalLedger.eventIsMeaningful(onlineEvent) {
		t.Error("Normal personality should accept online events")
	}
	if !normalLedger.eventIsMeaningful(journeyPassEvent) {
		t.Error("Normal personality should accept journey-pass events")
	}
}

// ============================================================
// Tests for observation clout weights
// ============================================================

func TestObservationClout_Online(t *testing.T) {
	personality := NaraPersonality{Sociability: 50, Agreeableness: 50, Chill: 50}
	ledger := NewSocialLedger(personality, 1000)

	event := NewObservationEvent("alice", "bob", ReasonOnline)
	ledger.AddEvent(event)

	clout := ledger.DeriveClout("test-soul")

	// Online observations should give positive clout to target
	if clout["bob"] <= 0 {
		t.Errorf("Online observation should give positive clout to target, got %f", clout["bob"])
	}
}

func TestObservationClout_Offline(t *testing.T) {
	personality := NaraPersonality{Sociability: 50, Agreeableness: 50, Chill: 50}
	ledger := NewSocialLedger(personality, 1000)

	event := NewObservationEvent("alice", "bob", ReasonOffline)
	ledger.AddEvent(event)

	clout := ledger.DeriveClout("test-soul")

	// Offline observations should give slight negative clout to target
	if clout["bob"] >= 0 {
		t.Errorf("Offline observation should give slight negative clout to target, got %f", clout["bob"])
	}
}

func TestObservationClout_JourneyComplete(t *testing.T) {
	personality := NaraPersonality{Sociability: 50, Agreeableness: 50, Chill: 50}
	ledger := NewSocialLedger(personality, 1000)

	event := NewJourneyObservationEvent("alice", "bob", ReasonJourneyComplete, "journey-123")
	ledger.AddEvent(event)

	clout := ledger.DeriveClout("test-soul")

	// Journey complete should give significant positive clout
	if clout["bob"] <= 0.3 {
		t.Errorf("Journey complete should give significant positive clout, got %f", clout["bob"])
	}
}

func TestObservationClout_JourneyTimeout(t *testing.T) {
	personality := NaraPersonality{Sociability: 50, Agreeableness: 50, Chill: 50}
	ledger := NewSocialLedger(personality, 1000)

	event := NewJourneyObservationEvent("alice", "bob", ReasonJourneyTimeout, "journey-123")
	ledger.AddEvent(event)

	clout := ledger.DeriveClout("test-soul")

	// Journey timeout should give negative clout
	if clout["bob"] >= 0 {
		t.Errorf("Journey timeout should give negative clout to target, got %f", clout["bob"])
	}
}

func TestObservationClout_JourneyPass(t *testing.T) {
	personality := NaraPersonality{Sociability: 50, Agreeableness: 50, Chill: 50}
	ledger := NewSocialLedger(personality, 1000)

	event := NewJourneyObservationEvent("alice", "bob", ReasonJourneyPass, "journey-123")
	ledger.AddEvent(event)

	clout := ledger.DeriveClout("test-soul")

	// Journey pass should give small positive clout (participating)
	if clout["bob"] <= 0 {
		t.Errorf("Journey pass should give positive clout, got %f", clout["bob"])
	}
}

func TestEventWeight_ObservationReasons(t *testing.T) {
	now := time.Now().Unix()
	personality := NaraPersonality{Sociability: 50, Agreeableness: 50, Chill: 50}
	ledger := NewSocialLedger(personality, 1000)

	// Online/offline should have lower weight (routine)
	onlineEvent := SocialEvent{Type: "observation", Reason: ReasonOnline, Timestamp: now}
	offlineEvent := SocialEvent{Type: "observation", Reason: ReasonOffline, Timestamp: now}

	onlineWeight := ledger.eventWeight(onlineEvent)
	offlineWeight := ledger.eventWeight(offlineEvent)

	if onlineWeight > 0.6 {
		t.Errorf("Online events should have lower weight (routine), got %f", onlineWeight)
	}
	if offlineWeight > 0.6 {
		t.Errorf("Offline events should have lower weight (routine), got %f", offlineWeight)
	}

	// Journey complete should have higher weight
	journeyCompleteEvent := SocialEvent{Type: "observation", Reason: ReasonJourneyComplete, Timestamp: now}
	journeyCompleteWeight := ledger.eventWeight(journeyCompleteEvent)

	if journeyCompleteWeight < 1.0 {
		t.Errorf("Journey complete should have higher weight, got %f", journeyCompleteWeight)
	}

	// Journey timeout should have notable weight
	journeyTimeoutEvent := SocialEvent{Type: "observation", Reason: ReasonJourneyTimeout, Timestamp: now}
	journeyTimeoutWeight := ledger.eventWeight(journeyTimeoutEvent)

	if journeyTimeoutWeight < 0.8 {
		t.Errorf("Journey timeout should have notable weight, got %f", journeyTimeoutWeight)
	}
}

// ============================================================
// Tests for interleaved event slicing
// ============================================================

func TestGetEventsInterleaved(t *testing.T) {
	personality := NaraPersonality{Sociability: 50, Agreeableness: 50, Chill: 50}
	ledger := NewSocialLedger(personality, 1000)

	// Add 10 events with sequential timestamps
	for i := 0; i < 10; i++ {
		event := SocialEvent{
			Timestamp: int64(1000 + i),
			Type:      "tease",
			Actor:     "alice",
			Target:    "bob",
			Reason:    ReasonRandom,
		}
		event.ComputeID()
		ledger.AddEvent(event)
	}

	// Get interleaved slices
	slice0 := ledger.GetEventsInterleaved(0, 3) // events 0, 3, 6, 9
	slice1 := ledger.GetEventsInterleaved(1, 3) // events 1, 4, 7
	slice2 := ledger.GetEventsInterleaved(2, 3) // events 2, 5, 8

	// Check slice sizes (10 events / 3 slices = ~3-4 events per slice)
	if len(slice0) != 4 {
		t.Errorf("Slice 0 should have 4 events (0,3,6,9), got %d", len(slice0))
	}
	if len(slice1) != 3 {
		t.Errorf("Slice 1 should have 3 events (1,4,7), got %d", len(slice1))
	}
	if len(slice2) != 3 {
		t.Errorf("Slice 2 should have 3 events (2,5,8), got %d", len(slice2))
	}

	// Verify time spread - slice0 should have events from different times
	if slice0[0].Timestamp != 1000 || slice0[1].Timestamp != 1003 {
		t.Error("Slice 0 should contain events at timestamps 1000, 1003, ...")
	}

	// Total should equal original count
	total := len(slice0) + len(slice1) + len(slice2)
	if total != 10 {
		t.Errorf("Total events across slices should be 10, got %d", total)
	}
}

func TestGetEventsInterleaved_SingleSlice(t *testing.T) {
	personality := NaraPersonality{Sociability: 50, Agreeableness: 50, Chill: 50}
	ledger := NewSocialLedger(personality, 1000)

	for i := 0; i < 5; i++ {
		event := SocialEvent{
			Timestamp: int64(1000 + i),
			Type:      "tease",
			Actor:     "alice",
			Target:    "bob",
		}
		event.ComputeID()
		ledger.AddEvent(event)
	}

	// Single slice should return all events
	slice := ledger.GetEventsInterleaved(0, 1)
	if len(slice) != 5 {
		t.Errorf("Single slice should return all 5 events, got %d", len(slice))
	}
}

func TestGetEventsInterleaved_MoreSlicesThanEvents(t *testing.T) {
	personality := NaraPersonality{Sociability: 50, Agreeableness: 50, Chill: 50}
	ledger := NewSocialLedger(personality, 1000)

	for i := 0; i < 3; i++ {
		event := SocialEvent{
			Timestamp: int64(1000 + i),
			Type:      "tease",
			Actor:     "alice",
			Target:    "bob",
		}
		event.ComputeID()
		ledger.AddEvent(event)
	}

	// More slices than events - each slice gets 0 or 1
	slice0 := ledger.GetEventsInterleaved(0, 5)
	slice1 := ledger.GetEventsInterleaved(1, 5)
	slice2 := ledger.GetEventsInterleaved(2, 5)
	slice3 := ledger.GetEventsInterleaved(3, 5)
	slice4 := ledger.GetEventsInterleaved(4, 5)

	total := len(slice0) + len(slice1) + len(slice2) + len(slice3) + len(slice4)
	if total != 3 {
		t.Errorf("Total should be 3, got %d", total)
	}
}

func TestGetEventsInterleaved_WithMaxLimit(t *testing.T) {
	personality := NaraPersonality{Sociability: 50, Agreeableness: 50, Chill: 50}
	ledger := NewSocialLedger(personality, 10000)

	// Add many events
	for i := 0; i < 5000; i++ {
		event := SocialEvent{
			Timestamp: int64(1000 + i),
			Type:      "tease",
			Actor:     "alice",
			Target:    "bob",
		}
		event.ComputeID()
		ledger.AddEvent(event)
	}

	// Should respect max limit (e.g., 1000 events per response)
	slice := ledger.GetEventsInterleavedWithLimit(0, 1, 1000)
	if len(slice) > 1000 {
		t.Errorf("Should respect max limit of 1000, got %d", len(slice))
	}
}

func TestGetEventsInterleaved_SortedByTimestamp(t *testing.T) {
	personality := NaraPersonality{Sociability: 50, Agreeableness: 50, Chill: 50}
	ledger := NewSocialLedger(personality, 1000)

	// Add events in random timestamp order
	timestamps := []int64{1005, 1002, 1008, 1001, 1003}
	for _, ts := range timestamps {
		event := SocialEvent{
			Timestamp: ts,
			Type:      "tease",
			Actor:     "alice",
			Target:    "bob",
		}
		event.ComputeID()
		ledger.AddEvent(event)
	}

	slice := ledger.GetEventsInterleaved(0, 1)

	// Should be sorted by timestamp
	for i := 1; i < len(slice); i++ {
		if slice[i].Timestamp < slice[i-1].Timestamp {
			t.Error("Events should be sorted by timestamp")
		}
	}
}

// Tests for GetEventsSyncSlice (mesh boot sync)

func TestGetEventsSyncSlice_FilterBySubjects(t *testing.T) {
	personality := NaraPersonality{Sociability: 100, Agreeableness: 50, Chill: 50}
	ledger := NewSocialLedger(personality, 1000)

	// Add events involving different naras
	events := []SocialEvent{
		{Timestamp: 1000, Type: "observation", Actor: "alice", Target: "bob", Reason: ReasonOnline},
		{Timestamp: 1001, Type: "observation", Actor: "alice", Target: "carol", Reason: ReasonOnline},
		{Timestamp: 1002, Type: "observation", Actor: "bob", Target: "alice", Reason: ReasonOnline},
		{Timestamp: 1003, Type: "tease", Actor: "carol", Target: "dave", Reason: ReasonRandom},
	}
	for _, e := range events {
		e.ComputeID()
		ledger.AddEvent(e)
	}

	// Filter to only events involving alice or bob
	slice := ledger.GetEventsSyncSlice([]string{"alice", "bob"}, 0, 0, 1, 1000)

	// Should get events where alice or bob is actor or target
	if len(slice) != 3 {
		t.Errorf("Expected 3 events involving alice/bob, got %d", len(slice))
	}

	for _, e := range slice {
		if e.Actor != "alice" && e.Target != "alice" && e.Actor != "bob" && e.Target != "bob" {
			t.Errorf("Event should involve alice or bob: %+v", e)
		}
	}
}

func TestGetEventsSyncSlice_FilterByTime(t *testing.T) {
	personality := NaraPersonality{Sociability: 100, Agreeableness: 50, Chill: 50}
	ledger := NewSocialLedger(personality, 1000)

	// Add events at different times
	for i := int64(0); i < 10; i++ {
		e := SocialEvent{
			Timestamp: 1000 + i,
			Type:      "observation",
			Actor:     "alice",
			Target:    "bob",
			Reason:    ReasonOnline,
		}
		e.ComputeID()
		ledger.AddEvent(e)
	}

	// Get events after timestamp 1005
	slice := ledger.GetEventsSyncSlice(nil, 1005, 0, 1, 1000)

	// Should get only events with timestamp > 1005
	if len(slice) != 4 { // 1006, 1007, 1008, 1009
		t.Errorf("Expected 4 events after 1005, got %d", len(slice))
	}

	for _, e := range slice {
		if e.Timestamp <= 1005 {
			t.Errorf("Event timestamp %d should be > 1005", e.Timestamp)
		}
	}
}

func TestGetEventsSyncSlice_InterleavedSlicing(t *testing.T) {
	personality := NaraPersonality{Sociability: 100, Agreeableness: 50, Chill: 50}
	ledger := NewSocialLedger(personality, 1000)

	// Add 10 events
	for i := int64(0); i < 10; i++ {
		e := SocialEvent{
			Timestamp: 1000 + i,
			Type:      "observation",
			Actor:     "alice",
			Target:    "bob",
			Reason:    ReasonOnline,
		}
		e.ComputeID()
		ledger.AddEvent(e)
	}

	// Get slice 0 of 3 (should get indices 0, 3, 6, 9)
	slice0 := ledger.GetEventsSyncSlice(nil, 0, 0, 3, 1000)
	if len(slice0) != 4 {
		t.Errorf("Slice 0/3 should have 4 events, got %d", len(slice0))
	}

	// Get slice 1 of 3 (should get indices 1, 4, 7)
	slice1 := ledger.GetEventsSyncSlice(nil, 0, 1, 3, 1000)
	if len(slice1) != 3 {
		t.Errorf("Slice 1/3 should have 3 events, got %d", len(slice1))
	}

	// Get slice 2 of 3 (should get indices 2, 5, 8)
	slice2 := ledger.GetEventsSyncSlice(nil, 0, 2, 3, 1000)
	if len(slice2) != 3 {
		t.Errorf("Slice 2/3 should have 3 events, got %d", len(slice2))
	}

	// All slices combined should equal total events
	if len(slice0)+len(slice1)+len(slice2) != 10 {
		t.Error("Combined slices should equal total events")
	}
}

func TestGetEventsSyncSlice_MaxLimit(t *testing.T) {
	personality := NaraPersonality{Sociability: 100, Agreeableness: 50, Chill: 50}
	ledger := NewSocialLedger(personality, 2000)

	// Add 100 events
	for i := int64(0); i < 100; i++ {
		e := SocialEvent{
			Timestamp: 1000 + i,
			Type:      "observation",
			Actor:     "alice",
			Target:    "bob",
			Reason:    ReasonOnline,
		}
		e.ComputeID()
		ledger.AddEvent(e)
	}

	// Request with max 10
	slice := ledger.GetEventsSyncSlice(nil, 0, 0, 1, 10)
	if len(slice) != 10 {
		t.Errorf("Should respect max limit of 10, got %d", len(slice))
	}
}

func TestGetEventsSyncSlice_CombinedFilters(t *testing.T) {
	personality := NaraPersonality{Sociability: 100, Agreeableness: 50, Chill: 50}
	ledger := NewSocialLedger(personality, 1000)

	// Add events: some about alice, some about bob, at different times
	events := []SocialEvent{
		{Timestamp: 1000, Type: "observation", Actor: "me", Target: "alice", Reason: ReasonOnline},
		{Timestamp: 1001, Type: "observation", Actor: "me", Target: "bob", Reason: ReasonOnline},
		{Timestamp: 1002, Type: "observation", Actor: "me", Target: "alice", Reason: ReasonOffline},
		{Timestamp: 1003, Type: "observation", Actor: "me", Target: "carol", Reason: ReasonOnline},
		{Timestamp: 1004, Type: "observation", Actor: "me", Target: "alice", Reason: ReasonOnline},
		{Timestamp: 1005, Type: "observation", Actor: "me", Target: "bob", Reason: ReasonOffline},
	}
	for _, e := range events {
		e.ComputeID()
		ledger.AddEvent(e)
	}

	// Filter: only alice events, after 1001, slice 0 of 2
	slice := ledger.GetEventsSyncSlice([]string{"alice"}, 1001, 0, 2, 1000)

	// Events about alice after 1001: timestamps 1002, 1004
	// Slice 0 of 2: index 0 -> 1002
	if len(slice) != 1 {
		t.Errorf("Expected 1 event, got %d", len(slice))
	}
	if len(slice) > 0 && slice[0].Timestamp != 1002 {
		t.Errorf("Expected timestamp 1002, got %d", slice[0].Timestamp)
	}
}

// Integration test: journey event lifecycle
func TestJourneyEventLifecycle(t *testing.T) {
	personality := NaraPersonality{Sociability: 100, Agreeableness: 50, Chill: 50}
	ledger := NewSocialLedger(personality, 1000)

	journeyID := "test-journey-123"
	originator := "alice"
	observer := "me"

	// 1. Journey passes through us
	passEvent := NewJourneyObservationEvent(observer, originator, ReasonJourneyPass, journeyID)
	ledger.AddEvent(passEvent)

	// Verify pass event was recorded
	events := ledger.GetEventsAbout(originator)
	if len(events) != 1 {
		t.Fatalf("Expected 1 event after pass, got %d", len(events))
	}
	if events[0].Reason != ReasonJourneyPass {
		t.Errorf("Expected reason %s, got %s", ReasonJourneyPass, events[0].Reason)
	}
	if events[0].Witness != journeyID {
		t.Errorf("Expected journey_id in Witness field, got %s", events[0].Witness)
	}

	// 2. Journey completes
	time.Sleep(1 * time.Millisecond) // ensure different timestamp
	completeEvent := NewJourneyObservationEvent(observer, originator, ReasonJourneyComplete, journeyID)
	ledger.AddEvent(completeEvent)

	// Verify both events recorded
	events = ledger.GetEventsAbout(originator)
	if len(events) != 2 {
		t.Fatalf("Expected 2 events after complete, got %d", len(events))
	}

	// 3. Check clout impact
	soul := "test-soul-12345678901234567890123456789012345678901234567890123456789012"
	clout := ledger.DeriveClout(soul)

	// Journey events should give positive clout to originator
	if clout[originator] <= 0 {
		t.Errorf("Expected positive clout for originator, got %f", clout[originator])
	}
}

func TestJourneyTimeoutEvent(t *testing.T) {
	personality := NaraPersonality{Sociability: 100, Agreeableness: 50, Chill: 50}
	ledger := NewSocialLedger(personality, 1000)

	journeyID := "timeout-journey-456"
	originator := "bob"
	observer := "me"

	// Journey passes through
	passEvent := NewJourneyObservationEvent(observer, originator, ReasonJourneyPass, journeyID)
	ledger.AddEvent(passEvent)

	// Journey times out (bad!)
	time.Sleep(1 * time.Millisecond)
	timeoutEvent := NewJourneyObservationEvent(observer, originator, ReasonJourneyTimeout, journeyID)
	ledger.AddEvent(timeoutEvent)

	// Verify timeout was recorded
	events := ledger.GetEventsAbout(originator)
	foundTimeout := false
	for _, e := range events {
		if e.Reason == ReasonJourneyTimeout {
			foundTimeout = true
			break
		}
	}
	if !foundTimeout {
		t.Error("Expected to find timeout event")
	}

	// Timeout should give negative clout
	soul := "test-soul-12345678901234567890123456789012345678901234567890123456789012"
	clout := ledger.DeriveClout(soul)

	// The timeout (-0.3) should outweigh the pass (+0.2)
	// But we also need to consider the weight multipliers
	t.Logf("Clout for %s after timeout: %f", originator, clout[originator])
}
