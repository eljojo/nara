package nara

import (
	"fmt"

	"github.com/sirupsen/logrus"
)

// processLedgerRequests processes incoming ledger sync requests
func (network *Network) processLedgerRequests() {
	for {
		select {
		case req := <-network.ledgerRequestInbox:
			network.handleLedgerRequest(req)
		case <-network.ctx.Done():
			logrus.Debug("processLedgerRequests: shutting down")
			return
		}
	}
}

// handleLedgerRequest responds to a sync request with events from our ledger
func (network *Network) handleLedgerRequest(req LedgerRequest) {
	if network.ReadOnly {
		return
	}

	// Get events for requested subjects from our ledger
	events := network.local.SyncLedger.GetSocialEventsForSubjects(req.Subjects)

	// Respond directly to the requester
	response := LedgerResponse{
		From:   network.meName(),
		Events: events,
	}

	topic := fmt.Sprintf("nara/ledger/%s/response", req.From.String())
	network.postEvent(topic, response)
	logrus.Infof("📤 sent %d events to %s", len(events), req.From.String())
}

// processLedgerResponses processes incoming ledger sync responses
func (network *Network) processLedgerResponses() {
	for {
		select {
		case resp := <-network.ledgerResponseInbox:
			network.handleLedgerResponse(resp)
		case <-network.ctx.Done():
			logrus.Debug("processLedgerResponses: shutting down")
			return
		}
	}
}

// handleLedgerResponse merges received events into our ledger
func (network *Network) handleLedgerResponse(resp LedgerResponse) {
	// Merge received events into our ledger (with personality filtering)
	added := network.local.SyncLedger.MergeSocialEventsFiltered(resp.Events, network.local.Me.Status.Personality)
	if added > 0 && network.logService != nil {
		network.logService.BatchGossipMerge(resp.From, added)
	}
}
