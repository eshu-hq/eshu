// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"os"
	"strings"
	"testing"
)

// TestSupplyChainImpactFindingSupportsMigrationShape pins the contract of
// migration 336 (#10880 PR-B1): the row identity is the support's fact id, not
// (canonical_key, scope_id), because the SQL canonical key collapses the CVE and
// advisory ids the writer's fact id keeps apart; the generation id cascades from
// scope_generations so retention and repository retirement remove supports with
// their facts; and each index is one a measured plan needs.
func TestSupplyChainImpactFindingSupportsMigrationShape(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("migrations/336_supply_chain_impact_finding_supports.sql")
	if err != nil {
		t.Fatalf("read migration 336: %v", err)
	}
	ddl := strings.Join(strings.Fields(string(raw)), " ")
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS supply_chain_impact_finding_supports",
		"fact_id TEXT PRIMARY KEY",
		"generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE",
		"payload JSONB NOT NULL",
		"(scope_id, generation_id)",
		"supply_chain_impact_finding_supports_repository_idx ON supply_chain_impact_finding_supports (repository_id)",
		"supply_chain_impact_finding_supports_finding_idx ON supply_chain_impact_finding_supports (finding_id)",
		"(canonical_key) WHERE scope_id = 'operator:vulnerability_suppressions'",
	} {
		if !strings.Contains(ddl, want) {
			t.Errorf("migration 336 lacks %q", want)
		}
	}
	for _, forbidden := range []string{
		"UNIQUE (canonical_key", "PRIMARY KEY (canonical_key", "UNIQUE (scope_id, canonical_key",
	} {
		if strings.Contains(ddl, forbidden) {
			t.Errorf("migration 336 keys the table by %q: (canonical_key, scope_id) is not unique among supports", forbidden)
		}
	}
}
