package nara

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"math/rand"
	"sort"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"

	"github.com/eljojo/nara/types"
)

// Checkpoint MQTT topics
const (
	TopicCheckpointPropose = "nara/checkpoint/propose"
	TopicCheckpointVote    = "nara/checkpoint/vote"
	TopicCheckpointFinal   = "nara/checkpoint/final" // Finalized checkpoint for everyone to store
)

// Checkpoint timing constants
const (
	DefaultCheckpointInterval = 24 * time.Hour
	DefaultVoteWindow         = 1 * time.Minute
	MinVotersRequired         = 5  // Outside the proposer, so 6 total signatures
	MaxCheckpointSignatures   = 10 // Store at most 10 signatures, prioritizing high-uptime naras
	CheckpointCheckInterval   = 15 * time.Minute
)

// CheckpointProposal is broadcast when a nara proposes a checkpoint about itself
// It's a self-attestation (attester == subject) plus round context
type CheckpointProposal struct {
	Attestation     // Embedded attestation (subject, observation, signature)
	Round       int `json:"round"` // Consensus round (1 or 2)
}

// CheckpointVote is a response to a proposal
// It's a third-party attestation (attester != subject) plus vote context
type CheckpointVote struct {
	Attestation       // Embedded attestation (subject, observation, voter as attester, signature)
	ProposalTS  int64 `json:"proposal_ts"` // Which proposal this vote is for
	Round       int   `json:"round"`       // Consensus round (1 or 2)
	Approved    bool  `json:"approved"`    // true = agree with proposal values
}

// pendingProposal tracks an in-flight checkpoint proposal awaiting votes
type pendingProposal struct {
	proposal    *CheckpointProposal
	votes       []*CheckpointVote
	votesMu     sync.Mutex
	expiresAt   time.Time
	round       int
	finalized   bool
	finalizedMu sync.Mutex
}

// CheckpointService manages checkpoint creation via MQTT consensus
type CheckpointService struct {
	network    *Network
	ledger     *SyncLedger
	local      *LocalNara
	mqttClient mqtt.Client

	// Pending proposals we initiated (we are the subject)
	myPendingProposal   *pendingProposal
	myPendingProposalMu sync.Mutex

	// Pending proposals from others that we're voting on
	pendingProposals map[string]*pendingProposal // keyed by "subject:proposedAt"

	// Last successful checkpoint time for ourselves
	lastCheckpointTime   time.Time
	lastCheckpointTimeMu sync.RWMutex

	// Configuration
	checkpointInterval time.Duration
	voteWindow         time.Duration

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
}

// NewCheckpointService creates a new CheckpointService
func NewCheckpointService(network *Network, ledger *SyncLedger, local *LocalNara) *CheckpointService {
	ctx, cancel := context.WithCancel(context.Background())
	return &CheckpointService{
		network:            network,
		ledger:             ledger,
		local:              local,
		pendingProposals:   make(map[string]*pendingProposal),
		checkpointInterval: DefaultCheckpointInterval,
		voteWindow:         DefaultVoteWindow,
		ctx:                ctx,
		cancel:             cancel,
	}
}

// SetMQTTClient sets the MQTT client (called after MQTT connects)
func (s *CheckpointService) SetMQTTClient(client mqtt.Client) {
	s.mqttClient = client
}

// Start begins the checkpoint service maintenance loop
func (s *CheckpointService) Start() {
	// Initialize lastCheckpointTime from ledger
	// Boot recovery populates the ledger with checkpoints from neighbors,
	// so we can query it instead of needing to persist to stash
	s.initializeLastCheckpointTime()

	go s.maintenanceLoop()
}

