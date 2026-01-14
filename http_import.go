package nara

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

// SoulAuthenticatedRequest is the interface for requests with soul-based authentication
type SoulAuthenticatedRequest interface {
	GetTimestamp() int64
	GetSignature() string
	SigningData() []byte // Returns the data that was signed
}

type soulAuthContextKey string

const (
	soulAuthVerifiedKey soulAuthContextKey = "soul_auth_verified"
	soulAuthSoulKey     soulAuthContextKey = "soul_auth_soul"
)

// EventImportRequest is the request body for event import
type EventImportRequest struct {
	Events    []SyncEvent `json:"events"`
	Timestamp int64       `json:"ts"`  // Unix timestamp for replay protection
	Signature string      `json:"sig"` // Base64 Ed25519 signature
}

// Implement SoulAuthenticatedRequest interface
func (r EventImportRequest) GetTimestamp() int64  { return r.Timestamp }
func (r EventImportRequest) GetSignature() string { return r.Signature }
func (r EventImportRequest) SigningData() []byte {
	// Signing data: sha256(timestamp:event_ids)
	hasher := sha256.New()
	hasher.Write([]byte(fmt.Sprintf("%d:", r.Timestamp)))
	for _, e := range r.Events {
		hasher.Write([]byte(e.ID))
	}
	return hasher.Sum(nil)
}

// EventImportResponse is the response for event import
type EventImportResponse struct {
	Success    bool   `json:"success"`
	Imported   int    `json:"imported"`
	Duplicates int    `json:"duplicates"`
	Error      string `json:"error,omitempty"`
}

// soulAuthMiddleware verifies soul-based authentication for requests
// Checks that:
// 1. Timestamp is within 5-minute window (replay protection)
// 2. Soul matches this nara's soul (owner-only access)
// 3. Signature is valid
//
// Usage: wrap handlers that need soul-based auth
func (network *Network) soulAuthMiddleware(handler func(http.ResponseWriter, *http.Request, SoulAuthenticatedRequest)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// This middleware expects the handler to parse the request body
		// and call verifySoulAuth with the parsed request
		// For now, we'll just pass through - actual verification happens in handler
		// This is a placeholder for future enhancement
		handler(w, r, nil)
	}
}

// verifySoulAuth verifies a soul-authenticated request
// Returns error if authentication fails
// Verifies the signature using this nara's own keypair (derived from its soul)
func (network *Network) verifySoulAuth(req SoulAuthenticatedRequest) error {
	// Check timestamp freshness (prevent replay attacks)
	requestTime := time.Unix(req.GetTimestamp(), 0)
	age := time.Since(requestTime)
	if age > 5*time.Minute || age < -5*time.Minute {
		return fmt.Errorf("timestamp too old or in future: %v", age)
	}

	// Get signing data from request
	data := req.SigningData()

	// Verify signature using this nara's own keypair
	// Since the client must have the same soul to generate a valid signature,
	// this ensures only the owner can perform authenticated actions
	sig, err := base64.StdEncoding.DecodeString(req.GetSignature())
	if err != nil {
		return fmt.Errorf("invalid signature encoding: %w", err)
	}

	if !VerifySignature(network.local.Keypair.PublicKey, data, sig) {
		return fmt.Errorf("signature verification failed")
	}

	return nil
}

// httpEventsImportHandler handles POST /api/events/import
// Allows the nara owner to import events with soul-based authentication
func (network *Network) httpEventsImportHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req EventImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendJSONError(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Verify soul-based authentication using the reusable middleware
	if err := network.verifySoulAuth(req); err != nil {
		logrus.Warnf("Soul auth failed: %v", err)
		sendJSONError(w, "Authentication failed: "+err.Error(), http.StatusForbidden)
		return
	}

	logrus.Infof("📥 Importing %d events", len(req.Events))

	// Import events using the existing MergeEvents method
	imported := network.local.SyncLedger.MergeEvents(req.Events)

	// Trigger projection update if we have projections
	if network.local.Projections != nil {
		network.local.Projections.Trigger()
	}

	logrus.Infof("✅ Imported %d events (%d duplicates)", imported, len(req.Events)-imported)

	response := EventImportResponse{
		Success:    true,
		Imported:   imported,
		Duplicates: len(req.Events) - imported,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}


// sendJSONError sends a JSON error response
func sendJSONError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(EventImportResponse{
		Success: false,
		Error:   message,
	})
}
