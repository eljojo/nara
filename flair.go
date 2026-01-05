package nara

import (
	"io/ioutil"
	"os"
	"strings"
	"crypto/sha256"
	"encoding/binary"
	"math/rand"
)

func (ln LocalNara) Flair() string {
	networkSize := len(ln.Network.Neighbourhood)
	awards := ""
	if ln.Me.Name == ln.Me.Hostname {
		awards = awards + "🧑‍🚀"
	}
	if ln.isRaspberryPi {
		awards = awards + "🍓"
	}
	if ln.isNixOs {
		awards = awards + "❄️"
	}
	if ln.isKubernetes {
		awards = awards + "🐳"
	}
	if networkSize > 2 {
		if ln.Me.Name == ln.Network.oldestNara().Name {
			awards = awards + "🧓"
		}
		if ln.Me.Name == ln.Network.oldestNaraBarrio().Name {
			awards = awards + "👑"
		}
		if ln.Me.Name == ln.Network.youngestNara().Name {
			awards = awards + "🐤"
		}
		if ln.Me.Name == ln.Network.youngestNaraBarrio().Name {
			awards = awards + "🐦"
		}
		if ln.Me.Name == ln.Network.mostRestarts().Name {
			awards = awards + "🔁"
		}
	}
	if ln.Me.Status.Chattiness >= 80 {
		awards = awards + "💬"
	}
	if ln.Me.Status.HostStats.LoadAvg >= 0.8 {
		awards = awards + "📈"
	} else if ln.Me.Status.HostStats.LoadAvg <= 0.1 {
		awards = awards + "🆒"
	}
	if ln.uptime() < (3600 * 6) {
		awards = awards + "👶"
	}
	if ln.isBooting() {
		awards = awards + "📡"
	}

	// Trend Flair
	if ln.Me.Status.Trend != "" {
		awards = awards + ln.Me.Status.TrendEmoji
	}

	// Expressive Personality Flair (The Chotchkie's Rule)
	// 15 pieces of flair minimum? No, let's base it on sociability
	flairCount := (ln.Me.Status.Personality.Sociability / 20) + 1
	hasher := sha256.New()
	hasher.Write([]byte(ln.Me.Name))
	seed := int64(binary.BigEndian.Uint64(hasher.Sum(nil)[:8]))
	r := rand.New(rand.NewSource(seed))
	for i := 0; i < flairCount; i++ {
		awards = awards + PersonalFlairPool[r.Intn(len(PersonalFlairPool))]
	}

	return awards
}

func (ln LocalNara) LicensePlate() string {
	barrio := ln.getMeObservation().ClusterEmoji
	return barrio
}

func isRaspberryPi() bool {
	content, err := ioutil.ReadFile("/proc/cpuinfo")
	if err != nil {
		return false
	}
	return strings.Contains(string(content), "Raspberry Pi")
}

func isNixOs() bool {
	_, err := os.Stat("/etc/nixos")
	nix_does_not_exist := os.IsNotExist(err)
	return !nix_does_not_exist
}

func isKubernetes() bool {
	return (os.Getenv("KUBERNETES_SERVICE_HOST") != "")
}
