# Ruby Parser

## Purpose

This package owns the line-oriented Ruby parser adapter used by the parent
parser engine. It extracts module and class declarations, method signatures,
require/load imports, module inclusions, local and instance variables, and
bounded method-call evidence.

## Ownership boundary

The package is responsible for Ruby source scanning and payload bucket
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

Ruby block tracking is line-oriented and uses `end` to pop the current module,
class, singleton-class, or method context. Method-call rows are deduplicated by
full name and source line so repeated calls on different lines remain visible.
PreScan sorts names after collecting them from the parsed function, class, and
module buckets.

Constants are represented in the legacy `variables` bucket with class or module
context instead of a separate constants bucket. Predicate, bang, and writer
method suffixes are preserved for qualified calls. Rails-style DSL chains are
captured as bounded call evidence only; this package does not mark Rails, Rake,
or other framework roots by itself. `def self.name` and `class << self` are
covered, while `def ClassName.name` is not part of the current contract.

## Related docs

- docs/plans/2026-05-09-parser-language-layout.md
