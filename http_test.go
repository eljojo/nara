package nara

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

	// Check that Aura field is present
	firstNara := naras[0].(map[string]interface{})
	aura, hasAura := firstNara["Aura"]
	if !hasAura {
		t.Error("expected nara to have Aura field in /narae.json response")
	}
	if aura == "" {
		t.Error("expected Aura to be non-empty")
	}
	t.Logf("Aura in /narae.json: %v", aura)
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
	ln.Me.Status.Observations["other"] = NaraObservation{Online: "ONLINE", Restarts: 5}
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
	ln.Me.Status.Observations["other"] = NaraObservation{Online: "ONLINE"}
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

func TestHttpDMHandler(t *testing.T) {
	// Create receiver nara
	receiver := NewLocalNara("receiver", testSoul("receiver"), "", "", "", -1, 0)

	// Create sender nara (we need their keypair to sign the event)
	sender := NewLocalNara("sender", testSoul("sender"), "", "", "", -1, 0)

	// Receiver must know about sender (public key) to verify signature
	senderNara := NewNara("sender")
	senderNara.Status.PublicKey = FormatPublicKey(sender.Keypair.PublicKey)
	receiver.Network.importNara(senderNara)

	// Create a signed tease event from sender to receiver
	event := NewTeaseSyncEvent("sender", "receiver", "high-restarts", sender.Keypair)

	// Marshal event to JSON
	eventJSON, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest("POST", "/dm", strings.NewReader(string(eventJSON)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(receiver.Network.httpDMHandler)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v, body: %s", status, http.StatusOK, rr.Body.String())
	}

	var response map[string]interface{}
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	if err != nil {
		t.Fatal(err)
	}

	if response["success"] != true {
		t.Errorf("expected success to be true, got %v", response["success"])
	}

	// Verify event was added to receiver's ledger
	events := receiver.SyncLedger.GetEventsByService(ServiceSocial)
	found := false
	for _, e := range events {
		if e.Social != nil && e.Social.Type == "tease" && e.Social.Actor == "sender" && e.Social.Target == "receiver" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected tease event to be added to receiver's ledger")
	}
}

func TestHttpDMHandler_UnsignedEvent(t *testing.T) {
	receiver := NewLocalNara("receiver", testSoul("receiver"), "", "", "", -1, 0)

	// Create an unsigned event
	event := NewSocialSyncEvent("tease", "sender", "receiver", "high-restarts", "")

	eventJSON, _ := json.Marshal(event)

	req, _ := http.NewRequest("POST", "/dm", strings.NewReader(string(eventJSON)))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(receiver.Network.httpDMHandler)
	handler.ServeHTTP(rr, req)

	// Should reject unsigned events
	if status := rr.Code; status != http.StatusBadRequest {
		t.Errorf("expected status 400 for unsigned event, got %v", status)
	}
}

func TestHttpDMHandler_UnknownEmitter(t *testing.T) {
	receiver := NewLocalNara("receiver", testSoul("receiver"), "", "", "", -1, 0)
	sender := NewLocalNara("unknown-sender", testSoul("unknown-sender"), "", "", "", -1, 0)

	// Create a signed event but receiver doesn't know sender
	event := NewTeaseSyncEvent("unknown-sender", "receiver", "high-restarts", sender.Keypair)

	eventJSON, _ := json.Marshal(event)

	req, _ := http.NewRequest("POST", "/dm", strings.NewReader(string(eventJSON)))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(receiver.Network.httpDMHandler)
	handler.ServeHTTP(rr, req)

	// Should reject events from unknown emitters
	if status := rr.Code; status != http.StatusForbidden {
		t.Errorf("expected status 403 for unknown emitter, got %v", status)
	}
}

func TestHttpDMHandler_InvalidSignature(t *testing.T) {
	receiver := NewLocalNara("receiver", testSoul("receiver"), "", "", "", -1, 0)
	sender := NewLocalNara("sender", testSoul("sender"), "", "", "", -1, 0)
	wrongSender := NewLocalNara("wrong-sender", testSoul("wrong-sender"), "", "", "", -1, 0)

	// Receiver knows about sender
	senderNara := NewNara("sender")
	senderNara.Status.PublicKey = FormatPublicKey(sender.Keypair.PublicKey)
	receiver.Network.importNara(senderNara)

	// Create event claiming to be from sender but signed by wrong keypair
	event := NewTeaseSyncEvent("sender", "receiver", "high-restarts", wrongSender.Keypair)

	eventJSON, _ := json.Marshal(event)

	req, _ := http.NewRequest("POST", "/dm", strings.NewReader(string(eventJSON)))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(receiver.Network.httpDMHandler)
	handler.ServeHTTP(rr, req)

	// Should reject events with invalid signatures
	if status := rr.Code; status != http.StatusForbidden {
		t.Errorf("expected status 403 for invalid signature, got %v", status)
	}
}

func TestHttpDMHandler_MethodNotAllowed(t *testing.T) {
	receiver := NewLocalNara("receiver", testSoul("receiver"), "", "", "", -1, 0)

	req, _ := http.NewRequest("GET", "/dm", nil)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(receiver.Network.httpDMHandler)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405 for GET, got %v", status)
	}
}

