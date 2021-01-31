package main

import (
	"encoding/json"
	"flag"
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
	"math/rand"
	"time"
)

var me = &Nara{Name: ""}
var inbox = make(chan [2]string, 1)

func main() {
	rand.Seed(time.Now().UnixNano())

	mqttHostPtr := flag.String("mqtt-host", "tcp://hass.eljojo.casa:1883", "mqtt server hostname")
	mqttUserPtr := flag.String("mqtt-user", "my_username", "mqtt server username")
	mqttPassPtr := flag.String("mqtt-pass", "my_password", "mqtt server password")
	naraIdPtr := flag.String("nara-id", "raspberry", "nara id")

	flag.Parse()
	me.Name = *naraIdPtr

	client := connectMQTT(*mqttHostPtr, *mqttUserPtr, *mqttPassPtr, *naraIdPtr)
	go announceForever(client)

	for {
		<-inbox
	}
}

type Nara struct {
	Name   string
	Status NaraStatus
}

type NaraStatus struct {
	PingGoogle string
}

func announce(client mqtt.Client) {
	topic := "nara/plaza"
	logrus.Println("Publishing to", topic)

	payload, err := json.Marshal(me)
	if err != nil {
		fmt.Println(err)
		return
	}
	token := client.Publish(topic, 0, false, string(payload))
	token.Wait()
}

func announceForever(client mqtt.Client) {
	chattiness := rand.Intn(15) + 5
	logrus.Println("chattiness = ", chattiness)
	for {
		time.Sleep(time.Duration(chattiness) * time.Second)
		announce(client)
	}
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

func plazaHandler(client mqtt.Client, msg mqtt.Message) {
	fmt.Printf("plazaHandler")
	fmt.Printf("[%s]  ", msg.Topic())
	fmt.Printf("%s\n", msg.Payload())
	inbox <- [2]string{msg.Topic(), string(msg.Payload())}
}

var connectHandler mqtt.OnConnectHandler = func(client mqtt.Client) {
	logrus.Println("Connected to MQTT")
	if token := client.Subscribe("nara/plaza", 0, plazaHandler); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	announce(client)
}

var connectLostHandler mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
	logrus.Printf("MQTT Connection lost: %v", err)
}
