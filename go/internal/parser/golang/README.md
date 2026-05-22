# internal/parser/golang

## Purpose

`internal/parser/golang` owns Go source parsing behind the parent parser
dispatcher. It emits Go payload metadata for declarations, imports, variables,
calls, receiver proof, function values, embedded SQL, package pre-scan
evidence, and conservative dead-code root evidence.

## Ownership boundary

This package owns Go syntax and Go-specific semantic extraction. The parent
`internal/parser` package owns registry dispatch, runtime caching, repository
pre-scan orchestration, SCIP support, and payload routing. This package must
not import the parent dispatcher in a way that creates cycles.

## Exported surface

See `doc.go` for the full contract. Main surfaces include `Parse`, `PreScan`,
`PreScanFileEvidence`, embedded SQL extraction, package interface/direct-method
pre-scan helpers, generic constraint helpers, method declaration keys, and the
shared option type aliases consumed by the parent parser.

## Dependencies

The package depends on tree-sitter Go bindings and parser shared helpers.
Collector, content shaping, facts, reducer, and query packages consume the
payload downstream but are not dependencies here.

## Telemetry

The package emits no metrics or spans directly. Collector snapshotting records
parse duration and parse-stage failures for Go files.

## Gotchas / invariants

- Direct method roots require scoped receiver evidence; do not fall back to
  same-method-name matching.
- Function-value references are reachability evidence only when source text
  proves the value escapes through a bounded call, field, return, callback, or
  interface contract.
- Imported package evidence must stay bounded to qualified package contracts
  and scoped imported-variable receiver types.
- Return-type metadata normalizes pointers, slices, arrays, selectors, and
  generic instantiations to terminal element types.
- Keep branch counting and cyclomatic complexity consistent with the parent
  parser contract.

## Focused tests

```bash
cd go
go test ./internal/parser/golang -run 'Test.*PreScan|Test.*Interface|Test.*EmbeddedSQL|Test.*Receiver|Test.*FunctionValue' -count=1
go test ./internal/parser -run 'TestDefaultEngineParsePathGo|TestDefaultEnginePreScan.*Go|Test.*Go.*DeadCode|Test.*Go.*Call' -count=1
go test ./internal/parser/golang ./internal/parser -count=1
```

Docs-only edits should also pass the package-doc verifier and `git diff --check`.

## Related docs

- `go/internal/parser/README.md`
- `docs/public/languages/feature-matrix.md`
- `docs/public/reference/dead-code-reachability-spec.md`
