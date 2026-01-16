package nara

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// LogCategory represents a log section for grouped output
type LogCategory string

const (
	CategoryPresence LogCategory = "PRESENCE"
	CategoryGossip   LogCategory = "GOSSIP"
	CategorySocial   LogCategory = "SOCIAL"
	CategoryHTTP     LogCategory = "HTTP"
	CategoryStash    LogCategory = "STASH"
	CategorySystem   LogCategory = "SYSTEM"
	CategoryMesh     LogCategory = "MESH"
)

const (
	defaultBatchInterval = 3 * time.Second
)

// LogEvent is the common struct for all loggable events
type LogEvent struct {
	Category  LogCategory
	Type      string // "howdy", "dm", "gossip-merge", "discovery", etc.
	Actor     string // generic actor field - can be a nara name, service name, or any other identifier
	Target    string // generic target field - can be a nara name, cluster name, or any other identifier
	Count     int    // for pre-aggregated events (e.g., "merged 50 events")
	Detail    string // optional extra info
	Instant   bool   // bypass batching, log immediately
	Timestamp time.Time

	// GroupFormat is an optional formatter for grouped verbose logs.
	// If set, this function is called instead of the default formatGroupedMessage switch.
	// The actors parameter is a pre-formatted string like "alice and bob" or "alice, bob, and carol".
	GroupFormat func(actors string) string
}

// verboseGroup holds aggregated events for verbose mode logging
type verboseGroup struct {
	eventType   string
	actors      []string
	target      string
	detail      string
	timer       *time.Timer
	groupFormat func(actors string) string // Optional custom formatter from event
}

// LogService provides unified logging with batching and event watching.
// It receives events from two sources:
// 1. Automatic: registered as a listener on SyncLedger
// 2. Manual: direct Push() calls for things not in the event store
type LogService struct {
	localName NaraName // Our nara's name for filtering self-events

	// Event channel for incoming log events
	events chan LogEvent

	// Batch state - events grouped by type
	batch   map[string][]LogEvent
	batchMu sync.Mutex

	// Instant log queue (for ordered output with batched logs)
	instantLogs   []string
	instantLogsMu sync.Mutex

	// Configuration
	batchInterval        time.Duration
	verbose              bool // When true, log everything immediately with full detail
	suppressLedgerEvents bool // When true, suppress events from ledger listener (during boot recovery)

	// Verbose mode aggregation - auto-groups similar events
	verboseBuffer   map[string]*verboseGroup // Key: "type\x00target\x00detail", Value: group data
	verboseBufferMu sync.Mutex

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewLogService creates a new LogService
func NewLogService(localName NaraName) *LogService {
	return &LogService{
		localName:     localName,
		events:        make(chan LogEvent, 100),
		batch:         make(map[string][]LogEvent),
		batchInterval: defaultBatchInterval,
		verboseBuffer: make(map[string]*verboseGroup),
	}
}

// RegisterWithLedger registers the LogService as a listener on the SyncLedger
func (ls *LogService) RegisterWithLedger(ledger *SyncLedger) {
	if ledger == nil {
		return
	}
	ledger.AddListener(func(event SyncEvent) {
		// Skip logging during boot recovery to avoid spamming console with historical events
		if ls.suppressLedgerEvents {
			return
		}
		if logEvent := ls.transformEvent(event); logEvent != nil {
			ls.Push(*logEvent)
		}
	})
}

// SetVerbose enables or disables verbose mode.
// In verbose mode, all logs are printed immediately with full detail.
// In normal mode, logs are batched and summarized.
func (ls *LogService) SetVerbose(verbose bool) {
	ls.verbose = verbose
}

// SetSuppressLedgerEvents enables or disables suppression of ledger events.
// When true, events from the ledger listener are ignored (useful during boot recovery).
// Manual Push() calls are still processed normally.
func (ls *LogService) SetSuppressLedgerEvents(suppress bool) {
	ls.suppressLedgerEvents = suppress
}

// Start begins the event processing and batch flushing goroutines
func (ls *LogService) Start(ctx context.Context) {
	ls.ctx, ls.cancel = context.WithCancel(ctx)

	// Start event processor
	ls.wg.Add(1)
	go ls.processEvents()

	// Start batch flusher
	ls.wg.Add(1)
	go ls.flushLoop()
}

// Stop gracefully shuts down the LogService
func (ls *LogService) Stop() {
	if ls.cancel != nil {
		ls.cancel()
	}
	ls.wg.Wait()
	// Final flush
	ls.flushBatch()
}

// Push sends a log event to the service
func (ls *LogService) Push(event LogEvent) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	select {
	case ls.events <- event:
	default:
		// Channel full, drop event (non-blocking)
	}
}

