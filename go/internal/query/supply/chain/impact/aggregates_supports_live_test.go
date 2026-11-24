// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10880 PR-B2a disposable PostgreSQL 18 equality proof: the granted count and the five inventory dimensions served from the per-support table (filled by the marker-gated backfill from the multi-support corpus) equal the legacy aggregates for every granted shape and non-rank filter, and equal the totals and buckets of the granted list for every rank filter (pick then filter), so a count never disagrees with the list it summarizes; a supports table that is not ready refuses with ErrAggregateReadModelUnavailable; reads ESHU_GROUPED_INVENTORY_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_GROUPED_INVENTORY_PROOF_DISPOSABLE ESHU_GROUPED_INVENTORY_PROOF_DSN

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core/cvss"
)

func supportsAggregateFilter(filter FindingFilter) AggregateFilter {
	return AggregateFilter{
		CVEID: filter.CVEID, AdvisoryID: filter.AdvisoryID, PackageID: filter.PackageID,
		RepositoryID: filter.RepositoryID, SubjectDigest: filter.SubjectDigest, ImageRef: filter.ImageRef,
		Status: filter.Status, Ecosystem: filter.Ecosystem, WorkloadID: filter.WorkloadID,
		ServiceID: filter.ServiceID, Environment: filter.Environment, Severity: filter.Severity,
		DetectionProfile: filter.DetectionProfile, PriorityBucket: filter.PriorityBucket,
		MinPriorityScore: filter.MinPriorityScore, SuppressionState: filter.SuppressionState,
		IncludeSuppressed:    filter.IncludeSuppressed,
		AllowedRepositoryIDs: filter.AllowedRepositoryIDs, AllowedScopeIDs: filter.AllowedScopeIDs,
	}
}

// supportsListOracle derives the count envelope and every inventory dimension
// from the rows of the granted list for the same filter, so the aggregates are
// held to the page they summarize.
func supportsListOracle(rows []FindingRow) (AggregateCount, map[InventoryDimension]map[string]int) {
	count := AggregateCount{ByPriorityBucket: map[string]int{}, BySeverity: map[string]int{}}
	dims := map[InventoryDimension]map[string]int{
		InventoryByStatus: {}, InventoryByPriorityBucket: {}, InventoryBySeverity: {},
		InventoryByRepository: {}, InventoryByEcosystem: {},
	}
	orDefault := func(value, fallback string) string {
		if value == "" {
			return fallback
		}
		return value
	}
	for _, row := range rows {
		count.TotalFindings++
		switch {
		case row.Status == "affected_exact":
			count.AffectedExact++
			count.AffectedFindings++
		case row.Status == "affected_derived":
			count.AffectedDerived++
			count.AffectedFindings++
		case row.Status == "possibly_affected":
			count.PossiblyAffected++
			count.AffectedFindings++
		case len(row.Status) >= 12 && row.Status[:12] == "not_affected":
			count.NotAffected++
		}
		priority := orDefault(row.PriorityBucket, "unknown")
		severity := cvss.Bucket(row.CVSSScore)
		count.ByPriorityBucket[priority]++
		count.BySeverity[severity]++
		dims[InventoryByStatus][orDefault(row.Status, "unknown")]++
		dims[InventoryByPriorityBucket][priority]++
		dims[InventoryBySeverity][severity]++
		dims[InventoryByRepository][orDefault(row.RepositoryID, "unknown")]++
		dims[InventoryByEcosystem][orDefault(row.Ecosystem, "unknown")]++
	}
	return count, dims
}

func supportsInventoryMap(t *testing.T, ctx context.Context, store PostgresAggregateStore, filter AggregateFilter, dimension InventoryDimension) (map[string]int, []InventoryRow) {
	t.Helper()
	page, err := store.SupplyChainImpactInventory(ctx, filter, dimension, 100, 0)
	if err != nil {
		t.Fatalf("inventory %s: %v", dimension, err)
	}
	out := map[string]int{}
	for _, row := range page.Rows {
		out[row.Value] = row.Count
	}
	return out, page.Rows
}

