package nara

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/eljojo/nara/types"
	"github.com/sirupsen/logrus"
)

// TestStashDistribution_Integration tests the complete stash distribution workflow.
//
// This test creates a real mesh network with 4 naras and verifies:
// 1. Stash data is successfully distributed to 3 confidants via mesh HTTP
// 2. Confidants receive and store the encrypted stash
// 3. Owner can retrieve the stash from confidants
// 4. Recovery successfully retrieves stash from confidants
// 5. HTTP endpoints return correct data throughout the workflow
//
// This is a true end-to-end integration test that verifies the entire stash system works.
func TestStashDistribution_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logrus.SetLevel(logrus.DebugLevel)
	// Create mesh with 4 naras
	names := []string{"owner", "confidant-a", "confidant-b", "confidant-c"}
	mesh := testCreateMeshNetwork(t, names, 50, 1000, 11892)

	owner := mesh.Get(0)
	confidantA := mesh.Get(1)
	confidantB := mesh.Get(2)
	confidantC := mesh.Get(3)

	// Initialize runtime and stash services for all naras
	for i := 0; i < 4; i++ {
		nara := mesh.Naras[i]
		if err := nara.Network.initRuntime(); err != nil {
			t.Fatalf("Failed to initialize runtime for %s: %v", nara.Me.Name, err)
		}
		if err := nara.Network.startRuntime(); err != nil {
			t.Fatalf("Failed to start runtime for %s: %v", nara.Me.Name, err)
		}
		if nara.Network.stashService == nil {
			t.Fatalf("Stash service not initialized for %s", nara.Me.Name)
		}
		// Start confidants (indices 1, 2, 3) so they connect to MQTT and can process events
		// Owner (index 0) is started later with specific HTTP config
		// TODO: Make this cleaner in future refactor
		if i > 0 {
			go nara.Start(false, false, "", nil, TransportHybrid)
		}
	}

	// Verify the system can discover peers (prerequisite for automatic selection)
	peerCount := len(owner.Network.Neighbourhood)
	if peerCount < 3 {
		t.Fatalf("Need at least 3 known peers for confidant selection, found %d", peerCount)
	}
	t.Logf("✅ Owner knows %d peers (peer discovery working)", peerCount)

	// Verify owner has 0 confidants initially
	initialConfidants := owner.Network.stashService.Confidants()
	if len(initialConfidants) != 0 {
		t.Fatalf("Owner should have 0 confidants initially, found %d", len(initialConfidants))
	}

	// Owner creates stash data
	stashData := map[string]interface{}{
		"preferences": map[string]interface{}{
			"theme":    "dark",
			"language": "en",
		},
		"bookmarks": []string{
			"https://example.com",
			"https://nara.test",
		},
		"notes": "test integration data",
	}
	stashJSON, err := json.Marshal(stashData)
	if err != nil {
		t.Fatalf("Failed to marshal stash data: %v", err)
	}

	// Owner distributes stash to confidants
	t.Logf("Distributing stash from %s to 3 confidants...", owner.Me.Name)
	err = owner.Network.stashService.SetStashData(stashJSON)
	if err != nil {
		t.Fatalf("Failed to set stash data: %v. Stash distribution should succeed with real mesh!", err)
	}

	// Wait for distribution to complete
	time.Sleep(300 * time.Millisecond)

	// Verify: Owner has the stash locally
	ownerStashData, ownerStashTimestamp := owner.Network.stashService.GetStashData()
	if len(ownerStashData) == 0 {
		t.Fatal("Owner should have stash data locally")
	}
	if ownerStashTimestamp == 0 {
		t.Error("Owner stash timestamp should be set")
	}
	t.Logf("✅ Owner has stash data locally (timestamp: %d)", ownerStashTimestamp)

	// Verify: All 3 confidants received and stored the encrypted stash
	confidantServices := []*LocalNara{confidantA, confidantB, confidantC}
	successCount := 0
	for i, confidant := range confidantServices {
		hasStash := confidant.Network.stashService.HasStashFor(owner.Me.Status.ID)
		if !hasStash {
			t.Errorf("Confidant %d (%s) should have stored the owner's stash (ID: %s)",
				i, confidant.Me.Name, owner.Me.Status.ID)
		} else {
			successCount++
			t.Logf("✅ Confidant %s successfully stored owner's stash", confidant.Me.Name)
		}
	}

	if successCount == 0 {
		t.Fatal("No confidants received the stash - distribution completely failed!")
	}
	if successCount < 3 {
		t.Logf("⚠️  Only %d/3 confidants received the stash", successCount)
	} else {
		t.Logf("✅ Stash successfully distributed to all 3 confidants")
	}

	// Test HTTP endpoints
	httpAddr := ":9500"
	go owner.Start(true, false, httpAddr, nil, TransportHybrid)
	time.Sleep(200 * time.Millisecond)
	baseURL := fmt.Sprintf("http://localhost%s", httpAddr)

	// Test GET /api/stash/status
	t.Run("http_status_endpoint", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/api/stash/status")
		if err != nil {
			t.Fatalf("GET /api/stash/status failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected status 200, got %d", resp.StatusCode)
		}

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		// Verify has_stash is true
		hasStash, ok := result["has_stash"].(bool)
		if !ok || !hasStash {
			t.Error("Expected has_stash: true")
		}

		// Verify my_stash contains data
		myStash, ok := result["my_stash"].(map[string]interface{})
		if !ok || myStash == nil {
			t.Fatal("Expected my_stash to be present")
		}

		if _, ok := myStash["timestamp"]; !ok {
			t.Error("my_stash missing timestamp")
		}

		stashDataField, ok := myStash["data"].(map[string]interface{})
		if !ok || stashDataField == nil {
			t.Fatal("my_stash missing data field")
		}

		// Verify actual data matches what we stored
		if prefs, ok := stashDataField["preferences"].(map[string]interface{}); ok {
			if theme, ok := prefs["theme"].(string); !ok || theme != "dark" {
				t.Errorf("Expected theme 'dark', got %v", prefs["theme"])
			}
		} else {
			t.Error("Stash data missing preferences")
		}

		t.Logf("✅ HTTP /api/stash/status returns correct data")
	})

	// Test GET /api/stash/confidants
	t.Run("http_confidants_endpoint", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/api/stash/confidants")
		if err != nil {
			t.Fatalf("GET /api/stash/confidants failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected status 200, got %d", resp.StatusCode)
		}

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		confidants, ok := result["confidants"].([]interface{})
		if !ok {
			t.Fatal("Expected confidants array")
		}

		// Should have 3 confidants
		if len(confidants) != 3 {
			t.Errorf("Expected 3 confidants, got %d", len(confidants))
		}

		t.Logf("✅ HTTP /api/stash/confidants returns 3 confidants")
	})

	// Test stash recovery workflow - simulates a complete reboot with automatic recovery
	t.Run("recovery_workflow", func(t *testing.T) {
		// Simulate complete restart: shutdown and recreate with same identity but empty state
		rebooted := mesh.RestartNara(0) // owner is at index 0

		// Initialize runtime and stash service for rebooted nara
		if err := rebooted.Network.initRuntime(); err != nil {
			t.Fatalf("Failed to initialize runtime for rebooted nara: %v", err)
		}
		if err := rebooted.Network.startRuntime(); err != nil {
			t.Fatalf("Failed to start runtime for rebooted nara: %v", err)
		}

		t.Logf("Rebooted nara with same identity, checking for empty stash...")

		// Verify: Rebooted nara has no local stash
		localData, _ := rebooted.Network.stashService.GetStashData()
		if len(localData) > 0 {
			t.Error("Rebooted nara should start with no local stash")
		}

		// Verify no confidants configured (empty list after reboot)
		stateBytes, _ := rebooted.Network.stashService.MarshalState()
		var state struct {
			Confidants []string `json:"confidants"`
		}
		_ = json.Unmarshal(stateBytes, &state)
		if len(state.Confidants) != 0 {
			t.Errorf("Rebooted nara should have empty confidant list, got %d", len(state.Confidants))
		}

		t.Logf("✅ Rebooted nara has empty stash and empty confidant list")

		// Start HTTP server on rebooted nara
		rebootedAddr := ":9502"
		go rebooted.Start(true, false, rebootedAddr, nil, TransportHybrid)
		time.Sleep(300 * time.Millisecond)
		rebootedURL := fmt.Sprintf("http://localhost%s", rebootedAddr)

		// Send hey-there announcement (in real system, this happens automatically on boot)
		// Confidants detect the hey-there and automatically push stash back
		t.Logf("Sending hey-there so confidants push stash back...")
		rebooted.Network.heyThere()
		t.Logf("Sent hey-there message from rebooted nara (heyThere)")
		// With TransportHybrid, MQTT will handle the hey-there broadcast
		// so we don't need manual propagation

		// Poll status endpoint to wait for recovery to complete
		var recoveredData []byte
		var recoveredTimestamp int64
		for i := 0; i < 15; i++ {
			time.Sleep(500 * time.Millisecond)

			// Check via HTTP status endpoint
			statusResp, err := http.Get(rebootedURL + "/api/stash/status")
			if err != nil {
				continue
			}

			var status map[string]interface{}
			if err := json.NewDecoder(statusResp.Body).Decode(&status); err != nil {
				statusResp.Body.Close()
				continue
			}
			statusResp.Body.Close()

			hasStash, _ := status["has_stash"].(bool)
			if hasStash {
				// Got it! Extract data
				if myStash, ok := status["my_stash"].(map[string]interface{}); ok {
					if ts, ok := myStash["timestamp"].(float64); ok {
						recoveredTimestamp = int64(ts)
					}
					if data, ok := myStash["data"]; ok {
						dataJSON, _ := json.Marshal(data)
						recoveredData = dataJSON
					}
				}
				t.Logf("✅ Stash recovered after %d polls", i+1)
				break
			}
		}

		// Verify: Rebooted nara now has the stash data
		if len(recoveredData) == 0 {
			t.Fatal("Rebooted nara should have recovered stash data from confidants")
		}
		if recoveredTimestamp == 0 {
			t.Error("Recovered stash should have timestamp")
		}

		// Verify the recovered data matches original
		var recovered map[string]interface{}
		if err := json.Unmarshal(recoveredData, &recovered); err != nil {
			t.Fatalf("Failed to unmarshal recovered data: %v", err)
		}

		if prefs, ok := recovered["preferences"].(map[string]interface{}); ok {
			if theme, ok := prefs["theme"].(string); !ok || theme != "dark" {
				t.Errorf("Expected theme 'dark' in recovered data, got %v", prefs["theme"])
			}
		} else {
			t.Error("Recovered data missing preferences")
		}

		t.Logf("✅ Rebooted nara successfully recovered stash via simulated hey-there workflow")
	})

	t.Logf("✅ End-to-end stash distribution test completed successfully!")
}

