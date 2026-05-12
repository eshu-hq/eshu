# Haskell Parser

## Purpose

This package owns the line-oriented Haskell parser adapter used by the parent
parser engine. It extracts module declarations, imports with common aliases,
data and class names, top-level functions, bounded function-call evidence from
definition bodies and continuation lines, and simple local variables from where
blocks. It also annotates dead-code root kinds for explicit module exports,
`main`, typeclass methods, and instance methods.

## Ownership boundary

The package is responsible for Haskell source scanning and payload bucket
population. The parent parser package still owns registry dispatch, engine
orchestration, repo path handling, and parse telemetry.

## Exported surface

The godoc contract is in doc.go. Current exports are Parse and PreScan.

## Dependencies

This package imports the Go standard library and internal/parser/shared. It
must not import the parent internal/parser package.

## Telemetry

This package emits no telemetry. Parse timing remains owned by the parent
parser engine.

## Gotchas / invariants

Where-block variable extraction depends on raw-line indentation. Keep that
check stable so top-level declarations are not misclassified as local
variables. Explicit export parsing is intentionally bounded to the module
header; modules without an export list do not mark every top-level declaration
as a dead-code root. Indented keyword-led bindings such as `let name = ...`
stay inside the current function context, so call evidence on the right-hand
side is kept without creating a fake `let` function. PreScan sorts names after
collecting them from the parsed function, class, and module buckets.

## Related docs

- docs/plans/2026-05-09-parser-language-layout.md
