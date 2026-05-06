---
name: eshu-folder-doc-keeper
description: Use when a Go directory's README.md or doc.go drifts from the code, when a stale-marker line appears in .eshu-doc-state/stale.jsonl, when the user asks to update a package README, regenerate folder docs, document a new package, or after touching code under go/ where the package contract changed. Works the same under Claude Code and Codex. Keeps the README + doc.go pair current with the actual package surface, splits content by audience, and runs the humanizer pass before finalizing.
---

# eshu-folder-doc-keeper

Keep `README.md` and `doc.go` in every `go/` directory aligned with the code.
The two files share a directory but not an audience — write each for who reads
it, not by copy-pasting between them.

## Why both files

`doc.go` is the package contract for godoc consumers. `go doc ./internal/foo`
prints it on its own. It must compile, name real exported identifiers, and
explain invariants callers rely on.

`README.md` is the architectural lens for humans who open the directory in a
file browser or a code review. It carries ownership boundaries, dependency
notes, telemetry the package emits, and operational gotchas — context that
does not belong in package comments.

If the same sentence belongs in both, refer between them. The contract goes in
`doc.go`; the README points readers at it. Drift between the two is a sign the
audience split was forgotten.

## Required README sections

Every `README.md` under `go/` uses these headings in this order. Skip a section
only when it genuinely does not apply (e.g. no telemetry from a pure helper
package), and say so in one line rather than omitting the heading silently.

```markdown
# <Package Title>

## Purpose
One paragraph. What this package owns and why it exists. Concrete nouns, no
inflated significance.

## Ownership boundary
What this package is responsible for, and — when relevant — what it is NOT
responsible for. Reference the ownership table in `CLAUDE.md` when the package
appears there.

## Exported surface
The types, interfaces, and functions other packages depend on. Cross-link to
`doc.go` for the godoc-rendered contract. Do not duplicate the godoc text.

## Dependencies
Internal packages this package imports. Note any port/interface boundaries
(`GraphQuery`, `GraphWrite`, `InstrumentedDB`) so readers know where the
abstractions live.

## Telemetry
Metrics, span names, log scopes this package emits. Use exact names so
operators can grep. If telemetry lives in a parent package, link to it.

## Gotchas / invariants
Anything that surprised someone the first time they touched this package:
ordering, retry rules, transaction scope, idempotency keys, conflict domains.

## Related docs
`docs/docs/...` pages, ADRs, runbooks. Skip when none exist.
```

## Required doc.go shape

```go
// Package <name> <one-sentence summary of what callers get>.
//
// <2-4 sentences explaining the contract: what guarantees the package gives
// callers, what failure modes they must handle, what invariants must hold.
// Reference the spec, ADR, or behavior contract this package implements.>
package <name>
```

The package comment must explain the contract, invariant, failure mode, or
operational reason — placeholder comments that only repeat the identifier are
not acceptable. This rule is in `CLAUDE.md`.

## Voice

If the `humanizer` skill is available (it ships at `~/.claude/skills/humanizer`
for Claude Code and `~/.codex/skills/humanizer` for Codex), invoke it before
finalizing prose. If it is not installed, apply the same rules manually: the
checklist below captures the high-impact ones. Concrete nouns, active verbs,
no inflated significance. The house style is set by
`go/internal/runtime/README.md` and `go/internal/storage/cypher/README.md` —
short, technical, factual.

Avoid:

- "stands as," "serves as," "key role," "underscores," "robust"
- formulaic openings ("In this package, …", "This document describes …")
- em-dash overuse, boldface overuse
- "Let me know if …," "I hope this helps"

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
5. Verify (still inside `go/`):
   - `go vet ./<package>` passes.
   - `go doc ./<package>` prints the new comment.
   - No section duplicates content between README and doc.go.
6. Remove the resolved lines from `.eshu-doc-state/stale.jsonl` (or rotate the
   file to a `.resolved` sibling so you keep history).
7. Stage the changes. Do not commit — the user controls commits.

## Update workflow when invoked manually

Without a marker, ask which directory or domain to update, then run steps
2 through 7 above for it.

## Scaffolding a new package

When creating a `README.md` and `doc.go` for a directory that has neither:

1. Read every `.go` file in the directory (not its subdirectories) to build a
   faithful summary. Do not guess.
2. Determine the actual `package <name>` declaration — `doc.go` must match.
3. Identify exported identifiers, scoped to this directory only:
   `rg --max-depth 1 '^(func|type|var|const) [A-Z]' <dir>`.
4. Identify telemetry call sites, also scoped to this directory:
   `rg --max-depth 1 'telemetry\.|tracer\.Start' <dir>`.
5. Fill the templates from `references/templates.md`.
6. Run the humanizer pass.
7. From `go/`, verify with `go vet ./<package>` and `go doc ./<package>`.

## When to push back

If asked to write a README that would invent facts the code does not support,
stop and ask. The Eshu rule is "wrong graph truth, query truth, or deployment
truth is a product failure" — wrong README truth carries the same risk for
operators reading it at 3 AM. Reduce the claim instead of inflating it.

## Reference

`references/templates.md` holds copy-paste templates and three worked
examples drawn from existing Eshu packages.
