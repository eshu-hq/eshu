// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sql

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tree_sitter_sql "github.com/alexaandru/go-sitter-forest/sql"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func newSQLTestParser(t *testing.T) *tree_sitter.Parser {
	t.Helper()

	parser := tree_sitter.NewParser()
	t.Cleanup(parser.Close)
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_sql.GetLanguage())); err != nil {
		t.Fatalf("SetLanguage() error = %v, want nil", err)
	}
	return parser
}

func TestParseDoesNotMaterializeTableConstraintsAsColumns(t *testing.T) {
	t.Parallel()

	path := writeSQLTestFile(t, "schema.sql", `CREATE TABLE public.users (
  id BIGSERIAL NOT NULL,
  org_id UUID NOT NULL,
  email TEXT NOT NULL,
  PRIMARY KEY (id),
  FOREIGN KEY (org_id) REFERENCES public.orgs(id),
  CONSTRAINT users_email_unique UNIQUE (email)
);
`)

	got := parseSQLTestFile(t, path)

	assertSQLBucketContainsName(t, got, "sql_columns", "public.users.id")
	assertSQLBucketContainsName(t, got, "sql_columns", "public.users.org_id")
	assertSQLBucketContainsName(t, got, "sql_columns", "public.users.email")
	assertSQLBucketMissingName(t, got, "sql_columns", "public.users.PRIMARY")
	assertSQLBucketMissingName(t, got, "sql_columns", "public.users.FOREIGN")
	assertSQLRelationshipExists(t, got, "REFERENCES_TABLE", "public.users", "public.orgs")
}

func TestParseRoutineViewAndMigrationReferences(t *testing.T) {
	t.Parallel()

	path := writeSQLTestFile(
		t,
		filepath.Join("prisma", "migrations", "20260509_sql_contract", "migration.sql"),
		`CREATE TABLE public.users (
  id BIGSERIAL PRIMARY KEY
);

CREATE TABLE public.user_archive (
  id BIGSERIAL PRIMARY KEY
);

CREATE OR REPLACE VIEW public.active_users AS
SELECT id
FROM public.users;

CREATE OR REPLACE FUNCTION public.count_users() RETURNS integer
LANGUAGE sql
AS $$
  SELECT count(*) FROM public.users;
$$;

CREATE OR REPLACE PROCEDURE public.archive_users()
LANGUAGE plpgsql
AS $proc$
BEGIN
  INSERT INTO public.user_archive
  SELECT id FROM public.users;
  DELETE FROM public.users WHERE archived = TRUE;
END;
$proc$;
`,
	)

	got := parseSQLTestFile(t, path)

	assertSQLBucketContainsName(t, got, "sql_tables", "public.users")
	assertSQLBucketContainsName(t, got, "sql_views", "public.active_users")
	assertSQLBucketContainsName(t, got, "sql_functions", "public.count_users")
	assertSQLBucketContainsName(t, got, "sql_functions", "public.archive_users")
	assertSQLRelationshipExists(t, got, "READS_FROM", "public.active_users", "public.users")
	assertSQLRelationshipExists(t, got, "READS_FROM", "public.count_users", "public.users")
	assertSQLRelationshipExists(t, got, "READS_FROM", "public.archive_users", "public.user_archive")
	assertSQLRelationshipExists(t, got, "READS_FROM", "public.archive_users", "public.users")
	assertSQLMigrationExists(t, got, "prisma", "SqlTable", "public.user_archive")
}

// TestParseProcedureIndexedSourceIsOriginalText guards that, when IndexSource is
// on, the persisted source snippet for a CREATE PROCEDURE is the real procedure
// text, not the synthetic CREATE FUNCTION ... RETURNS void the grammar parses.
func TestParseProcedureIndexedSourceIsOriginalText(t *testing.T) {
	t.Parallel()

	path := writeSQLTestFile(t, "procedure.sql", `CREATE OR REPLACE PROCEDURE public.archive_users(retention INT)
LANGUAGE plpgsql
AS $proc$
BEGIN
  DELETE FROM public.users WHERE archived = TRUE;
END;
$proc$;
`)

	got := parseSQLTestFile(t, path)

	source := sqlBucketSource(got, "sql_functions", "public.archive_users")
	if source == "" {
		t.Fatalf("sql_functions missing public.archive_users with source in %#v", got["sql_functions"])
	}
	if !strings.Contains(source, "PROCEDURE") {
		t.Fatalf("indexed source must keep the original PROCEDURE text, got %q", source)
	}
	if strings.Contains(source, "RETURNS void") {
		t.Fatalf("indexed source must not contain the synthetic rewrite, got %q", source)
	}
}

