# Perl Parser

## Purpose

This package owns the line-oriented Perl parser adapter used by the parent
parser engine. It extracts package declarations, use imports, subroutines,
variables, and simple call evidence.

## Ownership boundary

The package is responsible for Perl source scanning and payload bucket
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

Package names are emitted as class rows using the final `::` segment to
preserve the legacy payload. Function calls are deduplicated by name, matching
the old regex adapter behavior. PreScan sorts names after collecting them from
the parsed function and class buckets.

## Related docs

- docs/plans/2026-05-09-parser-language-layout.md
