// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sql_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathSQLProceduralBodiesCaptureReferences(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "procedural.sql")
	parsertest.WriteFile(
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertNamedBucketContains(t, got, "sql_functions", "public.sync_user_segment")
	parsertest.AssertNamedBucketContains(t, got, "sql_triggers", "users_sync_segment")
	// public.users is the UPDATE target (UPDATE public.users ... FROM
	// public.segments), a write — it must NOT be a READS_FROM edge even though
	// the generic relation walk shadow-tags it "select" at the update target's
	// offset (#5345, codex P1). public.segments IS a genuine read via the FROM
	// clause and keeps its READS_FROM.
	assertSQLRelationshipMissing(t, got, "READS_FROM", "public.sync_user_segment", "public.users")
	assertSQLRelationship(t, got, "READS_FROM", "public.sync_user_segment", "public.segments")
	assertSQLRelationship(t, got, "TRIGGERS_ON", "users_sync_segment", "public.users")
	assertSQLRelationship(t, got, "EXECUTES", "users_sync_segment", "public.sync_user_segment")
}

func TestDefaultEngineParsePathSQLFixtureAlterTableAddColumn(t *testing.T) {
	t.Parallel()

	repoRoot := sqlFixturePath(t, "ecosystems", "sql_comprehensive")
	filePath := sqlFixturePath(t, "ecosystems", "sql_comprehensive", "migrations", "V1__bootstrap.sql")

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertNamedBucketContains(t, got, "sql_columns", "public.users.created_at")
	assertSQLRelationship(t, got, "HAS_COLUMN", "public.users", "public.users.created_at")
}
