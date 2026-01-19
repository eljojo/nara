package runtime

import (
	"errors"
	"fmt"

	"github.com/sirupsen/logrus"
)

// === ID Stage ===

// IDStage computes the unique envelope ID for a message.
//
// This always runs first in the emit pipeline. The ID is deterministic
// but always unique per message instance (includes nanosecond timestamp).
type IDStage struct{}

func (s *IDStage) Process(msg *Message, ctx *PipelineContext) StageResult {
	if msg.ID == "" {
		id, err := ComputeID(msg)
		if err != nil {
			return Fail(fmt.Errorf("compute ID: %w", err))
		}
		msg.ID = id
	}
	return Continue(msg)
}

// === Sign Stages ===

// DefaultSignStage signs the message with the runtime's keypair.
type DefaultSignStage struct{}

func (s *DefaultSignStage) Process(msg *Message, ctx *PipelineContext) StageResult {
	content, err := msg.SignableContent()
	if err != nil {
		return Fail(fmt.Errorf("get signable content: %w", err))
	}
	msg.Signature = ctx.Keypair.Sign(content)
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
// Needed for Chapter 2.
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
// Piggybacked responses from the HTTP response are routed through the receive
// pipeline for verification and handler invocation.
type MeshOnlyStage struct{}

func (s *MeshOnlyStage) Process(msg *Message, ctx *PipelineContext) StageResult {
	if msg.ToID == "" {
		return Fail(errors.New("mesh-only message has no target (ToID is empty)"))
	}

	if ctx.Transport == nil {
		return Fail(errors.New("no transport configured"))
	}

	responses, err := ctx.Transport.TrySendDirect(msg.ToID, msg)
	if err != nil {
		return Fail(fmt.Errorf("mesh send to %s: %w", msg.ToID, err))
	}

	// Route piggybacked responses through full receive pipeline.
	// This ensures: (1) signature verification, (2) CallRegistry/handler routing.
	//
	// CanPiggyback: true - if the handler returns more responses, they can also
	// be piggybacked since we're still in an HTTP context. However, we emit them
	// here since we don't have a way to return them to the original HTTP response.
	for _, resp := range responses {
		respBytes, err := resp.Marshal()
		if err != nil {
			logrus.Warnf("[runtime] failed to marshal piggybacked response: %v", err)
			continue
		}
		// Receive handles: verify -> CallRegistry OR handler
		// CanPiggyback: false here because we can't return to the original HTTP response
		returnedMsgs, err := ctx.Runtime.Receive(respBytes, ReceiveOptions{CanPiggyback: false})
		if err != nil {
			logrus.Warnf("[runtime] failed to process piggybacked response: %v", err)
			continue
		}
		// Any messages from handlers were already emitted by Receive since CanPiggyback=false
		// But if any were somehow returned, emit them
		for _, outMsg := range returnedMsgs {
			if emitErr := ctx.Runtime.Emit(outMsg); emitErr != nil {
				logrus.Warnf("[runtime] failed to emit message from piggybacked handler: %v", emitErr)
			}
		}
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

	data, err := msg.Marshal()
	if err != nil {
		return Fail(fmt.Errorf("marshal message: %w", err))
	}

	if err := ctx.Transport.PublishMQTT(s.Topic, data); err != nil {
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

	data, err := msg.Marshal()
	if err != nil {
		return Fail(fmt.Errorf("marshal message: %w", err))
	}

	topic := fmt.Sprintf(s.TopicPattern, msg.From)
	if err := ctx.Transport.PublishMQTT(topic, data); err != nil {
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
