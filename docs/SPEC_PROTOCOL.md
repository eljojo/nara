# Nara Spec Maintenance Protocol (Living English Spec)

This repository treats the product spec as a **living set of English documents** that must stay in sync with the code and tests.

Codex (or any AI coding agent) is responsible for maintaining the spec in:
`docs/src/content/docs/spec`

The spec is *not* marketing docs. It is a **re-implementation grade spec**:
another engineer or AI should be able to rebuild Nara using only the spec + this protocol.

---

## Repository Contract

### Ground truth order (highest wins)
When resolving ambiguity or contradictions, use this order:

1. **Tests** (most authoritative)
2. **Source code behavior** (actual implementation)
3. **Spec documents** (must be updated to match #1 and #2)
4. **README / website copy / blog-like text** (lowest authority)

If the spec contradicts code/tests, **update the spec** (and optionally suggest test improvements).
Do **not** silently change code to match spec unless explicitly instructed.

### Non-goals
- Do not rewrite the system into a different architecture.
- Do not introduce new features or semantics “because it’s nicer”.
- Do not “correct” intentional weirdness. If code is weird, document the weirdness.
- Do not delete history or large refactors unless explicitly asked.

---

## Spec Folder Layout

All spec lives in: `docs/src/content/docs/spec`

### File structure
- `index.md` is the top-level overview and table of contents.
- One feature per file, e.g.:
  - `identity.md`
  - `events.md`
  - `sync.md`
  - `plaza-mqtt.md`
  - `mesh-http.md`
  - `zines.md`
  - `stash.md`
  - `observations.md`
  - `checkpoints.md`
  - `world-postcards.md`
  - `social.md`
  - `coordinates.md`
  - `web-ui.md`
  - etc.

Codex may add new files when needed, but should keep each file:
- focused
- reasonably sized
- linked from `index.md`

---

## Mandatory Document Template (for every feature file)

Every spec file MUST follow this structure (in this order), even if some sections are short:

1. **Purpose**
   - What this feature is for (plain English).
   - What it enables in the network myth.

2. **Conceptual Model**
   - Entities involved (names, souls, events, peers, etc.)
   - Key invariants as bullet points.

3. **External Behavior**
   - What other parts of the system can assume.
   - What the network observes.
   - Any user-facing behavior (CLI/API/UI) if applicable.

4. **Interfaces**
   - List of relevant:
     - CLI flags / env vars
     - HTTP endpoints
     - MQTT topics / message shapes
     - Public structs / packages (names only; no massive code dumps)
   - For each interface: describe inputs, outputs, and error behavior.

5. **Event Types & Schemas (if relevant)**
   - Enumerate event types this feature emits/consumes.
   - Define required fields and signing expectations.

6. **Algorithms**
   - Step-by-step descriptions of derivations, gossip selection, consensus, etc.
   - Include edge cases and “intended weirdness”.

7. **Failure Modes**
   - What happens on partial connectivity, restarts, missing peers, clock skew, etc.
   - Expected degradation behavior.

8. **Security / Trust Model (if relevant)**
   - What is signed, what is encrypted, what is unverifiable hearsay.
   - What “authentic” means for this feature.

9. **Test Oracle**
   - The most important part: a list of claims that can be verified by tests.
   - Prefer statements like:
     - “Given X events, projection Y must produce Z.”
     - “A soul bound to name A must be rejected when claiming name B.”
   - Link to the most relevant test files if easy.

10. **Open Questions / TODO**
   - Only if genuinely unknown or intentionally unfinished.
   - If tests/code settle it, it is not an open question—document it.

---

## Spec Style Rules

### Token-efficient documentation
- **Assume a senior programmer audience** familiar with distributed systems and cryptography.
- **Reference industry standards** and note deviations rather than explaining basics.
  - Example: "Uses Ed25519 signatures (RFC 8032) with custom nonce generation" instead of explaining digital signatures.
- **Be succinct**: Document behaviors unique to Nara, not Go internals or standard patterns.
- **Target Rust-level portability**: Document what matters for re-implementation in any language, not Go-specific details.

### Visual aids
- **Encourage Mermaid diagrams** for complex flows, state machines, and protocol sequences.
- Diagrams should complement text, not duplicate it.
- Keep diagrams focused on one concept each.

### English requirements
- Use plain English with technical precision.
- Be specific and concrete.
- Avoid hand-wavy phrases like "should probably".
- Prefer "MUST / MUST NOT / SHOULD / MAY" language for invariants and guarantees.

### Keep it re-implementable
- Include enough detail to rebuild behavior without reading code.
- Focus on protocols, wire formats, and observable behavior.
- Avoid copying large code blocks; instead describe behavior and list names.
- Document deviations from standard implementations.

### Don't lie
- If behavior is unclear, identify the ambiguity and resolve it by reading tests/code.
- If tests don't cover it, document current behavior and optionally add a TODO.

---

## Codex Workflow (Protocol)

Codex MUST execute the following steps in order.

### Step 0: Establish current spec state
1. Read `docs/src/content/docs/spec/index.md`
2. Enumerate all feature files linked in the index (and any unlinked spec files).

### Step 1: Build a “feature map” from the repository
Scan the repository to locate:
- main entrypoints (binaries, services)
- package/module boundaries
- configuration surface area (flags/env/config)
- protocols (MQTT topics, HTTP endpoints)
- test suite structure

Create an internal list of features and where they are implemented.

### Step 2: Diff spec vs reality
For each feature file:
1. Read the spec file
2. Inspect the relevant code
3. Inspect the relevant tests
4. Identify gaps in the spec:
   - missing interfaces
   - missing invariants
   - wrong claims
   - unspecified edge cases that are enforced by tests
   - mismatched names / concepts

### Step 3: Patch the spec
Update the spec files so they match reality:
- Add missing details
- Correct incorrect statements
- Add “Test Oracle” bullets that reflect what tests enforce
- Ensure the mandatory template sections exist and are ordered correctly

### Step 4: Maintain index.md
- Ensure every spec file is linked from `index.md`
- Ensure the index has a readable taxonomy:
  - Overview
  - Identity
  - Transport
  - Event model
  - Memory + persistence model
  - Social layer
  - UI / APIs
  - Deployment & operations (if applicable)

### Step 5: Report
Produce a concise report in the Codex output containing:
- What files changed
- Why they changed (1–2 sentences each)
- Any remaining ambiguities
- Suggested tests to add (only if it materially improves spec authority)

---

## Scope of Allowed Changes

Codex is allowed to:
- Create new spec files
- Edit existing spec files
- Reorganize the index
- Fix terminology inconsistencies
- Add diagrams in Mermaid (optional, but useful)

Codex is NOT allowed to:
- Change system behavior without explicit instruction
- Delete significant spec content without replacing it
- Remove intentional weirdness
- Add new features disguised as “documentation”

---

## Definition of Done

This protocol run is complete when:

- Every feature in the codebase has a corresponding spec file (or is explicitly documented as out-of-scope).
- Every spec file follows the template.
- Specs are consistent with tests and code.
- `index.md` links everything and is navigable.
- The spec contains enough detail for re-implementation by another agent.

---

## Notes for humans

- Seed specs can be incomplete. Codex’s job is to close the loop.
- The spec is the durable artifact; code can evolve, but the spec must track it.
- When in doubt: document the behavior as it exists, then propose improvements separately.
