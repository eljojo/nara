package stash

import (
	"time"

	"github.com/eljojo/nara/messages"
	"github.com/eljojo/nara/runtime"
	"github.com/sirupsen/logrus"
)

// === Version-specific handlers ===

// handleRefreshV1 handles stash-refresh broadcasts.
//
// When someone broadcasts a refresh request, we check if we have their stash
// and if so, we send it back to them directly via mesh.
func (s *Service) handleRefreshV1(msg *runtime.Message, p *messages.StashRefreshPayload) {
	s.log.Debug("received refresh request from %s", p.OwnerID)

	// Check if we have a stash for this owner
	stash := s.retrieve(p.OwnerID)
	if stash == nil {
		s.log.Debug("no stash for %s", p.OwnerID)
		return
	}

	// Send the stash back via mesh
	response := &runtime.Message{
		Kind: "stash:response",
		ToID: p.OwnerID,
		Payload: &messages.StashResponsePayload{
			OwnerID:    p.OwnerID,
			Found:      true,
			Nonce:      stash.Nonce,
			Ciphertext: stash.Ciphertext,
			StoredAt:   stash.StoredAt.Unix(),
		},
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
	logrus.Infof("[stash] received store request from %s (msgID: %s)", p.OwnerID, msg.ID)

	// Validate payload
	if err := p.Validate(); err != nil {
		logrus.Warnf("[stash] invalid store payload from %s: %v", p.OwnerID, err)

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

	logrus.Infof("[stash] sending success ack to %s (InReplyTo: %s)", p.OwnerID, msg.ID)
	// Send success ack
	err := s.rt.Emit(msg.Reply("stash:ack", &messages.StashStoreAck{
		OwnerID:  p.OwnerID,
		Success:  true,
		StoredAt: time.Now().Unix(),
	}))
	if err != nil {
		logrus.Errorf("[stash] failed to send ack: %v", err)
	} else {
		logrus.Infof("[stash] ack sent successfully to %s", p.OwnerID)
	}
}

// handleStoreAckV1 handles stash:ack responses.
//
// This is called when a confidant acknowledges our store request.
// We use the correlator to match this to the pending request.
func (s *Service) handleStoreAckV1(msg *runtime.Message, p *messages.StashStoreAck) {
	logrus.Infof("[stash] received store ack from %s (InReplyTo: %s, Success: %v)", msg.FromID, msg.InReplyTo, p.Success)

	// Match to pending request via correlator
	// The correlator uses msg.InReplyTo to find the original request
	matched := s.storeCorrelator.Receive(msg.InReplyTo, *p)

	if !matched {
		logrus.Warnf("[stash] received unexpected store ack from %s (no pending request, InReplyTo: %s)", msg.FromID, msg.InReplyTo)
	} else {
		logrus.Infof("[stash] successfully matched ack to pending request %s", msg.InReplyTo)
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

		// Send not-found response
		_ = s.rt.Emit(msg.Reply("stash:response", &messages.StashResponsePayload{
			OwnerID:   p.OwnerID,
			RequestID: p.RequestID,
			Found:     false,
		}))
		return
	}

	// Look up their stash
	stash := s.retrieve(p.OwnerID)
	if stash == nil {
		s.log.Debug("no stash for %s", p.OwnerID)

		// Send not-found response
		_ = s.rt.Emit(msg.Reply("stash:response", &messages.StashResponsePayload{
			OwnerID:   p.OwnerID,
			RequestID: p.RequestID,
			Found:     false,
		}))
		return
	}

	// Send the stash back
	_ = s.rt.Emit(msg.Reply("stash:response", &messages.StashResponsePayload{
		OwnerID:    p.OwnerID,
		RequestID:  p.RequestID,
		Found:      true,
		Nonce:      stash.Nonce,
		Ciphertext: stash.Ciphertext,
		StoredAt:   stash.StoredAt.Unix(),
	}))

	s.log.Info("sent stash to %s (%d bytes)", p.OwnerID, len(stash.Ciphertext))
}

// handleResponseV1 handles stash:response messages.
//
// This is called when a confidant sends us our stash back.
func (s *Service) handleResponseV1(msg *runtime.Message, p *messages.StashResponsePayload) {
	s.log.Debug("received response from %s (found=%v)", msg.FromID, p.Found)

	// Match to pending request via correlator
	matched := s.requestCorrelator.Receive(msg.InReplyTo, *p)

	if !matched {
		s.log.Warn("received unexpected response from %s (no pending request)", msg.FromID)
	}
}
