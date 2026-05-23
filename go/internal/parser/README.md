# internal/parser

## Purpose

`internal/parser` owns parser dispatch: registry lookup, tree-sitter runtime
caching, native language adapter routing, repository pre-scan orchestration,
Go package semantic pre-scan routing, template/raw text handling, and optional
SCIP index parsing.

## Ownership boundary

This package owns shared parser contracts. Language subpackages own syntax and
language-specific pre-scan behavior. Collector code owns snapshot timing and
fact streaming; content shaping owns payload-to-entity conversion; projector
and reducer packages consume facts downstream.

Parser output is fact input. Entity keys, relationship keys, metadata keys, or
dead-code root kinds must move with fixtures, content shaping, fact contracts,
and downstream docs.

## Exported surface

See `doc.go` and `go doc ./internal/parser` for the contract. Use
`DefaultRegistry().Definitions()` for the current language mapping instead of
copying extension tables into docs.

## Dependencies

The package depends on tree-sitter runtimes, language subpackages, shared
parser helpers, SCIP protobuf parsing, and content metadata helpers. It must
not import collector, projector, reducer, storage, query, or telemetry.

## Telemetry

The parser package emits no metrics or spans directly. Collector snapshotting
records parse duration with `eshu_dp_file_parse_duration_seconds` and logs
parse-stage failures.

## Gotchas / invariants

- Parser output must be deterministic for the same source bytes.
- Repository pre-scan merges concurrent worker results in input order.
- Dead-code roots are conservative reachability evidence, not cleanup verdicts.
- Ambiguous or dynamic language constructs should remain unresolved.
- SCIP supplements native parser facts; callers must not treat it as a
  replacement.
- Keep child language packages behind shared helpers so they do not import the
  parent dispatcher.

## Focused tests

```bash
cd go
go test ./internal/parser -run 'TestDefaultEngine|TestDefaultRegistry|Test.*SCIP|Test.*PreScan|Test.*DeadCode' -count=1
go test ./internal/parser -bench BenchmarkPreScanGoPackageSemanticRoots -run '^$'
go test ./internal/parser/... -count=1
```

Docs-only edits should also pass the package-doc verifier and `git diff --check`.

## Related docs

- `docs/public/languages/feature-matrix.md`
- `docs/public/contributing-language-support.md`
- `docs/public/reference/dead-code-reachability-spec.md`
- `go/internal/parser/golang/README.md`
- `go/internal/collector/README.md`
