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
- `Parse` extracts deterministic bucket rows for one decoded document.

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

## Related docs

- `docs/plans/2026-05-09-parser-language-layout.md`
