package nara

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
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
	DefaultVoteWindow         = 5 * time.Minute
	MinVotersRequired         = 2  // Outside the proposer, so 3 total signatures
	MaxCheckpointSignatures   = 10 // Store at most 10 signatures, prioritizing high-uptime naras
)

// CheckpointProposal is broadcast when a nara proposes a checkpoint about itself
type CheckpointProposal struct {
	Subject    string `json:"subject"`     // The nara proposing (itself)
	SubjectID  string `json:"subject_id"`  // Nara ID
	ProposedAt int64  `json:"proposed_at"` // Unix timestamp
	Round      int    `json:"round"`       // 1 or 2

	// Values to sign (proposer's view)
	Restarts    int64 `json:"restarts"`
	TotalUptime int64 `json:"total_uptime"`
	FirstSeen   int64 `json:"first_seen"`

	Signature string `json:"signature"` // Proposer signs these values
}

// SignableContent returns the canonical string for signing a proposal
func (p *CheckpointProposal) SignableContent() string {
	return fmt.Sprintf("checkpoint-proposal:%s:%d:%d:%d:%d:%d",
		p.SubjectID, p.ProposedAt, p.Restarts, p.TotalUptime, p.FirstSeen, p.Round)
}

// CheckpointVote is a response to a proposal
type CheckpointVote struct {
	Subject    string `json:"subject"`     // Who we're voting about
	Voter      string `json:"voter"`       // Who is voting
	VoterID    string `json:"voter_id"`    // Voter's nara ID
	ProposalTS int64  `json:"proposal_ts"` // Which proposal this vote is for
	Round      int    `json:"round"`       // 1 or 2

	Approved bool `json:"approved"` // true = agree with proposal values

	// Values we're signing (same as proposal if approved, our own if rejected)
	Restarts    int64 `json:"restarts"`
	TotalUptime int64 `json:"total_uptime"`
	FirstSeen   int64 `json:"first_seen"`

	Signature string `json:"signature"` // Signs the values above (verifiable!)
}