func TestHttpEventsSSEHandler(t *testing.T) {
	sender := NewLocalNara("sender", testSoul("sender"), "", "", "", 50, 1000)
	receiver := NewLocalNara("receiver", testSoul("receiver"), "", "", "", 50, 1000)

	// Add sender to receiver's neighbourhood so we can verify events
	receiver.Network.importNara(sender.Me)

	// Create test events of various types to ensure UIFormat works
	socialEvent := NewTeaseSyncEvent("sender", "receiver", "test-reason", sender.Keypair)

	pingEvent := SyncEvent{
		ID:        "ping-test",
		Timestamp: 1000000000,
		Service:   ServicePing,
		Ping: &PingObservation{
			Observer: "sender",
			Target:   "receiver",
			RTT:      12.5,
		},
	}
	pingEvent.Sign("sender", sender.Keypair)

	// Test that we can marshal events with UI format without errors
	testEvents := []SyncEvent{socialEvent, pingEvent}

	for i, event := range testEvents {
		// Simulate what the SSE handler does
		var uiFormat map[string]string
		if event.Social != nil {
			uiFormat = event.Social.UIFormat()
		} else if event.Ping != nil {
			uiFormat = event.Ping.UIFormat()
		}

		if uiFormat == nil {
			t.Errorf("event %d: failed to get UI format", i)
			continue
		}

		data := map[string]interface{}{
			"service":   event.Service,
			"timestamp": event.Timestamp,
			"emitter":   event.Emitter,
			"icon":      uiFormat["icon"],
			"text":      uiFormat["text"],
			"detail":    uiFormat["detail"],
		}

		// Try to marshal - this is where a panic/error would occur
		jsonData, err := json.Marshal(data)
		if err != nil {
			t.Errorf("event %d: failed to marshal event data: %v", i, err)
		}

		// Verify UI format fields are present
		var result map[string]interface{}
		if err := json.Unmarshal(jsonData, &result); err != nil {
			t.Errorf("event %d: failed to unmarshal: %v", i, err)
		}

		if result["icon"] == nil || result["text"] == nil {
			t.Errorf("event %d: icon or text field is missing from marshaled data", i)
		}

		t.Logf("event %d marshaled successfully: %s", i, string(jsonData))
	}

	// Test the actual SSE endpoint WITH middleware to catch issues like responseLogger not implementing http.Flusher
	// Use a context with timeout so the handler doesn't block forever
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req, err := http.NewRequest("GET", "/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()

	// Use the middleware-wrapped handler, not the raw handler
	handler := receiver.Network.loggingMiddleware("/events", receiver.Network.httpEventsSSEHandler)

	// Run handler in goroutine since it blocks
	done := make(chan bool)
	go func() {
		handler.ServeHTTP(rr, req)
		done <- true
	}()

	// Wait for handler to start and send initial event, or timeout
	select {
	case <-done:
		// Handler completed (due to context timeout)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("handler didn't complete within timeout")
	}

	// The handler should start successfully (status 200) and set SSE headers
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler with middleware returned wrong status code: got %v want %v, body: %s",
			status, http.StatusOK, rr.Body.String())
	}

	// Verify it's actually an SSE endpoint
	contentType := rr.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/event-stream") {
		t.Errorf("expected Content-Type to contain text/event-stream, got %s", contentType)
	}

	// Verify the initial connection event is sent
	body := rr.Body.String()
	if !strings.Contains(body, "event: connected") {
		t.Errorf("expected initial connection event, got: %s", body)
	}
}
