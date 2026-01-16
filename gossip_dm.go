package nara

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/eljojo/nara/types"
)

// SendDM sends a SyncEvent directly to a target nara via HTTP POST to /dm.
// Returns true if the DM was successfully delivered.
// If delivery fails, the event should already be in the sender's ledger and
// will spread via gossip instead.
func (network *Network) SendDM(targetName types.NaraName, event SyncEvent) bool {
	// Resolve nara name to ID
	naraID := network.getNaraIDByName(targetName)
	if naraID == "" {
		logrus.Debugf("📬 Cannot DM %s: could not resolve nara ID", targetName)
		return false
	}

	// Send via mesh client with timeout
	ctx, cancel := context.WithTimeout(network.ctx, 15*time.Second)
	defer cancel()

	err := network.meshClient.SendDM(ctx, naraID, event)
	if err != nil {
		logrus.Debugf("📬 Failed to send DM to %s: %v", targetName, err)
		return false
	}

	return true
}
