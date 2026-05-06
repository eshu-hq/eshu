package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathSQLProceduralBodiesCaptureReferences(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "procedural.sql")
	writeTestFile(
		t,
		filePath,
		`CREATE OR REPLACE FUNCTION public.sync_user_segment() RETURNS trigger AS $$
BEGIN
  UPDATE public.users u
  SET segment = s.segment
  FROM public.segments s
  WHERE s.user_id = NEW.id AND u.id = NEW.id;
  RETURN NEW;
EXCEPTION
  WHEN OTHERS THEN
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER users_sync_segment
AFTER UPDATE ON public.users
FOR EACH ROW EXECUTE PROCEDURE public.sync_user_segment();
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

	assertNamedBucketContains(t, got, "sql_functions", "public.sync_user_segment")
	assertNamedBucketContains(t, got, "sql_triggers", "users_sync_segment")
	assertSQLRelationship(t, got, "READS_FROM", "public.sync_user_segment", "public.users")
	assertSQLRelationship(t, got, "READS_FROM", "public.sync_user_segment", "public.segments")
	assertSQLRelationship(t, got, "TRIGGERS_ON", "users_sync_segment", "public.users")
	assertSQLRelationship(t, got, "EXECUTES", "users_sync_segment", "public.sync_user_segment")
}

func TestDefaultEngineParsePathSQLFixtureAlterTableAddColumn(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("ecosystems", "sql_comprehensive")
	filePath := repoFixturePath("ecosystems", "sql_comprehensive", "migrations", "V1__bootstrap.sql")

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNamedBucketContains(t, got, "sql_columns", "public.users.created_at")
	assertSQLRelationship(t, got, "HAS_COLUMN", "public.users", "public.users.created_at")
}

func TestDetectSQLMigrationTool(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		path string
		want string
	}{
		{
			name: "flyway",
			path: "/repo/sql/V42__backfill_orders.sql",
			want: "flyway",
		},
		{
			name: "prisma",
			path: "/repo/prisma/migrations/20260414_add_orders/migration.sql",
			want: "prisma",
		},
		{
			name: "liquibase changelog",
			path: "/repo/liquibase/changelog/20260414_add_orders.sql",
			want: "liquibase",
		},
		{
			name: "golang migrate",
			path: "/repo/migrations/202604140101_add_orders.up.sql",
			want: "golang-migrate",
		},
		{
			name: "generic migrations",
			path: "/repo/migrations/20260414_add_orders.sql",
			want: "generic",
		},
		{
			name: "unknown",
			path: "/repo/sql/orders.sql",
			want: "",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := detectSQLMigrationTool(testCase.path); got != testCase.want {
				t.Fatalf("detectSQLMigrationTool(%q) = %q, want %q", testCase.path, got, testCase.want)
			}
		})
	}
}
