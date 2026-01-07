package nara

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand"
	"time"

	"github.com/sirupsen/logrus"
)

var TrendEmojis = []string{"✨", "🌈", "🔥", "💎", "🍄", "☄️", "🎨", "🎭", "🧶", "🎬", "🎤", "🎧", "🎹", "🎷", "🎸", "🎻", "🎺", "🥁"}

var PersonalFlairPool = []string{"🍀", "🌸", "🍕", "🍔", "🍦", "🍩", "🧁", "🍭", "🍬", "🍫", "🍿", "🧂", "🍳", "🧇", "🥞", "🧈"}

func (ln *LocalNara) seedPersonality() {
	hasher := sha256.New()
	hasher.Write([]byte(ln.Soul))
	hash := hasher.Sum(nil)
	seed := int64(binary.BigEndian.Uint64(hash[:8]))
	r := rand.New(rand.NewSource(seed))

	ln.Me.Status.Personality.Agreeableness = r.Intn(100)
	ln.Me.Status.Personality.Sociability = r.Intn(100)
	ln.Me.Status.Personality.Chill = r.Intn(100)

	logrus.Printf("🎭 personality for %s: agreeableness=%d, sociability=%d, chill=%d",
		ln.Me.Name,
		ln.Me.Status.Personality.Agreeableness,
		ln.Me.Status.Personality.Sociability,
		ln.Me.Status.Personality.Chill)
}

func (network *Network) trendMaintenance() {
	for {
		time.Sleep(30 * time.Second)
		network.maintenanceStep()
	}
}

func (network *Network) maintenanceStep() {
	if network.local.Me.Status.Trend == "" {
		network.considerJoiningTrend()
	} else {
		network.considerLeavingTrend()
	}
}

func (network *Network) considerJoiningTrend() {
	trends := make(map[string]int)
	onlineCount := 0
	network.local.mu.Lock()
	for name, nara := range network.Neighbourhood {
		if !network.local.getObservationLocked(name).isOnline() {
			continue
		}
		onlineCount++
		if nara.Status.Trend != "" {
			trends[nara.Status.Trend]++
		}
	}
	network.local.mu.Unlock()

	// Try to join an existing trend first
	for trendName, count := range trends {
		// find the emoji for this trend from someone who has it
		emoji := "🕶️"
		network.local.mu.Lock()
		for _, nara := range network.Neighbourhood {
			if nara.Status.Trend == trendName {
				emoji = nara.Status.TrendEmoji
				break
			}
		}
		network.local.mu.Unlock()

		if network.shouldJoinTrend(trendName, count) {
			network.local.Me.Status.Trend = trendName
			network.local.Me.Status.TrendEmoji = emoji
			logrus.Printf("💅 %s is now following trend: %s", network.meName(), trendName)
			network.Buzz.increase(3)
			return
		}
	}

	// Didn't join anything - maybe start something new?
	network.considerStartingTrend(trends, onlineCount)
}

func (network *Network) considerStartingTrend(trends map[string]int, onlineCount int) {
	numTrends := len(trends)
	personality := network.local.Me.Status.Personality

	// Base chance to start a trend (sociability-driven)
	baseChance := float64(personality.Sociability) / 10.0

	// The more trends exist, the less likely to start a new one
	// Divide by (1 + numTrends) so: 0 trends = full chance, 1 trend = half, 2 = third, etc.
	chance := baseChance / float64(1+numTrends)

	// Contrarian boost: if there's a dominant mainstream trend, rebels might start underground
	if numTrends > 0 && onlineCount > 0 {
		// Find the most popular trend
		maxFollowers := 0
		for _, count := range trends {
			if count > maxFollowers {
				maxFollowers = count
			}
		}

		// How mainstream is the top trend? (percentage of online naras)
		mainstreamPct := (maxFollowers * 100) / onlineCount

		// If something is too mainstream (>50%), contrarians get restless
		if mainstreamPct > 50 {
			// Contrarian factor: low agreeableness = more likely to rebel
			contrarian := 100 - personality.Agreeableness
			// Boost chance based on how mainstream things are and how contrarian we are
			rebellionBoost := float64(contrarian*(mainstreamPct-50)) / 100.0
			chance += rebellionBoost / float64(1+numTrends) // Still reduced by existing trend count
		}
	}

	if rand.Intn(100) < int(chance) {
		newTrend := fmt.Sprintf("%s-style", network.meName())
		emoji := TrendEmojis[rand.Intn(len(TrendEmojis))]

		network.local.Me.Status.Trend = newTrend
		network.local.Me.Status.TrendEmoji = emoji

		if numTrends == 0 {
			logrus.Printf("✨ %s started a new trend: %s", network.meName(), newTrend)
		} else {
			logrus.Printf("🎸 %s started an underground trend: %s", network.meName(), newTrend)
		}
		network.Buzz.increase(5)
	}
}

func (network *Network) shouldJoinTrend(trendName string, count int) bool {
	// do we like the people in it?
	sameVibeCount := 0
	myVibe := network.calculateVibe(network.meName())

	network.local.mu.Lock()
	for name, nara := range network.Neighbourhood {
		if nara.Status.Trend == trendName {
			if network.calculateVibe(name) == myVibe {
				sameVibeCount++
			}
		}
	}
	network.local.mu.Unlock()

	// probability to join
	chance := network.local.Me.Status.Personality.Agreeableness
	if sameVibeCount > 0 {
		chance += 20 * sameVibeCount
	}

	return rand.Intn(150) < chance
}

func (network *Network) considerLeavingTrend() {
	// probability to leave
	// higher chill = lower chance to leave
	chance := 100 - network.local.Me.Status.Personality.Chill

	// if nobody else is in the trend, higher chance to leave
	othersInTrend := 0
	network.local.mu.Lock()
	for name, nara := range network.Neighbourhood {
		if !network.local.getObservationLocked(name).isOnline() {
			continue
		}
		if nara.Status.Trend == network.local.Me.Status.Trend {
			othersInTrend++
		}
	}
	network.local.mu.Unlock()

	if othersInTrend == 0 {
		chance += 30
	}

	if rand.Intn(500) < chance {
		logrus.Printf("🏃 %s is over this trend: %s", network.meName(), network.local.Me.Status.Trend)
		network.local.Me.Status.Trend = ""
		network.local.Me.Status.TrendEmoji = ""
		network.Buzz.increase(2)
	}
}
