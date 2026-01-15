You are Codex operating inside the Nara repository.

Your job: Sync the spec documentation with recent code changes from git diff or a specific commit.

## Task

Analyze either:
1. Current uncommitted changes (`git diff`)
2. A specific commit (`git show <commit-hash>`)

Then update the relevant spec documents to reflect these changes.

## Workflow

### Step 1: Analyze the changes
- Run `git diff` (for uncommitted) or `git show <hash>` (for specific commit)
- Identify which features/domains are affected
- Note any new behaviors, modified protocols, or changed invariants

### Step 2: Map changes to spec files
- Determine which spec files in `docs/src/content/docs/spec/` need updates
- Check if new features require new spec files

### Step 3: Update the spec
For each affected spec file:
- Add/modify sections to document new behavior
- Update interfaces, event schemas, algorithms as needed
- Ensure Test Oracle sections reflect any new test patterns
- Add Mermaid diagrams for complex flow changes

### Step 4: Handle breaking changes
If changes break existing behavior:
- Document both old and new behavior
- Note migration considerations if applicable
- Update failure modes and edge cases

## Documentation principles
- **Be concise**: Assume senior programmer audience
- **Focus on behavior changes**: Not implementation details
- **Reference standards**: Note deviations from industry norms
- **Stay language-agnostic**: Document for re-implementation in any language

## Special considerations for different change types

### New features
- Create new spec file if feature is substantial
- Update index.md with new entry
- Follow mandatory template from SPEC_PROTOCOL.md

### Bug fixes
- Update Test Oracle sections with fixed invariants
- Document previously undefined behavior now defined

### Refactors
- Only update if observable behavior changed
- Focus on protocol/interface changes, not internals

### Performance optimizations
- Document only if they change guarantees or limits
- Note new capacity expectations if relevant

## Output
Provide a summary with:
- Which files were analyzed from git
- Which spec files were updated/created
- Key behavioral changes documented
- Any remaining ambiguities found

Remember: The spec tracks observable behavior and protocols, not Go internals. Be smart about token usage.