// processEvents reads from the event channel and routes to batch or instant
func (ls *LogService) processEvents() {
	defer ls.wg.Done()

	for {
		select {
		case <-ls.ctx.Done():
			return
		case event := <-ls.events:
			if ls.verbose {
				// In verbose mode, print everything immediately with full detail
				ls.logVerbose(event)
			} else if event.Instant {
				ls.logInstant(event)
			} else {
				ls.addToBatch(event)
			}
		}
	}
}

// logVerbose handles verbose mode logging with smart auto-grouping.
// Events with the same type+target+detail are grouped and actors are aggregated.
// After 1 second of no new events for a group, it's flushed and logged.
func (ls *LogService) logVerbose(event LogEvent) {
	// Events marked instant or without an actor bypass grouping
	if event.Instant || event.Actor == "" {
		msg := ls.formatVerboseMessage(event)
		logrus.Debugf("%s", msg)
		return
	}

	// Build grouping key: type + target + detail
	key := event.Type + "\x00" + event.Target + "\x00" + event.Detail

	ls.verboseBufferMu.Lock()
	defer ls.verboseBufferMu.Unlock()

	group, exists := ls.verboseBuffer[key]
	if !exists {
		group = &verboseGroup{
			eventType:   event.Type,
			target:      event.Target,
			detail:      event.Detail,
			actors:      []string{},
			groupFormat: event.GroupFormat,
		}
		ls.verboseBuffer[key] = group
	} else if event.GroupFormat != nil && group.groupFormat == nil {
		// Use the first non-nil formatter we see
		group.groupFormat = event.GroupFormat
	}

	// Add actor if not already present
	actorExists := false
	for _, a := range group.actors {
		if a == event.Actor {
			actorExists = true
			break
		}
	}
	if !actorExists {
		group.actors = append(group.actors, event.Actor)
	}

	// Reset timer
	if group.timer != nil {
		group.timer.Stop()
	}
	group.timer = time.AfterFunc(1*time.Second, func() {
		ls.flushVerboseGroup(key)
	})
}

// flushVerboseGroup logs an aggregated group of events
func (ls *LogService) flushVerboseGroup(key string) {
	ls.verboseBufferMu.Lock()
	group, exists := ls.verboseBuffer[key]
	if !exists {
		ls.verboseBufferMu.Unlock()
		return
	}
	delete(ls.verboseBuffer, key)
	ls.verboseBufferMu.Unlock()

	if len(group.actors) == 0 {
		return
	}

	// Format the log message with aggregated actors
	actorsList := formatActorsList(group.actors)

	var msg string
	if group.groupFormat != nil {
		// Use custom formatter provided by the event type
		msg = group.groupFormat(actorsList)
	} else {
		// Fall back to default formatting
		msg = ls.formatGroupedMessage(group.eventType, actorsList, group.target, group.detail)
	}
	logrus.Debugf("%s", msg)
}

// formatActorsList formats a list of actors nicely
func formatActorsList(actors []string) string {
	switch len(actors) {
	case 1:
		return actors[0]
	case 2:
		return actors[0] + " and " + actors[1]
	case 3, 4:
		return strings.Join(actors[:len(actors)-1], ", ") + ", and " + actors[len(actors)-1]
	default:
		return strings.Join(actors[:3], ", ") + fmt.Sprintf(", and %d others", len(actors)-3)
	}
}

