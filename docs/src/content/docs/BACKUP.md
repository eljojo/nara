---
title: Backup & Restore
slug: concepts/backup
---

# Backup & Restore

The `nara-backup` tool allows you to backup and restore events from the Nara network. This is useful for preserving network history, migrating between instances, or recovering from data loss.

## Overview

- **`dump-events`** - Connects to the mesh network, fetches all events from all peers, deduplicates them, and outputs to stdout
- **`restore-events`** - Restores events from stdin to a specific nara instance using soul-based authentication

Events are stored in **JSON Lines** format (JSONL) - one JSON object per line. This format makes it easy to append new events without rewriting the entire file.

## Installation

Build the backup tool:

```bash
make build-backup
```

This creates `bin/nara-backup`.

## Dump Events

The `dump-events` command connects to the Headscale mesh, discovers all peers, fetches their events, deduplicates by event ID, sorts by timestamp, and outputs as JSONL.

### Basic Usage

```bash
# Dump all events to a file
nara-backup dump-events > backup.jsonl

# Show progress while dumping
nara-backup dump-events -verbose > backup.jsonl
```

### How It Works

1. **Connects to mesh** - Uses the same Headscale credentials as the main nara binary
2. **Discovers peers** - Uses tsnet Status API to find all naras on the network
3. **Fetches events** - POSTs to `/events/sync` on each peer via mesh HTTP
4. **Deduplicates** - Events with the same ID are merged (only one copy kept)
5. **Sorts** - Events are sorted by timestamp (oldest first)
6. **Outputs JSONL** - One JSON object per line for easy appending

### Options

```
-verbose     Show progress on stderr (default: false)
-timeout     Operation timeout (default: 5m)
-help        Show usage information
```

### Appending Events

Because the output format is JSON Lines, you can easily append more events later:

```bash
# Initial backup
nara-backup dump-events > backup.jsonl

# Later, append more events
nara-backup dump-events >> backup.jsonl
```

When you restore, duplicates are automatically handled - events with the same ID won't be imported twice.

## Restore Events

The `restore-events` command reads events from stdin and imports them into a target nara instance. Authentication is done using your nara's soul - only the owner can restore events.

### Basic Usage

```bash
# Restore events to a nara instance
nara-backup restore-events \
  -nara-url https://my-nara.example.com \
  -soul BZbvJDjG3hkhsb9y8e4nYy3DPmPFUQ5DKLHe6oqH5sbe \
  < backup.jsonl
```

### Authentication

Restore uses **soul-based authentication**:

1. You provide your nara's soul via the `-soul` flag
2. The tool derives an Ed25519 keypair from the soul and signs the request
3. Signs: `sign(sha256(timestamp:event_ids))`
4. The receiving nara:
   - Verifies the timestamp is fresh (< 5 minutes old)
   - Verifies the signature using its own keypair (derived from its soul)
   - Since both have the same soul, only the owner can create a valid signature
5. Only if all checks pass, events are imported

This ensures **only the nara owner** can restore events to their instance.

### Options

```
-nara-url    Target nara URL (required, e.g. https://my-nara.com)
-soul        Soul string for authentication (required)
-verbose     Show progress on stderr (default: false)
-timeout     Operation timeout (default: 5m)
-help        Show usage information
```

### Deduplication

The restore process automatically handles duplicate events:

- Events with the same ID are deduplicated
- The response shows how many events were imported vs. duplicates
- Safe to restore the same backup multiple times

Example response:
```
✅ Successfully imported events to https://my-nara.com
   Imported: 1234 events
   Duplicates: 567 events
```

## JSON Lines Format

Events are stored one per line as JSON objects:

```jsonl
{"id":"a1b2c3","ts":1704067200000000000,"svc":"observation","emitter":"alice",...}
{"id":"d4e5f6","ts":1704067201000000000,"svc":"social","emitter":"bob",...}
{"id":"g7h8i9","ts":1704067202000000000,"svc":"ping","emitter":"carol",...}
```

