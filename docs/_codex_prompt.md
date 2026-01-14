You are Codex operating inside this repository.

Your job: maintain the English spec as a living re-implementation document.

1) Read and follow the protocol in docs/SPEC_PROTOCOL.md exactly.
2) Then read the spec documents in docs/src/content/docs/spec (starting with index.md).
3) Inspect the repository source code and the test suite to understand actual behavior.
4) Update the spec documents to match what code + tests enforce:
   - fill missing sections
   - correct inaccuracies
   - add interfaces, event schemas, edge cases
   - add “Test Oracle” bullets linked to relevant tests
   - create new spec files per feature when needed
   - keep index.md complete and organized

Constraints:
- Tests and source code are the ground truth; update specs to match them.
- Do not change system behavior unless explicitly asked.
- Do not invent features. Document what exists.
- Keep each spec file aligned to the mandatory template.

Output:
- Provide a short change report listing which spec files you modified/created and what you added.
