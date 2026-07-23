// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sql

import (
	"fmt"
	"path/filepath"
	"reflect"
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

// TestGoldenCorpusDropMigrationRecordsBothTargets exercises the exact static
// fixture that B-7 copies into sql_comprehensive. Its one-line IF EXISTS comma
// list must retain both targets, not only the grammar production's one parsed
// object_reference (#5482).
func TestGoldenCorpusDropMigrationRecordsBothTargets(t *testing.T) {
	t.Parallel()

	path := filepath.Join(
		"..", "..", "..", "..",
		"tests", "fixtures", "ecosystems", "sql_comprehensive", "migrations",
		"V2__drop_legacy_tables.sql",
	)
	targets := sqlMigrationTargetsForTool(t, parseSQLTestFile(t, path), "flyway")
	if got, want := len(targets), 2; got != want {
		t.Fatalf("B-7 DROP migration target count = %d, want %d: %#v", got, want, targets)
	}
	payload := map[string]any{"sql_migrations": []map[string]any{{"tool": "flyway", "migration_targets": targets}}}
	assertSQLMigrationTarget(t, payload, "flyway", "SqlTable", "public.users", "drop", 2)
	assertSQLMigrationTarget(t, payload, "flyway", "SqlTable", "public.orgs", "drop", 2)
}

func TestParseDropTargetTailAcceptsCompleteCommaList(t *testing.T) {
	t.Parallel()

	targets, ok := parseDropTargetTail(", \"audit\".\"old-users\", legacy.events CASCADE;")
	if !ok {
		t.Fatal("parseDropTargetTail() rejected a complete comma list")
	}
	if got, want := targets, []recoveredDropTarget{
		{name: "audit.old-users", offset: 2},
		{name: "legacy.events", offset: 23},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("parseDropTargetTail() = %#v, want %#v", got, want)
	}
}

func TestParseDropTargetTailAcceptsCommentsAndRestrict(t *testing.T) {
	t.Parallel()

	tail := ", public.users /* retained */, -- between targets\n public.orgs RESTRICT;"
	targets, ok := parseDropTargetTail(tail)
	if !ok {
		t.Fatal("parseDropTargetTail() rejected comments and RESTRICT")
	}
	if got, want := targets, []recoveredDropTarget{
		{name: "public.users", offset: strings.Index(tail, "public.users")},
		{name: "public.orgs", offset: strings.Index(tail, "public.orgs")},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("parseDropTargetTail() = %#v, want %#v", got, want)
	}
}

func TestParseDropTargetTailRejectsMalformedTrailingSQL(t *testing.T) {
	t.Parallel()

	if _, ok := parseDropTargetTail(", public.orgs SELECT * FROM public.users;"); ok {
		t.Fatal("parseDropTargetTail() accepted malformed trailing SQL")
	}
	if _, ok := parseDropTargetTail(", public.orgs /* unterminated"); ok {
		t.Fatal("parseDropTargetTail() accepted an unterminated block comment")
	}
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

// TestParseMigrationTargetsFromMalformedDropErrorRejectsRecoveredTarget proves
// that tree-sitter ERROR recovery cannot mint a migration target. The grammar
// exposes public.users beneath a direct ERROR child whose source span also
// contains public.orgs, but the missing comma invalidates the complete DROP
// target statement. No recovered reference or apparently valid final target
// may survive that malformed input.
func TestParseMigrationTargetsFromMalformedDropErrorRejectsRecoveredTarget(t *testing.T) {
	t.Parallel()

	path := writeSQLTestFile(
		t,
		filepath.Join("prisma", "migrations", "20260722_drop_malformed", "migration.sql"),
		"DROP TABLE public.users public.orgs, public.events;\n",
	)

	got := parseSQLTestFile(t, path)
	assertSQLMigrationTargetMissing(t, got, "prisma", "SqlTable", "public.users")
	assertSQLMigrationTargetMissing(t, got, "prisma", "SqlTable", "public.orgs")
	assertSQLMigrationTargetMissing(t, got, "prisma", "SqlTable", "public.events")
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
