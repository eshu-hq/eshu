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
parse telemetry. Haskell production files stay independent of the parent
package; external `haskell_test` files may import it only for black-box tests of
the public `Engine`.

## Exported surface

The godoc contract is in [doc.go](doc.go). Current exports are Parse,
ParseWithParser, PreScan, and PreScanWithParser.

## Dependencies

This package imports the Go standard library, `go/internal/parser/shared`,
go-tree-sitter, and the Haskell tree-sitter grammar binding. It must not import
the parent `go/internal/parser` package from production or same-package tests.
The external Engine regression imports the parent only from `haskell_test`.

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

`engine_haskell_test.go` uses the external `haskell_test` package. It may import
the parent parser because Go compiles it only for tests. Keep that exception
limited to black-box coverage of `Engine.ParsePath`.

## Related docs

- docs/public/languages/support-maturity.md
- docs/public/languages/haskell.md
