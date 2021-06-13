package nara

import (
	"errors"
	"github.com/shirou/gopsutil/host"
	"github.com/shirou/gopsutil/load"
	"github.com/sirupsen/logrus"
	"net"
	"runtime"
	"strings"
	"time"
)

type HostStats struct {
	Uptime  uint64
	LoadAvg float64
}

func (ln *LocalNara) updateHostStatsForever() {
	for {
		ln.updateHostStats()
		time.Sleep(5 * time.Second)
	}
}

func (ln *LocalNara) updateHostStats() {
	uptime, _ := host.Uptime()
	ln.Me.Status.HostStats.Uptime = uptime

	load, _ := load.Avg()
	loadavg := load.Load1 / float64(runtime.NumCPU())
	ln.Me.Status.HostStats.LoadAvg = float64(int64(loadavg*100)) / 100 // truncate to 2 digits

	chattiness := int64(ln.Network.weightedBuzz())

	if ln.forceChattiness >= 0 && ln.forceChattiness <= 100 {
		chattiness = int64(ln.forceChattiness)
	} else {
		if loadavg < 1 {
			chattiness = chattiness + int64((1-loadavg)*20)
		} else {
			ln.Me.Status.Chattiness = 0
		}
	}

	if chattiness > 100 {
		chattiness = 100
	}

	if chattiness <= 10 && ln.Network.skippingEvents == false {
		logrus.Println("[warning] low chattiness, newspaper events may be dropped")
		ln.Network.skippingEvents = true
	} else if chattiness > 10 && ln.Network.skippingEvents == true {
		logrus.Println("[recovered] chattiness is healthy again, not dropping events anymore")
		ln.Network.skippingEvents = false
	}

	ln.Me.Status.Chattiness = chattiness
}

// https://stackoverflow.com/questions/23558425/how-do-i-get-the-local-ip-address-in-go
func externalIP() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue // interface down
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue // loopback interface
		}
		addrs, err := iface.Addrs()
		if err != nil {
			return "", err
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip = ip.To4()
			if ip == nil {
				continue // not an ipv4 address
			}

			// skip non-tailscale IPs
			if !strings.HasPrefix(ip.String(), "100.") {
				continue
			}

			return ip.String(), nil
		}
	}
	return "", errors.New("are you connected to the network?")
}
