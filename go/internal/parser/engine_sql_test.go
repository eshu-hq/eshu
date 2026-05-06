package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathSQLSchemaObjectsAndRelationships(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "schema.sql")
	writeTestFile(
		t,
		filePath,
		`CREATE TABLE public.orgs (
  id UUID PRIMARY KEY
);

CREATE TABLE public.users (
  id BIGSERIAL PRIMARY KEY,
  org_id UUID REFERENCES public.orgs(id),
  email TEXT NOT NULL,
  CONSTRAINT fk_org FOREIGN KEY (org_id) REFERENCES public.orgs(id)
);

CREATE VIEW public.active_users AS
SELECT u.id, u.email
FROM public.users u
JOIN public.orgs o ON o.id = u.org_id;

CREATE FUNCTION public.touch_updated_at() RETURNS trigger AS $$
BEGIN
  UPDATE public.users SET email = email WHERE id = NEW.id;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER users_touch BEFORE UPDATE ON public.users
FOR EACH ROW EXECUTE FUNCTION public.touch_updated_at();

CREATE INDEX idx_users_org_id ON public.users (org_id);
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if got["path"] != filePath {
		t.Fatalf("path = %#v, want %#v", got["path"], filePath)
	}
	if got["lang"] != "sql" {
		t.Fatalf("lang = %#v, want %#v", got["lang"], "sql")
	}
	if got["is_dependency"] != false {
		t.Fatalf("is_dependency = %#v, want %#v", got["is_dependency"], false)
	}

	assertNamedBucketContains(t, got, "sql_tables", "public.orgs")
	assertNamedBucketContains(t, got, "sql_tables", "public.users")
	assertNamedBucketContains(t, got, "sql_views", "public.active_users")
	assertNamedBucketContains(t, got, "sql_functions", "public.touch_updated_at")
	assertNamedBucketContains(t, got, "sql_triggers", "users_touch")
	assertNamedBucketContains(t, got, "sql_indexes", "idx_users_org_id")

	assertNamedBucketContains(t, got, "sql_columns", "public.orgs.id")
	assertNamedBucketContains(t, got, "sql_columns", "public.users.id")
	assertNamedBucketContains(t, got, "sql_columns", "public.users.org_id")
	assertNamedBucketContains(t, got, "sql_columns", "public.users.email")

	assertSQLRelationship(t, got, "HAS_COLUMN", "public.users", "public.users.org_id")
	assertSQLRelationship(t, got, "REFERENCES_TABLE", "public.users", "public.orgs")
	assertSQLRelationship(t, got, "READS_FROM", "public.active_users", "public.users")
	assertSQLRelationship(t, got, "TRIGGERS_ON", "users_touch", "public.users")
	assertSQLRelationship(t, got, "EXECUTES", "users_touch", "public.touch_updated_at")
	assertSQLRelationship(t, got, "INDEXES", "idx_users_org_id", "public.users")
}

func TestDefaultEngineParsePathSQLMigrationMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(
		repoRoot,
		"prisma",
		"migrations",
		"20260411_add_users",
		"migration.sql",
	)
	writeTestFile(
		t,
		filePath,
		`CREATE TABLE public.users (
  id BIGSERIAL PRIMARY KEY
);

ALTER TABLE public.users ADD COLUMN email TEXT;
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	items, ok := got["sql_migrations"].([]map[string]any)
	if !ok {
		t.Fatalf("sql_migrations = %T, want []map[string]any", got["sql_migrations"])
	}
	if len(items) != 1 {
		t.Fatalf("len(sql_migrations) = %d, want 1", len(items))
	}
	if items[0]["tool"] != "prisma" {
		t.Fatalf("sql_migrations[0].tool = %#v, want %#v", items[0]["tool"], "prisma")
	}
	if items[0]["target_kind"] != "SqlTable" {
		t.Fatalf("sql_migrations[0].target_kind = %#v, want %#v", items[0]["target_kind"], "SqlTable")
	}
	if items[0]["target_name"] != "public.users" {
		t.Fatalf("sql_migrations[0].target_name = %#v, want %#v", items[0]["target_name"], "public.users")
	}
}

