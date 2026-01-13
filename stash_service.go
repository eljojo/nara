package nara

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

// =============================================================================
// StashService - Unified stash business logic layer
// =============================================================================

// StashServiceDeps extends NetworkContext with stash-specific functionality.
// This interface allows the service to be decoupled from Network while still
// accessing necessary functionality.
type StashServiceDeps interface {
	NetworkContext

	// Stash-specific: emit social events for stash operations
	EmitSocialEvent(event SyncEvent)
}

// StashService encapsulates all stash business logic for both owner and confidant roles.
// It provides a clean API for HTTP handlers and network operations to use.
//
// Architecture:
//
//	StashService (orchestration layer - all business logic)
//	├── StashManager        (owner side: MY stash → distributed to confidants)
//	├── ConfidantStashStore (confidant side: OTHERS' stashes → stored by me)
//	└── StashSyncTracker    (rate limiting sync operations)
//
// - StashManager: Manages YOUR OWN stash data and tracks which confidants hold copies.
// - ConfidantStashStore: Stores OTHER naras' stashes when you act as their confidant.
// - StashService: Wraps both and provides all operations (accept, retrieve, distribute, etc.)
type StashService struct {
	// Owner-side: manages OUR stash that we distribute to confidants
	manager *StashManager

	// Confidant-side: stores OTHER naras' stashes (we're their confidant)
	confidantStore *ConfidantStashStore

	// Rate limiting for stash sync operations
	syncTracker *StashSyncTracker

	// External dependencies
	deps StashServiceDeps
}

// NewStashService creates a new StashService with the given components.
// Pass nil for components that aren't needed (e.g., nil manager if not using stash).
func NewStashService(
	manager *StashManager,
	confidantStore *ConfidantStashStore,
	syncTracker *StashSyncTracker,
	deps StashServiceDeps,
) *StashService {
	return &StashService{
		manager:        manager,
		confidantStore: confidantStore,
		syncTracker:    syncTracker,
		deps:           deps,
	}
}

// =============================================================================
// Confidant-side operations (storing stashes for others)
// =============================================================================

// AcceptStash handles an incoming stash store request from an owner.
// Returns whether the stash was accepted and a reason if rejected.
func (s *StashService) AcceptStash(owner string, stash *StashPayload) (accepted bool, reason string) {
	if s.confidantStore == nil {
		return false, "stash_disabled"
	}

	if stash == nil {
		return false, "stash_required"
	}

	if stash.Size() > MaxStashSize {
		logrus.Warnf("📦 Rejected stash from %s: too large (%d bytes)", owner, stash.Size())
		return false, "stash_too_large"
	}

	// Try to store (returns false if at capacity)
	if !s.confidantStore.Store(owner, stash) {
		metrics := s.confidantStore.GetMetrics()
		logrus.Warnf("📦 Rejected stash from %s: at capacity (%d/%d)",
			owner, metrics.StashesStored, s.confidantStore.maxStashes)
		return false, "at_capacity"
	}

	logrus.Infof("📦 Accepted stash for %s (%d bytes)", owner, stash.Size())

	// Emit social event for storing stash
	if s.deps != nil {
		socialEvent := SocialEventPayload{
			Type:    "service",
			Actor:   s.deps.MyName(),
			Target:  owner,
			Reason:  ReasonStashStored,
			Witness: s.deps.MyName(),
		}
		syncEvent := SyncEvent{
			Service:   ServiceSocial,
			Timestamp: time.Now().UnixNano(),
			Emitter:   s.deps.MyName(),
			Social:    &socialEvent,
		}
		s.deps.EmitSocialEvent(syncEvent)
	}

	return true, ""
}

// RetrieveStashFor returns the stored stash for the given owner, or nil if not found.
func (s *StashService) RetrieveStashFor(owner string) *StashPayload {
	if s.confidantStore == nil {
		return nil
	}

	s.confidantStore.mu.RLock()
	payload := s.confidantStore.stashes[owner]
	s.confidantStore.mu.RUnlock()

	if payload != nil {
		logrus.Infof("📦 Sent stash to %s (%d bytes)", owner, payload.Size())
	}

	return payload
}

// DeleteStashFor removes the stored stash for the given owner.
// Returns true if a stash was deleted, false if none existed.
func (s *StashService) DeleteStashFor(owner string) bool {
	if s.confidantStore == nil {
		return false
	}

	deleted := s.confidantStore.Delete(owner)
	if deleted {
		logrus.Infof("📦 Deleted stash for %s", owner)
	}

	return deleted
}

