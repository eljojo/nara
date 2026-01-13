package nara

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// TestStashHTTPExchange_EndToEnd tests the full HTTP-based stash flow:
// 1. Owner creates stash and distributes to confidant
// 2. Confidant accepts and stores the stash
// 3. Owner "restarts" (loses local stash)
// 4. Owner recovers stash from confidant via HTTP
func TestStashHTTPExchange_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Enable debug logs to troubleshoot
	logrus.SetLevel(logrus.DebugLevel)
	defer logrus.SetLevel(logrus.WarnLevel)

	// Start embedded MQTT broker on unique port
	broker := startTestMQTTBroker(t, 11885)
	defer broker.Close()
	time.Sleep(200 * time.Millisecond) // Let broker start

	// Create owner and confidant naras with MQTT broker
	ownerIdentity := testIdentity("owner")
	owner, _ := NewLocalNara(ownerIdentity, "localhost:11885", "", "", 50, DefaultMemoryProfile())
	owner.Network.confidantStore.SetMaxStashes(10)
	owner.Network.stashManager = NewStashManager("owner", owner.Keypair, 2)
	owner.Network.initStashService()

	confidantIdentity := testIdentity("confidant")
	confidant, _ := NewLocalNara(confidantIdentity, "localhost:11885", "", "", 50, DefaultMemoryProfile())
	confidant.Network.confidantStore.SetMaxStashes(10)
	confidant.Network.stashManager = NewStashManager("confidant", confidant.Keypair, 2)
	confidant.Network.initStashService()

	// Connect both to MQTT manually (they would normally connect during Start())
	if token := owner.Network.Mqtt.Connect(); token.Wait() && token.Error() != nil {
		t.Fatalf("Owner MQTT connection failed: %v", token.Error())
	}
	if token := confidant.Network.Mqtt.Connect(); token.Wait() && token.Error() != nil {
		t.Fatalf("Confidant MQTT connection failed: %v", token.Error())
	}
	defer owner.Network.disconnectMQTT()
	defer confidant.Network.disconnectMQTT()
	time.Sleep(100 * time.Millisecond) // Let MQTT connections establish

	// Start HTTP servers for both (with mesh auth middleware)
	ownerMux := http.NewServeMux()
	ownerMux.HandleFunc("/stash/store", owner.Network.meshAuthMiddleware("/stash/store", owner.Network.httpStashHandler))
	ownerMux.HandleFunc("/stash/retrieve", owner.Network.meshAuthMiddleware("/stash/retrieve", owner.Network.httpStashRetrieveHandler))
	ownerMux.HandleFunc("/stash/push", owner.Network.meshAuthMiddleware("/stash/push", owner.Network.httpStashPushHandler))
	ownerHTTP := httptest.NewServer(ownerMux)
	defer ownerHTTP.Close()

	confidantMux := http.NewServeMux()
	confidantMux.HandleFunc("/stash/store", confidant.Network.meshAuthMiddleware("/stash/store", confidant.Network.httpStashHandler))
	confidantMux.HandleFunc("/stash/retrieve", confidant.Network.meshAuthMiddleware("/stash/retrieve", confidant.Network.httpStashRetrieveHandler))
	confidantMux.HandleFunc("/stash/push", confidant.Network.meshAuthMiddleware("/stash/push", confidant.Network.httpStashPushHandler))
	confidantHTTP := httptest.NewServer(confidantMux)
	defer confidantHTTP.Close()

	// Set up test URLs and HTTP client
	sharedClient := &http.Client{Timeout: 5 * time.Second}
	owner.Network.testHTTPClient = sharedClient
	owner.Network.testMeshURLs = map[string]string{"confidant": confidantHTTP.URL}
	confidant.Network.testHTTPClient = sharedClient
	confidant.Network.testMeshURLs = map[string]string{"owner": ownerHTTP.URL}

	// Set up identities so they can verify each other's signatures
	ownerNara := NewNara("owner")
	ownerNara.Status.PublicKey = FormatPublicKey(owner.Keypair.PublicKey)
	confidant.Network.importNara(ownerNara)

	confidantNara := NewNara("confidant")
	confidantNara.Status.PublicKey = FormatPublicKey(confidant.Keypair.PublicKey)
	owner.Network.importNara(confidantNara)

	// STEP 1: Owner creates stash data
	originalData := map[string]interface{}{
		"notes": "My secret data",
		"preferences": map[string]interface{}{
			"theme":         "dark",
			"notifications": true,
		},
		"numbers": []int{1, 2, 3, 4, 5},
	}
	originalDataJSON, err := json.Marshal(originalData)
	if err != nil {
		t.Fatalf("Failed to marshal original data: %v", err)
	}

	// Set current stash on owner
	owner.Network.stashManager.SetCurrentStash(originalDataJSON)

	// STEP 2: Owner exchanges stash with confidant
	owner.Network.exchangeStashWithPeer("confidant")

	// Give it a moment to complete
	time.Sleep(100 * time.Millisecond)

	// STEP 3: Verify confidant has stored the stash
	if !confidant.Network.confidantStore.HasStashFor("owner") {
		t.Fatal("Confidant should have stored owner's stash")
	}

	// Verify owner marked confidant as confirmed
	if !owner.Network.stashManager.confidantTracker.Has("confidant") {
		t.Fatal("Owner should have marked confidant as confirmed")
	}

	metrics := confidant.Network.confidantStore.GetMetrics()
	if metrics.StashesStored != 1 {
		t.Errorf("Expected confidant to store 1 stash, got %d", metrics.StashesStored)
	}

	// STEP 4: Simulate owner restart - loses local stash
	owner.Network.stashManager = NewStashManager("owner", owner.Keypair, 2)
	owner.Network.initStashService()
	if owner.Network.stashManager.HasStashData() {
		t.Fatal("Owner should not have stash data after restart")
	}

	// STEP 5: Owner sends hey-there (simulating boot), confidant sees it and pushes stash back
	pubKeyStr := FormatPublicKey(owner.Keypair.PublicKey)
	heyThereEvent := NewHeyThereSyncEvent("owner", pubKeyStr, "", owner.ID, owner.Keypair)

	// Publish hey-there via MQTT
	topic := "nara/plaza/hey_there"
	owner.Network.postEvent(topic, heyThereEvent)

	// Wait for confidant to process hey-there and push stash back via HTTP
	time.Sleep(3 * time.Second) // Give time for hey-there handler to trigger HTTP push

	// STEP 6: Verify owner recovered stash
	if !owner.Network.stashManager.HasStashData() {
		t.Fatal("Owner should have recovered stash data")
	}

	recoveredStash := owner.Network.stashManager.GetCurrentStash()
	if recoveredStash == nil {
		t.Fatal("Recovered stash should not be nil")
	}

	// STEP 7: Verify recovered data matches original
	var recoveredData map[string]interface{}
	if err := json.Unmarshal(recoveredStash.Data, &recoveredData); err != nil {
		t.Fatalf("Failed to unmarshal recovered data: %v", err)
	}

	// Check notes field
	if notes, ok := recoveredData["notes"].(string); !ok || notes != "My secret data" {
		t.Errorf("Expected notes='My secret data', got %v", recoveredData["notes"])
	}

	// Check preferences
	if prefs, ok := recoveredData["preferences"].(map[string]interface{}); ok {
		if theme, ok := prefs["theme"].(string); !ok || theme != "dark" {
			t.Errorf("Expected theme='dark', got %v", prefs["theme"])
		}
		if notif, ok := prefs["notifications"].(bool); !ok || !notif {
			t.Errorf("Expected notifications=true, got %v", prefs["notifications"])
		}
	} else {
		t.Error("Expected preferences object")
	}

	// Verify confidant is still marked as confirmed
	if !owner.Network.stashManager.confidantTracker.Has("confidant") {
		t.Error("Owner should still have confidant marked after recovery")
	}

	t.Logf("✓ End-to-end stash exchange successful: %d bytes stored and recovered",
		len(recoveredStash.Data))
}

