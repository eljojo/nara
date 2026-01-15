package nara

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sirupsen/logrus"
)

// GET /api/stash/status - Get current stash data, confidants, and metrics
func (network *Network) httpStashStatusHandler(w http.ResponseWriter, r *http.Request) {
	// Get storage limit from runtime (based on memory mode)
	storageLimit := 5 // Default
	if network.runtime != nil {
		storageLimit = network.runtime.StorageLimit()
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
		json.NewEncoder(w).Encode(response)
		return
	}

	// Get current stash data
	myStashData, timestamp := network.stashService.GetStashData()
	if len(myStashData) > 0 {
		var dataMap map[string]interface{}
		json.Unmarshal(myStashData, &dataMap)
		response["has_stash"] = true
		response["my_stash"] = map[string]interface{}{
			"timestamp": timestamp,
			"data":      dataMap,
		}
	}

	// Get current stash state
	if stateBytes, err := network.stashService.MarshalState(); err == nil && len(stateBytes) > 0 {
		var state struct {
			Confidants []string                   `json:"confidants"`
			Stored     map[string]interface{} `json:"stored"`
		}

		if err := json.Unmarshal(stateBytes, &state); err == nil {
			// Build confidants list with peer details
			confidantList := make([]map[string]interface{}, 0, len(state.Confidants))
			network.local.mu.Lock()
			for _, confidantID := range state.Confidants {
				info := map[string]interface{}{
					"id":     confidantID,
					"status": "confirmed",
				}

				// Try to get peer details from Neighbourhood
				if nara, ok := network.Neighbourhood[confidantID]; ok {
					nara.mu.Lock()
					info["name"] = nara.Name
					// Check if online via observation
					if obs, ok := network.local.Me.Status.Observations[confidantID]; ok {
						info["online"] = obs.isOnline()
					}
					info["memory_mode"] = nara.Status.MemoryMode
					nara.mu.Unlock()
				}

				confidantList = append(confidantList, info)
			}
			network.local.mu.Unlock()

			response["confidants"] = confidantList
			response["target_count"] = len(state.Confidants)

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
	json.NewEncoder(w).Encode(response)
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
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": fmt.Sprintf("Failed to distribute stash: %v", err),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Stash updated and distributed to confidants",
	})
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
		}
	}()

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Stash recovery initiated from confidants",
	})
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
				network.local.mu.Lock()
				for _, confidantID := range state.Confidants {
					info := map[string]interface{}{
						"id":     confidantID,
						"status": "confirmed",
					}

					// Get peer details if available
					if nara, ok := network.Neighbourhood[confidantID]; ok {
						nara.mu.Lock()
						info["name"] = nara.Name
						// Check if online via observation
						if obs, ok := network.local.Me.Status.Observations[confidantID]; ok {
							info["online"] = obs.isOnline()
						}
						info["memory_mode"] = nara.Status.MemoryMode
						nara.mu.Unlock()
					}

					confidants = append(confidants, info)
				}
				network.local.mu.Unlock()
			}
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"confidants": confidants,
	})
}
