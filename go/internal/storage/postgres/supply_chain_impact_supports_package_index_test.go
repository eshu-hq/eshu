// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"os"
	"strings"
	"testing"
)

// TestSupplyChainImpactSupportsPackageIndexMatchesTheMigration pins that migration
// 345 builds the partial package_id btree the reconciliation read's package arm
// can use (#10880 PR-B2c): one CONCURRENTLY statement, keyed (package_id, fact_id),
// and partial on exactly the literal predicate the arm repeats
// (supplyChainImpactSupportsPackageIndexPredicate), so the planner can prove the
// index usable for the arm.
func TestSupplyChainImpactSupportsPackageIndexMatchesTheMigration(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("migrations/345_supply_chain_impact_supports_package_idx.sql")
	if err != nil {
		t.Fatalf("read migration 345: %v", err)
	}
	var statement []string
	for _, line := range strings.Split(string(raw), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "--") {
			statement = append(statement, trimmed)
		}
	}
	ddl := strings.Join(statement, " ")
	want := "CREATE INDEX CONCURRENTLY IF NOT EXISTS supply_chain_impact_finding_supports_package_idx " +
		"ON supply_chain_impact_finding_supports (package_id, fact_id) " +
		"WHERE " + supplyChainImpactSupportsPackageIndexPredicate + ";"
	if ddl != want {
		t.Errorf("migration 345 statement =\n%s\nwant\n%s", ddl, want)
	}
	if got := strings.Count(ddl, ";"); got != 1 {
		t.Errorf("migration 345 holds %d statements, want exactly 1 (a concurrent build cannot share a transaction)", got)
	}
}
