# Elixir Parser

## Purpose

`internal/parser/elixir` owns Elixir language extraction that can run without
importing the parent `internal/parser` package. It emits the Elixir payload and
pre-scan names for modules, protocols, functions, imports, attributes,
variables, and bounded call metadata.

## Ownership boundary

The package owns Elixir parse and pre-scan behavior plus the lexical and scope
helpers used by those operations. The parent parser still owns registry
dispatch, repository-level pre-scan orchestration, and content metadata
enrichment.

## Exported surface

The godoc contract is in `doc.go`. Current exports are:

- `Parse` extracts modules, protocols, functions, imports, attributes, and
  bounded call metadata.
- `PreScan` returns deterministic names for import-map pre-scan.

## Dependencies

This package imports `internal/parser/shared` and the Go standard library. It
must not import the parent parser package.

## Telemetry

This package emits no metrics, spans, or logs. Parser timing remains owned by
the collector snapshot path.

## Gotchas / invariants

Elixir source is parsed with bounded line and scope helpers, not tree-sitter.
Keep helper behavior deterministic because pre-scan output feeds repository
import maps. Helpers should stay package-local unless another language-owned
package has a real caller.

## Related docs

- `docs/plans/2026-05-09-parser-language-layout.md`
