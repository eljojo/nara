package nara

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// PeerDiscovery is a strategy for discovering mesh peers
// Allows testing without real network scanning
type PeerDiscovery interface {
	// ScanForPeers returns a list of discovered peers with their IPs
	ScanForPeers(myIP string) []DiscoveredPeer
}

// DiscoveredPeer represents a nara found via discovery
type DiscoveredPeer struct {
	Name      string
	MeshIP    string
	PublicKey string // Ed25519 public key (from /ping response)
}

// TailscalePeerDiscovery scans the Tailscale mesh subnet for peers
type TailscalePeerDiscovery struct {
	client *http.Client
}

// ScanForPeers scans 100.64.0.1-254 for naras responding to /ping (parallelized)
func (d *TailscalePeerDiscovery) ScanForPeers(myIP string) []DiscoveredPeer {
	logrus.Debugf("📡 Starting parallel peer scan (myIP=%s)", myIP)

	var peers []DiscoveredPeer
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Use a semaphore to limit concurrent requests (avoid overwhelming the network)
	const maxConcurrent = 50
	sem := make(chan struct{}, maxConcurrent)

	// Scan mesh subnet (100.64.0.0/10 for Tailscale)
	for i := 1; i <= 254; i++ {
		ip := fmt.Sprintf("100.64.0.%d", i)

		// Skip our own IP
		if ip == myIP {
			continue
		}

		wg.Add(1)
		sem <- struct{}{} // Acquire semaphore

		go func(ip string) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore

			// Try to ping this IP
			url := fmt.Sprintf("http://%s:%d/ping", ip, DefaultMeshPort)
			resp, err := d.client.Get(url)
			if err != nil {
				return // Not a nara or unreachable
			}

			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				return
			}

			// Decode to get the nara's name
			var pingResp map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&pingResp); err != nil {
				resp.Body.Close()
				return
			}
			resp.Body.Close()

			naraName, ok := pingResp["from"].(string)
			if !ok || naraName == "" {
				return
			}

			// Extract public key if present
			pubKey, _ := pingResp["public_key"].(string)

			mu.Lock()
			peers = append(peers, DiscoveredPeer{
				Name:      naraName,
				MeshIP:    ip,
				PublicKey: pubKey,
			})
			mu.Unlock()
		}(ip)
	}

	wg.Wait()
	logrus.Debugf("📡 Parallel peer scan complete: found %d peers", len(peers))
	return peers
}

// discoverMeshPeers discovers peers and fetches their public keys
// This replaces MQTT-based discovery in gossip-only mode
// Note: Does NOT bootstrap from peers - that's handled by bootRecovery
func (network *Network) discoverMeshPeers() {
	if network.tsnetMesh == nil {
		return
	}

	myName := network.meName()
	logrus.Debugf("📡 Mesh discovery starting: myName=%s", myName)

	// Try Status API first (instant, no network scanning)
	var peers []DiscoveredPeer
	ctx, cancel := context.WithTimeout(network.ctx, 5*time.Second)
	defer cancel()

	tsnetPeers, err := network.tsnetMesh.Peers(ctx)
	if err != nil {
		logrus.Debugf("📡 Status API failed, falling back to IP scan: %v", err)
		// Fall back to IP scanning if Status API fails (includes public keys)
		if network.peerDiscovery != nil {
			peers = network.peerDiscovery.ScanForPeers(network.tsnetMesh.IP())
		}
	} else {
		// Convert TsnetPeer to DiscoveredPeer (no public keys yet)
		for _, p := range tsnetPeers {
			peers = append(peers, DiscoveredPeer{Name: p.Name, MeshIP: p.IP})
		}
		logrus.Infof("📡 Got %d peers from tsnet Status API (instant!)", len(peers))

		// Fetch public keys from peers in parallel
		peers = network.fetchPublicKeysFromPeers(peers)
	}

	discovered := 0
	for _, peer := range peers {
		// Skip self
		if peer.Name == myName {
			continue
		}

		// Check if we already know this nara
		network.local.mu.Lock()
		existing, exists := network.Neighbourhood[peer.Name]
		network.local.mu.Unlock()

		if !exists {
			// New peer - add to neighborhood with public key
			nara := NewNara(peer.Name)
			nara.Status.MeshIP = peer.MeshIP
			nara.Status.MeshEnabled = true
			nara.Status.PublicKey = peer.PublicKey
			network.importNara(nara)
			network.recordObservationOnlineNara(peer.Name, 0) // Properly sets both Online and LastSeen
			network.emitSeenEvent(peer.Name, "mesh")
			discovered++
			if peer.PublicKey != "" {
				logrus.Infof("📡 Discovered mesh peer: %s at %s (🔑)", peer.Name, peer.MeshIP)
			} else {
				logrus.Infof("📡 Discovered mesh peer: %s at %s (no key yet)", peer.Name, peer.MeshIP)
			}
		} else if peer.PublicKey != "" && existing.Status.PublicKey == "" {
			// Update existing peer with newly discovered public key
			existing.Status.PublicKey = peer.PublicKey
			logrus.Infof("📡 Updated public key for %s (🔑)", peer.Name)
		}
	}

	if discovered > 0 {
		logrus.Printf("📡 Mesh discovery complete: found %d new peers", discovered)
	}
}

