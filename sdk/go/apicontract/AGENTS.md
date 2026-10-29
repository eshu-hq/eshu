# API Wire-Contract SDK Agent Rules

This directory is a public Go module for Eshu API wire contracts. It must
remain independent from Eshu internals.

## Required Checks

- Read the root `AGENTS.md` and `docs/internal/agent-guide.md` before edits.
- Keep `go.mod` as a standalone module with no require or replace directives.
- Do not import `github.com/eshu-hq/eshu/go/internal/...`.
- Use only the Go standard library outside tests.
- When changing a shape or entry, update `README.md`, `doc.go`,
  `testdata/golden/*.json`, and the core drift tests in the same change.
- Run `go test ./... -count=1` from this directory.
- Run `gofmt` for changed Go files and `git diff --check` from the repo root.

## Contract Rules

- Every JSON field name, tag, and enum value must stay byte-identical to the
  core source type. The golden files are the machine-checked proof.
- Shapes only: no rendering, composition, storage, reducer, graph, queue, or
  supervisor logic. Pure validation and parsing helpers are allowed.
- A missing registry name in `ClientEntries` fails the core drift test; add
  the name to the test list, the table, and the golden together.
- New files need the MIT SPDX header; run
  `bash scripts/add-license-header.sh` before the first commit that holds the
  file.
