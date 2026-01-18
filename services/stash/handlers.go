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
func (s *Service) handleRefreshV1(msg *runtime.Message, p *messages.StashRefreshPayload) {
	s.log.Debug("received refresh request from %s", p.OwnerID)

	// Check if we have a stash for this owner
	stash := s.GetStoredStash(p.OwnerID)
	if stash == nil {
		s.log.Debug("no stash for %s", p.OwnerID)
		return
	}

	// Send the stash back via mesh
	response := &runtime.Message{
		Kind:    "stash:response",
		ToID:    p.OwnerID,
		Payload: buildStashResponse(p.OwnerID, "", stash),
	}

	if err := s.rt.Emit(response); err != nil {
		s.log.Error("failed to send stash response: %v", err)
	} else {
		s.log.Info("sent stash to %s (%d bytes)", p.OwnerID, len(stash.Ciphertext))
	}
}

// handleStoreV1 handles stash:store requests.
//
// Someone wants to store their encrypted stash with us (we're their confidant).
func (s *Service) handleStoreV1(msg *runtime.Message, p *messages.StashStorePayload) {
	s.log.Info("received store request from %s (msgID: %s)", p.OwnerID, msg.ID)

	// Validate payload
	if err := p.Validate(); err != nil {
		s.log.Warn("invalid store payload from %s: %v", p.OwnerID, err)

		// Send failure ack
		_ = s.rt.Emit(msg.Reply("stash:ack", &messages.StashStoreAck{
			OwnerID:  p.OwnerID,
			Success:  false,
			Reason:   err.Error(),
			StoredAt: time.Now().Unix(),
		}))
		return
	}

	// Store the encrypted stash
	s.store(p.OwnerID, p.Nonce, p.Ciphertext)

	s.log.Info("sending success ack to %s (InReplyTo: %s)", p.OwnerID, msg.ID)
	// Send success ack
	err := s.rt.Emit(msg.Reply("stash:ack", &messages.StashStoreAck{
		OwnerID:  p.OwnerID,
		Success:  true,
		StoredAt: time.Now().Unix(),
	}))
	if err != nil {
		s.log.Error("failed to send ack: %v", err)
	} else {
		s.log.Info("ack sent successfully to %s", p.OwnerID)
	}
}

// handleStoreAckV1 handles stash:ack responses.
//
// This is called when a confidant acknowledges our store request.
// We use the correlator to match this to the pending request.
func (s *Service) handleStoreAckV1(msg *runtime.Message, p *messages.StashStoreAck) {
	s.log.Info("received store ack from %s (InReplyTo: %s, Success: %v)", msg.FromID, msg.InReplyTo, p.Success)

	// Match to pending request via correlator
	// The correlator uses msg.InReplyTo to find the original request
	matched := s.storeCorrelator.Receive(msg.InReplyTo, *p)

	if !matched {
		s.log.Warn("received unexpected store ack from %s (no pending request, InReplyTo: %s)", msg.FromID, msg.InReplyTo)
	} else {
		s.log.Info("successfully matched ack to pending request %s", msg.InReplyTo)
	}
}

// handleRequestV1 handles stash:request messages.
//
// Someone is asking for their stash that we're storing.
func (s *Service) handleRequestV1(msg *runtime.Message, p *messages.StashRequestPayload) {
	s.log.Debug("received request from %s", p.OwnerID)

	// Validate
	if err := p.Validate(); err != nil {
		s.log.Warn("invalid request from %s: %v", p.OwnerID, err)
		_ = s.rt.Emit(msg.Reply("stash:response", buildStashResponse(p.OwnerID, p.RequestID, nil)))
		return
	}

	stash := s.GetStoredStash(p.OwnerID)
	if stash == nil {
		s.log.Warn("request failed: no stash found for %s", p.OwnerID)
		_ = s.rt.Emit(msg.Reply("stash:response", buildStashResponse(p.OwnerID, p.RequestID, nil)))
		return
	}

	// Send the stash back
	_ = s.rt.Emit(msg.Reply("stash:response", buildStashResponse(p.OwnerID, p.RequestID, stash)))

	s.log.Info("sent stash to %s (%d bytes)", p.OwnerID, len(stash.Ciphertext))
}

// handleResponseV1 handles stash:response messages.
//
// This is called when a confidant sends us our stash back.
func (s *Service) handleResponseV1(msg *runtime.Message, p *messages.StashResponsePayload) {
	s.log.Debug("received response from %s (found=%v)", msg.FromID, p.Found)

	// Try to match to pending request via correlator first
	// This handles explicit RequestFrom() calls
	matched := s.requestCorrelator.Receive(msg.InReplyTo, *p)
	if matched {
		return
	}

	// If not matched, check if it's a proactive recovery (hey-there flow)
	// If we have no stash, we accept authoritative pushes from confidants
	if !s.HasStashData() && p.Found && len(p.Ciphertext) > 0 {
		s.log.Info("received proactive stash recovery from %s", msg.FromID)

		// Decrypt and set as our local stash
		if err := s.handleRecoveryPayload(p); err != nil {
			s.log.Error("failed to process proactive recovery: %v", err)
		}
		return
	}

	// Only warn if we're not in recovery mode and it wasn't a proactive push
	if !s.HasStashData() {
		// matches proactive push criteria but failed something?
		// actually if it didn't match proactive criteria (e.g. not found or empty), we might just ignore it
	} else {
		s.log.Warn("received unexpected response from %s (no pending request)", msg.FromID)
	}
}

// handleRecoveryPayload processes a recovery payload (decryption and storage).
func (s *Service) handleRecoveryPayload(p *messages.StashResponsePayload) error {
	// Decrypt using runtime's keypair
	plaintext, err := s.rt.Open(p.Nonce, p.Ciphertext)
	if err != nil {
		return fmt.Errorf("decrypt: %w", err)
	}

	// Update local state
	s.mu.Lock()
	s.myStashData = plaintext
	s.myStashTimestamp = p.StoredAt
	s.mu.Unlock()

	s.log.Info("successfully recovered stash (%d bytes)", len(plaintext))
	return nil
}
