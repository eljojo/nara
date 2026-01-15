package runtime

import "time"

// === Sign Helpers ===

// DefaultSign returns the default signing stage.
func DefaultSign() Stage {
	return &DefaultSignStage{}
}

// NoSign returns a no-op signing stage.
func NoSign() Stage {
	return &NoSignStage{}
}

// === Store Helpers ===

// DefaultStore returns a store stage with the given GC priority.
//
// Priority values:
//   - 0: Never prune (checkpoints, critical observations)
//   - 1: Important (hey-there, chau)
//   - 2: Normal (social events)
//   - 3: Low priority (seen events)
//   - 4: Expendable (pings)
func DefaultStore(priority int) Stage {
	return &DefaultStoreStage{Priority: priority}
}

// NoStore returns a no-op store stage (ephemeral messages).
func NoStore() Stage {
	return &NoStoreStage{}
}

// ContentKeyStore returns a store stage with ContentKey-based deduplication.
func ContentKeyStore(priority int) Stage {
	return &ContentKeyStoreStage{Priority: priority}
}

// === Gossip Helpers ===

// Gossip returns a stage that adds the message to the gossip queue.
func Gossip() Stage {
	return &GossipStage{}
}

// NoGossip returns a no-op gossip stage.
func NoGossip() Stage {
	return &NoGossipStage{}
}

// === Transport Helpers ===

// NoTransport returns a no-op transport stage (local-only messages).
func NoTransport() Stage {
	return &NoTransportStage{}
}

// MeshOnly returns a stage that sends directly via mesh to ToID.
func MeshOnly() Stage {
	return &MeshOnlyStage{}
}

// MQTT returns a stage that broadcasts to a fixed MQTT topic.
func MQTT(topic string) Stage {
	return &MQTTStage{Topic: topic}
}

// MQTTPerNara returns a stage that broadcasts to a per-nara topic.
func MQTTPerNara(pattern string) Stage {
	return &MQTTPerNaraStage{TopicPattern: pattern}
}

// === Verify Helpers ===

// DefaultVerify returns the default signature verification stage.
func DefaultVerify() Stage {
	return &DefaultVerifyStage{}
}

// SelfAttesting returns a verification stage that extracts the public key from the payload.
func SelfAttesting(extractKey func(any) []byte) Stage {
	return &SelfAttestingVerifyStage{ExtractKey: extractKey}
}

// CustomVerify returns a verification stage with a custom verification function.
func CustomVerify(verifyFunc func(*Message, *PipelineContext) StageResult) Stage {
	return &CustomVerifyStage{VerifyFunc: verifyFunc}
}

// NoVerify returns a no-op verification stage.
func NoVerify() Stage {
	return &NoVerifyStage{}
}

// === Dedupe Helpers ===

// IDDedupe returns a stage that deduplicates by message ID.
func IDDedupe() Stage {
	return &IDDedupeStage{}
}

// ContentKeyDedupe returns a stage that deduplicates by ContentKey.
func ContentKeyDedupe() Stage {
	return &ContentKeyDedupeStage{}
}

// === RateLimit Helper ===

// RateLimit returns a rate limiting stage.
func RateLimit(window time.Duration, max int, keyFunc func(*Message) string) Stage {
	return &RateLimitStage{
		Window:  window,
		Max:     max,
		KeyFunc: keyFunc,
	}
}

// === Filter Helpers ===

// Critical returns a filter that never drops messages (importance level 3).
func Critical() Stage {
	return &ImportanceFilterStage{Importance: 3}
}

// Normal returns a filter for normal importance messages (level 2).
func Normal() Stage {
	return &ImportanceFilterStage{Importance: 2}
}

// Casual returns a filter for casual importance messages (level 1) with a custom filter function.
func Casual(filterFunc func(*Message, *Personality) bool) Stage {
	return &ImportanceFilterStage{
		Importance:   1,
		CasualFilter: filterFunc,
	}
}

// NoFilter returns a no-op filter stage.
func NoFilter() Stage {
	return &NoFilterStage{}
}

// === ContentKey Helper ===

// ContentKey returns a stage that computes semantic identity for dedup.
func ContentKey(keyFunc func(any) string) Stage {
	return &ContentKeyStage{KeyFunc: keyFunc}
}

// NoContentKey returns a no-op content key stage.
func NoContentKey() Stage {
	return &NoContentKeyStage{}
}
