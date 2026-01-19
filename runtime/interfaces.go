package runtime

import (
	"errors"
	"sync"
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

	// Messaging
	Emit(msg *Message) error

	// Logging (runtime primitive, not a service)
	Log(service string) *ServiceLog

	// Environment
	Env() Environment

	// Network information (for automatic confidant selection)
	OnlinePeers() []*PeerInfo
	MemoryMode() string

	// Object access - callers use these directly instead of pass-through methods.
	// Runtime guarantees these are always non-nil.
	Keypair() KeypairInterface
	Identity() IdentityInterface

	// Request/response (Call emits and waits for a reply)
	Call(msg *Message, timeout time.Duration) <-chan CallResult
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
//
// Supports per-service filtering via Disable/Enable methods.
type Logger struct {
	mu       sync.RWMutex
	disabled map[string]bool // service names to suppress
}

// NewLogger creates a new logger with optional disabled services.
func NewLogger(disabledServices ...string) *Logger {
	l := &Logger{
		disabled: make(map[string]bool),
	}
	for _, s := range disabledServices {
		l.disabled[s] = true
	}
	return l
}

// Disable suppresses logging for the given service names.
func (l *Logger) Disable(services ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.disabled == nil {
		l.disabled = make(map[string]bool)
	}
	for _, s := range services {
		l.disabled[s] = true
	}
}

// Enable re-enables logging for the given service names.
func (l *Logger) Enable(services ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, s := range services {
		delete(l.disabled, s)
	}
}

// isEnabled checks if logging is enabled for a service (internal, lock-free check).
func (l *Logger) isEnabled(service string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.disabled == nil || !l.disabled[service]
}

// Debug logs a debug message.
func (l *Logger) Debug(service string, format string, args ...any) {
	if !l.isEnabled(service) {
		return
	}
	logrus.Debugf("[%s] "+format, append([]any{service}, args...)...)
}

// Info logs an info message.
func (l *Logger) Info(service string, format string, args ...any) {
	if !l.isEnabled(service) {
		return
	}
	logrus.Infof("[%s] "+format, append([]any{service}, args...)...)
}

// Warn logs a warning message.
func (l *Logger) Warn(service string, format string, args ...any) {
	if !l.isEnabled(service) {
		return
	}
	logrus.Warnf("[%s] "+format, append([]any{service}, args...)...)
}

// Error logs an error message.
func (l *Logger) Error(service string, format string, args ...any) {
	if !l.isEnabled(service) {
		return
	}
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

// CallRegistry manages pending Call requests.
//
// This is the central request/response correlation system. When a service
// calls rt.Call(msg, timeout), the registry tracks the pending request and
// matches incoming responses via InReplyTo.
type CallRegistry struct {
	pending map[string]*pendingCall
	mu      sync.Mutex
}

type pendingCall struct {
	ch      chan CallResult
	sentAt  time.Time
	expires time.Time
}

// NewCallRegistry creates a new call registry.
func NewCallRegistry() *CallRegistry {
	r := &CallRegistry{
		pending: make(map[string]*pendingCall),
	}
	go r.reapLoop()
	return r
}

// Register adds a pending call to track.
func (r *CallRegistry) Register(id string, ch chan CallResult, timeout time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.pending[id] = &pendingCall{
		ch:      ch,
		sentAt:  time.Now(),
		expires: time.Now().Add(timeout),
	}
}

// Resolve matches an incoming response to a pending call.
// Returns true if a pending call was found and resolved.
func (r *CallRegistry) Resolve(inReplyTo string, response *Message) bool {
	r.mu.Lock()
	pending, ok := r.pending[inReplyTo]
	if ok {
		delete(r.pending, inReplyTo)
	}
	r.mu.Unlock()

	if ok {
		pending.ch <- CallResult{Response: response}
		return true
	}

	return false
}

// Cancel removes a pending call without resolving it.
func (r *CallRegistry) Cancel(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.pending, id)
}

// reapLoop cleans up timed-out requests.
func (r *CallRegistry) reapLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for range ticker.C {
		r.mu.Lock()
		now := time.Now()
		for id, req := range r.pending {
			if now.After(req.expires) {
				req.ch <- CallResult{Error: ErrCallTimeout}
				delete(r.pending, id)
			}
		}
		r.mu.Unlock()
	}
}

// ErrCallTimeout is returned when a call times out.
var ErrCallTimeout = errors.New("call timed out")
