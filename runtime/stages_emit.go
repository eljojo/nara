package runtime

import (
	"errors"
	"fmt"
)

// === ID Stage ===

// IDStage computes the unique envelope ID for a message.
//
// This always runs first in the emit pipeline. The ID is deterministic
// but always unique per message instance (includes nanosecond timestamp).
type IDStage struct{}

func (s *IDStage) Process(msg *Message, ctx *PipelineContext) StageResult {
	if msg.ID == "" {
		msg.ID = ComputeID(msg)
	}
	return Continue(msg)
}

// === Sign Stages ===

// DefaultSignStage signs the message with the runtime's keypair.
type DefaultSignStage struct{}

func (s *DefaultSignStage) Process(msg *Message, ctx *PipelineContext) StageResult {
	msg.Signature = ctx.Keypair.Sign(msg.SignableContent())
	return Continue(msg)
}

// NoSignStage skips signing (for messages where signature is in payload or not needed).
type NoSignStage struct{}

func (s *NoSignStage) Process(msg *Message, ctx *PipelineContext) StageResult {
	return Continue(msg) // No signature on Message itself
}

// === Store Stages ===

// NoStoreStage skips storage (for ephemeral messages).
//
// Stash uses this because stash messages don't need to be stored in the ledger.
type NoStoreStage struct{}

func (s *NoStoreStage) Process(msg *Message, ctx *PipelineContext) StageResult {
	return Continue(msg) // Don't store
}

// DefaultStoreStage stores the message in the ledger with a GC priority.
//
// Not used by stash in Chapter 1, but needed for Chapter 2.
type DefaultStoreStage struct {
	Priority int // 0 = never prune, higher = prune sooner
}

func (s *DefaultStoreStage) Process(msg *Message, ctx *PipelineContext) StageResult {
	if ctx.Ledger == nil {
		return Continue(msg) // No ledger configured
	}

	if err := ctx.Ledger.Add(msg, s.Priority); err != nil {
		return Fail(fmt.Errorf("ledger add: %w", err))
	}
	return Continue(msg)
}

// ContentKeyStoreStage stores with ContentKey-based deduplication.
//
// Only stores if no message with the same ContentKey exists.
// Used for observations where multiple naras may report the same fact.
type ContentKeyStoreStage struct {
	Priority int
}

func (s *ContentKeyStoreStage) Process(msg *Message, ctx *PipelineContext) StageResult {
	if ctx.Ledger == nil {
		return Continue(msg)
	}

	if msg.ContentKey != "" {
		if ctx.Ledger.HasContentKey(msg.ContentKey) {
			return Drop("content_exists") // Same fact already stored
		}
	}

	if err := ctx.Ledger.Add(msg, s.Priority); err != nil {
		return Fail(fmt.Errorf("ledger add: %w", err))
	}
	return Continue(msg)
}

// === Gossip Stages ===

// NoGossipStage skips gossip (message won't be included in zines).
//
// Stash uses this because stash messages are direct mesh only.
type NoGossipStage struct{}

func (s *NoGossipStage) Process(msg *Message, ctx *PipelineContext) StageResult {
	return Continue(msg) // Don't add to gossip queue
}

// GossipStage explicitly queues the message for gossip.
//
// The gossip service will include this message in zines sent to peers.
type GossipStage struct{}

func (s *GossipStage) Process(msg *Message, ctx *PipelineContext) StageResult {
	if ctx.GossipQueue != nil {
		ctx.GossipQueue.Add(msg)
	}
	return Continue(msg)
}

// === Transport Stages ===

// NoTransportStage skips network transport (local-only messages).
//
// Used for service-to-service communication within a single nara.
type NoTransportStage struct{}

func (s *NoTransportStage) Process(msg *Message, ctx *PipelineContext) StageResult {
	return Continue(msg) // No network send
}

// MeshOnlyStage sends the message directly via mesh to a specific target.
//
// Fails if the target is unreachable. Used by stash for direct peer communication.
type MeshOnlyStage struct{}

func (s *MeshOnlyStage) Process(msg *Message, ctx *PipelineContext) StageResult {
	if msg.ToID == "" {
		return Fail(errors.New("mesh-only message has no target (ToID is empty)"))
	}

	if ctx.Transport == nil {
		return Fail(errors.New("no transport configured"))
	}

	if err := ctx.Transport.TrySendDirect(msg.ToID, msg); err != nil {
		return Fail(fmt.Errorf("mesh send to %s: %w", msg.ToID, err))
	}

	return Continue(msg)
}

// MQTTStage broadcasts to a fixed MQTT topic.
type MQTTStage struct {
	Topic string
}

func (s *MQTTStage) Process(msg *Message, ctx *PipelineContext) StageResult {
	if ctx.Transport == nil {
		return Fail(errors.New("no transport configured"))
	}

	if err := ctx.Transport.PublishMQTT(s.Topic, msg.Marshal()); err != nil {
		return Fail(fmt.Errorf("mqtt publish to %s: %w", s.Topic, err))
	}

	return Continue(msg)
}

// MQTTPerNaraStage broadcasts to a per-nara topic.
type MQTTPerNaraStage struct {
	TopicPattern string // e.g., "nara/newspaper/%s"
}

func (s *MQTTPerNaraStage) Process(msg *Message, ctx *PipelineContext) StageResult {
	if ctx.Transport == nil {
		return Fail(errors.New("no transport configured"))
	}

	topic := fmt.Sprintf(s.TopicPattern, msg.From)
	if err := ctx.Transport.PublishMQTT(topic, msg.Marshal()); err != nil {
		return Fail(fmt.Errorf("mqtt publish to %s: %w", topic, err))
	}

	return Continue(msg)
}

// === Notify Stage ===

// NotifyStage always runs last in the emit pipeline.
//
// It notifies local subscribers that a message was emitted.
// This is how services can react to their own emitted messages.
type NotifyStage struct{}

func (s *NotifyStage) Process(msg *Message, ctx *PipelineContext) StageResult {
	if ctx.EventBus != nil {
		ctx.EventBus.Emit(msg)
	}
	return Continue(msg)
}

// === ContentKey Stage ===

// ContentKeyStage computes the semantic identity for dedup.
//
// Only used for messages that need cross-observer deduplication (like observations).
type ContentKeyStage struct {
	KeyFunc func(payload any) string
}

func (s *ContentKeyStage) Process(msg *Message, ctx *PipelineContext) StageResult {
	if msg.ContentKey == "" && s.KeyFunc != nil {
		msg.ContentKey = s.KeyFunc(msg.Payload)
	}
	return Continue(msg)
}

// NoContentKeyStage is a no-op (most messages don't need content keys).
type NoContentKeyStage struct{}

func (s *NoContentKeyStage) Process(msg *Message, ctx *PipelineContext) StageResult {
	return Continue(msg)
}
