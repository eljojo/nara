package nara

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"

	"github.com/bugsnag/bugsnag-go"
	"github.com/sirupsen/logrus"
)

func (network *Network) startHttpServer() error {
	listen_interface := fmt.Sprintf("%s:0", network.local.Me.Ip)
	listener, err := net.Listen("tcp", listen_interface)
	if err != nil {
		return fmt.Errorf("listen error: %w", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	url := fmt.Sprintf("http://%s:%d", network.local.Me.Ip, port)
	network.local.Me.ApiUrl = url
	network.local.Me.HttpPort = port
	logrus.Printf("Listening on %s or %s", url, network.local.Me.ApiGatewayUrl())

	http.HandleFunc("/ping_events", network.httpPingDbHandler)
	http.HandleFunc("/wave_message", network.httpWaveMessageHandler)
	http.HandleFunc("/message", network.httpNewWaveMessageHandler)
	http.HandleFunc("/", network.httpHomepageHandler)

	go http.Serve(listener, nil)
	return nil
}

func (network *Network) httpPingDbHandler(w http.ResponseWriter, r *http.Request) {
	pingEvents := network.pingEvents()

	logrus.Printf("Serving Ping DB to %s", r.RemoteAddr)
	w.Header().Set("Content-Type", "application/json")

	payload, err := json.Marshal(pingEvents)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Fprint(w, string(payload))
}

func (network *Network) httpHomepageHandler(w http.ResponseWriter, r *http.Request) {
	logrus.Printf("Serving Status to %s", r.RemoteAddr)
	w.Header().Set("Content-Type", "application/json")

	nara := network.local.Me
	nara.mu.Lock()
	payload, err := json.Marshal(nara)
	nara.mu.Unlock()
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Fprint(w, string(payload))
}

func (network *Network) httpWaveMessageHandler(w http.ResponseWriter, r *http.Request) {
	logrus.Printf("Receiving WaveMessage from %s", r.RemoteAddr)
	decoder := json.NewDecoder(r.Body)
	var wm WaveMessage
	err := decoder.Decode(&wm)
	if err != nil {
		bugsnag.Notify(err)
		logrus.Error(err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if wm.Valid() {
		network.waveMessageInbox <- wm
	} else {
		logrus.Printf("discarding invalid WaveMessage")
		w.WriteHeader(http.StatusBadRequest)
	}
}

func (network *Network) httpNewWaveMessageHandler(w http.ResponseWriter, r *http.Request) {
	logrus.Printf("Creating new WaveMessage at request from %s", r.RemoteAddr)

	err := r.ParseForm()
	if err != nil {
		bugsnag.Notify(err)
		logrus.Error(err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	wm := newWaveMessage(network.meName(), r.FormValue("body"))

	if wm.Valid() {
		network.waveMessageInbox <- wm
	} else {
		logrus.Printf("discarding invalid WaveMessage")
		w.WriteHeader(http.StatusBadRequest)
	}
}

func (network *Network) httpPostWaveMessage(name string, wm WaveMessage) error {
	jsonValue, _ := json.Marshal(wm)
	nara := network.getNara(name)
	url := fmt.Sprintf("%s/wave_message", nara.BestApiUrl())
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonValue))
	if err != nil {
		return err
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("failed to post waveMessage to %s, response code: %d", name, resp.StatusCode)
	}

	return nil
}

func httpFetchJson(url string, result interface{}) error {
	resp, err := http.Get(url)

	if err != nil {
		return fmt.Errorf("failed to get from url %s: %w", url, err)
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		return fmt.Errorf("failed to get from url %s: %w", url, err)
	}

	err = json.Unmarshal(body, result)
	if err != nil {
		return fmt.Errorf("failed to decode response: %w\n%s", err, body)
	}

	return nil
}
