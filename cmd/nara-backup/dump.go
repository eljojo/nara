package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/eljojo/nara"
	"github.com/sirupsen/logrus"
)

func dumpEventsCmd(args []string) {
	fs := flag.NewFlagSet("dump-events", flag.ExitOnError)
	name := fs.String("name", "", "nara name (required)")
	soulStr := fs.String("soul", "", "soul string (required)")
	verbose := fs.Bool("verbose", false, "show progress on stderr")
	timeout := fs.Duration("timeout", 5*time.Minute, "total operation timeout")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: nara-backup dump-events -name <name> -soul <soul> [options]

Connects to the Tailscale mesh as your nara, discovers all peers, and fetches events from each.
Uses mesh authentication (signing with your keypair) to fetch from each peer.
Outputs events in JSON Lines format (one JSON object per line) to stdout.

Options:
`)
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Examples:
  # Dump events to file
  nara-backup dump-events -name alice -soul <your-soul> > backup.jsonl

  # Dump with progress info
  nara-backup dump-events -name alice -soul <your-soul> -verbose > backup.jsonl

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

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

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

	logrus.Infof("📡 Discovered %d peers", len(peers))

	// Fetch events from each peer
	allEvents := make(map[string]nara.SyncEvent) // deduplicate by ID

	for i, peer := range peers {
		logrus.Infof("📥 [%d/%d] Fetching events from %s (%s)...", i+1, len(peers), peer.Name, peer.IP)

		events, err := mesh.FetchEvents(ctx, peer.IP, peer.Name)
		if err != nil {
			logrus.Warnf("⚠️  Failed to fetch from %s: %v", peer.Name, err)
			continue
		}

		before := len(allEvents)
		for _, event := range events {
			if event.ID != "" {
				allEvents[event.ID] = event
			}
		}
		after := len(allEvents)
		newEvents := after - before

		logrus.Infof("✅ Got %d events from %s (%d new, %d total unique)", len(events), peer.Name, newEvents, after)
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
