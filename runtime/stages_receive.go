package runtime

import (
	"time"
)

// === Verify Stages ===

// DefaultVerifyStage verifies the message signature against a known public key.
//
// Looks up the public key by FromID and verifies the signature.
type DefaultVerifyStage struct{}

func (s *DefaultVerifyStage) Process(msg *Message, ctx *PipelineContext) StageResult {
	// Look up public key for sender
	pubKey := ctx.Runtime.LookupPublicKey(msg.FromID)
	if pubKey == nil {
		return Drop("unknown_sender")
	}

	// Verify signature
	if !msg.VerifySignature(pubKey) {
		return Drop("invalid_signature")
	}

	return Continue(msg)
}

// SelfAttestingVerifyStage uses public key embedded in the payload.
//
// Used for identity announcements where the public key is in the payload itself.
type SelfAttestingVerifyStage struct {
	ExtractKey func(payload any) []byte
}

func (s *SelfAttestingVerifyStage) Process(msg *Message, ctx *PipelineContext) StageResult {
	pubKey := s.ExtractKey(msg.Payload)
	if pubKey == nil {
		return Drop("no_public_key_in_payload")
	}

	if !msg.VerifySignature(pubKey) {
		return Drop("invalid_signature")
	}

	// Register the public key for future verification
	ctx.Runtime.RegisterPublicKey(msg.FromID, pubKey)

	return Continue(msg)
}

// CustomVerifyStage uses a custom verification function.
//
// Used for complex verification like checkpoint multi-sig.
type CustomVerifyStage struct {
	VerifyFunc func(msg *Message, ctx *PipelineContext) StageResult
}

func (s *CustomVerifyStage) Process(msg *Message, ctx *PipelineContext) StageResult {
	return s.VerifyFunc(msg, ctx)
}

// NoVerifyStage skips verification (for unverified protocol messages or local messages).
type NoVerifyStage struct{}

func (s *NoVerifyStage) Process(msg *Message, ctx *PipelineContext) StageResult {
	return Continue(msg)
}

// === Dedupe Stages ===

// IDDedupeStage rejects messages with duplicate ID (exact same message).
//
// This prevents processing the same message twice if received via multiple paths.
type IDDedupeStage struct{}

func (s *IDDedupeStage) Process(msg *Message, ctx *PipelineContext) StageResult {
	if ctx.Ledger != nil && ctx.Ledger.HasID(msg.ID) {
		return Drop("duplicate_id")
	}
	return Continue(msg)
}

// ContentKeyDedupeStage rejects messages with duplicate ContentKey (same fact).
//
// Used for observations where multiple naras may report the same fact.
// Different from IDDedupe: same ContentKey, different IDs.
type ContentKeyDedupeStage struct{}

func (s *ContentKeyDedupeStage) Process(msg *Message, ctx *PipelineContext) StageResult {
	if msg.ContentKey != "" && ctx.Ledger != nil {
		if ctx.Ledger.HasContentKey(msg.ContentKey) {
			return Drop("duplicate_content")
		}
	}
	return Continue(msg)
}

// === RateLimit Stage ===

// RateLimitStage throttles incoming messages based on a key function.
type RateLimitStage struct {
	Window  time.Duration
	Max     int
	KeyFunc func(msg *Message) string
}

func (s *RateLimitStage) Process(msg *Message, ctx *PipelineContext) StageResult {
	// Rate limiting implementation would go here
	// For now, just continue (will be implemented in Chapter 2)
	return Continue(msg)
}

// === Filter Stages ===

// ImportanceFilterStage filters messages based on importance level and personality.
//
// Importance levels:
//   - 3 (Critical): Never filtered
//   - 2 (Normal): Filtered only if very chill
//   - 1 (Casual): Uses custom filter function
type ImportanceFilterStage struct {
	Importance   int // 1=casual, 2=normal, 3=critical
	CasualFilter func(msg *Message, personality *Personality) bool
}

func (s *ImportanceFilterStage) Process(msg *Message, ctx *PipelineContext) StageResult {
	switch s.Importance {
	case 3: // Critical - never filter
		return Continue(msg)

	case 2: // Normal - filter only if very chill
		if ctx.Personality != nil && ctx.Personality.Chill > 85 {
			return Drop("filtered_by_chill")
		}
		return Continue(msg)

	case 1: // Casual - use custom filter
		if s.CasualFilter != nil && ctx.Personality != nil {
			if !s.CasualFilter(msg, ctx.Personality) {
				return Drop("filtered_by_personality")
			}
		}
		return Continue(msg)

	default:
		return Continue(msg)
	}
}

// NoFilterStage is a no-op filter (all messages pass).
type NoFilterStage struct{}

func (s *NoFilterStage) Process(msg *Message, ctx *PipelineContext) StageResult {
	return Continue(msg)
}
