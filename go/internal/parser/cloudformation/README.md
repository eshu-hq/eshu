# CloudFormation Parser

## Purpose

`internal/parser/cloudformation` owns CloudFormation and SAM template evidence
shared by JSON and YAML parser adapters. It recognizes bounded template shapes,
evaluates simple condition expressions, and extracts resource, parameter,
output, condition, import, and export rows.

## Ownership boundary

This package is responsible for CloudFormation document classification,
condition evaluation, and payload row construction. JSON and YAML adapters own
file decoding, top-level parser dispatch, and attaching these rows to their
language payloads.

## Exported surface

The godoc contract is in `doc.go`. Current exports are:

- `Result` groups the extracted CloudFormation buckets.
- `IsTemplate` reports whether a decoded document is CloudFormation or SAM.
- `Parse` extracts deterministic bucket rows for one decoded document,
  stamping every entity with the single document-root line.
- `ParseWithPositions` extracts the same buckets but stamps each entity with
  its own real line_number/end_line from a caller-measured `Positions` value
  when the caller has one (issue #5328). `Parse` is `ParseWithPositions`
  called with a zero `Positions`.
- `Positions`, `SectionPositions`, and `EntityPosition` carry the per-entity
  line evidence a caller with a real source-position walk (currently only the
  YAML adapter) passes to `ParseWithPositions`.

## Dependencies

This package imports `internal/parser/shared` for deterministic bucket sorting
and the Go standard library. It must not import the parent parser package,
collector, graph storage, query, or reducer packages.

## Telemetry

This package emits no metrics, spans, or logs. Parser timing remains owned by
the collector snapshot path that calls the parent parser engine.

## Gotchas / invariants

`Parse` preserves the legacy bucket names and row fields consumed by JSON and
YAML callers. Keep the output deterministic: map keys are sorted before rows
are emitted, and row slices are sorted by line number then name.

Condition evaluation is intentionally bounded to literal booleans, parameter
defaults, `Condition`, and simple `Fn::Equals`, `Fn::And`, `Fn::Or`, and
`Fn::Not` forms. Dynamic or unresolved values remain unevaluated rather than
inventing deployment truth.

An Export always inherits its owning Output's `EntityPosition` rather than
getting a separately-walked line, because an Export always nests inside its
Output in the template shape. This package emits no metrics, spans, or logs
(see Telemetry above), so a caller that wants to observe how often its own
position walk degrades to a `SectionPositions.FallbackLine` (or omits
`end_line` entirely) must read that signal back out of its own return value
and record it itself; the YAML adapter does this via a
`cloudformation_position_fallbacks` payload row the collector layer turns
into telemetry.

## Related docs

- `docs/public/languages/support-maturity.md`
