package nara

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sirupsen/logrus"

	"github.com/eljojo/nara/types"
)

// GET /api/stash/status - Get current stash data, confidants, and metrics
func (network *Network) httpStashStatusHandler(w http.ResponseWriter, r *http.Request) {
	// Get storage limit from stash service (based on memory mode)
	storageLimit := 5 // Default if service not available
	if network.stashService != nil {
		storageLimit = network.stashService.StorageLimit()
	}

	response := map[string]interface{}{
		"has_stash":  false,
		"my_stash":   nil,
		"confidants": []map[string]interface{}{},
		"metrics": map[string]interface{}{
			"stashes_stored": 0,
			"total_bytes":    0,
			"storage_limit":  storageLimit,
		},
	}

	if network.stashService == nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			logrus.Errorf("Failed to encode stash status response: %v", err)
		}
		return
	}

	// Get current stash data
	myStashData, timestamp := network.stashService.GetStashData()
	if len(myStashData) > 0 {
		var dataMap map[string]interface{}
		_ = json.Unmarshal(myStashData, &dataMap)
		response["has_stash"] = true
		response["my_stash"] = map[string]interface{}{
			"timestamp": timestamp,
			"data":      dataMap,
		}
	}

	// Get current stash state
	if stateBytes, err := network.stashService.MarshalState(); err == nil && len(stateBytes) > 0 {
		var state struct {
			Confidants []string               `json:"confidants"`
			Stored     map[string]interface{} `json:"stored"`
		}

		if err := json.Unmarshal(stateBytes, &state); err == nil {
			// Build confidants list with peer details
			// Quickly gather references with minimal lock time
			confidantList := make([]map[string]interface{}, 0, len(state.Confidants))

			for _, confidantID := range state.Confidants {
				info := map[string]interface{}{
					"id":     confidantID,
					"status": "confirmed",
				}

				// Try to get peer details from NeighbourhoodByID (confidantID is a types.NaraID, not a name!)
				naraID := types.NaraID(confidantID)
				network.local.mu.Lock()
				nara, exists := network.NeighbourhoodByID[naraID]
				network.local.mu.Unlock()

				if exists {
					nara.mu.Lock()
					naraName := nara.Name
					info["name"] = naraName
					info["memory_mode"] = nara.Status.MemoryMode
					nara.mu.Unlock()

					// Check if online via observation (using name, not ID)
					network.local.mu.Lock()
					if obs, ok := network.local.Me.Status.Observations[naraName]; ok {
						info["online"] = obs.isOnline()
					}
					network.local.mu.Unlock()
				}

				confidantList = append(confidantList, info)
			}

			response["confidants"] = confidantList
			response["target_count"] = network.stashService.TargetConfidants()

			// Metrics from stored stashes (we're a confidant for these)
			totalBytes := 0
			for _, stash := range state.Stored {
				if encStash, ok := stash.(map[string]interface{}); ok {
					if ciphertext, ok := encStash["Ciphertext"].([]interface{}); ok {
						totalBytes += len(ciphertext)
					}
				}
			}
			response["metrics"] = map[string]interface{}{
				"stashes_stored": len(state.Stored),
				"total_bytes":    totalBytes,
				"storage_limit":  storageLimit,
			}
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logrus.Errorf("Failed to encode stash status response: %v", err)
	}
}

// POST /api/stash/update - Update stash data and distribute to confidants
func (network *Network) httpStashUpdateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if network.stashService == nil {
		http.Error(w, "Stash service not initialized", http.StatusInternalServerError)
		return
	}

	// Read raw JSON body
	var data json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Update stash data and distribute to all confidants
	if err := network.stashService.SetStashData(data); err != nil {
		logrus.Errorf("📦 Failed to update stash: %v", err)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if encErr := json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": fmt.Sprintf("Failed to distribute stash: %v", err),
		}); encErr != nil {
			logrus.Errorf("Failed to encode stash update error response: %v", encErr)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Stash updated and distributed to confidants",
	}); err != nil {
		logrus.Errorf("Failed to encode stash update success response: %v", err)
	}
}

// POST /api/stash/recover - Trigger manual stash recovery from confidants
func (network *Network) httpStashRecoverHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if network.stashService == nil {
		http.Error(w, "Stash service not initialized", http.StatusInternalServerError)
		return
	}

	// Trigger recovery from any available confidant
	go func() {
		data, err := network.stashService.RecoverFromAny()
		if err != nil {
			logrus.Errorf("📦 Stash recovery failed: %v", err)
		} else {
			logrus.Infof("📦 Stash recovered successfully (%d bytes)", len(data))
			// Store the recovered data locally
			if err := network.stashService.SetStashData(data); err != nil {
				logrus.Errorf("📦 Failed to store recovered stash: %v", err)
			}
		}
	}()

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Stash recovery initiated from confidants",
	}); err != nil {
		logrus.Errorf("Failed to encode stash recovery response: %v", err)
	}
}

// GET /api/stash/confidants - Get list of confidants with details
func (network *Network) httpStashConfidantsHandler(w http.ResponseWriter, r *http.Request) {
	confidants := []map[string]interface{}{}

	if network.stashService != nil {
		if stateBytes, err := network.stashService.MarshalState(); err == nil {
			var state struct {
				Confidants []string `json:"confidants"`
			}

			if err := json.Unmarshal(stateBytes, &state); err == nil {
				for _, confidantID := range state.Confidants {
					info := map[string]interface{}{
						"id":     confidantID,
						"status": "confirmed",
					}

					// Get peer details if available (confidantID is a types.NaraID, not a name!)
					naraID := types.NaraID(confidantID)
					network.local.mu.Lock()
					nara, exists := network.NeighbourhoodByID[naraID]
					network.local.mu.Unlock()

					if exists {
						nara.mu.Lock()
						naraName := nara.Name
						info["name"] = naraName
						info["memory_mode"] = nara.Status.MemoryMode
						nara.mu.Unlock()

						// Check if online via observation (using name, not ID)
						network.local.mu.Lock()
						if obs, ok := network.local.Me.Status.Observations[naraName]; ok {
							info["online"] = obs.isOnline()
						}
						network.local.mu.Unlock()
					}

					confidants = append(confidants, info)
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"confidants": confidants,
	}); err != nil {
		logrus.Errorf("Failed to encode stash confidants response: %v", err)
	}
}
