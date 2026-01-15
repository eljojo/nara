package nara

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

// SendDM sends a SyncEvent directly to a target nara via HTTP POST to /dm.
// Returns true if the DM was successfully delivered.
// If delivery fails, the event should already be in the sender's ledger and
// will spread via gossip instead.
// TODO: Migrate to MeshClient.SendDM() method to reduce code duplication and improve maintainability
func (network *Network) SendDM(targetName NaraName, event SyncEvent) bool {
	// Determine URL - use test override if available
	url := network.buildMeshURL(targetName, "/dm")
	if url == "" {
		logrus.Debugf("📬 Cannot DM %s: not reachable via mesh", targetName)
		return false
	}

	// Encode the event
	eventBytes, err := json.Marshal(event)
	if err != nil {
		logrus.Warnf("📬 Failed to encode DM for %s: %v", targetName, err)
		return false
	}

	ctx, cancel := context.WithTimeout(network.ctx, 15*time.Second)
	defer cancel()

	// Create request with auth headers
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(eventBytes))
	if err != nil {
		logrus.Warnf("📬 Failed to create DM request for %s: %v", targetName, err)
		return false
	}
	req.Header.Set("Content-Type", "application/json")

	// Add mesh authentication headers (Ed25519 signature)
	network.AddMeshAuthHeaders(req)

	client := network.getMeshHTTPClient()

	resp, err := client.Do(req)
	if err != nil {
		logrus.Debugf("📬 Failed to send DM to %s: %v", targetName, err)
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}