// initializeLastCheckpointTime queries the ledger for the most recent checkpoint
// and sets lastCheckpointTime accordingly. This prevents checkpoint spam on boot.
func (s *CheckpointService) initializeLastCheckpointTime() {
	if s.ledger == nil {
		logrus.Debug("checkpoint: ledger not initialized, cannot query for last checkpoint")
		return
	}

	myName := s.local.Me.Name
	event := s.ledger.GetCheckpointEvent(myName)

	s.lastCheckpointTimeMu.Lock()
	defer s.lastCheckpointTimeMu.Unlock()

	if event != nil && event.Checkpoint != nil {
		// Found a checkpoint - use its AsOfTime as our last checkpoint time
		s.lastCheckpointTime = time.Unix(event.Checkpoint.AsOfTime, 0)
		logrus.Infof("checkpoint: initialized from ledger - last checkpoint was at %s", s.lastCheckpointTime.Format(time.RFC3339))
	} else {
		// No checkpoint in ledger - this is our first boot or no checkpoint exists yet
		// Set to now so we don't immediately propose (wait 24h from boot)
		s.lastCheckpointTime = time.Now()
		logrus.Debug("checkpoint: no previous checkpoint found in ledger, will wait 24h before proposing")
	}
}

// Stop gracefully shuts down the checkpoint service
func (s *CheckpointService) Stop() {
	s.cancel()
}

// maintenanceLoop checks periodically if we need to propose a checkpoint
func (s *CheckpointService) maintenanceLoop() {
	// Check every 15 minutes if we need to propose
	ticker := time.NewTicker(CheckpointCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.checkAndPropose()
		case <-s.ctx.Done():
			return
		}
	}
}

// checkAndPropose checks if 24h have passed and proposes a checkpoint if needed
func (s *CheckpointService) checkAndPropose() {
	// Don't propose until boot recovery is complete - we need accurate derived values
	if !s.network.IsBootRecoveryComplete() {
		logrus.Debug("checkpoint: skipping proposal - boot recovery not complete, opinions still forming")
		return
	}

	s.lastCheckpointTimeMu.RLock()
	lastCheckpoint := s.lastCheckpointTime
	s.lastCheckpointTimeMu.RUnlock()

	// Check if 24h have passed since last checkpoint
	if time.Since(lastCheckpoint) < s.checkpointInterval {
		return
	}

	// Check if we already have a pending proposal
	s.myPendingProposalMu.Lock()
	if s.myPendingProposal != nil && !s.myPendingProposal.finalized {
		s.myPendingProposalMu.Unlock()
		return
	}
	s.myPendingProposalMu.Unlock()

	// Check if there are enough naras online to potentially reach consensus
	// Need MinVotersRequired voters + ourselves, so check for MinVotersRequired + 1
	onlineCount := len(s.network.NeighbourhoodOnlineNames())
	if onlineCount < MinVotersRequired+1 {
		logrus.Debugf("checkpoint: skipping proposal - not enough naras online (%d < %d required)", onlineCount, MinVotersRequired+1)
		return
	}

	// Propose a new checkpoint
	s.ProposeCheckpoint()
}

// ProposeCheckpoint broadcasts a checkpoint proposal about ourselves
func (s *CheckpointService) ProposeCheckpoint() {
	s.proposeCheckpointRound(1, nil)
}

// round2Values holds consensus values computed from round 1 votes
type round2Values struct {
	restarts    int64
	totalUptime int64
	firstSeen   int64
}

