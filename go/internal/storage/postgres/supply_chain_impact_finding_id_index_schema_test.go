// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func TestSupplyChainImpactFindingIDIndexStaysInBootstrapAndMigration(t *testing.T) {
	t.Parallel()

	var migration Definition
	for _, definition := range BootstrapDefinitions() {
		if definition.Name == "supply_chain_impact_finding_id_index" {
			migration = definition
		}
	}
	if migration.Name == "" {
		t.Fatal("supply_chain_impact_finding_id_index definition missing")
	}

	const indexName = "fact_records_supply_chain_impact_finding_id_idx"
	for name, sql := range map[string]string{
		"bootstrap": factRecordSchemaSQL,
		"migration": migration.SQL,
	} {
		for _, want := range []string{
			indexName,
			"(payload->>'finding_id')",
			"fact_kind = 'reducer_supply_chain_impact_finding'",
			"is_tombstone = FALSE",
		} {
			if !strings.Contains(sql, want) {
				t.Fatalf("%s SQL missing %q:\n%s", name, want, sql)
			}
		}
	}
	if count := strings.Count(migration.SQL, "CREATE INDEX CONCURRENTLY IF NOT EXISTS"); count != 1 {
		t.Fatalf("migration has %d concurrent index statements, want 1:\n%s", count, migration.SQL)
	}
	if count := strings.Count(migration.SQL, ";"); count != 1 {
		t.Fatalf("migration has %d SQL statements, want one isolated concurrent index:\n%s", count, migration.SQL)
	}
}
