// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// A granted count or inventory call (#10880 PR-B2a) reads the per-support table
// when the winners gate is on, and refuses with ErrAggregateReadModelUnavailable
// (also matching ErrFindingSupportsUnavailable) while the table is not ready. The
// winners row cannot answer a grant, and the legacy fact read is the shape the
// supports table retires, so neither is a fallback.

func aggregateSupportsCountRow() db.Row {
	return &cov7StoreRow{values: []any{
		cov7NullInt(3), cov7NullInt(2), cov7NullInt(1),
		cov7NullInt(1), cov7NullInt(0), cov7NullInt(1),
		[]byte(`[{"bucket":"p1","bucket_count":2}]`), []byte(`[{"bucket":"critical","bucket_count":3}]`),
	}}
}

func aggregateGrantedFilters() map[string]AggregateFilter {
	return map[string]AggregateFilter{
		"scope grant":      {Status: "affected_exact", AllowedScopeIDs: []string{"scope:a"}},
		"repository grant": {CVEID: "CVE-2026-1", AllowedRepositoryIDs: []string{"repo:a"}},
		"precise profile":  {DetectionProfile: ProfilePrecise, AllowedScopeIDs: []string{"scope:a"}},
		"image ref":        {ImageRef: "registry.example/app:1", AllowedRepositoryIDs: []string{"repo:a"}},
		"rank filter":      {Severity: "high", AllowedScopeIDs: []string{"scope:a"}},
	}
}

func TestAggregatesGrantedCountReadsSupportsWhenReady(t *testing.T) {
	t.Parallel()
	for name, filter := range aggregateGrantedFilters() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			probe := supportsRouteQueryer(true, nil)
			probe.onRow = func(string) db.Row { return aggregateSupportsCountRow() }
			store := NewPostgresAggregateStoreWithReadStore(probe, true)
			count, err := store.CountSupplyChainImpactFindings(t.Context(), filter)
			if err != nil {
				t.Fatalf("count = %v, want an envelope", err)
			}
			if count.Freshness.State != "fresh" || count.Freshness.LatestObservedAt != "" {
				t.Fatalf("freshness = %+v, want fresh with no winners watermark", count.Freshness)
			}
			if len(probe.queries) != 2 {
				t.Fatalf("granted count issued %d statements, want the readiness probe and one count", len(probe.queries))
			}
			count1 := probe.queries[1]
			if !strings.Contains(count1, "FROM supply_chain_impact_finding_supports") ||
				strings.Contains(count1, "FROM fact_records AS fact") ||
				strings.Contains(count1, "supply_chain_impact_canonical_winners") {
				t.Fatalf("granted gate-on count did not read the supports table:\n%s", count1)
			}
		})
	}
}

func TestAggregatesGrantedInventoryReadsSupportsForEveryDimension(t *testing.T) {
	t.Parallel()
	for _, dimension := range []InventoryDimension{
		InventoryByStatus, InventoryByPriorityBucket, InventoryBySeverity, InventoryByRepository, InventoryByEcosystem,
	} {
		t.Run(string(dimension), func(t *testing.T) {
			t.Parallel()
			probe := supportsRouteQueryer(true, nil)
			store := NewPostgresAggregateStoreWithReadStore(probe, true)
			page, err := store.SupplyChainImpactInventory(
				t.Context(), AggregateFilter{Status: "affected_exact", AllowedScopeIDs: []string{"scope:a"}}, dimension, 10, 0)
			if err != nil {
				t.Fatalf("inventory = %v, want a page", err)
			}
			if page.Freshness.State != "fresh" || page.Freshness.LatestObservedAt != "" {
				t.Fatalf("freshness = %+v, want fresh with no winners watermark", page.Freshness)
			}
			if len(probe.queries) != 2 || !strings.Contains(probe.queries[1], "FROM supply_chain_impact_finding_supports") ||
				strings.Contains(probe.queries[1], "FROM fact_records AS fact") {
				t.Fatalf("granted inventory (%s) did not read the supports table: %d statements", dimension, len(probe.queries))
			}
			if strings.Contains(probe.queries[1], supplyChainImpactInventoryGroupExpressionPlaceholder) {
				t.Fatal("inventory query kept the group expression placeholder")
			}
		})
	}
}

func TestAggregatesGrantedCallRefusesWhenSupportsAreNotReady(t *testing.T) {
	t.Parallel()
	for name, probe := range map[string]*cov7StoreQueryer{
		"behind the contract or fenced": supportsRouteQueryer(false, nil),
		"readiness probe failed":        supportsRouteQueryer(false, errors.New("relation does not exist")),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := NewPostgresAggregateStoreWithReadStore(probe, true)
			filter := AggregateFilter{Status: "affected_exact", AllowedScopeIDs: []string{"scope:a"}}
			_, countErr := store.CountSupplyChainImpactFindings(t.Context(), filter)
			_, inventoryErr := store.SupplyChainImpactInventory(t.Context(), filter, InventoryByStatus, 10, 0)
			for what, err := range map[string]error{"count": countErr, "inventory": inventoryErr} {
				if !errors.Is(err, ErrAggregateReadModelUnavailable) || !errors.Is(err, ErrFindingSupportsUnavailable) {
					t.Fatalf("%s = %v, want ErrAggregateReadModelUnavailable that also matches ErrFindingSupportsUnavailable", what, err)
				}
			}
			for _, query := range probe.queries {
				if !strings.Contains(query, "supply_chain_impact_finding_supports_backfill") {
					t.Fatalf("a refused call issued a non-probe statement (never winners or legacy):\n%s", query)
				}
			}
		})
	}
}

func TestAggregatesGateOffKeepsTheLegacyReadWithoutAProbe(t *testing.T) {
	t.Parallel()
	probe := supportsRouteQueryer(false, nil)
	probe.onRow = func(string) db.Row { return aggregateSupportsCountRow() }
	store := NewPostgresAggregateStoreWithReadStore(probe, false)
	if _, err := store.CountSupplyChainImpactFindings(t.Context(),
		AggregateFilter{Status: "affected_exact", AllowedScopeIDs: []string{"scope:a"}}); err != nil {
		t.Fatalf("count = %v, want an envelope", err)
	}
	if len(probe.queries) != 1 || !strings.Contains(probe.queries[0], "FROM fact_records AS fact") {
		t.Fatalf("gate-off granted count = %d statements, want one legacy fact read with no probe", len(probe.queries))
	}
}