// proposeCheckpointRound broadcasts a checkpoint proposal for a specific round
// For round 2, consensusValues should contain the trimmed mean from round 1 votes
func (s *CheckpointService) proposeCheckpointRound(round int, consensusValues *round2Values) {
	if s.mqttClient == nil || !s.mqttClient.IsConnected() {
		logrus.Debug("checkpoint: MQTT not connected, skipping proposal")
		return
	}

	if s.ledger == nil {
		logrus.Warn("checkpoint: ledger not initialized, skipping proposal")
		return
	}

	myName := s.local.Me.Name
	myID := s.local.Me.Status.ID

	var restarts, totalUptime, firstSeen int64
	if round == 2 && consensusValues != nil {
		// Round 2: use consensus values from round 1 votes
		restarts = consensusValues.restarts
		totalUptime = consensusValues.totalUptime
		firstSeen = consensusValues.firstSeen
	} else {
		// Round 1: derive our current values from ledger
		restarts = s.ledger.DeriveRestartCount(myName)
		totalUptime = s.ledger.DeriveTotalUptime(myName)
		firstSeen = s.ledger.GetFirstSeenFromEvents(myName)
	}

	now := time.Now().Unix()

	// v2: Get reference point - the last checkpoint we've seen for ourselves
	lastSeenCheckpointID := s.ledger.GetLatestCheckpointID(myName)

	// Create attestation (self-attestation: I claim this about myself)
	attestation := Attestation{
		Version:   2, // v2: includes reference point
		Subject:   myName,
		SubjectID: myID,
		Observation: NaraObservation{
			Restarts:    restarts,
			TotalUptime: totalUptime,
			StartTime:   firstSeen,
		},
		Attester:             myName, // Same as Subject for self-attestation
		AttesterID:           myID,   // Same as SubjectID for self-attestation
		AsOfTime:             now,
		LastSeenCheckpointID: lastSeenCheckpointID, // v2: reference point
	}

	// Sign the attestation
	attestation.Signature = SignContent(&attestation, s.local.Keypair)

	// Create proposal (attestation + round context)
	proposal := &CheckpointProposal{
		Attestation: attestation,
		Round:       round,
	}

	// Store as pending
	pending := &pendingProposal{
		proposal:  proposal,
		votes:     make([]*CheckpointVote, 0),
		expiresAt: time.Now().Add(s.voteWindow),
		round:     round,
	}

	s.myPendingProposalMu.Lock()
	s.myPendingProposal = pending
	s.myPendingProposalMu.Unlock()

	// Broadcast proposal
	payload, err := json.Marshal(proposal)
	if err != nil {
		logrus.Warnf("checkpoint: failed to marshal proposal: %v", err)
		return
	}

	token := s.mqttClient.Publish(TopicCheckpointPropose, 1, false, payload)
	if token.Wait() && token.Error() != nil {
		logrus.Warnf("checkpoint: failed to publish proposal: %v", token.Error())
		return
	}

	logrus.Infof("📢 proposed checkpoint round %d for %s (restarts=%d, uptime=%ds)",
		round, myName, restarts, totalUptime)

	// Schedule finalization after vote window
	go func() {
		select {
		case <-time.After(s.voteWindow):
			s.finalizeProposal()
		case <-s.ctx.Done():
			return
		}
	}()
}

