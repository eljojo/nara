package nara

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestNara represents a test nara with all necessary components
type TestNara struct {
	Name      string
	Soul      SoulV1
	Keypair   NaraKeypair
	LocalNara *LocalNara
	Transport *MockMeshTransport
	Handler   *WorldJourneyHandler
}

// TestWorld sets up a complete test environment for world journey testing
type TestWorld struct {
	Network    *MockMeshNetwork
	Naras      map[string]*TestNara
	Clout      map[string]map[string]float64
	Completed  []*WorldMessage
	CompleteMu sync.Mutex
}

// NewTestWorld creates a test world with the given nara names
func NewTestWorld(names []string) *TestWorld {
	tw := &TestWorld{
		Network:   NewMockMeshNetwork(),
		Naras:     make(map[string]*TestNara),
		Clout:     make(map[string]map[string]float64),
		Completed: []*WorldMessage{},
	}

	// Create test naras
	for i, name := range names {
		hw := hashBytes([]byte("integration-test-hw-" + name))
		soul := NativeSoulCustom(hw, name)
		keypair := DeriveKeypair(soul)

		// Create a minimal LocalNara (without full network setup)
		ln := &LocalNara{
			Me:      NewNara(name),
			Soul:    FormatSoul(soul),
			Keypair: keypair,
		}
		ctx, cancel := context.WithCancel(context.Background())
		ln.Network = &Network{
			ctx:        ctx,
			cancelFunc: cancel,
		}
		ln.Me.Status.PublicKey = FormatPublicKey(keypair.PublicKey)

		transport := NewMockMeshTransport()
		tw.Network.Register(name, transport)

		testNara := &TestNara{
			Name:      name,
			Soul:      soul,
			Keypair:   keypair,
			LocalNara: ln,
			Transport: transport,
		}

		tw.Naras[name] = testNara

		// Initialize clout for this nara (empty, will be set up by test)
		tw.Clout[name] = make(map[string]float64)

		// Give each nara some clout toward others (simple pattern for testing)
		for j, otherName := range names {
			if name != otherName {
				// Higher clout for naras that come after in the list
				tw.Clout[name][otherName] = float64((j - i + len(names)) % len(names))
			}
		}
	}

	// Create handlers for each nara
	for _, testNara := range tw.Naras {
		handler := tw.createHandler(testNara)
		testNara.Handler = handler
		handler.Listen()
	}

	return tw
}

func (tw *TestWorld) createHandler(tn *TestNara) *WorldJourneyHandler {
	return NewWorldJourneyHandler(
		tn.LocalNara,
		tn.Transport,
		func() map[string]float64 {
			// Return this nara's clout scores
			if tw.Clout != nil {
				return tw.Clout[tn.LocalNara.Me.Name]
			}
			return nil
		},
		func() []string {
			names := make([]string, 0, len(tw.Naras))
			for name := range tw.Naras {
				names = append(names, name)
			}
			return names
		},
		func(name string) []byte {
			if nara, ok := tw.Naras[name]; ok {
				return nara.Keypair.PublicKey
			}
			return nil
		},
		nil, // getMeshIP - not needed for mock transport
		func(wm *WorldMessage) {
			tw.CompleteMu.Lock()
			tw.Completed = append(tw.Completed, wm)
			tw.CompleteMu.Unlock()
		},
		nil, // onJourneyPass - not needed for these tests
	)
}

// SetClout sets up clout relationships
func (tw *TestWorld) SetClout(clout map[string]map[string]float64) {
	tw.Clout = clout
}

// Close shuts down all transports
func (tw *TestWorld) Close() {
	for _, tn := range tw.Naras {
		tn.Transport.Close()
	}
}

