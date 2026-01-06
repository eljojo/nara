package main

import (
	"crypto/tls"
	"flag"
	"github.com/bugsnag/bugsnag-go"
	"github.com/eljojo/nara"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/sirupsen/logrus"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	"runtime"
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
	rand.Seed(time.Now().UnixNano())

	bugsnag.Configure(bugsnag.Configuration{
		APIKey:          "0bd8e595fccf5f1befe9151c3a32ea61",
		ProjectPackages: []string{"main"},
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	})

	mqttHostPtr := flag.String("mqtt-host", getEnv("MQTT_HOST", "tls://mqtt.nara.network:8883"), "mqtt server hostname")
	mqttUserPtr := flag.String("mqtt-user", getEnv("MQTT_USER", deobfuscate(defaultUserEnc)), "mqtt server username")
	mqttPassPtr := flag.String("mqtt-pass", getEnv("MQTT_PASS", deobfuscate(defaultPassEnc)), "mqtt server password")
	httpAddrPtr := flag.String("http-addr", getEnv("HTTP_ADDR", ""), "http server address (e.g. :8080)")
	naraIdPtr := flag.String("nara-id", getEnv("NARA_ID", ""), "nara id")
	soulPtr := flag.String("soul", getEnv("NARA_SOUL", ""), "nara soul to inherit identity")
	showNeighboursPtr := flag.Bool("show-neighbours", true, "show table with neighbourhood")
	showNeighboursSpeedPtr := flag.Int("refresh-rate", 600, "refresh rate in seconds for neighbourhood table")
	forceChattinessPtr := flag.Int("force-chattiness", -1, "specific chattiness to force, -1 for auto (default)")
	verbosePtr := flag.Bool("verbose", false, "log debug stuff")
	readOnlyPtr := flag.Bool("read-only", false, "watch the network without sending any messages")
	serveUiPtr := flag.Bool("serve-ui", false, "serve the web UI")
	publicUrlPtr := flag.String("public-url", getEnv("PUBLIC_URL", ""), "public URL for this nara's web UI")
	noMeshPtr := flag.Bool("no-mesh", false, "disable mesh networking via Headscale")
	headscaleUrlPtr := flag.String("headscale-url", getEnv("HEADSCALE_URL", deobfuscate(defaultHeadscaleURLEnc)), "Headscale control server URL")
	authKeyPtr := flag.String("authkey", getEnv("TS_AUTHKEY", deobfuscate(defaultHeadscaleKeyEnc)), "Headscale auth key for automatic registration")

	flag.Parse()

	if *verbosePtr {
		logrus.SetLevel(logrus.DebugLevel)
	}

	info, _ := host.Info()
	macs := nara.CollectSoulFragments()
	hwFingerprint := nara.HashHardware(strings.Join([]string{info.HostID, macs}, "-"))

	identity := nara.DetermineIdentity(*naraIdPtr, *soulPtr, getHostname(), hwFingerprint)
	soulStr := nara.FormatSoul(identity.Soul)

	localNara := nara.NewLocalNara(identity.Name, soulStr, *mqttHostPtr, *mqttUserPtr, *mqttPassPtr, *forceChattinessPtr)
	localNara.Me.Status.PublicUrl = *publicUrlPtr

	// Log identity status
	if !identity.IsValidBond {
		logrus.Warn("⚠️  Inauthentic: soul does not match name")
	} else if !identity.IsNative {
		logrus.Info("🧳 Traveler: foreign soul (valid bond)")
	}

	logrus.Infof("🔮 Soul: %s", soulStr)

	// Configure mesh (enabled by default)
	var meshConfig *nara.TsnetConfig
	if !*noMeshPtr {
		meshConfig = &nara.TsnetConfig{
			Hostname:   identity.Name,
			ControlURL: *headscaleUrlPtr,
			AuthKey:    *authKeyPtr,
		}
		logrus.Infof("🕸️  Mesh enabled: %s", *headscaleUrlPtr)
	} else {
		logrus.Info("🕸️  Mesh disabled")
	}

	localNara.Start(*serveUiPtr, *readOnlyPtr, *httpAddrPtr, meshConfig)
	if *showNeighboursPtr {
		go localNara.PrintNeigbourhoodForever(*showNeighboursSpeedPtr)
	}

	localNara.SetupCloseHandler()
	defer localNara.Network.Chau()

	// sleep forever while goroutines do their thing
	for {
		time.Sleep(10 * time.Millisecond)
		runtime.Gosched() // https://blog.container-solutions.com/surprise-golang-thread-scheduling
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func getHostname() string {
	hostname, _ := os.Hostname()
	return strings.Split(hostname, ".")[0]
}
