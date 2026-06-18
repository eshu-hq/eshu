# Elixir Parser

## Purpose

`internal/parser/elixir` owns Elixir language extraction that can run without
importing the parent `internal/parser` package. It emits the Elixir payload,
pre-scan names, `dead_code_root_kinds`, `exactness_blockers`, modules,
protocols, functions, imports, attributes, variables, bounded call metadata,
and Hex dependency evidence from literal `mix.exs` deps plus `mix.lock`
entries. Tree-sitter supplies syntax-aware function spans, multiline
signatures, decorators, and module context while bounded lexical helpers
preserve existing call, dependency, and metadata contracts. Dynamic `apply(...)`
calls mark the enclosing function even when the dispatch sits on a one-line
`def ..., do:` declaration.

## Ownership boundary

The package owns Elixir parse and pre-scan behavior plus the lexical, scope, and
dead-code root helpers used by those operations. The parent parser still owns
registry dispatch, repository-level pre-scan orchestration, and content metadata
enrichment.

## Exported surface

The godoc contract is in `doc.go`. Current exports are:

- `Parse` extracts modules, protocols, functions, imports, attributes, bounded
  call metadata, parser-backed root kinds, and observed dynamic-dispatch
  blockers from both function bodies and one-line declarations. It also emits
  Hex `config_kind=dependency` rows for literal Mix deps and lockfile entries,
  while git deps remain provenance-only `vcs_dependency` rows.
- `ParseWithParser` performs the same extraction with a caller-owned
  tree-sitter parser. The parent engine uses this path so the shared runtime
  owns grammar caching and parser setup.
- `PreScan` returns deterministic names for import-map pre-scan.
- `PreScanWithParser` returns deterministic names with a caller-owned
  tree-sitter parser.

## Dependencies

This package imports `internal/parser/shared`, `go-tree-sitter`, the Elixir
tree-sitter grammar binding, and the Go standard library. It must not import
the parent parser package. The root parser module replaces
`github.com/tree-sitter/tree-sitter-elixir` with the official
`github.com/elixir-lang/tree-sitter-elixir v0.3.5` repository because that
repository declares the canonical tree-sitter module path.

## Telemetry

This package emits no metrics, spans, or logs. Parser timing remains owned by
the collector snapshot path.

## Gotchas / invariants

Tree-sitter metadata augments the existing bounded line and scope helpers; it
must not drop payload keys or reorder buckets without downstream tests. Keep
helper behavior deterministic because pre-scan output feeds repository import
maps and root metadata feeds query classification. Dead-code roots stay
conservative: Application `start/2` needs Application syntax, and callback
roots validate arity where Elixir defines the callback shape. Helpers should
stay package-local unless another language-owned package has a real caller.

## Related docs

- `docs/public/languages/support-maturity.md`
