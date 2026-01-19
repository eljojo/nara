package runtime

import (
	"reflect"
	"testing"
	"time"

	"github.com/eljojo/nara/types"
)

// TestReceive_MQTTContext_EmitsResponse tests that when a message arrives
// via MQTT (CanPiggyback: false), the handler's MeshOnly response is emitted
// via normal transport rather than being returned for piggybacking.
//
// Flow:
//   MQTT broadcast [ping:request] → handler returns [ping:response (MeshOnly)]
//   → response is EMITTED (not piggybacked since no HTTP response to piggyback on)
func TestReceive_MQTTContext_EmitsResponse(t *testing.T) {
	// Create runtime with tracking transport
	transport := &trackingTransport{}
	rt := newTestRuntime(t, transport)

	// Register behaviors
	registerPingBehaviors(rt)

	// Create a ping request message
	reqMsg := &Message{
		ID:        "req-123",
		Kind:      "ping:request",
		Version:   1,
		FromID:    "alice-id",
		ToID:      "bob-id",
		Timestamp: time.Now(),
		Payload:   &PingRequestPayload{Message: "hello"},
	}

	// Marshal it (as if it arrived over the wire)
	reqBytes, err := reqMsg.Marshal()
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	// Receive with CanPiggyback: false (simulating MQTT context)
	piggybacked, err := rt.Receive(reqBytes, ReceiveOptions{CanPiggyback: false})
	if err != nil {
		t.Fatalf("Receive failed: %v", err)
	}

	// Should NOT have piggybacked anything (MQTT can't piggyback)
	if len(piggybacked) != 0 {
		t.Errorf("expected 0 piggybacked messages, got %d", len(piggybacked))
	}

	// Should have emitted the response via transport
	if len(transport.emitted) != 1 {
		t.Fatalf("expected 1 emitted message, got %d", len(transport.emitted))
	}

	emitted := transport.emitted[0]
	if emitted.Kind != "ping:response" {
		t.Errorf("expected ping:response, got %s", emitted.Kind)
	}
	if emitted.InReplyTo != "req-123" {
		t.Errorf("expected InReplyTo=req-123, got %s", emitted.InReplyTo)
	}
}

// TestReceive_HTTPContext_PiggybacksResponse tests that when a message arrives
// via HTTP mesh (CanPiggyback: true), the handler's MeshOnly response is
// piggybacked (returned) rather than being emitted separately.
//
// Flow:
//   HTTP POST [ping:request] → handler returns [ping:response (MeshOnly)]
//   → response is PIGGYBACKED in HTTP response body (not emitted separately)
func TestReceive_HTTPContext_PiggybacksResponse(t *testing.T) {
	// Create runtime with tracking transport
	transport := &trackingTransport{}
	rt := newTestRuntime(t, transport)

	// Register behaviors
	registerPingBehaviors(rt)

	// Create a ping request message
	reqMsg := &Message{
		ID:        "req-456",
		Kind:      "ping:request",
		Version:   1,
		FromID:    "alice-id",
		ToID:      "bob-id",
		Timestamp: time.Now(),
		Payload:   &PingRequestPayload{Message: "hello"},
	}

	// Marshal it
	reqBytes, err := reqMsg.Marshal()
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	// Receive with CanPiggyback: true (simulating HTTP context)
	piggybacked, err := rt.Receive(reqBytes, ReceiveOptions{CanPiggyback: true})
	if err != nil {
		t.Fatalf("Receive failed: %v", err)
	}

	// Should have piggybacked the response
	if len(piggybacked) != 1 {
		t.Fatalf("expected 1 piggybacked message, got %d", len(piggybacked))
	}

	piggy := piggybacked[0]
	if piggy.Kind != "ping:response" {
		t.Errorf("expected ping:response, got %s", piggy.Kind)
	}
	if piggy.InReplyTo != "req-456" {
		t.Errorf("expected InReplyTo=req-456, got %s", piggy.InReplyTo)
	}

	// Should NOT have emitted via transport (piggybacking avoids extra HTTP call)
	if len(transport.emitted) != 0 {
		t.Errorf("expected 0 emitted messages (piggybacked instead), got %d", len(transport.emitted))
	}
}

// TestReceive_HTTPContext_NonMeshOnlyResponse tests that even in HTTP context,
// responses that don't use MeshOnly transport are emitted normally.
//
// Flow:
//   HTTP POST [broadcast:request] → handler returns [broadcast:response (MQTT)]
//   → response is EMITTED via MQTT (can't piggyback non-MeshOnly)
func TestReceive_HTTPContext_NonMeshOnlyResponse(t *testing.T) {
	// Create runtime with tracking transport
	transport := &trackingTransport{}
	rt := newTestRuntime(t, transport)

	// Register behaviors - response uses MQTT, not MeshOnly
	registerBroadcastBehaviors(rt)

	// Create a broadcast request message
	reqMsg := &Message{
		ID:        "req-789",
		Kind:      "broadcast:request",
		Version:   1,
		FromID:    "alice-id",
		Timestamp: time.Now(),
		Payload:   &BroadcastRequestPayload{Topic: "announcements"},
	}

	// Marshal it
	reqBytes, err := reqMsg.Marshal()
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	// Receive with CanPiggyback: true (HTTP context)
	piggybacked, err := rt.Receive(reqBytes, ReceiveOptions{CanPiggyback: true})
	if err != nil {
		t.Fatalf("Receive failed: %v", err)
	}

	// Should NOT have piggybacked (response uses MQTT, not MeshOnly)
	if len(piggybacked) != 0 {
		t.Errorf("expected 0 piggybacked messages (MQTT can't piggyback), got %d", len(piggybacked))
	}

	// Should have emitted via MQTT transport
	if len(transport.mqttPublished) != 1 {
		t.Fatalf("expected 1 MQTT published message, got %d", len(transport.mqttPublished))
	}

	if transport.mqttPublished[0].topic != "nara/plaza/broadcast" {
		t.Errorf("expected topic nara/plaza/broadcast, got %s", transport.mqttPublished[0].topic)
	}
}

