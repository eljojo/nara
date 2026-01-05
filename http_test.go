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
	if len(naras) != 2 {
		t.Errorf("expected 2 naras, got %v", len(naras))
	}

	foundOther := false
	foundMe := false
	for _, n := range naras {
		nara := n.(map[string]interface{})
		if nara["Name"] == "other" {
			foundOther = true
			if nara["Flair"] != "🌟" {
				t.Errorf("expected other nara flair to be 🌟, got %v", nara["Flair"])
			}
		}
		if nara["Name"] == "test-nara" {
			foundMe = true
		}
	}

	if !foundOther || !foundMe {
		t.Errorf("expected to find both other and me in the naras list, foundOther: %v, foundMe: %v", foundOther, foundMe)
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
