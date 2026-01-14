# Plan C: Core + Plugins Architecture

A detailed vision for restructuring Nara into a small core with pluggable services.

---

## The Vision

Imagine a Nara where:

- **The core is tiny** — just identity, the event ledger, and a lifecycle coordinator
- **Features are plugins** — stash, checkpoints, social, gossip are all optional services
- **Tests are fast** — spin up a nara with only the services you need
- **Adding features is easy** — implement an interface, register it, done
- **Debugging is clear** — each service has explicit boundaries and dependencies

```go
// In a test file
func TestPresenceOnly(t *testing.T) {
    n := core.NewNara("test-nara",
        WithService(presence.New()),
        // no stash, no checkpoints, no social — fast and focused
    )
    defer n.Shutdown()
    // test presence behavior in isolation
}

// In production main.go
func main() {
    n := core.NewNara(name,
        WithService(presence.New()),
        WithService(gossip.New()),
        WithService(stash.New()),
        WithService(social.New()),
        WithService(checkpoint.New()),
        WithTransport(mqtt.New(broker)),
        WithTransport(mesh.New(tsnet)),
        WithHTTP(api.New(), ui.New()),
    )
    n.Run()
}
```

---

## Directory Structure

```
nara/
├── core/
│   ├── nara.go              # LocalNara: identity, soul, personality
│   ├── ledger.go            # SyncLedger: the event store
│   ├── event.go             # SyncEvent, event types, payloads
│   ├── projection.go        # ProjectionStore: derived state
│   ├── coordinator.go       # Lifecycle: start, shutdown, wiring
│   ├── service.go           # Service interface definition
│   ├── hooks.go             # Event hooks / subscriptions
│   └── peers.go             # PeerRegistry: who's in the neighbourhood
│
├── services/
│   ├── presence/
│   │   ├── service.go       # PresenceService implementation
│   │   ├── heythere.go      # Hey-there announcements
│   │   ├── howdy.go         # Howdy responses
│   │   ├── chau.go          # Goodbye announcements
│   │   └── newspaper.go     # Presence aggregation
│   │
│   ├── gossip/
│   │   ├── service.go       # GossipService implementation
│   │   ├── zine.go          # Zine creation and parsing
│   │   ├── exchange.go      # Zine exchange protocol
│   │   └── discovery.go     # Peer discovery
│   │
│   ├── stash/
│   │   ├── service.go       # StashService implementation
│   │   ├── manager.go       # Stash lifecycle
│   │   ├── confidant.go     # Confidant selection
│   │   └── encryption.go    # XChaCha20 encryption
│   │
│   ├── social/
│   │   ├── service.go       # SocialService implementation
│   │   ├── tease.go         # Teasing logic
│   │   ├── clout.go         # Reputation scoring
│   │   └── trends.go        # Fashion/trends
│   │
│   ├── checkpoint/
│   │   ├── service.go       # CheckpointService implementation
│   │   ├── proposal.go      # Checkpoint proposals
│   │   ├── voting.go        # Voting rounds
│   │   └── consensus.go     # Trimmed mean consensus
│   │
│   ├── neighbourhood/
│   │   ├── service.go       # NeighbourhoodService implementation
│   │   ├── tracking.go      # Peer tracking
│   │   ├── observations.go  # Observation events
│   │   └── opinion.go       # Opinion derivation
│   │
│   └── world/
│       ├── service.go       # WorldService (postcards/journeys)
│       ├── message.go       # Journey message type
│       └── handler.go       # Journey completion
│
├── transport/
│   ├── transport.go         # Transport interface
│   ├── mqtt/
│   │   ├── transport.go     # MQTT transport implementation
│   │   ├── handlers.go      # Topic subscriptions
│   │   └── publish.go       # Publishing helpers
│   └── mesh/
│       ├── transport.go     # Mesh transport implementation
│       ├── client.go        # HTTP mesh client (existing)
│       └── auth.go          # Mesh authentication
│
├── http/
│   ├── server.go            # HTTP server setup
│   ├── api/
│   │   ├── handler.go       # API endpoints
│   │   ├── events.go        # Event endpoints
│   │   └── projections.go   # Projection endpoints
│   ├── ui/
│   │   ├── handler.go       # UI serving
│   │   └── templates.go     # HTML templates
│   └── mesh/
│       ├── handler.go       # Mesh HTTP handlers
│       ├── sync.go          # Sync endpoints
│       └── stash.go         # Stash exchange endpoints
│
├── identity/
│   ├── soul.go              # Soul generation (standalone)
│   ├── crypto.go            # Ed25519 keypairs
│   ├── attestation.go       # Identity attestation
│   └── detection.go         # Identity discovery
│
├── boot/
│   ├── recovery.go          # Boot recovery orchestration
│   ├── sync.go              # Initial sync
│   └── backfill.go          # Ledger backfill
│
└── cmd/
    └── nara/
        └── main.go          # Production entry point
```

