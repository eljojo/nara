package nara

import (
	"bytes"
	"encoding/json"
	"embed"
	"io/fs"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"

	"github.com/bugsnag/bugsnag-go"
	"github.com/sirupsen/logrus"
)

//go:embed nara-web/public/*
var staticContent embed.FS

func (network *Network) startHttpServer(serveUI bool) error {
	listen_interface := fmt.Sprintf("%s:0", network.local.Me.Ip)
	listener, err := net.Listen("tcp", listen_interface)
	if err != nil {
		return fmt.Errorf("listen error: %w", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	url := fmt.Sprintf("http://%s:%d", network.local.Me.Ip, port)
	network.local.Me.ApiUrl = url
	network.local.Me.HttpPort = port
	logrus.Printf("Listening on %s", url)

	http.HandleFunc("/ping_events", network.httpPingDbHandler)
	http.HandleFunc("/wave_message", network.httpWaveMessageHandler)
	http.HandleFunc("/message", network.httpNewWaveMessageHandler)

	if serveUI {
		http.HandleFunc("/api.json", network.httpApiJsonHandler)
		http.HandleFunc("/narae.json", network.httpNaraeJsonHandler)
		http.HandleFunc("/last_wave.json", network.httpLastWaveJsonHandler)
		http.HandleFunc("/traefik.json", network.httpTraefikJsonHandler)
		http.HandleFunc("/metrics", network.httpMetricsHandler)
		http.HandleFunc("/status/", network.httpStatusJsonHandler)
		publicFS, _ := fs.Sub(staticContent, "nara-web/public")
		http.Handle("/", http.FileServer(http.FS(publicFS)))
	} else {
		// also when not serving UI it's useful to have these endpoints
		// TODO: maybe we should just serve them all the time?
		// http.HandleFunc("/api.json", network.httpApiJsonHandler)
		// http.HandleFunc("/narae.json", network.httpNaraeJsonHandler)
		// http.HandleFunc("/last_wave.json", network.httpLastWaveJsonHandler)
		// http.HandleFunc("/status/", network.httpStatusJsonHandler)
		http.HandleFunc("/", network.httpHomepageHandler)
	}

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
		return fmt.Errorf("failed to decode response: %w %s", err, body)
	}

	return nil
}

func (network *Network) httpApiJsonHandler(w http.ResponseWriter, r *http.Request) {
	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	var naras []map[string]interface{}
	for _, nara := range network.Neighbourhood {
		// we need to merge Name into Status to match legacy API
		statusMap := make(map[string]interface{})
		jsonStatus, _ := json.Marshal(nara.Status)
		json.Unmarshal(jsonStatus, &statusMap)
		statusMap["Name"] = nara.Name
		naras = append(naras, statusMap)
	}

	response := map[string]interface{}{
		"naras":  naras,
		"server": network.local.Me.Name,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (network *Network) httpNaraeJsonHandler(w http.ResponseWriter, r *http.Request) {
	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	var naras []map[string]interface{}
	for _, nara := range network.Neighbourhood {
		naraMap := map[string]interface{}{
			"Name":         nara.Name,
			"Flair":        nara.Status.Flair,
			"LicensePlate": nara.Status.LicensePlate,
			"Buzz":         nara.Status.Buzz,
			"Chattiness":   nara.Status.Chattiness,
			"LastSeen":     nara.Status.Observations[nara.Name].LastSeen,
			"LastRestart":  nara.Status.Observations[nara.Name].LastRestart,
			"Online":       nara.Status.Observations[nara.Name].Online,
			"StartTime":    nara.Status.Observations[nara.Name].StartTime,
			"Restarts":     nara.Status.Observations[nara.Name].Restarts,
			"Uptime":       nara.Status.HostStats.Uptime,
		}
		naras = append(naras, naraMap)
	}

	response := map[string]interface{}{
		"naras":  naras,
		"server": network.local.Me.Name,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (network *Network) httpLastWaveJsonHandler(w http.ResponseWriter, r *http.Request) {
	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	response := map[string]interface{}{
		"wave":   network.LastWave,
		"server": network.local.Me.Name,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (network *Network) httpStatusJsonHandler(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Path[len("/status/") : len(r.URL.Path)-len(".json")]
	network.local.mu.Lock()
	nara, exists := network.Neighbourhood[name]
	network.local.mu.Unlock()

	if !exists {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(nara.Status)
}

func (network *Network) httpMetricsHandler(w http.ResponseWriter, r *http.Request) {
	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	var lines []string

	lines = append(lines, "# HELP nara_info Basic string data from each Nara")
	lines = append(lines, "# TYPE nara_info gauge")
	lines = append(lines, "# HELP nara_online 1 if the Nara is ONLINE, else 0")
	lines = append(lines, "# TYPE nara_online gauge")
	lines = append(lines, "# HELP nara_buzz Buzz level reported by the Nara")
	lines = append(lines, "# TYPE nara_buzz gauge")
	lines = append(lines, "# HELP nara_chattiness Chattiness level reported by the Nara")
	lines = append(lines, "# TYPE nara_chattiness gauge")
	lines = append(lines, "# HELP nara_last_seen Unix timestamp when the Nara was last seen")
	lines = append(lines, "# TYPE nara_last_seen gauge")
	lines = append(lines, "# HELP nara_last_restart Unix timestamp when the Nara last restarted")
	lines = append(lines, "# TYPE nara_last_restart gauge")
	lines = append(lines, "# HELP nara_start_time Unix timestamp when the Nara started")
	lines = append(lines, "# TYPE nara_start_time gauge")
	lines = append(lines, "# HELP nara_uptime_seconds Uptime reported by the host")
	lines = append(lines, "# TYPE nara_uptime_seconds gauge")
	lines = append(lines, "# HELP nara_restarts_total Restart count from the Nara")
	lines = append(lines, "# TYPE nara_restarts_total counter")

	for _, nara := range network.Neighbourhood {
		obs := nara.Status.Observations[nara.Name]

		lines = append(lines, fmt.Sprintf(`nara_info{name="%s",flair="%s",license_plate="%s"} 1`, nara.Name, nara.Status.Flair, nara.Status.LicensePlate))

		onlineValue := 0
		if obs.Online == "ONLINE" {
			onlineValue = 1
		}
		lines = append(lines, fmt.Sprintf(`nara_online{name="%s"} %d`, nara.Name, onlineValue))
		lines = append(lines, fmt.Sprintf(`nara_buzz{name="%s"} %d`, nara.Name, nara.Status.Buzz))
		lines = append(lines, fmt.Sprintf(`nara_chattiness{name="%s"} %d`, nara.Name, nara.Status.Chattiness))

		if obs.LastSeen > 0 {
			lines = append(lines, fmt.Sprintf(`nara_last_seen{name="%s"} %d`, nara.Name, obs.LastSeen))
		}
		if obs.LastRestart > 0 {
			lines = append(lines, fmt.Sprintf(`nara_last_restart{name="%s"} %d`, nara.Name, obs.LastRestart))
		}
		if obs.StartTime > 0 {
			lines = append(lines, fmt.Sprintf(`nara_start_time{name="%s"} %d`, nara.Name, obs.StartTime))
		}
		if nara.Status.HostStats.Uptime > 0 {
			lines = append(lines, fmt.Sprintf(`nara_uptime_seconds{name="%s"} %d`, nara.Name, nara.Status.HostStats.Uptime))
		}
		lines = append(lines, fmt.Sprintf(`nara_restarts_total{name="%s"} %d`, nara.Name, obs.Restarts))
	}

	w.Header().Set("Content-Type", "text/plain")
	for _, line := range lines {
		fmt.Fprintln(w, line)
	}
}

func (network *Network) httpTraefikJsonHandler(w http.ResponseWriter, r *http.Request) {
	network.local.mu.Lock()
	defer network.local.mu.Unlock()

	routers := make(map[string]interface{})
	services := make(map[string]interface{})

	for _, nara := range network.Neighbourhood {
		if nara.Status.Observations[nara.Name].Online != "ONLINE" || nara.ApiUrl == "" {
			continue
		}

		name := nara.Name
		domain := fmt.Sprintf("%s.nara.network", name)
		serviceName := fmt.Sprintf("%s-api", name)

		routers[serviceName] = map[string]interface{}{
			"entryPoints": []string{"public"},
			"rule":        fmt.Sprintf("Host(`%s`)", domain), // Fixed backticks for Host rule
			"service":     serviceName,
		}
		routers[fmt.Sprintf("%s-secure", serviceName)] = map[string]interface{}{
			"entryPoints": []string{"public-secure"},
			"rule":        fmt.Sprintf("Host(`%s`)", domain), // Fixed backticks for Host rule
			"service":     serviceName,
			"tls":         map[string]interface{}{},
		}

		services[serviceName] = map[string]interface{}{
			"loadBalancer": map[string]interface{}{
				"servers": []map[string]string{{"url": nara.ApiUrl}},
			},
		}
	}

	response := map[string]interface{}{
		"http": map[string]interface{}{"routers": routers, "services": services},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
