package nara

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// Mesh HTTP Authentication
//
// All mesh HTTP traffic (except /ping) requires Ed25519 signatures:
//
// REQUEST:
//   X-Nara-Name: alice
//   X-Nara-Timestamp: 1704067200000 (unix millis)
//   X-Nara-Signature: base64(sign(name + timestamp + method + path))
//
// RESPONSE:
//   X-Nara-Name: bob
//   X-Nara-Timestamp: 1704067200123
//   X-Nara-Signature: base64(sign(name + timestamp + sha256(body)))
//
// Requests are rejected if:
// - Missing required headers
// - Timestamp too old (>30 seconds)
// - Signature doesn't verify against sender's public key (from MQTT broadcasts)

const (
	// MeshAuthMaxAge is how old a request timestamp can be before rejection
	MeshAuthMaxAge = 30 * time.Second

	// Header names for mesh authentication
	HeaderNaraName      = "X-Nara-Name"
	HeaderNaraTimestamp = "X-Nara-Timestamp"
	HeaderNaraSignature = "X-Nara-Signature"
)

// MeshAuthRequest creates signed request headers
func MeshAuthRequest(name string, keypair NaraKeypair, method, path string) http.Header {
	timestamp := time.Now().UnixMilli()
	message := fmt.Sprintf("%s%d%s%s", name, timestamp, method, path)

	headers := http.Header{}
	headers.Set(HeaderNaraName, name)
	headers.Set(HeaderNaraTimestamp, strconv.FormatInt(timestamp, 10))
	headers.Set(HeaderNaraSignature, keypair.SignBase64([]byte(message)))
	return headers
}

// MeshAuthResponse creates signed response headers for a response body
func MeshAuthResponse(name string, keypair NaraKeypair, body []byte) http.Header {
	timestamp := time.Now().UnixMilli()
	bodyHash := sha256.Sum256(body)
	message := fmt.Sprintf("%s%d%s", name, timestamp, base64.StdEncoding.EncodeToString(bodyHash[:]))

	headers := http.Header{}
	headers.Set(HeaderNaraName, name)
	headers.Set(HeaderNaraTimestamp, strconv.FormatInt(timestamp, 10))
	headers.Set(HeaderNaraSignature, keypair.SignBase64([]byte(message)))
	return headers
}

// VerifyMeshRequest verifies the signature on an incoming mesh HTTP request
func VerifyMeshRequest(r *http.Request, getPublicKey func(string) []byte) (string, error) {
	name := r.Header.Get(HeaderNaraName)
	timestampStr := r.Header.Get(HeaderNaraTimestamp)
	signatureStr := r.Header.Get(HeaderNaraSignature)

	if name == "" || timestampStr == "" || signatureStr == "" {
		return "", fmt.Errorf("missing auth headers")
	}

	// Parse and validate timestamp
	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid timestamp")
	}

	requestTime := time.UnixMilli(timestamp)
	age := time.Since(requestTime)
	if age > MeshAuthMaxAge || age < -MeshAuthMaxAge {
		return "", fmt.Errorf("timestamp too old or in future: %v", age)
	}

	// Get sender's public key
	pubKey := getPublicKey(name)
	if pubKey == nil {
		return "", fmt.Errorf("unknown sender: %s", name)
	}

	// Decode signature
	signature, err := base64.StdEncoding.DecodeString(signatureStr)
	if err != nil {
		return "", fmt.Errorf("invalid signature encoding")
	}

	// Verify signature
	message := fmt.Sprintf("%s%d%s%s", name, timestamp, r.Method, r.URL.Path)
	if !VerifySignature(pubKey, []byte(message), signature) {
		return "", fmt.Errorf("signature verification failed")
	}

	return name, nil
}

// VerifyMeshResponse verifies the signature on a mesh HTTP response
func VerifyMeshResponse(resp *http.Response, body []byte, getPublicKey func(string) []byte) (string, error) {
	name := resp.Header.Get(HeaderNaraName)
	timestampStr := resp.Header.Get(HeaderNaraTimestamp)
	signatureStr := resp.Header.Get(HeaderNaraSignature)

	if name == "" || timestampStr == "" || signatureStr == "" {
		return "", fmt.Errorf("missing auth headers in response")
	}

	// Parse timestamp
	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid timestamp in response")
	}

	// Get responder's public key
	pubKey := getPublicKey(name)
	if pubKey == nil {
		return "", fmt.Errorf("unknown responder: %s", name)
	}

	// Decode signature
	signature, err := base64.StdEncoding.DecodeString(signatureStr)
	if err != nil {
		return "", fmt.Errorf("invalid signature encoding in response")
	}

	// Verify signature (over name + timestamp + sha256(body))
	bodyHash := sha256.Sum256(body)
	message := fmt.Sprintf("%s%d%s", name, timestamp, base64.StdEncoding.EncodeToString(bodyHash[:]))
	if !VerifySignature(pubKey, []byte(message), signature) {
		return "", fmt.Errorf("response signature verification failed")
	}

	return name, nil
}

