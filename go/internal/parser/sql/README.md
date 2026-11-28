# SQL Parser

## Purpose

`internal/parser/sql` owns SQL source extraction for schema objects, columns,
routine reads and writes, trigger/index relationships, and migration metadata. It
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

## Engine test surface

`engine_sql_test.go`, `sql_parity_test.go`, and `sql_core_parity_test.go`
(external package `sql_test`, relocated from the parent by #6062) drive
`parser.DefaultEngine().ParsePath` against inline and
`tests/fixtures/ecosystems/sql_comprehensive` SQL fixtures: 11 test functions,
from
`rg --no-filename -o '^func Test' engine_sql_test.go sql_parity_test.go sql_core_parity_test.go | wc -l`
run in this directory. They may import `internal/parser` because they sit in
the external `sql_test` package: an external test package is compiled
separately from `internal/parser` itself, so the import does not close the
cycle that `parsertest` would otherwise create. Keep that exception limited to
black-box tests of the public parent engine. Shared assertions come from
`go/internal/parser/parsertest`; `sql_test_helpers_test.go` holds only
`sqlFixturePath` (the parent's `repoFixturePath` is unexported and lives in
`internal/parser`'s own `testhelpers_test.go`, so `package sql_test` cannot
reach it -- a visibility constraint, not a directory-depth one) and `writeSQLTestFile` (creates
parent directories before delegating to `parsertest.WriteFile` for the nested
`prisma/migrations/` and `migrations/` fixtures). The SQL-only
`assertSQLRelationship` and `assertSQLRelationshipMissing` assertions live in
`engine_sql_test.go` because no other language asserts on the
`sql_relationships` bucket. The 8 `package sql` test files stay white-box and
must not import the parent.

Run the engine tests from the `go/` module root with
`go test ./internal/parser/sql -count=1`; pin a subset with
`../scripts/go-test-run-guard.sh 11 TestDefaultEngineParsePathSQL -- ./internal/parser/sql -count=1`,
which runs from the `go/` module root and fails closed if fewer than 11 tests
match (minimum derived from
`go test -list 'TestDefaultEngineParsePathSQL' ./internal/parser/sql`).

## Telemetry

This package emits no metrics or spans; parse timing remains owned by the
collector snapshot path and parent parser engine. It does emit one structured
`slog.Warn` log line, `"sql parse segment bounded"`, when a statement segment
exceeds `maxSQLSegmentBytes` and is bounded before (or instead of) a
tree-sitter parse (#4422). See
[Large-schema line-number lookup (#4422)](#large-schema-line-number-lookup-4422)
below for the payload and log field shapes.

## Extraction strategy

All SQL DDL/DML symbol extraction runs on a tree-sitter abstract syntax tree.
The package vendors the `github.com/alexaandru/go-sitter-forest/sql` grammar (a
Go packaging of DerekStride/tree-sitter-sql) through the parent parser runtime
loader. Tables, columns, views, materialized views, functions, procedures,
triggers, indexes, and their relationships are read from grammar nodes
(`create_table`, `column_definition`, `object_reference`, and the rest); no SQL
DDL regular expressions remain.

The file is segmented into statement-sized fragments (`segments.go`) before
parsing. The grammar parses one statement reliably but degrades on concatenated
or malformed input, so per-statement parsing recovers a malformed statement
without losing its neighbours. Segment byte offsets are added back to node
positions so line numbers refer to the original source.

The only regular expressions left in this package are the migration-tool
**path** patterns in `migrations.go` (for example `/prisma/migrations/` or the
Flyway `V<n>__*.sql` filename convention). These classify a file by its path and
never extract SQL symbols.

### Dialect strategy

The grammar is dialect-agnostic at the DDL surface this package extracts.
Dialect handling is concentrated in name normalization (`normalizeSQLName`
strips PostgreSQL/ANSI `"quotes"`, MySQL `` `backticks` ``, and MSSQL
`[brackets]`) and in one bounded source rewrite:

- **PostgreSQL / ANSI**: parsed directly. Dollar-quoted routine bodies
  (`$$...$$`, `$tag$...$tag$`) are recognized by the segmenter and the routine
  body walker.
- **MySQL**: backtick identifiers are normalized; standard DDL parses directly.
- **MSSQL (T-SQL)**: bracket identifiers are normalized. `CREATE PROCEDURE`,
  which the grammar does not parse, is rewritten to `CREATE FUNCTION ... RETURNS
  void` by a bounded keyword/clause transform (`rewriteProcedureSegment`) before
  parsing; the routine name, arguments, body, and `LANGUAGE` clause are
  preserved verbatim for AST extraction and the routine is flagged as a
  procedure.
- **SQLite**: standard `CREATE TABLE/VIEW/INDEX/TRIGGER` DDL parses directly.

## Documented coverage gaps

These are honest limitations, not faked coverage. They match or improve on the
prior regex behavior:

- The grammar cannot parse `CREATE PROCEDURE` natively; it is recovered via the
  bounded `CREATE FUNCTION` rewrite above. A procedure whose header the rewrite
  cannot locate (no balanced argument list) is still parsed for its body but may
  miss the `RETURNS`-shim insertion.
- `select` mentions inside routine and view bodies emit `READS_FROM` and stamp
  the entity's bounded `source_tables` metadata (#5345). Routine
  `INSERT`/`UPDATE`/`DELETE` targets emit `WRITES_TO` and stamp bounded
  `write_tables` metadata (#5410); they never become reads. Table-level and
  inline foreign keys stamp bounded `referenced_tables` metadata for
  `REFERENCES_TABLE` (#5410). In recognized migration files, non-select target
  mentions also live under the single `SqlMigration` entity's
  `migration_targets` metadata (#5346), which the reducer resolves into
  `MIGRATES`. A target reached only through `select` is excluded because a
  read-only backfill does not migrate its source table. Comma-separated
  `DROP TABLE` targets are recorded with `operation: "drop"` up to the shared
  64-target cap; truncation is deterministic and does not emit a new `SqlTable`
  entity. The operation remains migration-target metadata:
  `MIGRATES` continues to represent adjacency/provenance, and migration-order
  reachability and head-state absence are not inferred.
- Highly dialect-specific statements outside the extracted construct set
  (sequences, types, policies, grants, vendor pragmas) are not extracted, the
  same as before.
- A statement segment over `maxSQLSegmentBytes` (256 KiB) has its dollar-quoted
  body elided, or its tree-sitter parse skipped entirely if still oversized
  after elision (#4422, see below). The routine's own body-level table
  mentions (`READS_FROM` and `WRITES_TO` relationships sourced from inside the
  elided or skipped body) are lost in that case; the routine's signature entity is not.
  This is recorded in `payload["sql_parse_bounded"]`, never silently dropped.

## Gotchas / invariants

Output ordering is part of the parser fact contract. `Parse` deduplicates
entity and relationship rows, then sorts each SQL bucket by line number and
name-compatible fallback before returning.

Migration metadata is path-sensitive. Keep detection rules deterministic and
covered by package-local tests when adding support for another migration tool.
`buildSQLMigrationEntries` (`migrations.go`) emits exactly one `SqlMigration`
entity per recognized migration file, never one row per touched target — a
nameless row would mint a garbage uid through the generic content-entity
pipeline (#5346). Prisma names every migration file `migration.sql`, so its
stamped identifier is the migration's parent directory name instead of the
basename; every other supported tool's filename is already a meaningful
identifier.

`migration_targets` deduplicates only identical `(kind, name, operation)`
entries, retaining the first source line. A migration that creates and later
drops the same target therefore preserves both operation records, while the
reducer still emits one `MIGRATES` edge for that migration-target pair.

SQL relationship extraction is conservative. Table constraints such as
`PRIMARY KEY`, `FOREIGN KEY`, `UNIQUE`, `CHECK`, and `EXCLUDE` are not SQL column
rows, but their bounded `REFERENCES` clauses still emit table relationships and
stamp reducer-consumable entity metadata.

## Related docs

- `docs/public/languages/support-maturity.md`