// TestStashUpdate_HTTPWorkflow tests updating stash via HTTP endpoint
// TODO(flakey)
func TestStashUpdate_HTTPWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logrus.SetLevel(logrus.WarnLevel)
	// Create mesh with 4 naras
	names := []string{"owner", "conf-1", "conf-2", "conf-3"}
	mesh := testCreateMeshNetwork(t, names, 50, 1000, 11891)

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

	// Manually configure confidants (automatic selection needs additional setup during test init)
	owner.Network.stashService.SetConfidants([]types.NaraID{
		mesh.Get(1).Me.Status.ID,
		mesh.Get(2).Me.Status.ID,
		mesh.Get(3).Me.Status.ID,
	})

	// Start HTTP server
	ownerAddr := ":9501"
	go owner.Start(true, false, ownerAddr, nil, TransportHybrid)
	time.Sleep(200 * time.Millisecond)
	baseURL := fmt.Sprintf("http://localhost%s", ownerAddr)

	// Initial stash data
	stashData := map[string]interface{}{
		"counter": 1,
		"name":    "test",
	}
	jsonData, _ := json.Marshal(stashData)

	// Update stash via HTTP
	resp, err := http.Post(
		baseURL+"/api/stash/update",
		"application/json",
		bytes.NewReader(jsonData),
	)
	if err != nil {
		t.Fatalf("POST /api/stash/update failed: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	success, ok := result["success"].(bool)
	if !ok || !success {
		t.Fatalf("Update should succeed with 3 confidants. Got: %v", result)
	}

	// Wait for distribution
	time.Sleep(300 * time.Millisecond)

	// Update again with different data
	stashData["counter"] = 2
	stashData["updated"] = true
	jsonData, _ = json.Marshal(stashData)

	resp2, err := http.Post(
		baseURL+"/api/stash/update",
		"application/json",
		bytes.NewReader(jsonData),
	)
	if err != nil {
		t.Fatalf("Second POST failed: %v", err)
	}
	defer resp2.Body.Close()

	var result2 map[string]interface{}
	if err := json.NewDecoder(resp2.Body).Decode(&result2); err != nil {
		t.Fatalf("Failed to decode second response: %v", err)
	}

	success2, ok := result2["success"].(bool)
	if !ok || !success2 {
		t.Fatalf("Second update should succeed. Got: %v", result2)
	}

	time.Sleep(300 * time.Millisecond)

	// Verify final state via status endpoint
	resp3, err := http.Get(baseURL + "/api/stash/status")
	if err != nil {
		t.Fatalf("GET status failed: %v", err)
	}
	defer resp3.Body.Close()

	var status map[string]interface{}
	if err := json.NewDecoder(resp3.Body).Decode(&status); err != nil {
		t.Fatalf("Failed to decode status response: %v", err)
	}

	myStash := status["my_stash"].(map[string]interface{})
	data := myStash["data"].(map[string]interface{})

	if counter, ok := data["counter"].(float64); !ok || int(counter) != 2 {
		t.Errorf("Expected counter=2, got %v", data["counter"])
	}

	if updated, ok := data["updated"].(bool); !ok || !updated {
		t.Errorf("Expected updated=true, got %v", data["updated"])
	}

	t.Logf("✅ Successfully updated stash multiple times via HTTP")
}
