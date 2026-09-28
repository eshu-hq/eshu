// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func TestWorkloadNamesScopeIndexMigrationIsSingleConcurrentStatement(t *testing.T) {
	t.Parallel()

	const migrationName = "fact_records_workload_names_scope_idx"
	const indexName = "fact_records_workload_names_scope_idx"
	var migration Definition
	for _, definition := range BootstrapDefinitions() {
		if definition.Name == migrationName {
			migration = definition
			break
		}
	}
	if migration.Name == "" {
		t.Fatalf("%s migration definition missing", migrationName)
	}

	if want := "CREATE INDEX CONCURRENTLY IF NOT EXISTS " + indexName; !strings.Contains(migration.SQL, want) {
		t.Fatalf("migration SQL missing index marker %q:\n%s", want, migration.SQL)
	}
	if count := strings.Count(migration.SQL, "CREATE INDEX CONCURRENTLY IF NOT EXISTS"); count != 1 {
		t.Fatalf("migration has %d concurrent index statements, want 1:\n%s", count, migration.SQL)
	}
	if count := strings.Count(migration.SQL, ";"); count != 1 {
		t.Fatalf("migration has %d SQL statements, want one isolated concurrent index:\n%s", count, migration.SQL)
	}
}
