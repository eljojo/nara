package nara

import (
	"crypto/tls"
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
	subscribeMqtt(client, "nara/plaza/social", network.socialHandler)
	subscribeMqtt(client, "nara/plaza/journey_complete", network.journeyCompleteHandler)
	subscribeMqtt(client, "nara/newspaper/#", network.newspaperHandler)
	subscribeMqtt(client, "nara/selfies/#", network.selfieHandler)

	// Subscribe to ledger requests and responses for this nara
	ledgerRequestTopic := fmt.Sprintf("nara/ledger/%s/request", network.meName())
	ledgerResponseTopic := fmt.Sprintf("nara/ledger/%s/response", network.meName())
	subscribeMqtt(client, ledgerRequestTopic, network.ledgerRequestHandler)
	subscribeMqtt(client, ledgerResponseTopic, network.ledgerResponseHandler)
}

func (network *Network) heyThereHandler(client mqtt.Client, msg mqtt.Message) {
	heyThere := &HeyThereEvent{}
	if err := json.Unmarshal(msg.Payload(), heyThere); err != nil {
		logrus.Infof("heyThereHandler: invalid JSON: %v", err)
		return
	}

	if heyThere.From == network.meName() || heyThere.From == "" {
		return
	}

	network.heyThereInbox <- *heyThere
}

func (network *Network) selfieHandler(client mqtt.Client, msg mqtt.Message) {
	nara := NewNara("")
	if err := json.Unmarshal(msg.Payload(), nara); err != nil {
		logrus.Infof("selfieHandler: invalid JSON: %v", err)
		return
	}

	if nara.Name == network.meName() || nara.Name == "" {
		return
	}

	network.selfieInbox <- *nara
}

func (network *Network) chauHandler(client mqtt.Client, msg mqtt.Message) {
	event := ChauEvent{}
	if err := json.Unmarshal(msg.Payload(), &event); err != nil {
		logrus.Infof("chauHandler: invalid JSON: %v", err)
		return
	}

	network.chauInbox <- event
}

func (network *Network) socialHandler(client mqtt.Client, msg mqtt.Message) {
	event := SocialEvent{}
	if err := json.Unmarshal(msg.Payload(), &event); err != nil {
		logrus.Infof("socialHandler: invalid JSON: %v", err)
		return
	}

	// Ignore our own events
	if event.Actor == network.meName() {
		return
	}

	// Validate event
	if !event.IsValid() || event.Actor == "" || event.Target == "" {
		return
	}

	network.socialInbox <- event
}

func (network *Network) ledgerRequestHandler(client mqtt.Client, msg mqtt.Message) {
	req := LedgerRequest{}
	if err := json.Unmarshal(msg.Payload(), &req); err != nil {
		logrus.Infof("ledgerRequestHandler: invalid JSON: %v", err)
		return
	}

	// Ignore our own requests
	if req.From == network.meName() || req.From == "" {
		return
	}

	network.ledgerRequestInbox <- req
}

func (network *Network) ledgerResponseHandler(client mqtt.Client, msg mqtt.Message) {
	resp := LedgerResponse{}
	if err := json.Unmarshal(msg.Payload(), &resp); err != nil {
		logrus.Infof("ledgerResponseHandler: invalid JSON: %v", err)
		return
	}

	// Ignore responses from ourselves (shouldn't happen, but be safe)
	if resp.From == network.meName() {
		return
	}

	network.ledgerResponseInbox <- resp
}

func (network *Network) journeyCompleteHandler(client mqtt.Client, msg mqtt.Message) {
	completion := JourneyCompletion{}
	if err := json.Unmarshal(msg.Payload(), &completion); err != nil {
		logrus.Infof("journeyCompleteHandler: invalid JSON: %v", err)
		return
	}

	// Ignore our own completion signals
	if completion.ReportedBy == network.meName() {
		return
	}

	// Validate required fields
	if completion.JourneyID == "" || completion.Originator == "" {
		return
	}

	network.journeyCompleteInbox <- completion
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

	var event NewspaperEvent
	if err := json.Unmarshal(msg.Payload(), &event); err != nil {
		logrus.Infof("newspaperHandler: invalid JSON: %v", err)
		return
	}

	// Use 'from' from topic as the authoritative source (it matches the MQTT topic)
	// but preserve signature for verification
	network.newspaperInbox <- NewspaperEvent{From: from, Status: event.Status, Signature: event.Signature}
}

func subscribeMqtt(client mqtt.Client, topic string, handler func(client mqtt.Client, msg mqtt.Message)) {
	maxRetries := 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		token := client.Subscribe(topic, 0, handler)
		token.Wait()

		if token.Error() == nil {
			logrus.Debugf("Subscribed to %s", topic)
			return
		}

		if attempt < maxRetries {
			logrus.Warnf("Failed to subscribe to %s (attempt %d/%d): %v, retrying...", topic, attempt, maxRetries, token.Error())
			time.Sleep(time.Duration(attempt) * time.Second)
		} else {
			logrus.Errorf("Failed to subscribe to %s after %d attempts: %v", topic, maxRetries, token.Error())
		}
	}
}

func (network *Network) postEvent(topic string, event interface{}) {
	// Skip MQTT in gossip-only mode
	if network.TransportMode == TransportGossip {
		return
	}

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
	// Skip if MQTT was never connected (gossip-only mode)
	if network.TransportMode == TransportGossip {
		return
	}
	network.Mqtt.Disconnect(100)
	logrus.Printf("Disconnected from MQTT")
}

// Shutdown gracefully stops all background goroutines
func (network *Network) Shutdown() {
	logrus.Printf("🛑 Initiating graceful shutdown...")

	// Cancel context to signal all goroutines to stop
	if network.cancelFunc != nil {
		network.cancelFunc()
	}

	// Give goroutines a moment to finish their current work
	time.Sleep(100 * time.Millisecond)

	logrus.Printf("✅ Graceful shutdown complete")
}

func initializeMQTT(onConnect mqtt.OnConnectHandler, name string, host string, user string, pass string) mqtt.Client {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(host)
	opts.SetClientID(name)
	opts.SetUsername(user)
	opts.SetPassword(pass)
	opts.SetOrderMatters(false)
	opts.SetAutoReconnect(false)

	if strings.HasPrefix(host, "ssl://") || strings.HasPrefix(host, "tls://") {
		tlsConfig := &tls.Config{InsecureSkipVerify: true}
		opts.SetTLSConfig(tlsConfig)
	}

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
