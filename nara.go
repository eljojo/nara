package main

import (
	"encoding/json"
	"flag"
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
	"github.com/sparrc/go-ping"
	"math/rand"
	// "strconv"
	"strings"
	"time"
)

type Nara struct {
	Name   string
	Status NaraStatus
}

type NaraStatus struct {
	PingGoogle string
}

var me = &Nara{}
var inbox = make(chan [2]string, 1)
var neighbourhood = make(map[string]Nara)
var lastHeyThere int64

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
	go measurePing()

	for {
		<-inbox
	}
}

func announce(client mqtt.Client) {
	topic := fmt.Sprintf("%s/%s", "nara/plaza", me.Name)
	logrus.Println("announcing self on", topic)

	payload, err := json.Marshal(me.Status)
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

func plazaHandler(client mqtt.Client, msg mqtt.Message) {
	var from = strings.Split(msg.Topic(), "nara/plaza/")[1]

	if from == me.Name {
		return
	}

	var status NaraStatus
	json.Unmarshal(msg.Payload(), &status)

	fmt.Printf("plazaHandler ")
	fmt.Printf("update from %s: ", from)
	fmt.Printf("%s\n", status)

	other, present := neighbourhood[from]
	if present {
		other.Status = status
	} else {
		heyThere(client)
	}
	inbox <- [2]string{msg.Topic(), string(msg.Payload())}
}

func heyThereHandler(client mqtt.Client, msg mqtt.Message) {
	var nara Nara
	json.Unmarshal(msg.Payload(), &nara)
	neighbourhood[nara.Name] = nara

	fmt.Printf("heyThereHandler ")
	fmt.Printf("%s\n", neighbourhood)
	heyThere(client)
}

func heyThere(client mqtt.Client) {
	if (time.Now().Unix() - lastHeyThere) <= 5 {
		return
	}
	lastHeyThere = time.Now().Unix()

	topic := "nara/hey_there"
	logrus.Println("announcing self on", topic)

	payload, err := json.Marshal(me)
	if err != nil {
		fmt.Println(err)
		return
	}
	token := client.Publish(topic, 0, false, string(payload))
	token.Wait()
}

func measurePing() {
	for {
		pinger, err := ping.NewPinger("8.8.8.8")
		if err != nil {
			panic(err)
		}
		pinger.Count = 5
		err = pinger.Run() // blocks until finished
		if err != nil {
			// panic(err)
			continue
		}
		stats := pinger.Statistics() // get send/receive/rtt stats

		// me.Status.PingGoogle = fmt.Sprintf("%sms", strconv.Itoa(rand.Intn(100)))
		me.Status.PingGoogle = stats.AvgRtt.String()
		time.Sleep(5 * time.Second)
	}
}

var connectHandler mqtt.OnConnectHandler = func(client mqtt.Client) {
	logrus.Println("Connected to MQTT")

	if token := client.Subscribe("nara/plaza/#", 0, plazaHandler); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	if token := client.Subscribe("nara/hey_there", 0, heyThereHandler); token.Wait() && token.Error() != nil {
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
