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
  read-only backfill does not migrate its source table. Every comma-separated
  `DROP TABLE` target is recorded with `operation: "drop"` without emitting a
  new `SqlTable` entity. The operation remains migration-target metadata:
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

## Performance and observability evidence

The regex-to-tree-sitter migration changes the parser parse stage, so it carries
tracked before/after evidence here.

- Performance Evidence: `BenchmarkParseComprehensive` (in
  `language_bench_test.go`) parses one representative multi-construct SQL
  document (tables, columns, index, view, materialized view, function,
  procedure, trigger, alter-table, DML reference scanning).
  - Baseline (regex implementation at merge-base `84bb4c87`, same benchmark
    body, Apple M5 Max, `darwin/arm64`, `-benchmem -count=3`): best
    `1,599,272 ns/op`, `31,916 B/op`, `456 allocs/op`.
  - After (tree-sitter AST, this branch, identical input and host): best
    `713,081 ns/op`, `127,222 B/op`, `4,025 allocs/op`.
- No-Regression Evidence: wall time on the dominant parse cost improves about
  2.2x (1.60 ms to 0.71 ms per file). Heap per parse rises from ~32 KB to
  ~127 KB and allocations from 456 to ~4,025 because the grammar builds a
  syntax tree; the absolute per-file cost stays small and the tree is closed per
  segment (`defer tree.Close()` in `parseSegment`), so there is no retained
  growth across files. Reproduce with:
  `go test ./internal/parser/sql -run '^$' -bench BenchmarkParseComprehensive -benchmem -count=3`.
- No-Regression Evidence (#5482): `DROP TABLE` scans only direct target
  references in the existing AST mention walk, including the grammar's direct
  `ERROR` recovery child for valid comma-separated lists. It adds no parse pass,
  queue work, graph write, or new entity. The existing 64-target
  `migration_targets` cap remains in force. `go test ./internal/parser/sql
  -count=1` passes after the change; the intentional output delta is bounded
  `operation: "drop"` target metadata for recognized DROP migrations.
- Observability Evidence: No-Observability-Change. This package emits no new
  metric, span, log, status field, or runtime knob for #5482. Its existing
  oversized-segment path may still emit `slog.Warn("sql parse segment
  bounded", ...)` as described under Telemetry; parse timing remains owned by
  the collector snapshot path and the parent parser engine.

### Large-schema line-number lookup (#4422)

Performance Evidence: `BenchmarkParseLargeSQLSchemaLineNumbers` parses one
synthetic public-safe `CREATE TABLE` statement with 4,000 column definitions,
which stresses the line-number mapping used for emitted table, column, and
migration rows. Baseline `origin/main` at `3f38f41fc` with the old
`strings.Count(string(source[:offset]), "\n")` lookup, same benchmark body,
Apple M5 Max, `darwin/arm64`, `-benchmem -count=3`: `50.51 ms/op`, `50.87 ms/op`, and
`50.51 ms/op`; about `232.4 MB/op`, `315,796 allocs/op`. After the precomputed
newline index on the rebased branch, a quiet local rerun with `-count=5`
measured `39.63 ms/op`, `39.03 ms/op`, `42.86 ms/op`, `35.16 ms/op`, and
`36.47 ms/op`; about `11.87 MB/op`, `311,809-311,810 allocs/op`.

No-Regression Evidence: `go test ./internal/parser/sql -count=1` and
`TestSQLLineIndexMatchesLineNumberForOffsets` prove line numbers still match
the old 1-based behavior for start, middle, negative, and past-end offsets. The
change preserves SQL payload buckets, sort order, source offsets, tree-sitter
parsing, statement segmentation, and migration target semantics. It removes the
per-emitted-row source-prefix allocation, but it does not claim the full private
corpus outlier is fully resolved until a fresh full-corpus run confirms no
single SQL file dominates collection wall time.

Observability Evidence: No-Observability-Change. This change adds no metrics,
spans, logs, status fields, or runtime knobs. The package's existing bounded-
segment warning remains unchanged. Operators continue to use collector
parse-stage timing and `eshu_dp_file_parse_duration_seconds`; the new benchmark
is the focused local proof for the public-safe large-schema shape.

### Oversized dollar-quoted routine body (#4422)

Root cause: a single `CREATE FUNCTION ... AS $$ <body> $$` statement whose
dollar-quoted body is large is handed whole to tree-sitter (`parseSegment` in
`language.go`), because the segmenter treats a dollar-quoted span as one
un-split unit (`segments.go`). tree-sitter parses that opaque body
superlinearly and can hard-crash the process via a tree-sitter error-recovery
assertion (`stack_node_retain`, SIGABRT) on malformed/pathological input. There
was previously no size cap anywhere in the SQL parser.

Performance Evidence: characterized on this branch with a synthetic
`CREATE FUNCTION` whose dollar-quoted plpgsql body was built from repeated
`UPDATE users SET x = x + 1 WHERE id = 5;` lines. At a 1 MB body, parsing
exceeded a 90-second test bound (a floor, not the true unbounded time) and was
separately observed to crash the process via the tree-sitter assertion above;
the corpus outlier this reproduces was reported in #4422 at approximately 1140s
for one file. After bounding segments over `maxSQLSegmentBytes` (256 KiB) by
eliding the interior of every dollar-quoted body (keeping the delimiters, so the
buffer stays well-formed), `TestParseBoundsOversizedDollarQuotedFunctionBody`
parses the same 1 MB-body shape in about 0.02s (`darwin/arm64`) with no crash --
from a >90s floor (test-capped) to sub-second. Reproduce with:
`go test ./internal/parser/sql -run TestParseBoundsOversizedDollarQuotedFunctionBody -v -count=1`.

No-Regression Evidence: `TestParseSmallDollarQuotedFunctionIsUnaffected` proves
an ordinary, under-cap dollar-quoted function still extracts its signature
entity and its body table mention (`READS_FROM`) exactly as before, with no
`sql_parse_bounded` entry. `TestSegmentBoundThresholdElidesOnlyOverCapBodies`
proves the exact `maxSQLSegmentBytes` boundary: a segment just under the cap is
untouched, one just over triggers elision.
`TestParseDoesNotCrashOnPathologicalNonDollarQuotedSegment` proves a
non-dollar-quoted oversized segment (which elision cannot shrink) still returns
without panicking or hanging by skipping the tree-sitter parse for that segment
(`action="segment_skipped"`). `BenchmarkParseComprehensive` remains unaffected
(all its statements are far under the 256 KiB cap).

Observability Evidence: every bounded segment is recorded in
`payload["sql_parse_bounded"]` (fields: `path`, `segment_offset`,
`original_bytes`, `action` = `"body_elided"` or `"segment_skipped"`) and
logged via `slog.Warn("sql parse segment bounded", ...)` with the same fields
plus `component="parser.sql"`, so a dropped routine body is observable, never
silent. This is a genuine observability addition (see Telemetry above), not a
No-Observability-Change.

## Related docs

- `docs/public/languages/support-maturity.md`
