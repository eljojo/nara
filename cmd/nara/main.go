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

func main() {
	rand.Seed(time.Now().UnixNano())

	bugsnag.Configure(bugsnag.Configuration{
		APIKey:          "0bd8e595fccf5f1befe9151c3a32ea61",
		ProjectPackages: []string{"main"},
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	})

	mqttHostPtr := flag.String("mqtt-host", getEnv("MQTT_HOST", "tcp://hass.eljojo.casa:1883"), "mqtt server hostname")
	mqttUserPtr := flag.String("mqtt-user", getEnv("MQTT_USER", "my_username"), "mqtt server username")
	mqttPassPtr := flag.String("mqtt-pass", getEnv("MQTT_PASS", "my_password"), "mqtt server password")
	httpAddrPtr := flag.String("http-addr", getEnv("HTTP_ADDR", ""), "http server address (e.g. :8080)")
	naraIdPtr := flag.String("nara-id", getEnv("NARA_ID", ""), "nara id")
	soulPtr := flag.String("soul", getEnv("NARA_SOUL", ""), "nara soul to inherit identity")
	showNeighboursPtr := flag.Bool("show-neighbours", true, "show table with neighbourhood")
	showNeighboursSpeedPtr := flag.Int("refresh-rate", 600, "refresh rate in seconds for neighbourhood table")
	forceChattinessPtr := flag.Int("force-chattiness", -1, "specific chattiness to force, -1 for auto (default)")
	verbosePtr := flag.Bool("verbose", false, "log debug stuff")
	readOnlyPtr := flag.Bool("read-only", false, "watch the network without sending any messages")
	serveUiPtr := flag.Bool("serve-ui", false, "serve the web UI")

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

	// Log identity status
	if !identity.IsValidBond {
		logrus.Warn("⚠️  Inauthentic: soul does not match name")
	} else if !identity.IsNative {
		logrus.Info("🧳 Traveler: foreign soul (valid bond)")
	}

	logrus.Infof("🔮 Soul: %s", soulStr)

	localNara.Start(*serveUiPtr, *readOnlyPtr, *httpAddrPtr)
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
