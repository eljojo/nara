package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/eljojo/nara/types"
	"github.com/sirupsen/logrus"
)

// Runtime is the core execution environment for services.
//
// It manages message pipelines, service lifecycle, and provides
// primitives like logging and transport.
type Runtime struct {
	// Identity
	me      *Nara
	keypair KeypairInterface

	// Storage
	ledger LedgerInterface // May be nil for stash-only

	// Transport
	transport   TransportInterface
	gossipQueue GossipQueueInterface

	// Event bus for local notifications
	eventBus EventBusInterface

	// Personality for filtering
	personality *Personality

	// Network information (for automatic confidant selection)
	networkInfo NetworkInfoInterface

	// Identity lookups
	identity IdentityInterface

	// Services
	services []Service
	handlers map[string][]func(*Message)

	// Logging
	logger LoggerInterface

	// Environment
	env Environment

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc

	// Call registry (Chapter 3)
	calls *CallRegistry

	// Local behavior registry (for per-runtime behavior isolation)
	localBehaviors map[string]*Behavior
}

// RuntimeConfig is passed to NewRuntime.
type RuntimeConfig struct {
	Me          *Nara
	Keypair     KeypairInterface
	Ledger      LedgerInterface      // Optional (nil for stash-only)
	Transport   TransportInterface   // Required
	EventBus    EventBusInterface    // Optional
	GossipQueue GossipQueueInterface // Optional (nil for stash-only)
	Identity    IdentityInterface    // Optional (for public key lookups)
	Personality *Personality         // Optional
	NetworkInfo NetworkInfoInterface // Optional (for peer/memory info)
	Logger      LoggerInterface      // Optional (defaults to simple logger)
	Environment Environment          // Default: EnvProduction
}

// ReceiveOptions controls how Receive() handles response messages from handlers.
//
// # Background: The Piggybacking Optimization
//
// When handlers process incoming messages, they often return response messages
// (e.g., stash:store handler returns an ack). These responses need to be delivered
// back to the sender somehow.
//
// For HTTP-based transport (mesh), we can "piggyback" responses in the HTTP response
// body, eliminating a separate round-trip:
//
//	Without piggybacking (2 HTTP calls):
//	  Alice → Bob: POST /mesh/message [store]
//	  Bob → Alice: POST /mesh/message [ack]   ← separate HTTP call
//
//	With piggybacking (1 HTTP call):
//	  Alice → Bob: POST /mesh/message [store]
//	  Bob → Alice: HTTP 200 + [ack] in body   ← piggybacked!
//
// For MQTT (broadcast), there's no response to piggyback on - responses must be
// emitted via normal transport.
//
// # How It Works
//
// When Receive() processes a message and the handler returns responses:
//
//   - If CanPiggyback=true AND response uses MeshOnly transport:
//     → Prepare message (ID + Sign) and return it for piggybacking
//     → Caller includes it in HTTP response body
//
//   - If CanPiggyback=false OR response uses non-MeshOnly transport:
//     → Emit message via normal transport (Emit() → behavior's transport stage)
//     → Message is delivered separately
//
// # When to Use Each Mode
//
//	CanPiggyback: true   - HTTP mesh handlers (httpMeshMessageHandler)
//	                     - Processing piggybacked responses (MeshOnlyStage)
//
//	CanPiggyback: false  - MQTT handlers
//	                     - Any context without a response channel
//
// # Example
//
//	// HTTP handler - can piggyback MeshOnly responses
//	func httpHandler(w http.ResponseWriter, r *http.Request) {
//	    responses, _ := runtime.Receive(body, ReceiveOptions{CanPiggyback: true})
//	    json.Encode(w, responses)  // Include in HTTP response
//	}
//
//	// MQTT handler - cannot piggyback, responses emitted normally
//	func mqttHandler(payload []byte) {
//	    runtime.Receive(payload, ReceiveOptions{CanPiggyback: false})
//	    // Responses already emitted via their declared transport
//	}
type ReceiveOptions struct {
	// CanPiggyback indicates whether MeshOnly response messages can be
	// piggybacked in the caller's response (e.g., HTTP response body).
	//
	// When true: MeshOnly responses are prepared (ID + Sign) and returned
	// for the caller to include in their response. They are NOT emitted.
	//
	// When false: All responses are emitted via normal transport, regardless
	// of their transport type. Nothing is returned for piggybacking.
	CanPiggyback bool
}

