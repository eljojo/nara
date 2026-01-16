package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/eljojo/nara"
	"github.com/eljojo/nara/identity"
	"github.com/sirupsen/logrus"
)

func dumpEventsCmd(args []string) {
	fs := flag.NewFlagSet("dump-events", flag.ExitOnError)
	name := fs.String("name", "", "nara name (required)")
	soulStr := fs.String("soul", "", "soul string (required)")
	verbose := fs.Bool("verbose", false, "show progress on stderr")
	timeout := fs.Duration("timeout", 3*time.Minute, "timeout per peer (not total)")
	workers := fs.Int("workers", 5, "number of parallel workers for fetching")
	idsFrom := fs.String("ids-from", "", "skip events with IDs already in this JSONL file (for idempotent backups)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: nara-backup dump-events -name <name> -soul <soul> [options]

Connects to the Tailscale mesh as your nara, discovers all peers, and fetches events from each.
Uses mesh authentication (signing with your keypair) to fetch from each peer.
Outputs events in JSON Lines format (one JSON object per line) to stdout.

Events are streamed to stdout as they arrive (unsorted), deduplicated by ID.
Uses a worker pool (-workers flag) to query multiple peers in parallel for faster backup.
Each peer gets its own timeout window (-timeout flag). Slow/offline peers won't starve later peers.

Idempotent backups: Use -ids-from to skip events already in an existing file.
This allows you to safely append new events to the same file.

Options:
`)
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Examples:
  # Initial backup
  nara-backup dump-events -name alice -soul <your-soul> > events.jsonl

  # Idempotent backup (append only new events, safe to run repeatedly)
  # IMPORTANT: Use >> (append), NOT > (truncate), when using -ids-from!
  nara-backup dump-events -name alice -soul <your-soul> -ids-from events.jsonl >> events.jsonl

  # Dump with progress info
  nara-backup dump-events -name alice -soul <your-soul> -verbose > events.jsonl

  # Use more workers for faster parallel fetching
  nara-backup dump-events -name alice -soul <your-soul> -workers 10 > events.jsonl

  # Sort events by timestamp after dumping (optional)
  nara-backup dump-events -name alice -soul <your-soul> | \
    jq -s 'sort_by(.ts)[]' > backup-sorted.jsonl
`)
	}

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	if *name == "" || *soulStr == "" {
		fmt.Fprintf(os.Stderr, "Error: both -name and -soul are required\n\n")
		fs.Usage()
		os.Exit(1)
	}

	// Configure logging
	if *verbose {
		logrus.SetLevel(logrus.InfoLevel)
		logrus.SetOutput(os.Stderr) // Log to stderr so stdout is clean for JSON
	} else {
		logrus.SetLevel(logrus.WarnLevel)
		logrus.SetOutput(os.Stderr)
	}

	// Parse soul
	logrus.Info("🔑 Parsing soul...")
	soul, err := identity.ParseSoul(*soulStr)
	if err != nil {
		logrus.Fatalf("❌ Invalid soul: %v", err)
	}

	// Use background context for overall operation (no global timeout)
	ctx := context.Background()

	// Connect to mesh
	mesh, err := NewBackupMesh(ctx, *name, soul)
	if err != nil {
		logrus.Fatalf("❌ Failed to connect to mesh: %v", err)
	}
	defer mesh.Close()

	// Discover peers
	logrus.Info("🔍 Discovering peers...")
	peers, err := mesh.DiscoverPeers(ctx)
	if err != nil {
		logrus.Fatalf("❌ Failed to discover peers: %v", err)
	}

	if len(peers) == 0 {
		logrus.Warn("⚠️  No peers discovered on mesh")
		return
	}

	logrus.Infof("📡 Discovered %d peers (using %d workers)", len(peers), *workers)

	// Load existing event IDs if provided (for idempotent backups)
	seenIDs := make(map[string]struct{})
	if *idsFrom != "" {
		loadedCount, err := loadEventIDsFromFile(*idsFrom, seenIDs)
		if err != nil {
			logrus.Fatalf("❌ Failed to load IDs from %s: %v", *idsFrom, err)
		}
		if loadedCount > 0 {
			logrus.Infof("📋 Loaded %d existing event IDs from %s", loadedCount, *idsFrom)
		}
	}

	// Worker pool setup
	type fetchJob struct {
		peer nara.TsnetPeer
		idx  int
	}

	type fetchResult struct {
		peer   nara.TsnetPeer
		events []nara.SyncEvent
		err    error
		idx    int
	}

	jobs := make(chan fetchJob, len(peers))
	results := make(chan fetchResult, len(peers))

	// Start workers
	var wg sync.WaitGroup
	for w := 0; w < *workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for job := range jobs {
				logrus.Infof("📥 [%d/%d] Worker %d fetching from %s (%s)...",
					job.idx+1, len(peers), workerID, job.peer.Name, job.peer.IP)

				// Create per-peer timeout context
				peerCtx, peerCancel := context.WithTimeout(ctx, *timeout)
				events, err := mesh.FetchEvents(peerCtx, job.peer.IP, job.peer.Name)
				peerCancel()

				results <- fetchResult{
					peer:   job.peer,
					events: events,
					err:    err,
					idx:    job.idx,
				}
			}
		}(w)
	}

	// Send jobs to workers
	go func() {
		for i, peer := range peers {
			jobs <- fetchJob{peer: peer, idx: i}
		}
		close(jobs)
	}()

	// Wait for workers to finish and close results channel
	go func() {
		wg.Wait()
		close(results)
	}()

	// Stream events to stdout as they arrive, deduplicating on the fly
	// seenIDs already initialized above (may have pre-loaded IDs from -ids-from)
	encoder := json.NewEncoder(os.Stdout)
	completed := 0
	totalUnique := 0

	for result := range results {
		completed++
		if result.err != nil {
			logrus.Warnf("⚠️  Failed to fetch from %s: %v", result.peer.Name, result.err)
			continue
		}

		newCount := 0
		for _, event := range result.events {
			if event.ID == "" {
				continue
			}

			// Skip if already seen
			if _, seen := seenIDs[event.ID]; seen {
				continue
			}

			// Mark as seen and output immediately
			seenIDs[event.ID] = struct{}{}
			if err := encoder.Encode(event); err != nil {
				logrus.Fatalf("❌ Failed to encode event: %v", err)
			}
			newCount++
			totalUnique++
		}

		logrus.Infof("✅ [%d/%d] Got %d events from %s (%d new, %d total unique)",
			completed, len(peers), len(result.events), result.peer.Name, newCount, totalUnique)
	}

	if totalUnique == 0 {
		logrus.Info("📭 No events collected")
		return
	}

	logrus.Infof("✅ Dump complete: %d unique events streamed", totalUnique)
}

// loadEventIDsFromFile reads a JSONL file and extracts event IDs into the provided set.
// Returns the number of IDs loaded.
func loadEventIDsFromFile(path string, seenIDs map[string]struct{}) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist yet - this is fine for first run
			return 0, nil
		}
		return 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	count := 0

	// Use a larger buffer for potentially large lines
	const maxCapacity = 1024 * 1024 // 1MB per line
	buf := make([]byte, maxCapacity)
	scanner.Buffer(buf, maxCapacity)

	for scanner.Scan() {
		var event struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			// Skip malformed lines but continue processing
			logrus.Warnf("Skipping malformed line in %s: %v", path, err)
			continue
		}
		if event.ID != "" {
			seenIDs[event.ID] = struct{}{}
			count++
		}
	}

	if err := scanner.Err(); err != nil {
		return count, fmt.Errorf("error reading file: %w", err)
	}

	return count, nil
}
