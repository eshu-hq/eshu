// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
	"time"
)

// TestLoadSupplyChainImpactWinnersAuditSweepBudgets pins the #11131 knobs: a
// positive value is used as given; unset, unparsable or non-positive values
// select the built-in defaults (0 here).
func TestLoadSupplyChainImpactWinnersAuditSweepBudgets(t *testing.T) {
	t.Parallel()
	env := func(key, value string) func(string) string {
		return func(k string) string {
			if k == key {
				return value
			}
			return ""
		}
	}
	for raw, want := range map[string]int{"": 0, "2500": 2500, " 7 ": 7, "0": 0, "-3": 0, "lots": 0} {
		if got := loadSupplyChainImpactWinnersAuditRangeBudget(env(supplyChainImpactWinnersAuditRangeBudgetEnv, raw)); got != want {
			t.Errorf("range budget %q = %d, want %d", raw, got, want)
		}
	}
	for raw, want := range map[string]time.Duration{"": 0, "2m": 2 * time.Minute, "0": 0, "-1m": 0, "soon": 0} {
		if got := loadSupplyChainImpactWinnersAuditTickBudget(env(supplyChainImpactWinnersAuditTickBudgetEnv, raw)); got != want {
			t.Errorf("tick budget %q = %v, want %v", raw, got, want)
		}
	}
}
