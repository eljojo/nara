package nara

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

// checkpointMaintenance periodically checks for subjects needing checkpoints
// and coordinates multi-party attestation with other high-uptime naras.
//
// Runs every ~15 minutes (±3min jitter) but only acts if:
// 1. USE_CHECKPOINT_CREATION=true
// 2. This nara is a high-uptime attester
// 3. There are subjects with backfill but no checkpoint
func (network *Network) checkpointMaintenance() {
	// Initial random delay (0-5 minutes) to spread startup load
	initialDelay := time.Duration(rand.Intn(5)) * time.Minute
	select {
	case <-time.After(initialDelay):
	case <-network.ctx.Done():
		return
	}

	// Main maintenance loop: every 15 minutes ±3min jitter
	for {
		jitter := time.Duration(rand.Intn(6)-3) * time.Minute
		interval := 15*time.Minute + jitter

		select {
		case <-time.After(interval):
			network.runCheckpointMaintenance()
		case <-network.ctx.Done():
			logrus.Debugf("checkpointMaintenance: shutting down gracefully")
			return
		}
	}
}

// runCheckpointMaintenance performs one round of checkpoint maintenance
func (network *Network) runCheckpointMaintenance() {
	// Check if checkpoint creation is enabled
	if !useCheckpointCreation() {
		return
	}

	// Check if we qualify as a high-uptime attester
	myObs := network.local.getMeObservation()
	if !IsHighUptime(myObs, DefaultMinCheckpointUptime) {
		logrus.Debugf("📋 Checkpoint maintenance: skipping (insufficient uptime)")
		return
	}

	// Find subjects needing checkpoints
	subjects := network.local.SyncLedger.GetSubjectsNeedingCheckpoint()
	if len(subjects) == 0 {
		logrus.Debugf("📋 Checkpoint maintenance: no subjects need checkpoints")
		return
	}

	logrus.Printf("📋 Checkpoint maintenance: %d subjects need checkpoints", len(subjects))

	// Process at most 3 subjects per maintenance run to spread work
	maxSubjects := 3
	if len(subjects) < maxSubjects {
		maxSubjects = len(subjects)
	}

	// Shuffle to avoid all naras trying to checkpoint the same subjects
	rand.Shuffle(len(subjects), func(i, j int) {
		subjects[i], subjects[j] = subjects[j], subjects[i]
	})

	for _, subject := range subjects[:maxSubjects] {
		if err := network.createCheckpointForSubject(subject); err != nil {
			logrus.Printf("📋 Failed to create checkpoint for %s: %v", subject, err)
		}
	}
}

// createCheckpointForSubject attempts to create a multi-signed checkpoint for a subject
func (network *Network) createCheckpointForSubject(subject string) error {
	// Prepare the proposal from our local data
	proposal := PrepareCheckpointProposal(network.local.SyncLedger, subject)
	if proposal == nil {
		logrus.Debugf("📋 No proposal data for %s", subject)
		return nil
	}

	logrus.Printf("📋 Proposing checkpoint for %s: restarts=%d, first_seen=%d",
		subject, proposal.Restarts, proposal.FirstSeen)

	// Sign it ourselves first
	mySignature := SignCheckpointProposal(proposal, network.local.Keypair)
	proposal.Attesters = append(proposal.Attesters, network.meName())
	proposal.Signatures = append(proposal.Signatures, mySignature)

	// Find other high-uptime naras to request signatures from
	observations := network.gatherObservations()
	highUptimeNaras := GetHighUptimeNaras(observations, DefaultCheckpointTopPercentile)

	// Remove ourselves from the list
	var candidates []string
	for _, name := range highUptimeNaras {
		if name != network.meName() {
			candidates = append(candidates, name)
		}
	}

	if len(candidates) == 0 {
		logrus.Debugf("📋 No other high-uptime naras to attest checkpoint for %s", subject)
		return nil
	}

	// Request signatures from candidates
	minAttesters := getMinCheckpointAttesters()
	maxRequests := minAttesters + 2 // Request from a few extra in case some decline

	if maxRequests > len(candidates) {
		maxRequests = len(candidates)
	}

	// Shuffle candidates for randomness
	rand.Shuffle(len(candidates), func(i, j int) {
		candidates[i], candidates[j] = candidates[j], candidates[i]
	})

	for _, candidate := range candidates[:maxRequests] {
		if len(proposal.Attesters) >= minAttesters {
			break // We have enough signatures
		}

		sig, ok := network.requestCheckpointSignature(candidate, proposal)
		if ok && sig != "" {
			proposal.Attesters = append(proposal.Attesters, candidate)
			proposal.Signatures = append(proposal.Signatures, sig)
		}
	}

	// Check if we got enough signatures
	if len(proposal.Attesters) < minAttesters {
		logrus.Printf("📋 Checkpoint for %s: insufficient attesters (%d/%d)",
			subject, len(proposal.Attesters), minAttesters)
		return nil
	}

	// Create and store the checkpoint event
	event := NewCheckpointEvent(subject, proposal.AsOfTime, proposal.FirstSeen, proposal.Restarts, proposal.TotalUptime)
	event.Checkpoint.Attesters = proposal.Attesters
	event.Checkpoint.Signatures = proposal.Signatures

	if network.local.SyncLedger.AddEvent(event) {
		logrus.Printf("📋 Created checkpoint for %s: restarts=%d, attesters=%v",
			subject, proposal.Restarts, proposal.Attesters)
	}

	return nil
}

