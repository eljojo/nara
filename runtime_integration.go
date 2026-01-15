package nara

import (
	"github.com/eljojo/nara/runtime"
	"github.com/eljojo/nara/services/stash"

	"github.com/sirupsen/logrus"
)

// initRuntime creates and initializes the Runtime with all services and adapters.
// This wires the new runtime-based architecture into the existing Network.
func (network *Network) initRuntime() error {
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

	// Create runtime.Personality from NaraPersonality
	personality := &runtime.Personality{
		Agreeableness: network.local.Me.Status.Personality.Agreeableness,
		Sociability:   network.local.Me.Status.Personality.Sociability,
		Chill:         network.local.Me.Status.Personality.Chill,
	}

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
		Personality: personality,
		Environment: runtime.EnvProduction,
	})

	// Create stash service
	stashService := stash.NewService()

	// Register stash behaviors BEFORE adding service
	// This tells the runtime how to handle stash messages
	stashService.RegisterBehaviors(rt)

	// Add service to runtime
	_ = rt.AddService(stashService)

	// Store runtime in network
	network.runtime = rt

	logrus.Info("🎯 Runtime initialized with stash service")

	return nil
}

// startRuntime starts the runtime and all registered services.
// This should be called during Network.Start() after other initialization.
func (network *Network) startRuntime() error {
	if network.runtime == nil {
		return nil // No runtime configured
	}

	if err := network.runtime.Start(); err != nil {
		return err
	}

	logrus.Info("🎯 Runtime started")
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
