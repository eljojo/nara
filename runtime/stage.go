package runtime

// StageResult represents the outcome of a pipeline stage.
//
// Every stage returns an explicit result - no silent failures.
type StageResult struct {
	Message *Message // The message to continue with (nil = dropped)
	Error   error    // Set if stage failed (transport error, validation failure, etc.)
	Reason  string   // Human-readable reason for drop ("rate_limited", "duplicate", "invalid_signature")
}

// Convenience constructors for stage results.

// Continue indicates the message should proceed to the next stage.
func Continue(msg *Message) StageResult {
	return StageResult{Message: msg}
}

// Drop indicates the message was intentionally filtered/rejected.
// Use this for deduplication, rate limiting, personality filtering, etc.
func Drop(reason string) StageResult {
	return StageResult{Reason: reason}
}

// Fail indicates something went wrong (transport failure, crypto error, etc.).
func Fail(err error) StageResult {
	return StageResult{Error: err}
}

// Helper methods for checking result types.

// IsContinue returns true if the result indicates continuation.
func (r StageResult) IsContinue() bool {
	return r.Message != nil && r.Error == nil
}

// IsDrop returns true if the result indicates an intentional drop.
func (r StageResult) IsDrop() bool {
	return r.Message == nil && r.Error == nil
}

// IsError returns true if the result indicates an error.
func (r StageResult) IsError() bool {
	return r.Error != nil
}

// Stage processes a message and returns an explicit result.
//
// Stages are the building blocks of message pipelines. They can:
//   - Transform the message (signing, ID assignment)
//   - Store it (ledger, gossip queue)
//   - Filter it (deduplication, rate limiting, personality)
//   - Transport it (MQTT, mesh)
//   - Drop it with a reason
//   - Fail with an error
type Stage interface {
	Process(msg *Message, ctx *PipelineContext) StageResult
}

// PipelineContext carries runtime dependencies that stages need.
//
// Stages receive this instead of importing the full runtime,
// which helps avoid circular dependencies and makes testing easier.
type PipelineContext struct {
	Runtime     RuntimeInterface     // For accessing runtime methods
	Ledger      LedgerInterface      // For storage stages
	Transport   TransportInterface   // For transport stages
	GossipQueue GossipQueueInterface // For gossip stages
	Keypair     KeypairInterface     // For signing stages
	Personality *Personality         // For filtering stages
	EventBus    EventBusInterface    // For notification stages
}

// Pipeline chains multiple stages together.
//
// Messages flow through stages sequentially. If any stage returns
// an error or drop, the pipeline stops and returns that result.
type Pipeline []Stage

// Run executes the pipeline on a message.
//
// Each stage processes the message in sequence. The pipeline stops
// early if any stage drops the message or encounters an error.
func (p Pipeline) Run(msg *Message, ctx *PipelineContext) StageResult {
	for _, stage := range p {
		result := stage.Process(msg, ctx)

		// Error - propagate up immediately
		if result.Error != nil {
			return result
		}

		// Dropped - stop processing
		if result.Message == nil {
			return result
		}

		// Continue with (possibly modified) message
		msg = result.Message
	}

	return Continue(msg)
}
