# Haskell Parser

## Purpose

This package owns the Haskell parser adapter used by the parent parser engine.
A single tree-sitter AST walk extracts module declarations, imports with common
aliases, data/newtype/type and class names, typeclass and instance methods,
top-level functions, and where-block local variables. Function-call evidence is
read lexically from each definition's right-hand-side text. The package also
annotates dead-code root kinds for explicit module exports, `main`, typeclass
methods, and instance methods.

## Ownership boundary

The package is responsible for Haskell source scanning, tree-sitter AST
traversal, and payload bucket population. The parent parser package still owns
registry dispatch, shared runtime parser construction, repo path handling, and
parse telemetry.

## Exported surface

The godoc contract is in doc.go. Current exports are Parse, ParseWithParser,
PreScan, and PreScanWithParser.

## Dependencies

This package imports the Go standard library, internal/parser/shared,
go-tree-sitter, and the Haskell tree-sitter grammar binding. It must not import
the parent internal/parser package.

## Telemetry

This package emits no telemetry. Parse timing remains owned by the parent
parser engine.

## Gotchas / invariants

Symbol extraction (modules, imports, classes, functions, variables) is keyed by
tree-sitter node spans, not a line index. The only line-text reads are function
`source` spans and the bounded call helper, which is the documented evidence
exception. Where-block variable extraction reads the `binds` field of a binding
node, so local bindings stay in the `variables` bucket and do not become
top-level `functions`. The function `source` field covers the defining clauses
and guards but stops before a trailing `where` block, matching the prior
contract. Explicit export parsing reads the module header's export list node;
modules without an export list do not mark every top-level declaration as a
dead-code root.

## Performance and observability evidence

No-Regression Evidence: the migration replaces the per-line regex scan (eight
compiled patterns over every `strings.Split` line, plus a partial tree-sitter
augmentation pass) with one recursive walk of the parse tree the parser already
builds. Each named declaration node is visited once, so the cost is bounded by
AST node count rather than line count times pattern count. No allocation-per-line,
goroutine, channel, lock, queue, or graph-write behavior is added; the package
stays single-threaded under the caller-owned parser. Parser throughput is owned
by the collector snapshot path and is unchanged in shape. Verified by `go test
./internal/parser/haskell -count=1` (byte-parity characterization goldens in
`testdata/characterization` plus behavior tests) and the downstream gates `go
test ./internal/parser/... ./internal/reducer ./internal/query -count=1`.

No-Observability-Change: this package emits no metrics, spans, logs, or status,
so operator-facing signals are identical before and after the migration.

The one documented byte-parity deviation is a regex bug the AST fixes: a
typeclass-method signature whose name and `::` sit on separate lines was missed
by the former single-line `name ::` pattern and is now captured by the
`signature` node. Proven failing-first by
`TestParseCapturesMultiLineTypeSignatureClassMethod`.

## Related docs

- docs/public/languages/support-maturity.md
- docs/public/languages/haskell.md
