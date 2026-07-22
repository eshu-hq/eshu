// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sql

import (
	"path/filepath"
	"testing"
)

// TestParseMigrationExcludesSelectOnlyMentionsFromTargets guards #5346:
// write-not-read honesty for migration targets. A migration file whose ONLY
// touch on a table is a SELECT read (e.g. a read-only backfill migration) must
// not record that table as a migration_targets entry, mirroring
// dropShadowedReads' #5345 write-not-read guarantee for READS_FROM.
func TestParseMigrationExcludesSelectOnlyMentionsFromTargets(t *testing.T) {
	t.Parallel()

	path := writeSQLTestFile(
		t,
		filepath.Join("prisma", "migrations", "20260625_backfill", "migration.sql"),
		`INSERT INTO public.audit_log (event)
SELECT 'backfill' FROM public.legacy_orders;
`,
	)

	got := parseSQLTestFile(t, path)

	assertSQLMigrationExists(t, got, "prisma", "SqlTable", "public.audit_log")
	assertSQLMigrationTargetMissing(t, got, "prisma", "SqlTable", "public.legacy_orders")
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

// TestParseMigrationTargetsFromDropTable guards #5482: DROP TABLE is migration
// evidence for an existing table, not a declaration of a new SqlTable entity.
func TestParseMigrationTargetsFromDropTable(t *testing.T) {
	t.Parallel()

	path := writeSQLTestFile(
		t,
		filepath.Join("prisma", "migrations", "20260722_drop_users", "migration.sql"),
		`DROP TABLE IF EXISTS public.users;
`,
	)

	got := parseSQLTestFile(t, path)

	assertSQLMigrationTarget(t, got, "prisma", "SqlTable", "public.users", "drop", 1)
	assertSQLBucketMissingName(t, got, "sql_tables", "public.users")
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

// assertSQLMigrationExists asserts the file's SqlMigration entity (tool must
// match) carries a migration_targets entry for targetKind/targetName (#5346:
// buildSQLMigrationEntries emits one SqlMigration entity per file, with every
// forward target nested under its migration_targets metadata).
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
		if gotTool != tool {
			continue
		}
		targets, _ := item["migration_targets"].([]map[string]any)
		for _, target := range targets {
			gotKind, _ := target["kind"].(string)
			gotName, _ := target["name"].(string)
			if gotKind == targetKind && gotName == targetName {
				return
			}
		}
	}
	t.Fatalf(
		"sql_migrations missing tool=%q kind=%q name=%q in %#v",
		tool,
		targetKind,
		targetName,
		items,
	)
}

func assertSQLMigrationTarget(
	t *testing.T,
	payload map[string]any,
	tool string,
	targetKind string,
	targetName string,
	operation string,
	lineNumber int,
) {
	t.Helper()

	items, _ := payload["sql_migrations"].([]map[string]any)
	for _, item := range items {
		gotTool, _ := item["tool"].(string)
		if gotTool != tool {
			continue
		}
		targets, _ := item["migration_targets"].([]map[string]any)
		for _, target := range targets {
			gotKind, _ := target["kind"].(string)
			gotName, _ := target["name"].(string)
			gotOperation, _ := target["operation"].(string)
			gotLineNumber, _ := target["line_number"].(int)
			if gotKind == targetKind && gotName == targetName && gotOperation == operation && gotLineNumber == lineNumber {
				return
			}
		}
	}
	t.Fatalf(
		"sql_migrations missing tool=%q kind=%q name=%q operation=%q line_number=%d in %#v",
		tool,
		targetKind,
		targetName,
		operation,
		lineNumber,
		items,
	)
}

// assertSQLMigrationTargetMissing asserts no migration_targets entry for
// targetKind/targetName exists under the file's tool-matching SqlMigration
// entity -- used to prove a select-only mention is excluded (#5346).
func assertSQLMigrationTargetMissing(
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
		if gotTool != tool {
			continue
		}
		targets, _ := item["migration_targets"].([]map[string]any)
		for _, target := range targets {
			gotKind, _ := target["kind"].(string)
			gotName, _ := target["name"].(string)
			if gotKind == targetKind && gotName == targetName {
				t.Fatalf(
					"sql_migrations unexpectedly contains tool=%q kind=%q name=%q",
					tool, targetKind, targetName,
				)
			}
		}
	}
}

// assertSQLMigrationEntityName asserts the tool-matching SqlMigration entity's
// own name (the stamped identifier, #5346) equals want.
func assertSQLMigrationEntityName(
	t *testing.T,
	payload map[string]any,
	tool string,
	want string,
) {
	t.Helper()

	items, _ := payload["sql_migrations"].([]map[string]any)
	for _, item := range items {
		gotTool, _ := item["tool"].(string)
		if gotTool != tool {
			continue
		}
		gotName, _ := item["name"].(string)
		if gotName != want {
			t.Fatalf("sql_migrations[tool=%q].name = %q, want %q", tool, gotName, want)
		}
		return
	}
	t.Fatalf("sql_migrations missing tool=%q entry in %#v", tool, items)
}
