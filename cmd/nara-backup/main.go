package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "dump-events":
		dumpEventsCmd(os.Args[2:])
	case "restore-events":
		restoreEventsCmd(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `nara-backup - Network backup and restore tool

Usage:
  nara-backup <command> [options]

Commands:
  dump-events      Dump all events from the network to stdout (JSONL format)
  restore-events   Restore events from stdin to a nara instance

Examples:
  # Dump all events to file
  nara-backup dump-events > backup.jsonl

  # Restore events to a nara instance
  nara-backup restore-events -nara-url https://my-nara.com -soul <soul> < backup.jsonl

  # Append more events later (duplicates are handled automatically)
  nara-backup dump-events >> backup.jsonl

For more information on each command, use:
  nara-backup <command> -help
`)
}
