package postgres

import (
	"strings"
	"testing"
)

// TestBootstrapDefinitionsIncludeSupplyChainImpactCanonicalWinners pins the
// #3389 winners read-model table into the bootstrap schema: the denormalized
// columns the list read filters/keysets on, plus the per-filter composite
// indexes that keep that read O(page). If a later edit drops a filter column or
// the keyset-trailing index, the materialized read regresses to a sort.
func TestBootstrapDefinitionsIncludeSupplyChainImpactCanonicalWinners(t *testing.T) {
	t.Parallel()

	var def Definition
	for _, d := range BootstrapDefinitions() {
		if d.Name == "supply_chain_impact_canonical_winners" {
			def = d
			break
		}
	}
	if def.Name == "" {
		t.Fatal("supply_chain_impact_canonical_winners definition missing")
	}
	if def.Path != "schema/data-plane/postgres/033_supply_chain_impact_canonical_winners.sql" {
		t.Fatalf("unexpected Path %q", def.Path)
	}
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS supply_chain_impact_canonical_winners",
		"canonical_key TEXT PRIMARY KEY",
		"winner_fact_id TEXT NOT NULL",
		"finding_id TEXT NOT NULL",
		"priority_score INTEGER NOT NULL",
		"source_count INTEGER NOT NULL",
		// Denormalized filter columns the read needs on this table alone.
		"impact_status TEXT NOT NULL",
		"severity_bucket TEXT NOT NULL",
		"repository_id TEXT NOT NULL",
		"service_ids JSONB NOT NULL",
		"workload_ids JSONB NOT NULL",
		"environments JSONB NOT NULL",
		// Keyset + per-filter composite indexes that keep the read O(page).
		"supply_chain_impact_canonical_winners_priority_idx",
		"supply_chain_impact_canonical_winners_status_idx",
		"supply_chain_impact_canonical_winners_severity_idx",
		"supply_chain_impact_canonical_winners_repository_idx",
		"USING GIN (service_ids)",
	} {
		if !strings.Contains(def.SQL, want) {
			t.Fatalf("winners schema SQL missing %q:\n%s", want, def.SQL)
		}
	}
}
