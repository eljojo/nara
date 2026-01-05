package main

import (
	"flag"
	"os"
	"net/http"
	"crypto/tls"
	"strings"
	"github.com/bugsnag/bugsnag-go"
	"github.com/eljojo/nara"
	"github.com/sirupsen/logrus"
	"math/rand"
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

	hostname, err := os.Hostname()
	if err != nil {
		hostname = "raspberry"
	}
	hostname = strings.Split(hostname, ".")[0]

	mqttHostPtr := flag.String("mqtt-host", getEnv("MQTT_HOST", "tcp://hass.eljojo.casa:1883"), "mqtt server hostname")
	mqttUserPtr := flag.String("mqtt-user", getEnv("MQTT_USER", "my_username"), "mqtt server username")
	mqttPassPtr := flag.String("mqtt-pass", getEnv("MQTT_PASS", "my_password"), "mqtt server password")
	naraIdPtr := flag.String("nara-id", getEnv("NARA_ID", hostname), "nara id")
	showNeighboursPtr := flag.Bool("show-neighbours", true, "show table with neighbourhood")
	showNeighboursSpeedPtr := flag.Int("refresh-rate", 60, "refresh rate in seconds for neighbourhood table")
	forceChattinessPtr := flag.Int("force-chattiness", -1, "specific chattiness to force, -1 for auto (default)")
	verbosePtr := flag.Bool("verbose", false, "log debug stuff")
	readOnlyPtr := flag.Bool("read-only", false, "watch the network without sending any messages")
	serveUiPtr := flag.Bool("serve-ui", false, "serve the web UI")

	flag.Parse()

	if *verbosePtr {
		logrus.SetLevel(logrus.DebugLevel)
	}

	localNara := nara.NewLocalNara(*naraIdPtr, *mqttHostPtr, *mqttUserPtr, *mqttPassPtr, *forceChattinessPtr)

	localNara.Start(*serveUiPtr, *readOnlyPtr)
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