func TestDefaultEngineParsePathSQLCreateOrReplaceView(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "views.sql")
	writeTestFile(
		t,
		filePath,
		`CREATE OR REPLACE VIEW public.active_users AS
SELECT u.id, u.email
FROM public.users u
JOIN public.orgs o ON o.id = u.org_id;
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNamedBucketContains(t, got, "sql_views", "public.active_users")
	assertSQLRelationship(t, got, "READS_FROM", "public.active_users", "public.users")
}

func TestDefaultEngineParsePathSQLAlterTableAddColumnMaterializesColumn(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "migrations", "V2__add_email.sql")
	writeTestFile(
		t,
		filePath,
		`CREATE TABLE public.users (
  id BIGSERIAL PRIMARY KEY
);

ALTER TABLE public.users ADD COLUMN email TEXT;
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNamedBucketContains(t, got, "sql_columns", "public.users.email")
	assertSQLRelationship(t, got, "HAS_COLUMN", "public.users", "public.users.email")
}

func TestDefaultEngineParsePathSQLAlterTableNormalizesMultipleAddColumnClauses(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "migrations", "V3__expand_users.sql")
	writeTestFile(
		t,
		filePath,
		`CREATE TABLE public.users (
  id BIGSERIAL PRIMARY KEY
);

ALTER TABLE public.users
  ADD COLUMN IF NOT EXISTS email TEXT,
  ADD COLUMN created_at TIMESTAMP;
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNamedBucketContains(t, got, "sql_columns", "public.users.email")
	assertNamedBucketContains(t, got, "sql_columns", "public.users.created_at")
	assertSQLRelationship(t, got, "HAS_COLUMN", "public.users", "public.users.email")
	assertSQLRelationship(t, got, "HAS_COLUMN", "public.users", "public.users.created_at")
}

func TestDefaultEngineParsePathSQLMaterializedViewsAndProcedures(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "analytics.sql")
	writeTestFile(
		t,
		filePath,
		`CREATE MATERIALIZED VIEW public.active_users AS
SELECT u.id
FROM public.users u;

CREATE PROCEDURE public.refresh_users()
LANGUAGE plpgsql
AS $$
BEGIN
  UPDATE public.users
  SET refreshed_at = NOW();
END;
$$;
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	view := assertBucketItemByName(t, got, "sql_views", "public.active_users")
	assertStringFieldValue(t, view, "view_kind", "materialized")
	assertSQLRelationship(t, got, "READS_FROM", "public.active_users", "public.users")

	procedure := assertBucketItemByName(t, got, "sql_functions", "public.refresh_users")
	assertStringFieldValue(t, procedure, "routine_kind", "procedure")
	assertSQLRelationship(t, got, "READS_FROM", "public.refresh_users", "public.users")
}

func TestDefaultEngineParsePathSQLPartialRecovery(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "broken.sql")
	writeTestFile(
		t,
		filePath,
		`CREATE TABLE public.users (
  id BIGSERIAL PRIMARY KEY,
  email TEXT

CREATE VIEW public.active_users AS
SELECT id FROM public.users;
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNamedBucketContains(t, got, "sql_tables", "public.users")
	assertNamedBucketContains(t, got, "sql_views", "public.active_users")
	assertSQLRelationship(t, got, "READS_FROM", "public.active_users", "public.users")
}

func assertSQLRelationship(
	t *testing.T,
	payload map[string]any,
	relationshipType string,
	sourceName string,
	targetName string,
) {
	t.Helper()

	items, ok := payload["sql_relationships"].([]map[string]any)
	if !ok {
		t.Fatalf("sql_relationships = %T, want []map[string]any", payload["sql_relationships"])
	}
	for _, item := range items {
		itemType, _ := item["type"].(string)
		itemSource, _ := item["source_name"].(string)
		itemTarget, _ := item["target_name"].(string)
		if itemType == relationshipType && itemSource == sourceName && itemTarget == targetName {
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
