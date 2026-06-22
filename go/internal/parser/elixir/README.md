# Elixir Parser

## Purpose

`internal/parser/elixir` owns Elixir language extraction that can run without
importing the parent `internal/parser` package. It emits the Elixir payload,
pre-scan names, `dead_code_root_kinds`, `exactness_blockers`, modules,
protocols, functions, imports, attributes, variables, bounded call metadata,
and Hex dependency evidence from literal `mix.exs` deps plus `mix.lock`
entries. All Elixir source symbols are extracted from the tree-sitter AST. A
single walk of the parse tree produces module, function, import, attribute, and
call rows keyed by node spans, so module membership, end lines, decorators,
multiline signatures, and per-clause context follow the tree rather than a text
line index. Dynamic `apply(...)` calls mark the enclosing function even when the
dispatch sits on a one-line `def ..., do:` declaration. There is no regex or
line-scan extraction of Elixir source symbols; the only manifest parsing left is
`mix.exs`/`mix.lock` Hex dependency evidence, which is a structured-format
manifest, not Elixir language source.

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

## Performance and observability evidence

No-Regression Evidence: the AST migration replaces a full per-line text scan
(`strings.Split` plus six compiled regexps over every line) with a single
recursive walk of the tree-sitter parse tree that the parser already builds for
this file. The walk visits each named node once, so the work is bounded by AST
node count rather than line count times pattern count, and the prior per-line
regex matching is removed. No new allocation-per-line, goroutine, channel,
lock, queue, or graph-write behavior is introduced; the package stays
single-threaded under the caller-owned parser. Parser throughput is owned by the
collector snapshot path and is unchanged in shape.

No-Observability-Change: this package emits no metrics, spans, logs, or status.
The change is parse-internal and adds none, so operator-facing signals are
identical before and after.

No-Regression Evidence: the `decoratorsBefore` scan now skips intervening
`comment` named siblings while walking from a definition back toward its
`@impl`/`@decorator` attributes, restoring the former line-scanner behavior that
carried a pending `@impl` across comment lines to the next function. The scan
still stops at the first non-comment, non-attribute sibling, so its cost stays
bounded by the same preceding-sibling slice it already iterated; no extra parse
pass, allocation-per-line, goroutine, channel, lock, queue, or graph-write is
added, and the package stays single-threaded under the caller-owned parser.
Verified by `go test ./internal/parser -run
TestDefaultEngineParsePathElixirImplDecoratorCarriesAcrossComments` (NornicDB
not involved; parser-only fixture `@impl true` + comment + `def init/1`), which
fails without the comment-skip and passes with it.

No-Observability-Change: the comment-skip fix is parse-internal and adds no
metrics, spans, logs, or status, so operator-facing signals are identical before
and after.

## Gotchas / invariants

Extraction walks the AST once; it must not drop payload keys or reorder buckets
without downstream tests. A function-call row requires a parenthesized argument
list, so dotted field access (`state.items`) and control-flow special forms
(`case`, `for`) are not calls. One-line `def ..., do: body` definitions do not
emit calls from the body but still flag dynamic dispatch. Keep helper behavior
deterministic because pre-scan output feeds repository import maps and root
metadata feeds query classification. Dead-code roots stay conservative:
Application `start/2` needs Application syntax, and callback roots validate
arity where Elixir defines the callback shape. Helpers should stay
package-local unless another language-owned package has a real caller.

## Files

- `language.go` parses the source and orchestrates the AST extraction.
- `ast_extract.go` walks definitions and drives module/function scoping.
- `ast_calls.go` emits import, attribute, call, guard-call, and dispatch rows.
- `ast_nodes.go` and `ast_shared.go` hold AST node accessors and classifiers.
- `dead_code_roots.go` computes conservative reachability roots.
- `hex_dependencies.go` parses `mix.exs`/`mix.lock` manifests (not source).
- `helpers.go` holds the alias-expansion and argument-splitting text helpers.

## Related docs

- `docs/public/languages/support-maturity.md`