// HandleProposal processes an incoming checkpoint proposal from another nara
// Voting is always enabled - we want all naras to participate in consensus
// even if they don't propose their own checkpoints
func (s *CheckpointService) HandleProposal(proposal *CheckpointProposal) {
	// Ignore our own proposals
	if proposal.Subject == s.local.Me.Name {
		return
	}

	// Don't vote until boot recovery is complete - we need accurate derived values
	if !s.network.IsBootRecoveryComplete() {
		logrus.Debugf("checkpoint: skipping vote for %s - boot recovery not complete, opinions still forming", proposal.Subject)
		return
	}

	// Ensure we have a ledger
	if s.ledger == nil {
		logrus.Debug("checkpoint: cannot handle proposal - ledger not initialized")
		return
	}

	// Verify the proposal signature
	if !s.verifyProposalSignature(proposal) {
		logrus.Warnf("checkpoint: rejected proposal from %s - invalid signature", proposal.Subject)
		return
	}

	// v2 nodes don't vote on v1 proposals (incompatible protocol versions)
	proposalVersion := proposal.Version
	if proposalVersion == 0 {
		proposalVersion = 1
	}
	if proposalVersion < 2 {
		logrus.Debugf("checkpoint: v2 node ignoring v1 proposal from %s (protocol version mismatch)", proposal.Subject)
		return
	}

	logrus.Debugf("checkpoint: received proposal from %s round %d", proposal.Subject, proposal.Round)

	// Get our view of this nara's state
	ourRestarts := s.ledger.DeriveRestartCount(proposal.Subject)
	ourUptime := s.ledger.DeriveTotalUptime(proposal.Subject)
	ourFirstSeen := s.ledger.GetFirstSeenFromEvents(proposal.Subject)

	// Determine if we approve (values match within tolerance)
	approved := s.valuesMatch(proposal.Observation.Restarts, ourRestarts, 5) &&
		s.valuesMatch(proposal.Observation.TotalUptime, ourUptime, 60) &&
		s.valuesMatch(proposal.Observation.StartTime, ourFirstSeen, 60)

	// v2: Get our reference point - the last checkpoint we've seen for the subject
	lastSeenCheckpointID := s.ledger.GetLatestCheckpointID(proposal.Subject)

	// Create attestation (third-party: I claim this about someone else)
	var observation NaraObservation
	if approved {
		// Sign their values
		observation = proposal.Observation
	} else {
		// Sign our values
		observation = NaraObservation{
			Restarts:    ourRestarts,
			TotalUptime: ourUptime,
			StartTime:   ourFirstSeen,
		}
	}

	attestation := Attestation{
		Version:              2, // v2: includes reference point
		Subject:              proposal.Subject,
		SubjectID:            proposal.SubjectID,
		Observation:          observation,
		Attester:             s.local.Me.Name, // We are the attester (voter)
		AttesterID:           s.local.Me.Status.ID,      // Our ID
		AsOfTime:             proposal.AsOfTime,    // Sign the SAME timestamp as proposal
		LastSeenCheckpointID: lastSeenCheckpointID, // v2: our reference point for this subject
	}

	// Sign the attestation
	attestation.Signature = SignContent(&attestation, s.local.Keypair)

	// Build vote (attestation + vote context)
	vote := &CheckpointVote{
		Attestation: attestation,
		ProposalTS:  proposal.AsOfTime, // Reference the proposal timestamp
		Round:       proposal.Round,
		Approved:    approved,
	}

	// Publish vote with jitter (0-3s) to prevent thundering herd
	// In tests, use zero jitter for faster consensus
	var jitter time.Duration
	if !s.network.testSkipJitter {
		jitter = time.Duration(rand.Intn(3000)) * time.Millisecond
	}

	publishVote := func() {
		payload, err := json.Marshal(vote)
		if err != nil {
			logrus.Warnf("checkpoint: failed to marshal vote: %v", err)
			return
		}

		if s.mqttClient != nil && s.mqttClient.IsConnected() {
			token := s.mqttClient.Publish(TopicCheckpointVote, 1, false, payload)
			if token.Wait() && token.Error() != nil {
				logrus.Warnf("checkpoint: failed to publish vote: %v", token.Error())
			} else {
				logrus.Debugf("checkpoint: voted %v for %s round %d", approved, proposal.Subject, proposal.Round)
			}
		}
	}

	if jitter == 0 {
		publishVote()
	} else {
		time.AfterFunc(jitter, publishVote)
	}
}

// HandleVote processes an incoming vote for one of our proposals
func (s *CheckpointService) HandleVote(vote *CheckpointVote) {
	// Only collect votes for our own proposals
	if vote.Subject != s.local.Me.Name {
		return
	}

	// Verify the vote signature before accepting
	if !s.verifyVoteSignature(vote) {
		logrus.Warnf("checkpoint: rejected vote from %s - invalid signature", vote.Attester)
		return
	}

	// v2 nodes don't accept votes from v1 nodes (incompatible protocol versions)
	voteVersion := vote.Version
	if voteVersion == 0 {
		voteVersion = 1
	}
	if voteVersion < 2 {
		logrus.Debugf("checkpoint: v2 node ignoring v1 vote from %s (protocol version mismatch)", vote.Attester)
		return
	}

	// Get pending proposal and check if vote matches - copy values while holding lock
	s.myPendingProposalMu.Lock()
	pending := s.myPendingProposal
	if pending == nil {
		s.myPendingProposalMu.Unlock()
		return
	}
	// Copy the values we need to check before releasing lock
	proposalTS := pending.proposal.AsOfTime
	proposalRound := pending.round
	s.myPendingProposalMu.Unlock()

	// Check if vote is for current proposal
	if vote.ProposalTS != proposalTS || vote.Round != proposalRound {
		return
	}

	// Check if already finalized
	pending.finalizedMu.Lock()
	if pending.finalized {
		pending.finalizedMu.Unlock()
		return
	}
	pending.finalizedMu.Unlock()

	// Add vote
	pending.votesMu.Lock()
	pending.votes = append(pending.votes, vote)
	shouldFinalize := len(pending.votes) >= MinVotersRequired
	pending.votesMu.Unlock()

	logrus.Debugf("checkpoint: received vote from %s (approved=%v)", vote.Attester, vote.Approved)

	if shouldFinalize {
		go s.finalizeProposal()
	}
}

