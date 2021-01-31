package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/shirou/gopsutil/host"
	"github.com/shirou/gopsutil/load"
	"github.com/sirupsen/logrus"
	"github.com/sparrc/go-ping"
	"math/rand"
	"net"
	// "strconv"
	"strings"
	"time"

	"os"
	"sort"

	"github.com/kataras/tablewriter"
	"github.com/lensesio/tableprinter"
)

type Nara struct {
	Name      string
	Hostname  string
	Ip        string
	Status    NaraStatus
	StartTime int64
}

type NaraStatus struct {
	PingStats map[string]string
	HostStats HostStats
	LastSeen  int64
}

type HostStats struct {
	Uptime  uint64
	LoadAvg float64
}

var me = &Nara{}

// var inbox = make(chan [2]string, 1)
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
	me.Status.PingStats = make(map[string]string)
	me.StartTime = time.Now().Unix()
	me.Status.LastSeen = time.Now().Unix()

	ip, err := externalIP()
	if err == nil {
		me.Ip = ip
		logrus.Println("local ip", ip)
	} else {
		logrus.Panic(err)
	}

	hostinfo, _ := host.Info()
	me.Hostname = hostinfo.Hostname

	client := connectMQTT(*mqttHostPtr, *mqttUserPtr, *mqttPassPtr, *naraIdPtr)
	go announceForever(client)
	// go measurePing("google", "8.8.8.8")
	go updateHostStats()
	go printNeigbourhoodForever()

	for {
		//<-inbox
	}
}

func announce(client mqtt.Client) {
	topic := fmt.Sprintf("%s/%s", "nara/plaza", me.Name)
	logrus.Println("plaza shoutout on", topic)

	me.Status.LastSeen = time.Now().Unix()

	payload, err := json.Marshal(me.Status)
	if err != nil {
		fmt.Println(err)
		return
	}
	token := client.Publish(topic, 0, false, string(payload))
	token.Wait()
}

func announceForever(client mqtt.Client) {
	// chattiness := rand.Intn(15) + 5
	chattiness := 15
	for {
		time.Sleep(time.Duration(rand.Intn(30)+chattiness) * time.Second)

		announce(client)
	}
}

func plazaHandler(client mqtt.Client, msg mqtt.Message) {
	if !strings.Contains(msg.Topic(), "nara/plaza/") {
		return
	}
	var from = strings.Split(msg.Topic(), "nara/plaza/")[1]

	if from == me.Name {
		return
	}

	var status NaraStatus
	json.Unmarshal(msg.Payload(), &status)

	// logrus.Printf("plazaHandler update from %s: %+v", from, status)

	other, present := neighbourhood[from]
	if present {
		status.LastSeen = time.Now().Unix()
		other.Status = status
		neighbourhood[from] = other
	} else {
		logrus.Println("unknown neighbour", from)
		heyThere(client)
	}
	// inbox <- [2]string{msg.Topic(), string(msg.Payload())}
}

func heyThereHandler(client mqtt.Client, msg mqtt.Message) {
	var nara Nara
	json.Unmarshal(msg.Payload(), &nara)

	if nara.Name == me.Name {
		return
	}

	_, present := neighbourhood[nara.Name]
	if !present {
		// go measurePing(nara.Name, nara.Ip)
	}

	nara.Status.LastSeen = time.Now().Unix()
	neighbourhood[nara.Name] = nara
	logrus.Println("heyThereHandler discovered", nara.Name)
	// logrus.Printf("neighbourhood: %+v", neighbourhood)

	// sleep some random amount to avoid ddosing new friends
	time.Sleep(time.Duration(rand.Intn(10)) * time.Second)

	heyThere(client)
}

func heyThere(client mqtt.Client) {
	if (time.Now().Unix() - lastHeyThere) <= 30 {
		return
	}

	lastHeyThere = time.Now().Unix()

	topic := "nara/hey_there"
	logrus.Printf("hey there! announcing on %s", topic)

	payload, err := json.Marshal(me)
	if err != nil {
		fmt.Println(err)
		return
	}
	token := client.Publish(topic, 0, false, string(payload))
	token.Wait()
}

func measurePing(name string, dest string) {
	logrus.Println("setting up pinger for", name, dest)
	for {
		pinger, err := ping.NewPinger(dest)
		if err != nil {
			me.Status.PingStats[name] = "error"
			time.Sleep(30 * time.Second)
			//panic(err)
			continue
		}
		pinger.Count = 5
		err = pinger.Run() // blocks until finished
		if err != nil {
			me.Status.PingStats[name] = "error"
			time.Sleep(30 * time.Second)
			// panic(err)
			continue
		}
		stats := pinger.Statistics() // get send/receive/rtt stats

		// me.Status.PingGoogle = fmt.Sprintf("%sms", strconv.Itoa(rand.Intn(100)))
		me.Status.PingStats[name] = stats.AvgRtt.String()
		time.Sleep(30 * time.Second)
	}
}

func updateHostStats() {
	for {
		uptime, _ := host.Uptime()
		me.Status.HostStats.Uptime = uptime

		load, _ := load.Avg()
		me.Status.HostStats.LoadAvg = load.Load1

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

// https://stackoverflow.com/questions/23558425/how-do-i-get-the-local-ip-address-in-go
func externalIP() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue // interface down
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue // loopback interface
		}
		addrs, err := iface.Addrs()
		if err != nil {
			return "", err
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip = ip.To4()
			if ip == nil {
				continue // not an ipv4 address
			}

			// HACK
			if ip.String() == "192.168.0.2" {
				continue
			}

			return ip.String(), nil
		}
	}
	return "", errors.New("are you connected to the network?")
}

type neighbour struct {
	Name     string  `header:"name"`
	Ip       string  `header:"IP"`
	Ping     string  `header:"ping"`
	LastSeen string  `header:"last seen"`
	Uptime   string  `header:"uptime"`
	Load     float64 `header:"load"`
}

func printNeigbourhoodForever() {
	for {
		printNeigbourhood()
		time.Sleep(5 * time.Second)
	}
}

func printNeigbourhood() {
	now := time.Now().Unix()

	printer := tableprinter.New(os.Stdout)
	naras := make([]neighbour, 0, len(neighbourhood))

	for _, nara := range neighbourhood {
		ping, present := me.Status.PingStats[nara.Name]
		if !present {
			ping, _ = nara.Status.PingStats[me.Name]
		}
		lastSeen := fmt.Sprintf("%ds ago", now-nara.Status.LastSeen)
		uptime := fmt.Sprintf("%ds", now-nara.StartTime)
		loadAvg := nara.Status.HostStats.LoadAvg
		nei := neighbour{nara.Name, nara.Ip, ping, lastSeen, uptime, loadAvg}
		naras = append(naras, nei)
	}

	sort.Slice(naras, func(i, j int) bool {
		return naras[j].Name > naras[i].Name
	})

	// Optionally, customize the table, import of the underline 'tablewriter' package is required for that.
	printer.BorderTop, printer.BorderBottom, printer.BorderLeft, printer.BorderRight = true, true, true, true
	printer.CenterSeparator = "│"
	printer.ColumnSeparator = "│"
	printer.RowSeparator = "─"
	printer.HeaderBgColor = tablewriter.BgBlackColor
	printer.HeaderFgColor = tablewriter.FgGreenColor

	// Print the slice of structs as table, as shown above.
	printer.Print(naras)
}
