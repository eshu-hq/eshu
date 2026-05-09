# Swift Parser

## Purpose

`internal/parser/swift` owns Swift language extraction that can run without
importing the parent `internal/parser` package. It emits the Swift payload and
pre-scan names for imports, nominal types, functions, variables, and bounded
call metadata.

## Ownership boundary

The package owns Swift parse and pre-scan behavior plus the line-based helper
logic used by those operations. The parent parser still owns registry dispatch,
repository-level pre-scan orchestration, and content metadata enrichment.

## Exported surface

The godoc contract is in `doc.go`. Current exports are:

- `Parse` extracts Swift imports, types, functions, variables, and calls.
- `PreScan` returns deterministic names for import-map pre-scan.

## Dependencies

This package imports `internal/parser/shared` and the Go standard library. It
must not import the parent parser package.

## Telemetry

This package emits no metrics, spans, or logs. Parser timing remains owned by
the collector snapshot path.

## Gotchas / invariants

Swift source is parsed with bounded regex helpers, not tree-sitter. Keep helper
output deterministic because pre-scan output feeds repository import maps.
Helpers should stay package-local unless another language-owned package has a
real caller.

## Related docs

- `docs/plans/2026-05-09-parser-language-layout.md`
