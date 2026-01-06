package nara

import (
	"context"
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

		// Add jitter (0-5s) to prevent thundering herd when multiple narae reconnect simultaneously
		// Skip jitter in tests for faster discovery
		if network.testSkipJitter {
			network.heyThere()
		} else {
			jitter := time.Duration(rand.Intn(5000)) * time.Millisecond
			time.AfterFunc(jitter, func() {
				network.heyThere()
			})
		}
	}
	return connectHandler
}

func (network *Network) subscribeHandlers(client mqtt.Client) {
	subscribeMqtt(client, "nara/plaza/hey_there", network.heyThereHandler)
	subscribeMqtt(client, "nara/plaza/chau", network.chauHandler)
	subscribeMqtt(client, "nara/plaza/howdy", network.howdyHandler)
	subscribeMqtt(client, "nara/plaza/social", network.socialHandler)
	subscribeMqtt(client, "nara/plaza/journey_complete", network.journeyCompleteHandler)
	subscribeMqtt(client, "nara/newspaper/#", network.newspaperHandler)

	// Subscribe to ledger requests and responses for this nara
	ledgerRequestTopic := fmt.Sprintf("nara/ledger/%s/request", network.meName())
	ledgerResponseTopic := fmt.Sprintf("nara/ledger/%s/response", network.meName())
	subscribeMqtt(client, ledgerRequestTopic, network.ledgerRequestHandler)
	subscribeMqtt(client, ledgerResponseTopic, network.ledgerResponseHandler)

	// Subscribe to stash topics for distributed encrypted storage
	stashStoreTopic := fmt.Sprintf("nara/stash/%s/store", network.meName())
	stashAckTopic := fmt.Sprintf("nara/stash/%s/ack", network.meName())
	stashResponseTopic := fmt.Sprintf("nara/stash/%s/response", network.meName())
	stashDeleteTopic := fmt.Sprintf("nara/stash/%s/delete", network.meName())
	subscribeMqtt(client, stashStoreTopic, network.stashStoreHandler)
	subscribeMqtt(client, stashAckTopic, network.stashAckHandler)
	subscribeMqtt(client, "nara/plaza/stash_request", network.stashRequestHandler)
	subscribeMqtt(client, stashResponseTopic, network.stashResponseHandler)
	subscribeMqtt(client, stashDeleteTopic, network.stashDeleteHandler)
}

func (network *Network) heyThereHandler(client mqtt.Client, msg mqtt.Message) {
	event := SyncEvent{}
	if err := json.Unmarshal(msg.Payload(), &event); err != nil {
		logrus.Debugf("heyThereHandler: invalid JSON: %v", err)
		return
	}

	// Try new SyncEvent format first
	if event.Service == ServiceHeyThere && event.HeyThere != nil {
		if event.HeyThere.From == network.meName() || event.HeyThere.From == "" {
			return
		}
		network.heyThereInbox <- event
		return
	}

	// Fallback: try legacy HeyThereEvent format (for old nodes during rollout)
	legacy := HeyThereEvent{}
	if err := json.Unmarshal(msg.Payload(), &legacy); err != nil {
		return
	}
	if legacy.From == "" || legacy.From == network.meName() {
		return
	}
	// Convert legacy to SyncEvent
	event = SyncEvent{
		Timestamp: time.Now().UnixNano(),
		Service:   ServiceHeyThere,
		HeyThere:  &legacy,
	}
	event.ComputeID()
	network.heyThereInbox <- event
}

func (network *Network) howdyHandler(client mqtt.Client, msg mqtt.Message) {
	howdy := &HowdyEvent{}
	if err := json.Unmarshal(msg.Payload(), howdy); err != nil {
		logrus.Debugf("howdyHandler: invalid JSON: %v", err)
		return
	}

	// Ignore our own howdy messages
	if howdy.From == network.meName() || howdy.From == "" {
		return
	}

	network.howdyInbox <- *howdy
}

func (network *Network) chauHandler(client mqtt.Client, msg mqtt.Message) {
	event := SyncEvent{}
	if err := json.Unmarshal(msg.Payload(), &event); err != nil {
		logrus.Debugf("chauHandler: invalid JSON: %v", err)
		return
	}

	// Try new SyncEvent format first
	if event.Service == ServiceChau && event.Chau != nil {
		if event.Chau.From == network.meName() || event.Chau.From == "" {
			return
		}
		network.chauInbox <- event
		return
	}

	// Fallback: try legacy ChauEvent format (for old nodes during rollout)
	legacy := ChauEvent{}
	if err := json.Unmarshal(msg.Payload(), &legacy); err != nil {
		return
	}
	if legacy.From == "" || legacy.From == network.meName() {
		return
	}
	// Convert legacy to SyncEvent
	event = SyncEvent{
		Timestamp: time.Now().UnixNano(),
		Service:   ServiceChau,
		Chau:      &legacy,
	}
	event.ComputeID()
	network.chauInbox <- event
}

