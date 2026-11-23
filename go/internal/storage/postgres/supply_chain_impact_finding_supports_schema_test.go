// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"os"
	"strings"
	"testing"
)

// TestSupplyChainImpactFindingSupportsMigrationShape pins the contract of
// migration 338 (#10880 PR-B1): the row identity is the support's fact id, not
// (canonical_key, scope_id), because the SQL canonical key collapses the CVE and
// advisory ids the writer's fact id keeps apart; the generation id cascades from
// scope_generations so retention and repository retirement remove supports with
// their facts; and each index is one a measured plan needs.
func TestSupplyChainImpactFindingSupportsMigrationShape(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("migrations/338_supply_chain_impact_finding_supports.sql")
	if err != nil {
		t.Fatalf("read migration 338: %v", err)
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
		// The generation foreign key's cascade reads the supports by generation_id
		// once per deleted generation; PostgreSQL 18.6 reads the whole
		// (scope_id, generation_id) index for it, so this index is required.
		"supply_chain_impact_finding_supports_generation_idx ON supply_chain_impact_finding_supports (generation_id)",
		// The old-writer fence: a dirty-scope table and triggers that skip rows
		// written under the maintainer's transaction setting.
		"CREATE TABLE IF NOT EXISTS supply_chain_impact_finding_supports_dirty ( scope_id TEXT PRIMARY KEY",
		// The fence function runs as its owner with a pinned search_path and no
		// PUBLIC execute (the form of migration 307); the live proofs pin the
		// posture and the behavior per schema.
		"RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER AS $$",
		"ALTER FUNCTION supply_chain_impact_finding_supports_mark_dirty() SET search_path FROM CURRENT",
		"REVOKE ALL ON FUNCTION supply_chain_impact_finding_supports_mark_dirty() FROM PUBLIC",
		"CREATE TRIGGER supply_chain_impact_finding_supports_dirty_insert AFTER INSERT ON fact_records FOR EACH ROW",
		"CREATE TRIGGER supply_chain_impact_finding_supports_dirty_update AFTER UPDATE ON fact_records FOR EACH ROW",
		"NEW.fact_kind = 'reducer_supply_chain_impact_finding'",
		"current_setting('eshu.supply_chain_impact_supports_writer', true) IS DISTINCT FROM 'supports_v1'",
	} {
		if !strings.Contains(ddl, want) {
			t.Errorf("migration 338 lacks %q", want)
		}
	}
	for _, forbidden := range []string{
		"UNIQUE (canonical_key", "PRIMARY KEY (canonical_key", "UNIQUE (scope_id, canonical_key",
	} {
		if strings.Contains(ddl, forbidden) {
			t.Errorf("migration 338 keys the table by %q: (canonical_key, scope_id) is not unique among supports", forbidden)
		}
	}
}