---

## Core Interfaces

### The Service Interface

Every feature implements this:

```go
// core/service.go
package core

type Service interface {
    // Name returns the service identifier (e.g., "presence", "stash")
    Name() string

    // Init is called once with access to core dependencies
    Init(deps ServiceDeps) error

    // Start begins the service's background work
    Start(ctx context.Context) error

    // Stop gracefully shuts down the service
    Stop() error
}

// ServiceDeps provides what services need from core
type ServiceDeps interface {
    // Identity
    Me() *Nara
    Soul() string
    Keypair() NaraKeypair

    // Event store
    Ledger() *SyncLedger
    Projections() *ProjectionStore

    // Peer awareness
    Peers() PeerRegistry

    // Event subscription
    Subscribe(eventType string, handler EventHandler)

    // Event emission
    Emit(event *SyncEvent)

    // Service discovery (optional dependencies)
    GetService(name string) (Service, bool)

    // Transport access
    Transport() TransportLayer
}
```

### Event Subscription

Services subscribe to events they care about:

```go
// core/hooks.go
type EventHandler func(event *SyncEvent)

type EventBus interface {
    Subscribe(eventType string, handler EventHandler)
    SubscribeAll(handler EventHandler)  // for gossip, logging
    Emit(event *SyncEvent)
}
```

### Transport Layer

Abstraction over MQTT and mesh:

```go
// transport/transport.go
type Transport interface {
    Name() string
    Start(ctx context.Context) error
    Stop() error

    // Publishing
    Broadcast(topic string, payload []byte) error
    SendTo(peer string, path string, payload []byte) ([]byte, error)

    // Subscriptions (transport routes to event bus)
    OnMessage(handler func(topic string, payload []byte))
}

type TransportLayer interface {
    Broadcast(topic string, payload []byte) error
    SendTo(peer string, path string, payload []byte) ([]byte, error)
    GetPeerAddress(name string) (string, bool)
}
```

### Peer Registry

Who's out there:

```go
// core/peers.go
type PeerRegistry interface {
    // Query
    Get(name string) (*Nara, bool)
    GetOnline() []*Nara
    GetAll() []*Nara
    Names() []string

    // Mutation (from presence service)
    Register(nara *Nara)
    UpdateStatus(name string, status PeerStatus)
    Remove(name string)

    // Mesh info
    GetMeshIP(name string) (string, bool)
    GetPublicKey(name string) ([]byte, bool)
}
```

---

## How Services Work

### Example: Presence Service

```go
// services/presence/service.go
package presence

type Service struct {
    deps   core.ServiceDeps
    ctx    context.Context
    cancel context.CancelFunc
}

func New() *Service {
    return &Service{}
}

func (s *Service) Name() string { return "presence" }

func (s *Service) Init(deps core.ServiceDeps) error {
    s.deps = deps

    // Subscribe to events we care about
    deps.Subscribe("hey-there", s.handleHeyThere)
    deps.Subscribe("chau", s.handleChau)

    return nil
}

func (s *Service) Start(ctx context.Context) error {
    s.ctx, s.cancel = context.WithCancel(ctx)

    // Start background loops
    go s.announceLoop()
    go s.howdyLoop()

    return nil
}

func (s *Service) Stop() error {
    s.cancel()
    return nil
}

// Event handlers
func (s *Service) handleHeyThere(event *core.SyncEvent) {
    payload := event.Payload.(*core.HeyTherePayload)

    // Register peer in registry
    s.deps.Peers().Register(&core.Nara{
        Name:      payload.Name,
        ID:        payload.ID,
        PublicKey: payload.PublicKey,
        // ...
    })
}

// Background work
func (s *Service) announceLoop() {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-s.ctx.Done():
            return
        case <-ticker.C:
            s.announce()
        }
    }
}

func (s *Service) announce() {
    event := core.NewSyncEvent("hey-there", &core.HeyTherePayload{
        Name:      s.deps.Me().Name,
        ID:        s.deps.Me().ID,
        PublicKey: s.deps.Keypair().PublicKey,
        // ...
    })
    s.deps.Emit(event)
}
```

