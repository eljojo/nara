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
		calls:          &CallRegistry{pending: make(map[string]*pendingCall)},
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

// LookupPublicKey looks up a public key by nara ID.
func (rt *Runtime) LookupPublicKey(id types.NaraID) []byte {
	if rt.identity == nil {
		return nil
	}
	return rt.identity.LookupPublicKey(id)
}

// LookupPublicKeyByName looks up a public key by nara name.
func (rt *Runtime) LookupPublicKeyByName(name types.NaraName) []byte {
	if rt.identity == nil {
		return nil
	}
	return rt.identity.LookupPublicKeyByName(name)
}

// RegisterPublicKey registers a public key for a nara ID.
func (rt *Runtime) RegisterPublicKey(id types.NaraID, key []byte) {
	if rt.identity != nil {
		rt.identity.RegisterPublicKey(id, key)
	}
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

// StorageLimit returns the maximum number of stashes based on memory mode.
func (rt *Runtime) StorageLimit() int {
	if rt.networkInfo == nil {
		return 5 // Default to low
	}
	return rt.networkInfo.StorageLimit()
}

// Seal encrypts plaintext using the runtime's keypair.
func (rt *Runtime) Seal(plaintext []byte) (nonce, ciphertext []byte, err error) {
	if rt.keypair == nil {
		return nil, nil, fmt.Errorf("keypair not configured")
	}
	return rt.keypair.Seal(plaintext)
}

// Open decrypts ciphertext using the runtime's keypair.
func (rt *Runtime) Open(nonce, ciphertext []byte) ([]byte, error) {
	if rt.keypair == nil {
		return nil, fmt.Errorf("keypair not configured")
	}
	return rt.keypair.Open(nonce, ciphertext)
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

// Receive processes an incoming message through the receive pipeline.
func (rt *Runtime) Receive(raw []byte) error {
	// Deserialize using behavior's PayloadType
	msg, err := rt.deserialize(raw)
	if err != nil {
		return fmt.Errorf("deserialize: %w", err)
	}

	// Check if this is a response to a pending Call (Chapter 3)
	if msg.InReplyTo != "" {
		if rt.calls.Resolve(msg.InReplyTo, msg) {
			return nil // Handled as call response
		}
		// Not a pending call - fall through to normal handling
	}

	behavior := rt.LookupBehavior(msg.Kind)
	if behavior == nil {
		return fmt.Errorf("unknown message kind: %s", msg.Kind)
	}

	logrus.Infof("[runtime] processing %s (from: %s, InReplyTo: %s)", msg.Kind, msg.FromID, msg.InReplyTo)

	// Build and run receive pipeline
	pipeline := rt.buildReceivePipeline(behavior)
	ctx := rt.newPipelineContext()

	result := pipeline.Run(msg, ctx)

	// Handle result
	if result.Error != nil {
		rt.applyErrorStrategy(msg, "receive", result.Error, behavior.Receive.OnError)
		return result.Error
	}
	if result.Message == nil {
		logrus.Debugf("[runtime] message %s dropped in receive: %s", msg.Kind, result.Reason)
		return nil
	}

	logrus.Infof("[runtime] invoking handler for %s", msg.Kind)
	// Invoke version-specific handler
	rt.invokeHandler(msg, behavior)

	return nil
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

func (rt *Runtime) invokeHandler(msg *Message, behavior *Behavior) {
	handler := behavior.Handlers[msg.Version]
	if handler == nil {
		logrus.Warnf("[runtime] no handler for %s v%d", msg.Kind, msg.Version)
		return
	}

	// Reflection call: handler(msg, msg.Payload)
	handlerVal := reflect.ValueOf(handler)
	handlerVal.Call([]reflect.Value{
		reflect.ValueOf(msg),
		reflect.ValueOf(msg.Payload),
	})
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

	// If service implements BehaviorRegistrar, call it
	if registrar, ok := svc.(BehaviorRegistrar); ok {
		registrar.RegisterBehaviors(rt)
	}

	return nil
}

// Start starts all services.
func (rt *Runtime) Start() error {
	for _, svc := range rt.services {
		// Automatically provide logger scoped to service name
		log := rt.Log(svc.Name())
		if err := svc.Init(rt, log); err != nil {
			return fmt.Errorf("init %s: %w", svc.Name(), err)
		}
	}

	for _, svc := range rt.services {
		if err := svc.Start(); err != nil {
			return fmt.Errorf("start %s: %w", svc.Name(), err)
		}
	}

	return nil
}

// Stop stops all services.
func (rt *Runtime) Stop() error {
	rt.cancel()

	for _, svc := range rt.services {
		if err := svc.Stop(); err != nil {
			logrus.Warnf("[runtime] stop %s: %v", svc.Name(), err)
		}
	}

	return nil
}

// === BehaviorRegistry implementation ===

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
