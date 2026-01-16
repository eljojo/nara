package nara

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/eljojo/nara/types"
)

type SyncRequest struct {
	From     types.NaraName   `json:"from"`     // who is asking
	Services []string         `json:"services"` // which services (empty = all)
	Subjects []types.NaraName `json:"subjects"` // which naras (empty = all)

	// Mode-based API (recommended)
	Mode       string `json:"mode,omitempty"`        // "sample", "page", or "recent"
	SampleSize int    `json:"sample_size,omitempty"` // for "sample" mode
	Cursor     string `json:"cursor,omitempty"`      // for "page" mode (timestamp of last event)
	PageSize   int    `json:"page_size,omitempty"`   // for "page" mode
	Limit      int    `json:"limit,omitempty"`       // for "recent" mode

	// Legacy parameters (deprecated, kept for backward compatibility)
	SinceTime  int64 `json:"since_time,omitempty"`  // events after this time
	SliceIndex int   `json:"slice_index,omitempty"` // for interleaved slicing
	SliceTotal int   `json:"slice_total,omitempty"` // total slices
	MaxEvents  int   `json:"max_events,omitempty"`  // limit
}

// SyncResponse contains events from a neighbor with optional signature
type SyncResponse struct {
	From       types.NaraName `json:"from"`
	Events     []SyncEvent    `json:"events"`
	NextCursor string         `json:"next_cursor,omitempty"` // For "page" mode pagination (timestamp of last event)
	Timestamp  int64          `json:"ts,omitempty"`          // When response was generated (Unix SECONDS, not nanoseconds)
	Signature  string         `json:"sig,omitempty"`         // Base64 Ed25519 signature
}

// NewSignedSyncResponse creates a signed sync response
func NewSignedSyncResponse(from types.NaraName, events []SyncEvent, keypair NaraKeypair) SyncResponse {
	resp := SyncResponse{
		From:      from,
		Events:    events,
		Timestamp: time.Now().Unix(),
	}

	// Sign the response
	resp.sign(keypair)
	return resp
}

// sign computes the signature for this response
func (r *SyncResponse) sign(keypair NaraKeypair) {
	// Skip signing if no valid keypair
	if len(keypair.PrivateKey) == 0 {
		return
	}
	r.Signature = keypair.SignBase64(r.signingData())
}

// signingData returns the canonical bytes to sign/verify
func (r *SyncResponse) signingData() []byte {
	hasher := sha256.New()

	// Include from and timestamp
	hasher.Write([]byte(fmt.Sprintf("%s:%d:", r.From, r.Timestamp)))

	// Include events (just their IDs for efficiency)
	for _, e := range r.Events {
		hasher.Write([]byte(e.ID))
	}

	return hasher.Sum(nil)
}

// VerifySignature verifies the response signature against a public key
func (r *SyncResponse) VerifySignature(publicKey ed25519.PublicKey) bool {
	if r.Signature == "" {
		return false
	}

	sigBytes, err := base64.StdEncoding.DecodeString(r.Signature)
	if err != nil {
		return false
	}

	data := r.signingData()
	return VerifySignature(publicKey, data, sigBytes)
}

// --- Personality-Aware Methods ---

// socialEventIsMeaningful decides if a personality cares enough to store the event
// This is the filter logic from SocialLedger.eventIsMeaningful, adapted for SyncEvent