// verifyVoteSignature verifies that a vote has a valid signature from the claimed voter
// Explicitly verifies the embedded attestation rather than the wrapper
func (s *CheckpointService) verifyVoteSignature(vote *CheckpointVote) bool {
	if vote.Attestation.Signature == "" {
		return false
	}

	// Get voter's public key by ID (stable identifier, not name)
	pubKey := s.network.getPublicKeyForNaraID(vote.AttesterID)
	if pubKey == nil {
		logrus.Debugf("checkpoint: cannot verify vote from attester_id=%s - unknown public key", vote.AttesterID)
		return false
	}

	// Verify the attestation signature (not the wrapper)
	return VerifyContent(&vote.Attestation, pubKey, vote.Attestation.Signature)
}

// verifyProposalSignature verifies that a proposal has a valid signature from the proposer
// Explicitly verifies the embedded attestation rather than the wrapper
func (s *CheckpointService) verifyProposalSignature(proposal *CheckpointProposal) bool {
	if proposal.Attestation.Signature == "" {
		return false
	}

	// Get proposer's public key by ID (stable identifier, not name)
	// For proposals, SubjectID == AttesterID (self-attestation)
	pubKey := s.network.getPublicKeyForNaraID(proposal.SubjectID)
	if pubKey == nil {
		logrus.Debugf("checkpoint: cannot verify proposal from subject_id=%s - unknown public key", proposal.SubjectID)
		return false
	}

	// Verify the attestation signature (not the wrapper)
	return VerifyContent(&proposal.Attestation, pubKey, proposal.Attestation.Signature)
}

// finalizeProposal attempts to finalize the current pending proposal
func (s *CheckpointService) finalizeProposal() {
	s.myPendingProposalMu.Lock()
	pending := s.myPendingProposal
	s.myPendingProposalMu.Unlock()

	if pending == nil {
		return
	}

	pending.finalizedMu.Lock()
	if pending.finalized {
		pending.finalizedMu.Unlock()
		return
	}
	pending.finalized = true
	pending.finalizedMu.Unlock()

	pending.votesMu.Lock()
	votes := make([]*CheckpointVote, len(pending.votes))
	copy(votes, pending.votes)
	pending.votesMu.Unlock()

	logrus.Infof("🗳️  finalizing checkpoint round %d for %s with %d votes",
		pending.round, pending.proposal.Subject, len(votes))

	// Try to find consensus
	checkpoint, success := s.tryFindConsensus(pending.proposal, votes)

	if success {
		// Create and store checkpoint event
		s.storeCheckpoint(checkpoint)
		s.lastCheckpointTimeMu.Lock()
		s.lastCheckpointTime = time.Now()
		s.lastCheckpointTimeMu.Unlock()
		logrus.Infof("✅ checkpoint consensus for %s (round %d, %d voters agreed)",
			pending.proposal.Subject, pending.round, len(checkpoint.VoterIDs))
	} else if pending.round == 1 {
		// Round 1 failed, try round 2 with computed values
		logrus.Infof("🔄 checkpoint round 1 failed for %s, attempting round 2 with computed values",
			pending.proposal.Subject)
		s.proposeRound2(votes)
	} else {
		// Round 2 failed, give up
		// Update lastCheckpointTime to prevent spam - wait another 24h before retrying
		s.lastCheckpointTimeMu.Lock()
		s.lastCheckpointTime = time.Now()
		s.lastCheckpointTimeMu.Unlock()
		logrus.Warnf("❌ checkpoint round 2 failed for %s, will retry in next 24h cycle",
			pending.proposal.Subject)
	}
}

