package nara

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/eljojo/nara/runtime"
	"github.com/sirupsen/logrus"
)

// httpMeshMessageHandler receives runtime messages via HTTP mesh.
//
// This endpoint is used by the runtime's transport adapter to send
// messages directly to other naras. Messages are serialized as JSON
// and delivered to the runtime's Receive pipeline.
//
// Response messages from handlers are piggybacked in the HTTP response body,
// eliminating the need for a separate HTTP call.
//
// Endpoint: POST /mesh/message
// Body: JSON-serialized runtime.Message
// Response: JSON array of response messages (can be empty)
func (n *Network) httpMeshMessageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read raw message bytes
	body, err := io.ReadAll(r.Body)
	if err != nil {
		logrus.Warnf("[mesh] Failed to read message body: %v", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Deliver to runtime for processing
	if n.runtime == nil {
		logrus.Warn("[mesh] Runtime not initialized, dropping message")
		http.Error(w, "Runtime not ready", http.StatusServiceUnavailable)
		return
	}

	// CanPiggyback: true - we can include MeshOnly responses in HTTP response body
	responses, err := n.runtime.Receive(body, runtime.ReceiveOptions{CanPiggyback: true})
	if err != nil {
		logrus.Warnf("[mesh] Failed to process message: %v", err)
		// Still return 200 - the message was received, just not processed
		// (this matches the behavior of other mesh endpoints)
	}

	// Write piggybacked responses to body
	if len(responses) > 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(responses); err != nil {
			logrus.Warnf("[mesh] Failed to encode responses: %v", err)
		}
		return
	}

	w.WriteHeader(http.StatusOK)
}
