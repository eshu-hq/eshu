# dbt SQL Lineage

## Purpose

`internal/parser/dbtsql` extracts bounded column lineage from compiled dbt model
SQL. It powers JSON dbt manifest parsing without making the JSON package import
the parent parser package.

## Ownership boundary

This package owns SQL projection parsing, CTE binding, relation alias binding,
supported transform metadata, and unresolved-expression reasons for dbt model
lineage. Expression helpers are split out so the bounded transform rules stay
readable and no helper file grows into a second parser. It does not own JSON
manifest decoding, data asset row construction, parser dispatch, collector fact
persistence, or graph projection.

## Exported surface

The godoc contract is in `doc.go`. Current exports are:

- `ColumnLineage` describes one output column and its source columns.
- `CompiledModelLineage` summarizes lineage and unresolved references.
- `ExtractCompiledModelLineage` extracts lineage from one compiled SQL string.

## Dependencies

This package imports only the Go standard library. It must not import the parent
`internal/parser` package, JSON parser package, collector, storage, query,
projector, or reducer packages.

## Telemetry

This package emits no metrics, spans, or logs. Parser timing remains owned by
the collector snapshot path and parent engine callers.

## Gotchas / invariants

The extractor is intentionally bounded. Expression helpers recognize only the
transform shapes covered by fixtures. Unsupported derived, templated,
aggregate, macro, window, and multi-input cases return explicit unresolved
reasons instead of guessing column truth.

`ExtractCompiledModelLineage` must stay deterministic. Relation maps,
projection order, unresolved summaries, and transform metadata are fixture-backed
contracts consumed by JSON dbt manifest parsing.

Expression support is intentionally narrow. Add new transforms only with
positive lineage coverage and an unresolved case that proves unsupported shapes
remain honest.

## Related docs

- `docs/plans/2026-05-09-parser-language-layout.md`
