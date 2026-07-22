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
	assertSQLBucketStringSlice(t, got, "sql_tables", "public.users", "referenced_tables",
		[]string{"public.orgs"})
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
	// public.user_archive is the INSERT target, not a read; a write must never
	// be stamped as a READS_FROM edge (#5345).
	assertSQLRelationshipMissing(t, got, "READS_FROM", "public.archive_users", "public.user_archive")
	assertSQLRelationshipExists(t, got, "READS_FROM", "public.archive_users", "public.users")
	assertSQLBucketStringSlice(t, got, "sql_functions", "public.archive_users", "write_tables",
		[]string{"public.user_archive", "public.users"})
	assertSQLRelationshipExists(t, got, "WRITES_TO", "public.archive_users", "public.user_archive")
	assertSQLRelationshipExists(t, got, "WRITES_TO", "public.archive_users", "public.users")
	assertSQLMigrationExists(t, got, "prisma", "SqlTable", "public.user_archive")
	// Prisma names every migration file "migration.sql"; the stamped identifier
	// must be the migration's parent directory name instead (#5346).
	assertSQLMigrationEntityName(t, got, "prisma", "20260509_sql_contract")
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

// TestParseViewAndFunctionStampSourceTablesMetadata guards #5345: the view and
// function entity items must carry entity_metadata.source_tables (the bridge
// the reducer's view/function READS_FROM derivation keys on), deduped and
// sorted, restricted to "select" mentions. A function whose body writes to a
// table via INSERT must not stamp that write target as a read.
func TestParseViewAndFunctionStampSourceTablesMetadata(t *testing.T) {
	t.Parallel()

	path := writeSQLTestFile(t, "views_and_functions.sql", `CREATE TABLE public.users (
  id BIGSERIAL PRIMARY KEY
);

CREATE TABLE public.orders (
  id BIGSERIAL PRIMARY KEY
);

CREATE OR REPLACE VIEW public.recent_orders AS
SELECT o.id
FROM public.orders o
JOIN public.users u ON u.id = o.id;

CREATE OR REPLACE FUNCTION public.archive_orders() RETURNS void
LANGUAGE plpgsql
AS $$
BEGIN
  INSERT INTO public.order_archive
  SELECT id FROM public.orders;
END;
$$;
`)

	got := parseSQLTestFile(t, path)

	assertSQLBucketStringSlice(t, got, "sql_views", "public.recent_orders", "source_tables",
		[]string{"public.orders", "public.users"})
	assertSQLBucketStringSlice(t, got, "sql_functions", "public.archive_orders", "source_tables",
		[]string{"public.orders"})
	assertSQLBucketStringSlice(t, got, "sql_functions", "public.archive_orders", "write_tables",
		[]string{"public.order_archive"})
	assertSQLRelationshipMissing(t, got, "READS_FROM", "public.archive_orders", "public.order_archive")
	assertSQLRelationshipExists(t, got, "WRITES_TO", "public.archive_orders", "public.order_archive")
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

// assertSQLBucketStringSlice asserts the named entity in bucket carries the
// exact (order-sensitive) string slice under key.
func assertSQLBucketStringSlice(
	t *testing.T,
	payload map[string]any,
	bucket string,
	name string,
	key string,
	want []string,
) {
	t.Helper()

	items, _ := payload[bucket].([]map[string]any)
	for _, item := range items {
		gotName, _ := item["name"].(string)
		if gotName != name {
			continue
		}
		got, _ := item[key].([]string)
		if len(got) != len(want) {
			t.Fatalf("%s[%q][%q] = %#v, want %#v", bucket, name, key, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s[%q][%q] = %#v, want %#v", bucket, name, key, got, want)
			}
		}
		return
	}
	t.Fatalf("%s missing name %q in %#v", bucket, name, items)
}

// assertSQLRelationshipMissing asserts no relationship of relationshipType
// from sourceName to targetName was emitted.
func assertSQLRelationshipMissing(
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
			t.Fatalf(
				"sql_relationships unexpectedly contained type=%q source_name=%q target_name=%q",
				relationshipType,
				sourceName,
				targetName,
			)
		}
	}
}