// tryDiscoverUnknownSender attempts to discover an unknown sender by fetching their
// public key via /ping from their mesh IP. This enables new naras to be recognized
// immediately when they first contact us, rather than waiting for MQTT broadcasts.
func (network *Network) tryDiscoverUnknownSender(name, remoteAddr string) bool {
	// Extract IP from remoteAddr (format: "100.64.0.x:port")
	ip := remoteAddr
	if colonIdx := strings.LastIndex(remoteAddr, ":"); colonIdx != -1 {
		ip = remoteAddr[:colonIdx]
	}

	// Only try for mesh IPs (100.64.x.x range)
	if !strings.HasPrefix(ip, "100.64.") {
		return false
	}

	// Try to fetch their public key via /ping
	// Must use tsnet HTTP client to route through the mesh
	if network.tsnetMesh == nil || network.tsnetMesh.Server() == nil {
		logrus.Debugf("📡 Cannot discover %s: no tsnet mesh available", name)
		return false
	}

	url := network.buildMeshURLFromIP(ip, "/ping")
	client := network.getMeshHTTPClient()
	if client == nil {
		logrus.Debugf("📡 Cannot discover %s: no mesh HTTP client available", name)
		return false
	}

	ctx, cancel := context.WithTimeout(network.ctx, 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false
	}

	resp, err := client.Do(req)
	if err != nil {
		logrus.Debugf("📡 Failed to ping %s at %s: %v", name, ip, err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	var pingResp struct {
		From      string `json:"from"`
		PublicKey string `json:"public_key"`
		MeshIP    string `json:"mesh_ip"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pingResp); err != nil {
		return false
	}

	// Verify the response is from who we expected
	if pingResp.From != name {
		logrus.Warnf("📡 Name mismatch: expected %s, got %s from %s", name, pingResp.From, ip)
		return false
	}

	if pingResp.PublicKey == "" {
		return false
	}

	// Import the newly discovered nara
	nara := NewNara(NaraName(name))
	nara.Status.PublicKey = pingResp.PublicKey
	nara.Status.MeshIP = pingResp.MeshIP
	nara.Status.MeshEnabled = true
	network.importNara(nara)

	// Log via LogService (batched)
	if network.logService != nil {
		network.logService.BatchDiscovery(NaraName(name))
	}
	return true
}

// meshAuthMiddleware wraps a handler to require mesh authentication
// Skip authentication for /ping endpoint (latency-critical)
func (network *Network) meshAuthMiddleware(path string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for /ping - needs to be fast
		if path == "/ping" {
			handler(w, r)
			return
		}

		// Verify request signature
		sender, err := VerifyMeshRequest(r, func(name string) []byte {
			return network.resolvePublicKeyForNara(NaraName(name))
		})

		// If sender is unknown, try to discover them via /ping
		if err != nil && strings.Contains(err.Error(), "unknown sender") {
			senderName := r.Header.Get(HeaderNaraName)
			if senderName != "" && network.tryDiscoverUnknownSender(senderName, r.RemoteAddr) {
				// Retry verification with newly discovered key
				sender, err = VerifyMeshRequest(r, func(name string) []byte {
					return network.resolvePublicKeyForNara(NaraName(name))
				})
			}
		}

		if err != nil {
			logrus.Warnf("🚫 mesh auth failed from %s: %v", r.RemoteAddr, err)
			http.Error(w, "Authentication failed: "+err.Error(), http.StatusUnauthorized)
			return
		}

		// Store verified sender in header for handler to use
		r.Header.Set("X-Nara-Verified", sender)

		// Wrap response writer to capture body for signing
		sw := &signedResponseWriter{
			ResponseWriter: w,
			buf:            &bytes.Buffer{},
			network:        network,
		}

		handler(sw, r)

		// Sign and send response
		sw.finalize()
	}
}

// signedResponseWriter captures the response body for signing
type signedResponseWriter struct {
	http.ResponseWriter
	buf        *bytes.Buffer
	network    *Network
	statusCode int
}

func (sw *signedResponseWriter) Write(b []byte) (int, error) {
	return sw.buf.Write(b)
}

func (sw *signedResponseWriter) WriteHeader(code int) {
	sw.statusCode = code
}

func (sw *signedResponseWriter) finalize() {
	body := sw.buf.Bytes()

	// Add auth headers to response
	authHeaders := MeshAuthResponse(sw.network.meName(), sw.network.local.Keypair, body)
	for k, v := range authHeaders {
		sw.ResponseWriter.Header()[k] = v
	}

	// Write status code if set
	if sw.statusCode != 0 {
		sw.ResponseWriter.WriteHeader(sw.statusCode)
	}

	// Write body
	if _, err := sw.ResponseWriter.Write(body); err != nil {
		logrus.WithError(err).Warn("Failed to write response body")
	}
}

// AddMeshAuthHeaders adds authentication headers to an outgoing HTTP request
func (network *Network) AddMeshAuthHeaders(req *http.Request) {
	headers := MeshAuthRequest(network.meName(), network.local.Keypair, req.Method, req.URL.Path)
	for k, v := range headers {
		req.Header[k] = v
	}
}

// VerifyMeshResponseBody verifies a mesh HTTP response and returns the body
func (network *Network) VerifyMeshResponseBody(resp *http.Response) ([]byte, bool) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logrus.Warnf("🚫 failed to read response body: %v", err)
		return nil, false
	}

	if _, err := VerifyMeshResponse(resp, body, network.resolvePublicKeyForNara); err != nil {
		logrus.Warnf("🚫 mesh response auth failed: %v", err)
		return body, false // Return body anyway, but indicate not verified
	}

	return body, true
}
