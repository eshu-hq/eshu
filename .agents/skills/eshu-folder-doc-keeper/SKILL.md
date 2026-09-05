---
name: eshu-folder-doc-keeper
description: Update Eshu Go package docs when contracts change, docs drift, or stale markers identify affected packages.
---

# Eshu folder doc keeper

Keep the affected package's documentation aligned with the code. Every changed
package under `go/internal` or `go/cmd` needs `README.md`, `doc.go`, and
`AGENTS.md`; the package-doc gate enforces this baseline.

- `doc.go` states the godoc contract: real package name and identifiers,
  guarantees, failure modes, and caller invariants.
- `README.md` explains ownership, dependencies, telemetry, and operational
  context. Point to the contract instead of duplicating it.
- `AGENTS.md` carries scoped contributor instructions. Preserve its harness
  scope and precedence; add only relevant invariants and change guidance.

Use the request, diff, and `.eshu-doc-state/stale.jsonl` to identify packages.
Ask for a directory only when the target cannot be inferred. Inspect enough
package source to support each claim; keep discovery within that directory
(`rg --max-depth 1`) so subpackages do not contaminate its documented surface.
Rewrite only affected sections and preserve human-authored content elsewhere.
Use `eshu-humanizer` for the prose pass. Reduce unsupported claims rather than
inventing facts; ask only when an unresolved decision affects the requested docs.

Read [workflows](references/workflows.md) for stale-marker processing or new
package scaffolding. Read [templates](references/templates.md) when creating
or restructuring package docs; its README headings are the package convention.
Keep a heading with a brief explanation when a section does not apply.

Run Go commands from `go/`, where `go.mod` lives. Check changed `doc.go` with
`go doc ./<package>`; use package vet/build checks when code or declarations
change. Run `scripts/verify-package-docs.sh` from the repo root for affected Go
packages, and its test mirror when changing the verifier. The repository docs
build and promotion gates still apply; this skill does not waive them.
Clear only resolved stale markers after verification. Continue through the
session's authorized completion boundary without adding a separate commit stop.