### Example: Stash Service (with optional dependency)

```go
// services/stash/service.go
package stash

type Service struct {
    deps       core.ServiceDeps
    presence   *presence.Service  // optional dependency
}

func (s *Service) Init(deps core.ServiceDeps) error {
    s.deps = deps

    // Optional: get presence service for peer selection
    if svc, ok := deps.GetService("presence"); ok {
        s.presence = svc.(*presence.Service)
    }

    deps.Subscribe("stash-request", s.handleStashRequest)
    deps.Subscribe("stash-response", s.handleStashResponse)

    return nil
}

func (s *Service) selectConfidants() []*core.Nara {
    // Use peer registry (always available)
    peers := s.deps.Peers().GetOnline()

    // Filter by criteria (memory, uptime, etc.)
    // ...

    return selected
}
```

### Example: Gossip Service (subscribes to ALL events)

```go
// services/gossip/service.go
package gossip

type Service struct {
    deps        core.ServiceDeps
    recentEvents []*core.SyncEvent
    mu          sync.Mutex
}

func (s *Service) Init(deps core.ServiceDeps) error {
    s.deps = deps

    // Gossip needs to see everything to build zines
    deps.SubscribeAll(s.collectEvent)

    return nil
}

func (s *Service) collectEvent(event *core.SyncEvent) {
    s.mu.Lock()
    defer s.mu.Unlock()

    // Store for next zine
    s.recentEvents = append(s.recentEvents, event)
}

func (s *Service) createZine() *Zine {
    s.mu.Lock()
    events := s.recentEvents
    s.recentEvents = nil
    s.mu.Unlock()

    // Filter by personality, create zine
    return &Zine{Events: s.filterByPersonality(events)}
}
```

---

## How Coordinator Wires Everything

```go
// core/coordinator.go
package core

type Coordinator struct {
    nara        *LocalNara
    ledger      *SyncLedger
    projections *ProjectionStore
    peers       *peerRegistry
    eventBus    *eventBus

    services    []Service
    transports  []Transport
    httpServer  *http.Server

    ctx    context.Context
    cancel context.CancelFunc
}

func NewNara(name string, opts ...Option) *Coordinator {
    c := &Coordinator{
        nara:        newLocalNara(name),
        ledger:      NewSyncLedger(),
        projections: NewProjectionStore(),
        peers:       newPeerRegistry(),
        eventBus:    newEventBus(),
    }

    for _, opt := range opts {
        opt(c)
    }

    return c
}

// Options for composition
func WithService(s Service) Option {
    return func(c *Coordinator) {
        c.services = append(c.services, s)
    }
}

func WithTransport(t Transport) Option {
    return func(c *Coordinator) {
        c.transports = append(c.transports, t)
    }
}

func WithHTTP(handlers ...http.Handler) Option {
    return func(c *Coordinator) {
        c.httpHandlers = append(c.httpHandlers, handlers...)
    }
}

// Lifecycle
func (c *Coordinator) Run() error {
    c.ctx, c.cancel = context.WithCancel(context.Background())

    // 1. Initialize all services (lets them subscribe to events)
    deps := c.newServiceDeps()
    for _, svc := range c.services {
        if err := svc.Init(deps); err != nil {
            return fmt.Errorf("init %s: %w", svc.Name(), err)
        }
    }

    // 2. Start transports (connects to MQTT, mesh)
    for _, t := range c.transports {
        t.OnMessage(c.routeIncomingMessage)
        if err := t.Start(c.ctx); err != nil {
            return fmt.Errorf("start transport %s: %w", t.Name(), err)
        }
    }

    // 3. Start services (background loops)
    for _, svc := range c.services {
        if err := svc.Start(c.ctx); err != nil {
            return fmt.Errorf("start %s: %w", svc.Name(), err)
        }
    }

    // 4. Start HTTP server
    if c.httpServer != nil {
        go c.httpServer.ListenAndServe()
    }

    // 5. Wait for shutdown signal
    <-c.ctx.Done()
    return c.shutdown()
}

func (c *Coordinator) shutdown() error {
    // Stop in reverse order
    for i := len(c.services) - 1; i >= 0; i-- {
        c.services[i].Stop()
    }
    for _, t := range c.transports {
        t.Stop()
    }
    return nil
}

// Event routing
func (c *Coordinator) routeIncomingMessage(topic string, payload []byte) {
    event, err := ParseSyncEvent(payload)
    if err != nil {
        return
    }

    // Store in ledger
    c.ledger.Add(event)

    // Update projections
    c.projections.Apply(event)

    // Notify subscribers
    c.eventBus.Emit(event)
}
```

