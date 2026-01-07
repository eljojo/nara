package nara

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strconv"
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
		sender, err := VerifyMeshRequest(r, network.getPublicKeyForNara)
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
	headers    http.Header
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
	sw.ResponseWriter.Write(body)
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

	sender, err := VerifyMeshResponse(resp, body, network.getPublicKeyForNara)
	if err != nil {
		logrus.Warnf("🚫 mesh response auth failed: %v", err)
		return body, false // Return body anyway, but indicate not verified
	}

	logrus.Debugf("✅ verified mesh response from %s", sender)
	return body, true
}
