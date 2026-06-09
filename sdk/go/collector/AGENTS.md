# Collector SDK Agent Rules

This directory is a public Go module for out-of-tree collector extension
contracts. It must remain independent from Eshu internals.

## Required Checks

- Read the root `AGENTS.md` and `docs/internal/agent-guide.md` before edits.
- Keep `go.mod` as a standalone module.
- Do not import `github.com/eshu-hq/eshu/go/internal/...`.
- Update `README.md`, `doc.go`, JSON Schema, and golden fixtures when changing
  public wire contracts.
- Run `go test ./... -count=1` from this directory.
- Run `gofmt` for changed Go files and `git diff --check` from the repo root.

## Contract Rules

- Extensions emit source facts only; reducers and core hosts own graph,
  workflow, queue, and query truth.
- New result states, source confidence values, or schema fields are wire
  protocol changes and require fixture and JSON Schema updates.
- Validators must fail closed before a host commit accepts unsafe, undeclared,
  duplicate-conflicting, or credential-bearing records.
