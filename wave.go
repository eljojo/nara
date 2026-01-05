package nara

import (
	"github.com/sirupsen/logrus"
	"time"
)

type WaveMessage struct {
	StartNara string
	Body      string
	SeenBy    []WaveSeenToken
	CreatedAt int64
}

type WaveSeenToken struct {
	Nara string
	Time int64
}

func newWaveMessage(from string, body string) WaveMessage {
	return WaveMessage{StartNara: from, Body: body, CreatedAt: timeNowMs()}
}

func (wm WaveMessage) markAsSeen(name string) WaveMessage {
	seenToken := WaveSeenToken{Nara: name, Time: timeNowMs()}
	return WaveMessage{
		StartNara: wm.StartNara,
		Body:      wm.Body,
		CreatedAt: wm.CreatedAt,
		SeenBy:    append(wm.SeenBy, seenToken),
	}
}

func (wm WaveMessage) hasSeen(name string) bool {
	for _, token := range wm.SeenBy {
		if token.Nara == name {
			return true
		}
	}
	return false
}

func (wm WaveMessage) nextNara(narae []string) string {
	for _, name := range narae {
		if !wm.hasSeen(name) {
			return name
		}
	}
	return wm.StartNara
}

func (wm WaveMessage) Valid() bool {
	return (wm.StartNara != "" && wm.Body != "" && len(wm.Body) <= 512)
}

func (network *Network) processWaveMessageEvents() {
	for {
		waveMessage := <-network.waveMessageInbox

		logrus.Printf("📯 WaveMessage from %s: %s", waveMessage.StartNara, waveMessage.Body)

		if len(waveMessage.SeenBy) > 0 {
			seenAt := waveMessage.CreatedAt
			logrus.Println("seen by:")
			for i, token := range waveMessage.SeenBy {
				diff := token.Time - seenAt
				seenAt = token.Time
				if i == 0 {
					logrus.Printf("- %s", token.Nara)
				} else {
					logrus.Printf("- %s (%dms later)", token.Nara, diff)
				}
			}
		}

		if waveMessage.hasSeen(network.meName()) {
			seconds := float64(timeNowMs()-waveMessage.CreatedAt) / 1000
			if network.ReadOnly {
				logrus.Printf("🙌 message seen, took %.2f seconds and was seen by %d narae", seconds, len(waveMessage.SeenBy))
				continue
			}
			count := len(waveMessage.SeenBy)
			logrus.Printf("🙌 message came back, took %.2f seconds and was seen by %d narae", seconds, count)

			if waveMessage.StartNara == network.meName() {
				topic := "nara/wave"
				network.postEvent(topic, waveMessage)
			}
			continue
		}

		if network.ReadOnly {
			continue
		}

		waveMessage = waveMessage.markAsSeen(network.meName())

		// WaveMessages are now propagated exclusively via MQTT
		// We post it back to the plaza and others will pick it up
		// This is simpler and doesn't require knowing IPs
		logrus.Printf("propagating WaveMessage via MQTT")

		topic := "nara/wave"
		network.postEvent(topic, waveMessage)
	}
}

func timeNowMs() int64 {
	return time.Now().UnixNano() / (int64(time.Millisecond) / int64(time.Nanosecond))
}
