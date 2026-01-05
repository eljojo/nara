package nara

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHttpApiJsonHandler(t *testing.T) {
	ln := NewLocalNara("test-nara", "host", "user", "pass", -1)
	network := ln.Network
	
	// Add a neighbour
	otherNara := NewNara("other")
	otherNara.Status.Flair = "🌟"
	network.Neighbourhood["other"] = otherNara

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

	naras := response["naras"].([]interface{})
	if len(naras) != 1 {
		t.Errorf("expected 1 nara in neighbourhood, got %v", len(naras))
	}

	nara0 := naras[0].(map[string]interface{})
	if nara0["Name"] != "other" {
		t.Errorf("expected nara name to be other, got %v", nara0["Name"])
	}
	if nara0["Flair"] != "🌟" {
		t.Errorf("expected nara flair to be 🌟, got %v", nara0["Flair"])
	}
}

func TestReadOnlyLogic(t *testing.T) {
	ln := NewLocalNara("test-nara", "host", "user", "pass", -1)
	network := ln.Network
	network.ReadOnly = true

	// Test that these don't crash and presumably return early
	network.announce()
	network.heyThere()
	network.selfie()
	network.Chau()
}

func TestHttpMetricsHandler(t *testing.T) {
	ln := NewLocalNara("test-nara", "host", "user", "pass", -1)
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
		`nara_info{name="other",flair="🌟"`,
	}

	for _, s := range expectedStrings {
		if !strings.Contains(body, s) {
			t.Errorf("expected metrics to contain %s", s)
		}
	}
}

func TestWaveMessageValidation(t *testing.T) {
	wm := newWaveMessage("test", "hello")
	if !wm.Valid() {
		t.Error("expected wave message to be valid")
	}

	wm2 := WaveMessage{StartNara: "", Body: "hello"}
	if wm2.Valid() {
		t.Error("expected wave message with no StartNara to be invalid")
	}

	wm3 := WaveMessage{StartNara: "test", Body: ""}
	if wm3.Valid() {
		t.Error("expected wave message with no Body to be invalid")
	}
}

func TestWaveMessageMarkAsSeen(t *testing.T) {
	wm := newWaveMessage("test", "hello")
	wm = wm.markAsSeen("nara1")

	if !wm.hasSeen("nara1") {
		t.Error("expected wm to be marked as seen by nara1")
	}

	if len(wm.SeenBy) != 1 {
		t.Errorf("expected 1 seen token, got %d", len(wm.SeenBy))
	}
}