// formatGroupedMessage creates a message for aggregated events
func (ls *LogService) formatGroupedMessage(eventType, actors, target, detail string) string {
	switch eventType {
	case "tease":
		return fmt.Sprintf("😈 %s teased %s (%s)", actors, target, detail)
	case "barrio-movement":
		return fmt.Sprintf("🏘️  %s moved barrio: %s", actors, detail)
	case "newspaper":
		if detail != "" {
			return fmt.Sprintf("📰 %s posted newspapers (%s)", actors, detail)
		}
		return fmt.Sprintf("📰 %s posted newspapers", actors)
	case "gossip-merge":
		return fmt.Sprintf("📰 merged events from %s", actors)
	case "howdy-for-me":
		return fmt.Sprintf("📬 got howdy from %s", actors)
	case "dm-received":
		return fmt.Sprintf("📬 got DM from %s", actors)
	case "ping-received":
		return fmt.Sprintf("🏓 got ping from %s", actors)
	case "discovery":
		return fmt.Sprintf("📡 discovered %s on mesh", actors)
	default:
		// Generic format
		if target != "" && detail != "" {
			return fmt.Sprintf("• %s → %s: %s", actors, target, detail)
		} else if target != "" {
			return fmt.Sprintf("• %s → %s", actors, target)
		} else if detail != "" {
			return fmt.Sprintf("• %s: %s", actors, detail)
		}
		return fmt.Sprintf("• %s", actors)
	}
}

// formatVerboseMessage creates a descriptive message for verbose mode
func (ls *LogService) formatVerboseMessage(event LogEvent) string {
	// Type-specific formatting (handle batched event types properly in verbose mode)
	switch event.Type {
	case "gossip-merge":
		return fmt.Sprintf("📰 merged %d events from %s", event.Count, event.Actor)
	case "mesh-sync":
		return fmt.Sprintf("📦 synced %d events from %s", event.Count, event.Actor)
	case "howdy-for-me":
		return fmt.Sprintf("📬 got howdy from %s", event.Actor)
	case "dm-received":
		return fmt.Sprintf("📬 got DM from %s", event.Actor)
	case "ping-received":
		return fmt.Sprintf("🏓 got ping from %s", event.Actor)
	case "discovery":
		return fmt.Sprintf("📡 discovered %s on mesh", event.Actor)
	case "peer-resolution-failed":
		return fmt.Sprintf("⚠️ couldn't resolve peer %s", event.Actor)
	case "newspaper":
		return fmt.Sprintf("📰 got newspaper from %s", event.Actor)
	case "barrio-movement":
		// Format: "🏘️  sand moved barrio: sand→ufo 🛸 (via grid, grid=174)"
		return fmt.Sprintf("🏘️  %s moved barrio: %s", event.Actor, event.Detail)
	case "tease":
		// Format: "😈 alice teased bob (sup)"
		if event.Detail != "" {
			return fmt.Sprintf("😈 %s teased %s (%s)", event.Actor, event.Target, event.Detail)
		}
		return fmt.Sprintf("😈 %s teased %s", event.Actor, event.Target)
	default:
		// Use Detail if provided (ledger events use payload.LogFormat())
		if event.Detail != "" {
			return event.Detail
		}

		// Fallback to generic format
		emoji := categoryEmoji[event.Category]
		if emoji == "" {
			emoji = "•"
		}
		if event.Target != "" && event.Count > 0 {
			return fmt.Sprintf("%s %s → %s (%d)", emoji, event.Actor, event.Target, event.Count)
		} else if event.Target != "" {
			return fmt.Sprintf("%s %s → %s", emoji, event.Actor, event.Target)
		} else if event.Count > 0 {
			return fmt.Sprintf("%s %s (%d)", emoji, event.Actor, event.Count)
		} else if event.Actor != "" {
			return fmt.Sprintf("%s %s", emoji, event.Actor)
		}
		return fmt.Sprintf("%s %s", emoji, event.Type)
	}
}

// addToBatch adds an event to the batch for later aggregation
func (ls *LogService) addToBatch(event LogEvent) {
	ls.batchMu.Lock()
	defer ls.batchMu.Unlock()

	ls.batch[event.Type] = append(ls.batch[event.Type], event)
}

// logInstant queues an event for immediate output (appears in next flush)
func (ls *LogService) logInstant(event LogEvent) {
	emoji := categoryEmoji[event.Category]
	if emoji == "" {
		emoji = "•"
	}

	var msg string
	if event.Detail != "" {
		msg = fmt.Sprintf("%s %s", emoji, event.Detail)
	} else if event.Target != "" {
		msg = fmt.Sprintf("%s %s → %s", emoji, event.Actor, event.Target)
	} else {
		msg = fmt.Sprintf("%s %s", emoji, event.Actor)
	}

	ls.instantLogsMu.Lock()
	ls.instantLogs = append(ls.instantLogs, msg)
	ls.instantLogsMu.Unlock()
}