---

## Test Scenarios

### Testing Presence in Isolation

```go
func TestPresenceAnnouncement(t *testing.T) {
    // Minimal nara with just presence
    n := core.NewNara("test-nara",
        WithService(presence.New()),
        WithTransport(mock.NewTransport()),
    )

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    go n.Run()
    defer n.Shutdown()

    // Verify hey-there was broadcast
    transport := n.Transport().(*mock.Transport)
    msg := transport.WaitForBroadcast(t, "nara/hey-there", 2*time.Second)

    assert.Contains(t, string(msg), "test-nara")
}
```

### Testing Gossip Without Stash

```go
func TestGossipZineCreation(t *testing.T) {
    n := core.NewNara("test-nara",
        WithService(presence.New()),
        WithService(gossip.New()),
        // No stash — focused test
    )

    // Inject some events
    n.Ledger().Add(testSocialEvent())
    n.Ledger().Add(testObservationEvent())

    // Get gossip service, create zine
    gossipSvc := n.GetService("gossip").(*gossip.Service)
    zine := gossipSvc.CreateZine()

    assert.Len(t, zine.Events, 2)
}
```

### Testing Checkpoint Consensus (Multiple Naras)

```go
func TestCheckpointConsensus(t *testing.T) {
    transport := mock.NewSharedTransport()

    naras := make([]*core.Coordinator, 5)
    for i := 0; i < 5; i++ {
        naras[i] = core.NewNara(fmt.Sprintf("nara-%d", i),
            WithService(presence.New()),
            WithService(neighbourhood.New()),
            WithService(checkpoint.New()),
            WithTransport(transport.Fork()),
        )
        go naras[i].Run()
    }
    defer func() {
        for _, n := range naras {
            n.Shutdown()
        }
    }()

    // Wait for checkpoint round
    time.Sleep(checkpointInterval + 5*time.Second)

    // Verify all naras have the same checkpoint
    var checkpoints []*Checkpoint
    for _, n := range naras {
        cp := n.GetService("checkpoint").(*checkpoint.Service).Latest()
        checkpoints = append(checkpoints, cp)
    }

    // All should match
    for i := 1; i < len(checkpoints); i++ {
        assert.Equal(t, checkpoints[0].Hash, checkpoints[i].Hash)
    }
}
```

### Testing Stash Recovery

```go
func TestStashRecoveryAfterRestart(t *testing.T) {
    transport := mock.NewSharedTransport()

    // Start a nara, let it distribute stash
    n1 := core.NewNara("owner",
        WithService(presence.New()),
        WithService(stash.New()),
        WithTransport(transport.Fork()),
    )
    go n1.Run()

    // Start confidants
    for i := 0; i < 3; i++ {
        c := core.NewNara(fmt.Sprintf("confidant-%d", i),
            WithService(presence.New()),
            WithService(stash.New()),
            WithTransport(transport.Fork()),
        )
        go c.Run()
    }

    // Wait for stash distribution
    time.Sleep(5 * time.Second)

    // Shutdown owner
    n1.Shutdown()

    // Restart owner with same soul
    n2 := core.NewNara("owner",
        WithSoul(n1.Soul()),  // Same identity
        WithService(presence.New()),
        WithService(stash.New()),
        WithTransport(transport.Fork()),
    )
    go n2.Run()

    // Verify stash was recovered
    time.Sleep(5 * time.Second)
    stashSvc := n2.GetService("stash").(*stash.Service)
    assert.True(t, stashSvc.HasRecovered())
}
```

---

## What Daily Development Feels Like

### Adding a New Feature

Say you want to add "achievements" — badges naras earn for uptime milestones.