// TestParseMigrationTargetsFromAlterAndReferences guards that migration metadata
// records tables that a migration only touches via ALTER TABLE or a REFERENCES
// (foreign key) clause, not just tables reached through DML reads.
func TestParseMigrationTargetsFromAlterAndReferences(t *testing.T) {
	t.Parallel()

	alterPath := writeSQLTestFile(
		t,
		filepath.Join("prisma", "migrations", "20260620_alter_only", "migration.sql"),
		`ALTER TABLE public.existing_orders ADD COLUMN shipped_at TIMESTAMPTZ;
`,
	)
	gotAlter := parseSQLTestFile(t, alterPath)
	assertSQLMigrationExists(t, gotAlter, "prisma", "SqlTable", "public.existing_orders")

	referencesPath := writeSQLTestFile(
		t,
		filepath.Join("prisma", "migrations", "20260621_fk_only", "migration.sql"),
		`ALTER TABLE public.line_items
  ADD CONSTRAINT line_items_order_fk
  FOREIGN KEY (order_id) REFERENCES public.orders (id);
`,
	)
	gotReferences := parseSQLTestFile(t, referencesPath)
	assertSQLMigrationExists(t, gotReferences, "prisma", "SqlTable", "public.orders")
}

func TestParseMySQLBacktickQuotedIdentifiers(t *testing.T) {
	t.Parallel()

	path := writeSQLTestFile(t, "backtick.sql", "CREATE TABLE `my_schema`.`users` (\n  `id` BIGINT NOT NULL,\n  `full_name` TEXT NOT NULL\n);\n")

	got := parseSQLTestFile(t, path)

	assertSQLBucketContainsName(t, got, "sql_tables", "my_schema.users")
	assertSQLBucketContainsName(t, got, "sql_columns", "my_schema.users.id")
	assertSQLBucketContainsName(t, got, "sql_columns", "my_schema.users.full_name")
}

func TestParseMSSQLBracketQuotedIdentifiers(t *testing.T) {
	t.Parallel()

	path := writeSQLTestFile(t, "bracket.sql", "CREATE TABLE [dbo].[Orders] (\n  [OrderID] INT NOT NULL,\n  [CustomerName] NVARCHAR(100) NOT NULL\n);\n")

	got := parseSQLTestFile(t, path)

	assertSQLBucketContainsName(t, got, "sql_tables", "dbo.Orders")
	assertSQLBucketContainsName(t, got, "sql_columns", "dbo.Orders.OrderID")
	assertSQLBucketContainsName(t, got, "sql_columns", "dbo.Orders.CustomerName")
}

func TestParseDMLDeleteMigrationTarget(t *testing.T) {
	t.Parallel()

	path := writeSQLTestFile(
		t,
		filepath.Join("prisma", "migrations", "20260622_delete", "migration.sql"),
		`DELETE FROM public.old_records WHERE created_at < '2020-01-01';
`,
	)

	got := parseSQLTestFile(t, path)

	assertSQLMigrationExists(t, got, "prisma", "SqlTable", "public.old_records")
}

func TestParseDMLInsertMigrationTarget(t *testing.T) {
	t.Parallel()

	path := writeSQLTestFile(
		t,
		filepath.Join("prisma", "migrations", "20260623_insert", "migration.sql"),
		`INSERT INTO public.audit_log (event, data) VALUES ('migration', '{}');
`,
	)

	got := parseSQLTestFile(t, path)

	assertSQLMigrationExists(t, got, "prisma", "SqlTable", "public.audit_log")
}