func (network *Network) socialHandler(client mqtt.Client, msg mqtt.Message) {
	// Try parsing as SyncEvent first (new format)
	syncEvent := SyncEvent{}
	if err := json.Unmarshal(msg.Payload(), &syncEvent); err == nil {
		if syncEvent.Service == ServiceSocial && syncEvent.Social != nil {
			// Ignore our own events
			if syncEvent.Social.Actor == network.meName() {
				return
			}
			network.socialInbox <- syncEvent
			return
		}
	}

	// Fallback: try legacy SocialEvent format (for old nodes during rollout)
	legacy := SocialEvent{}
	if err := json.Unmarshal(msg.Payload(), &legacy); err != nil {
		logrus.Infof("socialHandler: invalid JSON: %v", err)
		return
	}

	// Ignore our own events
	if legacy.Actor == network.meName() {
		return
	}

	// Validate event
	if !legacy.IsValid() || legacy.Actor == "" || legacy.Target == "" {
		return
	}

	// Convert to SyncEvent and send
	network.socialInbox <- SyncEventFromSocialEvent(legacy)
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

	var envelope struct {
		Status    json.RawMessage `json:"Status"`
		Signature string          `json:"Signature"`
	}
	if err := json.Unmarshal(msg.Payload(), &envelope); err != nil {
		logrus.Infof("newspaperHandler: invalid JSON: %v", err)
		return
	}

	// Use 'from' from topic as the authoritative source (it matches the MQTT topic)
	// but preserve signature for verification
	var status NaraStatus
	if err := json.Unmarshal(envelope.Status, &status); err != nil {
		logrus.Infof("newspaperHandler: invalid status JSON: %v", err)
		return
	}
	network.newspaperInbox <- NewspaperEvent{
		From:       from,
		Status:     status,
		Signature:  envelope.Signature,
		StatusJSON: envelope.Status,
	}
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

	// Perform final gossip round to spread our chau event (if gossip is enabled)
	if network.tsnetMesh != nil && network.TransportMode != TransportMQTT && !network.ReadOnly {
		logrus.Printf("📰 Sending final zine with chau event...")
		network.performGossipRound()
		// Give the HTTP requests a moment to complete
		time.Sleep(500 * time.Millisecond)
	}

	// Cancel context to signal all goroutines to stop
	if network.cancelFunc != nil {
		network.cancelFunc()
	}

	// Shutdown HTTP servers
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if network.httpServer != nil {
		network.httpServer.Shutdown(ctx)
	}
	if network.meshHttpServer != nil {
		network.meshHttpServer.Shutdown(ctx)
	}

	// Give goroutines a moment to finish their current work
	time.Sleep(100 * time.Millisecond)

	logrus.Printf("✅ Graceful shutdown complete")
}

func (network *Network) initializeMQTT(onConnect mqtt.OnConnectHandler, name string, host string, user string, pass string) mqtt.Client {
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
		network.mqttReconnectMu.Lock()
		if network.mqttReconnectActive {
			network.mqttReconnectMu.Unlock()
			logrus.Debug("MQTT reconnect already running; skipping duplicate")
			return
		}
		network.mqttReconnectActive = true
		network.mqttReconnectMu.Unlock()

		go func() {
			defer func() {
				network.mqttReconnectMu.Lock()
				network.mqttReconnectActive = false
				network.mqttReconnectMu.Unlock()
			}()
			for {
				select {
				case <-network.ctx.Done():
					logrus.Debug("MQTT Reconnect loop: shutting down")
					return
				default:
				}

				// wait between 5 and 35 seconds
				wait := rand.Intn(30) + 5
				logrus.Printf("MQTT: Waiting %d seconds before reconnecting...", wait)

				select {
				case <-time.After(time.Duration(wait) * time.Second):
					// continue
				case <-network.ctx.Done():
					return
				}

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

// --- Stash Handlers (Distributed Encrypted Storage) ---

func (network *Network) stashStoreHandler(client mqtt.Client, msg mqtt.Message) {
	storeMsg := &StashStore{}
	if err := json.Unmarshal(msg.Payload(), storeMsg); err != nil {
		logrus.Debugf("stashStoreHandler: invalid JSON: %v", err)
		return
	}

	if storeMsg.From == network.meName() || storeMsg.From == "" {
		return
	}

	network.stashStoreInbox <- *storeMsg
}

func (network *Network) stashAckHandler(client mqtt.Client, msg mqtt.Message) {
	ack := &StashStoreAck{}
	if err := json.Unmarshal(msg.Payload(), ack); err != nil {
		logrus.Debugf("stashAckHandler: invalid JSON: %v", err)
		return
	}

	if ack.From == network.meName() {
		return
	}

	network.stashAckInbox <- *ack
}

func (network *Network) stashRequestHandler(client mqtt.Client, msg mqtt.Message) {
	req := &StashRequest{}
	if err := json.Unmarshal(msg.Payload(), req); err != nil {
		logrus.Debugf("stashRequestHandler: invalid JSON: %v", err)
		return
	}

	// Ignore our own requests
	if req.From == network.meName() || req.From == "" {
		return
	}

	network.stashRequestInbox <- *req
}

func (network *Network) stashResponseHandler(client mqtt.Client, msg mqtt.Message) {
	resp := &StashResponse{}
	if err := json.Unmarshal(msg.Payload(), resp); err != nil {
		logrus.Debugf("stashResponseHandler: invalid JSON: %v", err)
		return
	}

	if resp.From == network.meName() {
		return
	}

	network.stashResponseInbox <- *resp
}

func (network *Network) stashDeleteHandler(client mqtt.Client, msg mqtt.Message) {
	deleteMsg := &StashDelete{}
	if err := json.Unmarshal(msg.Payload(), deleteMsg); err != nil {
		logrus.Debugf("stashDeleteHandler: invalid JSON: %v", err)
		return
	}

	if deleteMsg.From == network.meName() || deleteMsg.From == "" {
		return
	}

	network.stashDeleteInbox <- *deleteMsg
}
