package main

import (
	"flag"
	"fmt"
	_ "net/http/pprof"
	"os"
	"runtime/debug"
	"strings"

	"github.com/eljojo/nara"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/sirupsen/logrus"
)

// ------------------------------------------------------------------
// 🙏 A note to whoever is reading this code:
//
// The MQTT credentials below are lightly obfuscated (XOR) - not for
// real security, just to keep them out of `strings` and casual grep.
//
// These credentials are shared with the nara community for a fun,
// collaborative project. We're trusting you to be a good neighbor.
// Please don't abuse them, share them publicly, or do anything that
// would ruin the fun for everyone else.
//
// Be kind. 🌸
// ------------------------------------------------------------------

var credKey = []byte("nara")

// XOR-obfuscated default credentials (decoded at runtime)
// To encode new values: for each byte, XOR with credKey[i % len(credKey)]
var defaultUserEnc = []byte{6, 4, 30, 13, 1, 76, 17, 20, 28, 8, 29, 20, 29, 76, 28, 0, 28, 0, 95, 7, 28, 8, 23, 15, 10}
var defaultPassEnc = []byte{30, 13, 23, 0, 29, 4, 95, 3, 11, 76, 25, 8, 0, 5, 95, 21, 1, 76, 29, 20, 28, 76, 30, 8, 26, 21, 30, 4, 67, 21, 19, 12, 15, 6, 29, 21, 13, 9, 27, 18}
var defaultHeadscaleURLEnc = []byte{6, 21, 6, 17, 29, 91, 93, 78, 24, 17, 28, 79, 0, 0, 0, 0, 64, 15, 23, 21, 25, 14, 0, 10}
var defaultHeadscaleKeyEnc = []byte{12, 3, 69, 3, 91, 87, 20, 4, 94, 4, 71, 0, 92, 86, 68, 81, 94, 87, 68, 88, 93, 5, 68, 3, 93, 2, 71, 3, 11, 87, 20, 0, 87, 81, 75, 88, 91, 86, 65, 0, 12, 5, 75, 86, 8, 84, 19, 88}

func deobfuscate(enc []byte) string {
	result := make([]byte, len(enc))
	for i, b := range enc {
		result[i] = b ^ credKey[i%len(credKey)]
	}
	return string(result)
}

