// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sql

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseMigrationTargetsFromCommaSeparatedDropTable(t *testing.T) {
	t.Parallel()

	path := writeSQLTestFile(
		t,
		filepath.Join("prisma", "migrations", "20260722_drop_tables", "migration.sql"),
		`DROP TABLE IF EXISTS
  "audit"."old-users",
  public.accounts,
  legacy.events
CASCADE;
`,
	)

	got := parseSQLTestFile(t, path)

	assertSQLMigrationTarget(t, got, "prisma", "SqlTable", "audit.old-users", "drop", 2)
	assertSQLMigrationTarget(t, got, "prisma", "SqlTable", "public.accounts", "drop", 3)
	assertSQLMigrationTarget(t, got, "prisma", "SqlTable", "legacy.events", "drop", 4)
	assertSQLBucketMissingName(t, got, "sql_tables", "audit.old-users")
	assertSQLBucketMissingName(t, got, "sql_tables", "public.accounts")
	assertSQLBucketMissingName(t, got, "sql_tables", "legacy.events")
}

func TestParseMigrationTargetsFromDropTableDeduplicatesNames(t *testing.T) {
	t.Parallel()

	path := writeSQLTestFile(
		t,
		filepath.Join("prisma", "migrations", "20260722_drop_duplicate", "migration.sql"),
		`DROP TABLE
  public.users,
  public.users;
`,
	)

	got := parseSQLTestFile(t, path)
	targets := sqlMigrationTargetsForTool(t, got, "prisma")
	if gotCount := countSQLMigrationTargets(targets, "SqlTable", "public.users", "drop"); gotCount != 1 {
		t.Fatalf("public.users DROP target count = %d, want 1 in %#v", gotCount, targets)
	}
	assertSQLMigrationTarget(t, got, "prisma", "SqlTable", "public.users", "drop", 2)
}

func TestParseMigrationTargetsFromDropTableHonorsTargetCap(t *testing.T) {
	t.Parallel()

	targetNames := make([]string, 0, sqlMigrationTargetsCap+6)
	for index := 0; index < sqlMigrationTargetsCap+6; index++ {
		targetNames = append(targetNames, fmt.Sprintf("public.archived_%02d", index))
	}
	path := writeSQLTestFile(
		t,
		filepath.Join("prisma", "migrations", "20260722_drop_many", "migration.sql"),
		"DROP TABLE "+strings.Join(targetNames, ", ")+";\n",
	)

	got := parseSQLTestFile(t, path)
	targets := sqlMigrationTargetsForTool(t, got, "prisma")
	if gotCount := len(targets); gotCount != sqlMigrationTargetsCap {
		t.Fatalf("DROP migration target count = %d, want cap %d", gotCount, sqlMigrationTargetsCap)
	}
	assertSQLMigrationTarget(t, got, "prisma", "SqlTable", "public.archived_00", "drop", 1)
	assertSQLMigrationTarget(t, got, "prisma", "SqlTable", "public.archived_63", "drop", 1)
	assertSQLMigrationTargetMissing(t, got, "prisma", "SqlTable", "public.archived_64")
}

func sqlMigrationTargetsForTool(t *testing.T, payload map[string]any, tool string) []map[string]any {
	t.Helper()

	items, _ := payload["sql_migrations"].([]map[string]any)
	for _, item := range items {
		if gotTool, _ := item["tool"].(string); gotTool == tool {
			targets, _ := item["migration_targets"].([]map[string]any)
			return targets
		}
	}
	t.Fatalf("sql_migrations missing tool=%q in %#v", tool, items)
	return nil
}

func countSQLMigrationTargets(targets []map[string]any, kind, name, operation string) int {
	count := 0
	for _, target := range targets {
		if target["kind"] == kind && target["name"] == name && target["operation"] == operation {
			count++
		}
	}
	return count
}