// flushLoop runs the batch flush on interval
func (ls *LogService) flushLoop() {
	defer ls.wg.Done()

	ticker := time.NewTicker(ls.batchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ls.ctx.Done():
			return
		case <-ticker.C:
			ls.flushBatch()
		}
	}
}

// flushBatch outputs all batched data
func (ls *LogService) flushBatch() {
	ls.batchMu.Lock()
	batch := ls.batch
	ls.batch = make(map[string][]LogEvent)
	ls.batchMu.Unlock()

	ls.instantLogsMu.Lock()
	instants := ls.instantLogs
	ls.instantLogs = nil
	ls.instantLogsMu.Unlock()

	var sections []string

	// Add instant logs first
	sections = append(sections, instants...)

	// Format batched events by type
	sections = append(sections, ls.formatBatchedEvents(batch)...)

	// Output grouped sections
	for _, section := range sections {
		logrus.Info(section)
	}
}

// formatBatchedEvents converts batched events into formatted strings
func (ls *LogService) formatBatchedEvents(batch map[string][]LogEvent) []string {
	var sections []string

	// Process each event type
	for eventType, events := range batch {
		if len(events) == 0 {
			continue
		}

		switch eventType {
		case "welcome":
			sections = append(sections, ls.formatWelcomes(events)...)
		case "goodbye":
			sections = append(sections, ls.formatGoodbyes(events))
		case "gossip-merge":
			sections = append(sections, ls.formatGossipMerges(events))
		case "mesh-sync":
			sections = append(sections, ls.formatMeshSyncs(events))
		case "http":
			// HTTP requests now logged at debug level, not batched
		case "howdy-for-me":
			sections = append(sections, ls.formatHowdysForMe(events))
		case "dm-received":
			sections = append(sections, ls.formatDMsReceived(events))
		case "discovery":
			sections = append(sections, ls.formatDiscoveries(events))
		case "observed-howdy":
			// Only show if significant volume
			if len(events) > 5 {
				sections = append(sections, fmt.Sprintf("👀 witnessed %d howdy exchanges", len(events)))
			}
		case "peer-resolution-failed":
			sections = append(sections, ls.formatPeerResolutionFailed(events))
		case "boot-sync-request", "mesh-verified", "seen":
			// Don't output in normal mode - verbose only
		case "observed":
			sections = append(sections, ls.formatObserved(events))
		case "social-gossip":
			sections = append(sections, ls.formatSocialGossip(events))
		case "tease":
			sections = append(sections, ls.formatTeases(events))
		case "consensus":
			sections = append(sections, ls.formatConsensus(events))
		case "newspaper":
			sections = append(sections, ls.formatNewspapers(events))
		case "ping-received":
			sections = append(sections, ls.formatPingsReceived(events))
		case "boot-info":
			sections = append(sections, ls.formatBootInfo(events)...)
		case "barrio-movement":
			sections = append(sections, ls.formatBarrioMovements(events))
		default:
			// Generic formatting for unknown types
			if len(events) == 1 {
				e := events[0]
				if e.Detail != "" {
					sections = append(sections, fmt.Sprintf("%s %s", categoryEmoji[e.Category], e.Detail))
				}
			} else {
				sections = append(sections, fmt.Sprintf("%s %d %s events", categoryEmoji[events[0].Category], len(events), eventType))
			}
		}
	}

	return sections
}

// formatWelcomes formats welcome events: "👋 bart popped in (raccoon, lily waved)"
func (ls *LogService) formatWelcomes(events []LogEvent) []string {
	// Group by target (who came online)
	byTarget := make(map[string][]string)
	for _, e := range events {
		if e.Target != "" && e.Target != ls.localName.String() {
			byTarget[e.Target] = append(byTarget[e.Target], e.Actor)
		}
	}

	var sections []string
	for target, welcomers := range byTarget {
		unique := uniqueStrings(welcomers)
		if len(unique) == 0 {
			sections = append(sections, fmt.Sprintf("👋 %s popped in", target))
		} else if len(unique) == 1 {
			sections = append(sections, fmt.Sprintf("👋 %s popped in (%s waved)", target, unique[0]))
		} else if len(unique) <= 3 {
			sections = append(sections, fmt.Sprintf("👋 %s popped in (%s waved)", target, strings.Join(unique, ", ")))
		} else {
			sections = append(sections, fmt.Sprintf("👋 %s popped in (%d naras waved)", target, len(unique)))
		}
	}
	return sections
}

