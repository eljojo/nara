package stash

import (
	"github.com/eljojo/nara/messages"
	"github.com/eljojo/nara/runtime"
)

// RegisterBehaviors registers all stash message behaviors with the runtime.
//
// This is called during service initialization to declare how each
// stash message kind should be handled.
//
// If the runtime implements BehaviorRegistry (like MockRuntime), behaviors
// are registered locally for test isolation. Otherwise, uses the global registry.
func (s *Service) RegisterBehaviors(rt runtime.RuntimeInterface) {
	// Helper to register with local or global registry
	register := func(b *runtime.Behavior) {
		if reg, ok := rt.(runtime.BehaviorRegistry); ok {
			// Register locally (test isolation)
			reg.RegisterBehavior(b)
		} else {
			// Register globally (production)
			_ = runtime.Register(b)
		}
	}

	// stash-refresh: Ephemeral MQTT broadcast to trigger recovery
	register(
		runtime.Ephemeral("stash-refresh", "Trigger stash recovery from confidants", "nara/plaza/stash_refresh").
			WithPayload(runtime.PayloadTypeOf[messages.StashRefreshPayload]()).
			WithHandler(1, s.handleRefreshV1),
	)

	// stash:store: Direct mesh request to store encrypted data
	register(
		runtime.MeshRequest("stash:store", "Store encrypted stash with confidant").
			WithPayload(runtime.PayloadTypeOf[messages.StashStorePayload]()).
			WithHandler(1, s.handleStoreV1),
	)

	// stash:ack: Response to stash:store
	register(
		runtime.MeshRequest("stash:ack", "Acknowledge stash storage").
			WithPayload(runtime.PayloadTypeOf[messages.StashStoreAck]()).
			WithHandler(1, s.handleStoreAckV1),
	)

	// stash:request: Request stored data from confidant
	register(
		runtime.MeshRequest("stash:request", "Request stored stash").
			WithPayload(runtime.PayloadTypeOf[messages.StashRequestPayload]()).
			WithHandler(1, s.handleRequestV1),
	)

	// stash:response: Return stored data to owner
	register(
		runtime.MeshRequest("stash:response", "Return stored stash").
			WithPayload(runtime.PayloadTypeOf[messages.StashResponsePayload]()).
			WithHandler(1, s.handleResponseV1),
	)
}