// HasStashFor returns true if we have a stash stored for the given owner.
func (s *StashService) HasStashFor(owner string) bool {
	if s.confidantStore == nil {
		return false
	}
	return s.confidantStore.HasStashFor(owner)
}

// AcceptPushedStash handles receiving a stash push during recovery.
// It decrypts the stash, stores it locally, and marks the sender as a confidant.
func (s *StashService) AcceptPushedStash(sender string, stash *StashPayload) (accepted bool, reason string) {
	if s.manager == nil {
		return false, "stash_disabled"
	}

	if stash == nil {
		return false, "stash_required"
	}

	// Decrypt the stash
	encKeypair := DeriveEncryptionKeys(s.deps.Keypair().PrivateKey)
	stashData, err := DecryptStashPayload(stash, encKeypair)
	if err != nil {
		logrus.Warnf("📦 Failed to decrypt pushed stash from %s: %v", sender, err)
		return false, "decrypt_failed"
	}

	// Store recovered stash
	s.manager.SetCurrentStash(stashData.Data)
	logrus.Infof("📦 Recovered stash from %s via push (%d bytes, timestamp=%d)",
		sender, len(stashData.Data), stashData.Timestamp)

	// Mark them as a confidant
	if s.manager.confidantTracker != nil {
		s.manager.confidantTracker.Add(sender, time.Now().Unix())
	}

	return true, ""
}

// =============================================================================
// Owner-side operations (managing our stash distribution)
// =============================================================================

// SetCurrentStash updates the owner's current stash data.
func (s *StashService) SetCurrentStash(data json.RawMessage) {
	if s.manager != nil {
		s.manager.SetCurrentStash(data)
	}
}

// GetCurrentStash returns the owner's current stash data.
func (s *StashService) GetCurrentStash() *StashData {
	if s.manager == nil {
		return nil
	}
	return s.manager.GetCurrentStash()
}

// HasStashData returns true if the owner has stash data to distribute.
func (s *StashService) HasStashData() bool {
	return s.manager != nil && s.manager.HasStashData()
}

// GetConfidants returns the list of confirmed confidant names.
func (s *StashService) GetConfidants() []string {
	if s.manager == nil || s.manager.confidantTracker == nil {
		return nil
	}
	return s.manager.confidantTracker.GetAll()
}

// ConfidantCount returns the number of confirmed confidants.
func (s *StashService) ConfidantCount() int {
	if s.manager == nil || s.manager.confidantTracker == nil {
		return 0
	}
	return s.manager.confidantTracker.Count()
}

// TargetConfidantCount returns the target number of confidants.
func (s *StashService) TargetConfidantCount() int {
	if s.manager == nil || s.manager.confidantTracker == nil {
		return 0
	}
	return s.manager.confidantTracker.targetCount
}

// MarkConfidantConfirmed marks a confidant as having confirmed storage of our stash.
func (s *StashService) MarkConfidantConfirmed(name string, timestamp int64) {
	if s.manager != nil && s.manager.confidantTracker != nil {
		s.manager.confidantTracker.Add(name, timestamp)
	}
}

// MarkConfidantFailed marks a confidant as having failed (timeout, rejection, etc).
func (s *StashService) MarkConfidantFailed(name string) {
	if s.manager != nil && s.manager.confidantTracker != nil {
		s.manager.confidantTracker.MarkFailed(name)
	}
}

// =============================================================================
// Metrics and status
// =============================================================================

// GetStorageMetrics returns metrics about stash storage (confidant role).
func (s *StashService) GetStorageMetrics() StashMetrics {
	if s.confidantStore == nil {
		return StashMetrics{}
	}
	return s.confidantStore.GetMetrics()
}

// GetAllStoredOwners returns the list of owners we're storing stashes for.
func (s *StashService) GetAllStoredOwners() []string {
	if s.confidantStore == nil {
		return nil
	}
	return s.confidantStore.GetAllOwners()
}

// =============================================================================
// Owner-side HTTP operations (distributing stash to confidants)
// =============================================================================

// ExchangeStashWithPeer performs bidirectional stash exchange with a peer.
// - If we have stash data and need storage, we store with them
// - If we don't have stash data, we try to retrieve from them
func (s *StashService) ExchangeStashWithPeer(targetName string) {
	// Check rate limiting - don't sync more than once per 5 minutes
	if s.syncTracker != nil && !s.syncTracker.ShouldSync(targetName, 5*time.Minute) {
		return
	}

	if s.manager == nil || s.manager.confidantTracker == nil {
		return
	}

	// First, try to store our stash with them if needed
	needsStorage := s.manager.confidantTracker.Has(targetName) || s.manager.confidantTracker.NeedsMore()
	if needsStorage && s.manager.HasStashData() && !s.manager.confidantTracker.IsFailed(targetName) {
		s.StoreStashWithPeer(targetName)
	}

	// Second, try to retrieve our stash if we don't have it
	if !s.manager.HasStashData() {
		s.RetrieveStashFromPeer(targetName)
	}

	// Mark sync timestamp
	if s.syncTracker != nil {
		s.syncTracker.MarkSyncNow(targetName)
	}
}

