package main

import (
	"context"
	"fmt"
	"time"

	"github.com/eljojo/nara"
	"github.com/sirupsen/logrus"
	"tailscale.com/tsnet"
)

// BackupMesh wraps a tsnet mesh connection for the backup tool
type BackupMesh struct {
	server       *tsnet.Server
	meshClient   *nara.MeshClient // Reusable mesh HTTP client
	meshHostname string           // Ephemeral hostname on mesh
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

	server, _, err := nara.ConnectToMesh(ctx, config)
	if err != nil {
		return nil, err
	}

	logrus.Info("✅ Connected to mesh")

	// Create mesh HTTP client with optimized timeouts (5s connection, 5s request)
	// Centralized in mesh_client.go for consistency across nara app and backup tool
	client := nara.NewMeshHTTPClient(server)

	// Create mesh client with nara's identity (not ephemeral backup identity)
	meshClient := nara.NewMeshClient(client, naraName, naraKeypair)

	return &BackupMesh{
		server:       server,
		meshClient:   meshClient,
		meshHostname: meshHostname,
	}, nil
}

// DiscoverPeers discovers all peers on the mesh using tsnet Status API
func (m *BackupMesh) DiscoverPeers(ctx context.Context) ([]nara.TsnetPeer, error) {
	return nara.DiscoverMeshPeers(ctx, m.server)
}

// FetchEvents fetches all events from a specific peer via /events/sync
// Uses cursor-based pagination (mode: "page") for complete, deterministic retrieval
func (m *BackupMesh) FetchEvents(ctx context.Context, peerIP string, peerName string) ([]nara.SyncEvent, error) {
	var allEvents []nara.SyncEvent
	cursor := ""
	pageSize := 5000 // Server supports up to 5000 events per page

	for {
		// Fetch next page
		resp, err := m.meshClient.FetchSyncEventsWithCursor(ctx, peerIP, nara.SyncRequest{
			Mode:     "page",
			PageSize: pageSize,
			Cursor:   cursor,
		})

		if err != nil {
			return nil, fmt.Errorf("fetch page from %s (cursor: %s): %w", peerName, cursor, err)
		}

		allEvents = append(allEvents, resp.Events...)

		// If no cursor returned, we've reached the end
		if resp.NextCursor == "" {
			break
		}

		cursor = resp.NextCursor
	}

	logrus.Debugf("📦 Fetched %d total events from %s via pagination", len(allEvents), peerName)
	return allEvents, nil
}

// Close closes the mesh connection
func (m *BackupMesh) Close() error {
	logrus.Info("🔌 Disconnecting from mesh")
	return m.server.Close()
}