1. Create `services/achievements/service.go`
2. Implement the Service interface
3. Subscribe to relevant events (observation, checkpoint)
4. Emit achievement events when milestones hit
5. Register in main.go: `WithService(achievements.New())`

No touching core. No touching other services. Just add and wire.

### Debugging a Problem

"Stash isn't syncing properly"

1. Run with only stash + presence + transport
2. Add logging to stash service
3. Mock transport to inspect messages
4. Problem isolated — no gossip/checkpoint/social noise

### Writing Tests

"I need to test checkpoint voting"

```go
n := core.NewNara("test",
    WithService(checkpoint.New()),
    WithTransport(mock.NewTransport()),
)
// No presence spam, no stash overhead, no social teasing
// Just checkpoint logic
```

### Understanding the Code

New developer asks: "How does presence work?"

"Look at `services/presence/`. It's self-contained. Init subscribes to hey-there and chau events. Start runs the announce loop. That's it."

No hunting through 897-line network.go.

---

## HTTP Handlers in This World

HTTP handlers become thin wrappers that query services:

```go
// http/api/handler.go
type APIHandler struct {
    coordinator *core.Coordinator
}

func (h *APIHandler) HandleGetPeers(w http.ResponseWriter, r *http.Request) {
    peers := h.coordinator.Peers().GetOnline()
    json.NewEncoder(w).Encode(peers)
}

func (h *APIHandler) HandleGetProjections(w http.ResponseWriter, r *http.Request) {
    proj := h.coordinator.Projections()
    json.NewEncoder(w).Encode(proj.Summary())
}

func (h *APIHandler) HandleGetCheckpoint(w http.ResponseWriter, r *http.Request) {
    if svc, ok := h.coordinator.GetService("checkpoint"); ok {
        cp := svc.(*checkpoint.Service).Latest()
        json.NewEncoder(w).Encode(cp)
    } else {
        http.Error(w, "checkpoint service not enabled", 404)
    }
}
```

Services that aren't running = endpoints return 404. Clean.

---

## Summary: Life in Plan C

| Aspect | Before (Now) | After (Plan C) |
|--------|--------------|----------------|
| Adding a feature | Touch network.go, add methods, wire everywhere | Create service dir, implement interface, register |
| Testing | Start full nara, deal with all services | Start with only services you need |
| Understanding code | Hunt through 219 Network methods | Look at one service directory |
| Debugging | Logs from everything | Enable logging per-service |
| Dependencies | Everything sees everything | Explicit via ServiceDeps interface |
| Boot time in tests | All services start | Only requested services |
| HTTP handlers | Reach into Network internals | Query coordinator/services |

---

---

# Feasibility Analysis

Now let's get real about what's hard.

---

## Easy Migrations (Do First)

### 1. Identity Package ✅

**Current state:** 4 files, 478 lines, zero dependencies on Network/LocalNara

**Migration:**
- Move `identity_*.go` to `identity/`
- Update imports
- Done

**Risk:** Zero. This is pure functions.

**Effort:** 1 hour

---

### 2. MeshClient (Already Done) ✅

**Current state:** Already decoupled, takes dependencies via constructor

**Status:** Model for other extractions. No work needed.

---

### 3. Social Service ⚡

**Current state:** 536 lines, 3 files. Mostly self-contained.

**Dependencies:**
- Reads from SyncLedger (events)
- Writes to SyncLedger (tease events)
- Reads from Projections (clout scores)
- Uses Network.socialInbox channel

**Migration path:**
1. Create `services/social/service.go`
2. Replace inbox with event subscription
3. Inject SyncLedger via ServiceDeps
4. Move tease/clout/trend logic

**Risk:** Low. Social is mostly reactive (responds to events).

**Effort:** 1-2 days

---

### 4. World/Journey Service ⚡

**Current state:** 3 files, handles postcards

**Dependencies:**
- Mesh transport for forwarding
- Ledger for journey events

**Migration path:**
1. Create `services/world/service.go`
2. Move journey logic

**Risk:** Low. Self-contained feature.

**Effort:** 1 day

---

## Medium Difficulty Migrations

### 5. Presence Service ⚠️

**Current state:** 6 files, 1543 lines. 25 Network methods.

**The hard parts:**
- **Inboxes:** `heyThereInbox`, `newspaperInbox`, `chauInbox`, `howdyInbox` — 4 channels to replace
- **Neighbourhood mutation:** Presence is what populates the peer registry
- **Mesh IP tracking:** Presence extracts mesh IPs from hey-there messages
- **Newspaper aggregation:** Collects and redistributes presence info

