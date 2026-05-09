# SQL Parser

## Purpose

`internal/parser/sql` owns SQL source extraction for schema objects, columns,
routine references, trigger/index relationships, and migration metadata. It
exists so SQL parsing behavior can evolve behind a language-owned package
without depending on the parent parser dispatcher.

## Ownership boundary

This package is responsible for reading one SQL file and returning deterministic
payload buckets for SQL entities and relationships. The parent
`internal/parser` package still owns registry lookup, engine dispatch,
repository path resolution, and content metadata inference.

## Exported surface

The godoc contract is in `doc.go`. Current exports are `Parse` and `Options`.

## Dependencies

This package imports the Go standard library and `internal/parser/shared` for
`Options`, source reads, payload appends, and numeric sorting helpers. It must
not import the parent `internal/parser` package, collector packages, graph
storage, projector, query, or reducer code.

## Telemetry

This package emits no metrics, spans, or logs. Parse timing remains owned by the
collector snapshot path and parent parser engine.

## Gotchas / invariants

Output ordering is part of the parser fact contract. `Parse` deduplicates
entity and relationship rows, then sorts each SQL bucket by line number and
name-compatible fallback before returning.

Migration metadata is path-sensitive. Keep detection rules deterministic and
covered by package-local tests when adding support for another migration tool.

SQL relationship extraction is conservative. Regex-backed mentions should not
claim table truth unless the statement shape provides bounded evidence.

## Related docs

- `docs/plans/2026-05-09-parser-language-layout.md`
