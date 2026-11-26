// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// reverseAuditSupportsSwaps puts each swap's legacy fragment back, so a derived statement
// can be compared with the legacy one byte for byte.
func reverseAuditSupportsSwaps(statement string, swaps []winnersAuditSupportsSwap) string {
	for _, swap := range swaps {
		statement = strings.ReplaceAll(statement, swap.supports, swap.legacy)
	}
	return statement
}

// TestWinnersAuditSupportsInputsDeriveFromTheLegacyStatements proves every audit statement
// moved by #10880 PR-C2 l is its legacy statement with exactly the declared swaps: reversing
// them gives the legacy text back, so nothing else in the range, probe, lateral, staged batch
// or ident fetch changed.
func TestWinnersAuditSupportsInputsDeriveFromTheLegacyStatements(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		derived, legacy string
		swaps           []winnersAuditSupportsSwap
	}{
		"first range":    {selectWinnersAuditFirstRangeFromSupportsSQL, selectWinnersAuditFirstRangeSQL, winnersAuditRangeSwaps},
		"next range":     {selectWinnersAuditNextRangeFromSupportsSQL, selectWinnersAuditNextRangeSQL, winnersAuditRangeSwaps},
		"null probe":     {probeWinnersAuditNullKeysFromSupportsSQL, probeWinnersAuditNullKeysSQL, winnersAuditNullProbeSwaps},
		"pending select": {winnersAuditRestrictedPendingSelectFromSupports("t"), winnersAuditRestrictedPendingSelect("t"), winnersAuditPendingLateralSwaps},
		"staged batch":   {stagedWinnersBatchActiveFactsFromSupportsSQL, stagedWinnersBatchActiveFactsSQL, stagedWinnersBatchSwaps},
		"staged ident":   {stagedWinnerFactIdentFromSupportsSQL, stagedWinnerFactIdentSQL, stagedWinnerFactIdentSwaps},
	} {
		if c.derived == c.legacy {
			t.Errorf("%s: the supports statement equals the legacy one", name)
		}
		if got := reverseAuditSupportsSwaps(c.derived, c.swaps); got != c.legacy {
			t.Errorf("%s: reversing the swaps does not give the legacy statement back: something else changed", name)
		}
	}
	// The scope expansion shares the maintainer's declared swaps.
	if got := reverseWinnersSupportsInput(winnersScopeExpansionSelectFromSupports(winnersStagingTable)); got != winnersScopeExpansionSelect(winnersStagingTable) {
		t.Error("scope expansion: reversing the swaps does not give the legacy statement back")
	}
}

// TestWinnersAuditSwapsPanicOnADriftedStatement is the seeded RED half: a swap whose
// legacy fragment occurs the wrong number of times panics instead of silently not applying.
func TestWinnersAuditSwapsPanicOnADriftedStatement(t *testing.T) {
	t.Parallel()

	for name, run := range map[string]func(){
		"fragment missing": func() {
			applyWinnersAuditSupportsSwaps("planted", "SELECT 1", []winnersAuditSupportsSwap{{"FROM fact_records", "FROM supports", 1}})
		},
		"fragment repeated": func() {
			applyWinnersAuditSupportsSwaps("planted", "FROM fact_records FROM fact_records", []winnersAuditSupportsSwap{{"FROM fact_records", "FROM supports", 1}})
		},
		"count too high": func() {
			applyWinnersAuditSupportsSwaps("planted", "FROM fact_records", []winnersAuditSupportsSwap{{"FROM fact_records", "FROM supports", 3}})
		},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: no panic on a drifted statement", name)
				}
			}()
			run()
		}()
	}
	if got := applyWinnersAuditSupportsSwaps("ok", "a FROM fact_records b FROM fact_records", []winnersAuditSupportsSwap{{"FROM fact_records", "FROM supports", 2}}); got != "a FROM supports b FROM supports" {
		t.Errorf("a swap with the declared count did not apply to every occurrence: %q", got)
	}
}
