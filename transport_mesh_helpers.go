package nara

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"tailscale.com/ipn/store/mem"
	"tailscale.com/tsnet"
)

// MeshConnectionConfig holds configuration for connecting to the mesh
type MeshConnectionConfig struct {
	Hostname   string
	ControlURL string
	AuthKey    string
	Ephemeral  bool
	Verbose    bool
}

// ConnectToMesh creates a tsnet connection and returns the server and HTTP client
func ConnectToMesh(ctx context.Context, config MeshConnectionConfig) (*tsnet.Server, *http.Client, error) {
	server := &tsnet.Server{
		Hostname:   config.Hostname,
		ControlURL: config.ControlURL,
		AuthKey:    config.AuthKey,
		Store:      new(mem.Store),
		Ephemeral:  config.Ephemeral,
	}

	if !config.Verbose {
		server.Logf = func(format string, args ...interface{}) {} // Suppress logs
	}

	// Start and wait for connection
	if _, err := server.Up(ctx); err != nil {
		return nil, nil, fmt.Errorf("failed to connect to mesh: %w", err)
	}

	return server, server.HTTPClient(), nil
}
