# Elixir Parser

## Purpose

`internal/parser/elixir` owns Elixir language extraction that can run without
importing the parent `internal/parser` package. It emits the Elixir payload,
pre-scan names, `dead_code_root_kinds`, `exactness_blockers`, modules,
protocols, functions, imports, attributes, variables, and bounded call
metadata. Dynamic `apply(...)` calls mark the enclosing function even when the
dispatch sits on a one-line `def ..., do:` declaration.

## Ownership boundary

The package owns Elixir parse and pre-scan behavior plus the lexical, scope, and
dead-code root helpers used by those operations. The parent parser still owns
registry dispatch, repository-level pre-scan orchestration, and content metadata
enrichment.

## Exported surface

The godoc contract is in `doc.go`. Current exports are:

- `Parse` extracts modules, protocols, functions, imports, attributes, bounded
  call metadata, parser-backed root kinds, and observed dynamic-dispatch
  blockers from both function bodies and one-line declarations.
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
import maps and root metadata feeds query classification. Helpers should stay
package-local unless another language-owned package has a real caller.

## Related docs

- `docs/plans/2026-05-09-parser-language-layout.md`
