# SQL Parser Audit

## Overview
The SQL parser (`go/internal/parser/sql/`) extracts schema objects, columns, routines, triggers, indexes, and relationships from SQL DDL source using a tree-sitter grammar. It segments source into statement-sized fragments for fault tolerance, rewrites `CREATE PROCEDURE` to `CREATE FUNCTION` for MSSQL T-SQL support, normalizes dialect-specific quote characters, and emits migration metadata for Prisma, Flyway, Liquibase, and golang-migrate projects.

## Claimed Constructs
List every construct the parser claims to extract, with source references.

1. **Tables** — `entities.go:69-79`, `ast.go:19` (`create_table` → `sql_tables`)
2. **Columns** — `entities.go:95-123`, `ast.go:88` (`column_definition` → `sql_columns`)
3. **Views** — `entities.go:132-155` (`create_view` → `sql_views`)
4. **Materialized views** — `entities.go:44-45`, `:132-155` (`create_materialized_view` → `sql_views` with `view_kind`)
5. **Functions** — `entities.go:157-186` (`create_function` → `sql_functions`)
6. **Procedures** — `entities.go:160-162`, `language.go:89-96` (`CREATE PROCEDURE` rewrite → `sql_functions` with `routine_kind=procedure`)
7. **Triggers** — `entities.go:202-226` (`create_trigger` → `sql_triggers`)
8. **Indexes** — `entities.go:188-200` (`create_index` → `sql_indexes`)
9. **Alter table add column** — `entities.go:228-260` (`alter_table` → `sql_columns`)
10. **Relationships** (`entities.go:315-335`, `shared.go:62-111`):
    - `HAS_COLUMN` — table to column
    - `REFERENCES_TABLE` — table to referenced table (inline REFERENCES and table-level FK constraints)
    - `READS_FROM` — view/function to table (SELECT in body)
    - `WRITES_TO` — function/procedure to table (`INSERT`/`UPDATE`/`DELETE` target)
    - `TRIGGERS_ON` — trigger to table
    - `EXECUTES` — trigger to function
    - `INDEXES` — index to table
11. **Migration metadata** — `migrations.go:40-119`:
    - Tool detection: Prisma, Flyway, Liquibase, golang-migrate, generic
    - Migration rows: `tool`, `target_kind`, `target_name`, `line_number`
12. **Statement segmentation** — `segments.go:31-89` (CREATE/ALTER boundaries, string/comment/dollar-quote skipping)
13. **CREATE PROCEDURE rewrite** — `language.go:151-179` (keyword replacement + `RETURNS void` insertion)
14. **Dialect name normalization** — `shared.go:28-44` (PostgreSQL `"..."`, MySQL `` `...` ``, MSSQL `[...]`)
15. **Inline column REFERENCES** — `nodes.go:58-71`
16. **Routine language detection** — `nodes.go:107-117`
17. **IndexSource with procedure source mapping** — `entities.go:282-313`
18. **DML mention collection (insert/update/delete/alter/references)** — `shared.go:61-111`

## Verified-by-Test Constructs
List constructs verified by tests, with file:function references.

1. **Tables, views, functions, triggers, indexes** — `engine_sql_test.go:11-87` (`TestDefaultEngineParsePathSQLSchemaObjectsAndRelationships`)
2. **Columns** — `engine_sql_test.go:76-79` (id, org_id, email columns)
3. **HAS_COLUMN relationship** — `engine_sql_test.go:81`
4. **REFERENCES_TABLE relationship** — `engine_sql_test.go:82`
5. **READS_FROM relationship** — `engine_sql_test.go:83`
6. **TRIGGERS_ON relationship** — `engine_sql_test.go:84`
7. **EXECUTES relationship** — `engine_sql_test.go:85`
8. **INDEXES relationship** — `engine_sql_test.go:86`
9. **Prisma migration metadata** — `engine_sql_test.go:89-136` (`TestDefaultEngineParsePathSQLMigrationMetadata`)
10. **CREATE OR REPLACE VIEW** — `engine_sql_test.go:139-166` (`TestDefaultEngineParsePathSQLCreateOrReplaceView`)
11. **ALTER TABLE ADD COLUMN** — `engine_sql_test.go:168-196` (`TestDefaultEngineParsePathSQLAlterTableAddColumnMaterializesColumn`)
12. **Multiple ADD COLUMN clauses** — `engine_sql_test.go:198-230` (`TestDefaultEngineParsePathSQLAlterTableNormalizesMultipleAddColumnClauses`)
13. **Materialized views and procedures** — `engine_sql_test.go:232-272` (`TestDefaultEngineParsePathSQLMaterializedViewsAndProcedures`)
14. **Partial recovery from malformed SQL** — `engine_sql_test.go:274-306` (`TestDefaultEngineParsePathSQLPartialRecovery`)
15. **Table constraints NOT materialized as columns** — `sql/language_test.go:27-48` (`TestParseDoesNotMaterializeTableConstraintsAsColumns`)
16. **CREATE OR REPLACE PROCEDURE parsing, routine reads, and routine writes** — `sql/language_test.go` (`TestParseRoutineViewAndMigrationReferences`)
17. **Procedure IndexSource preserves original text** — `sql/language_test.go:102-126` (`TestParseProcedureIndexedSourceIsOriginalText`)
18. **Migration targets from ALTER and REFERENCES** — `sql/language_test.go:131-153` (`TestParseMigrationTargetsFromAlterAndReferences`)
19. **Migration tool detection** — `sql/migrations_test.go:8-59` (Prisma, Flyway, Liquibase, golang-migrate, generic, negative)
20. **Performance benchmark** — `sql/language_bench_test.go` (`BenchmarkParseComprehensive`)
21. **Bounded, deterministic routine write targets** — `sql/source_tables_cap_test.go` (`TestRoutineWriteTargetsCapTruncatesDeterministically`)

