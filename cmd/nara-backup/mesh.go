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

	server, client, err := nara.ConnectToMesh(ctx, config)
	if err != nil {
		return nil, err
	}

	logrus.Info("✅ Connected to mesh")

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
// Uses slicing strategy (like boot recovery) instead of time-based pagination
// because maxEvents returns the NEWEST events, making sinceTime pagination impossible
func (m *BackupMesh) FetchEvents(ctx context.Context, peerIP string, peerName string) ([]nara.SyncEvent, error) {
	// Strategy: Make one request with maxEvents=0 to get all events (capped at 2000 by server)
	// If we get exactly 2000 events, there might be more, so we'll use slicing to get the rest

	// First request: try to get all events
	events, err := m.meshClient.FetchSyncEvents(ctx, peerIP, nara.SyncRequest{
		MaxEvents: 0, // No limit (server will cap at 2000)
	})

	if err != nil {
		return nil, fmt.Errorf("fetch from %s: %w", peerName, err)
	}

	// If we got fewer than 2000 events, that's everything
	if len(events) < 2000 {
		return events, nil
	}

	// We got exactly 2000 events, which means there are likely more
	// Use slicing strategy to fetch the rest
	// We'll request multiple slices until we've covered the entire event space
	seenIDs := make(map[string]bool)
	for _, e := range events {
		seenIDs[e.ID] = true
	}

	allEvents := events
	sliceTotal := 10 // Start with 10 slices

	// Fetch remaining slices (we already got slice 0 implicitly)
	for sliceIndex := 1; sliceIndex < sliceTotal; sliceIndex++ {
		sliceEvents, err := m.meshClient.FetchSyncEvents(ctx, peerIP, nara.SyncRequest{
			SliceIndex: sliceIndex,
			SliceTotal: sliceTotal,
			MaxEvents:  0,
		})

		if err != nil {
			return nil, fmt.Errorf("fetch slice %d from %s: %w", sliceIndex, peerName, err)
		}

		// Add new events
		for _, e := range sliceEvents {
			if !seenIDs[e.ID] {
				allEvents = append(allEvents, e)
				seenIDs[e.ID] = true
			}
		}
	}

	return allEvents, nil
}

// Close closes the mesh connection
func (m *BackupMesh) Close() error {
	logrus.Info("🔌 Disconnecting from mesh")
	return m.server.Close()
}
