// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestClaimReducerWorkBatchQueryUsesWindowFunctionsNotCorrelatedFairRank is a
// query-shape regression gate: it asserts claimReducerWorkBatchQuery contains
// the rank-once window constructs (#3624 Track 2) and does NOT contain the
// O(N^2) correlated "count(*) ... fair_peer" pattern the rewrite eliminates,
// so a future edit cannot silently reintroduce the per-row correlated
// fair-rank re-evaluation this rewrite exists to remove.
func TestClaimReducerWorkBatchQueryUsesWindowFunctionsNotCorrelatedFairRank(t *testing.T) {
	t.Parallel()

	query := claimReducerWorkBatchQuery
	for _, want := range []string{
		"row_number() OVER w_same",
		"row_number() OVER w_fairsame",
		"ROWS UNBOUNDED PRECEDING",
		"WINDOW",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("batch claim query missing rank-once window construct %q:\n%s", want, query)
		}
	}
	for _, forbidden := range []string{
		"FROM fact_work_items AS fair_peer",
		"count(*)\n            FROM fact_work_items AS fair_peer",
		"AS fair_same_superseded",
	} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("batch claim query still has the correlated O(N^2) fair-rank pattern %q:\n%s", forbidden, query)
		}
	}
}
