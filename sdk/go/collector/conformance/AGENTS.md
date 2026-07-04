# Collector Conformance Harness Agent Rules

This directory is part of the public `github.com/eshu-hq/eshu/sdk/go/collector`
Go module. It hosts the out-of-tree-runnable conformance harness.

## Required Checks

- Read the root `AGENTS.md` and `docs/internal/agent-guide.md` before edits.
- Do not import `github.com/eshu-hq/eshu/go/internal/...`. Keep the module
  dependency-free (standard library plus the sibling `collector` package only).
- Do not add file or network I/O to `Run`. It must stay a pure function over a
  decoded `Manifest`, decoded `[]collector.Result`, and caller-supplied
  `PayloadSchemas` bytes. Payload JSON Schemas are passed in by the caller (the
  in-tree host reads `sdk/go/factschema/schema/*.json`, an out-of-tree collector
  reads the pinned `sdk/go/factschema/fixturepack`); this package never reads a
  schema file itself and never imports a JSON-Schema library, so the module stays
  dependency-free.
- The payload schema validator (`payload_schema.go`) MUST fail closed on any
  construct outside its supported subset — an unknown keyword, type, or
  composition (`$ref`/`oneOf`/`anyOf`/`allOf`/`enum`/`pattern`/numeric bounds, a
  nested shape it does not model) returns an error, never a silent pass. When a
  new checked-in schema outgrows the subset, extend the validator to cover the
  construct; do NOT relax the fail-closed guardrail. The in-tree test
  `go/internal/extensionconformance.TestConformanceValidatesEveryFixturePackSchemaConstruct`
  turns the build red when a real schema outgrows the validator.
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