// NewRuntime creates a new runtime with the given configuration.
func NewRuntime(cfg RuntimeConfig) *Runtime {
	ctx, cancel := context.WithCancel(context.Background())

	env := cfg.Environment
	if env == 0 {
		env = EnvProduction
	}

	// Use provided logger or default to simple logger
	logger := cfg.Logger
	if logger == nil {
		logger = &Logger{}
	}

	rt := &Runtime{
		me:             cfg.Me,
		keypair:        cfg.Keypair,
		ledger:         cfg.Ledger,
		transport:      cfg.Transport,
		eventBus:       cfg.EventBus,
		gossipQueue:    cfg.GossipQueue,
		identity:       cfg.Identity,
		personality:    cfg.Personality,
		networkInfo:    cfg.NetworkInfo,
		logger:         logger,
		handlers:       make(map[string][]func(*Message)),
		env:            env,
		ctx:            ctx,
		cancel:         cancel,
		calls:          NewCallRegistry(),
		localBehaviors: make(map[string]*Behavior),
	}

	return rt
}

// === RuntimeInterface implementation ===

// Me returns the local nara.
func (rt *Runtime) Me() *Nara {
	return rt.me
}

// MeID returns the local nara's ID.
func (rt *Runtime) MeID() types.NaraID {
	if rt.me != nil {
		return rt.me.ID
	}
	return ""
}

// Log returns a logger scoped to the given service.
func (rt *Runtime) Log(service string) *ServiceLog {
	return &ServiceLog{
		name:   service,
		logger: rt.logger,
	}
}

// Env returns the runtime environment.
func (rt *Runtime) Env() Environment {
	return rt.env
}

// OnlinePeers returns a list of currently online peers.
func (rt *Runtime) OnlinePeers() []*PeerInfo {
	if rt.networkInfo == nil {
		return []*PeerInfo{}
	}
	return rt.networkInfo.OnlinePeers()
}

// MemoryMode returns the current memory mode (low/medium/high).
func (rt *Runtime) MemoryMode() string {
	if rt.networkInfo == nil {
		return "low" // Default
	}
	return rt.networkInfo.MemoryMode()
}

// Keypair returns the keypair interface.
// Runtime guarantees this is always non-nil.
func (rt *Runtime) Keypair() KeypairInterface {
	return rt.keypair
}

// Identity returns the identity interface.
// Runtime guarantees this is always non-nil.
func (rt *Runtime) Identity() IdentityInterface {
	return rt.identity
}

// === Message handling ===

// Emit sends a message through the emit pipeline.
func (rt *Runtime) Emit(msg *Message) error {
	// Set timestamp if not set
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}

	// Set FromID if not set
	if msg.FromID == "" {
		msg.FromID = rt.MeID()
	}
	if msg.From == "" && rt.me != nil {
		msg.From = rt.me.Name
	}

	// Look up behavior (use local registry for this runtime)
	behavior := rt.LookupBehavior(msg.Kind)
	if behavior == nil {
		return fmt.Errorf("unknown message kind: %s", msg.Kind)
	}

	// Set version to current if not specified
	if msg.Version == 0 {
		msg.Version = behavior.CurrentVersion
		if msg.Version == 0 {
			msg.Version = 1
		}
	}

	// Build and run emit pipeline
	pipeline := rt.buildEmitPipeline(behavior)
	ctx := rt.newPipelineContext()

	result := pipeline.Run(msg, ctx)

	// Handle result
	if result.Error != nil {
		rt.applyErrorStrategy(msg, "emit", result.Error, behavior.Emit.OnError)
		return result.Error
	}
	if result.Message == nil {
		logrus.Debugf("[runtime] message %s dropped in emit: %s", msg.Kind, result.Reason)
	}

	return nil
}

// Call emits a message and waits for a reply.
//
// This is the request/response primitive. The message is emitted, and the
// runtime tracks it so that when a response arrives (via InReplyTo), the
// caller gets notified.
func (rt *Runtime) Call(msg *Message, timeout time.Duration) <-chan CallResult {
	ch := make(chan CallResult, 1)

	// Ensure timestamp is set (required for ID computation)
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}

	// Ensure message has an ID before registering
	if msg.ID == "" {
		id, err := ComputeID(msg)
		if err != nil {
			ch <- CallResult{Error: fmt.Errorf("compute message ID: %w", err)}
			return ch
		}
		msg.ID = id
	}

	// Register the pending call
	rt.calls.Register(msg.ID, ch, timeout)

	// Emit the message
	if err := rt.Emit(msg); err != nil {
		// Failed to emit - cancel pending and return error
		rt.calls.Cancel(msg.ID)
		ch <- CallResult{Error: err}
		return ch
	}

	return ch
}

