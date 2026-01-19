package nara

import (
	"time"

	"github.com/eljojo/nara/runtime"
	"github.com/eljojo/nara/services/stash"

	"github.com/sirupsen/logrus"
)

// initRuntime creates and initializes the Runtime with all services and adapters.
// This wires the new runtime-based architecture into the existing Network.
func (network *Network) initRuntime() error {
	start := time.Now()
	logrus.Debugf("🎯 [TIMING] initRuntime() starting...")

	// Skip if already initialized
	if network.runtime != nil {
		logrus.Debugf("🎯 [TIMING] initRuntime() skipped (already initialized) - took %v", time.Since(start))
		return nil
	}

	// Create adapters to bridge old Network to new runtime interfaces
	transportAdapter := NewTransportAdapter(network)
	keypairAdapter := NewKeypairAdapter(network.local.Keypair)
	eventBusAdapter := NewEventBusAdapter()
	// Identity adapter is available but not yet wired into runtime (will be in Chapter 2)
	_ = NewIdentityAdapter(network)

	// Create ledger adapter (uses existing sync ledger)
	ledgerAdapter := &LedgerAdapter{
		ledger: network.local.SyncLedger,
	}

	// Create gossip queue adapter
	gossipAdapter := &GossipQueueAdapter{
		network: network,
	}

	// Create network info adapter (for peer/memory information)
	networkInfoAdapter := NewNetworkInfoAdapter(network)

	// Create runtime.Personality from NaraPersonality
	personality := &runtime.Personality{
		Agreeableness: network.local.Me.Status.Personality.Agreeableness,
		Sociability:   network.local.Me.Status.Personality.Sociability,
		Chill:         network.local.Me.Status.Personality.Chill,
	}

	// Create identity adapter
	identityAdapter := NewIdentityAdapter(network)

	// Create logger adapter (bridges runtime logging to LogService)
	loggerAdapter := NewLogServiceAdapter(network.logService)

	// Create the runtime with configuration
	rt := runtime.NewRuntime(runtime.RuntimeConfig{
		Me: &runtime.Nara{
			ID:   network.local.ID,
			Name: network.meName(),
		},
		Keypair:     keypairAdapter,
		Ledger:      ledgerAdapter,
		Transport:   transportAdapter,
		EventBus:    eventBusAdapter,
		GossipQueue: gossipAdapter,
		Identity:    identityAdapter,
		NetworkInfo: networkInfoAdapter,
		Personality: personality,
		Logger:      loggerAdapter,
		Environment: runtime.EnvProduction,
	})

	// Create stash service
	stashService := stash.NewService()

	// Register stash behaviors BEFORE adding service
	// This tells the runtime how to handle stash messages
	stashService.RegisterBehaviors(rt)

	// Add service to runtime
	_ = rt.AddService(stashService)

	// Store runtime and stash service in network
	network.runtime = rt
	network.stashService = stashService

	logrus.Infof("🎯 [TIMING] initRuntime() completed - took %v", time.Since(start))

	return nil
}

// startRuntime starts the runtime and all registered services.
// This should be called during Network.Start() after other initialization.
func (network *Network) startRuntime() error {
	start := time.Now()
	logrus.Debugf("🎯 [TIMING] startRuntime() starting...")

	if network.runtime == nil {
		logrus.Debugf("🎯 [TIMING] startRuntime() skipped (no runtime) - took %v", time.Since(start))
		return nil // No runtime configured
	}

	if err := network.runtime.Start(); err != nil {
		return err
	}

	logrus.Infof("🎯 [TIMING] startRuntime() completed - took %v", time.Since(start))
	return nil
}

// stopRuntime gracefully stops the runtime and all services.
// This should be called during Network shutdown.
func (network *Network) stopRuntime() error {
	if network.runtime == nil {
		return nil // No runtime configured
	}

	if err := network.runtime.Stop(); err != nil {
		return err
	}

	logrus.Info("🎯 Runtime stopped")
	return nil
}

// LedgerAdapter bridges SyncLedger to runtime.LedgerInterface.
type LedgerAdapter struct {
	ledger *SyncLedger
}

func (a *LedgerAdapter) Add(msg *runtime.Message, priority int) error {
	// Convert runtime.Message to SyncEvent for legacy ledger
	// In Chapter 2, we'll properly integrate this
	return nil
}

func (a *LedgerAdapter) HasID(id string) bool {
	// In Chapter 2, we'll properly integrate this
	return false
}

func (a *LedgerAdapter) HasContentKey(contentKey string) bool {
	// In Chapter 2, we'll properly integrate this
	return false
}

// GossipQueueAdapter bridges Network gossip to runtime.GossipQueueInterface.
type GossipQueueAdapter struct {
	network *Network
}

func (a *GossipQueueAdapter) Add(msg *runtime.Message) {
	// In Chapter 2, we'll properly integrate with zine exchange
}

// === Network Info Adapter ===

// NetworkInfoAdapter provides network and peer information to services.
type NetworkInfoAdapter struct {
	network *Network
}

// NewNetworkInfoAdapter creates a network info adapter.
func NewNetworkInfoAdapter(network *Network) *NetworkInfoAdapter {
	return &NetworkInfoAdapter{network: network}
}

// OnlinePeers returns a list of currently online peers.
func (a *NetworkInfoAdapter) OnlinePeers() []*runtime.PeerInfo {
	peers := make([]*runtime.PeerInfo, 0)

	a.network.local.mu.Lock()
	defer a.network.local.mu.Unlock()

	for id, nara := range a.network.Neighbourhood {
		nara.mu.Lock()

		// Check if online via observations
		online := false
		uptime := time.Duration(0)
		if obs, ok := a.network.local.Me.Status.Observations[id]; ok {
			online = obs.isOnline()
			// Calculate uptime from observation
			if obs.LastRestart > 0 {
				// Current session uptime = time since last restart
				uptime = time.Since(time.Unix(obs.LastRestart, 0))
			} else if obs.StartTime > 0 {
				// If no restarts recorded, use time since first seen
				uptime = time.Since(time.Unix(obs.StartTime, 0))
			}
		}

		if online {
			peers = append(peers, &runtime.PeerInfo{
				ID:     nara.ID,
				Name:   nara.Name,
				Uptime: uptime,
			})
		}

		nara.mu.Unlock()
	}

	return peers
}

// MemoryMode returns the current memory mode (low/medium/high).
func (a *NetworkInfoAdapter) MemoryMode() string {
	a.network.local.mu.Lock()
	defer a.network.local.mu.Unlock()
	return a.network.local.Me.Status.MemoryMode
}
