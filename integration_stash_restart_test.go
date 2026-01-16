package nara

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/eljojo/nara/types"
	"github.com/sirupsen/logrus"
)

// TestStashRestart_Integration tests the restart and recovery workflow.
//
// This test verifies:
// 1. Nara can store stash data with confidants
// 2. Nara can "restart" (lose local data)
// 3. Nara can recover stash from confidants after restart
// 4. Recovered data matches original data
func TestStashRestart_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logrus.SetLevel(logrus.WarnLevel)

	// Create mesh with 4 naras: owner + 3 confidants
	names := []string{"owner", "conf-a", "conf-b", "conf-c"}
	mesh := testCreateMeshNetwork(t, names, 50, 1000)

	owner := mesh.Get(0)

	// Initialize runtime for all naras
	for i := 0; i < 4; i++ {
		nara := mesh.Naras[i]
		if err := nara.Network.initRuntime(); err != nil {
			t.Fatalf("Failed to init runtime: %v", err)
		}
		if err := nara.Network.startRuntime(); err != nil {
			t.Fatalf("Failed to start runtime: %v", err)
		}
	}

	// Configure confidants
	confidantIDs := []types.NaraID{
		mesh.Get(1).Me.Status.ID,
		mesh.Get(2).Me.Status.ID,
		mesh.Get(3).Me.Status.ID,
	}
	owner.Network.stashService.SetConfidants(confidantIDs)

	// === PHASE 1: Store stash data ===
	t.Log("Phase 1: Storing stash with confidants...")

	originalData := map[string]interface{}{
		"session_id": "test-session-123",
		"preferences": map[string]interface{}{
			"theme":    "dark",
			"language": "en",
		},
		"bookmarks": []string{
			"https://example.com",
			"https://nara.test",
		},
	}
	originalJSON, err := json.Marshal(originalData)
	if err != nil {
		t.Fatalf("Failed to marshal data: %v", err)
	}

	// Store stash
	if err := owner.Network.stashService.SetStashData(originalJSON); err != nil {
		t.Fatalf("Failed to set stash data: %v", err)
	}

	// Wait for distribution
	time.Sleep(300 * time.Millisecond)

	// Verify confidants have the stash
	storedCount := 0
	for i := 1; i <= 3; i++ {
		if mesh.Get(i).Network.stashService.HasStashFor(owner.Me.Status.ID) {
			storedCount++
			t.Logf("✅ Confidant %s has owner's stash", mesh.Get(i).Me.Name)
		}
	}

	if storedCount < 3 {
		t.Fatalf("Expected 3 confidants to have stash, got %d", storedCount)
	}

	// === PHASE 2: Simulate restart (data loss) ===
	t.Log("Phase 2: Simulating data loss (restart scenario)...")

	// Clear local stash to simulate data loss during restart
	// In a real restart, the nara would boot with the same identity and network connections,
	// but the stash data would be lost if not persisted to disk
	owner.Network.stashService.ClearMyStash()

	// Verify local stash is empty
	data, timestamp := owner.Network.stashService.GetStashData()
	if len(data) > 0 {
		t.Fatal("Expected local stash to be empty after data loss")
	}
	if timestamp != 0 {
		t.Error("Expected timestamp to be 0 after data loss")
	}

	t.Log("✅ Local stash cleared (simulating data loss on restart)")

	// === PHASE 3: Recover from confidants ===
	t.Log("Phase 3: Recovering stash from confidants...")

	recoveredData, err := owner.Network.stashService.RecoverFromAny()
	if err != nil {
		t.Fatalf("Failed to recover stash: %v", err)
	}

	if len(recoveredData) == 0 {
		t.Fatal("Recovered data is empty")
	}

	// Verify recovered data matches original
	var recoveredMap map[string]interface{}
	if err := json.Unmarshal(recoveredData, &recoveredMap); err != nil {
		t.Fatalf("Failed to unmarshal recovered data: %v", err)
	}

	// Check session_id
	if sessionID, ok := recoveredMap["session_id"].(string); !ok || sessionID != "test-session-123" {
		t.Errorf("Expected session_id 'test-session-123', got %v", recoveredMap["session_id"])
	}

	// Check preferences
	if prefs, ok := recoveredMap["preferences"].(map[string]interface{}); ok {
		if theme, ok := prefs["theme"].(string); !ok || theme != "dark" {
			t.Errorf("Expected theme 'dark', got %v", prefs["theme"])
		}
	} else {
		t.Error("Recovered data missing preferences")
	}

	t.Log("✅ Successfully recovered stash from confidants")
	t.Log("✅ Recovered data matches original data")
}

// TestStashRestart_ManualRecovery tests recovery via RequestFrom (specific confidant).
func TestStashRestart_ManualRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logrus.SetLevel(logrus.WarnLevel)

	// Create mesh with 4 naras
	names := []string{"owner", "conf-a", "conf-b", "conf-c"}
	mesh := testCreateMeshNetwork(t, names, 50, 1000)

	owner := mesh.Get(0)
	confidantB := mesh.Get(2)

	// Initialize runtime for all naras
	for i := 0; i < 4; i++ {
		nara := mesh.Naras[i]
		if err := nara.Network.initRuntime(); err != nil {
			t.Fatalf("Failed to init runtime: %v", err)
		}
		if err := nara.Network.startRuntime(); err != nil {
			t.Fatalf("Failed to start runtime: %v", err)
		}
	}

	// Configure confidants
	confidantIDs := []types.NaraID{
		mesh.Get(1).Me.Status.ID,
		mesh.Get(2).Me.Status.ID,
		mesh.Get(3).Me.Status.ID,
	}
	owner.Network.stashService.SetConfidants(confidantIDs)

	// Store stash
	testData := []byte(`{"manual":"recovery","test":true}`)
	if err := owner.Network.stashService.SetStashData(testData); err != nil {
		t.Fatalf("Failed to set stash data: %v", err)
	}

	// Wait for distribution
	time.Sleep(300 * time.Millisecond)

	// Simulate data loss
	owner.Network.stashService.ClearMyStash()

	// Recover from specific confidant (confidant-b)
	recovered, err := owner.Network.stashService.RequestFrom(confidantB.Me.Status.ID)
	if err != nil {
		t.Fatalf("Failed to recover from specific confidant: %v", err)
	}

	if string(recovered) != string(testData) {
		t.Errorf("Expected recovered data to match original. Got: %s", recovered)
	}

	t.Log("✅ Successfully recovered from specific confidant")
}
