package main

import (
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
)

var connectHandler mqtt.OnConnectHandler = func(client mqtt.Client) {
	logrus.Println("Connected to MQTT")

	if token := client.Subscribe("nara/newspaper/#", 0, newspaperHandler); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	if token := client.Subscribe("nara/plaza/hey_there", 0, heyThereHandler); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	if token := client.Subscribe("nara/plaza/chau", 0, chauHandler); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	heyThere(client)
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

var connectLostHandler mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
	logrus.Printf("MQTT Connection lost: %v", err)
}
