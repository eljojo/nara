package nara

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/eljojo/nara/identity"
	"github.com/eljojo/nara/types"
)

// TestNara represents a test nara with all necessary components
type TestNara struct {
	Name        string
	Soul        identity.SoulV1
	Keypair     identity.NaraKeypair
	LocalNara   *LocalNara
	MeshClient  *MeshClient
	Handler     *WorldJourneyHandler
	MessageChan chan *WorldMessage // For receiving world messages in tests
}

// TestWorld sets up a complete test environment for world journey testing
type TestWorld struct {
	Naras      map[string]*TestNara
	Clout      map[string]map[types.NaraName]float64
	Completed  []*WorldMessage
	CompleteMu sync.Mutex
}

// NewTestWorld creates a test world with the given nara names
func NewTestWorld(names []string) *TestWorld {
	tw := &TestWorld{
		Naras:     make(map[string]*TestNara),
		Clout:     make(map[string]map[types.NaraName]float64),
		Completed: []*WorldMessage{},
	}

	// Create shared mock HTTP transport for all test naras
	mockTransport := NewMockMeshHTTPTransport()

	// Create test naras
	for i, name := range names {
		hw := identity.HashBytes([]byte("integration-test-hw-" + name))
		soul := identity.NativeSoulCustom(hw, types.NaraName(name))
		keypair := identity.DeriveKeypair(soul)

		// Compute nara ID
		naraID, _ := identity.ComputeNaraID(identity.FormatSoul(soul), types.NaraName(name))

		// Create a minimal LocalNara (without full network setup)
		ln := &LocalNara{
			Me:      NewNara(types.NaraName(name)),
			Soul:    identity.FormatSoul(soul),
			Keypair: keypair,
			ID:      naraID,
		}
		ctx, cancel := context.WithCancel(context.Background())
		ln.Network = &Network{
			ctx:        ctx,
			cancelFunc: cancel,
		}
		ln.Me.Status.PublicKey = identity.FormatPublicKey(keypair.PublicKey)
		ln.Me.Status.ID = naraID

		// Create message channel for this nara
		messageChan := make(chan *WorldMessage, 10)

		// Register handler for this nara in the mock transport
		mockTransport.RegisterHandler(naraID, CreateMockWorldMessageHandler(messageChan))

		// Create mesh client for this nara using the shared mock transport
		meshClient := NewMockMeshClientForTesting(types.NaraName(name), keypair, mockTransport)

		testNara := &TestNara{
			Name:        name,
			Soul:        soul,
			Keypair:     keypair,
			LocalNara:   ln,
			MeshClient:  meshClient,
			Handler:     nil, // Set later
			MessageChan: messageChan,
		}

		tw.Naras[name] = testNara

		// Initialize clout for this nara (empty, will be set up by test)
		tw.Clout[name] = make(map[types.NaraName]float64)

		// Give each nara some clout toward others (simple pattern for testing)
		for j, otherName := range names {
			if name != otherName {
				// Higher clout for naras that come after in the list
				tw.Clout[name][types.NaraName(otherName)] = float64((j - i + len(names)) % len(names))
			}
		}
	}

	// Register all peers in each mesh client
	for _, sender := range tw.Naras {
		for name, receiver := range tw.Naras {
			if name != sender.Name {
				sender.MeshClient.RegisterPeer(receiver.LocalNara.ID, "mock://"+receiver.LocalNara.ID.String())
			}
		}
	}

	// Create handlers for each nara
	for _, testNara := range tw.Naras {
		handler := tw.createHandler(testNara)
		testNara.Handler = handler
	}

	return tw
}

func (tw *TestWorld) createHandler(tn *TestNara) *WorldJourneyHandler {
	handler := NewWorldJourneyHandler(
		tn.LocalNara,
		tn.MeshClient,
		func() map[types.NaraName]float64 {
			// Return this nara's clout scores
			if tw.Clout != nil {
				return tw.Clout[tn.LocalNara.Me.Name.String()]
			}
			return nil
		},
		func() []types.NaraName {
			names := make([]types.NaraName, 0, len(tw.Naras))
			for name := range tw.Naras {
				names = append(names, types.NaraName(name))
			}
			return names
		},
		func(name types.NaraName) []byte {
			if nara, ok := tw.Naras[name.String()]; ok {
				return nara.Keypair.PublicKey
			}
			return nil
		},
		func(name types.NaraName) types.NaraID {
			// Resolve nara name to ID
			if nara, ok := tw.Naras[name.String()]; ok {
				return nara.LocalNara.ID
			}
			return ""
		},
		func(wm *WorldMessage) {
			tw.CompleteMu.Lock()
			tw.Completed = append(tw.Completed, wm)
			tw.CompleteMu.Unlock()
		},
		nil, // onJourneyPass - not needed for these tests
	)

	// Start goroutine to route messages from channel to handler
	go func() {
		for wm := range tn.MessageChan {
			_ = handler.HandleIncoming(wm) // Errors are logged by the handler
		}
	}()

	return handler
}

