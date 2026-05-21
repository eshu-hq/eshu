# SQL Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` plus the entrypoint and tests listed below.

## Parser Contract
- Language: `sql`
- Family: `language`
- Parser: `DefaultEngine (sql)`
- Entrypoint: `go/internal/parser/sql_language.go`
- Fixture repo: `tests/fixtures/ecosystems/sql_comprehensive/`
- Unit test suite: `go/internal/parser/engine_sql_test.go`
- Integration validation: compose-backed fixture verification (see [Local Testing Runbook](../reference/local-testing.md))

## Capability Checklist
| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
|-----------|----|--------|------------------------|-----------------|---------------|---------------|----------------------|-----------|
| Tables | `sql-tables` | supported | `sql_tables` | `name, line_number` | `node:SqlTable` | `go/internal/parser/engine_sql_test.go::TestDefaultEngineParsePathSQLSchemaObjectsAndRelationships`, `go/internal/parser/sql_core_parity_test.go::TestDefaultEngineParsePathSQLCoreDDLVariants` | Compose-backed fixture verification | Includes bounded `CREATE TABLE IF NOT EXISTS` support. |
| Columns | `sql-columns` | supported | `sql_columns` | `name, line_number` | `node:SqlColumn` | `go/internal/parser/engine_sql_test.go::TestDefaultEngineParsePathSQLSchemaObjectsAndRelationships` | Compose-backed fixture verification | - |
| Views | `sql-views` | supported | `sql_views` | `name, line_number` | `node:SqlView` | `go/internal/parser/engine_sql_test.go::TestDefaultEngineParsePathSQLSchemaObjectsAndRelationships`, `go/internal/parser/engine_sql_test.go::TestDefaultEngineParsePathSQLCreateOrReplaceView`, `go/internal/parser/engine_sql_test.go::TestDefaultEngineParsePathSQLMaterializedViewsAndProcedures` | Compose-backed fixture verification | Includes bounded `CREATE OR REPLACE VIEW` and `CREATE MATERIALIZED VIEW` support via `view_kind=materialized`. |
| Functions | `sql-functions` | supported | `sql_functions` | `name, line_number` | `node:SqlFunction` | `go/internal/parser/engine_sql_test.go::TestDefaultEngineParsePathSQLSchemaObjectsAndRelationships`, `go/internal/parser/engine_sql_test.go::TestDefaultEngineParsePathSQLMaterializedViewsAndProcedures`, `go/internal/parser/sql_core_parity_test.go::TestDefaultEngineParsePathSQLCoreRoutineVariants` | Compose-backed fixture verification | Includes bounded `CREATE PROCEDURE` support via `routine_kind=procedure`, plus tagged dollar-quoted procedural bodies with `LANGUAGE` before or after `AS`. |
| Triggers | `sql-triggers` | supported | `sql_triggers` | `name, line_number` | `node:SqlTrigger` | `go/internal/parser/engine_sql_test.go::TestDefaultEngineParsePathSQLSchemaObjectsAndRelationships` | Compose-backed fixture verification | - |
| Indexes | `sql-indexes` | supported | `sql_indexes` | `name, line_number` | `node:SqlIndex` | `go/internal/parser/engine_sql_test.go::TestDefaultEngineParsePathSQLSchemaObjectsAndRelationships`, `go/internal/parser/sql_core_parity_test.go::TestDefaultEngineParsePathSQLCoreDDLVariants` | Compose-backed fixture verification | Includes bounded `CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS` support. |
| SQL relationships | `sql-relationships` | supported | `sql_relationships` | `type, source_name, target_name, line_number` | `relationship:HAS_COLUMN/REFERENCES_TABLE/READS_FROM/TRIGGERS_ON/EXECUTES/INDEXES` | `go/internal/parser/engine_sql_test.go::TestDefaultEngineParsePathSQLSchemaObjectsAndRelationships` | Compose-backed fixture verification | - |
| Migration intelligence | `sql-migrations` | supported | `sql_migrations` | `tool, target_kind, target_name, line_number` | `relationship:MIGRATES` | `go/internal/parser/engine_sql_test.go::TestDefaultEngineParsePathSQLMigrationMetadata` | Compose-backed fixture verification | - |

## Support Maturity
- Grammar routing: `supported`
- Normalization: `supported`
- Framework pack status: `unsupported`
- Framework packs: -
- Query surfacing: `supported`
- Dead-code maturity: `derived`
- Real-repo validation: `supported (bounded)`
- End-to-end indexing: `supported (bounded)`
- Notes:
  - SQL support runs end to end through the current parser, migration extraction, embedded SQL link hints, and the JSON-backed dbt and data-intelligence families.
