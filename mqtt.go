package nara

import (
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
)

func (network *Network) mqttOnConnectHandler() mqtt.OnConnectHandler {
	var connectHandler mqtt.OnConnectHandler = func(client mqtt.Client) {
		logrus.Println("Connected to MQTT")

		if token := client.Subscribe("nara/newspaper/#", 0, network.newspaperHandler); token.Wait() && token.Error() != nil {
			panic(token.Error())
		}

		if token := client.Subscribe("nara/plaza/hey_there", 0, network.heyThereHandler); token.Wait() && token.Error() != nil {
			panic(token.Error())
		}

		if token := client.Subscribe("nara/plaza/chau", 0, network.chauHandler); token.Wait() && token.Error() != nil {
			panic(token.Error())
		}

		network.heyThere()
	}
	return connectHandler
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
