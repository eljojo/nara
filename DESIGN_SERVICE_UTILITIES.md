# Service Utilities Design

Opt-in utilities that services can use. These are NOT baked into the runtime - services choose which ones they need. This follows the "DirectX" pattern: the runtime provides primitives, utilities provide convenience.

---

## Philosophy

**Baked-in vs Opt-in:**
- **Baked-in:** Pipeline stages, message routing, transport - runtime handles these
- **Opt-in:** Request correlation, rate limiting, encryption, retry logic - services import if needed

**Why opt-in?**
1. Services have different needs (stash needs encryption, social doesn't)
2. Keeps runtime small and focused
3. Easier to test services in isolation
4. Can evolve utilities without touching runtime

---

## Phase 5 Utilities (Needed for Stash)

### 1. ~~Correlator~~ → CallRegistry (Request/Response)

> **IMPLEMENTED:** Correlator has been replaced by `CallRegistry` in the runtime.
> Services now use `rt.Call(msg, timeout)` instead of a separate Correlator utility.
> See `runtime/interfaces.go` for CallRegistry implementation.

**Purpose:** Track pending requests and match responses.

**Current Problem:**
```go
// stash_types.go:182-188 - ConfidantTracker has manual correlation
type ConfidantTracker struct {
    confidants  map[string]int64 // name -> timestamp they confirmed (acked)
    pending     map[string]int64 // name -> timestamp we sent (awaiting ack)
    failed      map[string]int64 // name -> timestamp of last failure
    // ...
}
```

Also in `checkpoint_service.go:46-57`:
```go
type pendingProposal struct {
    proposal    *CheckpointProposal
    votes       []*CheckpointVote
    expiresAt   time.Time
    // ...
}
```

**Proposed Utility:**
```go
// Generic request/response correlation with timeouts
type Correlator[Resp any] struct {
    pending  map[string]*pendingRequest[Resp]
    mu       sync.Mutex
    timeout  time.Duration
}

type pendingRequest[Resp any] struct {
    ch        chan Result[Resp]
    expiresAt time.Time
}

type Result[T any] struct {
    Value T
    Err   error
}

func NewCorrelator[Resp any](timeout time.Duration) *Correlator[Resp]
func (c *Correlator[Resp]) Send(rt *Runtime, msg *Message) <-chan Result[Resp]
func (c *Correlator[Resp]) Receive(requestID string, resp Resp) bool
func (c *Correlator[Resp]) startReaper()  // Background cleanup
```

**Stash Usage:**
```go
type StashService struct {
    storeCorrelator *Correlator[StashStoreAck]
    // ...
}

func (s *StashService) StoreWith(confidant string, data []byte) error {
    msg := &Message{Kind: "stash:store", To: confidant, ...}
    result := <-s.storeCorrelator.Send(s.rt, msg)
    if result.Err != nil {
        return result.Err  // Timeout or failure
    }
    // Handle result.Value (the ack)
}

func (s *StashService) HandleStoreAck(msg *Message) {
    var ack StashStoreAck
    json.Unmarshal(msg.Payload, &ack)
    s.storeCorrelator.Receive(msg.InReplyTo, ack)
}
```

**Replaces:**
- `stash_types.go:182-270` - ConfidantTracker pending/timeout logic
- `checkpoint_service.go:46-57` - pendingProposal vote collection


### 3. RateLimiter (Per-Key Throttling)

**Purpose:** Limit operations per key within a time window.

**Current Implementations:**

1. `sync_ledger.go:12-84` - ObservationRateLimit (per-subject):
```go
type ObservationRateLimit struct {
    SubjectCounts map[string][]int64 // subject -> timestamps of events
    WindowSec     int64              // Time window in seconds
    MaxEvents     int                // Max events per subject per window
    mu            sync.Mutex
}
```

2. `observations.go:12-28` - verifyPingRateLimit (per-peer with cached result):
```go
var (
    verifyPingLastAttempt   = make(map[string]int64)
    verifyPingLastResult    = make(map[string]bool)
    verifyPingLastAttemptMu sync.Mutex
)
```

**Problem:** Two different implementations, both ad-hoc.

**Proposed Utility:**
```go
// utilities/ratelimiter.go
package utilities

type RateLimiter struct {
    window   time.Duration
    maxCount int
    entries  map[string]*entry
    mu       sync.Mutex
}

type entry struct {
    timestamps []int64
    lastResult any  // Optional: cache last result
}

func NewRateLimiter(window time.Duration, maxCount int) *RateLimiter

// Check if key is allowed (doesn't record)
func (rl *RateLimiter) Check(key string) bool

// Check and record if allowed
func (rl *RateLimiter) Allow(key string) bool

// Check with cached result fallback
// If rate-limited, returns cached result instead of "denied"
func (rl *RateLimiter) AllowOrCached(key string) (allowed bool, cached any, hasCached bool)

// Record a result to cache (for AllowOrCached)
func (rl *RateLimiter) SetCachedResult(key string, result any)

// Cleanup stale entries (call periodically)
func (rl *RateLimiter) Cleanup()

// Get stats for debugging
func (rl *RateLimiter) Stats() map[string]int
```

**Current Usages to Replace:**

1. `sync_ledger.go:12-84` - ObservationRateLimit:
```go
// Before
rl := NewObservationRateLimit()
if !rl.CheckAndAdd(subject, timestamp) {
    return false // rate limited
}

// After
rl := utilities.NewRateLimiter(5*time.Minute, 10)
if !rl.Allow(subject) {
    return false
}
```

2. `observations.go:735-754` - Ping verification:
```go
// Before (observations.go:741-754)
verifyPingLastAttemptMu.Lock()
lastAttempt := verifyPingLastAttempt[name]
lastResult := verifyPingLastResult[name]
now := time.Now().Unix()
if now-lastAttempt < 60 {
    verifyPingLastAttemptMu.Unlock()
    return lastResult  // Return cached result
}
verifyPingLastAttempt[name] = now
verifyPingLastAttemptMu.Unlock()

// After
allowed, cached, hasCached := pingRateLimiter.AllowOrCached(name)
if !allowed && hasCached {
    return cached.(bool)  // Return cached result
}
if !allowed {
    return false
}
// ... do ping ...
pingRateLimiter.SetCachedResult(name, success)
```

**Stash Usage:**
```go
// Rate limit stash requests per owner
stashRequestLimiter := utilities.NewRateLimiter(1*time.Minute, 5)

func (s *StashService) HandleRequest(msg *Message) {
    owner := msg.From
    if !s.stashRequestLimiter.Allow(owner) {
        return // Too many requests
    }
    // Process request...
}
```

---

### 4. Logger (Revamped LogService)

**Purpose:** Unified logging for the runtime with service-awareness, smart batching, and per-service control built in from the start.

**Current Problems with `logservice.go`:**
1. **30+ `BatchXxx()` methods** - Each event type has its own method (`BatchGossipMerge`, `BatchDMReceived`, etc.)
2. **Hardcoded formatting** - `formatTeases()`, `formatWelcomes()` etc. baked into LogService
3. **No service awareness** - Logs don't know which service emitted them
4. **Global verbose flag** - Can't debug one service without flooding console
5. **Tightly coupled to Network** - `network.logService.BatchXxx()` calls everywhere

**New Design: Service-First Logger**

The runtime provides a `Logger` that services request by name. Each service gets its own logger with independent controls.

```go
// runtime/logger.go
package runtime

// Logger is the central logging coordinator
type Logger struct {
    services     map[string]*ServiceLog  // Per-service loggers
    output       chan LogEntry           // Single output channel
    batchWindow  time.Duration           // How long to batch (default 3s)
    mu           sync.RWMutex

    // Global controls
    globalVerbose bool
    disabledServices map[string]bool
}

// ServiceLog is what each service gets
type ServiceLog struct {
    name     string
    emoji    string           // Service emoji (📦 for stash, 😈 for social, etc.)
    logger   *Logger          // Back-reference to parent
    verbose  bool             // Per-service verbose override
    enabled  bool             // Per-service enable/disable
}

// LogEntry is the universal log structure
type LogEntry struct {
    Service   string          // "stash", "social", "presence"
    Level     LogLevel        // Debug, Info, Warn, Error
    EventType string          // "store", "tease", "howdy" - for smart grouping
    Actor     string          // Who did it (for grouping: "alice and bob did X")
    Target    string          // Optional: who it was done to
    Message   string          // Human-readable message
    Count     int             // For pre-aggregated events
    Instant   bool            // Bypass batching
    Time      time.Time
}

type LogLevel int
const (
    LevelDebug LogLevel = iota
    LevelInfo
    LevelWarn
    LevelError
)
```

**Service API - Clean and Simple:**

```go
// Get a logger for your service
func (rt *Runtime) Log(service string) *ServiceLog

// ServiceLog methods - no more BatchXxx() explosion
func (l *ServiceLog) Debug(format string, args ...any)
func (l *ServiceLog) Info(format string, args ...any)
func (l *ServiceLog) Warn(format string, args ...any)
func (l *ServiceLog) Error(format string, args ...any)

// Structured events with smart batching
func (l *ServiceLog) Event(eventType, actor string, opts ...LogOption)

// Options for structured events
func WithTarget(t string) LogOption
func WithMessage(m string) LogOption
func WithCount(n int) LogOption
func Instant() LogOption  // Skip batching
```

**Smart Batching (Keep What Works):**

The current batching logic is good - keep it but make it generic:

```go
// Batching groups events by (service, eventType, target) and aggregates actors
// After batchWindow with no new events, flush and format

// Input over 3 seconds:
//   {Service: "social", EventType: "tease", Actor: "alice", Target: "bob"}
//   {Service: "social", EventType: "tease", Actor: "carol", Target: "bob"}
//   {Service: "social", EventType: "tease", Actor: "dave", Target: "bob"}

// Output (single line):
//   😈 [social] alice, carol, and dave teased bob

// The formatting is now pluggable per event type, not hardcoded
```

**Pluggable Formatters:**

```go
// Services register how their events should be formatted
type EventFormatter func(entries []LogEntry) string

func (l *Logger) RegisterFormatter(service, eventType string, f EventFormatter)

// Built-in default: "actor1, actor2, and actor3 did eventType to target"
// Services can override for custom formatting
```

**Runtime Integration:**

```go
func NewRuntime(cfg RuntimeConfig) *Runtime {
    rt := &Runtime{
        logger: NewLogger(LoggerConfig{
            BatchWindow:      3 * time.Second,
            DisabledServices: cfg.DisabledLoggers,
            Verbose:          cfg.Verbose,
        }),
    }
    return rt
}

// Services get their logger on init
func (s *StashService) Init(rt *Runtime) {
    s.log = rt.Log("stash")  // Returns *ServiceLog for "stash"
}
```

**Per-Service Control:**

```go
// Disable a noisy service
rt.Logger().SetServiceEnabled("presence", false)

// Enable verbose for one service (debug it without flooding)
rt.Logger().SetServiceVerbose("stash", true)

// Or via config
RuntimeConfig{
    DisabledLoggers: []string{"presence", "social"},
    VerboseLoggers:  []string{"stash"},
}
```

**Stash Usage:**

```go
type StashService struct {
    rt  *Runtime
    log *ServiceLog
}

func (s *StashService) Init(rt *Runtime) {
    s.rt = rt
    s.log = rt.Log("stash")  // Gets or creates ServiceLog for "stash"
}

func (s *StashService) handleStore(msg *Message) {
    s.log.Debug("received store request from %s", msg.From)

    // ... process ...

    // Structured event - will be batched and grouped with other stores
    s.log.Event("store", msg.From, WithMessage("stored 2.3KB"))
}

func (s *StashService) handleRequest(msg *Message) {
    s.log.Event("request", msg.From)
    // If alice and bob both request within 3s:
    // Output: "📦 [stash] alice and bob requested stash"
}
```

**What Gets Deleted from Current LogService:**

```go
// DELETE all these - replaced by generic Event()
func (ls *LogService) BatchGossipMerge(from string, eventCount int)
func (ls *LogService) BatchMeshSync(from string, eventCount int)
func (ls *LogService) BatchHowdyForMe(from string)
func (ls *LogService) BatchDMReceived(from string)
func (ls *LogService) BatchDiscovery(name string)
func (ls *LogService) BatchObservedHowdy(observer, target string)
func (ls *LogService) BatchPeerResolutionFailed(name string)
func (ls *LogService) BatchBootSyncRequest(name string, eventsRequested int)
func (ls *LogService) BatchMeshVerified(name string)
func (ls *LogService) BatchConsensus(subject, consensusType string, observers int, result int64)
func (ls *LogService) BatchNewspaper(from string, changes string)
func (ls *LogService) BatchPingsReceived(from string)
func (ls *LogService) BatchBootInfo(key, value string)
func (ls *LogService) BatchBarrioMovement(name, oldCluster, newCluster, emoji, method string, gridSize float64)

// DELETE all these - replaced by pluggable formatters
func (ls *LogService) formatWelcomes(events []LogEvent) []string
func (ls *LogService) formatGoodbyes(events []LogEvent) string
func (ls *LogService) formatGossipMerges(events []LogEvent) string
func (ls *LogService) formatMeshSyncs(events []LogEvent) string
func (ls *LogService) formatHowdysForMe(events []LogEvent) string
func (ls *LogService) formatDMsReceived(events []LogEvent) string
func (ls *LogService) formatDiscoveries(events []LogEvent) string
func (ls *LogService) formatPeerResolutionFailed(events []LogEvent) string
func (ls *LogService) formatConsensus(events []LogEvent) string
func (ls *LogService) formatNewspapers(events []LogEvent) string
func (ls *LogService) formatPingsReceived(events []LogEvent) string
func (ls *LogService) formatBootInfo(events []LogEvent) []string
func (ls *LogService) formatObserved(events []LogEvent) string
func (ls *LogService) formatSocialGossip(events []LogEvent) string
func (ls *LogService) formatTeases(events []LogEvent) string
func (ls *LogService) formatBarrioMovements(events []LogEvent) string
```

**What Gets Kept:**

- Batching window logic (3s default)
- Actor aggregation ("alice, bob, and carol")
- Emoji per category
- Instant bypass for important logs
- Verbose mode auto-grouping

**Benefits:**

| Before | After |
|--------|-------|
| 30+ `BatchXxx()` methods | Single `Event()` method |
| Hardcoded formatters | Pluggable per event type |
| Global verbose only | Per-service verbose |
| No service tracking | Service name on every log |
| `network.logService.BatchXxx()` | `s.log.Event()` |

---

## Phase 6+ Utilities (Future)

### 6. RetryWithBackoff

**Purpose:** Retry failed operations with exponential backoff.

**Current Usage:** `stash_types.go:194-196`:
```go
const FailureBackoffTime = 5 * time.Minute

// Used in ConfidantTracker.CleanupExpiredFailures()
cutoff := now - int64(FailureBackoffTime.Seconds())
```

And `boot_recovery.go:58`:
```go
// Retry up to 3 times with backoff if no neighbors found
```

**Proposed Utility:**
```go
type RetryPolicy struct {
    MaxRetries  int
    BaseDelay   time.Duration
    MaxDelay    time.Duration
    Jitter      bool  // Add randomness to prevent thundering herd
}

type Retrier struct {
    policy RetryPolicy
}

func NewRetrier(policy RetryPolicy) *Retrier

// Retry until success or max retries
func (r *Retrier) Do(fn func() error) error

// With context for cancellation
func (r *Retrier) DoWithContext(ctx context.Context, fn func() error) error

// Return the delay for attempt N (for manual control)
func (r *Retrier) DelayFor(attempt int) time.Duration
```

**Example:**
```go
retrier := utilities.NewRetrier(utilities.RetryPolicy{
    MaxRetries: 3,
    BaseDelay:  1 * time.Second,
    MaxDelay:   30 * time.Second,
    Jitter:     true,
})

err := retrier.Do(func() error {
    return s.storeWithConfidant(confidant, data)
})
```

---

### 6. CircuitBreaker

**Purpose:** Fail fast when a peer is consistently unreachable.

**Current Problem:** Stash keeps trying dead confidants until timeout. No circuit breaker.

**Proposed Utility:**
```go
type CircuitBreaker struct {
    failureThreshold int           // Failures before opening
    resetTimeout     time.Duration // How long to stay open
    state            State         // Closed, Open, HalfOpen
    failures         int
    lastFailure      time.Time
    mu               sync.Mutex
}

type State int
const (
    StateClosed State = iota   // Normal operation
    StateOpen                  // Failing fast
    StateHalfOpen              // Testing if recovered
)

func NewCircuitBreaker(threshold int, reset time.Duration) *CircuitBreaker

// Call wraps a function with circuit breaker logic
func (cb *CircuitBreaker) Call(fn func() error) error

// Manual control
func (cb *CircuitBreaker) RecordSuccess()
func (cb *CircuitBreaker) RecordFailure()
func (cb *CircuitBreaker) State() State
func (cb *CircuitBreaker) AllowRequest() bool
```

**Example:**
```go
// Per-peer circuit breakers
breakers := make(map[string]*utilities.CircuitBreaker)

func (s *StashService) storeWithConfidant(name string, data []byte) error {
    cb := s.getOrCreateBreaker(name)

    return cb.Call(func() error {
        return s.doStore(name, data)
    })
}
```

---

### 7. ConsensusHelper (Trimmed Mean)

**Purpose:** Aggregate values from multiple sources with outlier removal.

**Current Implementation:** `math_helpers.go:5-85`:
```go
func TrimmedMeanInt64(values []int64) int64 {
    // Calculates median, removes outliers, returns average
}
```

Used in `checkpoint_service.go:717-721`:
```go
consensus := &round2Values{
    restarts:    TrimmedMeanInt64(restartVals),
    totalUptime: TrimmedMeanInt64(uptimeVals),
    firstSeen:   TrimmedMeanInt64(firstSeenVals),
}
```

**Proposed Utility:** Generalize for any consensus scenario:
```go
type ConsensusHelper[T any] struct {
    votes      map[string]T      // voter -> value
    threshold  int               // Minimum votes needed
    aggregator func([]T) T       // How to combine votes
    mu         sync.Mutex
}

func NewConsensusHelper[T any](threshold int, aggregator func([]T) T) *ConsensusHelper[T]

func (ch *ConsensusHelper[T]) AddVote(voter string, value T)
func (ch *ConsensusHelper[T]) VoteCount() int
func (ch *ConsensusHelper[T]) HasQuorum() bool
func (ch *ConsensusHelper[T]) Result() (T, bool)  // Returns aggregated result if quorum met
func (ch *ConsensusHelper[T]) Reset()

// Built-in aggregators
func TrimmedMeanAggregator(values []int64) int64
func MajorityAggregator[T comparable](values []T) T
```

**Checkpoint Usage:**
```go
type CheckpointService struct {
    restartConsensus *utilities.ConsensusHelper[int64]
    uptimeConsensus  *utilities.ConsensusHelper[int64]
}

func (s *CheckpointService) HandleVote(vote *CheckpointVote) {
    s.restartConsensus.AddVote(vote.Attester, vote.Observation.Restarts)
    s.uptimeConsensus.AddVote(vote.Attester, vote.Observation.TotalUptime)

    if s.restartConsensus.HasQuorum() {
        result, _ := s.restartConsensus.Result()
        // Use result...
    }
}
```

---

### 8. DedupCache

**Purpose:** Track seen items to prevent duplicates.

**Current Implementation:** Handled inline in `sync_ledger.go`:
```go
type SyncLedger struct {
    eventIDs map[string]bool  // Simple set of seen IDs
}
```

**Proposed Utility:** More flexible with TTL:
```go
type DedupCache struct {
    seen map[string]int64  // key -> expiry timestamp
    ttl  time.Duration
    mu   sync.Mutex
}

func NewDedupCache(ttl time.Duration) *DedupCache

// Returns true if NOT seen before (and marks as seen)
func (dc *DedupCache) Check(key string) bool

// Just check without marking
func (dc *DedupCache) Seen(key string) bool

// Manually mark as seen
func (dc *DedupCache) Mark(key string)

// Cleanup expired entries
func (dc *DedupCache) Cleanup()
```

---

### 9. BatchCollector

**Purpose:** Accumulate items and flush on size or time.

**Not currently implemented but would help with:**
- Batching gossip messages
- Batching stash updates
- Batching observation events

**Proposed Utility:**
```go
type BatchCollector[T any] struct {
    items   []T
    maxSize int
    maxWait time.Duration
    onFlush func([]T)
    timer   *time.Timer
    mu      sync.Mutex
}

func NewBatchCollector[T any](size int, wait time.Duration, flush func([]T)) *BatchCollector[T]

func (bc *BatchCollector[T]) Add(item T)
func (bc *BatchCollector[T]) Flush()  // Manual flush
func (bc *BatchCollector[T]) Stop()   // Stop timer, flush remaining
```

---

## Summary: By Phase

| Utility | Current Location | Phase | Priority |
|---------|------------------|-------|----------|
| Correlator | checkpoint_service.go, stash_types.go | 5 | High |
| Encryptor | identity_crypto.go | 5 | High |
| RateLimiter | sync_ledger.go, observations.go | 5 | High |
| Logger | logservice.go → **runtime/logger.go** | 5 | High |
| RetryWithBackoff | stash_types.go, boot_recovery.go | 6 | Medium |
| CircuitBreaker | (not implemented) | 6 | Medium |
| ConsensusHelper | math_helpers.go, checkpoint_service.go | 7 | Medium |
| DedupCache | sync_ledger.go | 6 | Low |
| BatchCollector | (not implemented) | 6 | Low |

---

## Package Structure

```
runtime/
├── logger.go           // Revamped LogService (service-aware, per-service control)
└── ...                 // Other runtime files

utilities/
├── correlator.go       // Request/response correlation
├── encryptor.go        // XChaCha20-Poly1305 encryption
├── ratelimiter.go      // Per-key rate limiting with caching
├── retrier.go          // Exponential backoff retry
├── circuitbreaker.go   // Fail-fast for unreachable peers
├── consensus.go        // Vote aggregation with outlier removal
├── dedup.go            // TTL-based deduplication cache
├── batch.go            // Time/size-based batching
└── utilities_test.go   // Comprehensive tests
```

All utilities are:
- **Generic** where possible (Go 1.18+ generics)
- **Thread-safe** (internal locking)
- **Zero external dependencies** (only stdlib)
- **Independently testable** (no runtime coupling)
