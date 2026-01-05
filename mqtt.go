package nara

import (
	"encoding/json"
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
	"math/rand"
	"strings"
	"time"
)

func (network *Network) mqttOnConnectHandler() mqtt.OnConnectHandler {
	var connectHandler mqtt.OnConnectHandler = func(client mqtt.Client) {
		logrus.Println("Connected to MQTT")

		network.subscribeHandlers(client)
		network.heyThere()
	}
	return connectHandler
}

func (network *Network) subscribeHandlers(client mqtt.Client) {
	subscribeMqtt(client, "nara/plaza/hey_there", network.heyThereHandler)
	subscribeMqtt(client, "nara/plaza/chau", network.chauHandler)
	subscribeMqtt(client, "nara/newspaper/#", network.newspaperHandler)
	subscribeMqtt(client, "nara/selfies/#", network.selfieHandler)
	subscribeMqtt(client, "nara/ping/#", network.pingHandler)
	subscribeMqtt(client, "nara/wave", network.waveMqttHandler)
	subscribeMqtt(client, "nara/debug/clear_ping", network.mqttClearPingHandler)
}

func (network *Network) pingHandler(client mqtt.Client, msg mqtt.Message) {
	var pingEvent PingEvent
	json.Unmarshal(msg.Payload(), &pingEvent)
	network.pingInbox <- pingEvent

	// network.recordObservationOnlineNara(pingEvent.From) // dubious
}

func (network *Network) heyThereHandler(client mqtt.Client, msg mqtt.Message) {
	heyThere := &HeyThereEvent{}
	json.Unmarshal(msg.Payload(), heyThere)

	if heyThere.From == network.meName() || heyThere.From == "" {
		return
	}

	network.heyThereInbox <- *heyThere
}

func (network *Network) selfieHandler(client mqtt.Client, msg mqtt.Message) {
	nara := NewNara("")
	json.Unmarshal(msg.Payload(), nara)

	if nara.Name == network.meName() || nara.Name == "" {
		return
	}

	network.selfieInbox <- *nara
}

func (network *Network) chauHandler(client mqtt.Client, msg mqtt.Message) {
	nara := NewNara("")
	json.Unmarshal(msg.Payload(), nara)

	network.chauInbox <- *nara
}

func (network *Network) newspaperHandler(client mqtt.Client, msg mqtt.Message) {
	if network.skippingEvents == true && rand.Intn(2) == 0 {
		return
	}
	if !strings.Contains(msg.Topic(), "nara/newspaper/") {
		return
	}

	var from = strings.Split(msg.Topic(), "nara/newspaper/")[1]
	if from == network.meName() {
		return
	}

	var status NaraStatus
	json.Unmarshal(msg.Payload(), &status)

	network.newspaperInbox <- NewspaperEvent{From: from, Status: status}
}

func (network *Network) waveMqttHandler(client mqtt.Client, msg mqtt.Message) {
	var wm WaveMessage
	json.Unmarshal(msg.Payload(), &wm)

	network.local.mu.Lock()
	network.LastWave = wm
	network.local.mu.Unlock()

	if wm.StartNara == network.meName() {
		// if we started the wave, we don't need to process it again
		// but we might want to log that it came back if it's the first time we see it back
		if len(wm.SeenBy) > 1 && wm.hasSeen(network.meName()) {
			seconds := float64(timeNowMs()-wm.CreatedAt) / 1000
			logrus.Printf("🙌 message came back via MQTT, took %.2f seconds and was seen by %d narae", seconds, len(wm.SeenBy))
		}
		return
	}

	// IMPORTANT - avoids endless loops by not re-processing our own broadcasts
	// this check is now redundant because of the one above, but kept for clarity
	// if wm.StartNara == network.meName() { return }

	if wm.Valid() {
		network.waveMessageInbox <- wm
	} else {
		logrus.Printf("discarding invalid WaveMessage")
	}
}

func subscribeMqtt(client mqtt.Client, topic string, handler func(client mqtt.Client, msg mqtt.Message)) {
	if token := client.Subscribe(topic, 0, handler); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}
}

func (network *Network) postPing(ping PingEvent) {
	topic := fmt.Sprintf("%s/%s/%s", "nara/ping", ping.From, ping.To)
	network.postEvent(topic, ping)
}

func (network *Network) postEvent(topic string, event interface{}) {
	logrus.Debugf("posting on %s", topic)

	network.local.mu.Lock() // TODO: this sucks, remove ASAP
	payload, err := json.Marshal(event)
	if err != nil {
		fmt.Println(err)
		return
	}
	network.local.mu.Unlock()
	token := network.Mqtt.Publish(topic, 0, false, string(payload))
	token.Wait()
}

func (network *Network) disconnectMQTT() {
	network.Mqtt.Disconnect(100)
	logrus.Printf("Disconnected from MQTT")
}

func initializeMQTT(onConnect mqtt.OnConnectHandler, name string, host string, user string, pass string) mqtt.Client {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(host)
	opts.SetClientID(name)
	opts.SetUsername(user)
	opts.SetPassword(pass)
	opts.SetOrderMatters(false)
	opts.SetAutoReconnect(false)
	opts.OnConnect = onConnect
	opts.OnConnectionLost = func(client mqtt.Client, err error) {
		logrus.Printf("MQTT Connection lost: %v", err)
		go func() {
			for {
				// wait between 5 and 35 seconds
				wait := rand.Intn(30) + 5
				logrus.Printf("MQTT: Waiting %d seconds before reconnecting...", wait)
				time.Sleep(time.Duration(wait) * time.Second)

				token := client.Connect()
				if token.Wait() && token.Error() == nil {
					logrus.Printf("MQTT: Reconnected")
					return
				}
				logrus.Printf("MQTT: Reconnect failed: %v", token.Error())
			}
		}()
	}
	client := mqtt.NewClient(opts)
	return client
}
func (network *Network) mqttClearPingHandler(client mqtt.Client, msg mqtt.Message) {
	logrus.Printf("🏓 MQTT: /nara/debug/clear_ping: Clearing ping stats and increasing Buzz")
	network.local.mu.Lock()
	for _, nara := range network.Neighbourhood {
		nara.clearPing()
	}
	network.Buzz.increase(200)
	network.local.mu.Unlock()
}
