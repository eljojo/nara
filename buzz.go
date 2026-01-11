package nara

import (
	"github.com/sirupsen/logrus"
	"sync"
	"time"
)

const BuzzMax = 182
const BuzzMin = 0
const BuzzDecrease = 3     // per second
const BuzzUpdateEvery = 10 // seconds

type Buzz struct {
	count int
	mu    sync.Mutex
}

func newBuzz() *Buzz {
	b := &Buzz{count: 100} // starting value
	return b
}

func (network *Network) maintenanceBuzz() {
	ticker := time.NewTicker(BuzzUpdateEvery * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			network.Buzz.decrease(BuzzDecrease * BuzzUpdateEvery)
			network.local.Me.Status.Buzz = network.weightedBuzz()
		case <-network.ctx.Done():
			return
		}
	}
}

func (b *Buzz) increase(howMuch int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.count = b.count + howMuch
	// logrus.Debugf("increasing buzz to %d", b.count)
	if b.count > BuzzMax {
		b.count = BuzzMax
		logrus.Debugf("reached max buzz %d", b.count)
	}
}

func (b *Buzz) decrease(howMuch int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.count = b.count - howMuch
	// logrus.Debugf("decreasing buzz to %d", b.count)
	if b.count < BuzzMin {
		b.count = BuzzMin
	}
}

func (b Buzz) getLocal() int {
	return b.count
}

func (network Network) getNetworkAverageBuzz() int {
	sum := 0
	count := 0
	network.local.mu.Lock()
	defer network.local.mu.Unlock()
	for name, nara := range network.Neighbourhood {
		if network.local.getObservation(name).isOnline() && nara.Name != network.meName() {
			sum = sum + nara.Status.Buzz
			count = count + 1
		}
	}
	if count == 0 {
		return 0
	}
	return sum / count
}

func (network Network) getHighestBuzz() int {
	highest := network.local.Me.Status.Buzz
	network.local.mu.Lock()
	defer network.local.mu.Unlock()
	for name, nara := range network.Neighbourhood {
		if !network.local.getObservationLocked(name).isOnline() {
			continue
		}
		if nara.Status.Buzz > highest {
			highest = nara.Status.Buzz
		}
	}
	return highest
}

func (n Network) weightedBuzz() int {
	sum := 0
	sum = sum + int(float64(n.Buzz.getLocal())*0.5)
	sum = sum + int(float64(n.getNetworkAverageBuzz())*0.2)
	sum = sum + int(float64(n.getHighestBuzz())*0.3)
	if sum > BuzzMax {
		return BuzzMax
	}
	return sum
}
