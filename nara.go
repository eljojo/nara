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
	"os/signal"
	"runtime"
	"sort"
	"syscall"

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
	PingStats  map[string]float64
	HostStats  HostStats
	LastSeen   int64
	Chattiness int64
}

type HostStats struct {
	Uptime  uint64
	LoadAvg float64
}

var me = &Nara{}

// var inbox = make(chan [2]string)
var neighbourhood = make(map[string]Nara)
var lastHeyThere int64

func main() {
	rand.Seed(time.Now().UnixNano())

	mqttHostPtr := flag.String("mqtt-host", "tcp://hass.eljojo.casa:1883", "mqtt server hostname")
	mqttUserPtr := flag.String("mqtt-user", "my_username", "mqtt server username")
	mqttPassPtr := flag.String("mqtt-pass", "my_password", "mqtt server password")
	naraIdPtr := flag.String("nara-id", "raspberry", "nara id")
	showNeighboursPtr := flag.Bool("show-neighbours", true, "show table with neighbourhood")
	showNeighboursSpeedPtr := flag.Int("refresh-rate", 60, "refresh rate in seconds for neighbourhood table")

	flag.Parse()
	me.Name = *naraIdPtr
	me.Status.PingStats = make(map[string]float64)
	me.StartTime = time.Now().Unix()
	me.Status.LastSeen = time.Now().Unix()
	me.Status.Chattiness = 50

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
	go measurePingForever()
	go updateHostStats()
	if *showNeighboursPtr {
		go printNeigbourhoodForever(*showNeighboursSpeedPtr)
	}

	SetupCloseHandler(client)
	defer chau(client)

	for {
		time.Sleep(10 * time.Millisecond)
		runtime.Gosched() // https://blog.container-solutions.com/surprise-golang-thread-scheduling
		// <-inbox
	}
}

func SetupCloseHandler(client mqtt.Client) {
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		fmt.Println("babaayyy")
		chau(client)
		os.Exit(0)
	}()
}

func announce(client mqtt.Client) {
	topic := fmt.Sprintf("%s/%s", "nara/newspaper", me.Name)
	logrus.Println("posting on", topic)

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
	for {
		ts := chattinessRate(*me, 5, 60)
		time.Sleep(time.Duration(ts) * time.Second)

		announce(client)
	}
}

func chattinessRate(nara Nara, min int64, max int64) int64 {
	return min + ((max - min) * (100 - nara.Status.Chattiness) / 100)
}

func newspaperHandler(client mqtt.Client, msg mqtt.Message) {
	if me.Status.Chattiness <= 10 && rand.Intn(int(me.Status.Chattiness)+1) == 0 {
		// logrus.Println("skipping newspaper event due to low chattiness")
		return
	}
	if !strings.Contains(msg.Topic(), "nara/newspaper/") {
		return
	}
	var from = strings.Split(msg.Topic(), "nara/newspaper/")[1]

	if from == me.Name {
		return
	}

	var status NaraStatus
	json.Unmarshal(msg.Payload(), &status)

	// logrus.Printf("newspaperHandler update from %s: %+v", from, status)

	other, present := neighbourhood[from]
	if present {
		status.LastSeen = time.Now().Unix()
		other.Status = status
		neighbourhood[from] = other
	} else {
		logrus.Println("whodis?", from)
		if me.Status.Chattiness > 0 {
			heyThere(client)
		}
	}
	// inbox <- [2]string{msg.Topic(), string(msg.Payload())}
}

func heyThereHandler(client mqtt.Client, msg mqtt.Message) {
	var nara Nara
	json.Unmarshal(msg.Payload(), &nara)

	if nara.Name == me.Name || nara.Name == "" {
		return
	}

	nara.Status.LastSeen = time.Now().Unix()
	neighbourhood[nara.Name] = nara
	logrus.Printf("%s: hey there!", nara.Name)
	// logrus.Printf("neighbourhood: %+v", neighbourhood)

	// sleep some random amount to avoid ddosing new friends
	time.Sleep(time.Duration(rand.Intn(10)) * time.Second)

	heyThere(client)
}

