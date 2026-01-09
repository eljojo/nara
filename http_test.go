package nara

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHttpNaraeJsonHandler(t *testing.T) {
	ln := NewLocalNara("test-nara", testSoul("test-nara"), "host", "user", "pass", -1, 0)
	network := ln.Network

	req, err := http.NewRequest("GET", "/narae.json", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(network.httpNaraeJsonHandler)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	var response map[string]interface{}
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	if err != nil {
		t.Fatal(err)
	}

	if response["server"] != "test-nara" {
		t.Errorf("expected server to be test-nara, got %v", response["server"])
	}

	naras := response["naras"].([]interface{})
	if len(naras) != 1 {
		t.Errorf("expected 1 nara, got %v", len(naras))
	}
}

func TestHttpApiJsonHandler(t *testing.T) {
	ln := NewLocalNara("test-nara", testSoul("test-nara"), "host", "user", "pass", -1, 0)
	network := ln.Network

	req, err := http.NewRequest("GET", "/api.json", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(network.httpApiJsonHandler)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	var response map[string]interface{}
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	if err != nil {
		t.Fatal(err)
	}

	if response["server"] != "test-nara" {
		t.Errorf("expected server to be test-nara, got %v", response["server"])
	}
}

func TestHttpStatusJsonHandler(t *testing.T) {
	ln := NewLocalNara("test-nara", testSoul("test-nara"), "host", "user", "pass", -1, 0)
	network := ln.Network

	// Test existing nara
	otherNara := NewNara("other")
	otherNara.Status.Flair = "🌟"
	network.Neighbourhood["other"] = otherNara

	req, err := http.NewRequest("GET", "/status/other.json", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(network.httpStatusJsonHandler)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	var status NaraStatus
	err = json.Unmarshal(rr.Body.Bytes(), &status)
	if err != nil {
		t.Fatal(err)
	}

	if status.Flair != "🌟" {
		t.Errorf("expected flair to be 🌟, got %v", status.Flair)
	}

	// Test non-existing nara
	req, _ = http.NewRequest("GET", "/status/missing.json", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %v", rr.Code)
	}
}

func TestHttpMetricsHandler(t *testing.T) {
	ln := NewLocalNara("test-nara", testSoul("test-nara"), "host", "user", "pass", -1, 0)
	network := ln.Network

	otherNara := NewNara("other")
	otherNara.Status.Flair = "🌟"
	otherNara.Status.Observations["other"] = NaraObservation{Online: "ONLINE", Restarts: 5}
	network.Neighbourhood["other"] = otherNara

	req, err := http.NewRequest("GET", "/metrics", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(network.httpMetricsHandler)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	body := rr.Body.String()
	expectedStrings := []string{
		`nara_online{name="other"} 1`,
		`nara_restarts_total{name="other"} 5`,
		`nara_info{name="other",flair="🌟",license_plate=""} 1`,
	}

	for _, s := range expectedStrings {
		if !strings.Contains(body, s) {
			t.Errorf("expected metrics to contain %s", s)
		}
	}
}

func TestHttpPingHandler(t *testing.T) {
	ln := NewLocalNara("test-nara", testSoul("test-nara"), "host", "user", "pass", -1, 0)
	network := ln.Network

	req, err := http.NewRequest("GET", "/ping", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(network.httpPingHandler)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	var response map[string]interface{}
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	if err != nil {
		t.Fatal(err)
	}

	// Should have timestamp
	if _, ok := response["t"]; !ok {
		t.Error("expected 't' (timestamp) in response")
	}

	// Should have from field
	if response["from"] != "test-nara" {
		t.Errorf("expected from to be test-nara, got %v", response["from"])
	}
}

func TestHttpCoordinatesHandler(t *testing.T) {
	ln := NewLocalNara("test-nara", testSoul("test-nara"), "host", "user", "pass", -1, 0)
	network := ln.Network

	req, err := http.NewRequest("GET", "/coordinates", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(network.httpCoordinatesHandler)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	var response map[string]interface{}
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	if err != nil {
		t.Fatal(err)
	}

	// Should have name
	if response["name"] != "test-nara" {
		t.Errorf("expected name to be test-nara, got %v", response["name"])
	}

	// Should have coordinates
	coords, ok := response["coordinates"].(map[string]interface{})
	if !ok {
		t.Fatal("expected coordinates to be an object")
	}

	// Coordinates should have expected fields
	if _, ok := coords["x"]; !ok {
		t.Error("expected 'x' in coordinates")
	}
	if _, ok := coords["y"]; !ok {
		t.Error("expected 'y' in coordinates")
	}
	if _, ok := coords["height"]; !ok {
		t.Error("expected 'height' in coordinates")
	}
	if _, ok := coords["error"]; !ok {
		t.Error("expected 'error' in coordinates")
	}
}

func TestHttpNetworkMapHandler(t *testing.T) {
	ln := NewLocalNara("test-nara", testSoul("test-nara"), "host", "user", "pass", -1, 0)
	network := ln.Network

	// Add a neighbor with coordinates
	otherNara := NewNara("other")
	otherNara.Status.Coordinates = NewNetworkCoordinate()
	otherNara.Status.Observations["other"] = NaraObservation{Online: "ONLINE"}
	network.Neighbourhood["other"] = otherNara

	req, err := http.NewRequest("GET", "/network/map", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(network.httpNetworkMapHandler)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	var response map[string]interface{}
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	if err != nil {
		t.Fatal(err)
	}

	// Should have server
	if response["server"] != "test-nara" {
		t.Errorf("expected server to be test-nara, got %v", response["server"])
	}

	// Should have nodes
	nodes, ok := response["nodes"].([]interface{})
	if !ok {
		t.Fatal("expected nodes to be an array")
	}

	// Should have 2 nodes (self + other)
	if len(nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(nodes))
	}

	// Check that self is marked correctly
	foundSelf := false
	foundOther := false
	for _, n := range nodes {
		node := n.(map[string]interface{})
		name := node["name"].(string)
		if name == "test-nara" {
			foundSelf = true
			if node["is_self"] != true {
				t.Error("expected self node to have is_self=true")
			}
		}
		if name == "other" {
			foundOther = true
			if node["online"] != true {
				t.Error("expected other node to be online")
			}
		}
	}

	if !foundSelf {
		t.Error("expected to find self in nodes")
	}
	if !foundOther {
		t.Error("expected to find other in nodes")
	}
}

func TestHttpNetworkMapHandler_NodesWithoutCoordinates(t *testing.T) {
	ln := NewLocalNara("test-nara", testSoul("test-nara"), "host", "user", "pass", -1, 0)
	network := ln.Network

	// Add a neighbor WITHOUT coordinates (simulating older version)
	oldNara := NewNara("old-version")
	oldNara.Status.Coordinates = nil // No coordinates
	oldNara.Status.Observations["old-version"] = NaraObservation{Online: "ONLINE"}
	network.Neighbourhood["old-version"] = oldNara

	// Add a neighbor WITH coordinates
	newNara := NewNara("new-version")
	newNara.Status.Coordinates = NewNetworkCoordinate()
	newNara.Status.Observations["new-version"] = NaraObservation{Online: "ONLINE"}
	network.Neighbourhood["new-version"] = newNara

	req, err := http.NewRequest("GET", "/network/map", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(network.httpNetworkMapHandler)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	var response map[string]interface{}
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	if err != nil {
		t.Fatal(err)
	}

	nodes := response["nodes"].([]interface{})

	// Should have 3 nodes (self + old + new)
	if len(nodes) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(nodes))
	}

	// Check that nodes with and without coordinates are both present
	foundOld := false
	foundNew := false
	for _, n := range nodes {
		node := n.(map[string]interface{})
		name := node["name"].(string)
		if name == "old-version" {
			foundOld = true
			if node["coordinates"] != nil {
				t.Error("expected old-version to have nil coordinates")
			}
		}
		if name == "new-version" {
			foundNew = true
			if node["coordinates"] == nil {
				t.Error("expected new-version to have coordinates")
			}
		}
	}

	if !foundOld {
		t.Error("expected to find old-version in nodes")
	}
	if !foundNew {
		t.Error("expected to find new-version in nodes")
	}
}

func TestHttpEventsSyncHandler(t *testing.T) {
	ln := NewLocalNara("test-nara", testSoul("test-nara"), "host", "user", "pass", -1, 0)
	network := ln.Network

	// Add some events to the SyncLedger
	ln.SyncLedger.AddPingObservation("test-nara", "other", 42.5)
	ln.SyncLedger.AddEvent(NewSocialSyncEvent("tease", "test-nara", "other", "high-restarts", ""))

	// Create sync request
	reqBody := SyncRequest{
		From:       "requester",
		SliceIndex: 0,
		SliceTotal: 1,
		MaxEvents:  100,
	}
	reqJSON, _ := json.Marshal(reqBody)

	req, err := http.NewRequest("POST", "/events/sync", strings.NewReader(string(reqJSON)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(network.httpEventsSyncHandler)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	var response SyncResponse
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	if err != nil {
		t.Fatal(err)
	}

	// Check response has events
	if len(response.Events) != 2 {
		t.Errorf("expected 2 events, got %d", len(response.Events))
	}

	// Check response is signed
	if response.Signature == "" {
		t.Error("expected response to have signature")
	}

	// Verify signature with the nara's public key
	pubKey, err := ParsePublicKey(ln.Me.Status.PublicKey)
	if err != nil {
		t.Fatalf("failed to parse public key: %v", err)
	}
	if !response.VerifySignature(pubKey) {
		t.Error("expected signature verification to pass")
	}
}

func TestHttpEventsSyncHandler_FilterByService(t *testing.T) {
	ln := NewLocalNara("test-nara", testSoul("test-nara"), "host", "user", "pass", -1, 0)
	network := ln.Network

	// Add mixed events
	ln.SyncLedger.AddPingObservation("test-nara", "other", 42.5)
	ln.SyncLedger.AddEvent(NewSocialSyncEvent("tease", "test-nara", "other", "high-restarts", ""))

	// Request only ping events
	reqBody := SyncRequest{
		From:     "requester",
		Services: []string{ServicePing},
	}
	reqJSON, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/events/sync", strings.NewReader(string(reqJSON)))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(network.httpEventsSyncHandler)
	handler.ServeHTTP(rr, req)

	var response SyncResponse
	json.Unmarshal(rr.Body.Bytes(), &response)

	// Should only have ping event
	if len(response.Events) != 1 {
		t.Errorf("expected 1 ping event, got %d", len(response.Events))
	}
	if response.Events[0].Service != ServicePing {
		t.Errorf("expected ping service, got %s", response.Events[0].Service)
	}
}

func TestHttpTeaseCountsHandler(t *testing.T) {
	ln := NewLocalNara("test-nara", testSoul("test-nara"), "host", "user", "pass", -1, 0)
	network := ln.Network

	// Add some tease events
	ln.SyncLedger.AddEvent(NewSocialSyncEvent("tease", "alice", "bob", "high-restarts", ""))
	ln.SyncLedger.AddEvent(NewSocialSyncEvent("tease", "alice", "charlie", "comeback", ""))
	ln.SyncLedger.AddEvent(NewSocialSyncEvent("tease", "bob", "alice", "random", ""))

	req, err := http.NewRequest("GET", "/social/teases", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(network.httpTeaseCountsHandler)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	var response map[string]interface{}
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	if err != nil {
		t.Fatal(err)
	}

	if response["server"] != "test-nara" {
		t.Errorf("expected server to be test-nara, got %v", response["server"])
	}

	teases, ok := response["teases"].([]interface{})
	if !ok {
		t.Fatal("expected teases to be an array")
	}

	// Should have 2 entries (alice: 2, bob: 1)
	if len(teases) != 2 {
		t.Errorf("expected 2 tease entries, got %d", len(teases))
	}

	// First should be alice with 2 teases (sorted by count descending)
	first := teases[0].(map[string]interface{})
	if first["actor"] != "alice" {
		t.Errorf("expected first actor to be alice, got %v", first["actor"])
	}
	if first["count"].(float64) != 2 {
		t.Errorf("expected alice to have 2 teases, got %v", first["count"])
	}
}
