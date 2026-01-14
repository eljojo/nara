package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/eljojo/nara"
	"github.com/sirupsen/logrus"
	"tailscale.com/tsnet"
)

// BackupMesh wraps a tsnet mesh connection for the backup tool
type BackupMesh struct {
	server       *tsnet.Server
	client       *http.Client
	naraName     string // The nara identity we're backing up
	naraKeypair  nara.NaraKeypair
	meshHostname string // Ephemeral hostname on mesh
}

// NewBackupMesh creates a new mesh connection
// Connects with ephemeral identity but signs requests as the provided nara
func NewBackupMesh(ctx context.Context, naraName string, naraSoul nara.SoulV1) (*BackupMesh, error) {
	naraKeypair := nara.DeriveKeypair(naraSoul)

	// Connect with ephemeral hostname (won't conflict with actual nara)
	meshHostname := fmt.Sprintf("nara-backup-%d", time.Now().Unix()%100000)
	logrus.Infof("🕸️  Connecting to mesh as %s (backing up %s)...", meshHostname, naraName)

	config := nara.MeshConnectionConfig{
		Hostname:   meshHostname,
		ControlURL: nara.DefaultHeadscaleURL(),
		AuthKey:    nara.DefaultHeadscaleAuthKey(),
		Ephemeral:  true,
		Verbose:    false,
	}

	server, client, err := nara.ConnectToMesh(ctx, config)
	if err != nil {
		return nil, err
	}

	logrus.Info("✅ Connected to mesh")

	return &BackupMesh{
		server:       server,
		client:       client,
		naraName:     naraName,
		naraKeypair:  naraKeypair,
		meshHostname: meshHostname,
	}, nil
}

// DiscoverPeers discovers all peers on the mesh using tsnet Status API
func (m *BackupMesh) DiscoverPeers(ctx context.Context) ([]nara.TsnetPeer, error) {
	return nara.DiscoverMeshPeers(ctx, m.server)
}

// FetchEvents fetches all events from a specific peer via /events/sync
func (m *BackupMesh) FetchEvents(ctx context.Context, peerIP string, peerName string) ([]nara.SyncEvent, error) {
	url := nara.BuildMeshURL(peerIP, "/events/sync")

	reqBody := nara.SyncRequest{
		From:      m.naraName, // Use nara's name, not ephemeral backup name
		MaxEvents: 100000,     // Request large batch
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Sign request as the nara (not as ephemeral backup identity)
	nara.SignMeshRequest(req, m.naraName, m.naraKeypair)

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	var response nara.SyncResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return response.Events, nil
}

// Close closes the mesh connection
func (m *BackupMesh) Close() error {
	logrus.Info("🔌 Disconnecting from mesh")
	return m.server.Close()
}
