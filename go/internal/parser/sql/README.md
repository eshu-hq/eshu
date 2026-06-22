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
- DML mentions inside routine and view bodies materialize as `READS_FROM`
  relationships rather than mutation-specific relationship types (`WRITES_TO`,
  and similar). This preserves the prior contract.
- Highly dialect-specific statements outside the extracted construct set
  (sequences, types, policies, grants, vendor pragmas) are not extracted, the
  same as before.

## Gotchas / invariants

Output ordering is part of the parser fact contract. `Parse` deduplicates
entity and relationship rows, then sorts each SQL bucket by line number and
name-compatible fallback before returning.

Migration metadata is path-sensitive. Keep detection rules deterministic and
covered by package-local tests when adding support for another migration tool.

SQL relationship extraction is conservative. Table constraints such as
`PRIMARY KEY`, `FOREIGN KEY`, `UNIQUE`, `CHECK`, and `EXCLUDE` are not SQL column
rows, but their bounded `REFERENCES` clauses still emit table relationships.

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
- Observability Evidence: No-Observability-Change. This package emits no
  metrics, spans, or logs by contract (see Telemetry above); parse timing
  remains owned by the collector snapshot path and the parent parser engine,
  which are unchanged. The migration alters extraction internals only, not any
  operator-facing signal, status field, or wire contract.

## Related docs

- `docs/public/languages/support-maturity.md`
