You are Codex optimizing Nara's existing spec documents.

**Job**: Refactor spec files for maximum clarity with minimum tokens.

**Process**:
1. Read all files in docs/src/content/docs/spec/
2. For each spec file, optimize by:
   - **Removing redundancy**: Consolidate repeated explanations
   - **Leveraging standards**: Replace verbose explanations with "standard X with modifications Y"
   - **Pruning obviousness**: Remove explanations of well-known patterns
   - **Tightening language**: Convert passive to active voice, remove filler words
   - **Structuring efficiently**: Use bullet points over paragraphs where appropriate
   - **Adding diagrams**: Replace complex text flows with Mermaid diagrams. A picture is worth 1000 tokens.

**Optimization techniques**:
- Replace: "The system uses public key cryptography with Ed25519 keys to sign messages"
- With: "Ed25519 signatures (RFC 8032)"

- Replace: "When a node receives a message, it validates the signature, checks if it has seen the message before, and if not, adds it to its ledger and forwards it to other nodes"
- With: "Standard gossip protocol with deduplication"

- Replace long protocol descriptions with:
  ```mermaid
  sequenceDiagram
    A->>B: HeyThere
    B->>A: Howdy
    A->>B: Events[]
  ```

**Quality checks**:
- Maintains re-implementation grade detail
- Preserves all unique Nara behaviors
- Follows mandatory template structure
- Test Oracle sections remain actionable
- No loss of critical information

**Token reduction targets**:
- 5-50% reduction typical
- Longer files benefit more
- Keep Test Oracle sections detailed (they're high-value)

**Deliverable**:
- List of files optimized
- Approximate token reduction achieved
- Any clarifications added despite reduction

Process files in order of size (largest first) to maximize impact.
