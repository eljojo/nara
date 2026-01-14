package nara

import (
	"context"
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

// SignMeshRequest adds mesh authentication headers to an HTTP request
// Uses the provided name and keypair to sign
func SignMeshRequest(req *http.Request, name string, keypair NaraKeypair) {
	timestamp := time.Now().UnixMilli()
	message := fmt.Sprintf("%s%d%s%s", name, timestamp, req.Method, req.URL.Path)

	req.Header.Set(HeaderNaraName, name)
	req.Header.Set(HeaderNaraTimestamp, strconv.FormatInt(timestamp, 10))
	req.Header.Set(HeaderNaraSignature, keypair.SignBase64([]byte(message)))
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

// BuildMeshURL builds a mesh URL for a given IP and path
func BuildMeshURL(ip string, path string) string {
	return fmt.Sprintf("http://%s:%d%s", ip, DefaultMeshPort, path)
}
