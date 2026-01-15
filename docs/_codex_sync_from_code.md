You are Codex syncing Nara's spec with Go implementation.

**Job**: Scan Go code systematically and update specs to match actual behavior.

**Process**:
1. Read docs/SPEC_PROTOCOL.md for guidelines
2. Systematically scan Go files by domain prefix:
   - `identity_*.go` → identity.md
   - `sync_*.go` → sync-protocol.md
   - `presence_*.go` → presence.md
   - `gossip_*.go` → zines.md
   - `stash_*.go` → stash.md
   - `social_*.go` → social-events.md
   - `world_*.go` → world-postcards.md
   - `checkpoint_*.go` → checkpoints.md
   - `neighbourhood_*.go` → observations.md
   - `transport_*.go` → plaza-mqtt.md, mesh-http.md
   - `http_*.go` → http-api.md, web-ui.md
   - `boot_*.go` → boot-sequence.md
   - `network*.go` → memory-model.md, configuration.md
3. For each domain:
   - Read the Go implementation
   - Read corresponding tests (*_test.go)
   - Identify behaviors, protocols, invariants
   - Update/create spec files to match
4. Focus on:
   - Wire protocols and message formats
   - Public interfaces and their contracts
   - Event types and schemas
   - Consensus algorithms and derivations
   - Error behaviors and failure modes

**What to document**:
- Observable behavior and protocols
- Deviations from industry standards
- Unique Nara patterns and invariants
- Test-enforced guarantees

**What NOT to document**:
- Go-specific implementation details
- Standard library usage
- Internal helper functions
- Obvious distributed systems patterns

**Deliverable**:
- List of Go files examined
- Spec files updated/created
- Key behaviors documented

Work domain by domain to stay within context limits.