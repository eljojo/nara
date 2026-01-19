package nara

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eljojo/nara/types"
)

// TestProfileJsonHandler_LocalNara tests that the profile endpoint works for the local nara
func TestProfileJsonHandler_LocalNara(t *testing.T) {
	// Create test nara
	ln := testNara(t, "test-local")

	// Create HTTP request for local nara's profile
	req := httptest.NewRequest("GET", "/profile/test-local.json", nil)
	w := httptest.NewRecorder()

	// Call handler
	ln.Network.httpProfileJsonHandler(w, req)

	// Check response
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	// Decode response
	var profile map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&profile); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify name
	if name, ok := profile["name"].(string); !ok || name != "test-local" {
		t.Errorf("expected name=test-local, got %v", profile["name"])
	}

	// Verify id is set
	if id, ok := profile["id"].(string); !ok || id == "" {
		t.Errorf("expected non-empty id, got %v", profile["id"])
	}
}

// TestProfileJsonHandler_Neighbor tests that the profile endpoint works for a neighbor nara
func TestProfileJsonHandler_Neighbor(t *testing.T) {
	// Create test nara
	ln := testNara(t, "test-local")

	// Add a neighbor
	neighbor := NewNara("test-neighbor")
	neighbor.ID = types.NaraID("neighbor-id-123")
	neighbor.Status.ID = neighbor.ID
	ln.Network.importNara(neighbor)

	// Set observation for neighbor
	ln.setObservation("test-neighbor", NaraObservation{
		Online:   "ONLINE",
		LastSeen: 12345,
	})

	// Create HTTP request for neighbor's profile
	req := httptest.NewRequest("GET", "/profile/test-neighbor.json", nil)
	w := httptest.NewRecorder()

	// Call handler
	ln.Network.httpProfileJsonHandler(w, req)

	// Check response
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	// Decode response
	var profile map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&profile); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify name
	if name, ok := profile["name"].(string); !ok || name != "test-neighbor" {
		t.Errorf("expected name=test-neighbor, got %v", profile["name"])
	}

	// Verify online status
	if online, ok := profile["online"].(string); !ok || online != "ONLINE" {
		t.Errorf("expected online=ONLINE, got %v", profile["online"])
	}
}

// TestProfileJsonHandler_NotFound tests that the profile endpoint returns 404 for unknown naras
func TestProfileJsonHandler_NotFound(t *testing.T) {
	// Create test nara
	ln := testNara(t, "test-local")

	// Create HTTP request for unknown nara
	req := httptest.NewRequest("GET", "/profile/unknown-nara.json", nil)
	w := httptest.NewRecorder()

	// Call handler
	ln.Network.httpProfileJsonHandler(w, req)

	// Check response
	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}
