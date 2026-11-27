// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

func normalizeSQL(text string) string { return strings.Join(strings.Fields(text), " ") }

// TestPrecisePredicateIsTheShippedFilter pins that every shipped read spells the
// match-reason list of the precise filter exactly as the route V predicate does,
// so the winners branch, the supplement probe and the partial index test the same
// set the shipped reads test.
func TestPrecisePredicateIsTheShippedFilter(t *testing.T) {
	t.Parallel()
	list := "'" + strings.Join(supplyChainImpactPreciseMatchReasons, "', '") + "'"
	for name, text := range map[string]string{
		"legacy list":      ListFindingsQuery,
		"winners list":     ListFindingsFromWinnersQuery,
		"supports list":    ListFindingsFromSupportsQuery,
		"winners count":    AggregateWinnersCombinedCountQuery,
		"legacy count":     AggregateCombinedCountQuery,
		"grouped findings": groupedInventoryFindingsCTE,
	} {
		if !strings.Contains(normalizeSQL(text), list) {
			t.Errorf("%s no longer spells the precise match-reason list as route V does", name)
		}
	}
	predicate := SupportsPreciseIndexPredicateSQL
	for _, want := range []string{"detection_profile = 'precise'", "detection_profile = ''", "impact_status IN ('affected_exact', 'not_affected_known_fixed')", "observed_version <> ''"} {
		if !strings.Contains(predicate, want) {
			t.Errorf("the index predicate lost %q: %s", want, predicate)
		}
	}
	if strings.Contains(predicate, ".") && strings.Contains(predicate, "w.") {
		t.Errorf("the index predicate carries a table alias: %s", predicate)
	}
}

// TestSupplementDerivationsFailOnALostMarker proves each marker of the route V
// derivations: a shipped text that lost one block does not derive silently.
func TestSupplementDerivationsFailOnALostMarker(t *testing.T) {
	t.Parallel()
	lose := func(text, marker string) string { return strings.Replace(text, marker, "renamed", 1) }
	for name, tc := range map[string]struct {
		derive func(string) (string, error)
		legacy string
		marker string
	}{
		"list profile block":      {deriveSupplementList, ListFindingsFromWinnersQuery, "      AND ($14 = '' OR w.priority_bucket = $14)"},
		"list filtered CTE":       {deriveSupplementList, ListFindingsFromWinnersQuery, "filtered AS NOT MATERIALIZED (\n    SELECT w.finding_id,"},
		"list paged CTE":          {deriveSupplementList, ListFindingsFromWinnersQuery, "),\npaged AS MATERIALIZED ("},
		"list payload join":       {deriveSupplementList, ListFindingsFromWinnersQuery, "JOIN supply_chain_impact_canonical_winners AS win\n  ON win.canonical_key = page.canonical_key\n"},
		"aggregate profile block": {deriveAggregateSupplement, aggregateWinnersFindingsCTE, "    AND ($13 = '' OR w.priority_bucket = $13)"},
		"aggregate CTE":           {deriveAggregateSupplement, aggregateWinnersFindingsCTE, "aggregate_winners AS NOT MATERIALIZED (\n  SELECT w.impact_status,"},
		"grouped profile block":   {deriveGroupedSupplement, groupedInventoryFindingsCTE, "    AND ($13 = '' OR w.priority_bucket = $13)"},
		"grouped CTE":             {deriveGroupedSupplement, groupedInventoryFindingsCTE, "grouped_findings AS MATERIALIZED (\n  SELECT w.repository_id,"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			mutated := lose(tc.legacy, tc.marker)
			if mutated == tc.legacy {
				t.Fatalf("mutation %q changed nothing: the marker text moved", name)
			}
			if _, err := tc.derive(mutated); err == nil {
				t.Fatalf("derivation accepted a shipped text that lost the %s marker", name)
			}
		})
	}
	for name, derive := range map[string]func() error{
		"list":      func() error { _, err := deriveSupplementList(ListFindingsFromWinnersQuery); return err },
		"aggregate": func() error { _, err := deriveAggregateSupplement(aggregateWinnersFindingsCTE); return err },
		"grouped":   func() error { _, err := deriveGroupedSupplement(groupedInventoryFindingsCTE); return err },
	} {
		if err := derive(); err != nil {
			t.Fatalf("derivation of the shipped %s text failed: %v", name, err)
		}
	}
}

// TestRouteVAndImageReadsReferenceEveryBind proves every positional parameter of
// the new reads is referenced (Postgres rejects an untyped one) and that no read
// goes beyond its bound list.
func TestRouteVAndImageReadsReferenceEveryBind(t *testing.T) {
	t.Parallel()
	groupedV, err := groupedInventorySupplementQuery(GroupedInventoryByService)
	if err != nil {
		t.Fatal(err)
	}
	groupedImage, err := groupedInventoryImageQuery(GroupedInventoryByService)
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		query string
		binds int
	}{
		"V list":          {ListFindingsFromWinnersSupplementQuery, 30},
		"V count":         {AggregateSupplementCombinedCountQuery, 26},
		"V inventory":     {AggregateSupplementInventoryQuery("fact.impact_status"), 28},
		"V grouped":       {groupedV, 22},
		"image list":      {ListFindingsFromSupportsImageQuery, 30},
		"image count":     {AggregateSupportsImageCombinedCountQuery, 26},
		"image inventory": {strings.Replace(AggregateSupportsImageInventoryQueryTemplate, supplyChainImpactInventoryGroupExpressionPlaceholder, "fact.impact_status", 1), 28},
		"image grouped":   {groupedImage, 22},
	} {
		for n := 1; n <= tc.binds; n++ {
			token := fmt.Sprintf("$%d", n)
			found := false
			for _, at := range indexAll(tc.query, token) {
				if next := at + len(token); next >= len(tc.query) || tc.query[next] < '0' || tc.query[next] > '9' {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s never references %s", name, token)
			}
		}
		beyond := fmt.Sprintf("$%d", tc.binds+1)
		for _, at := range indexAll(tc.query, beyond) {
			if next := at + len(beyond); next >= len(tc.query) || tc.query[next] < '0' || tc.query[next] > '9' {
				t.Errorf("%s references %s beyond its %d bound parameters", name, beyond, tc.binds)
				break
			}
		}
	}
}

