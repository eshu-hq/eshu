# Haskell Parser

## Purpose

This package owns the Haskell parser adapter used by the parent parser engine.
Tree-sitter supplies syntax-aware function spans and names, while bounded
lexical helpers preserve existing module declarations, imports with common
aliases, data and class names, top-level functions, function-call evidence from
definition bodies and continuation lines, and simple local variables from where
blocks without promoting those local bindings to top-level functions. It also
annotates dead-code root kinds for explicit module exports, `main`, typeclass
methods, and instance methods.

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

Tree-sitter metadata augments the existing bounded line and scope helpers; it
must not drop payload keys or reorder buckets without downstream tests.
Where-block variable extraction depends on raw-line indentation. Keep that
check stable so local bindings stay in the `variables` bucket and do not become
top-level `functions`. Explicit export parsing is intentionally bounded to the
module header; modules without an export list do not mark every top-level
declaration as a dead-code root. Indented keyword-led bindings such as
`let name = ...` stay inside the current function context, so call evidence on
the right-hand side is kept without creating a fake `let` function. PreScan
sorts names after collecting them from the parsed function, class, and module
buckets.

## Related docs

- docs/public/languages/support-maturity.md
