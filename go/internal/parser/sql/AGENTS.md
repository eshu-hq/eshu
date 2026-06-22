# AGENTS.md - internal/parser/sql guidance

## Read first

1. README.md - package boundary, extraction/dialect strategy, gaps, invariants
2. doc.go - godoc contract for the SQL helper package
3. language.go - top-level `Parse` flow, segment parsing, procedure rewrite
4. segments.go - statement segmentation that bounds each parse
5. ast.go - tree-sitter node navigation helpers (named/child-by-kind, refs)
6. entities.go - per-statement AST extractors and payload assembly
7. nodes.go - column type, constraint, index, and routine node readers
8. shared.go - name normalization, line numbers, AST table-mention collection
9. migrations.go - migration-tool path detection and migration row assembly
10. migrations_test.go - package-local migration layout coverage

## Invariants this package enforces

- Dependency direction stays one way: parent parser code may import this
  package, but this package must not import `internal/parser`.
- `Parse` returns the same payload shape and ordering the parent SQL adapter
  emitted before the package split.
- SQL entity and relationship rows are deduplicated before sorting.
- Line numbers refer to the original SQL source file.
- Migration-tool detection is path-sensitive and must remain deterministic.

## Common changes and how to scope them

- Add SQL DDL support by writing a focused SQL parser test first, then adding
  the statement kind to `sqlStatementKinds` (ast.go) and a dedicated AST
  extractor in `entities.go`. Inspect real grammar node kinds with a scratch
  parse before assuming a shape; do not reintroduce regex symbol extraction.
- Add migration-tool support in `migrations.go` with package-local coverage in
  `migrations_test.go`. Migration detection is path-based regex only and never
  extracts SQL symbols.
- Keep table constraints out of `sql_columns` while preserving bounded
  references relationships from those same constraint clauses.
- Keep registry dispatch, engine routing, and content metadata inference in the
  parent parser package.
- Keep shared helpers language-neutral. SQL-only helpers belong in this package.

## Failure modes and how to debug

- Missing table, view, function, trigger, or index rows usually mean the
  statement construct landed under an unexpected grammar node, so
  `visitStatementConstructs` did not reach it, or the segment failed to parse.
  Print the root node S-expression for the failing segment to confirm node
  kinds before editing an extractor.
- Wrong relationships usually mean `collectMentionsFromNode` walked a clause it
  should have skipped or the segmenter merged two statements.
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