// SignableContent returns the canonical string for signing a vote
func (v *CheckpointVote) SignableContent() string {
	return fmt.Sprintf("checkpoint-vote:%s:%d:%d:%d:%d:%d",
		v.VoterID, v.ProposalTS, v.Restarts, v.TotalUptime, v.FirstSeen, v.Round)
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
	pendingProposals   map[string]*pendingProposal // keyed by "subject:proposedAt"
	pendingProposalsMu sync.RWMutex

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
// Only starts if USE_CHECKPOINT_CREATION is enabled
func (s *CheckpointService) Start() {
	if !useCheckpointCreation() {
		logrus.Debug("checkpoint: creation disabled via USE_CHECKPOINT_CREATION, service passive")
		return
	}
	go s.maintenanceLoop()
}

// Stop gracefully shuts down the checkpoint service
func (s *CheckpointService) Stop() {
	s.cancel()
}

// maintenanceLoop checks periodically if we need to propose a checkpoint
func (s *CheckpointService) maintenanceLoop() {
	// Check every 15 minutes if we need to propose
	ticker := time.NewTicker(15 * time.Minute)
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

	proposal := &CheckpointProposal{
		Subject:     myName,
		SubjectID:   myID,
		ProposedAt:  now,
		Round:       round,
		Restarts:    restarts,
		TotalUptime: totalUptime,
		FirstSeen:   firstSeen,
	}

	// Sign the proposal
	proposal.Signature = SignContent(proposal, s.local.Keypair)

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

	logrus.Printf("checkpoint: proposed round %d for %s (restarts=%d, uptime=%d)",
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

	logrus.Debugf("checkpoint: received proposal from %s round %d", proposal.Subject, proposal.Round)

	// Get our view of this nara's state
	ourRestarts := s.ledger.DeriveRestartCount(proposal.Subject)
	ourUptime := s.ledger.DeriveTotalUptime(proposal.Subject)
	ourFirstSeen := s.ledger.GetFirstSeenFromEvents(proposal.Subject)

	// Determine if we approve (values match within tolerance)
	approved := s.valuesMatch(proposal.Restarts, ourRestarts, 5) &&
		s.valuesMatch(proposal.FirstSeen, ourFirstSeen, 60)

	// Build vote with either their values (if approved) or our values (if rejected)
	vote := &CheckpointVote{
		Subject:    proposal.Subject,
		Voter:      s.local.Me.Name,
		VoterID:    s.local.Me.Status.ID,
		ProposalTS: proposal.ProposedAt,
		Round:      proposal.Round,
		Approved:   approved,
	}

	if approved {
		// Sign their values
		vote.Restarts = proposal.Restarts
		vote.TotalUptime = proposal.TotalUptime
		vote.FirstSeen = proposal.FirstSeen
	} else {
		// Sign our values
		vote.Restarts = ourRestarts
		vote.TotalUptime = ourUptime
		vote.FirstSeen = ourFirstSeen
	}

	// Sign the vote
	vote.Signature = SignContent(vote, s.local.Keypair)

	// Publish vote
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

// HandleVote processes an incoming vote for one of our proposals
func (s *CheckpointService) HandleVote(vote *CheckpointVote) {
	// Only collect votes for our own proposals
	if vote.Subject != s.local.Me.Name {
		return
	}

	// Verify the vote signature before accepting
	if !s.verifyVoteSignature(vote) {
		logrus.Warnf("checkpoint: rejected vote from %s - invalid signature", vote.Voter)
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
	proposalTS := pending.proposal.ProposedAt
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
	pending.votesMu.Unlock()

	logrus.Debugf("checkpoint: received vote from %s (approved=%v)", vote.Voter, vote.Approved)
}

// verifyVoteSignature verifies that a vote has a valid signature from the claimed voter
func (s *CheckpointService) verifyVoteSignature(vote *CheckpointVote) bool {
	if vote.Signature == "" {
		return false
	}

	// Get voter's public key
	pubKey := s.network.getPublicKeyForNara(vote.Voter)
	if pubKey == nil {
		logrus.Debugf("checkpoint: cannot verify vote from %s - unknown public key", vote.Voter)
		return false
	}

	// Verify signature using Signable interface
	return VerifyContent(vote, pubKey, vote.Signature)
}

// verifyProposalSignature verifies that a proposal has a valid signature from the proposer
func (s *CheckpointService) verifyProposalSignature(proposal *CheckpointProposal) bool {
	if proposal.Signature == "" {
		return false
	}

	// Get proposer's public key
	pubKey := s.network.getPublicKeyForNara(proposal.Subject)
	if pubKey == nil {
		logrus.Debugf("checkpoint: cannot verify proposal from %s - unknown public key", proposal.Subject)
		return false
	}

	// Verify signature using Signable interface
	return VerifyContent(proposal, pubKey, proposal.Signature)
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

	logrus.Printf("checkpoint: finalizing round %d with %d votes", pending.round, len(votes))

	// Try to find consensus
	checkpoint, success := s.tryFindConsensus(pending.proposal, votes)

	if success {
		// Create and store checkpoint event
		s.storeCheckpoint(checkpoint)
		s.lastCheckpointTimeMu.Lock()
		s.lastCheckpointTime = time.Now()
		s.lastCheckpointTimeMu.Unlock()
		logrus.Printf("checkpoint: successfully created checkpoint for %s with %d voters",
			pending.proposal.Subject, len(checkpoint.VoterIDs))
	} else if pending.round == 1 {
		// Round 1 failed, try round 2 with computed values
		logrus.Printf("checkpoint: round 1 failed, attempting round 2")
		s.proposeRound2(votes)
	} else {
		// Round 2 failed, give up
		logrus.Printf("checkpoint: round 2 failed, giving up until next 24h cycle")
	}
}

// tryFindConsensus attempts to find a set of matching signatures
func (s *CheckpointService) tryFindConsensus(proposal *CheckpointProposal, votes []*CheckpointVote) (*CheckpointEventPayload, bool) {
	if len(votes) < MinVotersRequired {
		logrus.Printf("checkpoint: insufficient voters (%d < %d)", len(votes), MinVotersRequired)
		return nil, false
	}

	// Group votes by their signed values
	type valueKey struct {
		Restarts    int64
		TotalUptime int64
		FirstSeen   int64
	}

	groups := make(map[valueKey][]*CheckpointVote)

	for _, vote := range votes {
		key := valueKey{vote.Restarts, vote.TotalUptime, vote.FirstSeen}
		groups[key] = append(groups[key], vote)
	}

	// Also add our own proposal values as a potential group
	proposalKey := valueKey{proposal.Restarts, proposal.TotalUptime, proposal.FirstSeen}

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
		uptime := s.getVoterUptime(vote.Voter)
		sortedVotes = append(sortedVotes, voteSortEntry{vote, uptime})
	}
	sort.Slice(sortedVotes, func(i, j int) bool {
		return sortedVotes[i].uptime > sortedVotes[j].uptime
	})

	// Build checkpoint with matching signatures
	checkpoint := &CheckpointEventPayload{
		Subject:     proposal.Subject,
		SubjectID:   proposal.SubjectID,
		AsOfTime:    proposal.ProposedAt,
		Restarts:    bestKey.Restarts,
		TotalUptime: bestKey.TotalUptime,
		FirstSeen:   bestKey.FirstSeen,
		VoterIDs:    make([]string, 0, MaxCheckpointSignatures),
		Signatures:  make([]string, 0, MaxCheckpointSignatures),
		Importance:  ImportanceCritical,
	}

	// Add proposer's signature first if values match (proposer always included)
	proposerIncluded := false
	if bestKey.Restarts == proposal.Restarts &&
		bestKey.TotalUptime == proposal.TotalUptime &&
		bestKey.FirstSeen == proposal.FirstSeen {
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
		checkpoint.VoterIDs = append(checkpoint.VoterIDs, entry.vote.VoterID)
		checkpoint.Signatures = append(checkpoint.Signatures, entry.vote.Signature)
	}

	return checkpoint, true
}

// getVoterUptime looks up a voter's total uptime from the ledger
func (s *CheckpointService) getVoterUptime(voterName string) int64 {
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
		restartVals = append(restartVals, vote.Restarts)
		uptimeVals = append(uptimeVals, vote.TotalUptime)
		firstSeenVals = append(firstSeenVals, vote.FirstSeen)
	}

	// Compute trimmed mean consensus values from round 1 votes
	consensus := &round2Values{
		restarts:    trimmedMeanInt64(restartVals),
		totalUptime: trimmedMeanInt64(uptimeVals),
		firstSeen:   trimmedMeanInt64(firstSeenVals),
	}

	// Propose round 2 with consensus values
	s.proposeCheckpointRound(2, consensus)
}

// storeCheckpoint adds the finalized checkpoint to the ledger and broadcasts it
func (s *CheckpointService) storeCheckpoint(checkpoint *CheckpointEventPayload) {
	event := NewCheckpointEvent(
		checkpoint.Subject,
		checkpoint.AsOfTime,
		checkpoint.FirstSeen,
		checkpoint.Restarts,
		checkpoint.TotalUptime,
	)

	// Update with voter info
	event.Checkpoint.VoterIDs = checkpoint.VoterIDs
	event.Checkpoint.Signatures = checkpoint.Signatures
	event.Checkpoint.SubjectID = checkpoint.SubjectID

	if s.ledger.AddEvent(event) {
		logrus.Printf("checkpoint: stored checkpoint for %s", checkpoint.Subject)
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

	// Verify all signatures match the checkpoint values
	validSignatures := s.verifyCheckpointSignatures(event.Checkpoint)
	if validSignatures < MinVotersRequired {
		logrus.Warnf("checkpoint: rejected checkpoint for %s - only %d valid signatures (need %d)",
			event.Checkpoint.Subject, validSignatures, MinVotersRequired)
		return
	}

	if s.ledger.AddEvent(*event) {
		logrus.Printf("checkpoint: stored final checkpoint for %s from network (%d valid signatures)",
			event.Checkpoint.Subject, validSignatures)
	}
}

// verifyCheckpointSignatures verifies all signatures in a checkpoint and returns count of valid ones
func (s *CheckpointService) verifyCheckpointSignatures(checkpoint *CheckpointEventPayload) int {
	if len(checkpoint.VoterIDs) != len(checkpoint.Signatures) {
		logrus.Warnf("checkpoint: voter/signature count mismatch for %s", checkpoint.Subject)
		return 0
	}

	validCount := 0
	for i, voterID := range checkpoint.VoterIDs {
		signature := checkpoint.Signatures[i]

		// Get voter's public key by ID
		pubKey := s.network.getPublicKeyForNaraID(voterID)
		if pubKey == nil {
			logrus.Debugf("checkpoint: cannot verify signature from voter %s - unknown public key", voterID)
			continue
		}

		// Build signable content - need to determine if this is proposer or voter signature
		// Proposer signs with proposal format, voters sign with vote format
		var signableContent string
		if voterID == checkpoint.SubjectID {
			// This is the proposer's signature
			signableContent = fmt.Sprintf("checkpoint-proposal:%s:%d:%d:%d:%d:%d",
				checkpoint.SubjectID, checkpoint.AsOfTime, checkpoint.Restarts, checkpoint.TotalUptime, checkpoint.FirstSeen, 1)
		} else {
			// This is a voter's signature
			signableContent = fmt.Sprintf("checkpoint-vote:%s:%d:%d:%d:%d:%d",
				voterID, checkpoint.AsOfTime, checkpoint.Restarts, checkpoint.TotalUptime, checkpoint.FirstSeen, 1)
		}

		if VerifySignatureBase64(pubKey, []byte(signableContent), signature) {
			validCount++
		} else {
			logrus.Debugf("checkpoint: invalid signature from voter %s for %s", voterID, checkpoint.Subject)
		}
	}

	return validCount
}

// valuesMatch checks if two values are within tolerance
func (s *CheckpointService) valuesMatch(a, b, tolerance int64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff <= tolerance
}

// trimmedMeanInt64 computes trimmed mean, removing outliers
func trimmedMeanInt64(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	if len(values) == 1 {
		return values[0]
	}

	// Sort
	sorted := make([]int64, len(values))
	copy(sorted, values)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	// Get median
	median := sorted[len(sorted)/2]

	// Filter outliers (keep values within 0.2x - 5x of median)
	// Handle edge cases: zero median, negative median
	var filtered []int64
	if median == 0 {
		// For zero median, keep values close to zero (within small range)
		for _, v := range sorted {
			if v >= -10 && v <= 10 {
				filtered = append(filtered, v)
			}
		}
	} else if median > 0 {
		// Positive median: standard 0.2x - 5x range
		lowerBound := median / 5
		upperBound := median * 5
		for _, v := range sorted {
			if v >= lowerBound && v <= upperBound {
				filtered = append(filtered, v)
			}
		}
	} else {
		// Negative median: invert bounds (5x is smaller, 0.2x is larger)
		lowerBound := median * 5 // More negative (smaller)
		upperBound := median / 5 // Less negative (larger)
		for _, v := range sorted {
			if v >= lowerBound && v <= upperBound {
				filtered = append(filtered, v)
			}
		}
	}

	if len(filtered) == 0 {
		return median
	}

	// Compute average
	var sum int64
	for _, v := range filtered {
		sum += v
	}
	return sum / int64(len(filtered))
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