**Open question:** Does PeerRegistry live in core, or is it owned by Presence?

**Proposal:** PeerRegistry in core, Presence mutates it via interface.

**Migration path:**
1. Extract PeerRegistry to core
2. Create PresenceService with event subscriptions
3. Replace inboxes with Subscribe("hey-there", handler)
4. Move newspaper logic

**Risk:** Medium. Presence is the foundation for knowing who exists.

**Effort:** 3-5 days

---

### 6. Gossip Service ⚠️

**Current state:** 4 files, 649 lines. Tightly coupled.

**The hard parts:**
- **Zine creation:** Needs access to ALL recent events (SubscribeAll pattern)
- **Personality filtering:** Zines are filtered by personality
- **Mesh transport:** Direct HTTP to peers for exchange
- **Discovery:** Finds new peers via mesh

**Dependencies:**
- SyncLedger (read events for zine)
- Personality (filtering)
- MeshClient (HTTP calls)
- PeerRegistry (who to gossip with)

**Open question:** How does gossip get events that happened before it subscribed?

**Proposal:** ServiceDeps.Ledger().GetRecent(since time.Time) for backfill.

**Migration path:**
1. GossipService subscribes to all events
2. Maintains local buffer of recent events
3. Creates zines on tick
4. Uses MeshClient (already decoupled) for exchange

**Risk:** Medium. Event buffering needs careful design.

**Effort:** 3-5 days

---

### 7. Stash Service ⚠️

**Current state:** 5 files, 1897 lines. Already somewhat decoupled.

**The hard parts:**
- **Confidant selection:** Needs peer info + memory profile + uptime
- **Mesh transport:** Direct HTTP for stash exchange
- **Recovery:** Asks peers for stash on boot
- **Bidirectional:** Both stores others' stash AND distributes own

**Current good parts:**
- Already has `StashService`, `StashManager`, `ConfidantStashStore`
- Already uses `StashServiceDeps` interface pattern

**Open question:** How does stash know about boot recovery?

**Proposal:** Boot recovery becomes a lifecycle phase, not a service. Stash has a `Recover()` method called during boot.

**Migration path:**
1. Clean up existing StashServiceDeps interface
2. Move files to `services/stash/`
3. Wire via ServiceDeps

**Risk:** Medium. Already partially extracted.

**Effort:** 2-3 days

---

### 8. Checkpoint Service ⚠️

**Current state:** 4 files, 1518 lines. Complex consensus logic.

**The hard parts:**
- **MQTT topics:** Publishes/subscribes to checkpoint-specific topics
- **Timing:** Needs coordinated timing across network
- **Vote collection:** Collects votes from peers, computes consensus
- **Multi-party signatures:** Multiple naras sign the checkpoint
- **Depends on observations:** Uses observation data for values

