# SQL Parser

This page describes the current Go parser and query contract for SQL. For the
full matrix, see [Parser Feature Matrix](feature-matrix.md) and
[Parser Support Matrix](support-maturity.md).

## Parser Contract

| Field | Value |
| --- | --- |
| Language | `sql` |
| Parser | `DefaultEngine (sql)` |
| Entrypoint | `go/internal/parser/sql_language.go` |
| Fixture repo | `tests/fixtures/ecosystems/sql_comprehensive/` |
| Main parser tests | `go/internal/parser/engine_sql_test.go`, `go/internal/parser/sql_core_parity_test.go`, `go/internal/parser/sql_parity_test.go` |
| Runtime validation | Compose-backed fixture verification; see [Local Testing](../reference/local-testing.md) |

## Supported Surfaces

| Surface | Current contract |
| --- | --- |
| Schema objects | Tables, columns, views, materialized views, indexes, functions, procedures, triggers, and migration metadata. |
| Relationships | Parsed and materialized to the graph: `HAS_COLUMN`; `READS_FROM` (view/function -> table or view, select-only direct edges, #5345); `REFERENCES_TABLE` (FK table -> table, #5410); `WRITES_TO` (function/procedure -> table for `INSERT`/`UPDATE`/`DELETE`, #5410); `TRIGGERS`; `EXECUTES`; `QUERIES_TABLE`; `INDEXES`; and `MIGRATES` (migration file -> forward target it creates, alters, or DML-writes; select-only mentions and DROP targets are excluded, #5346). SQL-table blast radius follows `READS_FROM` through at most two view hops; stored graph edges remain direct. See [Edge Source-Tool Provenance](../reference/edge-source-tool-provenance.md#tier-3-no-edge-level-tool-intentional). |
| Routine metadata | Bounded Postgres-style function and procedure bodies, including dollar-quoted bodies and `LANGUAGE` metadata. |
| dbt lineage | Compiled-model lineage for supported select expressions, safe scalar wrappers, and documented unresolved summaries. Parsed into the `data_relationships` payload bucket (`COMPILES_TO`, `ASSET_DERIVES_FROM`, `COLUMN_DERIVES_FROM`, `USES_MACRO`) but not currently materialized to the graph or exposed through `content_relationships` — see [Edge Source-Tool Provenance](../reference/edge-source-tool-provenance.md#tier-3-no-edge-level-tool-intentional). |
| Query fallback | SQL content entities can surface through entity resolve/context when materialized content rows exist. |

## Operational Notes

Large SQL schema files precompute a newline index before extraction. Emitted
table, column, relationship, and migration rows still use 1-based original file
line numbers, but offset-to-line lookup no longer scans from the beginning of
the file for every emitted row.

## Dead-Code Support

SQL dead-code support is `derived`. Stored routines can be returned as
candidates, and parser-proven trigger-to-function `EXECUTES` edges protect
trigger-invoked routines.

SQL remains non-exact for cleanup because dynamic SQL, dialect-specific routine
resolution, and migration-order reachability are unresolved.

## Framework And Library Support

Supported today:

- This parser does not claim application-framework support.
- Stored routines and parser-proven trigger-to-function `EXECUTES` edges are
  modeled as derived reachability evidence.
- dbt compiled-model lineage is supported only for the documented select
  expression shapes.

Not claimed today:

- Dynamic SQL, dialect-specific routine resolution, migration-order
  reachability, and broad dbt macro semantics remain outside the exactness
  boundary.

## Known Limitations

- Procedural SQL beyond the checked Postgres-style routine forms is not a broad
  language guarantee.
- Broader DDL mutation normalization beyond checked `ADD COLUMN`, table, index,
  view, routine, and trigger shapes is bounded.
- dbt lineage intentionally reports unresolved references, opaque templated
  expressions, complex macros, and some derived expressions instead of guessing.
- A single SQL statement segment larger than 256 KiB has its dollar-quoted
  routine body elided (or, if still oversized, its parse skipped) before
  tree-sitter runs, to bound a superlinear/aborting parse on very large routine
  bodies (#4422). The routine's signature entity is still extracted;
  `READS_FROM` and `WRITES_TO` mentions sourced from inside the elided body are not. The bound
  is recorded in `payload["sql_parse_bounded"]` and logged, never silently
  dropped.

## Related Docs

- [Dead Code Language Maturity](../reference/dead-code-language-maturity.md)
- [Language Query DSL](../reference/language-query-dsl.md)
- [Local Testing](../reference/local-testing.md)
