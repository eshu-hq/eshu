// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"fmt"
	"strings"
	"testing"
)

// TestAggregateSupportsDerivationFailsOnALostMarker proves each marker of the
// aggregate derivation: a legacy CTE that lost one block does not derive silently.
func TestAggregateSupportsDerivationFailsOnALostMarker(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(string) string{
		"source candidates":  func(q string) string { return strings.Replace(q, "\nsource_candidates AS (", "\nrenamed AS (", 1) },
		"source winners":     func(q string) string { return strings.Replace(q, "\nsource_winners AS (", "\nrenamed AS (", 1) },
		"operator overrides": func(q string) string { return strings.Replace(q, "operator_overrides AS (", "renamed AS (", 1) },
		"override join": func(q string) string {
			return strings.Replace(q, "LEFT JOIN operator_overrides AS override\n    ON override.canonical_key = source.canonical_key", "LEFT JOIN renamed AS override", 1)
		},
		"joined winners select": func(q string) string {
			return strings.Replace(q, "SELECT source.impact_status,", "SELECT source.priority_bucket,", 1)
		},
		"canonical facts select": func(q string) string {
			return strings.Replace(q, "canonical_facts AS (\n  SELECT impact_status,", "canonical_facts AS (\n  SELECT priority_bucket,", 1)
		},
		"suppression filter": func(q string) string {
			return strings.Replace(q, "  WHERE ($15 = '' OR fact.suppression_state = $15)", "  WHERE TRUE", 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			legacy := mutate(AggregateCanonicalFactsCTE)
			if legacy == AggregateCanonicalFactsCTE {
				t.Fatalf("mutation %q changed nothing: the marker text moved", name)
			}
			if _, err := deriveAggregateSupportsCTE(legacy); err == nil {
				t.Fatalf("derivation accepted a legacy CTE that lost the %s marker", name)
			}
		})
	}
	if _, err := deriveAggregateSupportsCTE(AggregateCanonicalFactsCTE); err != nil {
		t.Fatalf("derivation of the shipped legacy CTE failed: %v", err)
	}
}

// TestAggregateSupportsQueriesKeepTheLegacyTail proves the supports count and
// inventory queries differ from the legacy ones only in the CTE: the select that
// follows is the legacy text, so the two cannot drift unseen.
func TestAggregateSupportsQueriesKeepTheLegacyTail(t *testing.T) {
	t.Parallel()
	for name, pair := range map[string][2]string{
		"count":     {AggregateCombinedCountQuery, AggregateSupportsCombinedCountQuery},
		"inventory": {InventoryQueryTemplate, InventorySupportsQueryTemplate},
	} {
		legacyTail := strings.TrimPrefix(pair[0], AggregateCanonicalFactsCTE)
		if legacyTail == pair[0] || legacyTail == "" {
			t.Fatalf("%s: the legacy query no longer starts with the canonical-facts CTE", name)
		}
		if !strings.HasSuffix(pair[1], legacyTail) || strings.TrimSuffix(pair[1], legacyTail) != AggregateSupportsCanonicalFactsCTE {
			t.Fatalf("%s: the supports query is not the supports CTE plus the legacy tail", name)
		}
	}
}

// TestAggregateSupportsQueriesReadOnlyTheSupportsTable proves the granted
// aggregates keep the legacy fact read out: the facts table is read only by the
// runtime-filter and scanner-authority joins, never for a finding row.
func TestAggregateSupportsQueriesReadOnlyTheSupportsTable(t *testing.T) {
	t.Parallel()
	for _, query := range []string{AggregateSupportsCombinedCountQuery, InventorySupportsQueryTemplate} {
		for _, banned := range []string{"FROM fact_records AS fact", "operator_candidates", "operator_overrides AS"} {
			if strings.Contains(query, banned) {
				t.Fatalf("supports aggregate still contains %q", banned)
			}
		}
		if !strings.Contains(query, "FROM supply_chain_impact_finding_supports") {
			t.Fatal("supports aggregate never reads the supports table")
		}
	}
}

// TestSupportsReadsReferenceEveryBind proves every positional parameter of the
// supports reads is referenced. Postgres rejects a statement whose parameter
// the text never mentions ("could not determine data type"), and a bind the read
// ignores would mean a filter that silently does nothing.
func TestSupportsReadsReferenceEveryBind(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		query string
		binds int
		// unreferenced is a bind the read legitimately never mentions.
		unreferenced map[int]bool
	}{
		"list":      {ListFindingsFromSupportsQuery, 30, nil},
		"count":     {AggregateSupportsCombinedCountQuery, 26, nil},
		"inventory": {InventorySupportsQueryTemplate, 28, nil},
	} {
		for n := 1; n <= tc.binds; n++ {
			if tc.unreferenced[n] {
				continue
			}
			token := fmt.Sprintf("$%d", n)
			found := false
			for _, at := range indexAll(tc.query, token) {
				next := at + len(token)
				if next >= len(tc.query) || tc.query[next] < '0' || tc.query[next] > '9' {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s read never references %s", name, token)
			}
		}
	}
}

func indexAll(text, token string) []int {
	var at []int
	for from := 0; ; {
		i := strings.Index(text[from:], token)
		if i < 0 {
			return at
		}
		at = append(at, from+i)
		from += i + len(token)
	}
}
