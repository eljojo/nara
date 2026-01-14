package nara

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"tailscale.com/tsnet"
)

// MeshClient handles authenticated HTTP communication over the Tailscale mesh.
// It encapsulates the HTTP client and identity used for signing requests.
// This allows both the main nara app and external tools (like nara-backup)
// to share the same mesh communication logic.
type MeshClient struct {
	httpClient *http.Client
	name       string      // Who we are (for request signing)
	keypair    NaraKeypair // For signing requests
}

// NewMeshClient creates a new mesh client with the given identity
func NewMeshClient(httpClient *http.Client, name string, keypair NaraKeypair) *MeshClient {
	return &MeshClient{
		httpClient: httpClient,
		name:       name,
		keypair:    keypair,
	}
}

// signRequest adds mesh authentication headers to an HTTP request
func (m *MeshClient) signRequest(req *http.Request) {
	timestamp := time.Now().UnixMilli()
	message := fmt.Sprintf("%s%d%s%s", m.name, timestamp, req.Method, req.URL.Path)

	req.Header.Set(HeaderNaraName, m.name)
	req.Header.Set(HeaderNaraTimestamp, strconv.FormatInt(timestamp, 10))
	req.Header.Set(HeaderNaraSignature, m.keypair.SignBase64([]byte(message)))
}

// BuildMeshURL builds a mesh URL for a given IP and path
func BuildMeshURL(ip string, path string) string {
	return fmt.Sprintf("http://%s:%d%s", ip, DefaultMeshPort, path)
}

// DiscoverMeshPeers discovers peers using tsnet Status API
func DiscoverMeshPeers(ctx context.Context, server *tsnet.Server) ([]TsnetPeer, error) {
	lc, err := server.LocalClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get local client: %w", err)
	}

	status, err := lc.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get status: %w", err)
	}

	var peers []TsnetPeer
	for _, peer := range status.Peer {
		if len(peer.TailscaleIPs) == 0 || peer.HostName == "" {
			continue
		}
		// Skip backup tool peers
		if len(peer.HostName) >= 12 && peer.HostName[:12] == "nara-backup-" {
			continue
		}
		peers = append(peers, TsnetPeer{
			Name: peer.HostName,
			IP:   peer.TailscaleIPs[0].String(),
		})
	}

	return peers, nil
}

// FetchSyncEvents fetches events from a peer via POST /events/sync
// Returns just the events (for backward compatibility)
func (m *MeshClient) FetchSyncEvents(ctx context.Context, peerIP string, req SyncRequest) ([]SyncEvent, error) {
	resp, err := m.FetchSyncEventsWithCursor(ctx, peerIP, req)
	if err != nil {
		return nil, err
	}
	return resp.Events, nil
}

// FetchSyncEventsWithCursor fetches events from a peer via POST /events/sync
// Returns the full response including NextCursor for pagination
func (m *MeshClient) FetchSyncEventsWithCursor(ctx context.Context, peerIP string, req SyncRequest) (*SyncResponse, error) {
	// Ensure From is set to our identity
	req.From = m.name

	jsonBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := BuildMeshURL(peerIP, "/events/sync")
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	m.signRequest(httpReq)

	resp, err := m.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var response SyncResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &response, nil
}

// FetchCheckpoints fetches checkpoint events from a peer using the unified sync API
// Returns checkpoints in oldest-first order with cursor-based pagination
func (m *MeshClient) FetchCheckpoints(ctx context.Context, peerIP string, cursor string, pageSize int) (*SyncResponse, error) {
	if pageSize <= 0 || pageSize > 5000 {
		pageSize = 1000
	}

	return m.FetchSyncEventsWithCursor(ctx, peerIP, SyncRequest{
		Mode:     "page",
		Services: []string{ServiceCheckpoint},
		Cursor:   cursor,
		PageSize: pageSize,
	})
}

// TODO: Add PostGossipZine when Zine type signature is confirmed
// TODO: Add SendDM when DirectMessage type is confirmed (currently uses SyncEvent)

// Ping sends a ping to a peer and returns the round-trip time
func (m *MeshClient) Ping(ctx context.Context, peerIP string) (time.Duration, error) {
	url := BuildMeshURL(peerIP, "/ping")
	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}
	// Note: /ping doesn't require auth for latency reasons

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	return time.Since(start), nil
}

// TODO: Add RelayWorldMessage when needed
// TODO: Add FetchCoordinates when needed
// TODO: Add StashStore when needed
// TODO: Add StashRetrieve when needed
// TODO: Add StashPush when needed

// Note: Additional mesh client methods can be added as needed.
// Currently implemented: FetchSyncEvents, FetchCheckpoints, Ping
// Sufficient for boot recovery, checkpoint sync, and backup tool.