// TestStashHTTPExchange_Rejection tests stash rejection when confidant is at capacity
func TestStashHTTPExchange_Rejection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	logrus.SetLevel(logrus.WarnLevel)
	defer logrus.SetLevel(logrus.WarnLevel)

	// Start MQTT broker
	broker := startTestMQTTBroker(t, 11886)
	defer broker.Close()
	time.Sleep(200 * time.Millisecond)

	// Create owner and confidant
	ownerIdentity := testIdentity("owner")
	owner, _ := NewLocalNara(ownerIdentity, "localhost:11886", "", "", 50, DefaultMemoryProfile())
	owner.Network.confidantStore.SetMaxStashes(5)
	owner.Network.stashManager = NewStashManager("owner", owner.Keypair, 2)
	owner.Network.initStashService()

	confidantIdentity := testIdentity("confidant")
	confidant, _ := NewLocalNara(confidantIdentity, "localhost:11886", "", "", 50, DefaultMemoryProfile())
	confidant.Network.confidantStore.SetMaxStashes(1) // Only 1 stash!
	confidant.Network.stashManager = NewStashManager("confidant", confidant.Keypair, 2)
	confidant.Network.initStashService()

	if token := owner.Network.Mqtt.Connect(); token.Wait() && token.Error() != nil {
		t.Fatalf("Owner MQTT connection failed: %v", token.Error())
	}
	if token := confidant.Network.Mqtt.Connect(); token.Wait() && token.Error() != nil {
		t.Fatalf("Confidant MQTT connection failed: %v", token.Error())
	}
	defer owner.Network.disconnectMQTT()
	defer confidant.Network.disconnectMQTT()
	time.Sleep(100 * time.Millisecond)

	// Start HTTP servers (with mesh auth middleware)
	ownerMux := http.NewServeMux()
	ownerMux.HandleFunc("/stash/store", owner.Network.meshAuthMiddleware("/stash/store", owner.Network.httpStashHandler))
	ownerMux.HandleFunc("/stash/retrieve", owner.Network.meshAuthMiddleware("/stash/retrieve", owner.Network.httpStashRetrieveHandler))
	ownerMux.HandleFunc("/stash/push", owner.Network.meshAuthMiddleware("/stash/push", owner.Network.httpStashPushHandler))
	ownerHTTP := httptest.NewServer(ownerMux)
	defer ownerHTTP.Close()

	confidantMux := http.NewServeMux()
	confidantMux.HandleFunc("/stash/store", confidant.Network.meshAuthMiddleware("/stash/store", confidant.Network.httpStashHandler))
	confidantMux.HandleFunc("/stash/retrieve", confidant.Network.meshAuthMiddleware("/stash/retrieve", confidant.Network.httpStashRetrieveHandler))
	confidantMux.HandleFunc("/stash/push", confidant.Network.meshAuthMiddleware("/stash/push", confidant.Network.httpStashPushHandler))
	confidantHTTP := httptest.NewServer(confidantMux)
	defer confidantHTTP.Close()

	sharedClient := &http.Client{Timeout: 5 * time.Second}
	owner.Network.testHTTPClient = sharedClient
	owner.Network.testMeshURLs = map[string]string{"confidant": confidantHTTP.URL}
	confidant.Network.testHTTPClient = sharedClient
	confidant.Network.testMeshURLs = map[string]string{"owner": ownerHTTP.URL}

	ownerNara := NewNara("owner")
	ownerNara.Status.PublicKey = FormatPublicKey(owner.Keypair.PublicKey)
	confidant.Network.importNara(ownerNara)

	confidantNara := NewNara("confidant")
	confidantNara.Status.PublicKey = FormatPublicKey(confidant.Keypair.PublicKey)
	owner.Network.importNara(confidantNara)

	// Fill confidant's capacity with someone else's stash first
	dummyNara := testLocalNara(t,"dummy")
	dummyData := testStashData(time.Now().Unix(), []string{"🎲"})
	dummyKeypair := DeriveEncryptionKeys(dummyNara.Keypair.PrivateKey)
	dummyPayload, _ := CreateStashPayload("dummy", dummyData, dummyKeypair)
	confidant.Network.confidantStore.Store("dummy", dummyPayload)

	// Verify confidant is at capacity
	metrics := confidant.Network.confidantStore.GetMetrics()
	if metrics.StashesStored != 1 {
		t.Fatalf("Expected 1 stash stored, got %d", metrics.StashesStored)
	}

	// Now owner tries to store stash
	ownerData := map[string]interface{}{"test": "data"}
	ownerDataJSON, _ := json.Marshal(ownerData)
	owner.Network.stashManager.SetCurrentStash(ownerDataJSON)

	owner.Network.exchangeStashWithPeer("confidant")
	time.Sleep(100 * time.Millisecond)

	// Confidant should have rejected (at capacity)
	if confidant.Network.confidantStore.HasStashFor("owner") {
		t.Error("Confidant should have rejected owner's stash (at capacity)")
	}

	// Owner should NOT have confidant marked as confirmed
	if owner.Network.stashManager.confidantTracker.Has("confidant") {
		t.Error("Owner should not mark confidant as confirmed after rejection")
	}

	t.Log("✓ Stash rejection works correctly when at capacity")
}