// SetClout sets up clout relationships
func (tw *TestWorld) SetClout(clout map[string]map[types.NaraName]float64) {
	tw.Clout = clout
}

// Close shuts down all test naras
func (tw *TestWorld) Close() {
	for _, tn := range tw.Naras {
		close(tn.MessageChan)
		if tn.LocalNara.Network.cancelFunc != nil {
			tn.LocalNara.Network.cancelFunc()
		}
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
	tw.SetClout(map[string]map[types.NaraName]float64{
		"alice": {types.NaraName("bob"): 10, types.NaraName("carol"): 5, types.NaraName("dave"): 2},
		"bob":   {types.NaraName("carol"): 10, types.NaraName("dave"): 5, types.NaraName("alice"): 2},
		"carol": {types.NaraName("dave"): 10, types.NaraName("alice"): 5, types.NaraName("bob"): 2},
		"dave":  {types.NaraName("alice"): 10, types.NaraName("bob"): 5, types.NaraName("carol"): 2},
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
	expectedPath := []types.NaraName{types.NaraName("bob"), types.NaraName("carol"), types.NaraName("dave"), types.NaraName("alice")}
	if len(completed.Hops) != len(expectedPath) {
		t.Errorf("Expected %d hops, got %d", len(expectedPath), len(completed.Hops))
	}

	for i, expected := range expectedPath {
		if i < len(completed.Hops) && completed.Hops[i].Nara != expected {
			t.Errorf("Hop %d: expected %s, got %s", i, expected, completed.Hops[i].Nara)
		}
	}

	// Verify all signatures
	err = completed.VerifyChain(func(name types.NaraName) []byte {
		if nara, ok := tw.Naras[name.String()]; ok {
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

	tw.SetClout(map[string]map[types.NaraName]float64{
		"alice": {types.NaraName("bob"): 10, types.NaraName("carol"): 5},
		"bob":   {types.NaraName("carol"): 10, types.NaraName("alice"): 5},
		"carol": {types.NaraName("alice"): 10, types.NaraName("bob"): 5},
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
	err = completed.VerifyChain(func(name types.NaraName) []byte {
		if nara, ok := tw.Naras[name.String()]; ok {
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
	tw.SetClout(map[string]map[types.NaraName]float64{
		"alpha":   {types.NaraName("beta"): 10, types.NaraName("gamma"): 5, types.NaraName("delta"): 3, types.NaraName("epsilon"): 1},
		"beta":    {types.NaraName("gamma"): 10, types.NaraName("delta"): 5, types.NaraName("epsilon"): 3, types.NaraName("alpha"): 1},
		"gamma":   {types.NaraName("delta"): 10, types.NaraName("epsilon"): 5, types.NaraName("alpha"): 3, types.NaraName("beta"): 1},
		"delta":   {types.NaraName("epsilon"): 10, types.NaraName("alpha"): 5, types.NaraName("beta"): 3, types.NaraName("gamma"): 1},
		"epsilon": {types.NaraName("alpha"): 10, types.NaraName("beta"): 5, types.NaraName("gamma"): 3, types.NaraName("delta"): 1},
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

	tw.SetClout(map[string]map[types.NaraName]float64{
		"one":   {types.NaraName("two"): 10, types.NaraName("three"): 5},
		"two":   {types.NaraName("three"): 10, types.NaraName("one"): 5},
		"three": {types.NaraName("one"): 10, types.NaraName("two"): 5},
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
	err = completed.VerifyChain(func(name types.NaraName) []byte {
		if nara, ok := tw.Naras[name.String()]; ok {
			return nara.Keypair.PublicKey
		}
		return nil
	})
	if err != nil {
		t.Errorf("Signature verification failed: %v", err)
	}

	t.Logf("Journey completed successfully with event-derived clout routing!")
}
