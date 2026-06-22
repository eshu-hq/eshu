# Haskell Parser

## Purpose

This package owns the Haskell parser adapter used by the parent parser engine.
Primary symbol extraction is a tree-sitter grammar walk: the module header,
data/newtype/type and data-family declarations, typeclasses, instances,
class-method type signatures, and top-level value bindings all come from grammar
nodes. Two bounded textual-evidence readers remain by design and are not symbol
extraction: a where-block scan records simple local variables (kept out of the
functions bucket), and a lexical token scan over definition right-hand sides
records function-call evidence. Imports use a bounded import-line reader that
resolves common aliases. The adapter annotates dead-code root kinds for explicit
module exports, `main`, typeclass methods, and instance methods.

## Ownership boundary

The package is responsible for Haskell source scanning, tree-sitter syntax
metadata, and payload bucket population. The parent parser package still owns
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

Primary symbols come from the tree-sitter grammar walk in
`tree_sitter_symbols.go`; the bucket builders in `tree_sitter_buckets.go` must
not drop payload keys or reorder buckets without downstream tests. A top-level
binding whose head line already contains `=` stores that single line as
`source`; a guarded or multi-clause binding whose head line has no `=` stores
its full node range and records `is_dependency`, matching the prior payload.
Where-block variable extraction still depends on raw-line indentation. Keep that
check stable so local bindings stay in the `variables` bucket and do not become
top-level `functions`. Explicit export parsing reads the module header export
list; modules without an export list do not mark every top-level declaration as
a dead-code root. PreScan sorts names after collecting them from the parsed
function, class, and module buckets.

## Related docs

- docs/public/languages/support-maturity.md
