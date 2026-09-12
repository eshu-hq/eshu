// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alerts

import (
	"strings"
	"testing"
)

func TestSecurityAlertReconciliationSQLAppliesScopedGrant(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		query      string
		beforeText string
		predicate  string
	}{
		{
			// The list query has a window-function ORDER BY inside the CTE, so
			// anchor on the final LIMIT to assert the grant predicate precedes
			// pagination.
			name:       "list",
			query:      listQuery,
			beforeText: "LIMIT $10",
			predicate:  "COALESCE(cardinality($11::text[]), 0) = 0",
		},
		{
			name:       "total",
			query:      aggregateTotalQuery,
			beforeText: ";",
			predicate:  "COALESCE(cardinality($8::text[]), 0) = 0",
		},
		{
			name:       "group",
			query:      aggregateGroupQueryTemplate,
			beforeText: "GROUP BY",
			predicate:  "COALESCE(cardinality($8::text[]), 0) = 0",
		},
		{
			name:       "inventory",
			query:      inventoryQueryTemplate,
			beforeText: "GROUP BY",
			predicate:  "COALESCE(cardinality($8::text[]), 0) = 0",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(tc.query, tc.predicate) {
				t.Fatalf("query missing scoped grant predicate %q:\n%s", tc.predicate, tc.query)
			}
			if strings.Index(tc.query, tc.predicate) > strings.Index(tc.query, tc.beforeText) {
				t.Fatalf("grant predicate %q appears after %s:\n%s", tc.predicate, tc.beforeText, tc.query)
			}
		})
	}
	if !strings.Contains(inventoryQueryTemplate, "LIMIT $9 OFFSET $10") {
		t.Fatalf("inventory limit/offset must shift to $9/$10 after the grant array:\n%s", inventoryQueryTemplate)
	}
}
