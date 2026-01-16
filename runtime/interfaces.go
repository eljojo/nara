package runtime

import (
	"time"

	"github.com/eljojo/nara/types"
	"github.com/sirupsen/logrus"
)

// RuntimeInterface is what services and stages can access.
//
// This interface prevents circular dependencies and makes it easy
// to create mocks for testing.
type RuntimeInterface interface {
	// Identity
	Me() *Nara
	MeID() types.NaraID

	// Public key management
	LookupPublicKey(id types.NaraID) []byte
	LookupPublicKeyByName(name types.NaraName) []byte
	RegisterPublicKey(id types.NaraID, key []byte)

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

	// Self-encryption (only the owner can decrypt)
	Seal(plaintext []byte) (nonce, ciphertext []byte, err error)
	Open(nonce, ciphertext []byte) ([]byte, error)
}

// PeerInfo contains information about a network peer.
type PeerInfo struct {
	ID     types.NaraID
	Name   types.NaraName
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
	TrySendDirect(targetID types.NaraID, msg *Message) error
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
	// Encryption (self-encryption using XChaCha20-Poly1305)
	Seal(plaintext []byte) (nonce, ciphertext []byte, err error)
	Open(nonce, ciphertext []byte) ([]byte, error)
}

// IdentityInterface provides public key lookups for message verification.
type IdentityInterface interface {
	LookupPublicKey(id types.NaraID) []byte
	LookupPublicKeyByName(name types.NaraName) []byte
	RegisterPublicKey(id types.NaraID, key []byte)
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
// Notice there's also Nara struct in nara.go
// The full Nara struct will be migrated in Chapter 2.
type Nara struct {
	ID   types.NaraID
	Name types.NaraName
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
	logger LoggerInterface
}

// Logger methods forward to the logger with service name prefix
func (l *ServiceLog) Debug(format string, args ...any) {
	if l.logger != nil {
		l.logger.Debug(l.name, format, args...)
	}
}

func (l *ServiceLog) Info(format string, args ...any) {
	if l.logger != nil {
		l.logger.Info(l.name, format, args...)
	}
}

func (l *ServiceLog) Warn(format string, args ...any) {
	if l.logger != nil {
		l.logger.Warn(l.name, format, args...)
	}
}

func (l *ServiceLog) Error(format string, args ...any) {
	if l.logger != nil {
		l.logger.Error(l.name, format, args...)
	}
}

// LoggerInterface is the interface that the runtime logger must implement.
// This allows services to log without depending on the concrete logger implementation.
type LoggerInterface interface {
	Debug(service string, format string, args ...any)
	Info(service string, format string, args ...any)
	Warn(service string, format string, args ...any)
	Error(service string, format string, args ...any)
}

// Logger is the default logger implementation that logs to logrus.
// This is used when no custom logger is provided.
type Logger struct{}

// Debug logs a debug message.
func (l *Logger) Debug(service string, format string, args ...any) {
	logrus.Debugf("[%s] "+format, append([]any{service}, args...)...)
}

// Info logs an info message.
func (l *Logger) Info(service string, format string, args ...any) {
	logrus.Infof("[%s] "+format, append([]any{service}, args...)...)
}

// Warn logs a warning message.
func (l *Logger) Warn(service string, format string, args ...any) {
	logrus.Warnf("[%s] "+format, append([]any{service}, args...)...)
}

// Error logs an error message.
func (l *Logger) Error(service string, format string, args ...any) {
	logrus.Errorf("[%s] "+format, append([]any{service}, args...)...)
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
	// Init is called by the runtime with the runtime interface and a logger
	// scoped to this service's name. Services should NOT call rt.Log() themselves.
	Init(rt RuntimeInterface, log *ServiceLog) error
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
