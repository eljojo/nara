package nara

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHttpNaraeJsonHandler(t *testing.T) {
	ln := NewLocalNara("test-nara", "test-soul", "host", "user", "pass", -1)
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
	ln := NewLocalNara("test-nara", "test-soul", "host", "user", "pass", -1)
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
	ln := NewLocalNara("test-nara", "test-soul", "host", "user", "pass", -1)
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
	ln := NewLocalNara("test-nara", "test-soul", "host", "user", "pass", -1)
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