**Dependencies:**
- MQTT transport (checkpoint topics)
- Ledger (checkpoint events)
- Observations (uptime/restart values)
- PeerRegistry (who's voting)
- Neighbourhood (observation consensus)

**Open question:** Does checkpoint depend on neighbourhood, or are observations in core?

**Proposal:** Observations are foundational (like events). Move to core. Checkpoint uses observations via ServiceDeps.

**Migration path:**
1. Clarify observation ownership
2. CheckpointService subscribes to checkpoint events
3. Uses transport abstraction for MQTT topics
4. Move voting/consensus logic

**Risk:** Medium-High. Consensus is delicate.

**Effort:** 4-5 days

---

## Hard Migrations

### 9. Neighbourhood Service 🔴

**Current state:** 4 files, 982 lines. Deeply integrated with observations.

**The hard parts:**
- **Observations are everywhere:** 41 files depend on observation concepts
- **Bidirectional with checkpoint:** Checkpoint reads observations, observations include checkpoint data
- **Opinion derivation:** Computes consensus opinions from observations
- **Pruning:** Removes stale peers

**Open question:** Are observations a service or part of core?

**Observations feel foundational:**
- Every nara tracks observations
- Projections derive from observations
- Checkpoints use observations
- Tests need observations

**Proposal:** Observations go in core alongside events. Neighbourhood service handles peer lifecycle but queries observations from core.

```
core/
├── event.go         # SyncEvent
├── observation.go   # Observations (foundational)
└── ...

services/
└── neighbourhood/   # Peer lifecycle, pruning
```

**Risk:** High. Need to carefully separate "observation storage" (core) from "neighbourhood policy" (service).

**Effort:** 5-7 days

---

### 10. Transport Layer 🔴

**Current state:** 5 files, 1596 lines. MQTT + Mesh + HTTP handlers.

**The hard parts:**
- **MQTT handlers:** Currently wired in Network, call Network methods
- **Mesh HTTP handlers:** Serve sync requests, stash exchange, etc.
- **Message routing:** Incoming messages need to reach the right service
- **Topic management:** Each service needs different topics

**Current tight coupling:**
```go
// In transport_mqtt.go
func (n *Network) mqttOnConnectHandler() {
    n.Mqtt.Subscribe("nara/hey-there", n.handleHeyThere)
    n.Mqtt.Subscribe("nara/chau", n.handleChau)
    // ... 20+ subscriptions that call Network methods
}
```

**Open question:** How do services declare what topics/paths they need?

**Proposal:** Transport abstraction with registration:

```go
type Transport interface {
    Subscribe(topic string, handler MessageHandler)
    Publish(topic string, payload []byte)
    RegisterHTTPHandler(path string, handler http.Handler)
}

// In presence service init:
func (s *PresenceService) Init(deps ServiceDeps) {
    deps.Transport().Subscribe("nara/hey-there", s.handleHeyThere)
}
```

**Migration path:**
1. Create Transport interface
2. MQTT transport implements Subscribe/Publish
3. Mesh transport implements HTTP routing
4. Services register their handlers during Init

**Risk:** High. Transport touches everything.

**Effort:** 5-7 days

---

### 11. Boot Recovery 🔴

**Current state:** 5 files, 1099 lines. Orchestration layer.

**The hard parts:**
- **Sequence matters:** Must sync ledger before stash, checkpoints need peers
- **Not a service:** Boot is a phase, not ongoing behavior
- **Touches everything:** Calls into sync, stash, checkpoint, presence

**Open question:** Is boot a service, a lifecycle phase, or something else?

**Proposal:** Boot is a lifecycle phase in coordinator:

```go
func (c *Coordinator) Run() error {
    // Phase 1: Initialize services
    for _, svc := range c.services {
        svc.Init(deps)
    }

    // Phase 2: Connect transports
    for _, t := range c.transports {
        t.Start(ctx)
    }

    // Phase 3: Boot recovery (before services start)
    if err := c.bootRecovery(); err != nil {
        log.Warn("boot recovery failed:", err)
    }

    // Phase 4: Start services
    for _, svc := range c.services {
        svc.Start(ctx)
    }
}

func (c *Coordinator) bootRecovery() error {
    // Sync ledger from peers
    c.ledger.SyncFrom(c.peers)

    // Let services recover
    for _, svc := range c.services {
        if recoverable, ok := svc.(Recoverable); ok {
            recoverable.Recover()
        }
    }
}
```

**Risk:** Medium. Need to design recovery as optional service capability.

**Effort:** 3-4 days

---

### 12. HTTP Handlers 🔴

**Current state:** 7 files, 2818 lines. Mix of API, UI, mesh handlers.

**The hard parts:**
- **API handlers query everything:** Peers, projections, events, checkpoints
- **Mesh handlers serve services:** Sync, stash exchange, attestation
- **UI handlers need projections:** Dashboard data

**Open question:** How do HTTP handlers access services?

**Proposal:** HTTP handlers get coordinator reference, query services by name:

```go
func (h *APIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    switch r.URL.Path {
    case "/api/checkpoint/latest":
        if svc, ok := h.coord.GetService("checkpoint"); ok {
            json.NewEncoder(w).Encode(svc.(*checkpoint.Service).Latest())
        } else {
            http.Error(w, "checkpoint not enabled", 404)
        }
    }
}
```

**For mesh handlers:** Register during service Init:

```go
func (s *StashService) Init(deps ServiceDeps) {
    deps.Transport().RegisterHTTPHandler("/mesh/stash", s.handleStashExchange)
}
```

**Risk:** Medium. Mainly tedious, not complex.

**Effort:** 4-5 days

---

## Big Open Questions

### Q1: Where do observations live?

**Options:**
1. **Core** — Observations are foundational like events
2. **Service** — NeighbourhoodService owns observations
3. **Hybrid** — Core stores observations, service derives opinions

**Recommendation:** Hybrid. Core has `ObservationStore`, service has policy/opinion logic.

---

### Q2: How do we handle the inboxes?

**Current:** 8 channels in Network, services read from them

**Options:**
1. **Event subscriptions** — Replace channels with Subscribe()
2. **Keep channels internally** — EventBus uses channels under the hood
3. **Direct handlers** — Transport routes directly to service handlers

**Recommendation:** Event subscriptions. Clean, testable, no shared state.

---

### Q3: What's the migration order?

**Proposed order (safest to riskiest):**

1. **identity/** — Zero risk, pure functions
2. **Event types** — Move SyncEvent etc to core/event.go
3. **PeerRegistry** — Foundation for everything
4. **Transport abstraction** — Define interface, implement for MQTT/mesh
5. **services/social/** — Simple, low coupling
6. **services/world/** — Self-contained
7. **services/presence/** — Foundation for peers
8. **services/gossip/** — Depends on presence
9. **services/stash/** — Already partially extracted
10. **Observations to core** — Foundational
11. **services/neighbourhood/** — After observations
12. **services/checkpoint/** — Complex, do last
13. **Boot recovery** — Once services support Recover()
14. **HTTP handlers** — Final cleanup

---

### Q4: How do we migrate incrementally?

**The challenge:** Can't do big bang. Production must keep running.

**Proposal: Parallel path migration**

1. Create `core/` package with new interfaces
2. Implement `Coordinator` that wraps existing `Network`
3. Extract services one by one, each calling through to Network
4. Once all services extracted, delete Network methods
5. Eventually delete Network entirely

**Example: Extracting social**

```go
// Phase 1: Social service wraps Network
type SocialService struct {
    network *Network  // temporary
}

func (s *SocialService) handleTease(event *SyncEvent) {
    // Still calls network for now
    s.network.processSocialEvent(event)
}

// Phase 2: Move logic into service
func (s *SocialService) handleTease(event *SyncEvent) {
    // Logic now in service
    payload := event.Payload.(*SocialPayload)
    s.deps.Ledger().Add(event)
    s.updateClout(payload)
}

// Phase 3: Delete network.processSocialEvent()
```

---

### Q5: What about tests during migration?

**Challenge:** Existing tests use Network directly.

**Proposal:**
1. Keep Network tests working during migration
2. Add new tests for services
3. Once service extracted, migrate tests
4. Delete old Network tests

**Compatibility shim:**

```go
// In tests, can create Network-style nara OR new-style
func TestOldStyle(t *testing.T) {
    // Still works during migration
    ln := testNara(t, "test")
    ln.Network.processSocialEvent(...)
}

func TestNewStyle(t *testing.T) {
    // New pattern
    n := core.NewNara("test",
        WithService(social.New()),
    )
    n.GetService("social").(*social.Service).HandleEvent(...)
}
```

---

## Risk Summary

| Component | Difficulty | Risk | Effort |
|-----------|------------|------|--------|
| identity/ | Easy | Zero | 1 hour |
| Event types to core | Easy | Low | 2 hours |
| PeerRegistry | Easy | Low | 1 day |
| Transport interface | Hard | High | 5-7 days |
| services/social | Easy | Low | 1-2 days |
| services/world | Easy | Low | 1 day |
| services/presence | Medium | Medium | 3-5 days |
| services/gossip | Medium | Medium | 3-5 days |
| services/stash | Medium | Medium | 2-3 days |
| Observations to core | Hard | Medium | 3-4 days |
| services/neighbourhood | Hard | High | 5-7 days |
| services/checkpoint | Hard | High | 4-5 days |
| Boot recovery | Medium | Medium | 3-4 days |
| HTTP handlers | Medium | Low | 4-5 days |

**Total estimated effort:** 5-8 weeks of focused work

---

## First Steps

If you want to start, I recommend:

1. **Create `core/` package** with Service interface and Coordinator skeleton
2. **Extract `identity/`** — Zero risk proof of concept
3. **Extract `services/social/`** — First real service, low coupling
4. **Add tests** that use `core.NewNara(WithService(...))` pattern

This validates the architecture with minimal risk before tackling the hard parts (transport, presence, observations).