// Receive processes an incoming message through the receive pipeline.
//
// Response messages from handlers are either piggybacked (returned) or emitted
// depending on opts.CanPiggyback and the message's transport type.
// See ReceiveOptions documentation for details.
//
// Returns piggybacked messages (only when opts.CanPiggyback=true and message uses MeshOnly).
func (rt *Runtime) Receive(raw []byte, opts ReceiveOptions) ([]*Message, error) {
	// Deserialize using behavior's PayloadType
	msg, err := rt.deserialize(raw)
	if err != nil {
		return nil, fmt.Errorf("deserialize: %w", err)
	}

	// Check if this is a response to a pending Call (Chapter 3)
	if msg.InReplyTo != "" {
		if rt.calls.Resolve(msg.InReplyTo, msg) {
			return nil, nil // Handled as call response
		}
		// Not a pending call - fall through to normal handling
	}

	behavior := rt.LookupBehavior(msg.Kind)
	if behavior == nil {
		return nil, fmt.Errorf("unknown message kind: %s", msg.Kind)
	}

	logrus.Infof("[runtime] processing %s (from: %s, InReplyTo: %s)", msg.Kind, msg.FromID, msg.InReplyTo)

	// Build and run receive pipeline
	pipeline := rt.buildReceivePipeline(behavior)
	ctx := rt.newPipelineContext()

	result := pipeline.Run(msg, ctx)

	// Handle result
	if result.Error != nil {
		rt.applyErrorStrategy(msg, "receive", result.Error, behavior.Receive.OnError)
		return nil, result.Error
	}
	if result.Message == nil {
		logrus.Debugf("[runtime] message %s dropped in receive: %s", msg.Kind, result.Reason)
		return nil, nil
	}

	logrus.Infof("[runtime] invoking handler for %s", msg.Kind)
	// Invoke version-specific handler - returns response messages
	responseMessages := rt.invokeHandler(msg, behavior)

	// Process each response message: piggyback or emit
	var piggybacked []*Message
	for _, resp := range responseMessages {
		// Look up the response message's behavior to check its transport
		respBehavior := rt.LookupBehavior(resp.Kind)

		// Can we piggyback this message?
		// Conditions: caller supports piggybacking AND message uses MeshOnly transport
		canPiggybackThis := opts.CanPiggyback && rt.isMeshOnlyTransport(respBehavior)

		if canPiggybackThis {
			// Prepare for piggybacking (ID + Sign, no transport)
			if p, err := rt.prepareForPiggyback(resp); err == nil {
				piggybacked = append(piggybacked, p)
				logrus.Debugf("[runtime] piggybacking %s response", resp.Kind)
			} else {
				logrus.Warnf("[runtime] failed to prepare piggyback response: %v", err)
			}
		} else {
			// Emit via normal transport
			if err := rt.Emit(resp); err != nil {
				logrus.Warnf("[runtime] failed to emit response %s: %v", resp.Kind, err)
			} else {
				logrus.Debugf("[runtime] emitted %s response via normal transport", resp.Kind)
			}
		}
	}

	return piggybacked, nil
}

// isMeshOnlyTransport checks if a behavior uses MeshOnly transport.
// Only MeshOnly messages can be piggybacked (they're point-to-point HTTP).
func (rt *Runtime) isMeshOnlyTransport(b *Behavior) bool {
	if b == nil || b.Emit.Transport == nil {
		return false
	}
	_, ok := b.Emit.Transport.(*MeshOnlyStage)
	return ok
}

// prepareForPiggyback runs ID + Sign stages but skips transport.
// This prepares a message to be included in HTTP response body.
func (rt *Runtime) prepareForPiggyback(msg *Message) (*Message, error) {
	// Set timestamp if not set
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}

	// Set FromID if not set
	if msg.FromID == "" {
		msg.FromID = rt.MeID()
	}
	if msg.From == "" && rt.me != nil {
		msg.From = rt.me.Name
	}

	behavior := rt.LookupBehavior(msg.Kind)
	if behavior == nil {
		return nil, fmt.Errorf("unknown kind: %s", msg.Kind)
	}

	// Set version to current if not specified
	if msg.Version == 0 {
		msg.Version = behavior.CurrentVersion
		if msg.Version == 0 {
			msg.Version = 1
		}
	}

	// Build minimal pipeline: ID -> Sign (no store, no gossip, no transport)
	stages := []Stage{&IDStage{}, &DefaultSignStage{}}
	pipeline := Pipeline(stages)
	ctx := rt.newPipelineContext()

	result := pipeline.Run(msg, ctx)
	if result.Error != nil {
		return nil, result.Error
	}
	return result.Message, nil
}

// === Pipeline building ===