// StoreStashWithPeer sends our stash to a peer for storage.
func (s *StashService) StoreStashWithPeer(targetName string) {
	if s.manager == nil || s.deps == nil {
		return
	}

	// Determine base URL
	baseURL := s.deps.BuildMeshURL(targetName, "")
	if baseURL == "" {
		return
	}

	currentStash := s.manager.GetCurrentStash()
	if currentStash == nil {
		return
	}

	encKeypair := DeriveEncryptionKeys(s.deps.Keypair().PrivateKey)
	payload, err := CreateStashPayload(s.deps.MyName(), currentStash, encKeypair)
	if err != nil {
		logrus.Debugf("📦 Failed to create stash payload: %v", err)
		return
	}

	// Build request (auth is in mesh headers, not body)
	req := StashStoreRequest{
		Stash: payload,
	}

	reqBytes, _ := json.Marshal(req)
	ctx, cancel := context.WithTimeout(s.deps.Context(), 10*time.Second)
	defer cancel()

	httpReq, _ := http.NewRequestWithContext(ctx, "POST", baseURL+"/stash/store", bytes.NewBuffer(reqBytes))
	httpReq.Header.Set("Content-Type", "application/json")
	s.deps.AddMeshAuthHeaders(httpReq)

	client := s.deps.GetMeshHTTPClient()
	if client == nil {
		return
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		logrus.Debugf("📦 Failed to store stash with %s: %v", targetName, err)
		s.manager.confidantTracker.MarkFailed(targetName)
		return
	}
	defer resp.Body.Close()

	var response StashStoreResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err == nil && response.Accepted {
		s.manager.confidantTracker.Add(targetName, time.Now().Unix())
		logrus.Infof("📦 %s accepted stash", targetName)
	} else if !response.Accepted {
		logrus.Warnf("📦 %s rejected stash: %s", targetName, response.Reason)
		s.manager.confidantTracker.MarkFailed(targetName)
	}
}

// RetrieveStashFromPeer requests our stash back from a peer.
func (s *StashService) RetrieveStashFromPeer(targetName string) {
	if s.manager == nil || s.deps == nil {
		return
	}

	// Determine base URL
	baseURL := s.deps.BuildMeshURL(targetName, "")
	if baseURL == "" {
		return
	}

	// Auth is in mesh headers, body can be empty
	ctx, cancel := context.WithTimeout(s.deps.Context(), 10*time.Second)
	defer cancel()

	httpReq, _ := http.NewRequestWithContext(ctx, "POST", baseURL+"/stash/retrieve", nil)
	httpReq.Header.Set("Content-Type", "application/json")
	s.deps.AddMeshAuthHeaders(httpReq)

	client := s.deps.GetMeshHTTPClient()
	if client == nil {
		return
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		logrus.Debugf("📦 Failed to retrieve stash from %s: %v", targetName, err)
		return
	}
	defer resp.Body.Close()

	var response StashRetrieveResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err == nil && response.Found {
		encKeypair := DeriveEncryptionKeys(s.deps.Keypair().PrivateKey)
		stashData, err := DecryptStashPayload(response.Stash, encKeypair)
		if err == nil {
			s.manager.SetCurrentStash(stashData.Data)
			s.manager.confidantTracker.Add(targetName, time.Now().Unix())
			logrus.Infof("📦 Recovered stash from %s (%d bytes)", targetName, len(stashData.Data))
		}
	}
}

