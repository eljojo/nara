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
// Behaviors are registered locally with the runtime for test isolation.
func (s *Service) RegisterBehaviors(rt runtime.RuntimeInterface) {
	// stash-refresh: Ephemeral MQTT broadcast to trigger recovery
	rt.RegisterBehavior(
		runtime.Ephemeral("stash-refresh", "Trigger stash recovery from confidants", "nara/plaza/stash_refresh").
			WithPayload(runtime.PayloadTypeOf[messages.StashRefreshPayload]()).
			WithHandler(1, s.handleRefreshV1),
	)

	// stash:store: Direct mesh request to store encrypted data
	rt.RegisterBehavior(
		runtime.MeshRequest("stash:store", "Store encrypted stash with confidant").
			WithPayload(runtime.PayloadTypeOf[messages.StashStorePayload]()).
			WithHandler(1, s.handleStoreV1),
	)

	// stash:ack: Response to stash:store
	rt.RegisterBehavior(
		runtime.MeshRequest("stash:ack", "Acknowledge stash storage").
			WithPayload(runtime.PayloadTypeOf[messages.StashStoreAck]()).
			WithHandler(1, s.handleStoreAckV1),
	)

	// stash:request: Request stored data from confidant
	rt.RegisterBehavior(
		runtime.MeshRequest("stash:request", "Request stored stash").
			WithPayload(runtime.PayloadTypeOf[messages.StashRequestPayload]()).
			WithHandler(1, s.handleRequestV1),
	)

	// stash:response: Return stored data to owner
	rt.RegisterBehavior(
		runtime.MeshRequest("stash:response", "Return stored stash").
			WithPayload(runtime.PayloadTypeOf[messages.StashResponsePayload]()).
			WithHandler(1, s.handleResponseV1),
	)
}