## Unverified / Claimed-but-Untested Constructs
List constructs claimed but not covered by any test.

1. **Flyway migration filename detection** (`migrations.go`): tested via `detectSQLMigrationTool` but not integrated with a Flyway-migration path parse.
2. **Liquibase migration path detection** (`migrations.go`): classified but not integrated.
3. **`golang-migrate` path detection** (`migrations.go`): classified but not integrated.
4. **Schema-qualified trigger names**: tested with `public.users` schema but not with a double-quoted trigger target.
5. **Dollar-quote body with tag**: covered by the procedure test but not isolated from the other routine behavior.
6. **Column `data_type` multi-token spelling**: `CHARACTER VARYING(20)` is not covered.
7. **Commented-out SQL inside segments**: line and block comment skipping is not isolated in a test.

## Edge Cases Considered
List edge cases the tests actually cover with test references.

- **Table-level constraints (PRIMARY KEY, FOREIGN KEY, UNIQUE) not treated as columns** — `sql/language_test.go:27-48`
- **CREATE OR REPLACE PROCEDURE with `$$` dollar-quote body** — `sql/language_test.go:50-97`
- **Procedure IndexSource preserves original `PROCEDURE` text (not synthetic `FUNCTION` rewrite)** — `sql/language_test.go:102-126`
- **ALTER TABLE migration target via table mention** — `sql/language_test.go:131-142`
- **REFERENCES (foreign key) migration target via `ALTER TABLE ... ADD CONSTRAINT`** — `sql/language_test.go:143-153`
- **Partial recovery from malformed statement** — `engine_sql_test.go:274-306` (broken CREATE TABLE, valid VIEW still parsed)
- **CREATE OR REPLACE VIEW** — `engine_sql_test.go:139-166`
- **Materialized views** — `engine_sql_test.go:232-272`
- **Multiple ALTER TABLE ADD COLUMN clauses** — `engine_sql_test.go:198-230`
- **Inline column REFERENCES emitting REFERENCES_TABLE** — `entities.go:118-122`, tested via `engine_sql_test.go:24-27,82`
- **Routine INSERT/UPDATE/DELETE targets emit WRITES_TO, never READS_FROM** — `sql/language_test.go` and `engine_sql_test.go`
- **MySQL backtick and MSSQL bracket identifiers** — `sql/language_test.go`
- **Migration INSERT/UPDATE/DELETE targets** — `sql/migration_metadata_test.go`
- **Deduplication of entities and relationships** — tested at scale via comprehensive fixture test
- **Performance benchmark comparing regex vs tree-sitter** — `sql/language_bench_test.go`

## Edge Cases NOT Considered
List edge cases not tested.

- **SQLite bare identifiers** — no SQLite-specific fixture
- **Dialect-specific write statements beyond `INSERT`/`UPDATE`/`DELETE`**
- **Very large SQL files with 100+ segments**
- **Embedded `;` inside a string literal in a segment** — segmenter skips strings, but not tested with `';'`
- **Dollar-quote body with embedded `$$`** (should be handled by correct tag matching, but not tested)
- **`CREATE TABLE ... LIKE ...` or `CREATE TABLE ... AS SELECT ...`** — not tested
- **`DROP TABLE`, `DROP VIEW`, `DROP INDEX` statements** — not extracted per contract, but not tested as pass-through
- **UNICODE identifiers** (non-ASCII table/column names)
- **Concurrent parsing of the same file** (thread safety for parser reuse)

## Verdict
moderate

The SQL parser has parent-level engine tests covering core entity extraction, seven relationship types, migrations, materialized views, procedures, ALTER TABLE, and partial recovery. Package tests cover constraint/column separation, routine read/write classification, procedure IndexSource preservation, target caps, dialect quoting, and tool detection. Multi-token data types and Flyway/Liquibase migration path integration remain unverified.

## Recommended Actions
1. Add a test for multi-word data types (`CHARACTER VARYING(20)`).
2. Add integration tests for Flyway and Liquibase migration path detection (end-to-end parse + metadata).
3. Add a SQLite-specific fixture.
