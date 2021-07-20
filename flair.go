package nara

import (
	"github.com/enescakir/emoji"
	"github.com/sirupsen/logrus"
	"io/ioutil"
	"os"
	"strings"
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
	return awards
}

func (ln LocalNara) LicensePlate() string {
	barrio := ln.getMeObservation().ClusterEmoji
	country, err := emoji.CountryFlag(ln.Me.IRL.CountryCode)
	if err != nil {
		logrus.Panic("lol failed to get country emoji lmao", ln.Me.IRL.CountryCode, err)
	}
	return barrio + " " + country.String()
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
