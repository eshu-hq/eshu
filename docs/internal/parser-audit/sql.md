# SQL Parser Audit

## Overview
The SQL parser (`go/internal/parser/sql/`) extracts schema objects, columns, routines, triggers, indexes, and relationships from SQL DDL source using a tree-sitter grammar. It segments source into statement-sized fragments for fault tolerance, rewrites `CREATE PROCEDURE` to `CREATE FUNCTION` for MSSQL T-SQL support, normalizes dialect-specific quote characters, and emits migration metadata for Prisma, Flyway, Liquibase, and golang-migrate projects. The package has 2 subdirectory test files plus 7 parent-level engine SQL tests.

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
16. **CREATE OR REPLACE PROCEDURE parsing** — `sql/language_test.go:50-97` (`TestParseRoutineViewAndMigrationReferences`)
17. **Procedure IndexSource preserves original text** — `sql/language_test.go:102-126` (`TestParseProcedureIndexedSourceIsOriginalText`)
18. **Migration targets from ALTER and REFERENCES** — `sql/language_test.go:131-153` (`TestParseMigrationTargetsFromAlterAndReferences`)
19. **Migration tool detection** — `sql/migrations_test.go:8-59` (Prisma, Flyway, Liquibase, golang-migrate, generic, negative)
20. **Performance benchmark** — `sql/language_bench_test.go` (`BenchmarkParseComprehensive`)

## Unverified / Claimed-but-Untested Constructs
List constructs claimed but not covered by any test.

1. **DELETE statement table mention** (`shared.go:95-95`): no test verifies that `DELETE FROM` produces a table mention in a migration context.
2. **INSERT statement table mention** (`shared.go:88-89`): no test verifies `INSERT INTO` table mentions.
3. **UPDATE statement table mention** (`shared.go:90-93`): no test verifies `UPDATE ... SET` table mentions.
4. **MSSQL bracket identifier normalization** (`shared.go:37-38`, `[...]`): no test with `[dbo].[Users]` syntax.
5. **Flyway migration filename detection** (`migrations.go:24`, `V<n>__*.sql`): tested via `detectSQLMigrationTool` but not integrated with a Flyway-migration path parse.
6. **Liquibase migration path detection** (`migrations.go:19-20`): same as above — classified but not integrated.
7. **`golang-migrate` path detection** (`migrations.go:21`): classified but not integrated.
8. **Routine `LANGUAGE` clause parsing** (`nodes.go:107-117`): not tested with a function declaring `LANGUAGE sql`.
9. **Schema-qualified trigger names** (`shared.go:28-44`): tested with `public.users` schema but not with `public."orgs"` double-quoted schema.
10. **Dollar-quote body with tag** (`$proc$...$proc$`, `segments.go:181-185`): tested implicitly through procedure test, but the tag variant is not isolated.
11. **Column `data_type` multi-token spelling** (`nodes.go:16-42`): `CHARACTER VARYING(20)` not tested; only `BIGSERIAL`, `UUID`, `TEXT`, `INT`, `TIMESTAMPTZ` tests exist.
12. **Commented-out SQL inside segments** (`segments.go:47-48`): line and block comment skipping not tested.

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
- **Deduplication of entities and relationships** — tested at scale via comprehensive fixture test
- **Performance benchmark comparing regex vs tree-sitter** — `sql/language_bench_test.go`

## Edge Cases NOT Considered
List edge cases not tested.

- **MySQL backtick-quoted identifiers** — no MySQL-specific SQL fixture
- **MSSQL bracket identifiers** — no T-SQL `[dbo].[...]` fixture
- **SQLite bare identifiers** — no SQLite-specific fixture
- **Routine with `LANGUAGE` clause** — not explicitly tested
- **`DELETE`, `INSERT`, `UPDATE` DML statements in migration files** — no integration test with these DML types as migration targets
- **Very large SQL files with 100+ segments**
- **Embedded `;` inside a string literal in a segment** — segmenter skips strings, but not tested with `';'`
- **Dollar-quote body with embedded `$$`** (should be handled by correct tag matching, but not tested)
- **`CREATE TABLE ... LIKE ...` or `CREATE TABLE ... AS SELECT ...`** — not tested
- **`DROP TABLE`, `DROP VIEW`, `DROP INDEX` statements** — not extracted per contract, but not tested as pass-through
- **UNICODE identifiers** (non-ASCII table/column names)
- **Concurrent parsing of the same file** (thread safety for parser reuse)

## Verdict
moderate

The SQL parser has robust parent-level engine tests (7 tests) covering core entity extraction, all 6 relationship types, migrations, materialized views, procedures, ALTER TABLE, and partial recovery. Subdirectory tests add constraint/column separation, procedure IndexSource preservation, and tool detection. However, dialect-specific fixtures (MySQL backticks, MSSQL brackets) are untested, DML operation mentions (DELETE/INSERT/UPDATE) lack integration tests, and the `LANGUAGE` clause, multi-token data types, and Flyway/Liquibase migration path integration remain unverified.

## Recommended Actions
1. Add a MySQL-specific test fixture with backtick-quoted identifiers.
2. Add a MSSQL bracket-identifier test fixture.
3. Add integration tests for DML table mentions (DELETE, INSERT, UPDATE) producing migration targets.
4. Add a test for `LANGUAGE` clause extraction on routines.
5. Add a test for multi-word data types (`CHARACTER VARYING(20)`).
6. Add integration tests for Flyway and Liquibase migration path detection (end-to-end parse + metadata).
