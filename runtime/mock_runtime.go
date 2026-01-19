package runtime

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/eljojo/nara/types"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

// MockRuntime implements RuntimeInterface for testing services.
//
// It captures emitted messages, allows delivering messages to services,
// and provides fake infrastructure without MQTT/mesh dependencies.
type MockRuntime struct {
	t       *testing.T
	name    types.NaraName
	id      types.NaraID
	Emitted []*Message // Captured Emit() calls for assertions

	handlers   map[string][]func(*Message)
	keypair    *MockKeypair
	pubKeys    map[string][]byte
	env        Environment
	behaviors  map[string]*Behavior
	memoryMode string // Configurable for tests (default: "normal")
	calls      *CallRegistry
}

// NewMockRuntime creates a mock runtime with auto-cleanup via t.Cleanup().
func NewMockRuntime(t *testing.T, name types.NaraName, id types.NaraID) *MockRuntime {
	t.Helper()

	mock := &MockRuntime{
		t:         t,
		name:      name,
		id:        id,
		Emitted:   make([]*Message, 0),
		handlers:  make(map[string][]func(*Message)),
		keypair:   NewMockKeypair(),
		pubKeys:   make(map[string][]byte),
		env:       EnvTest,
		behaviors: make(map[string]*Behavior),
		calls:     NewCallRegistry(),
	}

	t.Cleanup(func() {
		mock.Stop()
	})

	return mock
}

// === RuntimeInterface implementation ===

func (m *MockRuntime) Me() *Nara {
	return &Nara{
		ID:   m.id,
		Name: m.name,
	}
}

func (m *MockRuntime) MeID() types.NaraID {
	return m.id
}

// Emit captures messages for test assertions.
func (m *MockRuntime) Emit(msg *Message) error {
	// Set defaults like real runtime
	if msg.ID == "" {
		msg.ID = ComputeID(msg)
	}
	if msg.FromID == "" {
		msg.FromID = m.id
	}

	// Capture for assertions
	m.Emitted = append(m.Emitted, msg)

	// Notify handlers
	for _, handler := range m.handlers[msg.Kind] {
		handler(msg)
	}

	return nil
}

func (m *MockRuntime) Log(service string) *ServiceLog {
	return &ServiceLog{
		name:   service,
		logger: &Logger{},
	}
}

func (m *MockRuntime) Env() Environment {
	return m.env
}

func (m *MockRuntime) OnlinePeers() []*PeerInfo {
	return nil // Mock returns no peers by default
}

func (m *MockRuntime) MemoryMode() string {
	if m.memoryMode != "" {
		return m.memoryMode
	}
	return "normal" // Default
}

// SetMemoryMode sets the memory mode for testing (low/medium/high).
func (m *MockRuntime) SetMemoryMode(mode string) {
	m.memoryMode = mode
}

// Keypair returns the keypair interface.
func (m *MockRuntime) Keypair() KeypairInterface {
	return m.keypair
}

// Identity returns the identity interface (MockRuntime implements it).
func (m *MockRuntime) Identity() IdentityInterface {
	return m // MockRuntime implements IdentityInterface
}

// Call emits a message and tracks for response correlation.
func (m *MockRuntime) Call(msg *Message, timeout time.Duration) <-chan CallResult {
	ch := make(chan CallResult, 1)

	// Ensure message has an ID
	if msg.ID == "" {
		msg.ID = ComputeID(msg)
	}

	// Register the pending call
	m.calls.Register(msg.ID, ch, timeout)

	// Emit the message (captures for assertions)
	if err := m.Emit(msg); err != nil {
		m.calls.Cancel(msg.ID)
		ch <- CallResult{Error: err}
		return ch
	}

	return ch
}

// ResolveCall allows tests to simulate a response arriving.
// Call this after checking Emitted to simulate the response.
func (m *MockRuntime) ResolveCall(inReplyTo string, response *Message) bool {
	return m.calls.Resolve(inReplyTo, response)
}

// === IdentityInterface implementation ===

func (m *MockRuntime) LookupPublicKey(id types.NaraID) []byte {
	return m.pubKeys[string(id)]
}

func (m *MockRuntime) LookupPublicKeyByName(name types.NaraName) []byte {
	// For mock, just use name as ID
	return m.pubKeys[string(name)]
}

func (m *MockRuntime) RegisterPublicKey(id types.NaraID, key []byte) {
	m.pubKeys[string(id)] = key
}

// === Test helpers ===