// tryFindConsensus attempts to find a set of matching signatures
func (s *CheckpointService) tryFindConsensus(proposal *CheckpointProposal, votes []*CheckpointVote) (*CheckpointEventPayload, bool) {
	if len(votes) < MinVotersRequired {
		logrus.Printf("checkpoint: insufficient voters (%d < %d)", len(votes), MinVotersRequired)
		return nil, false
	}

	// Group votes by their signed values (v2: includes reference point)
	type valueKey struct {
		Restarts             int64
		TotalUptime          int64
		FirstSeen            int64
		LastSeenCheckpointID string // v2: votes must agree on reference point
	}

	groups := make(map[valueKey][]*CheckpointVote)

	for _, vote := range votes {
		key := valueKey{
			Restarts:             vote.Observation.Restarts,
			TotalUptime:          vote.Observation.TotalUptime,
			FirstSeen:            vote.Observation.StartTime,
			LastSeenCheckpointID: vote.LastSeenCheckpointID,
		}
		groups[key] = append(groups[key], vote)
	}

	// Also add our own proposal values as a potential group
	proposalKey := valueKey{
		Restarts:             proposal.Observation.Restarts,
		TotalUptime:          proposal.Observation.TotalUptime,
		FirstSeen:            proposal.Observation.StartTime,
		LastSeenCheckpointID: proposal.LastSeenCheckpointID,
	}

	// Find the largest group that meets minimum threshold
	var bestKey valueKey
	var bestVotes []*CheckpointVote
	for key, groupVotes := range groups {
		if len(groupVotes) >= MinVotersRequired && len(groupVotes) > len(bestVotes) {
			bestKey = key
			bestVotes = groupVotes
		}
	}

	// Check if approvals of our proposal values also form a valid group
	var approvalVotes []*CheckpointVote
	for _, vote := range votes {
		if vote.Approved {
			approvalVotes = append(approvalVotes, vote)
		}
	}
	if len(approvalVotes) >= MinVotersRequired && len(approvalVotes) > len(bestVotes) {
		bestKey = proposalKey
		bestVotes = approvalVotes
	}

	if len(bestVotes) < MinVotersRequired {
		return nil, false
	}

	// Sort votes by voter uptime (highest first) and limit to MaxCheckpointSignatures
	// This ensures we keep signatures from the most reliable naras
	type voteSortEntry struct {
		vote   *CheckpointVote
		uptime int64
	}
	sortedVotes := make([]voteSortEntry, 0, len(bestVotes))
	for _, vote := range bestVotes {
		uptime := s.getVoterUptime(vote.Attester)
		sortedVotes = append(sortedVotes, voteSortEntry{vote, uptime})
	}
	sort.Slice(sortedVotes, func(i, j int) bool {
		return sortedVotes[i].uptime > sortedVotes[j].uptime
	})

	// Build checkpoint with matching signatures
	checkpoint := &CheckpointEventPayload{
		Version:              2, // v2: includes chain of trust
		Subject:              proposal.Subject,
		SubjectID:            proposal.SubjectID,
		PreviousCheckpointID: bestKey.LastSeenCheckpointID, // v2: reference point from consensus
		AsOfTime:             proposal.AsOfTime,
		Observation: NaraObservation{
			Restarts:    bestKey.Restarts,
			TotalUptime: bestKey.TotalUptime,
			StartTime:   bestKey.FirstSeen,
		},
		VoterIDs:   make([]types.NaraID, 0, MaxCheckpointSignatures),
		Signatures: make([]string, 0, MaxCheckpointSignatures),
		Round:      proposal.Round, // Needed for signature verification
	}

	// Add proposer's signature first if values match (proposer always included)
	proposerIncluded := false
	if bestKey.Restarts == proposal.Observation.Restarts &&
		bestKey.TotalUptime == proposal.Observation.TotalUptime &&
		bestKey.FirstSeen == proposal.Observation.StartTime &&
		bestKey.LastSeenCheckpointID == proposal.LastSeenCheckpointID {
		checkpoint.VoterIDs = append(checkpoint.VoterIDs, proposal.SubjectID)
		checkpoint.Signatures = append(checkpoint.Signatures, proposal.Signature)
		proposerIncluded = true
	}

	// Add top voter signatures up to MaxCheckpointSignatures total
	maxVoters := MaxCheckpointSignatures
	if proposerIncluded {
		maxVoters-- // Reserve one slot for proposer
	}
	for i, entry := range sortedVotes {
		if i >= maxVoters {
			break
		}
		checkpoint.VoterIDs = append(checkpoint.VoterIDs, entry.vote.AttesterID)
		checkpoint.Signatures = append(checkpoint.Signatures, entry.vote.Signature)
	}

	return checkpoint, true
}

