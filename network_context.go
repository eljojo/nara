package nara

import (
	"context"
	"net/http"

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
	MyName() string
	Keypair() NaraKeypair

	// Mesh HTTP operations
	BuildMeshURL(name string, path string) string
	GetMeshHTTPClient() HTTPClient
	AddMeshAuthHeaders(req *http.Request)
	HasMeshConnectivity(name string) bool

	// Peer information
	GetOnlineMeshPeers() []string
	GetPeerInfo(names []string) []PeerInfo
	GetOnlineStatus(name string) *OnlineState

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

func (c *networkContext) MyName() string {
	return c.network.meName()
}

func (c *networkContext) Keypair() NaraKeypair {
	return c.network.local.Keypair
}

func (c *networkContext) BuildMeshURL(name string, path string) string {
	return c.network.buildMeshURL(name, path)
}

func (c *networkContext) GetMeshHTTPClient() HTTPClient {
	return c.network.getMeshHTTPClient()
}

func (c *networkContext) AddMeshAuthHeaders(req *http.Request) {
	c.network.AddMeshAuthHeaders(req)
}

func (c *networkContext) HasMeshConnectivity(name string) bool {
	return c.network.hasMeshConnectivity(name)
}

func (c *networkContext) GetOnlineMeshPeers() []string {
	return c.network.NeighbourhoodOnlineNames()
}

func (c *networkContext) GetPeerInfo(names []string) []PeerInfo {
	return c.network.getPeerInfo(names)
}

func (c *networkContext) GetOnlineStatus(name string) *OnlineState {
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
		c.network.local.Projections.OnlineStatus().RunOnce()
	}
}

func (c *networkContext) Context() context.Context {
	return c.network.ctx
}

// stashServiceContext extends NetworkContext with stash-specific methods.
// This implements StashServiceDeps.
type stashServiceContext struct {
	networkContext
}

func (c *stashServiceContext) EmitSocialEvent(event SyncEvent) {
	select {
	case c.network.socialInbox <- event:
	default:
		logrus.Debugf("Social inbox full, skipping stash event")
	}
}

// newStashServiceContext creates a StashServiceDeps adapter for the given Network.
func newStashServiceContext(network *Network) StashServiceDeps {
	return &stashServiceContext{
		networkContext: networkContext{network: network},
	}
}
