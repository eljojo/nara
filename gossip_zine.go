package nara

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/eljojo/nara/types"
)

// Zine is a batch of recent events passed hand-to-hand between naras
// Like underground zines at punk shows, these spread organically through mesh network
type Zine struct {
	From      types.NaraName `json:"from"`       // Publisher nara
	CreatedAt int64          `json:"created_at"` // Unix timestamp
	Events    []SyncEvent    `json:"events"`     // Recent events (last ~5 minutes)
	Signature string         `json:"signature"`  // Cryptographic signature for authenticity
}

// createZine creates a zine (batch of recent events) to share with neighbors
// Returns nil if no events to share or if SyncLedger unavailable
func (network *Network) createZine() *Zine {
	if network.local.SyncLedger == nil {
		return nil
	}

	// Get events from last 5 minutes
	cutoff := time.Now().Add(-5 * time.Minute).UnixNano()
	allEvents := network.local.SyncLedger.GetAllEvents()

	var recentEvents []SyncEvent
	for _, e := range allEvents {
		if e.Timestamp >= cutoff {
			recentEvents = append(recentEvents, e)
		}
	}

	if len(recentEvents) == 0 {
		return nil // Nothing to share
	}

	zine := &Zine{
		From:      network.meName(),
		CreatedAt: time.Now().Unix(),
		Events:    recentEvents,
	}

	// Sign the zine for authenticity
	sig, err := SignZine(zine, network.local.Keypair)
	if err != nil {
		logrus.Warnf("📰 Failed to sign zine: %v", err)
		return nil
	}
	zine.Signature = sig

	return zine
}

// SignZine computes the signature for a zine
func SignZine(z *Zine, keypair NaraKeypair) (string, error) {
	if len(keypair.PrivateKey) == 0 {
		return "", fmt.Errorf("no private key available")
	}

	// Create signing data (from + timestamp + event IDs)
	hasher := sha256.New()
	hasher.Write([]byte(fmt.Sprintf("%s:%d:", z.From, z.CreatedAt)))
	for _, e := range z.Events {
		hasher.Write([]byte(e.ID))
	}

	signingData := hasher.Sum(nil)
	return keypair.SignBase64(signingData), nil
}

// VerifyZine verifies a zine's signature
func VerifyZine(z *Zine, publicKey ed25519.PublicKey) bool {
	if z.Signature == "" || len(publicKey) == 0 {
		return false
	}

	// Recompute signing data
	hasher := sha256.New()
	hasher.Write([]byte(fmt.Sprintf("%s:%d:", z.From, z.CreatedAt)))
	for _, e := range z.Events {
		hasher.Write([]byte(e.ID))
	}

	signingData := hasher.Sum(nil)

	// Decode signature
	sig, err := base64.StdEncoding.DecodeString(z.Signature)
	if err != nil {
		return false
	}

	return ed25519.Verify(publicKey, signingData, sig)
}
