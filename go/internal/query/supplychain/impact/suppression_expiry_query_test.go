// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"strings"
	"testing"
)

func TestSupplyChainImpactQueriesEvaluateSuppressionExpiryAtStatementTime(t *testing.T) {
	t.Parallel()

	queries := map[string]struct {
		query  string
		readAt string
	}{
		"list direct":       {query: ListSupplyChainImpactFindingsQuery, readAt: "$24::timestamptz"},
		"list materialized": {query: ListSupplyChainImpactFindingsFromWinnersQuery, readAt: "$24::timestamptz"},
		"aggregate shared":  {query: SupplyChainImpactAggregateCanonicalFactsCTE, readAt: "$20::timestamptz"},
		"aggregate count":   {query: SupplyChainImpactAggregateCountQuery, readAt: "$20::timestamptz"},
		"explain":           {query: ExplainSupplyChainImpactFindingQuery, readAt: "$13::timestamptz"},
	}
	for name, tc := range queries {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, want := range []string{
				tc.readAt,
				"expires_at",
				"'expired'",
			} {
				if !strings.Contains(tc.query, want) {
					t.Fatalf("%s query missing read-time expiry token %q", name, want)
				}
			}
			if strings.Contains(tc.query, "statement_timestamp()") {
				t.Fatalf("%s query reads statement time instead of the store-bound evaluation clock", name)
			}
		})
	}
}
