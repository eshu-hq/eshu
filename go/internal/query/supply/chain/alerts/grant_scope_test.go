// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alerts

import (
	"fmt"
	"strings"
	"testing"
)

// TestSecurityAlertReconciliationSQLLetsABaseGrantCoverItsShards pins the
// #9019 review F4 grant rule in every scoped read: the shard disjunct sits
// inside the grant predicate (before pagination or grouping), keys on the
// grant array of that query, and survives the aggregate templates' Sprintf.
// The live behavior (base grant sees its shards, ungranted and prefix-only
// ids see none) is TestShardSplitMergeLive.
func TestSecurityAlertReconciliationSQLLetsABaseGrantCoverItsShards(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, query, fragment, before string
	}{
		{"list", listQuery, shardGrantMatch11, "LIMIT $10"},
		{"total", aggregateTotalQuery, shardGrantMatch8, "SELECT COUNT(*)"},
		{"group", fmt.Sprintf(aggregateGroupQueryTemplate, "current_fact.payload->>'provider'"), shardGrantMatch8, "GROUP BY"},
		{"inventory", fmt.Sprintf(inventoryQueryTemplate, "current_fact.payload->>'provider'"), shardGrantMatch8, "GROUP BY"},
	} {
		if !strings.Contains(tc.query, tc.fragment) {
			t.Fatalf("%s query is missing the shard grant disjunct:\n%s", tc.name, tc.query)
		}
		if strings.Index(tc.query, tc.fragment) > strings.Index(tc.query, tc.before) {
			t.Fatalf("%s shard grant disjunct appears after %s", tc.name, tc.before)
		}
	}
	for _, fragment := range []string{shardGrantMatch8, shardGrantMatch11} {
		for _, want := range []string{"strpos(fact.scope_id, '#shard-') > 0", "split_part(fact.scope_id, '#shard-', 1) = ANY("} {
			if !strings.Contains(fragment, want) {
				t.Fatalf("shard grant disjunct is missing %q:\n%s", want, fragment)
			}
		}
		if strings.Contains(fragment, "%") || strings.Contains(strings.ToUpper(fragment), " LIKE ") {
			t.Fatalf("shard grant disjunct must not use a pattern:\n%s", fragment)
		}
	}
}
