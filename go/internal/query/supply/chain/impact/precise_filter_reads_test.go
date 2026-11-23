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
// supplyChainImpactPreciseMatchReasons promises: the shipped legacy and winners
// list, count and inventory reads and the three grouped queries spell the same
// match reason list, in the same order, as the constant that migration 343's index
// predicate and the supplement probe are built from. Test fixtures that copy the
// filter are not covered. A read that drifts from the list stops matching the index predicate, so a
// drift fails here instead of in a plan. Migration 343's header says the constant
// builds both; TestPreciseIndexMigrationCarriesThePredicate pins only the
// migration against the constant, and this test pins the reads.
func TestShippedReadsSpellThePreciseMatchReasonList(t *testing.T) {
	t.Parallel()
	want := matchReasonList()
	reads := map[string]string{
		"legacy list":       ListFindingsQuery,
		"winners list":      ListFindingsFromWinnersQuery,
		"legacy count":      AggregateCombinedCountQuery,
		"legacy inventory":  InventoryQuery("COALESCE(NULLIF(fact.impact_status, ''), 'unknown')"),
		"winners count":     AggregateWinnersCombinedCountQuery,
		"winners inventory": AggregateWinnersInventoryQuery("COALESCE(NULLIF(w.impact_status, ''), 'unknown')"),
	}
	for _, dimension := range []GroupedInventoryDimension{GroupedInventoryByService, GroupedInventoryByWorkload, GroupedInventoryByEnvironment} {
		query, err := groupedInventoryQuery(dimension)
		if err != nil {
			t.Fatalf("build grouped %s: %v", dimension, err)
		}
		reads["grouped "+string(dimension)] = query
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

// TestQueryCanonicalKeyCarriesStatusAndObservedVersion is the query package's side
// of the storage test of the same premise: the CanonicalFindingKeySQL literal the
// legacy reads partition by carries impact_status and observed_version, so every
// support of a key shares them with its winner (the premise of the precise status
// prune). The two key texts are separate copies; this test and the storage one
// each pin the two columns, not the identity of the copies.
func TestQueryCanonicalKeyCarriesStatusAndObservedVersion(t *testing.T) {
	t.Parallel()
	for _, part := range []string{"'observed_version'", "'impact_status'"} {
		if !strings.Contains(CanonicalFindingKeySQL, part) {
			t.Errorf("CanonicalFindingKeySQL lost %s: the precise status prune would skip a key that has a precise support", part)
		}
	}
}
