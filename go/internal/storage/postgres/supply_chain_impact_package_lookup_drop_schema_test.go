// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/coordination"
)

// TestSupplyChainImpactPackageLookupIndexRetired pins the #10544 retirement
// of fact_records_supply_chain_impact_package_lookup_idx. The keyed winners
// probe (migration 291) leads with the same package triple and adds the
// coalesced c0/advisory expressions, but on QA the planner estimated rows=1
// for the triple through either index (forced costs 8.46 old vs 8.45 new in
// the Gate B fixture), so it kept choosing the triple-only index and
// heap-fetched every candidate of a touched triple. The retirement drops the
// old index from existing deployments (migration 293) and from the Go
// bootstrap fragment so a fresh isolated schema never builds it; the probe
// and analyzeWinnersKeyedIdentsSQL are unchanged. The census and fixture
// proof are in docs/internal/evidence/10544-retire-package-lookup-idx.md.
func TestSupplyChainImpactPackageLookupIndexRetired(t *testing.T) {
	t.Parallel()

	const retired = "fact_records_supply_chain_impact_package_lookup_idx"
	if strings.Contains(factRecordSchemaSQL, retired) {
		t.Fatal("factRecordSchemaSQL must not contain " + retired +
			"; remove the CREATE INDEX lines from schema_fact_records.go")
	}
	// The neighboring impact indexes and the 291 replacement stay.
	for _, want := range []string{
		"fact_records_supply_chain_impact_lookup_idx",
		"fact_records_supply_chain_impact_status_lookup_idx",
		"fact_records_supply_chain_impact_priority_lookup_idx",
		"fact_records_supply_chain_impact_repository_lookup_idx",
		"fact_records_supply_chain_impact_winners_keyed_idx",
	} {
		if !strings.Contains(factRecordSchemaSQL, want) {
			t.Fatalf("factRecordSchemaSQL missing surviving index %q", want)
		}
	}

	var migration string
	for _, def := range BootstrapDefinitions() {
		if def.Name == "drop_fact_records_supply_chain_impact_package_lookup_idx" {
			migration = def.SQL
			break
		}
	}
	if migration == "" {
		t.Fatal("drop migration missing; create " +
			"293_drop_fact_records_supply_chain_impact_package_lookup_idx.sql")
	}
	if want := "DROP INDEX CONCURRENTLY IF EXISTS " + retired; !strings.Contains(migration, want) {
		t.Fatalf("drop migration missing %q:\n%s", want, migration)
	}
	_, statement, found := strings.Cut(migration, "DROP INDEX")
	if !found {
		t.Fatalf("drop migration has no DROP INDEX statement:\n%s", migration)
	}
	if terminators := strings.Count(statement, ";"); terminators != 1 {
		t.Fatalf("concurrent drop migration has %d SQL statements, want one", terminators)
	}
	if !coordination.IsSoleConcurrentIndexStatement(migration) {
		t.Fatalf("drop migration is not one isolated concurrent-index statement:\n%s", migration)
	}
}
