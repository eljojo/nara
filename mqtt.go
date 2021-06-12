package nara

import (
	"encoding/json"
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
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
	subscribeMqtt(client, "nara/newspaper/#", network.newspaperHandler)
	subscribeMqtt(client, "nara/plaza/hey_there", network.heyThereHandler)
	subscribeMqtt(client, "nara/plaza/chau", network.pingHandler)
	subscribeMqtt(client, "nara/ping/#", network.pingHandler)
}

func (network *Network) pingHandler(client mqtt.Client, msg mqtt.Message) {
	var pingEvent PingEvent
	json.Unmarshal(msg.Payload(), &pingEvent)
	network.pingInbox <- pingEvent
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

	payload, err := json.Marshal(event)
	if err != nil {
		fmt.Println(err)
		return
	}
	token := network.Mqtt.Publish(topic, 0, false, string(payload))
	token.Wait()
}

func initializeMQTT(onConnect mqtt.OnConnectHandler, name string, host string, user string, pass string) mqtt.Client {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(host)
	opts.SetClientID(name)
	opts.SetUsername(user)
	opts.SetPassword(pass)
	opts.OnConnect = onConnect
	opts.OnConnectionLost = connectLostHandler
	client := mqtt.NewClient(opts)
	return client
}

var connectLostHandler mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
	logrus.Printf("MQTT Connection lost: %v", err)
}