// formatGoodbyes formats goodbye events: "💨 mellow-salt-990 bounced"
func (ls *LogService) formatGoodbyes(events []LogEvent) string {
	names := make(map[string]bool)
	for _, e := range events {
		names[e.Actor] = true
	}

	nameList := make([]string, 0, len(names))
	for name := range names {
		nameList = append(nameList, name)
	}
	sort.Strings(nameList)

	if len(nameList) == 1 {
		return fmt.Sprintf("💨 %s bounced", nameList[0])
	} else if len(nameList) <= 3 {
		return fmt.Sprintf("💨 %s bounced", strings.Join(nameList, ", "))
	}
	return fmt.Sprintf("💨 %d naras bounced", len(nameList))
}

// formatGossipMerges formats gossip merge events
func (ls *LogService) formatGossipMerges(events []LogEvent) string {
	totalEvents := 0
	neighbors := make(map[string]bool)
	for _, e := range events {
		totalEvents += e.Count
		neighbors[e.Actor] = true
	}

	if len(neighbors) == 1 {
		for n := range neighbors {
			return fmt.Sprintf("📰 swapped zines with %s (%d events)", n, totalEvents)
		}
	}
	return fmt.Sprintf("📰 swapped zines with %d neighbors (%d events)", len(neighbors), totalEvents)
}

// formatMeshSyncs formats mesh sync events (boot recovery)
func (ls *LogService) formatMeshSyncs(events []LogEvent) string {
	totalEvents := 0
	peers := make(map[string]bool)
	for _, e := range events {
		totalEvents += e.Count
		peers[e.Actor] = true
	}
	return fmt.Sprintf("📦 caught up on %d events from %d peers", totalEvents, len(peers))
}

// formatHowdysForMe formats howdy-for-me events
func (ls *LogService) formatHowdysForMe(events []LogEvent) string {
	senders := make(map[string]bool)
	for _, e := range events {
		senders[e.Actor] = true
	}

	if len(senders) == 1 {
		for s := range senders {
			return fmt.Sprintf("📬 got a howdy from %s", s)
		}
	} else if len(senders) <= 3 {
		names := make([]string, 0, len(senders))
		for s := range senders {
			names = append(names, s)
		}
		sort.Strings(names)
		return fmt.Sprintf("📬 got howdys from %s", strings.Join(names, ", "))
	}
	return fmt.Sprintf("📬 got howdys from %d naras", len(senders))
}

// formatDMsReceived formats dm-received events
func (ls *LogService) formatDMsReceived(events []LogEvent) string {
	senders := make(map[string]int)
	for _, e := range events {
		senders[e.Actor]++
	}

	if len(senders) == 1 {
		for s, count := range senders {
			if count == 1 {
				return fmt.Sprintf("📬 got a DM from %s", s)
			}
			return fmt.Sprintf("📬 got %d DMs from %s", count, s)
		}
	}

	total := len(events)
	return fmt.Sprintf("📬 got %d DMs from %d naras", total, len(senders))
}

// formatDiscoveries formats discovery events
func (ls *LogService) formatDiscoveries(events []LogEvent) string {
	names := make([]string, 0, len(events))
	for _, e := range events {
		names = append(names, e.Actor)
	}

	if len(names) == 1 {
		return fmt.Sprintf("📡 discovered %s", names[0])
	} else if len(names) <= 3 {
		return fmt.Sprintf("📡 discovered %s", strings.Join(names, ", "))
	}
	return fmt.Sprintf("📡 discovered %d new naras", len(names))
}

// formatPeerResolutionFailed formats peer resolution failure events
func (ls *LogService) formatPeerResolutionFailed(events []LogEvent) string {
	names := make([]string, 0, len(events))
	seen := make(map[string]bool)
	for _, e := range events {
		if !seen[e.Actor] {
			seen[e.Actor] = true
			names = append(names, e.Actor)
		}
	}

	if len(names) == 1 {
		return fmt.Sprintf("⚠️ couldn't resolve %s", names[0])
	} else if len(names) <= 3 {
		return fmt.Sprintf("⚠️ couldn't resolve %s", strings.Join(names, ", "))
	}
	return fmt.Sprintf("⚠️ couldn't resolve %d peers", len(names))
}

// formatConsensus formats consensus events
func (ls *LogService) formatConsensus(events []LogEvent) string {
	subjects := make(map[string]bool)
	for _, e := range events {
		subjects[e.Actor] = true
	}
	return fmt.Sprintf("🧠 formed consensus for %d naras", len(subjects))
}

