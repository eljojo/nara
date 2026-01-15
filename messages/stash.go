package messages

import "errors"

// StashStorePayload is sent when storing encrypted data with a confidant.
//
// Kind: stash:store
// Flow: Owner → Confidant
// Response: StashStoreAck
// Transport: MeshOnly (direct HTTP)
//
// Version History:
//
//	v1 (2026-01): Initial version
type StashStorePayload struct {
	// OwnerID is who the stash belongs to (primary identifier for retrieval)
	OwnerID string `json:"owner_id"`

	// Owner is the display name (optional, for logging/debugging)
	Owner string `json:"owner,omitempty"`

	// Nonce for XChaCha20-Poly1305 decryption (24 bytes)
	Nonce []byte `json:"nonce"`

	// Ciphertext is the encrypted stash data
	Ciphertext []byte `json:"ciphertext"`

	// Timestamp when the stash was created (Unix timestamp)
	Timestamp int64 `json:"ts"`
}

// Validate checks if the payload is well-formed.
func (p *StashStorePayload) Validate() error {
	if p.OwnerID == "" {
		return errors.New("owner_id required")
	}
	if len(p.Nonce) != 24 {
		return errors.New("nonce must be 24 bytes for XChaCha20-Poly1305")
	}
	if len(p.Ciphertext) == 0 {
		return errors.New("ciphertext required")
	}
	return nil
}

// StashStoreAck acknowledges successful storage of a stash.
//
// Kind: stash:ack
// Flow: Confidant → Owner (response to stash:store)
// Transport: MeshOnly
//
// Version History:
//
//	v1 (2026-01): Initial version
type StashStoreAck struct {
	// OwnerID is echoed back for correlation
	OwnerID string `json:"owner_id"`

	// StoredAt is when the confidant stored the data (Unix timestamp)
	StoredAt int64 `json:"stored_at"`

	// Success indicates if the storage was successful
	Success bool `json:"success"`

	// Reason explains why storage failed (if Success is false)
	Reason string `json:"reason,omitempty"`
}

// StashRequestPayload requests stored data from a confidant.
//
// Kind: stash:request
// Flow: Owner → Confidant
// Response: StashResponsePayload
// Transport: MeshOnly
//
// Version History:
//
//	v1 (2026-01): Initial version
type StashRequestPayload struct {
	// OwnerID is who is requesting their stash back
	OwnerID string `json:"owner_id"`

	// RequestID for correlation (optional, for tracking)
	RequestID string `json:"request_id,omitempty"`
}

// Validate checks if the payload is well-formed.
func (p *StashRequestPayload) Validate() error {
	if p.OwnerID == "" {
		return errors.New("owner_id required")
	}
	return nil
}

// StashResponsePayload returns stored data to the owner.
//
// Kind: stash:response
// Flow: Confidant → Owner (response to stash:request)
// Transport: MeshOnly
//
// Version History:
//
//	v1 (2026-01): Initial version
type StashResponsePayload struct {
	// OwnerID is who the stash belongs to
	OwnerID string `json:"owner_id"`

	// RequestID is echoed from the request (for correlation)
	RequestID string `json:"request_id,omitempty"`

	// Found indicates if the confidant has a stash for this owner
	Found bool `json:"found"`

	// Nonce for decryption (only if Found is true)
	Nonce []byte `json:"nonce,omitempty"`

	// Ciphertext of the stored data (only if Found is true)
	Ciphertext []byte `json:"ciphertext,omitempty"`

	// StoredAt is when the stash was originally stored (Unix timestamp)
	StoredAt int64 `json:"stored_at,omitempty"`
}

// StashRefreshPayload triggers stash recovery from confidants.
//
// Kind: stash-refresh
// Flow: Broadcast via MQTT plaza
// Transport: MQTT (ephemeral, not stored)
//
// This is sent when a nara starts up and wants to recover its stash
// from confidants. Confidants who have a stash for this owner will
// respond via mesh with stash:response.
//
// Version History:
//
//	v1 (2026-01): Initial version
type StashRefreshPayload struct {
	// OwnerID is who wants their stash back
	OwnerID string `json:"owner_id"`

	// Owner is the display name (optional)
	Owner string `json:"owner,omitempty"`
}

// Validate checks if the payload is well-formed.
func (p *StashRefreshPayload) Validate() error {
	if p.OwnerID == "" {
		return errors.New("owner_id required")
	}
	return nil
}
