package nara

import (
	"fmt"

	"github.com/shirou/gopsutil/v3/host"
	"github.com/sirupsen/logrus"

	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

const NaraVersion = "0.2.0"

type LocalNara struct {
	Me              *Nara
	Network         *Network
	Soul            string
	ID              string // Nara ID: deterministic hash of soul+name for unique identity
	Keypair         NaraKeypair
	SyncLedger      *SyncLedger      // Unified event store for all syncable data (social + ping + future types)
	Projections     *ProjectionStore // Event-sourced projections for derived state
	MemoryProfile   MemoryProfile
	forceChattiness int
	isRaspberryPi   bool
	isNixOs         bool
	isKubernetes    bool
	mu              sync.Mutex
}

type Nara struct {
	Name     string
	Hostname string `json:"-"`
	Version  string
	Status   NaraStatus
	ID       string // Nara ID from other naras (redundant with Status.ID but convenient)
	mu       sync.Mutex
	// remember to sync with setValuesFrom
}

type NaraPersonality struct {
	Agreeableness int // 0-100, high = more likely to join a clique
	Sociability   int // 0-100, high = more likely to start a clique
	Chill         int // 0-100, high = less likely to leave a clique
}

type NaraStatus struct {
	LicensePlate        string
	Flair               string
	HostStats           HostStats
	Chattiness          int64
	Buzz                int
	Observations        map[string]NaraObservation
	Trend               string
	TrendEmoji          string
	Personality         NaraPersonality
	Aura                Aura // Visual identity (colors, glow, etc.) derived from personality and soul
	Version             string
	PublicUrl           string
	PublicKey           string             // Base64-encoded Ed25519 public key
	ID                  string             // Nara ID: deterministic hash of soul+name
	MeshEnabled         bool               // True if this nara is connected to the Headscale mesh
	MeshIP              string             // Tailscale IP for direct mesh communication (no DNS needed)
	Coordinates         *NetworkCoordinate `json:"coordinates,omitempty"`    // Vivaldi network coordinates
	TransportMode       string             `json:"transport_mode,omitempty"` // "mqtt", "gossip", or "hybrid"
	EventStoreTotal     int                `json:"event_store_total,omitempty"`
	EventStoreByService map[string]int     `json:"event_store_by_service,omitempty"`
	EventStoreCritical  int                `json:"event_store_critical,omitempty"`
	MemoryMode          string             `json:"memory_mode,omitempty"`
	MemoryBudgetMB      int                `json:"memory_budget_mb,omitempty"`
	MemoryMaxEvents     int                `json:"memory_max_events,omitempty"`
	StashStored         int                `json:"stash_stored,omitempty"`     // Number of stashes stored for others
	StashBytes          int64              `json:"stash_bytes,omitempty"`      // Total bytes of stash data stored
	StashConfidants     int                `json:"stash_confidants,omitempty"` // Number of confidants storing my stash
	// remember to sync with setValuesFrom
	// NOTE: Soul was removed - NEVER serialize private keys!
}

func NewLocalNara(identity IdentityResult, mqtt_host string, mqtt_user string, mqtt_pass string, forceChattiness int, memoryProfile MemoryProfile) (*LocalNara, error) {
	logrus.Printf("📟 Booting nara: %s (%s)", identity.Name, identity.ID)

	soulStr := FormatSoul(identity.Soul)
	if memoryProfile.MaxEvents <= 0 {
		memoryProfile = DefaultMemoryProfile()
	}

	ln := &LocalNara{
		Me:              NewNara(identity.Name),
		Soul:            soulStr,
		ID:              identity.ID,
		MemoryProfile:   memoryProfile,
		forceChattiness: forceChattiness,
		isRaspberryPi:   isRaspberryPi(),
		isNixOs:         isNixOs(),
		isKubernetes:    isKubernetes(),
	}
	ln.Me.Version = NaraVersion
	ln.Me.Status.Version = NaraVersion
	ln.Me.Status.Coordinates = NewNetworkCoordinate() // Initialize Vivaldi coordinates
	ln.Me.Status.ID = identity.ID
	ln.Me.Status.MemoryMode = string(memoryProfile.Mode)
	ln.Me.Status.MemoryBudgetMB = memoryProfile.BudgetMB
	ln.Me.Status.MemoryMaxEvents = memoryProfile.MaxEvents
	// NOTE: Soul is NEVER set in Status - private keys must not be serialized!

	// Derive Ed25519 keypair from soul
	ln.Keypair = DeriveKeypair(identity.Soul)
	ln.Me.Status.PublicKey = FormatPublicKey(ln.Keypair.PublicKey)
	logrus.Printf("🔑 Keypair derived from soul")

	ln.seedPersonality()

	// Set aura after personality is initialized
	ln.Me.Status.Aura = ln.computeAura()

	// Initialize unified sync ledger for all service types (social + ping + observation)
	// GUARANTEE: SyncLedger is ALWAYS non-nil after NewLocalNara() completes
	// NOTE: Must be initialized BEFORE Network so CheckpointService can use it
	ln.SyncLedger = NewSyncLedger(memoryProfile.MaxEvents)
	if ln.SyncLedger == nil {
		panic("SyncLedger initialization failed - this should never happen")
	}

	// Initialize projections after SyncLedger
	ln.Projections = NewProjectionStore(ln.SyncLedger)

	// Initialize network (needs SyncLedger for CheckpointService)
	ln.Network = NewNetwork(ln, mqtt_host, mqtt_user, mqtt_pass)

	ln.updateHostStats()

	hostinfo, err := host.Info()
	if err != nil || hostinfo == nil {
		logrus.Warnf("⚠️  Warning: failed to get host info: %v", err)
	} else {
		ln.Me.Hostname = hostinfo.Hostname
	}

	observation := ln.getMeObservation()
	observation.LastRestart = time.Now().Unix()
	ln.setMeObservation(observation)

	return ln, nil
}

func NewNara(name string) *Nara {
	nara := &Nara{Name: name}
	nara.Status.Observations = make(map[string]NaraObservation)
	return nara
}

func (ln *LocalNara) Start(serveUI bool, readOnly bool, httpAddr string, meshConfig *TsnetConfig, transportMode TransportMode) {
	ln.Network.ReadOnly = readOnly
	ln.Network.TransportMode = transportMode
	ln.Me.Status.TransportMode = transportMode.String() // Share our transport mode with peers
	if serveUI {
		logrus.Printf("💻 Serving UI")
	}

	// Start projections
	if ln.Projections != nil {
		// Configure MISSING threshold to account for gossip mode
		ln.Projections.OnlineStatus().SetMissingThresholdFunc(func(name string) int64 {
			threshold := MissingThresholdSeconds
			nara := ln.Network.getNara(name)
			subjectIsGossip := nara.Name != "" && nara.Status.TransportMode == "gossip"
			observerIsGossip := ln.Network.TransportMode == TransportGossip
			if subjectIsGossip || observerIsGossip {
				threshold = MissingThresholdGossipSeconds
			}
			return threshold * int64(time.Second) // Convert to nanoseconds
		})
		ln.Projections.Start()
	}

	go ln.updateHostStatsForever()
	ln.Network.Start(serveUI, httpAddr, meshConfig)

	if readOnly {
		logrus.Printf("🤫 Read-only mode: not pinging or announcing")
	}
}

func (ln *LocalNara) SetupCloseHandler() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		fmt.Println("babaayyy")
		ln.Network.Chau()

		// Gracefully shutdown projections
		if ln.Projections != nil {
			ln.Projections.Shutdown()
		}

		// Gracefully shutdown all background goroutines
		ln.Network.Shutdown()

		ln.Network.disconnectMQTT()

		time.Sleep(50 * time.Millisecond) // sleep a bit to ensure message is sent
		os.Exit(0)
	}()
}

