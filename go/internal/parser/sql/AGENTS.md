# AGENTS.md - internal/parser/sql guidance

## Read first

1. README.md - package boundary, SQL payload behavior, and invariants
2. doc.go - godoc contract for the SQL helper package
3. language.go - top-level `Parse` flow and SQL entity bucket assembly
4. shared.go - SQL name normalization, line numbers, and table mention helpers
5. routines.go - function/procedure body extraction and reference scanning
6. migrations.go - migration-tool detection and migration row assembly
7. migrations_test.go - package-local migration layout coverage

## Invariants this package enforces

- Dependency direction stays one way: parent parser code may import this
  package, but this package must not import `internal/parser`.
- `Parse` returns the same payload shape and ordering the parent SQL adapter
  emitted before the package split.
- SQL entity and relationship rows are deduplicated before sorting.
- Line numbers refer to the original SQL source file.
- Migration-tool detection is path-sensitive and must remain deterministic.

## Common changes and how to scope them

- Add SQL DDL support by writing a focused SQL parser test first, then updating
  the narrow parser helper that owns the statement shape.
- Add migration-tool support in `migrations.go` with package-local coverage in
  `migrations_test.go`.
- Keep table constraints out of `sql_columns` while preserving bounded
  references relationships from those same constraint clauses.
- Keep registry dispatch, engine routing, and content metadata inference in the
  parent parser package.
- Keep shared helpers language-neutral. SQL-only helpers belong in this package.

## Failure modes and how to debug

- Missing table, view, function, trigger, or index rows usually mean the
  statement header regex did not match the dialect variant.
- Wrong relationships usually mean `collectSQLTableMentions` matched an
  unbounded statement fragment or missed a clause boundary.
- Wrong source snippets under `IndexSource` usually mean start/end offsets were
  changed without preserving original source positions.
- Missing migration rows usually mean `detectSQLMigrationTool` did not classify
  the normalized path.

## Anti-patterns specific to this package

- Importing the parent parser package to reuse engine or registry types.
- Emitting unsorted map-derived rows.
- Adding broad SQL name guesses that turn comments, aliases, or expressions
  into table truth.
- Moving dbt compiled-model lineage into this package without a separate design;
  that code has its own parent-package compatibility surface today.

## What NOT to change without an ADR

- Do not change SQL payload bucket names, entity type strings, relationship
  type strings, or migration target fields without updating content shape,
  facts, and downstream query expectations in the same branch.