// Deliver simulates receiving a message (calls behavior handlers).
//
// If the message has InReplyTo set, it first checks if there's a pending
// Call waiting for that response (simulating how the real runtime works).
// If the call is resolved, the handler is NOT invoked.
//
// Use this to test how a service reacts to incoming messages.
func (m *MockRuntime) Deliver(msg *Message) {
	// Check if this is a response to a pending Call (same as real runtime)
	if msg.InReplyTo != "" {
		if m.calls.Resolve(msg.InReplyTo, msg) {
			return // Handled as call response, don't invoke handler
		}
		// Not a pending call - fall through to normal handling
	}

	// Look up behavior in local registry
	behavior := m.behaviors[msg.Kind]
	if behavior == nil {
		m.t.Fatalf("no behavior registered for kind %s", msg.Kind)
		return
	}

	// Find version-specific handler
	handler := behavior.Handlers[msg.Version]
	if handler == nil {
		m.t.Fatalf("no handler for %s v%d", msg.Kind, msg.Version)
		return
	}

	// Invoke handler using reflection (same as real runtime)
	// Handler signature: func(*Message, *PayloadType)
	defer func() {
		if r := recover(); r != nil {
			m.t.Fatalf("handler panicked for %s: %v", msg.Kind, r)
		}
	}()
	handlerVal := reflect.ValueOf(handler)
	handlerVal.Call([]reflect.Value{
		reflect.ValueOf(msg),
		reflect.ValueOf(msg.Payload),
	})
}

// Subscribe registers a handler for a message kind.
func (m *MockRuntime) Subscribe(kind string, handler func(*Message)) {
	m.handlers[kind] = append(m.handlers[kind], handler)
}

// RegisterBehavior allows tests to register behaviors manually.
func (m *MockRuntime) RegisterBehavior(b *Behavior) {
	m.behaviors[b.Kind] = b
}

// Stop cleans up the mock runtime.
func (m *MockRuntime) Stop() {
	m.handlers = nil
	m.Emitted = nil
}

// === Assertion helpers ===

// EmittedCount returns the number of emitted messages.
func (m *MockRuntime) EmittedCount() int {
	return len(m.Emitted)
}

// LastEmitted returns the most recently emitted message.
func (m *MockRuntime) LastEmitted() *Message {
	if len(m.Emitted) == 0 {
		return nil
	}
	return m.Emitted[len(m.Emitted)-1]
}

// EmittedOfKind returns all emitted messages of a given kind.
func (m *MockRuntime) EmittedOfKind(kind string) []*Message {
	var result []*Message
	for _, msg := range m.Emitted {
		if msg.Kind == kind {
			result = append(result, msg)
		}
	}
	return result
}

// Clear clears all captured messages.
func (m *MockRuntime) Clear() {
	m.Emitted = make([]*Message, 0)
}

// === Mock Keypair ===

// MockKeypair is a fake keypair for testing.
type MockKeypair struct {
	pub          ed25519.PublicKey
	priv         ed25519.PrivateKey
	symmetricKey []byte // Cached encryption key
}

// NewMockKeypair creates a new mock keypair.
func NewMockKeypair() *MockKeypair {
	pub, priv, _ := ed25519.GenerateKey(nil)

	// Derive and cache encryption key (same HKDF as production)
	seed := priv.Seed()
	hkdfReader := hkdf.New(sha256.New, seed, []byte("nara:stash:v1"), []byte("symmetric"))
	symmetricKey := make([]byte, 32)
	if _, err := io.ReadFull(hkdfReader, symmetricKey); err != nil {
		panic("hkdf failed: " + err.Error())
	}

	return &MockKeypair{
		pub:          pub,
		priv:         priv,
		symmetricKey: symmetricKey,
	}
}

func (k *MockKeypair) Sign(data []byte) []byte {
	return ed25519.Sign(k.priv, data)
}

func (k *MockKeypair) PublicKey() []byte {
	return k.pub
}

// Seal encrypts plaintext using XChaCha20-Poly1305.
func (k *MockKeypair) Seal(plaintext []byte) (nonce, ciphertext []byte, err error) {
	// Derive a 32-byte symmetric key from the Ed25519 seed using HKDF
	seed := k.priv.Seed()
	hkdfReader := hkdf.New(sha256.New, seed, nil, []byte("nara-self-encryption"))
	symmetricKey := make([]byte, chacha20poly1305.KeySize)
	if _, err := io.ReadFull(hkdfReader, symmetricKey); err != nil {
		return nil, nil, err
	}

	// Create AEAD cipher
	aead, err := chacha20poly1305.NewX(symmetricKey)
	if err != nil {
		return nil, nil, err
	}

	// Generate random nonce
	nonce = make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}

	// Encrypt
	ciphertext = aead.Seal(nil, nonce, plaintext, nil)
	return nonce, ciphertext, nil
}

// Open decrypts ciphertext using XChaCha20-Poly1305.
func (k *MockKeypair) Open(nonce, ciphertext []byte) ([]byte, error) {
	// Derive the same symmetric key from the Ed25519 seed
	seed := k.priv.Seed()
	hkdfReader := hkdf.New(sha256.New, seed, nil, []byte("nara-self-encryption"))
	symmetricKey := make([]byte, chacha20poly1305.KeySize)
	if _, err := io.ReadFull(hkdfReader, symmetricKey); err != nil {
		return nil, err
	}

	// Create AEAD cipher
	aead, err := chacha20poly1305.NewX(symmetricKey)
	if err != nil {
		return nil, err
	}

	// Decrypt
	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}
