package nara

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

// syncCheckpointsFromNetwork fetches checkpoint history from random online naras
// This recovers the full network timeline after boot recovery completes
// Keeps trying naras until 5 successful responses or all naras exhausted
func (network *Network) syncCheckpointsFromNetwork(online []string) {
	if len(online) == 0 {
		logrus.Debug("📸 No online naras to sync checkpoints from")
		return
	}

	// Shuffle all online naras to randomize selection
	shuffled := make([]string, len(online))
	copy(shuffled, online)
	for i := range shuffled {
		j := i + int(time.Now().UnixNano()%(int64(len(shuffled)-i)))
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}

	const targetSuccessfulFetches = 5
	successfulFetches := 0
	attemptedNaras := []string{}
	totalMerged := 0
	totalWarned := 0

	// Try naras until we get 5 successful fetches or run out of candidates
	for _, naraName := range shuffled {
		if successfulFetches >= targetSuccessfulFetches {
			break
		}

		attemptedNaras = append(attemptedNaras, naraName)

		// Get the nara's IP address
		var ip string
		network.local.mu.Lock()
		if nara, exists := network.Neighbourhood[naraName]; exists {
			nara.mu.Lock()
			ip = nara.Status.MeshIP
			nara.mu.Unlock()
		}
		network.local.mu.Unlock()

		if ip == "" {
			logrus.Debugf("📸 %s: no mesh IP, skipping", naraName)
			continue
		}

		// Fetch all checkpoints from this nara (handles pagination internally)
		checkpoints := network.fetchAllCheckpointsFromNara(naraName, ip)
		if len(checkpoints) == 0 {
			logrus.Debugf("📸 %s: no checkpoints returned, trying next nara", naraName)
			continue
		}

		// Merge into our ledger using the same pattern as zine gossip
		// This handles signature verification and triggers projection updates
		added, warned := network.MergeSyncEventsWithVerification(checkpoints)

		logrus.Printf("📸 %s: fetched %d checkpoints, merged %d new ones", naraName, len(checkpoints), added)
		totalMerged += added
		totalWarned += warned
		successfulFetches++
	}

	if totalMerged > 0 {
		logrus.Printf("📸 Checkpoint sync complete: %d new checkpoints from %d/%d naras (attempted: %v)",
			totalMerged, successfulFetches, len(attemptedNaras), attemptedNaras)
		if totalWarned > 0 {
			logrus.Warnf("📸 Warning: %d checkpoints had signature verification issues", totalWarned)
		}
	} else {
		logrus.Debugf("📸 Checkpoint sync complete: no new checkpoints (attempted %d naras: %v)",
			len(attemptedNaras), attemptedNaras)
	}
}

// fetchAllCheckpointsFromNara fetches all checkpoint events from a remote nara via HTTP
// Handles pagination automatically to retrieve the complete checkpoint history
func (network *Network) fetchAllCheckpointsFromNara(naraName, ip string) []SyncEvent {
	// Allow tests to work without tsnetMesh if testHTTPClient is set
	if network.tsnetMesh == nil && network.testHTTPClient == nil {
		return nil
	}

	client := network.getMeshHTTPClient()
	if client == nil {
		return nil
	}

	var allCheckpoints []SyncEvent
	offset := 0
	limit := 1000 // fetch in batches of 1000

	for {
		// Build URL with pagination parameters using mesh helper (handles test vs production IPs)
		url := network.buildMeshURLFromIP(ip, fmt.Sprintf("/api/checkpoints/all?limit=%d&offset=%d", limit, offset))

		// Create request with timeout
		ctx, cancel := context.WithTimeout(network.ctx, 10*time.Second)
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			cancel()
			logrus.Debugf("📸 %s: failed to create request: %v", naraName, err)
			break
		}

		resp, err := client.Do(req)
		if err != nil {
			cancel()
			logrus.Debugf("📸 %s: failed to fetch checkpoints: %v", naraName, err)
			break
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			cancel()
			logrus.Debugf("📸 %s: bad status: %d", naraName, resp.StatusCode)
			break
		}

		// Parse response
		var response struct {
			Server      string       `json:"server"`
			Total       int          `json:"total"`
			Count       int          `json:"count"`
			Checkpoints []*SyncEvent `json:"checkpoints"`
			HasMore     bool         `json:"has_more"`
			Offset      int          `json:"offset"`
			Limit       int          `json:"limit"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			resp.Body.Close()
			cancel()
			logrus.Debugf("📸 %s: failed to decode response: %v", naraName, err)
			break
		}
		resp.Body.Close()
		cancel()

		// Convert pointers to values for MergeSyncEventsWithVerification
		for _, cp := range response.Checkpoints {
			if cp != nil {
				allCheckpoints = append(allCheckpoints, *cp)
			}
		}

		// Check if there are more pages
		if !response.HasMore {
			break
		}

		offset += limit
	}

	return allCheckpoints
}
