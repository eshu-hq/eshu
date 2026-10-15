// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/coordination"
)

// TestFactKeyPartitionIndexMigrations pins the #7420 (scope_id,
// generation_id, fact_id) indexes on the two package key sidecars. The
// explicit retention child deletes key on (scope_id, generation_id); without
// a scope-leading index the package key deletes plan as a sequential scan of
// the whole table (measured 67,368 pages at a 5% share). Each build must stay
// a sole CONCURRENTLY statement: the migration coordinator runs such a file
// in autocommit without the bootstrap lock_timeout (#7004), and PostgreSQL
// rejects a concurrent build inside a transaction block.
func TestFactKeyPartitionIndexMigrations(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, index, table string }{
		{"package_manifest_consumption_keys_partition_idx", "package_manifest_consumption_keys_partition_idx", "package_manifest_consumption_keys"},
		{"package_registry_identity_keys_partition_idx", "package_registry_identity_keys_partition_idx", "package_registry_identity_keys"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			migration := migrationSQLOrEmpty(tc.name)
			if migration == "" {
				t.Fatalf("migration %q is not embedded", tc.name)
			}
			normalized := strings.Join(strings.Fields(migration), " ")
			for _, fragment := range []string{
				"CREATE INDEX CONCURRENTLY IF NOT EXISTS " + tc.index,
				"ON " + tc.table + " (scope_id, generation_id, fact_id)",
			} {
				if !strings.Contains(normalized, fragment) {
					t.Fatalf("migration %q missing %q", tc.name, fragment)
				}
			}
			_, statement, found := strings.Cut(migration, "CREATE INDEX")
			if !found {
				t.Fatalf("migration %q has no CREATE INDEX statement", tc.name)
			}
			if terminators := strings.Count(statement, ";"); terminators != 1 {
				t.Fatalf("concurrent index migration %q has %d SQL statements, want one", tc.name, terminators)
			}
			if !coordination.IsSoleConcurrentIndexStatement(migration) {
				t.Fatalf("migration %q is not one isolated concurrent-index statement", tc.name)
			}
		})
	}
}

// migrationSQLOrEmpty returns the named migration's SQL, or "" when it is
// not embedded, so a missing file fails the test instead of panicking.
func migrationSQLOrEmpty(name string) string {
	for _, def := range BootstrapDefinitions() {
		if def.Name == name {
			return def.SQL
		}
	}
	return ""
}
