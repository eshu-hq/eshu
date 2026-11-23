// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"strings"
	"testing"
)

// matchReasonList is the precise match reason list as the reads spell it, with
// every space removed so line breaks and indentation do not matter.
func matchReasonList() string {
	quoted := make([]string, len(supplyChainImpactPreciseMatchReasons))
	for i, reason := range supplyChainImpactPreciseMatchReasons {
		quoted[i] = "'" + reason + "'"
	}
	return "IN(" + strings.Join(quoted, ",") + ")"
}

func squeeze(text string) string { return strings.Join(strings.Fields(text), "") }

// TestShippedReadsSpellThePreciseMatchReasonList pins what the comment on
// supplyChainImpactPreciseMatchReasons promises: every shipped read that applies
// the precise filter spells the same match reason list, in the same order, as the
// constant the partial index of migration 343 and the supplement probe are built
// from. A reader that drifts from the list stops matching the index predicate, so
// a drift fails here instead of in a plan. The grouped inventory query is checked
// in each dimension it is built for.
func TestShippedReadsSpellThePreciseMatchReasonList(t *testing.T) {
	t.Parallel()
	want := matchReasonList()
	reads := map[string]string{
		"legacy list":                 ListFindingsQuery,
		"winners list":                ListFindingsFromWinnersQuery,
		"supports list":               ListFindingsFromSupportsQuery,
		"legacy count":                AggregateCombinedCountQuery,
		"winners count":               AggregateWinnersCombinedCountQuery,
		"supports count":              AggregateSupportsCombinedCountQuery,
		"supplement list":             ListFindingsFromWinnersSupplementQuery,
		"supplement count":            AggregateSupplementCombinedCountQuery,
		"supplement inventory status": AggregateSupplementInventoryQuery("COALESCE(NULLIF(source.impact_status, ''), 'unknown')"),
	}
	for _, dimension := range []GroupedInventoryDimension{GroupedInventoryByService, GroupedInventoryByWorkload, GroupedInventoryByEnvironment} {
		for name, build := range map[string]func(GroupedInventoryDimension) (string, error){
			"grouped winners":    groupedInventoryQuery,
			"grouped supports":   groupedInventorySupportsQuery,
			"grouped supplement": groupedInventorySupplementQuery,
		} {
			query, err := build(dimension)
			if err != nil {
				t.Fatalf("build %s %s: %v", name, dimension, err)
			}
			reads[name+" "+string(dimension)] = query
		}
	}
	for name, query := range reads {
		if !strings.Contains(squeeze(query), want) {
			t.Errorf("%s does not spell the precise match reason list of supplyChainImpactPreciseMatchReasons in order", name)
		}
	}
	// Seeded violation: the same check rejects a read whose list lost one reason.
	broken := strings.Replace(squeeze(ListFindingsFromWinnersQuery), "'maven_known_fixed',", "", 1)
	if strings.Contains(broken, want) || broken == squeeze(ListFindingsFromWinnersQuery) {
		t.Fatal("the check accepted a read whose match reason list lost an entry")
	}
}
