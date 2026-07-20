// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathSQLCoreDDLVariants(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "ddl_variants.sql")
	writeTestFile(
		t,
		filePath,
		`CREATE TABLE IF NOT EXISTS public.audit_logs (
  id BIGSERIAL PRIMARY KEY,
  user_id UUID NOT NULL
);

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_audit_logs_user_id
ON public.audit_logs (user_id);
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

	assertNamedBucketContains(t, got, "sql_tables", "public.audit_logs")
	assertNamedBucketContains(t, got, "sql_columns", "public.audit_logs.user_id")
	assertNamedBucketContains(t, got, "sql_indexes", "idx_audit_logs_user_id")
	assertSQLRelationship(t, got, "HAS_COLUMN", "public.audit_logs", "public.audit_logs.user_id")
	assertSQLRelationship(t, got, "INDEXES", "idx_audit_logs_user_id", "public.audit_logs")
}

func TestDefaultEngineParsePathSQLCoreRoutineVariants(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "routine_variants.sql")
	writeTestFile(
		t,
		filePath,
		`CREATE TABLE public.audit_logs (
  id BIGSERIAL PRIMARY KEY,
  touched_at TIMESTAMP
);

CREATE TABLE public.audit_archive (
  id BIGSERIAL PRIMARY KEY
);

CREATE OR REPLACE FUNCTION public.touch_audit() RETURNS trigger
LANGUAGE plpgsql
AS $fn$
BEGIN
  UPDATE public.audit_logs
  SET touched_at = NOW()
  WHERE id = NEW.id;
  RETURN NEW;
END;
$fn$;

CREATE OR REPLACE PROCEDURE public.archive_audit()
AS $proc$
BEGIN
  INSERT INTO public.audit_archive
  SELECT id FROM public.audit_logs;
END;
$proc$
LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION public.purge_audit() RETURNS void
LANGUAGE plpgsql
AS $purge$
BEGIN
  DELETE FROM public.audit_archive
  WHERE id < 100;
END;
$purge$;
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

	function := assertBucketItemByName(t, got, "sql_functions", "public.touch_audit")
	assertStringFieldValue(t, function, "function_language", "plpgsql")
	// public.audit_logs is the UPDATE target, not a read. The generic
	// relation-read walk tags an UPDATE target's object_reference as "select"
	// at the same offset the update clause records it as a write; that spurious
	// read must not survive, so touch_audit (whose only statement is an UPDATE)
	// has NO READS_FROM edge (#5345, codex P1).
	assertSQLRelationshipMissing(t, got, "READS_FROM", "public.touch_audit", "public.audit_logs")

	procedure := assertBucketItemByName(t, got, "sql_functions", "public.archive_audit")
	assertStringFieldValue(t, procedure, "routine_kind", "procedure")
	assertStringFieldValue(t, procedure, "function_language", "plpgsql")
	// public.audit_archive is the INSERT target, not a read; a write must
	// never be stamped as a READS_FROM edge (#5345).
	assertSQLRelationshipMissing(t, got, "READS_FROM", "public.archive_audit", "public.audit_archive")
	assertSQLRelationship(t, got, "READS_FROM", "public.archive_audit", "public.audit_logs")

	// public.purge_audit's only statement is DELETE FROM public.audit_archive —
	// a write target that must never be stamped as a read (#5345, codex P1). The
	// DELETE-target-as-select shadow is dropped like the UPDATE case above.
	assertBucketItemByName(t, got, "sql_functions", "public.purge_audit")
	assertSQLRelationshipMissing(t, got, "READS_FROM", "public.purge_audit", "public.audit_archive")
}