// PushStashToOwner pushes a stash back to its owner via HTTP POST to /stash/push.
// This is called during hey-there recovery or stash-refresh requests.
// Runs the push in a background goroutine with optional delay.
func (s *StashService) PushStashToOwner(targetName string, delay time.Duration) {
	go func() {
		// Optional delay to let them finish booting or avoid thundering herd
		if delay > 0 {
			time.Sleep(delay)
		}

		// Check if we still have their stash
		if s.confidantStore == nil || s.deps == nil {
			return
		}

		s.confidantStore.mu.RLock()
		theirStash := s.confidantStore.stashes[targetName]
		s.confidantStore.mu.RUnlock()

		if theirStash == nil {
			logrus.Debugf("📦 No stash found for %s during push", targetName)
			return
		}

		// Build push request (auth is in mesh headers, not body)
		req := StashPushRequest{
			To:    targetName,
			Stash: theirStash,
		}

		// Determine URL
		url := s.deps.BuildMeshURL(targetName, "/stash/push")
		if url == "" {
			logrus.Debugf("📦 Cannot push stash to %s: not reachable via mesh", targetName)
			return
		}

		// Send push request
		reqBytes, _ := json.Marshal(req)
		ctx, cancel := context.WithTimeout(s.deps.Context(), 10*time.Second)
		defer cancel()

		httpReq, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBytes))
		httpReq.Header.Set("Content-Type", "application/json")
		s.deps.AddMeshAuthHeaders(httpReq)

		client := s.deps.GetMeshHTTPClient()
		if client == nil {
			return
		}

		resp, err := client.Do(httpReq)
		if err != nil {
			logrus.Debugf("📦 Failed to push stash to %s: %v", targetName, err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			logrus.Infof("📦 Pushed stash to %s (recovery)", targetName)
		}
	}()
}

// =============================================================================
// Maintenance operations
// =============================================================================

// MaintainConfidants ensures we have the target number of confidants for our stash.
// It cleans up expired pending/failed entries and actively searches for new confidants if needed.
func (s *StashService) MaintainConfidants() {
	if s.manager == nil || s.deps == nil || s.deps.IsReadOnly() {
		return
	}

	// Only maintain confidants if we have data to store
	if !s.manager.HasStashData() {
		return
	}

	tracker := s.manager.confidantTracker

	// Cleanup expired pending (didn't ack in time - probably old version or offline)
	expired := tracker.CleanupExpiredPending()
	for _, name := range expired {
		logrus.Debugf("📦 %s didn't ack stash (timeout), removed from pending", name)
	}

	// Cleanup expired failures (allow retrying after backoff period)
	recovered := tracker.CleanupExpiredFailures()
	for _, name := range recovered {
		logrus.Debugf("📦 %s failure backoff expired, can retry", name)
	}

	// If we have stash data but less than target confidants, actively try to find more
	if tracker.NeedsMore() {
		needed := tracker.NeedsCount()
		logrus.Debugf("📦 Need %d more confidants (have %d/%d), searching for candidates",
			needed, tracker.Count(), tracker.targetCount)

		// Get online naras
		online := s.deps.GetOnlineMeshPeers()

		// Filter to mesh-enabled only
		var meshEnabled []string
		for _, name := range online {
			if s.deps.HasMeshConnectivity(name) {
				meshEnabled = append(meshEnabled, name)
			}
		}

		if len(meshEnabled) == 0 {
			return
		}

		// Get peer info for selection
		peers := s.deps.GetPeerInfo(meshEnabled)
		if len(peers) == 0 {
			return
		}

		// Try to find and store with confidants
		// Strategy: First confidant uses best score (reliable high-memory nara)
		//           Remaining confidants are random (better distribution, avoid hotspots)
		// Keep trying peers until we fill all slots OR run out of candidates
		attemptCount := 0
		for tracker.NeedsMore() && len(peers) > 0 && attemptCount < 20 {
			attemptCount++
			var selectedName string
			var selectionMethod string

			currentCount := tracker.Count()
			if currentCount == 0 {
				// First confidant: pick the best (highest memory + uptime)
				selectedName = tracker.SelectBest(s.deps.MyName(), peers)
				selectionMethod = "best"
			} else {
				// Remaining confidants: pick randomly (distribute load)
				selectedName = tracker.SelectRandomFromPeers(s.deps.MyName(), peers)
				selectionMethod = "random"
			}

			if selectedName == "" {
				break // No more candidates
			}

			logrus.Debugf("📦 Selected %s as confidant candidate (%s, attempt %d)", selectedName, selectionMethod, attemptCount)

			// Try to store stash with this peer
			// StoreStashWithPeer handles marking as pending/failed
			s.StoreStashWithPeer(selectedName)

			// Remove this peer from consideration (avoid retrying in same round)
			newPeers := make([]PeerInfo, 0, len(peers)-1)
			for _, p := range peers {
				if p.Name != selectedName {
					newPeers = append(newPeers, p)
				}
			}
			peers = newPeers
		}
	}
}

