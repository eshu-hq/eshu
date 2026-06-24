// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestBootstrapDefinitionsIncludeSupplyChainImpactWinnersMaterialization pins the
// #3389 Phase 3 maintainer-watermark table into the bootstrap schema. It is the
// singleton row the atomic resweep stamps even when zero winners result, so the
// impact-findings read can tell "never populated" (building) from "reswept to
// zero findings" (fresh empty). If a later edit drops the singleton guard or the
// NOT NULL watermark, the freshness signal regresses.
func TestBootstrapDefinitionsIncludeSupplyChainImpactWinnersMaterialization(t *testing.T) {
	t.Parallel()

	var def Definition
	for _, d := range BootstrapDefinitions() {
		if d.Name == "supply_chain_impact_winners_materialization" {
			def = d
			break
		}
	}
	if def.Name == "" {
		t.Fatal("supply_chain_impact_winners_materialization definition missing")
	}
	if def.Path != "schema/data-plane/postgres/034_supply_chain_impact_winners_materialization.sql" {
		t.Fatalf("unexpected Path %q", def.Path)
	}
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS supply_chain_impact_winners_materialization",
		"singleton SMALLINT PRIMARY KEY DEFAULT 1 CHECK (singleton = 1)",
		"materialized_at TIMESTAMPTZ NOT NULL",
	} {
		if !strings.Contains(def.SQL, want) {
			t.Fatalf("winners materialization schema SQL missing %q:\n%s", want, def.SQL)
		}
	}
}
