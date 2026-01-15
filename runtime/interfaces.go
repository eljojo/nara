package runtime

import "time"

// RuntimeInterface is what services and stages can access.
//
// This interface prevents circular dependencies and makes it easy
// to create mocks for testing.
type RuntimeInterface interface {
	// Identity
	Me() *Nara
	MeID() string

	// Public key management
	LookupPublicKey(id string) []byte
	LookupPublicKeyByName(name string) []byte
	RegisterPublicKey(id string, key []byte)

	// Messaging
	Emit(msg *Message) error

	// Logging (runtime primitive, not a service)
	Log(service string) *ServiceLog

	// Environment
	Env() Environment

	// Network information (for automatic confidant selection)
	OnlinePeers() []*PeerInfo
	MemoryMode() string
	StorageLimit() int
}

// PeerInfo contains information about a network peer.
type PeerInfo struct {
	ID     string
	Name   string
	Uptime time.Duration
}

// LedgerInterface is what store stages use.
type LedgerInterface interface {
	Add(msg *Message, priority int) error
	HasID(id string) bool
	HasContentKey(contentKey string) bool
}

// TransportInterface is what transport stages use.
type TransportInterface interface {
	PublishMQTT(topic string, data []byte) error
	TrySendDirect(targetID string, msg *Message) error
}

// GossipQueueInterface is what gossip stages use.
type GossipQueueInterface interface {
	Add(msg *Message)
}

// NetworkInfoInterface provides network and peer information.
type NetworkInfoInterface interface {
	OnlinePeers() []*PeerInfo
	MemoryMode() string
	StorageLimit() int
}

// KeypairInterface is what sign stages use.
type KeypairInterface interface {
	Sign(data []byte) []byte
	PublicKey() []byte
}

// EventBusInterface is what notification stages use.
type EventBusInterface interface {
	Emit(msg *Message)
	Subscribe(kind string, handler func(*Message))
}

// Personality affects filtering behavior.
type Personality struct {
	Agreeableness int // 0-100
	Sociability   int // 0-100
	Chill         int // 0-100
}

// Nara represents a network participant.
//
// The full Nara struct will be migrated in Chapter 2.
type Nara struct {
	ID   string
	Name string
}

// Environment enum for runtime behavior.
//
// Like Rails environments - different defaults for different contexts.
type Environment int

const (
	EnvProduction  Environment = iota // Graceful: log errors, don't crash
	EnvDevelopment                    // Loud: warnings, fail on suspicious things
	EnvTest                           // Strict: panic on errors, catch bugs early
)

// ServiceLog is a logger scoped to a specific service.
//
// Services get this from rt.Log("service_name").
type ServiceLog struct {
	name   string
	logger *Logger
}

// Logger methods
func (l *ServiceLog) Debug(format string, args ...any) {
	// Implementation will come in Phase 3
}

func (l *ServiceLog) Info(format string, args ...any) {
	// Implementation will come in Phase 3
}

func (l *ServiceLog) Warn(format string, args ...any) {
	// Implementation will come in Phase 3
}

func (l *ServiceLog) Error(format string, args ...any) {
	// Implementation will come in Phase 3
}

// Logger is the central logging coordinator (owned by Runtime).
type Logger struct {
	// Implementation will come in Phase 3
}

// CallResult is returned by CallAsync (Chapter 3).
type CallResult struct {
	Response *Message
	Error    error
}

// Service is what all services implement.
//
// Services register with the runtime and get lifecycle callbacks.
type Service interface {
	// Identity
	Name() string

	// Lifecycle
	Init(rt RuntimeInterface) error
	Start() error
	Stop() error
}

// BehaviorRegistrar is optionally implemented by services that register behaviors.
type BehaviorRegistrar interface {
	RegisterBehaviors(rt RuntimeInterface)
}

// BehaviorRegistry is optionally implemented by runtimes that support local behavior registration.
// The mock runtime implements this to allow per-runtime behavior registration (for tests).
// The real runtime may delegate to the global registry.
type BehaviorRegistry interface {
	RegisterBehavior(b *Behavior)
}

// CallRegistry manages pending Call requests (Chapter 3).
type CallRegistry struct {
	// Implementation in Chapter 3
	pending map[string]*pendingCall
}

type pendingCall struct {
	ch      chan CallResult //nolint:unused // Will be used in Chapter 3
	expires time.Time       //nolint:unused // Will be used in Chapter 3
}

func (r *CallRegistry) Register(id string, ch chan CallResult, timeout time.Duration) {
	// Implementation in Chapter 3
}

func (r *CallRegistry) Resolve(inReplyTo string, response *Message) bool {
	// Implementation in Chapter 3
	return false
}

func (r *CallRegistry) Cancel(id string) {
	// Implementation in Chapter 3
}