// CheckConfidantHealth monitors the health of our confidants and removes offline ones.
// Replacements will be found by MaintainConfidants in the next tick.
func (s *StashService) CheckConfidantHealth() {
	if s.manager == nil || s.manager.confidantTracker == nil || s.deps == nil {
		return
	}

	// Get current confirmed confidants
	confidants := s.manager.confidantTracker.GetAll()
	if len(confidants) == 0 {
		return
	}

	// Ensure projection is up-to-date before checking statuses
	s.deps.EnsureProjectionsUpdated()

	// Check each confidant's status
	for _, confidantName := range confidants {
		state := s.deps.GetOnlineStatus(confidantName)
		if state == nil {
			// Never seen - remove them
			s.manager.confidantTracker.Remove(confidantName)
			logrus.Warnf("📦 Confidant %s removed (never seen)", confidantName)
			continue
		}

		// If offline or missing, remove them
		if state.Status == "OFFLINE" || state.Status == "MISSING" {
			s.manager.confidantTracker.Remove(confidantName)
			logrus.Warnf("📦 Confidant %s went offline, will find replacement", confidantName)
		}
	}

	// MaintainConfidants() will automatically find replacements in the next tick
}

// ReactToConfidantOffline is called immediately when we detect a nara went offline.
// If they're one of our confidants, remove them and trigger immediate replacement search.
func (s *StashService) ReactToConfidantOffline(name string) {
	if s.manager == nil || s.manager.confidantTracker == nil {
		return
	}

	// Check if this nara is one of our confidants
	if !s.manager.confidantTracker.Has(name) {
		return
	}

	// Remove them immediately
	s.manager.confidantTracker.Remove(name)
	logrus.Warnf("📦 Confidant %s went offline (reactive), finding replacement immediately", name)

	// Trigger immediate replacement search
	go s.MaintainConfidants()
}

// PruneGhostStashes removes stashes of owners who have been offline for 7+ days.
func (s *StashService) PruneGhostStashes() {
	if s.confidantStore == nil || s.deps == nil {
		return
	}

	// Check all stash owners
	owners := s.confidantStore.GetAllOwners()
	if len(owners) == 0 {
		return
	}

	// Ensure projection is up-to-date before checking statuses
	s.deps.EnsureProjectionsUpdated()

	isGhost := func(name string) bool {
		// Check online status projection
		state := s.deps.GetOnlineStatus(name)
		if state == nil {
			// No state = never seen = ghost
			return true
		}

		// If online, definitely not a ghost
		if state.Status == "ONLINE" {
			return false
		}

		// If offline, check how long
		lastSeen := state.LastEventTime
		elapsedNanos := time.Now().UnixNano() - lastSeen
		elapsed := time.Duration(elapsedNanos)

		// Ghost if offline for more than 7 days
		return elapsed > 7*24*time.Hour
	}

	evicted := s.confidantStore.EvictGhosts(isGhost)
	if evicted > 0 {
		logrus.Infof("📦 Pruned %d ghost stashes (offline 7+ days)", evicted)
	}
}

// =============================================================================
// Maintenance loop
// =============================================================================

// RunMaintenanceLoop runs the stash maintenance loop.
// It should be called as a goroutine. The loop handles:
// - Immediate stash distribution when triggered
// - Periodic confidant health checks
// - Periodic ghost stash pruning
// - Periodic metrics logging
func (s *StashService) RunMaintenanceLoop(trigger <-chan struct{}) {
	if s.deps == nil {
		return
	}

	// Prune ghost stashes on boot (naras offline >7 days)
	s.PruneGhostStashes()

	// Run every 5 minutes
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-trigger:
			// Immediate stash distribution triggered (e.g., stash data updated)
			logrus.Debugf("📦 Triggered immediate stash distribution")
			s.MaintainConfidants()
		case <-ticker.C:
			// Periodic maintenance
			// Check health of our confidants and find replacements if needed
			s.CheckConfidantHealth()

			// Maintain target number of confidants
			s.MaintainConfidants()

			// Prune ghost stashes periodically
			s.PruneGhostStashes()

			// Log stash metrics every 5 minutes
			s.LogMetrics()
		case <-s.deps.Context().Done():
			return
		}
	}
}

// LogMetrics logs stash storage metrics.
func (s *StashService) LogMetrics() {
	if s.confidantStore == nil {
		return
	}

	metrics := s.confidantStore.GetMetrics()
	myConfidants := s.ConfidantCount()
	targetConfidants := s.TargetConfidantCount()

	// Calculate my stash size from actual stash data
	var myStashSize int
	if s.manager != nil {
		currentStash := s.manager.GetCurrentStash()
		if currentStash != nil {
			myStashSize = len(currentStash.Data)
		}
	}

	logrus.Infof("📦 Stash metrics: stored=%d (%.1fMB), my_confidants=%d/%d, my_size=%.1fKB",
		metrics.StashesStored,
		float64(metrics.TotalStashBytes)/1024/1024,
		myConfidants,
		targetConfidants,
		float64(myStashSize)/1024)
}
