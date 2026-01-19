package nara

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eljojo/nara/identity"
)

// TestIntegration_EventImport_ValidSoul tests importing events with matching soul
func TestIntegration_EventImport_ValidSoul(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Create test nara with known soul
	soulStr := testSoul("test-nara")
	ln := testNara(t, "test-nara", WithSoul(soulStr))

	// Start HTTP server
	server := httptest.NewServer(ln.Network.createHTTPMux(false))
	defer server.Close()

	// Create some test events
	events := []SyncEvent{
		NewPingSyncEvent("alice", "bob", 10.5),
		NewPingSyncEvent("alice", "carol", 20.3),
	}

	// Create import request signed with the same soul
	soul, err := identity.ParseSoul(soulStr)
	if err != nil {
		t.Fatalf("Failed to parse soul: %v", err)
	}
	keypair := identity.DeriveKeypair(soul)

	request := EventImportRequest{
		Events:    events,
		Timestamp: time.Now().Unix(),
	}

	// Sign: sha256(timestamp:event_ids) - soul not sent in request
	hasher := sha256.New()
	hasher.Write([]byte(fmt.Sprintf("%d:", request.Timestamp)))
	for _, e := range request.Events {
		hasher.Write([]byte(e.ID))
	}
	data := hasher.Sum(nil)
	request.Signature = keypair.SignBase64(data)

	// POST to /api/events/import
	jsonBody, _ := json.Marshal(request)
	resp, err := http.Post(server.URL+"/api/events/import", "application/json", bytes.NewReader(jsonBody))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	// Parse response
	var importResp EventImportResponse
	if err := json.NewDecoder(resp.Body).Decode(&importResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !importResp.Success {
		t.Fatalf("Import failed: %s", importResp.Error)
	}

	if importResp.Imported != 2 {
		t.Errorf("Expected 2 imported events, got %d", importResp.Imported)
	}

	// Verify events are in ledger
	ledgerEvents := ln.SyncLedger.Events
	if len(ledgerEvents) != 2 {
		t.Errorf("Expected 2 events in ledger, got %d", len(ledgerEvents))
	}

	t.Logf("✅ Successfully imported %d events", importResp.Imported)
}

// TestIntegration_EventImport_InvalidSoul tests that wrong soul is rejected
func TestIntegration_EventImport_InvalidSoul(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Create test nara with one soul
	soulStr := testSoul("test-nara")
	ln := testNara(t, "test-nara", WithSoul(soulStr))

	// Start HTTP server
	server := httptest.NewServer(ln.Network.createHTTPMux(false))
	defer server.Close()

	// Create import request signed with a DIFFERENT soul
	differentSoulStr := testSoul("different-nara")
	differentSoul, err := identity.ParseSoul(differentSoulStr)
	if err != nil {
		t.Fatalf("Failed to parse different soul: %v", err)
	}
	differentKeypair := identity.DeriveKeypair(differentSoul)

	events := []SyncEvent{
		NewPingSyncEvent("alice", "bob", 10.5),
	}

	request := EventImportRequest{
		Events:    events,
		Timestamp: time.Now().Unix(),
	}

	// Sign with the different soul's keypair (will fail since server expects its own soul's signature)
	hasher := sha256.New()
	hasher.Write([]byte(fmt.Sprintf("%d:", request.Timestamp)))
	for _, e := range request.Events {
		hasher.Write([]byte(e.ID))
	}
	data := hasher.Sum(nil)
	request.Signature = differentKeypair.SignBase64(data)

	// POST to /api/events/import
	jsonBody, _ := json.Marshal(request)
	resp, err := http.Post(server.URL+"/api/events/import", "application/json", bytes.NewReader(jsonBody))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Should be rejected with 403 Forbidden
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d", resp.StatusCode)
	}

	// Verify no events were imported
	if len(ln.SyncLedger.Events) != 0 {
		t.Errorf("Expected 0 events in ledger, got %d", len(ln.SyncLedger.Events))
	}

	t.Log("✅ Wrong soul correctly rejected")
}

