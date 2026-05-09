package sql

import (
	"os"
	"path/filepath"
	"testing"
)

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

func parseSQLTestFile(t *testing.T, path string) map[string]any {
	t.Helper()

	got, err := Parse(path, false, Options{IndexSource: true})
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
