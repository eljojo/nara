package main

import (
	"encoding/json"
	"flag"
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
)

func main() {
	mqttHostPtr := flag.String("mqtt-host", "tcp://hass.eljojo.casa:1883", "mqtt server hostname")
	mqttUserPtr := flag.String("mqtt-user", "my_username", "mqtt server username")
	mqttPassPtr := flag.String("mqtt-pass", "my_password", "mqtt server password")
	naraIdPtr := flag.String("nara-id", "raspberry", "nara id")

	flag.Parse()
	client := connectMQTT(*mqttHostPtr, *mqttUserPtr, *mqttPassPtr, *naraIdPtr)

	// if token := c.Subscribe("$SYS/broker/load/#", 0, brokerLoadHandler); token.Wait() && token.Error() != nil {
	// 	fmt.Println(token.Error())
	// 	os.Exit(1)
	// }

	announce(client, *naraIdPtr)
}

type Nara struct {
	Name string
}

// hey there - hey how's it going?

func announce(client mqtt.Client, naraId string) {
	topic := "nara/happenings/hey_there"
	logrus.Println("Publishing to", topic)

	event := &Nara{Name: naraId}
	payload, err := json.Marshal(event)
	if err != nil {
		fmt.Println(err)
		return
	}
	token := client.Publish(topic, 0, false, string(payload))
	token.Wait()
}

func connectMQTT(host string, user string, pass string, deviceId string) mqtt.Client {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(host)
	opts.SetClientID(deviceId)
	opts.SetUsername(user)
	opts.SetPassword(pass)
	opts.OnConnect = connectHandler
	opts.OnConnectionLost = connectLostHandler
	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}
	return client
}

var connectHandler mqtt.OnConnectHandler = func(client mqtt.Client) {
	logrus.Println("Connected to MQTT")
}

var connectLostHandler mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
	logrus.Printf("MQTT Connection lost: %v", err)
}