// TestIntegration_EventImport_ExpiredTimestamp tests replay protection
func TestIntegration_EventImport_ExpiredTimestamp(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	soulStr := testSoul("test-nara")
	ln := testNara(t, "test-nara", WithSoul(soulStr))

	server := httptest.NewServer(ln.Network.createHTTPMux(false))
	defer server.Close()

	soul, _ := identity.ParseSoul(soulStr)
	keypair := identity.DeriveKeypair(soul)

	events := []SyncEvent{
		NewPingSyncEvent("alice", "bob", 10.5),
	}

	// Create request with timestamp from 10 minutes ago (> 5 min window)
	request := EventImportRequest{
		Events:    events,
		Timestamp: time.Now().Unix() - 600, // 10 minutes ago
	}

	// Sign with old timestamp
	hasher := sha256.New()
	hasher.Write([]byte(fmt.Sprintf("%d:", request.Timestamp)))
	for _, e := range request.Events {
		hasher.Write([]byte(e.ID))
	}
	data := hasher.Sum(nil)
	request.Signature = keypair.SignBase64(data)

	// POST to /api/events/import
	jsonBody, _ := json.Marshal(request)
	resp, err := http.Post(server.URL+"/api/events/import", "application/json", bytes.NewReader(jsonBody))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Should be rejected with 403 Forbidden
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d", resp.StatusCode)
	}

	t.Log("✅ Expired timestamp correctly rejected")
}

// TestIntegration_EventImport_Deduplication tests that duplicate events are handled
func TestIntegration_EventImport_Deduplication(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	soulStr := testSoul("test-nara")
	ln := testNara(t, "test-nara", WithSoul(soulStr))

	server := httptest.NewServer(ln.Network.createHTTPMux(false))
	defer server.Close()

	soul, _ := identity.ParseSoul(soulStr)
	keypair := identity.DeriveKeypair(soul)

	// Create events
	events := []SyncEvent{
		NewPingSyncEvent("alice", "bob", 10.5),
		NewPingSyncEvent("alice", "carol", 20.3),
	}

	// First import
	request := EventImportRequest{
		Events:    events,
		Timestamp: time.Now().Unix(),
	}

	hasher := sha256.New()
	hasher.Write([]byte(fmt.Sprintf("%d:", request.Timestamp)))
	for _, e := range request.Events {
		hasher.Write([]byte(e.ID))
	}
	data := hasher.Sum(nil)
	request.Signature = keypair.SignBase64(data)

	jsonBody, _ := json.Marshal(request)
	resp, _ := http.Post(server.URL+"/api/events/import", "application/json", bytes.NewReader(jsonBody))
	resp.Body.Close()

	// Verify first import
	if len(ln.SyncLedger.Events) != 2 {
		t.Fatalf("Expected 2 events after first import, got %d", len(ln.SyncLedger.Events))
	}

	// Second import with same events (duplicates)
	request2 := EventImportRequest{
		Events:    events,
		Timestamp: time.Now().Unix(),
	}

	hasher2 := sha256.New()
	hasher2.Write([]byte(fmt.Sprintf("%d:", request2.Timestamp)))
	for _, e := range request2.Events {
		hasher2.Write([]byte(e.ID))
	}
	data2 := hasher2.Sum(nil)
	request2.Signature = keypair.SignBase64(data2)

	jsonBody2, _ := json.Marshal(request2)
	resp2, err := http.Post(server.URL+"/api/events/import", "application/json", bytes.NewReader(jsonBody2))
	if err != nil {
		t.Fatalf("Failed to post second import: %v", err)
	}
	defer resp2.Body.Close()

	var importResp EventImportResponse
	if err := json.NewDecoder(resp2.Body).Decode(&importResp); err != nil {
		t.Fatalf("Failed to decode second import response: %v", err)
	}

	if !importResp.Success {
		t.Fatalf("Second import failed: %s", importResp.Error)
	}

	// Should report 0 imported, 2 duplicates
	if importResp.Imported != 0 {
		t.Errorf("Expected 0 new imports, got %d", importResp.Imported)
	}
	if importResp.Duplicates != 2 {
		t.Errorf("Expected 2 duplicates, got %d", importResp.Duplicates)
	}

	// Ledger should still have only 2 events
	if len(ln.SyncLedger.Events) != 2 {
		t.Errorf("Expected 2 events in ledger after deduplication, got %d", len(ln.SyncLedger.Events))
	}

	t.Logf("✅ Deduplication works: %d imported, %d duplicates", importResp.Imported, importResp.Duplicates)
}