func (rt *Runtime) buildEmitPipeline(b *Behavior) Pipeline {
	stages := []Stage{}

	// ID stage (always first - computes unique envelope ID)
	stages = append(stages, &IDStage{})

	// ContentKey stage (if behavior defines ContentKey function)
	if b.ContentKey != nil {
		stages = append(stages, &ContentKeyStage{KeyFunc: b.ContentKey})
	}

	// Sign stage
	if b.Emit.Sign != nil {
		stages = append(stages, b.Emit.Sign)
	} else {
		stages = append(stages, DefaultSign())
	}

	// Store stage
	if b.Emit.Store != nil {
		stages = append(stages, b.Emit.Store)
	} else {
		stages = append(stages, NoStore())
	}

	// Gossip stage (explicit, independent of store)
	if b.Emit.Gossip != nil {
		stages = append(stages, b.Emit.Gossip)
	} else {
		stages = append(stages, NoGossip())
	}

	// Transport stage
	if b.Emit.Transport != nil {
		stages = append(stages, b.Emit.Transport)
	}

	// Notify stage (always last)
	stages = append(stages, &NotifyStage{})

	return Pipeline(stages)
}

func (rt *Runtime) buildReceivePipeline(b *Behavior) Pipeline {
	stages := []Stage{}

	// Verify stage
	if b.Receive.Verify != nil {
		stages = append(stages, b.Receive.Verify)
	} else {
		stages = append(stages, DefaultVerify())
	}

	// Dedupe stage
	if b.Receive.Dedupe != nil {
		stages = append(stages, b.Receive.Dedupe)
	} else {
		stages = append(stages, IDDedupe())
	}

	// Rate limit stage (optional)
	if b.Receive.RateLimit != nil {
		stages = append(stages, b.Receive.RateLimit)
	}

	// Filter stage (optional)
	if b.Receive.Filter != nil {
		stages = append(stages, b.Receive.Filter)
	}

	// Store stage (from receive config)
	if b.Receive.Store != nil {
		stages = append(stages, b.Receive.Store)
	}

	// Gossip stage (spread to others if configured)
	if b.Emit.Gossip != nil {
		stages = append(stages, b.Emit.Gossip)
	}

	// Notify stage (always last)
	stages = append(stages, &NotifyStage{})

	return Pipeline(stages)
}

func (rt *Runtime) newPipelineContext() *PipelineContext {
	return &PipelineContext{
		Runtime:     rt,
		Ledger:      rt.ledger,
		Transport:   rt.transport,
		GossipQueue: rt.gossipQueue,
		Keypair:     rt.keypair,
		Identity:    rt.identity,
		Personality: rt.personality,
		EventBus:    rt.eventBus,
	}
}

// === Deserialization ===

