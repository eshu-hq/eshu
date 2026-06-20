# Collector Conformance Harness Agent Rules

This directory is part of the public `github.com/eshu-hq/eshu/sdk/go/collector`
Go module. It hosts the out-of-tree-runnable conformance harness.

## Required Checks

- Read the root `AGENTS.md` and `docs/internal/agent-guide.md` before edits.
- Do not import `github.com/eshu-hq/eshu/go/internal/...`. Keep the module
  dependency-free (standard library plus the sibling `collector` package only).
- Do not add file or network I/O to `Run`. It must stay a pure function over a
  decoded `Manifest` and decoded `[]collector.Result`.
- Keep `Report`, `Finding`, `Summary`, the `eshu.extension.conformance.v1`
  schema string, and every `FindingCode` byte-compatible with the in-tree host
  wrapper at `go/internal/extensionconformance`. The host re-exports these types
  as aliases; changing a JSON tag or finding code is a wire-contract change.
- Update `README.md`, `doc.go`, the host wrapper, and the scorecard example test
  when the report contract or proof checks change.
- Run `go test ./... -count=1` from `sdk/go/collector`, `gofmt` on changed Go
  files, and `git diff --check` from the repo root.

## Contract Rules

- Manifest proof metadata mirrors the in-tree component manifest validation:
  identity, compatible-core, digest-pinned artifact, versioned and
  confidence-labeled fact kinds, and the source-evidence-only reducer phase.
- The harness fails closed. A new blocking shape needs a `FindingCode` and a
  test in `conformance_test.go`.
- Fixture conformance proves package shape only. It must never claim hosted
  activation, graph truth, or production safety.
