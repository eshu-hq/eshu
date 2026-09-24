// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicd

import (
	"strings"
	"testing"
)

func TestCICDRunCorrelationSQLAppliesScopedAuthorizationBeforeOrderAndGrouping(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		query      string
		beforeText string
		repoParam  string
		scopeParam string
	}{
		{
			name:       "list",
			query:      listRunCorrelationsQuery,
			beforeText: "ORDER BY",
			repoParam:  "fact.payload->>'repository_id' = ANY($13::text[])",
			scopeParam: "fact.scope_id = ANY($14::text[])",
		},
		{
			name:       "total",
			query:      cicdRunCorrelationAggregateTotalQuery,
			beforeText: ";",
			repoParam:  "fact.payload->>'repository_id' = ANY($9::text[])",
			scopeParam: "fact.scope_id = ANY($10::text[])",
		},
		{
			name:       "group",
			query:      cicdRunCorrelationAggregateGroupQueryTemplate,
			beforeText: "GROUP BY",
			repoParam:  "fact.payload->>'repository_id' = ANY($9::text[])",
			scopeParam: "fact.scope_id = ANY($10::text[])",
		},
		{
			name:       "inventory",
			query:      cicdRunCorrelationInventoryQueryTemplate,
			beforeText: "GROUP BY",
			repoParam:  "fact.payload->>'repository_id' = ANY($11::text[])",
			scopeParam: "fact.scope_id = ANY($12::text[])",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, want := range []string{tc.repoParam, tc.scopeParam} {
				if !strings.Contains(tc.query, want) {
					t.Fatalf("query missing %q:\n%s", want, tc.query)
				}
				if strings.Index(tc.query, want) > strings.Index(tc.query, tc.beforeText) {
					t.Fatalf("authorization predicate %q appears after %s:\n%s", want, tc.beforeText, tc.query)
				}
			}
		})
	}
}
