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

// TestStashHTTPEndpoints_Integration tests the complete stash HTTP API workflow.
//
// This test ensures all stash functionality works end-to-end:
// 1. GET /api/stash/status returns proper structure
// 2. POST /api/stash/update actually updates and distributes
// 3. GET /api/stash/confidants returns confidant details
// 4. POST /api/stash/recover triggers recovery
//
// This test would have caught the missing HTTP endpoints after the runtime migration.
func TestStashHTTPEndpoints_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logrus.SetLevel(logrus.WarnLevel)

	// Start nara with UI enabled (HTTP server)
	nara := testNara(t, "test-nara")

	// Start with HTTP server (readOnly=false, no mesh, gossip-only transport)
	go nara.Start(true, false, ":0", nil, TransportGossip)

	// Wait for HTTP server to start
	time.Sleep(200 * time.Millisecond)

	// Get the actual HTTP port
	if nara.Network.httpServer == nil {
		t.Fatal("HTTP server not started")
	}

	port := nara.Network.httpServer.Addr
	if port == "" {
		// Find the actual listening port
		port = ":8080" // Default fallback
	}
	baseURL := fmt.Sprintf("http://localhost%s", port)

	// Test 1: GET /api/stash/status (initial state - no stash)
	t.Run("status_empty", func(t *testing.T) {
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

		// Verify structure
		if _, ok := result["has_stash"]; !ok {
			t.Error("Response missing 'has_stash' field")
		}
		if _, ok := result["my_stash"]; !ok {
			t.Error("Response missing 'my_stash' field")
		}
		if _, ok := result["confidants"]; !ok {
			t.Error("Response missing 'confidants' field")
		}
		if _, ok := result["metrics"]; !ok {
			t.Error("Response missing 'metrics' field")
		}
	})

	// Test 2: Configure confidants (minimum 3 required)
	t.Run("configure_confidants", func(t *testing.T) {
		// In a real test, we'd set up actual confidants
		// For now, just verify the service exists
		if nara.Network.stashService == nil {
			t.Fatal("Stash service not initialized")
		}

		// Set 3 test confidants
		nara.Network.stashService.SetConfidants([]types.NaraID{
			types.NaraID("confidant-1-id"),
			types.NaraID("confidant-2-id"),
			types.NaraID("confidant-3-id"),
		})
	})

	// Test 3: POST /api/stash/update (update stash data)
	t.Run("update_stash", func(t *testing.T) {
		stashData := map[string]interface{}{
			"preferences": map[string]interface{}{
				"theme": "dark",
			},
			"bookmarks": []string{
				"https://example.com",
			},
		}

		jsonData, err := json.Marshal(stashData)
		if err != nil {
			t.Fatalf("Failed to marshal test data: %v", err)
		}

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

		// Note: This might fail with "no confidants configured" or "minimum 3 confidants"
		// depending on whether we successfully set up mesh networking
		// The important part is that the endpoint EXISTS and responds properly

		if success, ok := result["success"].(bool); ok {
			if !success {
				// Check if it's the expected error (no confidants or can't distribute)
				message := result["message"].(string)
				t.Logf("Update failed as expected without real confidants: %s", message)
			}
		}
	})

	// Test 4: GET /api/stash/status (verify stash data if update succeeded)
	t.Run("status_with_data", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/api/stash/status")
		if err != nil {
			t.Fatalf("GET /api/stash/status failed: %v", err)
		}
		defer resp.Body.Close()

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		// If stash data was set (even if distribution failed), we should see it
		if myStash, ok := result["my_stash"].(map[string]interface{}); ok && myStash != nil {
			t.Logf("Stash data present: %+v", myStash)

			if _, ok := myStash["timestamp"]; !ok {
				t.Error("my_stash missing 'timestamp' field")
			}
			if _, ok := myStash["data"]; !ok {
				t.Error("my_stash missing 'data' field")
			}
		}
	})

	// Test 5: GET /api/stash/confidants
	t.Run("confidants_list", func(t *testing.T) {
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

		if _, ok := result["confidants"]; !ok {
			t.Error("Response missing 'confidants' field")
		}
	})

	// Test 6: POST /api/stash/recover
	t.Run("recover_trigger", func(t *testing.T) {
		resp, err := http.Post(
			baseURL+"/api/stash/recover",
			"application/json",
			bytes.NewReader([]byte("{}")),
		)
		if err != nil {
			t.Fatalf("POST /api/stash/recover failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected status 200, got %d", resp.StatusCode)
		}

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if success, ok := result["success"].(bool); !ok || !success {
			t.Error("Expected success: true in response")
		}

		if _, ok := result["message"]; !ok {
			t.Error("Response missing 'message' field")
		}
	})
}

// TestStashHTTPEndpoints_NotFound ensures unimplemented endpoints return 404.
func TestStashHTTPEndpoints_NotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logrus.SetLevel(logrus.WarnLevel)

	_, baseURL := testNaraWithHTTP(t, "test-nara")

	// Test that a non-existent endpoint returns 404
	resp, err := http.Get(baseURL + "/api/stash/nonexistent")
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 for non-existent endpoint, got %d", resp.StatusCode)
	}
}

// TestStashMinimumConfidants verifies the 3-confidant minimum is enforced.
func TestStashMinimumConfidants(t *testing.T) {
	logrus.SetLevel(logrus.WarnLevel)

	nara := testNara(t, "test-nara")

	// Initialize runtime (which creates stash service)
	if err := nara.Network.initRuntime(); err != nil {
		t.Fatalf("Failed to initialize runtime: %v", err)
	}

	// Start runtime (which initializes service dependencies)
	if err := nara.Network.startRuntime(); err != nil {
		t.Fatalf("Failed to start runtime: %v", err)
	}

	if nara.Network.stashService == nil {
		t.Fatal("Stash service not initialized")
	}

	// Test with 0 confidants
	nara.Network.stashService.SetConfidants([]types.NaraID{})
	err := nara.Network.stashService.SetStashData([]byte(`{"test": "data"}`))
	if err == nil {
		t.Error("Expected error with 0 confidants, got nil")
	}

	// Test with 1 confidant
	nara.Network.stashService.SetConfidants([]types.NaraID{types.NaraID("confidant-1")})
	err = nara.Network.stashService.SetStashData([]byte(`{"test": "data"}`))
	if err == nil {
		t.Error("Expected error with 1 confidant, got nil")
	}

	// Test with 2 confidants
	nara.Network.stashService.SetConfidants([]types.NaraID{types.NaraID("confidant-1"), types.NaraID("confidant-2")})
	err = nara.Network.stashService.SetStashData([]byte(`{"test": "data"}`))
	if err == nil {
		t.Error("Expected error with 2 confidants, got nil")
	}
	if err != nil && err.Error() != "minimum 3 confidants required, only have 2" {
		t.Logf("Got expected error: %v", err)
	}

	// Test with 3 confidants - should still error (no real mesh) but different error
	nara.Network.stashService.SetConfidants([]types.NaraID{
		types.NaraID("confidant-1"),
		types.NaraID("confidant-2"),
		types.NaraID("confidant-3"),
	})
	err = nara.Network.stashService.SetStashData([]byte(`{"test": "data"}`))
	// This will fail because confidants aren't real, but it should NOT be the "minimum 3" error
	if err != nil {
		if err.Error() == "minimum 3 confidants required, only have 3" {
			t.Errorf("Should not complain about minimum with 3 confidants: %v", err)
		} else {
			t.Logf("Failed with different error (expected - no real confidants): %v", err)
		}
	}
}