- The `code_quality.dead_code` route now scans `SqlFunction` routines as
  derived cleanup candidates. Trigger routines are protected when parsed
  trigger metadata materializes as trigger-to-function `EXECUTES` edges.
- SQL remains non-exact for dead-code cleanup while dynamic SQL,
  dialect-specific routine resolution, and migration-order resolution are
  unresolved.
- The remaining dbt lineage limits
  (unresolved references, truly opaque templated expressions, complex macros,
  and some derived expressions) are bounded non-goals for the documented SQL
  surface. Real-repo and end-to-end validation is bounded by those same
  limits.
- Row-level aggregate lineage, simple windowed expressions, and simple
  qualified macro wrappers such as `dbt_utils.identity(source.amount)` are now
  tracked in the Go dbt path.
- Nested safe wrappers over those supported row-level forms, such as
  `upper(coalesce(source.segment, 'unknown'))`, are also tracked in the Go dbt
  path.
- The checked-in dbt parity matrix now explicitly proves cast, `date_trunc`,
  `concat`, `concat_ws`, `md5`, multi-source `case`, multi-source arithmetic,
  safe scalar wrappers over lineage-preserving qualified macros, top-level
  Jinja wrappers around supported lineage-safe expressions, and the key
  unresolved-summary paths in
  `go/internal/parser/dbt_sql_lineage_parity_test.go`.
- The Go query/content path now also has checked-in proof that dbt-derived
  `AnalyticsModel` and `DataAsset` content entities survive materialization and
  show semantic summaries through the normal entity resolve/context fallback
  surfaces.
- The fixture-backed dbt manifest proof now also covers wildcard expansion plus
  `coalesce(...)` lineage preservation for `orders_expanded.customer_segment` in
  `go/internal/parser/json_dbt_test.go`.
- The checked-in SQL procedural proof now covers `CREATE OR REPLACE FUNCTION`
  bodies, tagged dollar-quoted routine bodies with `LANGUAGE` before or after
  `AS`, `CREATE OR REPLACE VIEW`, `CREATE PROCEDURE`, legacy `EXECUTE
  PROCEDURE` trigger wiring, `CREATE MATERIALIZED VIEW`, `CREATE TABLE IF NOT
  EXISTS`, `CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS`, and fixture-backed
  `ALTER TABLE ... ADD COLUMN ...` column materialization, including bounded
  multi-clause `ADD COLUMN` normalization, in
  `go/internal/parser/engine_sql_test.go`,
  `go/internal/parser/sql_core_parity_test.go`, and
  `go/internal/parser/sql_parity_test.go`.
- The current content and query fallback path now also has checked-in SQL-core
  proof in `go/internal/content/shape/materialize_sql_test.go` and
  `go/internal/query/entity_content_sql_core_fallback_test.go`.
- Compiled-model lineage still carries explicit unresolved limits for
  unresolved references, truly opaque templated expressions, complex macros,
  and some derived expressions.
- Templated wrappers around opaque macro bodies stay unresolved on purpose;
  they are reported as `templated_expression_not_resolved` instead of being
  guessed into lineage.

## Data-Intelligence Path

The SQL and analytics runtime is implemented end to end in the current platform.

- Native SQL parsing and schema-object extraction live in `go/internal/parser/sql_language.go`
- Migration intelligence lives in `go/internal/parser/sql_migrations.go`
- Embedded SQL extraction for Go code lives in `go/internal/parser/go_embedded_sql.go`
- dbt compiled-SQL lineage lives in `go/internal/parser/dbt_sql_lineage.go`
- dbt manifest shaping lives in `go/internal/parser/json_dbt_manifest.go`
- JSON data-intelligence families live in `go/internal/parser/json_data_intelligence.go`
- dbt content-entity materialization proof lives in
  `go/internal/content/shape/materialize_analytics_test.go`
- dbt entity resolve/context proof lives in
  `go/internal/query/entity_content_sql_fallback_test.go`


## Known Limitations
- SQL routine dead-code is `derived`, not exact. Dynamic SQL, dialect-specific
  routine resolution, and migration-order-dependent reachability remain named
  exactness blockers.
- Dialect-specific procedural SQL beyond the common Postgres-style
  dollar-quoted function and procedure bodies proven above remains a bounded
  non-goal.
- Broader ALTER/DDL mutation normalization beyond checked-in `ADD COLUMN`
  materialization, bounded multi-clause `ADD COLUMN` normalization, and the
  core table/index variants proven above remains a bounded non-goal.
- Compiled dbt lineage still records partial coverage for unresolved references,
  templated expressions, complex macro expansion, and some derived
  expressions.
- Templated wrappers around opaque macro bodies remain an intentional non-goal
  and are surfaced as `templated_expression_not_resolved`. Non-templated
  opaque wrappers are not treated as one clean resolved category.