// getVoterUptime looks up a voter's total uptime from the ledger
func (s *CheckpointService) getVoterUptime(voterName types.NaraName) int64 {
	if s.ledger == nil {
		return 0
	}
	return s.ledger.DeriveTotalUptime(voterName)
}

// proposeRound2 creates a round 2 proposal using trimmed mean of round 1 votes
func (s *CheckpointService) proposeRound2(votes []*CheckpointVote) {
	if len(votes) == 0 {
		return
	}

	// Collect all proposed values from votes
	var restartVals, uptimeVals, firstSeenVals []int64
	for _, vote := range votes {
		restartVals = append(restartVals, vote.Observation.Restarts)
		uptimeVals = append(uptimeVals, vote.Observation.TotalUptime)
		firstSeenVals = append(firstSeenVals, vote.Observation.StartTime)
	}

	// Compute trimmed mean consensus values from round 1 votes
	consensus := &round2Values{
		restarts:    TrimmedMeanInt64(restartVals),
		totalUptime: TrimmedMeanInt64(uptimeVals),
		firstSeen:   TrimmedMeanInt64(firstSeenVals),
	}

	// Propose round 2 with consensus values
	s.proposeCheckpointRound(2, consensus)
}

// storeCheckpoint adds the finalized checkpoint to the ledger and broadcasts it
// Requires at least 2 valid signatures to accept the checkpoint
func (s *CheckpointService) storeCheckpoint(checkpoint *CheckpointEventPayload) {
	event := NewCheckpointEvent(
		checkpoint.Subject,
		checkpoint.AsOfTime,
		checkpoint.Observation.StartTime,
		checkpoint.Observation.Restarts,
		checkpoint.Observation.TotalUptime,
	)

	// Update with voter info and metadata
	event.Checkpoint.Version = 2 // v2: includes chain of trust
	event.Checkpoint.VoterIDs = checkpoint.VoterIDs
	event.Checkpoint.Signatures = checkpoint.Signatures
	event.Checkpoint.SubjectID = checkpoint.SubjectID
	event.Checkpoint.Round = checkpoint.Round

	// v2: Use the PreviousCheckpointID from consensus (already set in tryFindConsensus)
	// DO NOT overwrite it - signatures were created with this specific value
	event.Checkpoint.PreviousCheckpointID = checkpoint.PreviousCheckpointID

	// Recompute ID since PreviousCheckpointID affects ContentString
	event.ComputeID()

	// Verify at least MinCheckpointSignatures signatures before accepting
	result := s.verifyCheckpointSignatures(event.Checkpoint)
	if !result.Valid {
		logrus.Warnf("checkpoint: rejecting checkpoint for %s - %d/%d verified, %d/%d keys known (need %d+)",
			checkpoint.Subject, result.ValidCount, result.TotalCount, result.KnownCount, result.TotalCount, MinCheckpointSignatures)
		return
	}

	if s.ledger.AddEvent(event) {
		logrus.Infof("📝 stored checkpoint for %s (%d/%d signatures verified)",
			checkpoint.Subject, result.ValidCount, result.TotalCount)
	}

	// Broadcast finalized checkpoint so everyone stores it
	s.broadcastFinalCheckpoint(event)
}