**Benefits:**
- Append-friendly (use `>>` to add more events)
- Stream-processable (don't need to load entire file into memory)
- Works with standard tools (`jq`, `grep`, `wc -l`, etc.)

**Examples:**

```bash
# Count events
wc -l backup.jsonl

# Filter by service type
grep '"svc":"observation"' backup.jsonl

# Pretty-print first event
head -1 backup.jsonl | jq .

# Find events from a specific nara
grep '"emitter":"alice"' backup.jsonl
```

## Typical Workflows

### Full Network Backup

```bash
# Periodic backup (e.g., in a cron job)
nara-backup dump-events > "backup-$(date +%Y%m%d).jsonl"
```

### Incremental Append

```bash
# Daily incremental backup
nara-backup dump-events >> backup-incremental.jsonl
```

### Restore After Fresh Install

```bash
# 1. Install nara with your soul
./bin/nara -soul <your-soul> -http-addr :8080

# 2. In another terminal, restore events
nara-backup restore-events \
  -nara-url http://localhost:8080 \
  -soul <your-soul> \
  < backup.jsonl
```

### Migrate to New Instance

```bash
# 1. Dump from old instance's network perspective
nara-backup dump-events > migration.jsonl

# 2. Start new instance with same soul
./bin/nara -soul <your-soul> -http-addr :8080

# 3. Restore events
nara-backup restore-events \
  -nara-url http://localhost:8080 \
  -soul <your-soul> \
  < migration.jsonl
```

## Security Considerations

- **Soul protection**: Your soul is your private key. Never share it publicly.
- **Backup encryption**: The backup file contains plaintext events. Encrypt it if storing externally:
  ```bash
  nara-backup dump-events | gpg -c > backup.jsonl.gpg
  ```
- **Timestamp validation**: Restore requests must have fresh timestamps (< 5 minutes). This prevents replay attacks.
- **Owner-only restore**: Only someone with the nara's soul can create a valid signature. The soul itself is never transmitted.

## Troubleshooting

### "Failed to connect to mesh"

- Ensure Headscale is accessible
- Check that the default credentials are correct (they're shared in the main nara binary)
- Verify network connectivity

### "No peers discovered on mesh"

- Confirm other naras are online and connected to the mesh
- Wait a few minutes for mesh status to propagate
- Try again with `-verbose` to see more details

### "Authentication failed: soul does not match"

- Verify you're using the correct soul for the target nara
- The soul must be an exact match (case-sensitive, 44 characters)
- Get your nara's soul from the startup logs or API

### "Timestamp too old or in future"

- Check your system clock is synchronized
- The restore request must be made within 5 minutes of creation
- If restoring from a script, generate the request just before sending

## Implementation Details

### Mesh Connection

The backup tool uses an **ephemeral identity** to connect to the mesh:
- Generates a random soul for this session only
- Registers with Headscale as `nara-backup-<timestamp>`
- Uses same credentials as main nara binary (stored in `credentials.go`)
- Mesh authentication via Ed25519 signatures

### Event Deduplication

Events are deduplicated by their ID field:
- ID = first 16 bytes of SHA256(`timestamp + service + payload`)
- Two events with the same ID are considered identical
- Map-based deduplication in memory during dump
- MergeEvents() handles deduplication during restore

### Soul-Based Authentication

The restore authentication flow:
1. Client: derives keypair from soul, signs request with private key
2. Server: verifies timestamp freshness (replay protection)
3. Server: verifies signature using its own public key (derived from its soul)
4. Server: imports events if signature is valid

Since both client and server derive the same keypair from the same soul, only the owner can create a valid signature. The soul itself is never transmitted over the network.

This is implemented as a reusable middleware in `http_import.go` for use by other endpoints in the future.

## Future Extensions

Planned features for future versions:

- **Stash backup**: `dump-stash` and `restore-stash` commands for encrypted storage
- **Selective restore**: Filter by service type, time range, or specific naras
- **Compression**: Optional gzip compression for large backups
- **Progress tracking**: More detailed progress reporting for large dumps
- **Parallel fetching**: Concurrent requests to speed up dumps

## See Also

- [Event System](/concepts/events) - Understanding event structure
- [Sync Protocol](/concepts/sync) - How events propagate through the network
- [Identity](/concepts/identity) - Soul-based cryptographic identity