func main() {
	// rand.Seed is no longer needed in Go 1.20+; random values are automatically seeded

	// Check for -show-default-credentials before setting up flags
	showCreds := hasArg("-show-default-credentials") || hasArg("--show-default-credentials")

	// Credential defaults and descriptions based on --show-default-credentials
	creds := struct {
		mqttHost, mqttUser, mqttPass, headscaleURL, authKey                     string
		mqttHostDesc, mqttUserDesc, mqttPassDesc, headscaleURLDesc, authKeyDesc string
	}{
		mqttHostDesc:     "mqtt server hostname (ex \"tls://mqtt.example.com:8883\")",
		mqttUserDesc:     "mqtt server username",
		mqttPassDesc:     "mqtt server password",
		headscaleURLDesc: "Headscale control server URL (ex \"https://headscale.example.com\")",
		authKeyDesc:      "Headscale auth key (ex \"tskey-auth-...\")",
	}
	if showCreds {
		creds.mqttHost = "tls://mqtt.nara.network:8883"
		creds.mqttUser = deobfuscate(defaultUserEnc)
		creds.mqttPass = deobfuscate(defaultPassEnc)
		creds.headscaleURL = deobfuscate(defaultHeadscaleURLEnc)
		creds.authKey = deobfuscate(defaultHeadscaleKeyEnc)
		creds.mqttHostDesc = "mqtt server hostname"
		creds.mqttUserDesc = "mqtt server username"
		creds.mqttPassDesc = "mqtt server password"
		creds.headscaleURLDesc = "Headscale control server URL"
		creds.authKeyDesc = "Headscale auth key for automatic registration"
	}

	mqttHostPtr := flag.String("mqtt-host", getEnv("MQTT_HOST", creds.mqttHost), creds.mqttHostDesc)
	mqttUserPtr := flag.String("mqtt-user", getEnv("MQTT_USER", creds.mqttUser), creds.mqttUserDesc)
	mqttPassPtr := flag.String("mqtt-pass", getEnv("MQTT_PASS", creds.mqttPass), creds.mqttPassDesc)
	httpAddrPtr := flag.String("http-addr", getEnv("HTTP_ADDR", ""), "http server address (e.g. :8080)")
	naraIdPtr := flag.String("nara-id", getEnv("NARA_ID", ""), "nara id")
	soulPtr := flag.String("soul", getEnv("NARA_SOUL", ""), "nara soul to inherit identity")
	showNeighboursPtr := flag.Bool("show-neighbours", true, "show table with neighbourhood")
	showNeighboursSpeedPtr := flag.Int("refresh-rate", 600, "refresh rate in seconds for neighbourhood table")
	forceChattinessPtr := flag.Int("force-chattiness", -1, "specific chattiness to force, -1 for auto (default)")
	verbosePtr := flag.Bool("verbose", false, "enable debug logging")
	extraVerbosePtr := flag.Bool("vv", false, "extra verbose: debug logging + Tailscale internal logs")
	readOnlyPtr := flag.Bool("read-only", false, "watch the network without sending any messages")
	serveUiPtr := flag.Bool("serve-ui", false, "serve the web UI")
	publicUrlPtr := flag.String("public-url", getEnv("PUBLIC_URL", ""), "public URL for this nara's web UI")
	noMeshPtr := flag.Bool("no-mesh", false, "disable mesh networking via Headscale")
	headscaleUrlPtr := flag.String("headscale-url", getEnv("HEADSCALE_URL", creds.headscaleURL), creds.headscaleURLDesc)
	authKeyPtr := flag.String("authkey", getEnv("TS_AUTHKEY", creds.authKey), creds.authKeyDesc)
	memoryModePtr := flag.String("memory-mode", getEnv("MEMORY_MODE", "auto"), "memory profile: short, medium, hog, auto")
	ledgerCapacityPtr := flag.Int("ledger-capacity", getEnvInt("LEDGER_CAPACITY", 0), "max events in sync ledger (overrides memory-mode)")
	flag.Bool("show-default-credentials", false, "show credentials used by the app by default")
	transportModePtr := flag.String("transport", getEnv("TRANSPORT_MODE", "hybrid"), "transport mode: mqtt, gossip, or hybrid (default)")

	flag.Parse()

	// Apply real defaults for runtime if not set via env or flag
	if *mqttHostPtr == "" {
		*mqttHostPtr = "tls://mqtt.nara.network:8883"
	}
	if *mqttUserPtr == "" {
		*mqttUserPtr = deobfuscate(defaultUserEnc)
	}
	if *mqttPassPtr == "" {
		*mqttPassPtr = deobfuscate(defaultPassEnc)
	}
	if *headscaleUrlPtr == "" {
		*headscaleUrlPtr = deobfuscate(defaultHeadscaleURLEnc)
	}
	if *authKeyPtr == "" {
		*authKeyPtr = deobfuscate(defaultHeadscaleKeyEnc)
	}

	if *extraVerbosePtr {
		logrus.SetLevel(logrus.TraceLevel)
	} else if *verbosePtr {
		logrus.SetLevel(logrus.DebugLevel)
	}

	info, _ := host.Info()
	macs := nara.CollectSoulFragments()
	hwFingerprint := nara.HashHardware(strings.Join([]string{info.HostID, macs}, "-"))

	identity := nara.DetermineIdentity(*naraIdPtr, *soulPtr, getHostname(), hwFingerprint)

	// Use NewLocalNara with the identity result
	memoryMode := nara.ParseMemoryMode(*memoryModePtr)
	memoryProfile := nara.MemoryProfileForMode(memoryMode)
	memorySource := "flag"
	if memoryMode == nara.MemoryModeAuto {
		profile, source, err := nara.AutoMemoryProfile()
		if err != nil {
			logrus.Warnf("🧠 Memory auto-detect failed, using default: %v", err)
			profile = nara.DefaultMemoryProfile()
			source = "default"
		}
		memoryProfile = profile
		memorySource = source
	}
	if *ledgerCapacityPtr > 0 {
		memoryProfile.Mode = nara.MemoryModeCustom
		memoryProfile.MaxEvents = *ledgerCapacityPtr
		memoryProfile.BudgetMB = 0
		memorySource = "override"
	}
	if memoryProfile.Mode == nara.MemoryModeShort {
		if os.Getenv("GOMEMLIMIT") == "" {
			debug.SetMemoryLimit(220 << 20)
			logrus.Infof("🧠 GOMEMLIMIT set to 220MiB (short memory mode)")
		}
		if os.Getenv("GOGC") == "" {
			debug.SetGCPercent(50)
			logrus.Infof("🧠 GOGC set to 50 (short memory mode)")
		}
	}
	localNara, err := nara.NewLocalNara(identity, *mqttHostPtr, *mqttUserPtr, *mqttPassPtr, *forceChattinessPtr, memoryProfile)
	if err != nil {
		logrus.Fatalf("Failed to initialize nara: %v", err)
	}
	localNara.Me.Status.PublicUrl = *publicUrlPtr

	// Log identity status
	if identity.ID == "" {
		logrus.Warn("⚠️  Identity initialization failed: ID is empty")
	} else if !identity.IsValidBond {
		logrus.Warn("⚠️  Inauthentic: soul does not match name")
	} else if !identity.IsNative {
		logrus.Info("🧳 Traveler: foreign soul (valid bond)")
	}

	logrus.Infof("🔮 Soul: %s", nara.FormatSoul(identity.Soul))

	// Parse transport mode
	transportMode := parseTransportMode(*transportModePtr)
	logrus.Infof("🚀 Transport mode: %s", transportModeString(transportMode))

	// Configure mesh (enabled by default)
	var meshConfig *nara.TsnetConfig
	if !*noMeshPtr {
		meshConfig = &nara.TsnetConfig{
			Hostname:   identity.Name,
			ControlURL: *headscaleUrlPtr,
			AuthKey:    *authKeyPtr,
			Verbose:    *extraVerbosePtr,
		}
		logrus.Infof("🕸️  Mesh enabled: %s", *headscaleUrlPtr)
	} else {
		logrus.Info("🕸️  Mesh disabled")
	}

	logrus.Infof(
		"🧠 Memory profile: %s (budget %d MB, max events %d, source: %s)",
		memoryProfile.Mode,
		memoryProfile.BudgetMB,
		memoryProfile.MaxEvents,
		memorySource,
	)

	localNara.Start(*serveUiPtr, *readOnlyPtr, *httpAddrPtr, meshConfig, transportMode)

	// Enable verbose logging if debug flags are set
	if *verbosePtr || *extraVerbosePtr {
		localNara.Network.SetVerboseLogging(true)
	}

	if *showNeighboursPtr {
		go localNara.PrintNeigbourhoodForever(*showNeighboursSpeedPtr)
	}

	localNara.SetupCloseHandler()
	defer localNara.Network.Chau()

	// sleep until shutdown
	<-localNara.Network.Context().Done()
	logrus.Info("Main loop: shutting down")
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if value, ok := os.LookupEnv(key); ok {
		var result int
		if _, err := fmt.Sscanf(value, "%d", &result); err == nil {
			return result
		}
	}
	return fallback
}

func getHostname() string {
	hostname, _ := os.Hostname()
	return strings.Split(hostname, ".")[0]
}

func hasArg(name string) bool {
	for _, arg := range os.Args[1:] {
		if arg == name {
			return true
		}
	}
	return false
}

func parseTransportMode(mode string) nara.TransportMode {
	switch strings.ToLower(mode) {
	case "mqtt":
		return nara.TransportMQTT
	case "gossip":
		return nara.TransportGossip
	case "hybrid":
		return nara.TransportHybrid
	default:
		logrus.Warnf("Unknown transport mode '%s', defaulting to hybrid", mode)
		return nara.TransportHybrid
	}
}

func transportModeString(mode nara.TransportMode) string {
	switch mode {
	case nara.TransportMQTT:
		return "mqtt"
	case nara.TransportGossip:
		return "gossip"
	case nara.TransportHybrid:
		return "hybrid"
	default:
		return "unknown"
	}
}