// === Test Infrastructure ===

// PingRequestPayload is a test payload for ping requests.
type PingRequestPayload struct {
	Message string `json:"message"`
}

// PingResponsePayload is a test payload for ping responses.
type PingResponsePayload struct {
	Reply string `json:"reply"`
}

// BroadcastRequestPayload is a test payload for broadcast requests.
type BroadcastRequestPayload struct {
	Topic string `json:"topic"`
}

// BroadcastResponsePayload is a test payload for broadcast responses.
type BroadcastResponsePayload struct {
	Delivered bool `json:"delivered"`
}

// trackingTransport records all transport operations for assertions.
type trackingTransport struct {
	emitted       []*Message
	mqttPublished []mqttPublish
}

type mqttPublish struct {
	topic string
	data  []byte
}

func (t *trackingTransport) TrySendDirect(targetID types.NaraID, msg *Message) ([]*Message, error) {
	t.emitted = append(t.emitted, msg)
	return nil, nil // No piggybacked responses in this test
}

func (t *trackingTransport) PublishMQTT(topic string, data []byte) error {
	t.mqttPublished = append(t.mqttPublished, mqttPublish{topic: topic, data: data})
	return nil
}

// newTestRuntime creates a minimal runtime for testing Receive behavior.
func newTestRuntime(t *testing.T, transport TransportInterface) *Runtime {
	t.Helper()

	keypair := NewMockKeypair()

	rt := &Runtime{
		me:             &Nara{ID: "test-nara-id", Name: "test-nara"},
		keypair:        keypair,
		transport:      transport,
		identity:       &testIdentity{keys: make(map[types.NaraID][]byte)},
		logger:         &Logger{},
		env:            EnvTest,
		calls:          NewCallRegistry(),
		localBehaviors: make(map[string]*Behavior),
	}

	return rt
}

// registerPingBehaviors registers test behaviors for ping request/response.
// Request: received via any transport
// Response: uses MeshOnly (can be piggybacked)
func registerPingBehaviors(rt *Runtime) {
	// ping:request - can arrive via MQTT or mesh
	rt.RegisterBehavior(&Behavior{
		Kind:           "ping:request",
		CurrentVersion: 1,
		MinVersion:     1,
		PayloadTypes:   map[int]reflect.Type{1: reflect.TypeOf(PingRequestPayload{})},
		Handlers: map[int]any{
			1: func(msg *Message, p *PingRequestPayload) ([]*Message, error) {
				// Handler returns a MeshOnly response
				return []*Message{msg.Reply("ping:response", &PingResponsePayload{
					Reply: "pong: " + p.Message,
				})}, nil
			},
		},
		Receive: ReceiveBehavior{
			Verify: NoVerify(), // Skip verification for tests
			Dedupe: NoDedupe(),
		},
	})

	// ping:response - uses MeshOnly transport (can be piggybacked)
	rt.RegisterBehavior(&Behavior{
		Kind:           "ping:response",
		CurrentVersion: 1,
		MinVersion:     1,
		PayloadTypes:   map[int]reflect.Type{1: reflect.TypeOf(PingResponsePayload{})},
		Emit: EmitBehavior{
			Sign:      DefaultSign(),
			Transport: MeshOnly(), // This makes it piggybackable!
		},
	})
}

// registerBroadcastBehaviors registers test behaviors where response uses MQTT.
// This tests that non-MeshOnly responses are emitted even in HTTP context.
func registerBroadcastBehaviors(rt *Runtime) {
	// broadcast:request
	rt.RegisterBehavior(&Behavior{
		Kind:           "broadcast:request",
		CurrentVersion: 1,
		MinVersion:     1,
		PayloadTypes:   map[int]reflect.Type{1: reflect.TypeOf(BroadcastRequestPayload{})},
		Handlers: map[int]any{
			1: func(msg *Message, p *BroadcastRequestPayload) ([]*Message, error) {
				// Handler returns an MQTT broadcast response (not MeshOnly)
				return []*Message{{
					Kind:    "broadcast:response",
					Payload: &BroadcastResponsePayload{Delivered: true},
				}}, nil
			},
		},
		Receive: ReceiveBehavior{
			Verify: NoVerify(),
			Dedupe: NoDedupe(),
		},
	})

	// broadcast:response - uses MQTT transport (cannot be piggybacked)
	rt.RegisterBehavior(&Behavior{
		Kind:           "broadcast:response",
		CurrentVersion: 1,
		MinVersion:     1,
		PayloadTypes:   map[int]reflect.Type{1: reflect.TypeOf(BroadcastResponsePayload{})},
		Emit: EmitBehavior{
			Sign:      DefaultSign(),
			Transport: MQTT("nara/plaza/broadcast"), // MQTT, not MeshOnly!
		},
	})
}

// testIdentity is a minimal IdentityInterface for tests.
type testIdentity struct {
	keys map[types.NaraID][]byte
}

func (i *testIdentity) LookupPublicKey(id types.NaraID) []byte {
	return i.keys[id]
}

func (i *testIdentity) RegisterPublicKey(id types.NaraID, key []byte) {
	i.keys[id] = key
}

// NoDedupe returns a stage that skips deduplication (for tests).
func NoDedupe() Stage {
	return &noDedupeStage{}
}

type noDedupeStage struct{}

func (s *noDedupeStage) Process(msg *Message, ctx *PipelineContext) StageResult {
	return Continue(msg)
}