// broadcastFinalCheckpoint publishes the finalized checkpoint for all to store
func (s *CheckpointService) broadcastFinalCheckpoint(event SyncEvent) {
	if s.mqttClient == nil || !s.mqttClient.IsConnected() {
		return
	}

	payload, err := json.Marshal(event)
	if err != nil {
		logrus.Warnf("checkpoint: failed to marshal final checkpoint: %v", err)
		return
	}

	token := s.mqttClient.Publish(TopicCheckpointFinal, 1, false, payload)
	if token.Wait() && token.Error() != nil {
		logrus.Warnf("checkpoint: failed to publish final checkpoint: %v", token.Error())
	}
}

// HandleFinalCheckpoint stores a finalized checkpoint from another nara
func (s *CheckpointService) HandleFinalCheckpoint(event *SyncEvent) {
	if event.Service != ServiceCheckpoint || event.Checkpoint == nil {
		return
	}

	// Don't store our own checkpoints again (we already stored it)
	if event.Checkpoint.Subject == s.local.Me.Name {
		return
	}

	// Verify the checkpoint has minimum required signatures
	if len(event.Checkpoint.Signatures) < MinVotersRequired {
		logrus.Warnf("checkpoint: rejected checkpoint for %s - insufficient signatures (%d < %d)",
			event.Checkpoint.Subject, len(event.Checkpoint.Signatures), MinVotersRequired)
		return
	}

	// Verify at least MinCheckpointSignatures signatures from known naras
	// We may not know all voters, but we require at least 2 verifiable signatures for trust
	result := s.verifyCheckpointSignatures(event.Checkpoint)
	if !result.Valid {
		logrus.Warnf("checkpoint: rejected checkpoint for %s - %d/%d verified, %d/%d keys known (need %d+)",
			event.Checkpoint.Subject, result.ValidCount, result.TotalCount, result.KnownCount, result.TotalCount, MinCheckpointSignatures)
		return
	}

	if s.ledger.AddEvent(*event) {
		logrus.Printf("checkpoint: stored final checkpoint for %s from network (%d/%d verified)",
			event.Checkpoint.Subject, result.ValidCount, result.TotalCount)
	}
}

// verifyCheckpointSignatures verifies checkpoint signatures and returns detailed result
func (s *CheckpointService) verifyCheckpointSignatures(checkpoint *CheckpointEventPayload) CheckpointVerificationResult {
	lookup := PublicKeyLookup(func(id types.NaraID, name types.NaraName) ed25519.PublicKey {
		return s.network.getPublicKeyForNaraID(id)
	})
	return checkpoint.VerifySignatureWithCounts(lookup)
}

// valuesMatch checks if two values are within tolerance
func (s *CheckpointService) valuesMatch(a, b, tolerance int64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff <= tolerance
}

// MQTT Handlers - to be registered in mqtt.go

// CheckpointProposalHandler handles incoming checkpoint proposals
func (network *Network) checkpointProposalHandler(client mqtt.Client, msg mqtt.Message) {
	if network.checkpointService == nil {
		return
	}

	var proposal CheckpointProposal
	if err := json.Unmarshal(msg.Payload(), &proposal); err != nil {
		logrus.Debugf("checkpoint: invalid proposal JSON: %v", err)
		return
	}

	network.checkpointService.HandleProposal(&proposal)
}

// CheckpointVoteHandler handles incoming checkpoint votes
func (network *Network) checkpointVoteHandler(client mqtt.Client, msg mqtt.Message) {
	if network.checkpointService == nil {
		return
	}

	var vote CheckpointVote
	if err := json.Unmarshal(msg.Payload(), &vote); err != nil {
		logrus.Debugf("checkpoint: invalid vote JSON: %v", err)
		return
	}

	network.checkpointService.HandleVote(&vote)
}

// CheckpointFinalHandler handles finalized checkpoints from the network
func (network *Network) checkpointFinalHandler(client mqtt.Client, msg mqtt.Message) {
	if network.checkpointService == nil {
		return
	}

	var event SyncEvent
	if err := json.Unmarshal(msg.Payload(), &event); err != nil {
		logrus.Debugf("checkpoint: invalid final checkpoint JSON: %v", err)
		return
	}

	network.checkpointService.HandleFinalCheckpoint(&event)
}