// formatNewspapers formats newspaper events with summary of changes
func (ls *LogService) formatNewspapers(events []LogEvent) string {
	// Group by sender and collect changes
	bySender := make(map[string][]string)
	for _, e := range events {
		if e.Detail != "" {
			bySender[e.Actor] = append(bySender[e.Actor], e.Detail)
		} else {
			bySender[e.Actor] = append(bySender[e.Actor], "")
		}
	}

	// If single sender with changes, show details
	if len(bySender) == 1 {
		for s, changes := range bySender {
			if len(changes) == 1 && changes[0] != "" {
				return fmt.Sprintf("📰 got newspaper from %s (%s)", s, changes[0])
			} else if len(changes) == 1 {
				return fmt.Sprintf("📰 got newspaper from %s", s)
			}
			// Multiple newspapers from same sender
			nonEmpty := 0
			for _, c := range changes {
				if c != "" {
					nonEmpty++
				}
			}
			if nonEmpty > 0 {
				return fmt.Sprintf("📰 got %d newspapers from %s (%d with changes)", len(changes), s, nonEmpty)
			}
			return fmt.Sprintf("📰 got %d newspapers from %s", len(changes), s)
		}
	}

	// Multiple senders - show summary
	totalWithChanges := 0
	for _, changes := range bySender {
		for _, c := range changes {
			if c != "" {
				totalWithChanges++
			}
		}
	}
	if totalWithChanges > 0 {
		return fmt.Sprintf("📰 got %d newspapers from %d neighbors (%d with changes)", len(events), len(bySender), totalWithChanges)
	}
	return fmt.Sprintf("📰 got %d newspapers from %d neighbors", len(events), len(bySender))
}

// formatPingsReceived formats ping-received events
func (ls *LogService) formatPingsReceived(events []LogEvent) string {
	senders := make(map[string]bool)
	for _, e := range events {
		senders[e.Actor] = true
	}
	if len(senders) <= 3 {
		names := make([]string, 0, len(senders))
		for s := range senders {
			names = append(names, s)
		}
		sort.Strings(names)
		return fmt.Sprintf("🏓 got pings from %s", strings.Join(names, ", "))
	}
	return fmt.Sprintf("🏓 got pings from %d naras", len(senders))
}

// formatBootInfo formats boot-info events into a compact summary
func (ls *LogService) formatBootInfo(events []LogEvent) []string {
	var sections []string
	for _, e := range events {
		sections = append(sections, fmt.Sprintf("⚙️ %s", e.Detail))
	}
	return sections
}

// formatObserved formats observed social events
func (ls *LogService) formatObserved(events []LogEvent) string {
	if len(events) == 1 {
		return events[0].Detail
	}
	observers := make(map[string]bool)
	for _, e := range events {
		observers[e.Actor] = true
	}
	return fmt.Sprintf("👁️ %d observations from %d naras", len(events), len(observers))
}

// formatSocialGossip formats social gossip events
func (ls *LogService) formatSocialGossip(events []LogEvent) string {
	if len(events) == 1 {
		return events[0].Detail
	}
	return fmt.Sprintf("🗣️ %d gossip exchanges", len(events))
}

// formatTeases formats tease events: "5 teases ([a,b,c]->r2d2, c->d)"
func (ls *LogService) formatTeases(events []LogEvent) string {
	// Group actors by target
	byTarget := make(map[string][]string)
	for _, e := range events {
		byTarget[e.Target] = append(byTarget[e.Target], e.Actor)
	}

	// Build formatted pairs
	var pairs []string
	for target, actors := range byTarget {
		unique := uniqueStrings(actors)
		if len(unique) == 1 {
			pairs = append(pairs, fmt.Sprintf("%s→%s", unique[0], target))
		} else if len(unique) <= 3 {
			pairs = append(pairs, fmt.Sprintf("[%s]→%s", strings.Join(unique, ","), target))
		} else {
			pairs = append(pairs, fmt.Sprintf("[%d naras]→%s", len(unique), target))
		}
	}
	sort.Strings(pairs)

	count := len(events)
	if count == 1 {
		return fmt.Sprintf("😈 1 tease (%s)", strings.Join(pairs, ", "))
	}
	return fmt.Sprintf("😈 %d teases (%s)", count, strings.Join(pairs, ", "))
}

