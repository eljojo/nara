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
// and if so, we send it back to them directly via mesh.
func (s *Service) handleRefreshV1(msg *runtime.Message, p *messages.StashRefreshPayload) error {
	s.Log.Debug("received refresh request from %s", p.OwnerID)

	// Check if we have a stash for this owner
	stash := s.GetStoredStash(p.OwnerID)
	if stash == nil {
		s.Log.Debug("no stash for %s", p.OwnerID)
		return nil
	}

	// Send the stash back via mesh
	response := &runtime.Message{
		Kind:    "stash:response",
		ToID:    p.OwnerID,
		Payload: buildStashResponse(p.OwnerID, "", stash),
	}

	if err := s.RT.Emit(response); err != nil {
		return fmt.Errorf("send stash response: %w", err)
	}

	s.Log.Info("sent stash to %s (%d bytes)", p.OwnerID, len(stash.Ciphertext))
	return nil
}

// handleStoreV1 handles stash:store requests.
//
// Someone wants to store their encrypted stash with us (we're their confidant).
func (s *Service) handleStoreV1(msg *runtime.Message, p *messages.StashStorePayload) error {
	s.Log.Info("received store request from %s (msgID: %s)", p.OwnerID, msg.ID)

	// Validate payload
	if err := p.Validate(); err != nil {
		s.Log.Warn("invalid store payload from %s: %v", p.OwnerID, err)

		// Send failure ack
		if emitErr := s.RT.Emit(msg.Reply("stash:ack", &messages.StashStoreAck{
			OwnerID:  p.OwnerID,
			Success:  false,
			Reason:   err.Error(),
			StoredAt: time.Now().Unix(),
		})); emitErr != nil {
			return fmt.Errorf("send validation failure ack: %w", emitErr)
		}
		return nil // Validation error is not a handler error - we handled it
	}

	// Check storage limit (allow updates for existing owners)
	if !s.canStore(p.OwnerID) {
		s.Log.Warn("storage limit reached, rejecting store from %s", p.OwnerID)

		if err := s.RT.Emit(msg.Reply("stash:ack", &messages.StashStoreAck{
			OwnerID:  p.OwnerID,
			Success:  false,
			Reason:   "storage limit reached",
			StoredAt: time.Now().Unix(),
		})); err != nil {
			return fmt.Errorf("send storage limit ack: %w", err)
		}
		return nil // Limit reached is not a handler error - we handled it
	}

	// Store the encrypted stash
	s.store(p.OwnerID, p.Nonce, p.Ciphertext)

	s.Log.Info("sending success ack to %s (InReplyTo: %s)", p.OwnerID, msg.ID)
	// Send success ack
	if err := s.RT.Emit(msg.Reply("stash:ack", &messages.StashStoreAck{
		OwnerID:  p.OwnerID,
		Success:  true,
		StoredAt: time.Now().Unix(),
	})); err != nil {
		return fmt.Errorf("send success ack: %w", err)
	}

	s.Log.Info("ack sent successfully to %s", p.OwnerID)
	return nil
}

// handleStoreAckV1 handles stash:ack responses.
//
// Note: Most acks are handled automatically by the runtime's CallRegistry
// (when the ack is a response to our Call() request). This handler only
// runs for acks that don't match a pending call.
func (s *Service) handleStoreAckV1(msg *runtime.Message, p *messages.StashStoreAck) error {
	// This should rarely happen - acks are normally handled by CallRegistry
	s.Log.Debug("received unexpected store ack from %s (InReplyTo: %s, Success: %v)", msg.FromID, msg.InReplyTo, p.Success)
	return nil
}

// handleRequestV1 handles stash:request messages.
//
// Someone is asking for their stash that we're storing.
func (s *Service) handleRequestV1(msg *runtime.Message, p *messages.StashRequestPayload) error {
	s.Log.Debug("received request from %s", p.OwnerID)

	// Validate
	if err := p.Validate(); err != nil {
		s.Log.Warn("invalid request from %s: %v", p.OwnerID, err)
		if emitErr := s.RT.Emit(msg.Reply("stash:response", buildStashResponse(p.OwnerID, p.RequestID, nil))); emitErr != nil {
			return fmt.Errorf("send validation failure response: %w", emitErr)
		}
		return nil // Validation error handled
	}

	stash := s.GetStoredStash(p.OwnerID)
	if stash == nil {
		s.Log.Warn("request failed: no stash found for %s", p.OwnerID)
		if emitErr := s.RT.Emit(msg.Reply("stash:response", buildStashResponse(p.OwnerID, p.RequestID, nil))); emitErr != nil {
			return fmt.Errorf("send not-found response: %w", emitErr)
		}
		return nil // Not-found handled
	}

	// Send the stash back
	if err := s.RT.Emit(msg.Reply("stash:response", buildStashResponse(p.OwnerID, p.RequestID, stash))); err != nil {
		return fmt.Errorf("send stash response: %w", err)
	}

	s.Log.Info("sent stash to %s (%d bytes)", p.OwnerID, len(stash.Ciphertext))
	return nil
}

// handleResponseV1 handles stash:response messages.
//
// Note: Most responses are handled automatically by the runtime's CallRegistry
// (when the response is to our Call() request). This handler only runs for
// responses that don't match a pending call - typically proactive recovery
// from the "hey-there" flow.
func (s *Service) handleResponseV1(msg *runtime.Message, p *messages.StashResponsePayload) error {
	s.Log.Debug("received response from %s (found=%v)", msg.FromID, p.Found)

	// This handler only runs for responses that didn't match a pending Call.
	// Check if it's a proactive recovery (hey-there flow).
	// If we have no stash, we accept authoritative pushes from confidants.
	if !s.HasStashData() && p.Found && len(p.Ciphertext) > 0 {
		s.Log.Info("received proactive stash recovery from %s", msg.FromID)

		// Decrypt and set as our local stash
		if err := s.handleRecoveryPayload(p); err != nil {
			return fmt.Errorf("process proactive recovery: %w", err)
		}
		return nil
	}

	// Not a proactive recovery - this is unexpected
	if s.HasStashData() {
		s.Log.Debug("received unexpected response from %s (we already have stash data)", msg.FromID)
	}
	return nil
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
