package nara

import (
	"io"
	"net/http"

	"github.com/sirupsen/logrus"
)

// httpMeshMessageHandler receives runtime messages via HTTP mesh.
//
// This endpoint is used by the runtime's transport adapter to send
// messages directly to other naras. Messages are serialized as JSON
// and delivered to the runtime's Receive pipeline.
//
// Endpoint: POST /mesh/message
// Body: JSON-serialized runtime.Message
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

	if err := n.runtime.Receive(body); err != nil {
		logrus.Warnf("[mesh] Failed to process message: %v", err)
		// Still return 200 - the message was received, just not processed
		// (this matches the behavior of other mesh endpoints)
	}

	w.WriteHeader(http.StatusOK)
}