// TODO: Add integration test for fetchPublicKeysFromPeers using httptest.NewServer pattern
// See TestIntegration_CheckpointSync for reference on how to inject HTTP mux/client
// This should test parallel fetching of public keys from multiple peers via HTTP
//
// fetchPublicKeysFromPeers pings peers in parallel to get their public keys
// This is necessary because tsnet Status API only gives us names and IPs
func (network *Network) fetchPublicKeysFromPeers(peers []DiscoveredPeer) []DiscoveredPeer {
	if network.tsnetMesh == nil || network.tsnetMesh.Server() == nil {
		return peers
	}

	client := network.getMeshHTTPClient()
	if client == nil {
		return peers
	}

	var wg sync.WaitGroup
	var mu sync.Mutex

	// Process in parallel with a semaphore
	const maxConcurrent = 20
	sem := make(chan struct{}, maxConcurrent)

	for i := range peers {
		if peers[i].PublicKey != "" {
			continue // Already has public key
		}

		wg.Add(1)
		sem <- struct{}{} // Acquire semaphore

		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore

			url := network.buildMeshURLFromIP(peers[idx].MeshIP, "/ping")
			ctx, cancel := context.WithTimeout(network.ctx, 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
			if err != nil {
				return
			}

			resp, err := client.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return
			}

			var pingResp map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&pingResp); err != nil {
				return
			}

			mu.Lock()
			// Get the real nara name from the ping response (not Tailscale hostname)
			if name, ok := pingResp["from"].(string); ok && name != "" {
				peers[idx].Name = name
			}
			if pubKey, ok := pingResp["public_key"].(string); ok && pubKey != "" {
				peers[idx].PublicKey = pubKey
			}
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	// Count how many have public keys
	withKeys := 0
	for _, p := range peers {
		if p.PublicKey != "" {
			withKeys++
		}
	}
	logrus.Debugf("📡 Fetched public keys: %d/%d peers have keys", withKeys, len(peers))

	return peers
}

// meshDiscoveryForever periodically scans for new mesh peers
// Runs in gossip-only or hybrid mode to discover peers without MQTT
func (network *Network) meshDiscoveryForever() {
	// Initial discovery after mesh is ready
	select {
	case <-time.After(35 * time.Second):
		// continue
	case <-network.ctx.Done():
		return
	}
	network.discoverMeshPeers()

	// Periodic re-discovery (every 5 minutes)
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-network.ctx.Done():
			return
		case <-ticker.C:
			network.discoverMeshPeers()
		}
	}
}
