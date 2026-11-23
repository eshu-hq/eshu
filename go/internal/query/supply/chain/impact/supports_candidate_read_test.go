// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"strings"
	"testing"
)

// supportsCandidateListQuery returns the production supports list read. The
// B1 equality and plan-shape proofs keep running against it, so the shipped read
// is the one proven.
func supportsCandidateListQuery(t testing.TB) string {
	t.Helper()
	if ListFindingsFromSupportsQuery == "" {
		t.Fatal("ListFindingsFromSupportsQuery is empty")
	}
	return ListFindingsFromSupportsQuery
}

// TestSupportsListQueryDerivationFailsOnALostMarker proves each marker of the
// derivation: a legacy text that lost one block does not derive silently.
func TestSupportsListQueryDerivationFailsOnALostMarker(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(string) string{
		"source candidates":  func(q string) string { return strings.Replace(q, "\nsource_candidates AS (", "\nrenamed AS (", 1) },
		"source winners":     func(q string) string { return strings.Replace(q, "\nsource_winners AS (", "\nrenamed AS (", 1) },
		"operator overrides": func(q string) string { return strings.Replace(q, "operator_overrides AS (", "renamed AS (", 1) },
		"override join": func(q string) string {
			return strings.Replace(q, "LEFT JOIN operator_overrides AS override\n    ON override.canonical_key = source.canonical_key", "LEFT JOIN renamed AS override", 1)
		},
		"suppression filter": func(q string) string {
			return strings.Replace(q, "  WHERE ($20 = '' OR effective_suppression_state = $20)", "  WHERE TRUE", 1)
		},
		"payload joins": func(q string) string {
			return strings.Replace(q, "JOIN fact_records AS source\n  ON source.fact_id = page.fact_id", "JOIN renamed AS source", 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			legacy := mutate(ListFindingsQuery)
			if legacy == ListFindingsQuery {
				t.Fatalf("mutation %q changed nothing: the marker text moved", name)
			}
			if _, err := deriveSupportsListQuery(legacy); err == nil {
				t.Fatalf("derivation accepted a legacy query that lost the %s marker", name)
			}
		})
	}
	if _, err := deriveSupportsListQuery(ListFindingsQuery); err != nil {
		t.Fatalf("derivation of the shipped legacy query failed: %v", err)
	}
}

// TestSupportsListQueryReadsOnlyTheSupportsTable proves the derived read keeps
// the legacy fact read out of the granted path: the facts table is read only by
// the scanner-authority classification joins, never for a finding row.
func TestSupportsListQueryReadsOnlyTheSupportsTable(t *testing.T) {
	t.Parallel()
	for _, banned := range []string{"FROM fact_records AS fact", "JOIN fact_records AS source", "LEFT JOIN fact_records AS override", "operator_candidates", "operator_overrides AS"} {
		if strings.Contains(ListFindingsFromSupportsQuery, banned) {
			t.Fatalf("supports list read still contains %q", banned)
		}
	}
}

// TestSupportsListQueryCarriesTheRuntimeFilters proves the supports read keeps
// the service, workload and environment filters of the legacy read: each one
// constrains the support's own repository to the repositories the runtime facts
// currently map (an existence-style filter, ruling Q2 default). A read that drops
// them would answer a granted service_id call with every finding in the grant.
func TestSupportsListQueryCarriesTheRuntimeFilters(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"service", "workload", "environment"} {
		want := "runtime_filter.filter_kind = '" + kind + "'\n               AND runtime_filter.repository_id = fact.repository_id"
		if !strings.Contains(ListFindingsFromSupportsQuery, want) {
			t.Fatalf("supports list read lost the %s runtime filter predicate over the support's repository", kind)
		}
	}
}
