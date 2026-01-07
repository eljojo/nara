package nara

import (
	"runtime"
	"time"

	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/sirupsen/logrus"
)

type HostStats struct {
	Uptime  uint64
	LoadAvg float64
}

func (ln *LocalNara) updateHostStatsForever() {
	for {
		ln.updateHostStats()

		select {
		case <-time.After(5 * time.Second):
			// Continue to next iteration
		case <-ln.Network.ctx.Done():
			logrus.Debugf("updateHostStatsForever: shutting down gracefully")
			return
		}
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

	if ln.Me.Status.HostStats.LoadAvg > 0.8 && ln.Network.skippingEvents == false && !ln.isBooting() {
		logrus.Println("[warning] busy cpu, newspaper events may be dropped")
		ln.Network.skippingEvents = true
	} else if ln.Me.Status.HostStats.LoadAvg < 0.8 && ln.Network.skippingEvents == true {
		logrus.Println("[recovered] cpu is healthy again, not dropping events anymore")
		ln.Network.skippingEvents = false
	}

	if ln.isBooting() {
		chattiness = 100
		ln.Network.skippingEvents = false
	}

	ln.Me.Status.Chattiness = chattiness
}
