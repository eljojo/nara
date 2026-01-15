You are Codex operating inside the nara repository.

Your job: Create or update the nara styleguide document that captures the cultural, aesthetic, and philosophical essence of the project.

## Task

1. Read docs/src/content/docs/index.mdx for context on nara.
2. Read docs/SPEC_PROTOCOL.md for guidelines on spec writing.
3. Create/update `docs/src/content/docs/spec/styleguide.md`
4. Ensure it's linked from `docs/src/content/docs/spec/index.md`

## What the styleguide should contain

### Core Vibes Section
- The mythology and lore of nara as a living network
- The aesthetic choices (organic metaphors, computer sentience)
- The philosophical underpinnings (ephemeral memory, subjective trust, etc.)

### Terminology Guide
- Canonical terms vs what NOT to use
- Why these terms matter for the project's identity
- Examples: "soul" not "node ID", "gossip" not "message propagation"

### Cultural Do's and Don'ts
- Writing tone for docs, comments, and UI
- How to talk about nara (as experiment, not enterprise software)
- What metaphors to embrace vs avoid

### Design Principles
- Core invariants that define nara's character
- Trade-offs that are features, not bugs (e.g., no persistence)
- The social dynamics as first-class concerns

### Implementation Aesthetics
- How code should "feel" (organic, emergent behaviors)
- Naming conventions that reinforce the culture
- Patterns that express nara's philosophy

## Constraints
- This is NOT technical documentation—it's cultural documentation
- Should be readable by someone who wants to understand nara's "personality" as a project
- Should guide contributors on maintaining consistent vibes
- Be concise but evocative—capture the essence without bloat

## Style Notes
- Write with personality—this doc should embody the nara aesthetic
- Use examples from the actual codebase to illustrate points
- Think of it as the "brand guide" for an experimental art project, not corporate documentation

Output a brief summary of what you added/changed after updating the files.

## Very Important

- nara should always be written lowercase. no exceptions.
- this is not a "code style guide" in the sense of formatting or linting, it's not even aimed at developers. it's a "cultural style guide", aimed at explaining the lore and background of nara, trying to capture its essence as an art project/experiment in machine animism. we want to document its soul and personality, so contributors can maintain the "nara vibe" across docs, code comments, and design.
