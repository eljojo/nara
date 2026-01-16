package nara

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/eljojo/nara/identity"
	"github.com/eljojo/nara/types"
)

// MockMeshHTTPTransport implements http.RoundTripper for in-memory message routing in tests
type MockMeshHTTPTransport struct {
	handlers map[types.NaraID]func(req *http.Request) (*http.Response, error)
	mu       sync.RWMutex
}

// NewMockMeshHTTPTransport creates a new mock HTTP transport
func NewMockMeshHTTPTransport() *MockMeshHTTPTransport {
	return &MockMeshHTTPTransport{
		handlers: make(map[types.NaraID]func(req *http.Request) (*http.Response, error)),
	}
}

// RegisterHandler registers a handler for a specific nara ID
func (m *MockMeshHTTPTransport) RegisterHandler(naraID types.NaraID, handler func(req *http.Request) (*http.Response, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[naraID] = handler
}

// RoundTrip implements http.RoundTripper
func (m *MockMeshHTTPTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Extract nara ID from URL (format: mock://<naraID>/path)
	host := req.URL.Host
	naraID := types.NaraID(host)

	handler, ok := m.handlers[naraID]
	if !ok {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(bytes.NewReader([]byte("peer not found"))),
			Header:     make(http.Header),
		}, nil
	}

	return handler(req)
}

// CreateMockWorldMessageHandler creates a handler for /world/relay that routes to a channel
func CreateMockWorldMessageHandler(inbox chan *WorldMessage) func(req *http.Request) (*http.Response, error) {
	return func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/world/relay" {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(bytes.NewReader([]byte("not found"))),
				Header:     make(http.Header),
			}, nil
		}

		// Decode world message
		var wm WorldMessage
		if err := json.NewDecoder(req.Body).Decode(&wm); err != nil {
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Body:       io.NopCloser(bytes.NewReader([]byte(fmt.Sprintf("decode error: %v", err)))),
				Header:     make(http.Header),
			}, nil
		}

		// Route to inbox
		select {
		case inbox <- &wm:
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte("OK"))),
				Header:     make(http.Header),
			}, nil
		default:
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Body:       io.NopCloser(bytes.NewReader([]byte("inbox full"))),
				Header:     make(http.Header),
			}, nil
		}
	}
}

// NewMockMeshClientForTesting creates a mesh client with in-memory routing for tests
func NewMockMeshClientForTesting(name types.NaraName, keypair identity.NaraKeypair, transport *MockMeshHTTPTransport) *MeshClient {
	client := &http.Client{
		Transport: transport,
	}

	return NewMeshClient(client, name, keypair)
}
