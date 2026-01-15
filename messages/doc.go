// Package messages contains all Nara message payload types.
//
// This is the single source of truth for all message payloads exchanged
// in the Nara network. Every message kind has its payload defined here.
//
// # Structure
//
// Each file contains related payload types:
//   - stash.go: Distributed encrypted storage
//   - social.go: Social interactions (teases, trends)
//   - presence.go: Identity announcements, heartbeats
//   - checkpoint.go: Consensus checkpoints
//   - observation.go: Network state observations
//   - gossip.go: Peer-to-peer gossip bundles
//
// # Adding New Messages
//
// When adding a new message type:
//
//  1. Add the payload struct with godoc comments:
//     - What the message does
//     - Flow: who sends to whom
//     - Response: what message comes back (if any)
//     - Transport: MQTT, Mesh, or Local
//
//  2. Add a Validate() method for payload validation
//
//  3. Use ID fields as primary identifiers, names for display only
//
//  4. Document version history in comments
//
// # Example
//
//	// ExamplePayload demonstrates the pattern.
//	//
//	// Kind: example:request
//	// Flow: Alice → Bob
//	// Response: ExampleResponse
//	// Transport: MeshOnly
//	//
//	// Version History:
//	//   v1 (2024-01): Initial version
//	type ExamplePayload struct {
//	    ActorID string `json:"actor_id"`           // Primary identifier
//	    Actor   string `json:"actor,omitempty"`    // Display name only
//	    Data    []byte `json:"data"`
//	}
//
//	func (p *ExamplePayload) Validate() error {
//	    if p.ActorID == "" {
//	        return errors.New("actor_id required")
//	    }
//	    return nil
//	}
package messages
