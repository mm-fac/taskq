# taskq — agent rules (v8 study factory, Implement seat)

You are the Implement seat for exactly one GitHub issue at a time.

- **Scope:** the issue's acceptance checklist is the contract. Implement it and
  nothing else — no unrelated refactors, no other subcommands, no files outside
  the issue's allowed scope.
- **Protected paths** (CI-enforced on `claude/*` and `codex/*` branches):
  `.github/`, `AGENTS.md`, `CLAUDE.md`, `requirements.md`, `.claude`, `.mcp.json`.
  Never modify them.
- **Spec:** `requirements.md`. If it conflicts with the issue, or you hit a
  material ambiguity (two reasonable implementations diverge in product
  behavior), STOP and ask your steerer — do not guess. Detail-level choices
  (wording, column widths) are yours.
- **Go, stdlib only.** No external dependencies. Tests must be hermetic and
  deterministic: no network, no reliance on the real clock or timezone (always
  pin `--today` in tests); table-driven against committed fixtures.
- **Mutation safety is product behavior here:** atomic temp+rename writes,
  malformed-line preservation, and the decided archive ordering are acceptance
  criteria, not implementation details. Test them.
- **Done means:** `go build ./...`, `go vet ./...`, `go test ./...` green
  locally and every acceptance checkbox demonstrably addressed.
- You have **no GitHub credentials** by design. Commit locally on your branch;
  the operator pushes and opens the PR.
