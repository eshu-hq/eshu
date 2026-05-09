# PHP Parser

## Purpose

This package owns the line-oriented PHP parser adapter used by the parent parser
engine. It extracts namespace metadata, imports, classes, interfaces, traits,
functions, variables, call rows, receiver inference, anonymous classes, and
trait-use adaptation evidence.

## Ownership boundary

The package is responsible for PHP source scanning and payload bucket
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

PHP scope tracking is line-oriented and uses brace depth to pop class, trait,
interface, and function contexts. Call rows are deduplicated by full name and
source line so repeated calls on different lines remain visible. PreScan sorts
names after collecting them from the parsed function, class, trait, and
interface buckets.

## Related docs

- docs/plans/2026-05-09-parser-language-layout.md
