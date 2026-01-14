package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/eljojo/nara"
	"github.com/sirupsen/logrus"
)

func dumpEventsCmd(args []string) {
	fs := flag.NewFlagSet("dump-events", flag.ExitOnError)
	name := fs.String("name", "", "nara name (required)")
	soulStr := fs.String("soul", "", "soul string (required)")
	verbose := fs.Bool("verbose", false, "show progress on stderr")
	timeout := fs.Duration("timeout", 3*time.Minute, "timeout per peer (not total)")
	workers := fs.Int("workers", 5, "number of parallel workers for fetching")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: nara-backup dump-events -name <name> -soul <soul> [options]

Connects to the Tailscale mesh as your nara, discovers all peers, and fetches events from each.
Uses mesh authentication (signing with your keypair) to fetch from each peer.
Outputs events in JSON Lines format (one JSON object per line) to stdout.

Uses a worker pool (-workers flag) to query multiple peers in parallel for faster backup.
Each peer gets its own timeout window (-timeout flag). Slow/offline peers won't starve later peers.

Options:
`)
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Examples:
  # Dump events to file
  nara-backup dump-events -name alice -soul <your-soul> > backup.jsonl

  # Dump with progress info
  nara-backup dump-events -name alice -soul <your-soul> -verbose > backup.jsonl

  # Increase per-peer timeout for slow connections
  nara-backup dump-events -name alice -soul <your-soul> -timeout 5m > backup.jsonl

  # Use more workers for faster parallel fetching
  nara-backup dump-events -name alice -soul <your-soul> -workers 10 > backup.jsonl

  # Append more events later
  nara-backup dump-events -name alice -soul <your-soul> >> backup.jsonl
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
	soul, err := nara.ParseSoul(*soulStr)
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

	// Collect results and deduplicate
	allEvents := make(map[string]nara.SyncEvent)
	completed := 0

	for result := range results {
		completed++
		if result.err != nil {
			logrus.Warnf("⚠️  Failed to fetch from %s: %v", result.peer.Name, result.err)
			continue
		}

		before := len(allEvents)
		for _, event := range result.events {
			if event.ID != "" {
				allEvents[event.ID] = event
			}
		}
		after := len(allEvents)
		newEvents := after - before

		logrus.Infof("✅ [%d/%d] Got %d events from %s (%d new, %d total unique)",
			completed, len(peers), len(result.events), result.peer.Name, newEvents, after)
	}

	if len(allEvents) == 0 {
		logrus.Info("📭 No events collected")
		return
	}

	logrus.Infof("📦 Total unique events collected: %d", len(allEvents))

	// Convert to slice and sort by timestamp
	logrus.Info("🔄 Sorting events by timestamp...")
	events := make([]nara.SyncEvent, 0, len(allEvents))
	for _, e := range allEvents {
		events = append(events, e)
	}
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp < events[j].Timestamp
	})

	// Output as JSON Lines (one JSON object per line)
	logrus.Info("💾 Writing events to stdout...")
	encoder := json.NewEncoder(os.Stdout)
	for _, event := range events {
		if err := encoder.Encode(event); err != nil {
			logrus.Fatalf("❌ Failed to encode event: %v", err)
		}
	}

	logrus.Info("✅ Dump complete")
}