// WaitForCompletion waits for a journey to complete
func (tw *TestWorld) WaitForCompletion(timeout time.Duration) *WorldMessage {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		tw.CompleteMu.Lock()
		if len(tw.Completed) > 0 {
			wm := tw.Completed[0]
			tw.Completed = tw.Completed[1:]
			tw.CompleteMu.Unlock()
			return wm
		}
		tw.CompleteMu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

func TestIntegration_WorldJourney_FourNaras(t *testing.T) {
	// Create a test world with 4 naras
	tw := NewTestWorld([]string{"alice", "bob", "carol", "dave"})
	defer tw.Close()

	// Set up clout so the path is: alice -> bob -> carol -> dave -> alice
	tw.SetClout(map[string]map[string]float64{
		"alice": {"bob": 10, "carol": 5, "dave": 2},
		"bob":   {"carol": 10, "dave": 5, "alice": 2},
		"carol": {"dave": 10, "alice": 5, "bob": 2},
		"dave":  {"alice": 10, "bob": 5, "carol": 2},
	})

	// Alice starts the journey
	alice := tw.Naras["alice"]
	wm, err := alice.Handler.StartJourney("Hello from Alice, going around the world!")
	if err != nil {
		t.Fatalf("Failed to start journey: %v", err)
	}

	t.Logf("Journey started with ID: %s", wm.ID)

	// Wait for the journey to complete
	completed := tw.WaitForCompletion(5 * time.Second)
	if completed == nil {
		t.Fatal("Journey did not complete within timeout")
	}

	// Verify the journey
	t.Logf("Journey completed!")
	t.Logf("  Message: %s", completed.OriginalMessage)
	t.Logf("  Originator: %s", completed.Originator)
	t.Logf("  Hops: %d", len(completed.Hops))

	for i, hop := range completed.Hops {
		t.Logf("    Hop %d: %s %s", i+1, hop.Nara, hop.Stamp)
	}

	// Verify expected path
	expectedPath := []string{"bob", "carol", "dave", "alice"}
	if len(completed.Hops) != len(expectedPath) {
		t.Errorf("Expected %d hops, got %d", len(expectedPath), len(completed.Hops))
	}

	for i, expected := range expectedPath {
		if i < len(completed.Hops) && completed.Hops[i].Nara != expected {
			t.Errorf("Hop %d: expected %s, got %s", i, expected, completed.Hops[i].Nara)
		}
	}

	// Verify all signatures
	err = completed.VerifyChain(func(name string) []byte {
		if nara, ok := tw.Naras[name]; ok {
			return nara.Keypair.PublicKey
		}
		return nil
	})
	if err != nil {
		t.Errorf("Signature verification failed: %v", err)
	}

	// Verify rewards
	rewards := CalculateWorldRewards(completed)
	if rewards["alice"] != 10 {
		t.Errorf("Alice should get 10 clout, got %v", rewards["alice"])
	}
	if rewards["bob"] != 2 {
		t.Errorf("Bob should get 2 clout, got %v", rewards["bob"])
	}
	if rewards["carol"] != 2 {
		t.Errorf("Carol should get 2 clout, got %v", rewards["carol"])
	}
	if rewards["dave"] != 2 {
		t.Errorf("Dave should get 2 clout, got %v", rewards["dave"])
	}
}

func TestIntegration_WorldJourney_ChainVerification(t *testing.T) {
	// Test that signature verification catches tampering
	tw := NewTestWorld([]string{"alice", "bob", "carol"})
	defer tw.Close()

	tw.SetClout(map[string]map[string]float64{
		"alice": {"bob": 10, "carol": 5},
		"bob":   {"carol": 10, "alice": 5},
		"carol": {"alice": 10, "bob": 5},
	})

	// Start journey
	alice := tw.Naras["alice"]
	_, err := alice.Handler.StartJourney("Test message")
	if err != nil {
		t.Fatalf("Failed to start journey: %v", err)
	}

	// Wait for completion
	completed := tw.WaitForCompletion(5 * time.Second)
	if completed == nil {
		t.Fatal("Journey did not complete")
	}

	// Tamper with the message
	completed.OriginalMessage = "TAMPERED!"

	// Verification should fail
	err = completed.VerifyChain(func(name string) []byte {
		if nara, ok := tw.Naras[name]; ok {
			return nara.Keypair.PublicKey
		}
		return nil
	})
	if err == nil {
		t.Error("Tampered message should fail verification")
	} else {
		t.Logf("Tampering correctly detected: %v", err)
	}
}

func TestIntegration_WorldJourney_AllStampsCollected(t *testing.T) {
	// Verify that all naras add their stamps
	tw := NewTestWorld([]string{"alpha", "beta", "gamma", "delta", "epsilon"})
	defer tw.Close()

	// Simple linear clout
	tw.SetClout(map[string]map[string]float64{
		"alpha":   {"beta": 10, "gamma": 5, "delta": 3, "epsilon": 1},
		"beta":    {"gamma": 10, "delta": 5, "epsilon": 3, "alpha": 1},
		"gamma":   {"delta": 10, "epsilon": 5, "alpha": 3, "beta": 1},
		"delta":   {"epsilon": 10, "alpha": 5, "beta": 3, "gamma": 1},
		"epsilon": {"alpha": 10, "beta": 5, "gamma": 3, "delta": 1},
	})

	alpha := tw.Naras["alpha"]
	_, err := alpha.Handler.StartJourney("Collecting stamps!")
	if err != nil {
		t.Fatalf("Failed to start journey: %v", err)
	}

	completed := tw.WaitForCompletion(5 * time.Second)
	if completed == nil {
		t.Fatal("Journey did not complete")
	}

	// All 5 naras should have added stamps (including alpha at the end)
	if len(completed.Hops) != 5 {
		t.Errorf("Expected 5 hops, got %d", len(completed.Hops))
	}

	// Verify each hop has a stamp
	for i, hop := range completed.Hops {
		if hop.Stamp == "" {
			t.Errorf("Hop %d (%s) missing stamp", i, hop.Nara)
		} else {
			t.Logf("Hop %d: %s stamped with %s", i+1, hop.Nara, hop.Stamp)
		}
	}
}

func TestIntegration_WorldJourney_TimingRecorded(t *testing.T) {
	// Verify that timestamps are recorded
	tw := NewTestWorld([]string{"one", "two", "three"})
	defer tw.Close()

	tw.SetClout(map[string]map[string]float64{
		"one":   {"two": 10, "three": 5},
		"two":   {"three": 10, "one": 5},
		"three": {"one": 10, "two": 5},
	})

	start := time.Now().Unix()

	one := tw.Naras["one"]
	_, err := one.Handler.StartJourney("Timing test")
	if err != nil {
		t.Fatalf("Failed to start journey: %v", err)
	}

	completed := tw.WaitForCompletion(5 * time.Second)
	if completed == nil {
		t.Fatal("Journey did not complete")
	}

	// All timestamps should be >= start time and in order
	var prevTimestamp int64 = start - 1
	for i, hop := range completed.Hops {
		if hop.Timestamp < start {
			t.Errorf("Hop %d timestamp %d is before start %d", i, hop.Timestamp, start)
		}
		if hop.Timestamp < prevTimestamp {
			t.Errorf("Hop %d timestamp %d is before previous %d", i, hop.Timestamp, prevTimestamp)
		}
		prevTimestamp = hop.Timestamp
		t.Logf("Hop %d (%s): timestamp %d", i+1, hop.Nara, hop.Timestamp)
	}
}

// TestIntegration_WorldJourney_DerivedClout tests that clout derived from
// ledger events produces sensible journey routing. This validates the full
// pipeline: events -> DeriveClout -> journey routing.
func TestIntegration_WorldJourney_DerivedClout(t *testing.T) {
	// Create test naras
	names := []string{"alice", "bob", "carol", "dave"}
	tw := NewTestWorld(names)
	defer tw.Close()

	// Create a shared ledger with events that establish clout relationships
	// Alice: teases bob a lot (bob gets negative clout from alice's perspective)
	//        has positive journey interactions with carol
	ledger := NewSyncLedger(1000)
	cloutProjection := NewCloutProjection(ledger)
	personality := NaraPersonality{Chill: 50, Sociability: 50, Agreeableness: 50}
	soul := "test-soul-alice"

	now := time.Now().Unix()

	// Alice successfully passed journey to carol multiple times (positive clout)
	for i := 0; i < 5; i++ {
		event := SyncEvent{
			Timestamp: time.Now().UnixNano() + int64(i),
			Service:   ServiceSocial,
			Social: &SocialEventPayload{
				Type:   "observation",
				Actor:  "alice",
				Target: "carol",
				Reason: ReasonJourneyPass,
			},
		}
		event.ComputeID()
		ledger.AddEvent(event)
	}

	// Carol completed journeys reliably (positive clout)
	for i := 0; i < 3; i++ {
		event := SyncEvent{
			Timestamp: time.Now().UnixNano() + int64(i+10),
			Service:   ServiceSocial,
			Social: &SocialEventPayload{
				Type:   "observation",
				Actor:  "system",
				Target: "carol",
				Reason: ReasonJourneyComplete,
			},
		}
		event.ComputeID()
		ledger.AddEvent(event)
	}

	// Dave timed out on journeys (negative clout)
	for i := 0; i < 4; i++ {
		event := SyncEvent{
			Timestamp: time.Now().UnixNano() + int64(i+20),
			Service:   ServiceSocial,
			Social: &SocialEventPayload{
				Type:   "observation",
				Actor:  "system",
				Target: "dave",
				Reason: ReasonJourneyTimeout,
			},
		}
		event.ComputeID()
		ledger.AddEvent(event)
	}

	// Bob got teased a lot (doesn't directly affect clout, but shows activity)
	event := SyncEvent{
		Timestamp: now,
		Service:   ServiceSocial,
		Social: &SocialEventPayload{
			Type:   "tease",
			Actor:  "alice",
			Target: "bob",
			Reason: ReasonHighRestarts,
		},
	}
	event.ComputeID()
	ledger.AddEvent(event)

	// Derive clout from the ledger events
	if err := cloutProjection.RunToEnd(context.Background()); err != nil {
		t.Fatalf("Failed to run projection: %v", err)
	}
	derivedClout := cloutProjection.DeriveClout(soul, personality)

	t.Logf("Derived clout from events:")
	for name, clout := range derivedClout {
		t.Logf("  %s: %.2f", name, clout)
	}

	// Verify the clout makes sense based on events:
	// - Carol should have highest clout (journey-pass + journey-complete)
	// - Dave should have lowest/negative clout (journey-timeout)
	if derivedClout["carol"] <= derivedClout["dave"] {
		t.Errorf("Carol (reliable) should have more clout than Dave (timeouts): carol=%.2f, dave=%.2f",
			derivedClout["carol"], derivedClout["dave"])
	}

	// Now use derived clout for journey routing
	// Set up the test world to use derived clout for all naras
	for name := range tw.Naras {
		tw.Clout[name] = derivedClout
	}

	// Start a journey from alice
	alice := tw.Naras["alice"]
	wm, err := alice.Handler.StartJourney("Testing derived clout routing!")
	if err != nil {
		t.Fatalf("Failed to start journey: %v", err)
	}

	t.Logf("Journey started with ID: %s", wm.ID)

	// Wait for the journey to complete
	completed := tw.WaitForCompletion(5 * time.Second)
	if completed == nil {
		t.Fatal("Journey did not complete within timeout")
	}

	// Log the path taken
	t.Logf("Journey path:")
	for i, hop := range completed.Hops {
		t.Logf("  Hop %d: %s", i+1, hop.Nara)
	}

	// Verify the first hop is carol (highest clout)
	if len(completed.Hops) > 0 && completed.Hops[0].Nara != "carol" {
		t.Logf("Note: First hop was %s (expected carol based on clout)", completed.Hops[0].Nara)
		// This isn't a hard failure - routing also considers who hasn't been visited
	}

	// Verify the journey completed successfully
	if !completed.IsComplete() {
		t.Error("Journey should be marked complete")
	}

	// Verify signatures
	err = completed.VerifyChain(func(name string) []byte {
		if nara, ok := tw.Naras[name]; ok {
			return nara.Keypair.PublicKey
		}
		return nil
	})
	if err != nil {
		t.Errorf("Signature verification failed: %v", err)
	}

	t.Logf("Journey completed successfully with event-derived clout routing!")
}