func heyThere(client mqtt.Client) {
	ts := chattinessRate(*me, 45, 120)
	if (time.Now().Unix() - lastHeyThere) <= ts {
		return
	}

	lastHeyThere = time.Now().Unix()

	topic := "nara/plaza/hey_there"
	logrus.Printf("posting to %s", topic)

	payload, err := json.Marshal(me)
	if err != nil {
		fmt.Println(err)
		return
	}
	token := client.Publish(topic, 0, false, string(payload))
	token.Wait()
}

func chauHandler(client mqtt.Client, msg mqtt.Message) {
	var nara Nara
	json.Unmarshal(msg.Payload(), &nara)

	if nara.Name == me.Name || nara.Name == "" {
		return
	}

	_, present := neighbourhood[nara.Name]
	if present {
		delete(neighbourhood, nara.Name)
	}

	logrus.Printf("%s: chau!", nara.Name)
}

func chau(client mqtt.Client) {
	topic := "nara/plaza/chau"
	logrus.Printf("posting to %s", topic)

	payload, err := json.Marshal(me)
	if err != nil {
		fmt.Println(err)
		return
	}
	token := client.Publish(topic, 0, false, string(payload))
	token.Wait()
}

func measurePingForever() {
	for {
		measureAndStorePing("google", "8.8.8.8")

		for name, nara := range neighbourhood {
			measureAndStorePing(name, nara.Ip)
		}
		ts := chattinessRate(*me, 5, 120)
		time.Sleep(time.Duration(ts) * time.Second)
	}
}

func measureAndStorePing(name string, dest string) {
	ping, err := measurePing(name, dest)
	if err == nil {
		me.Status.PingStats[name] = ping
	} else {
		delete(me.Status.PingStats, name)
	}
}

func measurePing(name string, dest string) (float64, error) {
	pinger, err := ping.NewPinger(dest)
	if err != nil {
		return 0, err
	}
	pinger.Count = 5
	err = pinger.Run() // blocks until finished
	if err != nil {
		return 0, err
	}
	stats := pinger.Statistics() // get send/receive/rtt stats
	return float64(stats.AvgRtt/time.Microsecond) / 1000, nil
}

func updateHostStats() {
	for {
		uptime, _ := host.Uptime()
		me.Status.HostStats.Uptime = uptime

		load, _ := load.Avg()
		me.Status.HostStats.LoadAvg = load.Load1

		if load.Load1 < 1 {
			me.Status.Chattiness = int64((1 - load.Load1) * 100)
		} else {
			me.Status.Chattiness = 0
		}

		time.Sleep(5 * time.Second)
	}
}

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
	Name       string  `header:"name"`
	Ip         string  `header:"IP"`
	Ping       string  `header:"ping"`
	LastSeen   string  `header:"last seen"`
	Uptime     string  `header:"uptime"`
	Load       float64 `header:"load"`
	Chattiness int64   `header:"chat"`
}

func printNeigbourhoodForever(refreshRate int) {
	for {
		printNeigbourhood()
		time.Sleep(time.Duration(refreshRate) * time.Second)
	}
}

func printNeigbourhood() {
	now := time.Now().Unix()

	printer := tableprinter.New(os.Stdout)
	naras := make([]neighbour, 0, len(neighbourhood)+1)

	uptime := fmt.Sprintf("%ds", now-me.StartTime)
	nei := neighbour{me.Name, me.Ip, "-", "-", uptime, me.Status.HostStats.LoadAvg, me.Status.Chattiness}
	naras = append(naras, nei)

	for _, nara := range neighbourhood {
		ping := pingBetweenMs(*me, nara)
		lastSeen := fmt.Sprintf("%ds ago", now-nara.Status.LastSeen)
		uptime := fmt.Sprintf("%ds", now-nara.StartTime)
		loadAvg := nara.Status.HostStats.LoadAvg
		nei := neighbour{nara.Name, nara.Ip, ping, lastSeen, uptime, loadAvg, nara.Status.Chattiness}
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

func pingBetween(a Nara, b Nara) float64 {
	ping, present := a.Status.PingStats[b.Name]
	if !present {
		ping, _ = b.Status.PingStats[a.Name]
	}
	return ping
}

func pingBetweenMs(a Nara, b Nara) string {
	ping := pingBetween(a, b)
	if ping == 0 {
		return ""
	}
	return fmt.Sprintf("%.2fms", ping)
}