// TestSupportsAggregatesEqualTheListAndLegacyLive is the equality gate of the
// granted aggregates (#10880 PR-B2a). For every granted shape and filter the
// gate-on aggregate store, now answering from the supports table, returns the
// totals and buckets of the granted list for the same filter; for every non-rank
// filter it also returns the legacy aggregate's envelope and ordered inventory
// rows, because the legacy read and the ruled read agree there. Rank filters
// follow the ruled pick-then-filter order and are held to the list only.
func TestSupportsAggregatesEqualTheListAndLegacyLive(t *testing.T) {
	ctx, database := openSupportsEqualityDatabase(t)
	list := PostgresFindingStore{DB: database, ReadFromWinners: true}
	supports := PostgresAggregateStore{DB: database, ReadFromWinners: true}
	legacy := PostgresAggregateStore{DB: database, ReadFromWinners: false}
	dimensions := []InventoryDimension{
		InventoryByStatus, InventoryByPriorityBucket, InventoryBySeverity, InventoryByRepository, InventoryByEcosystem,
	}

	compared, nonEmpty := 0, 0
	for _, grant := range supportsEqualityGrants() {
		for _, tc := range supportsEqualityFilters() {
			if tc.filter.Limit != 0 {
				continue // paging is a list concern
			}
			t.Run(grant.name+"/"+tc.name, func(t *testing.T) {
				findingFilter := tc.filter
				findingFilter.AllowedRepositoryIDs, findingFilter.AllowedScopeIDs = grant.repositories, grant.scopes
				if findingFilter.Status == "" {
					findingFilter.Status = "affected_exact" // the list requires a narrowing filter
				}
				findingFilter.Limit = 200
				rows, err := list.ListSupplyChainImpactFindings(ctx, findingFilter)
				if err != nil {
					t.Fatalf("granted list: %v", err)
				}
				wantCount, wantDims := supportsListOracle(rows)
				filter := supportsAggregateFilter(findingFilter)

				got, err := supports.CountSupplyChainImpactFindings(ctx, filter)
				if err != nil {
					t.Fatalf("count: %v", err)
				}
				got.Freshness = AggregateFreshness{}
				if !reflect.DeepEqual(got, wantCount) {
					t.Fatalf("count differs from the list it summarizes\nlist:  %+v\ncount: %+v", wantCount, got)
				}
				for _, dimension := range dimensions {
					gotMap, gotRows := supportsInventoryMap(t, ctx, supports, filter, dimension)
					if !reflect.DeepEqual(gotMap, wantDims[dimension]) {
						t.Fatalf("inventory %s differs from the list it summarizes\nlist:      %v\ninventory: %v", dimension, wantDims[dimension], gotMap)
					}
					if !sort.SliceIsSorted(gotRows, func(i, j int) bool {
						if gotRows[i].Count != gotRows[j].Count {
							return gotRows[i].Count > gotRows[j].Count
						}
						return gotRows[i].Value < gotRows[j].Value
					}) {
						t.Fatalf("inventory %s rows are not ordered by count desc then value: %v", dimension, gotRows)
					}
					if !tc.rank {
						_, legacyRows := supportsInventoryMap(t, ctx, legacy, filter, dimension)
						if !reflect.DeepEqual(gotRows, legacyRows) {
							t.Fatalf("inventory %s differs from legacy\nlegacy:   %v\nsupports: %v", dimension, legacyRows, gotRows)
						}
					}
				}
				if !tc.rank {
					want, err := legacy.CountSupplyChainImpactFindings(ctx, filter)
					if err != nil {
						t.Fatalf("legacy count: %v", err)
					}
					want.Freshness = AggregateFreshness{}
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("count differs from legacy\nlegacy:   %+v\nsupports: %+v", want, got)
					}
				}
				compared++
				if got.TotalFindings > 0 {
					nonEmpty++
				}
			})
		}
	}
	if compared == 0 || nonEmpty < compared/4 {
		t.Fatalf("compared %d cases, %d non-empty: the matrix lost its signal", compared, nonEmpty)
	}
}

// TestSupportsAggregatesRefuseWhenNotReadyLive pins the fail-closed contract on
// a real database: a granted count or inventory call answers
// ErrAggregateReadModelUnavailable, never a winners or legacy answer, while the
// supports table is behind the contract or fenced, and recovers when it is not.
func TestSupportsAggregatesRefuseWhenNotReadyLive(t *testing.T) {
	ctx, database := openSupportsEqualityDatabase(t)
	store := PostgresAggregateStore{DB: database, ReadFromWinners: true}
	granted := AggregateFilter{Status: "affected_exact", AllowedScopeIDs: []string{multiSupportScopeA}}
	for _, tc := range []struct{ name, breakSQL, restoreSQL string }{
		{
			name:       "marker incomplete",
			breakSQL:   `UPDATE supply_chain_impact_finding_supports_backfill SET completed_at = NULL`,
			restoreSQL: `UPDATE supply_chain_impact_finding_supports_backfill SET completed_at = now()`,
		},
		{
			name:       "old-writer fence not empty",
			breakSQL:   `INSERT INTO supply_chain_impact_finding_supports_dirty (scope_id) VALUES ('scope:dirty')`,
			restoreSQL: `DELETE FROM supply_chain_impact_finding_supports_dirty`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := database.ExecContext(ctx, tc.breakSQL); err != nil {
				t.Fatalf("break readiness: %v", err)
			}
			_, countErr := store.CountSupplyChainImpactFindings(ctx, granted)
			_, inventoryErr := store.SupplyChainImpactInventory(ctx, granted, InventoryByStatus, 10, 0)
			if _, err := database.ExecContext(ctx, tc.restoreSQL); err != nil {
				t.Fatalf("restore readiness: %v", err)
			}
			for what, err := range map[string]error{"count": countErr, "inventory": inventoryErr} {
				if !errors.Is(err, ErrAggregateReadModelUnavailable) {
					t.Fatalf("not-ready %s = %v, want ErrAggregateReadModelUnavailable", what, err)
				}
			}
			if _, err := store.CountSupplyChainImpactFindings(ctx, granted); err != nil {
				t.Fatalf("count after restoring readiness: %v", err)
			}
		})
	}
}
