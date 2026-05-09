# Haskell Parser

## Purpose

This package owns the line-oriented Haskell parser adapter used by the parent
parser engine. It extracts module declarations, imports, data and class names,
top-level functions, and simple local variables from where blocks.

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
variables. PreScan sorts names after collecting them from the parsed function,
class, and module buckets.

## Related docs

- docs/plans/2026-05-09-parser-language-layout.md