func (ln *LocalNara) chattinessRate(min int64, max int64) int64 {
	return min + ((max - min) * (100 - ln.Me.Status.Chattiness) / 100)
}

func (ln *LocalNara) uptime() int64 {
	me := ln.getMeObservation()
	return me.LastSeen - me.LastRestart
}

func (ln *LocalNara) isBooting() bool {
	return ln.uptime() < 120
}

func (nara *Nara) setValuesFrom(other Nara) {
	nara.mu.Lock() // this protects Status
	defer nara.mu.Unlock()

	if other.Name == "" || other.Name != nara.Name {
		logrus.Printf("warning: fed incorrect Nara to setValuesFrom: %v", other)
		return
	}

	if other.Version != "" {
		nara.Version = other.Version
	}
	nara.Status.setValuesFrom(other.Status)
}

func (ns *NaraStatus) setValuesFrom(other NaraStatus) {
	ns.LicensePlate = other.LicensePlate
	if other.Flair != "" {
		ns.Flair = other.Flair
	}
	if (other.HostStats != HostStats{}) {
		ns.HostStats = other.HostStats
	}
	if other.Version != "" {
		ns.Version = other.Version
	}
	ns.Trend = other.Trend
	ns.TrendEmoji = other.TrendEmoji
	ns.Personality = other.Personality
	if other.Aura.Primary != "" {
		ns.Aura = other.Aura
	}
	// NOTE: Soul is never copied - private keys must not be shared!
	if other.LicensePlate != "" {
		ns.LicensePlate = other.LicensePlate
	}
	ns.Chattiness = other.Chattiness
	ns.Buzz = other.Buzz
	if other.Observations != nil {
		for name, nara := range other.Observations {
			ns.Observations[name] = nara
		}
	}
	if other.PublicUrl != "" {
		ns.PublicUrl = other.PublicUrl
	}
	if other.PublicKey != "" {
		ns.PublicKey = other.PublicKey
	}
	if other.ID != "" {
		ns.ID = other.ID
	}
	if other.MemoryMode != "" {
		ns.MemoryMode = other.MemoryMode
	}
	if other.MemoryBudgetMB != 0 {
		ns.MemoryBudgetMB = other.MemoryBudgetMB
	}
	if other.MemoryMaxEvents != 0 {
		ns.MemoryMaxEvents = other.MemoryMaxEvents
	}
	ns.MeshEnabled = other.MeshEnabled
	ns.MeshIP = other.MeshIP
	if other.Coordinates != nil {
		ns.Coordinates = other.Coordinates
	}
	if other.TransportMode != "" {
		ns.TransportMode = other.TransportMode
	}
}