func TestParseDMLUpdateMigrationTarget(t *testing.T) {
	t.Parallel()

	path := writeSQLTestFile(
		t,
		filepath.Join("prisma", "migrations", "20260624_update", "migration.sql"),
		`UPDATE public.accounts SET active = TRUE WHERE status = 'pending';
`,
	)

	got := parseSQLTestFile(t, path)

	assertSQLMigrationExists(t, got, "prisma", "SqlTable", "public.accounts")
}

func TestSQLLineIndexMatchesLineNumberForOffsets(t *testing.T) {
	t.Parallel()

	source := []byte("CREATE TABLE a (\n  id INT,\n  name TEXT\n);\n")
	index := newSQLLineIndex(source)

	for _, tc := range []struct {
		name   string
		offset int
		want   int
	}{
		{name: "start", offset: 0, want: 1},
		{name: "second-line", offset: bytes.Index(source, []byte("id")), want: 2},
		{name: "third-line", offset: bytes.Index(source, []byte("name")), want: 3},
		{name: "after-end", offset: len(source) + 10, want: 5},
		{name: "before-start", offset: -10, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := index.lineForOffset(tc.offset); got != tc.want {
				t.Fatalf("lineForOffset(%d) = %d, want %d", tc.offset, got, tc.want)
			}
		})
	}
}

func parseSQLTestFile(t *testing.T, path string) map[string]any {
	t.Helper()

	got, err := Parse(path, false, Options{IndexSource: true}, newSQLTestParser(t))
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	return got
}

func writeSQLTestFile(t *testing.T, relativePath string, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v, want nil", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}
	return path
}

func assertSQLBucketContainsName(t *testing.T, payload map[string]any, bucket string, name string) {
	t.Helper()

	if !sqlBucketHasName(payload, bucket, name) {
		t.Fatalf("%s missing name %q in %#v", bucket, name, payload[bucket])
	}
}

func assertSQLBucketMissingName(t *testing.T, payload map[string]any, bucket string, name string) {
	t.Helper()

	if sqlBucketHasName(payload, bucket, name) {
		t.Fatalf("%s unexpectedly contained name %q in %#v", bucket, name, payload[bucket])
	}
}

func sqlBucketHasName(payload map[string]any, bucket string, name string) bool {
	items, _ := payload[bucket].([]map[string]any)
	for _, item := range items {
		gotName, _ := item["name"].(string)
		if gotName == name {
			return true
		}
	}
	return false
}

// sqlBucketSource returns the indexed source snippet stored for a named entity,
// or "" when the entity or its source field is absent.
func sqlBucketSource(payload map[string]any, bucket string, name string) string {
	items, _ := payload[bucket].([]map[string]any)
	for _, item := range items {
		gotName, _ := item["name"].(string)
		if gotName == name {
			source, _ := item["source"].(string)
			return source
		}
	}
	return ""
}

func assertSQLRelationshipExists(
	t *testing.T,
	payload map[string]any,
	relationshipType string,
	sourceName string,
	targetName string,
) {
	t.Helper()

	items, _ := payload["sql_relationships"].([]map[string]any)
	for _, item := range items {
		gotType, _ := item["type"].(string)
		gotSource, _ := item["source_name"].(string)
		gotTarget, _ := item["target_name"].(string)
		if gotType == relationshipType && gotSource == sourceName && gotTarget == targetName {
			return
		}
	}
	t.Fatalf(
		"sql_relationships missing type=%q source_name=%q target_name=%q in %#v",
		relationshipType,
		sourceName,
		targetName,
		items,
	)
}

func assertSQLMigrationExists(
	t *testing.T,
	payload map[string]any,
	tool string,
	targetKind string,
	targetName string,
) {
	t.Helper()

	items, _ := payload["sql_migrations"].([]map[string]any)
	for _, item := range items {
		gotTool, _ := item["tool"].(string)
		gotKind, _ := item["target_kind"].(string)
		gotName, _ := item["target_name"].(string)
		if gotTool == tool && gotKind == targetKind && gotName == targetName {
			return
		}
	}
	t.Fatalf(
		"sql_migrations missing tool=%q target_kind=%q target_name=%q in %#v",
		tool,
		targetKind,
		targetName,
		items,
	)
}