func (rt *Runtime) deserialize(raw []byte) (*Message, error) {
	// First, peek at kind and version
	var envelope struct {
		Kind    string `json:"kind"`
		Version int    `json:"version"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}

	behavior := rt.LookupBehavior(envelope.Kind)
	if behavior == nil {
		return nil, fmt.Errorf("unknown kind: %s", envelope.Kind)
	}

	// Default to v1 if not specified (backwards compat)
	version := envelope.Version
	if version == 0 {
		version = 1
	}

	// Check version bounds
	if version < behavior.MinVersion || version > behavior.CurrentVersion {
		return nil, fmt.Errorf("unsupported version %d for %s (min=%d, max=%d)",
			version, envelope.Kind, behavior.MinVersion, behavior.CurrentVersion)
	}

	// Get correct payload type for this version
	payloadType := behavior.PayloadTypes[version]
	if payloadType == nil {
		return nil, fmt.Errorf("no payload type for %s v%d", envelope.Kind, version)
	}

	// Create typed payload
	payload := reflect.New(payloadType).Interface()

	// Unmarshal into a temporary struct with typed payload
	// TODO: question: why not use runtime/message.go?
	var temp struct {
		ID         string          `json:"id"`
		ContentKey string          `json:"content_key,omitempty"`
		Kind       string          `json:"kind"`
		Version    int             `json:"version"`
		From       types.NaraName  `json:"from,omitempty"`
		FromID     types.NaraID    `json:"from_id"`
		To         types.NaraName  `json:"to,omitempty"`
		ToID       types.NaraID    `json:"to_id,omitempty"`
		Timestamp  time.Time       `json:"timestamp"`
		Payload    json.RawMessage `json:"payload"`
		Signature  []byte          `json:"signature,omitempty"`
		InReplyTo  string          `json:"in_reply_to,omitempty"`
	}
	if err := json.Unmarshal(raw, &temp); err != nil {
		return nil, err
	}

	// Unmarshal payload into typed struct
	if err := json.Unmarshal(temp.Payload, payload); err != nil {
		return nil, fmt.Errorf("unmarshal payload: %w", err)
	}

	msg := &Message{
		ID:         temp.ID,
		ContentKey: temp.ContentKey,
		Kind:       temp.Kind,
		Version:    temp.Version,
		From:       temp.From,
		FromID:     temp.FromID,
		To:         temp.To,
		ToID:       temp.ToID,
		Timestamp:  temp.Timestamp,
		Payload:    payload,
		Signature:  temp.Signature,
		InReplyTo:  temp.InReplyTo,
	}

	return msg, nil
}

// === Handler invocation ===

// invokeHandler calls the version-specific handler and returns any response messages.
// Handlers have signature: func(*Message, *PayloadType) ([]*Message, error)
func (rt *Runtime) invokeHandler(msg *Message, behavior *Behavior) []*Message {
	handler := behavior.Handlers[msg.Version]
	if handler == nil {
		logrus.Warnf("[runtime] no handler for %s v%d", msg.Kind, msg.Version)
		return nil
	}

	// Reflection call: handler(msg, msg.Payload) -> ([]*Message, error)
	handlerVal := reflect.ValueOf(handler)
	results := handlerVal.Call([]reflect.Value{
		reflect.ValueOf(msg),
		reflect.ValueOf(msg.Payload),
	})

	// results[0] = []*Message, results[1] = error
	var messages []*Message
	if len(results) > 0 && !results[0].IsNil() {
		messages = results[0].Interface().([]*Message)
	}
	if len(results) > 1 && !results[1].IsNil() {
		err := results[1].Interface().(error)
		rt.applyErrorStrategy(msg, "handler", err, behavior.Receive.OnError)
	}

	return messages
}

// === Error handling ===

func (rt *Runtime) applyErrorStrategy(msg *Message, stage string, err error, strategy ErrorStrategy) {
	switch strategy {
	case ErrorDrop:
		// Silent drop
	case ErrorLog:
		logrus.Warnf("[runtime] %s failed for %s: %v", stage, msg.Kind, err)
	case ErrorRetry:
		logrus.Warnf("[runtime] %s failed for %s (will retry): %v", stage, msg.Kind, err)
		// Retry logic would go here
	case ErrorQueue:
		logrus.Warnf("[runtime] %s failed for %s (queued): %v", stage, msg.Kind, err)
		// Dead letter queue logic would go here
	case ErrorPanic:
		logrus.Fatalf("[runtime] Critical failure in %s for %s: %v", stage, msg.Kind, err)
	}
}

// === Service management ===

// AddService registers a service with the runtime.
func (rt *Runtime) AddService(svc Service) error {
	rt.services = append(rt.services, svc)
	return nil
}

// Start starts all services.
func (rt *Runtime) Start() error {
	for _, svc := range rt.services {
		// Auto-populate ServiceBase if service embeds it
		log := rt.Log(svc.Name())
		if accessor, ok := svc.(ServiceBaseAccessor); ok {
			accessor.SetBase(rt, log)
		}

		// Call service-specific initialization
		if err := svc.Init(); err != nil {
			return fmt.Errorf("init %s: %w", svc.Name(), err)
		}
		log.Info("initialized")

		// If service implements BehaviorRegistrar, call it after Init
		if registrar, ok := svc.(BehaviorRegistrar); ok {
			registrar.RegisterBehaviors(rt)
		}
	}

	for _, svc := range rt.services {
		if err := svc.Start(); err != nil {
			return fmt.Errorf("start %s: %w", svc.Name(), err)
		}
		rt.Log(svc.Name()).Info("started")
	}

	return nil
}

// Stop stops all services.
func (rt *Runtime) Stop() error {
	rt.cancel()

	for _, svc := range rt.services {
		if err := svc.Stop(); err != nil {
			logrus.Warnf("[runtime] stop %s: %v", svc.Name(), err)
		} else {
			rt.Log(svc.Name()).Info("stopped")
		}
	}

	return nil
}

// === Behavior registration ===

// RegisterBehavior registers a behavior locally for this runtime.
// This allows each runtime to have its own handlers, avoiding conflicts
// in multi-nara tests where services register handlers with their own state.
func (rt *Runtime) RegisterBehavior(b *Behavior) {
	rt.localBehaviors[b.Kind] = b
}

// LookupBehavior looks up a behavior in this runtime's local registry.
func (rt *Runtime) LookupBehavior(kind string) *Behavior {
	return rt.localBehaviors[kind]
}
