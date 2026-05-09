# Go Parser Helpers

## Purpose

This package owns Go parser helpers that do not need tree-sitter nodes or parent
parser payload helpers. The first helper extracts embedded SQL table references
from string literals passed to common database/sql and sqlx methods.

## Ownership boundary

The package is responsible for typed Go evidence that can be computed from
source text alone. The parent parser package still owns file I/O, tree-sitter
parsing, payload assembly, Go dead-code roots, import aliases, call metadata,
and bucket sorting.

## Exported surface

The godoc contract is in doc.go. Current exports are EmbeddedSQLQuery and
EmbeddedSQLQueries.

## Dependencies

This package imports only the Go standard library. It must not import the
parent parser package, collector packages, graph storage, or reducer code.

## Telemetry

This package emits no telemetry. Parse timing remains owned by the parent
parser engine.

## Gotchas / invariants

EmbeddedSQLQueries is intentionally conservative. It only emits rows when a SQL
literal is passed to a recognized database/sql or sqlx call and the SQL contains
an obvious table reference.

The helper preserves source line numbers by carrying literal offsets through
function-body extraction. Changing offset math can break downstream evidence
even when the table name is still correct.

## Related docs

- docs/plans/2026-05-09-parser-language-layout.md
