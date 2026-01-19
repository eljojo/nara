package stash

import (
	"fmt"
	"time"

	"github.com/eljojo/nara/messages"
	"github.com/eljojo/nara/runtime"
	"github.com/eljojo/nara/types"
)

// === Version-specific handlers ===

// buildStashResponse creates a StashResponsePayload.
// If stash is nil, returns a "not found" response.
// Otherwise returns a "found" response with the stash data.
func buildStashResponse(ownerID types.NaraID, requestID string, stash *EncryptedStash) *messages.StashResponsePayload {
	if stash == nil {
		return &messages.StashResponsePayload{
			OwnerID:   ownerID,
			RequestID: requestID,
			Found:     false,
		}
	}

	return &messages.StashResponsePayload{
		OwnerID:    ownerID,
		RequestID:  requestID,
		Found:      true,
		Nonce:      stash.Nonce,
		Ciphertext: stash.Ciphertext,
		StoredAt:   stash.StoredAt.Unix(),
	}
}

// handleRefreshV1 handles stash-refresh broadcasts.
//
// When someone broadcasts a refresh request, we check if we have their stash
// and if so, we return a response message to be piggybacked or emitted.
func (s *Service) handleRefreshV1(msg *runtime.Message, p *messages.StashRefreshPayload) ([]*runtime.Message, error) {
	s.Log.Debug("received refresh request from %s", p.OwnerID)

	// Check if we have a stash for this owner
	stash := s.GetStoredStash(p.OwnerID)
	if stash == nil {
		s.Log.Debug("no stash for %s", p.OwnerID)
		return nil, nil // No stash, no response
	}

	s.Log.Info("responding with stash for %s (%d bytes)", p.OwnerID, len(stash.Ciphertext))

	// Return response message for runtime to emit
	response := &runtime.Message{
		Kind:    "stash:response",
		ToID:    p.OwnerID,
		Payload: buildStashResponse(p.OwnerID, "", stash),
	}

	return []*runtime.Message{response}, nil
}

// handleStoreV1 handles stash:store requests.
//
// Someone wants to store their encrypted stash with us (we're their confidant).
// Returns an ack message for piggybacking in the HTTP response.
func (s *Service) handleStoreV1(msg *runtime.Message, p *messages.StashStorePayload) ([]*runtime.Message, error) {
	s.Log.Info("received store request from %s (msgID: %s)", p.OwnerID, msg.ID)

	// Validate payload
	if err := p.Validate(); err != nil {
		s.Log.Warn("invalid store payload from %s: %v", p.OwnerID, err)

		// Return failure ack
		return []*runtime.Message{msg.Reply("stash:ack", &messages.StashStoreAck{
			OwnerID:  p.OwnerID,
			Success:  false,
			Reason:   err.Error(),
			StoredAt: time.Now().Unix(),
		})}, nil
	}

	// Check storage limit (allow updates for existing owners)
	if !s.canStore(p.OwnerID) {
		s.Log.Warn("storage limit reached, rejecting store from %s", p.OwnerID)

		return []*runtime.Message{msg.Reply("stash:ack", &messages.StashStoreAck{
			OwnerID:  p.OwnerID,
			Success:  false,
			Reason:   "storage limit reached",
			StoredAt: time.Now().Unix(),
		})}, nil
	}

	// Store the encrypted stash
	s.store(p.OwnerID, p.Nonce, p.Ciphertext)

	s.Log.Info("returning success ack for %s (InReplyTo: %s)", p.OwnerID, msg.ID)

	// Return success ack for piggybacking
	return []*runtime.Message{msg.Reply("stash:ack", &messages.StashStoreAck{
		OwnerID:  p.OwnerID,
		Success:  true,
		StoredAt: time.Now().Unix(),
	})}, nil
}

// handleStoreAckV1 handles stash:ack responses.
//
// Note: Most acks are handled automatically by the runtime's CallRegistry
// (when the ack is a response to our Call() request). This handler only
// runs for acks that don't match a pending call.
func (s *Service) handleStoreAckV1(msg *runtime.Message, p *messages.StashStoreAck) ([]*runtime.Message, error) {
	// This should rarely happen - acks are normally handled by CallRegistry
	s.Log.Debug("received unexpected store ack from %s (InReplyTo: %s, Success: %v)", msg.FromID, msg.InReplyTo, p.Success)
	return nil, nil // No response needed
}

// handleRequestV1 handles stash:request messages.
//
// Someone is asking for their stash that we're storing.
// Returns a response message for piggybacking in the HTTP response.
func (s *Service) handleRequestV1(msg *runtime.Message, p *messages.StashRequestPayload) ([]*runtime.Message, error) {
	s.Log.Debug("received request from %s", p.OwnerID)

	// Validate
	if err := p.Validate(); err != nil {
		s.Log.Warn("invalid request from %s: %v", p.OwnerID, err)
		return []*runtime.Message{msg.Reply("stash:response", buildStashResponse(p.OwnerID, p.RequestID, nil))}, nil
	}

	stash := s.GetStoredStash(p.OwnerID)
	if stash == nil {
		s.Log.Warn("request failed: no stash found for %s", p.OwnerID)
		return []*runtime.Message{msg.Reply("stash:response", buildStashResponse(p.OwnerID, p.RequestID, nil))}, nil
	}

	s.Log.Info("returning stash for %s (%d bytes)", p.OwnerID, len(stash.Ciphertext))
	return []*runtime.Message{msg.Reply("stash:response", buildStashResponse(p.OwnerID, p.RequestID, stash))}, nil
}

// handleResponseV1 handles stash:response messages.
//
// Note: Most responses are handled automatically by the runtime's CallRegistry
// (when the response is to our Call() request). This handler only runs for
// responses that don't match a pending call - typically proactive recovery
// from the "hey-there" flow.
func (s *Service) handleResponseV1(msg *runtime.Message, p *messages.StashResponsePayload) ([]*runtime.Message, error) {
	s.Log.Debug("received response from %s (found=%v)", msg.FromID, p.Found)

	// This handler only runs for responses that didn't match a pending Call.
	// Check if it's a proactive recovery (hey-there flow).
	// If we have no stash, we accept authoritative pushes from confidants.
	if !s.HasStashData() && p.Found && len(p.Ciphertext) > 0 {
		s.Log.Info("received proactive stash recovery from %s", msg.FromID)

		// Decrypt and set as our local stash
		if err := s.handleRecoveryPayload(p); err != nil {
			return nil, fmt.Errorf("process proactive recovery: %w", err)
		}
		return nil, nil
	}

	// Not a proactive recovery - this is unexpected
	if s.HasStashData() {
		s.Log.Debug("received unexpected response from %s (we already have stash data)", msg.FromID)
	}
	return nil, nil // No response needed
}

// handleRecoveryPayload processes a recovery payload (decryption and storage).
func (s *Service) handleRecoveryPayload(p *messages.StashResponsePayload) error {
	// Decrypt using keypair
	plaintext, err := s.keypair.Open(p.Nonce, p.Ciphertext)
	if err != nil {
		return fmt.Errorf("decrypt: %w", err)
	}

	// Update local state
	s.mu.Lock()
	s.myStashData = plaintext
	s.myStashTimestamp = p.StoredAt
	s.mu.Unlock()

	s.Log.Info("successfully recovered stash (%d bytes)", len(plaintext))
	return nil
}
