// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"strings"
	"testing"
)

// The impact rows of the work budget never loosen (#10880 PR-C2 n, arbiter-c2-n-armed section 2).
// Each ceiling is the value the shipped table carries today: 307,638 blocks on the fact leg
// and 2,403 on the winners leg. A table that carries more than its ceiling fails here, so a
// regeneration from a worse run cannot raise a budget quietly. Lowering a budget needs a live
// run (the single leg after the write stop takes the winners value, 2,403, once a run at the
// C2 head confirms it); this test only moves in one direction, and the ceiling constants move
// down in the same commit that lowers the table.
const (
	factLegImpactBlksCeiling    = 307638
	winnersLegImpactBlksCeiling = 2403
)

// impactBudgetOver returns every impact route of the table text whose block budget exceeds
// ceiling.
func impactBudgetOver(t *testing.T, table string, ceiling int) []string {
	t.Helper()
	budgets, err := ParseRouteWorkBudgets(strings.NewReader(table))
	if err != nil {
		t.Fatalf("parse the work budget table: %v", err)
	}
	var over []string
	for _, route := range impactAggregateRoutes {
		if !budgets.Named(route) {
			over = append(over, route+" (no row: it would take the default budget)")
			continue
		}
		if blks := budgets.For(route).Blks; blks > ceiling {
			over = append(over, route)
		}
	}
	return over
}

// TestImpactWorkBudgetsNeverLoosen is the GREEN half: both shipped tables keep their impact
// rows at or under their ceilings.
func TestImpactWorkBudgetsNeverLoosen(t *testing.T) {
	t.Parallel()

	for path, ceiling := range map[string]int{
		shippedRouteWorkBudgetsPath:   factLegImpactBlksCeiling,
		shippedWinnersWorkBudgetsPath: winnersLegImpactBlksCeiling,
	} {
		raw, err := os.ReadFile(path) // #nosec G304 -- shipped fixture path, not user input
		if err != nil {
			t.Fatal(err)
		}
		if over := impactBudgetOver(t, string(raw), ceiling); len(over) != 0 {
			t.Errorf("%s loosens the impact work budget past %d blocks: %q", path, ceiling, over)
		}
	}
}

// TestImpactWorkBudgetRatchetCatchesALoosenedRow is the seeded RED half: the shipped table with
// one impact row raised by a block is reported, so the ratchet cannot pass a loosened budget.
func TestImpactWorkBudgetRatchetCatchesALoosenedRow(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(shippedWinnersWorkBudgetsPath) // #nosec G304 -- shipped fixture path, not user input
	if err != nil {
		t.Fatal(err)
	}
	planted := strings.Replace(string(raw), "\t14\t2403\t14\t", "\t14\t2404\t14\t", 1)
	if planted == string(raw) {
		t.Fatal("the plant did not change the table: the test would pass vacuously")
	}
	if over := impactBudgetOver(t, planted, winnersLegImpactBlksCeiling); len(over) != 1 {
		t.Fatalf("a loosened impact row was reported as %q, want exactly one route", over)
	}
}
