# Package documentation workflows

Paths in backticks are relative to the repository root unless noted.

## Update workflow when invoked from a stale marker

State lives at `.eshu-doc-state/stale.jsonl` so both Claude Code and Codex see
the same drift signal. Two paths feed it:

- **Claude Code:** the PostToolUse hook at `.claude/hooks/eshu-doc-staleness.sh`
  fires after each `Edit` or `Write` and runs `scripts/check-docs-stale.sh`
  against the changed file.
- **Codex (and any other tool):** the `AGENTS.md` / `CLAUDE.md`
  "Doc-keeper workflow" section instructs the agent to run
  `scripts/check-docs-stale.sh` after Go edits before wrapping up. The same
  script powers an optional git pre-commit hook.

Each JSONL line names the directory, which file is missing or stale, what
changed, and which tool detected it.

When you are invoked because the marker file has new lines:

1. Read `.eshu-doc-state/stale.jsonl` and group entries by directory.
2. For each directory:
   - From the repo root, `cd go` first — `go.mod` lives at `go/`, so all
     `go vet` and `go doc` calls must run from inside `go/`.
   - Run `go doc ./<package-import-path>` (e.g. `./internal/runtime`) to see
     the current public contract.
   - Run `rg --files --max-depth 1 -g '*.go' -g '!*_test.go' -g '!*/doc.go'
     <dir>` to enumerate the source files of *that* package only. The
     `--max-depth 1` flag is critical: without it, `rg` recurses into
     subpackages (for example `internal/reducer/{aws,dsl,tags,tfstate}`)
     and the generated README ends up describing the wrong package surface.
   - Diff the current README/doc.go against the surface. Identify the
     specific sections that no longer match.
3. Rewrite **only** the affected sections. Preserve everything else verbatim
   — humans add value to these files between regenerations.
4. Run the humanizer pass on the rewritten sections.
5. Verify the affected surface (Go commands run inside `go/`):
   - For changes to `doc.go`, `go doc ./<package>` prints the new comment;
     use `go vet ./<package>` when package declarations or code changed.
   - No section duplicates content between README and doc.go.
6. Remove the resolved lines from `.eshu-doc-state/stale.jsonl` (or rotate the
   file to a `.resolved` sibling so you keep history).
7. Continue the authorized task through its agreed completion boundary. Stage
   or commit only within the session’s existing scope and authorization; a
   doc update does not create a new approval requirement or grant publication.

## Update workflow when invoked manually

Without a marker, infer the directory from the request and changed files. Ask
only if that leaves materially different plausible targets. Apply the relevant
steps above to the identified package.

## Scaffolding a new package

When creating package docs for a directory that has neither:

1. Inspect the package declarations, exported surface, and relevant source
   within this directory to substantiate the contract and operational claims.
   Expand reading where behavior remains unclear; do not guess.
2. Determine the actual `package <name>` declaration — `doc.go` must match.
3. Identify exported identifiers, scoped to this directory only:
   `rg --max-depth 1 '^(func|type|var|const) [A-Z]' <dir>`.
4. Identify telemetry call sites, also scoped to this directory:
   `rg --max-depth 1 'telemetry\.|tracer\.Start' <dir>`.
5. Fill the templates from [templates](templates.md).
6. Create a package-local `AGENTS.md` with relevant invariants, common changes,
   failure modes, and ADR boundaries. Route to files when their surface changes;
   avoid a mandatory read stack for every edit.
7. Run the humanizer pass.
8. From `go/`, verify the new package with `go vet ./<package>` and
   `go doc ./<package>`.
9. Run `scripts/verify-package-docs.sh` from the repo root.
