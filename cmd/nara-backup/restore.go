package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/eljojo/nara"
	"github.com/eljojo/nara/identity"
	"github.com/sirupsen/logrus"
)

func restoreEventsCmd(args []string) {
	fs := flag.NewFlagSet("restore-events", flag.ExitOnError)
	naraURL := fs.String("nara-url", "", "target nara URL (required, e.g. https://my-nara.com)")
	soulStr := fs.String("soul", "", "soul string for authentication (required)")
	timeout := fs.Duration("timeout", 5*time.Minute, "operation timeout")
	verbose := fs.Bool("verbose", false, "show progress on stderr")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: nara-backup restore-events -nara-url <url> -soul <soul> [options]

Restores events from stdin (JSON Lines format) to a nara instance.
Requires the soul of the target nara for authentication.

Options:
`)
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Examples:
  # Restore from backup file
  nara-backup restore-events -nara-url https://my-nara.com -soul <soul> < backup.jsonl

  # Restore with progress info
  nara-backup restore-events -nara-url https://my-nara.com -soul <soul> -verbose < backup.jsonl
`)
	}

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	if *naraURL == "" || *soulStr == "" {
		fmt.Fprintf(os.Stderr, "Error: both -nara-url and -soul are required\n\n")
		fs.Usage()
		os.Exit(1)
	}

	// Configure logging
	if *verbose {
		logrus.SetLevel(logrus.InfoLevel)
		logrus.SetOutput(os.Stderr)
	} else {
		logrus.SetLevel(logrus.WarnLevel)
		logrus.SetOutput(os.Stderr)
	}

	// Parse soul to derive keypair for signing
	logrus.Info("🔑 Parsing soul...")
	soul, err := identity.ParseSoul(*soulStr)
	if err != nil {
		logrus.Fatalf("❌ Invalid soul: %v", err)
	}
	keypair := identity.DeriveKeypair(soul)

	// Read events from stdin (JSON Lines format - one JSON object per line)
	logrus.Info("📖 Reading events from stdin...")
	var events []nara.SyncEvent
	scanner := bufio.NewScanner(os.Stdin)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue // Skip empty lines
		}

		var event nara.SyncEvent
		if err := json.Unmarshal(line, &event); err != nil {
			logrus.Fatalf("❌ Failed to parse event on line %d: %v", lineNum, err)
		}
		events = append(events, event)
	}

	if err := scanner.Err(); err != nil {
		logrus.Fatalf("❌ Failed to read stdin: %v", err)
	}

	if len(events) == 0 {
		logrus.Info("📭 No events to restore")
		return
	}

	logrus.Infof("📦 Read %d events from stdin", len(events))

	// POST to target nara
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	// Batch events into chunks of 2000 (server limit)
	const batchSize = 2000
	totalImported := 0
	totalDuplicates := 0
	totalWarnings := 0

	for i := 0; i < len(events); i += batchSize {
		end := i + batchSize
		if end > len(events) {
			end = len(events)
		}
		batch := events[i:end]

		logrus.Infof("📤 Sending batch %d/%d (%d events)...", i/batchSize+1, (len(events)+batchSize-1)/batchSize, len(batch))

		// Create signed import request for this batch
		request := nara.EventImportRequest{
			Events:    batch,
			Timestamp: time.Now().Unix(),
		}

		// Sign the request: sha256(timestamp:event_ids)
		hasher := sha256.New()
		hasher.Write([]byte(fmt.Sprintf("%d:", request.Timestamp)))
		for _, e := range request.Events {
			hasher.Write([]byte(e.ID))
		}
		data := hasher.Sum(nil)
		request.Signature = keypair.SignBase64(data)

		// POST batch
		imported, duplicates, warnings, err := postImportRequest(ctx, *naraURL, request)
		if err != nil {
			logrus.Fatalf("❌ Batch import failed: %v", err)
		}

		totalImported += imported
		totalDuplicates += duplicates
		totalWarnings += warnings

		if warnings > 0 {
			logrus.Infof("   ✅ Batch complete: %d imported, %d duplicates, %d warnings", imported, duplicates, warnings)
		} else {
			logrus.Infof("   ✅ Batch complete: %d imported, %d duplicates", imported, duplicates)
		}
	}

	if totalWarnings > 0 {
		logrus.Warnf("⚠️  All batches complete: %d total imported, %d total duplicates, %d signature warnings", totalImported, totalDuplicates, totalWarnings)
	} else {
		logrus.Infof("✅ All batches complete: %d total imported, %d total duplicates", totalImported, totalDuplicates)
	}
}

func postImportRequest(ctx context.Context, naraURL string, req nara.EventImportRequest) (imported, duplicates, warnings int, err error) {
	url := naraURL + "/api/events/import"

	jsonBody, marshalErr := json.Marshal(req)
	if marshalErr != nil {
		return 0, 0, 0, fmt.Errorf("failed to marshal request: %w", marshalErr)
	}

	httpReq, reqErr := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if reqErr != nil {
		return 0, 0, 0, fmt.Errorf("failed to create request: %w", reqErr)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, doErr := client.Do(httpReq)
	if doErr != nil {
		return 0, 0, 0, fmt.Errorf("request failed: %w", doErr)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return 0, 0, 0, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var importResp nara.EventImportResponse
	if unmarshalErr := json.Unmarshal(body, &importResp); unmarshalErr != nil {
		return 0, 0, 0, fmt.Errorf("failed to parse response: %w", unmarshalErr)
	}

	if !importResp.Success {
		return 0, 0, 0, fmt.Errorf("import failed: %s", importResp.Error)
	}

	return importResp.Imported, importResp.Duplicates, importResp.Warnings, nil
}