// requestCheckpointSignature requests a signature from another high-uptime nara
func (network *Network) requestCheckpointSignature(name string, proposal *CheckpointEventPayload) (string, bool) {
	// Get mesh IP
	network.local.mu.Lock()
	nara, exists := network.Neighbourhood[name]
	network.local.mu.Unlock()

	if !exists {
		return "", false
	}

	nara.mu.Lock()
	meshIP := nara.Status.MeshIP
	nara.mu.Unlock()

	if meshIP == "" {
		return "", false
	}

	// Build request
	req := CheckpointSignatureRequest{
		Proposal:  proposal,
		Requester: network.meName(),
	}

	jsonBody, err := json.Marshal(req)
	if err != nil {
		logrus.Warnf("📋 Failed to marshal checkpoint request: %v", err)
		return "", false
	}

	// Make HTTP request
	url := fmt.Sprintf("http://%s:%d/checkpoint/sign", meshIP, DefaultMeshPort)

	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		logrus.Warnf("📋 Failed to create checkpoint request: %v", err)
		return "", false
	}
	httpReq.Header.Set("Content-Type", "application/json")
	network.AddMeshAuthHeaders(httpReq)

	// Use tsnet HTTP client
	if network.tsnetMesh == nil {
		return "", false
	}
	client := network.tsnetMesh.Server().HTTPClient()
	client.Timeout = 30 * time.Second

	resp, err := client.Do(httpReq)
	if err != nil {
		logrus.Debugf("📋 Checkpoint signature request to %s failed: %v", name, err)
		return "", false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logrus.Debugf("📋 Checkpoint signature request to %s returned status %d", name, resp.StatusCode)
		return "", false
	}

	var sigResp CheckpointSignatureResponse
	if err := json.NewDecoder(resp.Body).Decode(&sigResp); err != nil {
		logrus.Warnf("📋 Failed to decode checkpoint response from %s: %v", name, err)
		return "", false
	}

	if !sigResp.Approved {
		logrus.Debugf("📋 %s declined checkpoint signature: %s", name, sigResp.Reason)
		return "", false
	}

	logrus.Debugf("📋 Received checkpoint signature from %s", name)
	return sigResp.Signature, true
}

// gatherObservations collects observation data for all naras we know about
// Used to determine who qualifies as high-uptime
func (network *Network) gatherObservations() map[string]NaraObservation {
	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	observations := make(map[string]NaraObservation)

	for name, nara := range network.Neighbourhood {
		obs := nara.getObservation(name)
		observations[name] = obs
	}

	// Include ourselves
	myObs := network.local.getMeObservation()
	observations[network.meName()] = myObs

	return observations
}
