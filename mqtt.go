package main

import (
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
)

func (ln *LocalNara) mqttOnConnectHandler() mqtt.OnConnectHandler {
	var connectHandler mqtt.OnConnectHandler = func(client mqtt.Client) {
		logrus.Println("Connected to MQTT")

		if token := client.Subscribe("nara/newspaper/#", 0, ln.newspaperHandler); token.Wait() && token.Error() != nil {
			panic(token.Error())
		}

		if token := client.Subscribe("nara/plaza/hey_there", 0, ln.heyThereHandler); token.Wait() && token.Error() != nil {
			panic(token.Error())
		}

		if token := client.Subscribe("nara/plaza/chau", 0, ln.chauHandler); token.Wait() && token.Error() != nil {
			panic(token.Error())
		}

		ln.heyThere()
	}
	return connectHandler
}

func (ln *LocalNara) initializeMQTT(host string, user string, pass string) mqtt.Client {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(host)
	opts.SetClientID(ln.Me.Name)
	opts.SetUsername(user)
	opts.SetPassword(pass)
	opts.OnConnect = ln.mqttOnConnectHandler()
	opts.OnConnectionLost = connectLostHandler
	client := mqtt.NewClient(opts)
	return client
}

var connectLostHandler mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
	logrus.Printf("MQTT Connection lost: %v", err)
}
