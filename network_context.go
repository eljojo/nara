package nara

import (
	"context"
	"net/http"

	"github.com/eljojo/nara/identity"
	"github.com/eljojo/nara/types"
	"github.com/sirupsen/logrus"
)

// HTTPClient is a minimal interface for HTTP operations (allows mocking in tests)
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// NetworkContext provides a clean interface for services to access Network functionality
// without tight coupling. Services should depend on this interface (or an extension of it)
// rather than directly on *Network.
//
// This enables:
// - Easier testing (mock the interface)
// - Clear dependency boundaries
// - Reduced coupling between components
type NetworkContext interface {
	// Identity
	MyName() types.NaraName
	Keypair() identity.NaraKeypair

	// Mesh HTTP operations
	BuildMeshURL(name types.NaraName, path string) string
	GetMeshHTTPClient() HTTPClient
	AddMeshAuthHeaders(req *http.Request)
	HasMeshConnectivity(name types.NaraName) bool

	// Peer information
	GetOnlineMeshPeers() []types.NaraName
	GetPeerInfo(names []types.NaraName) []PeerInfo
	GetOnlineStatus(name types.NaraName) *OnlineState

	// State
	IsReadOnly() bool
	IsBooting() bool
	EnsureProjectionsUpdated()

	// Lifecycle
	Context() context.Context
}

// networkContext adapts Network to implement the NetworkContext interface.
// This is the canonical adapter that services should use.
type networkContext struct {
	network *Network
}

// NewNetworkContext creates a NetworkContext adapter for the given Network.
// This can be used by any service that needs access to network functionality.
func (network *Network) NewNetworkContext() NetworkContext {
	return &networkContext{network: network}
}

func (c *networkContext) MyName() types.NaraName {
	return c.network.meName()
}

func (c *networkContext) Keypair() identity.NaraKeypair {
	return c.network.local.Keypair
}

func (c *networkContext) BuildMeshURL(name types.NaraName, path string) string {
	return c.network.buildMeshURL(name, path)
}

func (c *networkContext) GetMeshHTTPClient() HTTPClient {
	return c.network.getMeshHTTPClient()
}

func (c *networkContext) AddMeshAuthHeaders(req *http.Request) {
	c.network.AddMeshAuthHeaders(req)
}

func (c *networkContext) HasMeshConnectivity(name types.NaraName) bool {
	return c.network.hasMeshConnectivity(name)
}

func (c *networkContext) GetOnlineMeshPeers() []types.NaraName {
	return c.network.NeighbourhoodOnlineNames()
}

func (c *networkContext) GetPeerInfo(names []types.NaraName) []PeerInfo {
	return []PeerInfo{}
}

func (c *networkContext) GetOnlineStatus(name types.NaraName) *OnlineState {
	if c.network.local.Projections == nil {
		return nil
	}
	return c.network.local.Projections.OnlineStatus().GetState(name)
}

func (c *networkContext) IsReadOnly() bool {
	return c.network.ReadOnly
}

func (c *networkContext) IsBooting() bool {
	return c.network.local.isBooting()
}

func (c *networkContext) EnsureProjectionsUpdated() {
	if c.network.local.Projections != nil && !c.network.local.isBooting() {
		if _, err := c.network.local.Projections.OnlineStatus().RunOnce(); err != nil {
			logrus.WithError(err).Warn("Failed to update online status projection")
		}
	}
}

func (c *networkContext) Context() context.Context {
	return c.network.ctx
}

// Stub types for old stash implementation
type PeerInfo struct{}
type StashServiceDeps interface{}