// transformEvent converts a SyncEvent to a LogEvent using the Payload interface
func (ls *LogService) transformEvent(event SyncEvent) *LogEvent {
	payload := event.Payload()
	if payload == nil {
		return nil
	}

	logEvent := payload.ToLogEvent()
	if logEvent == nil {
		return nil
	}

	// Filter out our own hey-there announcements
	if event.Service == ServiceHeyThere && event.HeyThere != nil {
		if event.HeyThere.From == ls.localName {
			return nil
		}
	}

	return logEvent
}

// categoryEmoji maps categories to their emoji prefix
var categoryEmoji = map[LogCategory]string{
	CategoryPresence: "👋",
	CategoryGossip:   "📰",
	CategorySocial:   "😈",
	CategoryHTTP:     "🌐",
	CategoryStash:    "📦",
	CategorySystem:   "⚙️",
	CategoryMesh:     "🕸️",
}

// --- Convenience methods for manual logging ---

// Info logs an informational message immediately
func (ls *LogService) Info(category LogCategory, format string, args ...interface{}) {
	ls.Push(LogEvent{
		Category: category,
		Type:     "info",
		Detail:   fmt.Sprintf(format, args...),
		Instant:  true,
	})
}

// Warn logs a warning message immediately (bypasses batching)
func (ls *LogService) Warn(category LogCategory, format string, args ...interface{}) {
	emoji := categoryEmoji[category]
	if emoji == "" {
		emoji = "⚠️"
	}
	msg := fmt.Sprintf(format, args...)
	logrus.Warnf("%s %s", emoji, msg)
}

// Error logs an error message immediately (bypasses batching)
func (ls *LogService) Error(category LogCategory, format string, args ...interface{}) {
	emoji := categoryEmoji[category]
	if emoji == "" {
		emoji = "❌"
	}
	msg := fmt.Sprintf(format, args...)
	logrus.Errorf("%s %s", emoji, msg)
}

// --- Batch helper methods for non-ledger events ---

// BatchHTTP records an HTTP request at debug level (not batched, moved to debug)
func (ls *LogService) BatchHTTP(method, path string, status int) {
	// Log HTTP requests at debug level only
	logrus.Debugf("🌐 %s %s (%d)", method, path, status)
}

// BatchGossipMerge records a gossip merge for batched output
func (ls *LogService) BatchGossipMerge(from NaraName, eventCount int) {
	ls.Push(LogEvent{
		Category: CategoryGossip,
		Type:     "gossip-merge",
		Actor:    from.String(),
		Count:    eventCount,
		GroupFormat: func(actors string) string {
			return fmt.Sprintf("📰 merged events from %s", actors)
		},
	})
}

// BatchMeshSync records a mesh sync for batched output
func (ls *LogService) BatchMeshSync(from NaraName, eventCount int) {
	ls.Push(LogEvent{
		Category: CategoryMesh,
		Type:     "mesh-sync",
		Actor:    from.String(),
		Count:    eventCount,
		GroupFormat: func(actors string) string {
			return fmt.Sprintf("📦 synced events from %s", actors)
		},
	})
}

// BatchHowdyForMe records a howdy directed at us for batched output
func (ls *LogService) BatchHowdyForMe(from NaraName) {
	ls.Push(LogEvent{
		Category: CategoryPresence,
		Type:     "howdy-for-me",
		Actor:    from.String(),
		GroupFormat: func(actors string) string {
			return fmt.Sprintf("📬 got howdy from %s", actors)
		},
	})
}

// BatchDMReceived records a received DM for batched output
func (ls *LogService) BatchDMReceived(from NaraName) {
	ls.Push(LogEvent{
		Category: CategorySocial,
		Type:     "dm-received",
		Actor:    from.String(),
		GroupFormat: func(actors string) string {
			return fmt.Sprintf("📬 got DM from %s", actors)
		},
	})
}

// BatchDiscovery records a discovered nara for batched output
func (ls *LogService) BatchDiscovery(name NaraName) {
	ls.Push(LogEvent{
		Category: CategoryMesh,
		Type:     "discovery",
		Actor:    name.String(),
		GroupFormat: func(actors string) string {
			return fmt.Sprintf("📡 discovered %s on mesh", actors)
		},
	})
}

