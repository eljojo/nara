package nara

import (
	"encoding/json"
	"fmt"
	"github.com/sirupsen/logrus"
	"io/ioutil"
	"net"
	"net/http"
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
	logrus.Printf("Listening on %s or %s", url, network.local.Me.ApiGatewayUrl())

	http.HandleFunc("/ping_events", network.httpPingDbHandler)

	go http.Serve(listener, nil)
	return nil
}

func (network *Network) httpPingDbHandler(w http.ResponseWriter, r *http.Request) {
	pingEvents := network.pingEvents()

	logrus.Printf("Serving Ping DB to %s", r.RemoteAddr)

	payload, err := json.Marshal(pingEvents)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Fprint(w, string(payload))
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
