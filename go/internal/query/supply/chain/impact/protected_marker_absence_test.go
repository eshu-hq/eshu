// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"strings"
	"testing"
)

// TestComposedQueriesCarryNoProtectedReadMarker guards the authorized-read
// composition: every production query string must have its protected-reads
// mode marker substituted before it reaches Postgres. An unsubstituted
// marker fails at runtime with column-does-not-exist, which would turn the
// intended derived-read fail-closed into a whole-read outage. Derived reads
// stay closed through the substituted FALSE branch, not through the marker.
func TestComposedQueriesCarryNoProtectedReadMarker(t *testing.T) {
	queries := map[string]string{
		"ListFindingsQuery":             ListFindingsQuery,
		"ListFindingsProtectedQuery":    ListFindingsProtectedQuery,
		"ListFindingsFromWinnersQuery":  ListFindingsFromWinnersQuery,
		"AggregateCanonicalFactsCTE":    AggregateCanonicalFactsCTE,
		"AggregateCountQuery":           AggregateCountQuery,
		"AggregatePriorityCountQuery":   AggregatePriorityCountQuery,
		"AggregateSeverityCountQuery":   AggregateSeverityCountQuery,
		"InventoryQueryTemplate":        InventoryQueryTemplate,
		"ExplainFindingQuery":           ExplainFindingQuery,
		"ExplainFindingByPublicIDQuery": ExplainFindingByPublicIDQuery,
	}
	for name, query := range queries {
		if query == "" {
			t.Errorf("composed query %s is empty", name)
			continue
		}
		if strings.Contains(query, supplyChainImpactProtectedModeMarker) {
			t.Errorf("composed query %s carries unsubstituted protected-reads marker", name)
		}
	}
}
