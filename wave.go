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
	return WaveMessage{StartNara: from, Body: body, CreatedAt: time.Now().Unix()}
}

func (wm WaveMessage) markAsSeen(name string) WaveMessage {
	seenToken := WaveSeenToken{Nara: name, Time: time.Now().Unix()}
	return WaveMessage{StartNara: wm.StartNara, Body: wm.Body, SeenBy: append(wm.SeenBy, seenToken)}
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
	return (wm.StartNara != "" && wm.Body != "")
}

func (network *Network) processWaveMessageEvents() {
	for {
		waveMessage := <-network.waveMessageInbox
		logrus.Printf("handling WaveMessage %+v", waveMessage)

		if waveMessage.hasSeen(network.meName()) {
			seconds := time.Now().Unix() - waveMessage.CreatedAt
			count := len(waveMessage.SeenBy)
			logrus.Printf("message came back home, took %d seconds and was seen by %d narae", seconds, count)
			continue
		}

		waveMessage = waveMessage.markAsSeen(network.meName())
		nextNara := waveMessage.nextNara(network.NeighbourhoodOnlineNames())

		err := network.httpPostWaveMessage(nextNara, waveMessage)
		if err == nil {
			logrus.Printf("posted WaveMessage to %s", nextNara)
		} else {
			logrus.Errorf("failed to post WaveMessage to %s: %w", nextNara, err)
		}
	}
}