// TestSupplementProbeShape pins the contract of branch B (ruling V and its index
// amendment): an explicit ORDER BY and LIMIT 1 over the supports of the key,
// eligibility as scalar subqueries (never joins, which made the planner drive from
// the scope tables), rank filters on the PICKED support and never on the winner row,
// the status prune, and the winners branch under the shipped precise predicate.
func TestSupplementProbeShape(t *testing.T) {
	t.Parallel()
	for name, query := range map[string]string{
		"list":      ListFindingsFromWinnersSupplementQuery,
		"aggregate": AggregateSupplementCombinedCountQuery,
	} {
		normal := normalizeSQL(query)
		for _, want := range []string{
			"ORDER BY s.priority_score DESC, s.has_payload_finding_id DESC, s.fact_id ASC LIMIT 1",
			"COALESCE((SELECT TRUE FROM ingestion_scopes AS sc JOIN scope_generations AS gen",
			"COALESCE((SELECT class.source_class = 'static' AND class.lifecycle = 'active'",
			"b_candidates AS (",
			"branch_b AS (",
			"w.impact_status IN ('affected_exact', 'not_affected_known_fixed') AND w.observed_version <> ''",
			"AND NOT (w.detection_profile = 'precise'",
		} {
			if !strings.Contains(normal, want) {
				t.Errorf("%s read lost %q", name, want)
			}
		}
		candidates := normal[strings.Index(normal, "b_candidates AS ("):strings.Index(normal, "branch_b AS (")]
		for _, banned := range []string{"severity_bucket", "priority_bucket", "w.ecosystem", "suppression_state"} {
			if strings.Contains(candidates, banned) {
				t.Errorf("%s: the candidate selection tests %q on the winner row; rank filters belong to the pick", name, banned)
			}
		}
		if strings.Contains(normal, "FROM fact_records") && strings.Contains(candidates, "fact_records") {
			t.Errorf("%s: the supplement reads the fact table for a finding row", name)
		}
	}
}

// TestImageReadsMaterializeTheCandidateFetch pins AS MATERIALIZED as part of the
// image read's contract (arbiter index ruling): without it the planner drives the
// read from the scope tables. The fetch is uncapped.
func TestImageReadsMaterializeTheCandidateFetch(t *testing.T) {
	t.Parallel()
	for name, query := range map[string]string{
		"list":      ListFindingsFromSupportsImageQuery,
		"aggregate": AggregateSupportsImageCombinedCountQuery,
	} {
		normal := normalizeSQL(query)
		for _, want := range []string{"image_supports AS MATERIALIZED", "image_ref <> ''", "FROM image_supports AS fact"} {
			if !strings.Contains(normal, want) {
				t.Errorf("image %s read lost %q", name, want)
			}
		}
		if strings.Contains(normal, "UNION ALL SELECT * FROM supply_chain_impact_finding_supports WHERE repository_id") {
			t.Errorf("image %s read still reads through the grant arms", name)
		}
		if strings.Contains(normal, "LIMIT 10000") {
			t.Errorf("image %s read caps its candidate fetch", name)
		}
	}
}

// TestSupplementVolatileStripMatchesWinnersStore pins #11600: the branch-B
// support-payload strip carries exactly the winners store volatile set, so a
// branch B row reads like a branch A row. The expected set is derived from
// supplychaincore.DigestExcludedPayloadKeys minus the served top-level key,
// the same rule as winnerPayloadStripExpr in
// supply_chain_impact_winners_select_digest_test.go: every other excluded key
// is volatile and the select strips it, while the served key stays.
func TestSupplementVolatileStripMatchesWinnersStore(t *testing.T) {
	t.Parallel()

	const servedDigestExcludedKey = "evidence_fact_ids"
	var want []string
	for _, key := range supplychaincore.DigestExcludedPayloadKeys() {
		if key != servedDigestExcludedKey {
			want = append(want, key)
		}
	}
	if len(want) == 0 {
		t.Fatal("derived volatile strip set is empty: the DigestExcludedPayloadKeys rule moved")
	}

	sql := supplyChainImpactVolatileStripSQL("pick.payload")
	strip := regexp.MustCompile(`- '([a-z_]+)'`)
	var got []string
	for _, m := range strip.FindAllStringSubmatch(sql, -1) {
		got = append(got, m[1])
	}
	counts := map[string]int{}
	for _, key := range got {
		counts[key]++
	}
	for _, key := range want {
		if counts[key] != 1 {
			t.Errorf("supplement strip has %d occurrences of %q, want exactly 1 in %q", counts[key], key, sql)
		}
	}
	if counts[servedDigestExcludedKey] != 0 {
		t.Errorf("supplement strip carries served key %q %d times, want 0 in %q", servedDigestExcludedKey, counts[servedDigestExcludedKey], sql)
	}
	if len(got) != len(want) {
		t.Errorf("supplement strip carries %d top-level strips %v, want exactly %d %v", len(got), got, len(want), want)
	}
}
