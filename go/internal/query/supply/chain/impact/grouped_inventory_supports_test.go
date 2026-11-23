// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
)

// TestGroupedInventorySupportsSQLShape pins the granted variant: it reads the
// supports table for the finding set and for the hidden-bucket signal, never the
// winners table or the fact table for a finding row, and the hidden signal binds
// only the grant ($18, $19), never a request filter, so completeness cannot act
// as a filter-dependent oracle on hidden findings.
func TestGroupedInventorySupportsSQLShape(t *testing.T) {
	hidden := groupedSupportsHiddenSQL
	for _, want := range []string{"FROM supply_chain_impact_finding_supports AS hidden_support", "hidden_support.scope_id NOT LIKE 'image://derived/%'", "$18", "$19"} {
		if !strings.Contains(hidden, want) {
			t.Fatalf("hidden-bucket CTE missing %q", want)
		}
	}
	for n := 1; n <= groupedInventoryArgCount; n++ {
		if n == 18 || n == 19 {
			continue
		}
		if regexp.MustCompile(fmt.Sprintf(`\$%d\b`, n)).MatchString(hidden) {
			t.Fatalf("hidden-bucket CTE binds request parameter $%d", n)
		}
	}
	for _, dimension := range []GroupedInventoryDimension{GroupedInventoryByService, GroupedInventoryByWorkload, GroupedInventoryByEnvironment} {
		granted, err := groupedInventorySupportsQuery(dimension)
		if err != nil {
			t.Fatalf("groupedInventorySupportsQuery(%q) error = %v", dimension, err)
		}
		unscoped, err := groupedInventoryQuery(dimension)
		if err != nil {
			t.Fatalf("groupedInventoryQuery(%q) error = %v", dimension, err)
		}
		if !strings.Contains(granted, supplyChainImpactReadModeSQL(hidden, false)) {
			t.Fatalf("%s granted query lost the supports hidden-bucket CTE", dimension)
		}
		for _, banned := range []string{"supply_chain_impact_canonical_winners", "FROM fact_records AS fact", "operator_candidates"} {
			if strings.Contains(granted, banned) {
				t.Fatalf("%s granted query still contains %q", dimension, banned)
			}
		}
		if strings.Contains(unscoped, "supply_chain_impact_finding_supports") {
			t.Fatalf("%s unscoped query reads the supports table", dimension)
		}
		assertGroupedPlaceholdersBound(t, granted, groupedInventoryArgCount)
	}
}

func TestGroupedInventoryStoreSelectsScopedQueryForGrants(t *testing.T) {
	cases := []struct {
		name   string
		filter AggregateFilter
		scoped bool
	}{
		{"unscoped", AggregateFilter{}, false},
		{"repository grant", AggregateFilter{AllowedRepositoryIDs: []string{"repository:r"}}, true},
		{"scope grant", AggregateFilter{AllowedScopeIDs: []string{"scope:s"}}, true},
		{"scope grant with image_ref", AggregateFilter{AllowedScopeIDs: []string{"scope:s"}, ImageRef: "registry.example/app:1"}, true},
		{"repository grant with precise profile", AggregateFilter{AllowedRepositoryIDs: []string{"repository:r"}, DetectionProfile: ProfilePrecise}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := &groupedFakeQueryer{watermark: []time.Time{groupedTestNow}, page: [][]any{{nil, nil, nil, nil, nil, nil, nil, int64(0)}}}
			if _, err := groupedTestStore(q).SupplyChainImpactGroupedInventory(context.Background(), tc.filter, GroupedInventoryByService, 10, 0); err != nil {
				t.Fatalf("SupplyChainImpactGroupedInventory() error = %v", err)
			}
			if len(q.calls) != 2 {
				t.Fatalf("calls = %d, want one probe and the grouped query", len(q.calls))
			}
			if tc.scoped != strings.Contains(q.calls[0].query, "supply_chain_impact_finding_supports_backfill") {
				t.Fatalf("granted = %v but the first call was %q", tc.scoped, q.calls[0].query)
			}
			if got := strings.Contains(q.calls[1].query, "FROM supply_chain_impact_finding_supports"); got != tc.scoped {
				t.Fatalf("supports query used = %v, want %v", got, tc.scoped)
			}
		})
	}
}

// TestGroupedInventoryGrantedCallRefusesWhenSupportsAreNotReady pins the
// fail-closed contract for a granted call: a supports table that is behind the
// contract, fenced, or unreadable answers ErrGroupedInventoryReadModelUnavailable
// (also matching ErrFindingSupportsUnavailable so the handler names the model)
// after the readiness probe alone, never a winners or fact read.
func TestGroupedInventoryGrantedCallRefusesWhenSupportsAreNotReady(t *testing.T) {
	for name, q := range map[string]*groupedFakeQueryer{
		"behind the contract or fenced": {supportsNotReady: true},
		"readiness probe failed":        {supportsProbeErr: errors.New("relation does not exist")},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := groupedTestStore(q).SupplyChainImpactGroupedInventory(context.Background(),
				AggregateFilter{AllowedScopeIDs: []string{"scope:s"}}, GroupedInventoryByService, 10, 0)
			if !errors.Is(err, ErrGroupedInventoryReadModelUnavailable) || !errors.Is(err, ErrFindingSupportsUnavailable) {
				t.Fatalf("err = %v, want ErrGroupedInventoryReadModelUnavailable that also matches ErrFindingSupportsUnavailable", err)
			}
			if len(q.calls) != 1 {
				t.Fatalf("calls = %d, want only the readiness probe", len(q.calls))
			}
		})
	}
}

// TestGroupedInventoryGrantedCallReportsFreshWithoutAWatermark pins that a
// supports-served page is current by construction: fresh, no winners watermark,
// and no watermark probe, so a lagging winners resweep never marks its buckets
// read_model_unavailable.
func TestGroupedInventoryGrantedCallReportsFreshWithoutAWatermark(t *testing.T) {
	q := &groupedFakeQueryer{watermarkErr: errors.New("must not be probed")}
	q.page = [][]any{groupedRow("service:a", false, 1, 10, `{"affected_exact":1}`, `{"high":1}`, false, 1)}
	page, err := groupedTestStore(q).SupplyChainImpactGroupedInventory(context.Background(),
		AggregateFilter{AllowedScopeIDs: []string{"scope:s"}}, GroupedInventoryByService, 10, 0)
	if err != nil {
		t.Fatalf("err = %v, want a page", err)
	}
	if page.Freshness.State != packet.FreshnessLabelFresh || page.Freshness.LatestObservedAt != "" {
		t.Fatalf("freshness = %+v, want fresh with no watermark", page.Freshness)
	}
	if page.Rows[0].Completeness != GroupCompletenessComplete {
		t.Fatalf("completeness = %q, want complete", page.Rows[0].Completeness)
	}
	for _, call := range q.calls {
		if call.query == SelectWinnersWatermarkQuery {
			t.Fatal("a supports-served call probed the winners watermark")
		}
	}
}
