// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sql

import (
	"fmt"
	"path/filepath"
	"testing"
)

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

// TestSqlMigrationIdentifier guards the #5346 identifier rule: prisma's
// migration.sql basename is worthless as a display name (every prisma
// migration file shares it), so prisma uses the migration's parent directory
// name; every other tool's file already names itself meaningfully, so its
// base name with the recognized extension stripped is used.
func TestSqlMigrationIdentifier(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		path string
		tool string
		want string
	}{
		{
			name: "prisma uses directory name not migration.sql",
			path: filepath.FromSlash("/repo/prisma/migrations/20260414_add_orders/migration.sql"),
			tool: "prisma",
			want: "20260414_add_orders",
		},
		{
			name: "flyway strips .sql",
			path: filepath.FromSlash("/repo/sql/V42__backfill_orders.sql"),
			tool: "flyway",
			want: "V42__backfill_orders",
		},
		{
			name: "golang-migrate strips .up.sql",
			path: filepath.FromSlash("/repo/migrations/202604140101_add_orders.up.sql"),
			tool: "golang-migrate",
			want: "202604140101_add_orders",
		},
		{
			name: "generic strips .sql",
			path: filepath.FromSlash("/repo/migrations/20260414_add_orders.sql"),
			tool: "generic",
			want: "20260414_add_orders",
		},
		{
			name: "liquibase strips .sql",
			path: filepath.FromSlash("/repo/liquibase/changelog/20260414_add_orders.sql"),
			tool: "liquibase",
			want: "20260414_add_orders",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := sqlMigrationIdentifier(testCase.path, testCase.tool); got != testCase.want {
				t.Fatalf("sqlMigrationIdentifier(%q, %q) = %q, want %q", testCase.path, testCase.tool, got, testCase.want)
			}
		})
	}
}

// TestBuildSQLMigrationEntriesReturnsEmptyForNonMigrationPath guards that a
// SQL file the layout/filename heuristics do not recognize as a migration
// produces no SqlMigration entity at all (#5346): registering an entity for
// every SQL file, migration or not, would misclassify plain schema files.
func TestBuildSQLMigrationEntriesReturnsEmptyForNonMigrationPath(t *testing.T) {
	t.Parallel()

	got := buildSQLMigrationEntries(
		filepath.FromSlash("/repo/sql/schema.sql"),
		newSQLLineIndex([]byte("")),
		map[string]any{},
		nil,
	)
	if len(got) != 0 {
		t.Fatalf("buildSQLMigrationEntries() = %#v, want empty for a non-migration path", got)
	}
}

// TestBuildSQLMigrationEntriesCapsMigrationTargets proves the
// sqlMigrationTargetsCap bound (mirrors TestSelectReadTargetsCapTruncatesDeterministically
// for source_tables, #5345): a migration touching more than
// sqlMigrationTargetsCap distinct tables must stamp exactly the first
// sqlMigrationTargetsCap targets in sorted (line, kind, name) order.
func TestBuildSQLMigrationEntriesCapsMigrationTargets(t *testing.T) {
	t.Parallel()

	const overCap = sqlMigrationTargetsCap + 6
	mentions := make([]sqlMention, 0, overCap)
	for i := overCap - 1; i >= 0; i-- {
		mentions = append(mentions, sqlMention{
			name:      fmt.Sprintf("table_%03d", i),
			operation: "alter",
			offset:    0,
		})
	}

	entries := buildSQLMigrationEntries(
		filepath.FromSlash("/repo/migrations/20260414_add_orders.sql"),
		newSQLLineIndex([]byte("")),
		map[string]any{},
		mentions,
	)
	if len(entries) != 1 {
		t.Fatalf("buildSQLMigrationEntries() len = %d, want 1", len(entries))
	}
	targets, ok := entries[0]["migration_targets"].([]map[string]any)
	if !ok {
		t.Fatalf("migration_targets type = %T, want []map[string]any", entries[0]["migration_targets"])
	}
	if len(targets) != sqlMigrationTargetsCap {
		t.Fatalf("len(migration_targets) = %d, want %d (cap)", len(targets), sqlMigrationTargetsCap)
	}
	for i := 0; i < sqlMigrationTargetsCap; i++ {
		want := fmt.Sprintf("table_%03d", i)
		if got, _ := targets[i]["name"].(string); got != want {
			t.Fatalf("migration_targets[%d].name = %q, want %q (sorted-then-capped)", i, got, want)
		}
	}
}
