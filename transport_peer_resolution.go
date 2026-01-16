package nara

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/eljojo/nara/types"
)

// --- Peer Resolution Protocol ---
// When we don't know a peer's identity (public key), we can query neighbors.
// Uses HTTP redirects: if a neighbor doesn't know, they redirect us to try someone else.

// resolvePeerBackground triggers resolvePeer in a goroutine with rate limiting
func (network *Network) resolvePeerBackground(target types.NaraName) {
	now := time.Now().Unix()
	const retryInterval = 600 // 10 minutes

	// Check if we're already resolving or tried recently
	if lastTry, loaded := network.pendingResolutions.LoadOrStore(target, now); loaded {
		if now-lastTry.(int64) < retryInterval {
			// Too soon to retry or already in progress
			return
		}
		// Update the timestamp for the new attempt
		network.pendingResolutions.Store(target, now)
	}

	go func() {
		// Ensure we eventually allow retries even if it fails/panics
		// (though the LoadOrStore above handles the interval)
		network.resolvePeer(target)
	}()
}

// resolvePeer queries neighbors to discover the identity of an unknown peer.
// Returns nil if no one knows the target within the timeout.
func (network *Network) resolvePeer(target types.NaraName) *PeerResponse {
	// Double check we don't already have it (e.g. if another resolution finished)
	if network.getPublicKeyForNara(target) != nil {
		return nil
	}

	logrus.Debugf("📡 Attempting to resolve unknown peer: %s", target)

	// Check if we already know this peer
	if network.getPublicKeyForNara(target) != nil {
		network.local.mu.Lock()
		nara := network.Neighbourhood[target]
		network.local.mu.Unlock()
		if nara != nil {
			nara.mu.Lock()
			resp := &PeerResponse{
				Target:    target,
				PublicKey: nara.Status.PublicKey,
				MeshIP:    nara.Status.MeshIP,
			}
			nara.mu.Unlock()
			return resp
		}
	}

	// Track who we've already asked to prevent loops
	asked := map[types.NaraName]bool{network.meName(): true}

	// Get initial neighbors to query
	neighbors := network.NeighbourhoodOnlineNames()
	if len(neighbors) == 0 {
		return nil
	}

	// Create HTTP client that doesn't follow redirects automatically
	client := network.getHTTPClient()
	noRedirectClient := &http.Client{
		Timeout: client.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // Don't follow redirects automatically
		},
	}

	// Try each neighbor, following redirects manually
	toAsk := make([]types.NaraName, len(neighbors))
	copy(toAsk, neighbors)

	for len(toAsk) > 0 && len(asked) < 10 { // Max 10 hops
		name := toAsk[0]
		toAsk = toAsk[1:]

		if asked[name] {
			continue
		}
		asked[name] = true

		url := network.getMeshURLForNara(name)
		if url == "" {
			continue
		}

		// Build asked list for the request
		askedList := make([]types.NaraName, 0, len(asked))
		for n := range asked {
			askedList = append(askedList, n)
		}

		resp := network.queryPeerAt(noRedirectClient, url, target, askedList)
		if resp == nil {
			continue
		}

		if resp.PublicKey != "" {
			// Found it! Import and return
			newNara := NewNara(target)
			newNara.Status.PublicKey = resp.PublicKey
			newNara.Status.MeshIP = resp.MeshIP
			newNara.Status.MeshEnabled = resp.MeshIP != ""
			network.importNara(newNara)
			logrus.Infof("📡 Resolved peer %s via query to %s (🔑)", target, name)
			return resp
		}

		// Got a redirect suggestion - add to list if not already asked
		if resp.Target != "" && !asked[resp.Target] {
			toAsk = append(toAsk, resp.Target)
		}
	}

	logrus.Debugf("📡 Peer resolution failed for %s after asking %d neighbors", target, len(asked)-1)
	return nil
}

// TODO: Add integration test for queryPeerAt using httptest.NewServer pattern
// See TestIntegration_CheckpointSync for reference on how to inject HTTP mux/client
// This should test the full HTTP request/response flow for peer queries
//
// queryPeerAt sends a peer query to a specific URL and handles the response.
// Returns a PeerResponse with PublicKey set if found, or with Target set if redirected.
func (network *Network) queryPeerAt(client *http.Client, baseURL string, target types.NaraName, asked []types.NaraName) *PeerResponse {
	// Convert asked to string slice for joining
	askedStr := make([]string, len(asked))
	for i, name := range asked {
		askedStr[i] = name.String()
	}

	// Build query URL with parameters
	queryURL := fmt.Sprintf("%s/peer/query?target=%s&asked=%s",
		baseURL, target, strings.Join(askedStr, ","))

	req, err := http.NewRequest("GET", queryURL, nil)
	if err != nil {
		return nil
	}

	resp, err := client.Do(req)
	if err != nil {
		logrus.Debugf("📡 Peer query to %s failed: %v", baseURL, err)
		return nil
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// They know the target - parse response
		var peerResp PeerResponse
		if err := json.NewDecoder(resp.Body).Decode(&peerResp); err != nil {
			return nil
		}
		return &peerResp

	case http.StatusTemporaryRedirect, http.StatusSeeOther:
		// They don't know, but suggest someone else
		// The redirect location contains the suggested neighbor's name
		location := resp.Header.Get("X-Nara-Redirect-To")
		if location != "" {
			return &PeerResponse{Target: types.NaraName(location)} // Target field used to indicate redirect
		}
		return nil

	case http.StatusNotFound:
		// They don't know and have no suggestions
		return nil

	default:
		return nil
	}
}

// httpPeerQueryHandler handles incoming peer queries.
// GET /peer/query?target=name&asked=a,b,c
// Returns: 200 + JSON if known, 307 + X-Nara-Redirect-To if redirecting, 404 if unknown
func (network *Network) httpPeerQueryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	target := r.URL.Query().Get("target")
	if target == "" {
		http.Error(w, "target parameter required", http.StatusBadRequest)
		return
	}

	// Parse the asked list
	askedStr := r.URL.Query().Get("asked")
	asked := make(map[types.NaraName]bool)
	if askedStr != "" {
		for _, name := range strings.Split(askedStr, ",") {
			asked[types.NaraName(name)] = true
		}
	}
	asked[network.meName()] = true // We've now been asked

	// Check if we know the target
	naraName := types.NaraName(target)
	pubKey := network.getPublicKeyForNara(naraName)
	if pubKey != nil {
		network.local.mu.Lock()
		nara := network.Neighbourhood[naraName]
		network.local.mu.Unlock()

		if nara != nil {
			nara.mu.Lock()
			response := PeerResponse{
				Target:    naraName,
				PublicKey: nara.Status.PublicKey,
				MeshIP:    nara.Status.MeshIP,
			}
			nara.mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(response); err != nil {
				logrus.WithError(err).Warn("Failed to encode response")
			}
			return
		}
	}

	// We don't know - find a neighbor to redirect to
	neighbors := network.NeighbourhoodOnlineNames()
	for _, name := range neighbors {
		if !asked[name] {
			// Redirect to this neighbor
			w.Header().Set("X-Nara-Redirect-To", name.String()) // NaraName to string
			w.WriteHeader(http.StatusTemporaryRedirect)
			return
		}
	}

	// No one else to ask
	http.NotFound(w, r)
}
