package nara

import (
	"crypto/sha256"
	"encoding/binary"
	"math/rand"
	"os"
	"strings"

	"github.com/eljojo/nara/identity"
)

func (ln *LocalNara) Flair() string {
	awards := ln.Me.Flair(ln.Soul, ln.isRaspberryPi, ln.isNixOs, ln.isKubernetes)

	// Network-dependent flair (only for local nara)
	networkSize := len(ln.Network.Neighbourhood)
	myStartTime := ln.Network.local.getMeObservation().StartTime
	if networkSize > 2 && myStartTime > 0 {
		// Age-based flair (requires knowing our own StartTime)
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
	} else if networkSize > 2 && myStartTime == 0 {
		// Waiting for opinions to form
		awards = awards + "🤔"
	}

	// Status-dependent flair
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

	return awards
}

func (nara *Nara) Flair(soul string, isRaspberryPi bool, isNixOs bool, isKubernetes bool) string {
	awards := ""

	if nara.IsInHarmony(soul) {
		awards = identity.Gemstone(nara.Name.String(), soul)
	} else {
		awards = "👤"
	}

	if nara.Name.String() == nara.Hostname {
		awards = awards + "🧑‍🚀"
	}
	if isRaspberryPi {
		awards = awards + "🍓"
	}
	if isNixOs {
		awards = awards + "❄️"
	}
	if isKubernetes {
		awards = awards + "🐳"
	}

	// Trend Flair
	if nara.Status.Trend != "" {
		awards = awards + nara.Status.TrendEmoji
	}

	// Expressive Personality Flair
	flairCount := (nara.Status.Personality.Sociability / 31) + 1
	hasher := sha256.New()
	hasher.Write([]byte(nara.Name))
	seed := int64(binary.BigEndian.Uint64(hasher.Sum(nil)[:8]))
	r := rand.New(rand.NewSource(seed))
	for i := 0; i < flairCount; i++ {
		awards = awards + PersonalFlairPool[r.Intn(len(PersonalFlairPool))]
	}

	return awards
}

func (nara *Nara) IsInHarmony(soulStr string) bool {
	if soulStr == "" {
		return false
	}

	soul, err := identity.ParseSoul(soulStr)
	if err != nil {
		return false
	}

	return identity.ValidateBond(soul, nara.Name)
}

func (ln *LocalNara) LicensePlate() string {
	barrio := ln.getMeObservation().ClusterEmoji
	return barrio
}

func isRaspberryPi() bool {
	content, err := os.ReadFile("/proc/cpuinfo")
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