// BatchObservedHowdy records an observed howdy for batched output
func (ls *LogService) BatchObservedHowdy(observer NaraName, target NaraName) {
	t := target
	ls.Push(LogEvent{
		Category: CategoryPresence,
		Type:     "observed-howdy",
		Actor:    observer.String(),
		Target:   target.String(),
		GroupFormat: func(actors string) string {
			return fmt.Sprintf("👀 %s observed howdy from %s", actors, t)
		},
	})
}

// BatchPeerResolutionFailed records a failed peer resolution for batched output
func (ls *LogService) BatchPeerResolutionFailed(name NaraName) {
	ls.Push(LogEvent{
		Category: CategoryMesh,
		Type:     "peer-resolution-failed",
		Actor:    name.String(),
		GroupFormat: func(actors string) string {
			return fmt.Sprintf("⚠️ couldn't resolve peer %s", actors)
		},
	})
}

// BatchBootSyncRequest records a boot sync request for batched output
func (ls *LogService) BatchBootSyncRequest(name NaraName, eventsRequested int) {
	ls.Push(LogEvent{
		Category: CategoryMesh,
		Type:     "boot-sync-request",
		Actor:    name.String(),
		Count:    eventsRequested,
	})
}

// BatchMeshVerified records a verified mesh response for batched output
func (ls *LogService) BatchMeshVerified(name NaraName) {
	ls.Push(LogEvent{
		Category: CategoryMesh,
		Type:     "mesh-verified",
		Actor:    name.String(),
	})
}

// BatchConsensus records a consensus calculation for batched output
func (ls *LogService) BatchConsensus(subject, consensusType string, observers int, result int64) {
	ls.Push(LogEvent{
		Category: CategorySystem,
		Type:     "consensus",
		Actor:    subject,
		Detail:   fmt.Sprintf("%s %s: %d observers → %d", consensusType, subject, observers, result),
		Count:    observers,
	})
}

// BatchNewspaper records a received newspaper for batched output
func (ls *LogService) BatchNewspaper(from string, changes string) {
	c := changes
	ls.Push(LogEvent{
		Category: CategoryGossip,
		Type:     "newspaper",
		Actor:    from,
		Detail:   changes,
		GroupFormat: func(actors string) string {
			if c != "" {
				return fmt.Sprintf("📰 %s posted newspapers (%s)", actors, c)
			}
			return fmt.Sprintf("📰 %s posted newspapers", actors)
		},
	})
}

// BatchPingsReceived records pings received for batched output
func (ls *LogService) BatchPingsReceived(from string) {
	ls.Push(LogEvent{
		Category: CategoryMesh,
		Type:     "ping-received",
		Actor:    from,
		GroupFormat: func(actors string) string {
			return fmt.Sprintf("🏓 got ping from %s", actors)
		},
	})
}

// BatchBootInfo records boot info for batched output
func (ls *LogService) BatchBootInfo(key, value string) {
	ls.Push(LogEvent{
		Category: CategorySystem,
		Type:     "boot-info",
		Actor:    key,
		Detail:   value,
	})
}

// BatchBarrioMovement records a barrio movement for batched output
func (ls *LogService) BatchBarrioMovement(name NaraName, oldCluster, newCluster, emoji, method string, gridSize float64) {
	detail := fmt.Sprintf("%s→%s %s (via %s, grid=%.0f)", oldCluster, newCluster, emoji, method, gridSize)
	ls.Push(LogEvent{
		Category: CategorySystem,
		Type:     "barrio-movement",
		Actor:    name.String(),
		Target:   oldCluster,
		Detail:   detail,
		GroupFormat: func(actors string) string {
			return fmt.Sprintf("🏘️  %s moved barrio: %s", actors, detail)
		},
	})
}

// formatBarrioMovements formats barrio movement events
func (ls *LogService) formatBarrioMovements(events []LogEvent) string {
	if len(events) == 1 {
		e := events[0]
		return fmt.Sprintf("🏘️  %s moved barrio: %s", e.Actor, e.Detail)
	}
	// Multiple movements - show summary
	names := make([]string, 0, len(events))
	for _, e := range events {
		names = append(names, e.Actor)
	}
	if len(names) <= 3 {
		return fmt.Sprintf("🏘️  %d barrio movements (%s)", len(events), strings.Join(names, ", "))
	}
	return fmt.Sprintf("🏘️  %d barrio movements", len(events))
}

// uniqueStrings returns unique strings from a slice, preserving order
func uniqueStrings(s []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(s))
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}
