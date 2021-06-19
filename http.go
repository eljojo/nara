package nara

import (
	"encoding/json"
	"fmt"
	"github.com/sirupsen/logrus"
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
	logrus.Printf("Listening for API HTTP server on %s", url)
	network.local.Me.ApiUrl = url

	http.HandleFunc("/ping_events", network.httpPingDbHandler)

	go http.Serve(listener, nil)
	return nil
}

func (network *Network) httpPingDbHandler(w http.ResponseWriter, r *http.Request) {
	pingEvents := network.pingEvents()

	payload, err := json.Marshal(pingEvents)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Fprint(w, string(payload))
}
