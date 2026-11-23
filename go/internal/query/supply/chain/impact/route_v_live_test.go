// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10880 PR-B2a disposable PostgreSQL 18 proof of the unscoped existence-filter reads on the multi-support corpus (arbiter ruling V): an unscoped profile=precise call (winners plus the supplement, one statement) and an unscoped image_ref call (supports through the image partial index) return the legacy filter-then-pick answer for list, count, the five inventory dimensions and grouped inventory, including key p2 whose winner fails precise and a key whose winner carries another image; rank filters follow the ruled pick-then-filter order and equal an independent oracle with every difference from legacy listed for B3; all three sorts and a cursor that starts on a supplement row page like legacy; a winners row whose support is gone and a support with no winners row behave as documented; reads ESHU_GROUPED_INVENTORY_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_GROUPED_INVENTORY_PROOF_DISPOSABLE ESHU_GROUPED_INVENTORY_PROOF_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// openRouteVDatabase is the multi-support corpus with the supports table filled
// and the winners rebuilt after the extras, so the winners table and the supports
// table describe the same facts, as they do in production.
func openRouteVDatabase(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	ctx, database := openSupportsEqualityDatabaseWith(t, seedRouteVExtras)
	if err := storagepostgres.NewSupplyChainImpactWinnersStore(storagepostgres.SQLDB{DB: database}).
		RebuildAllWinners(ctx); err != nil {
		t.Fatalf("RebuildAllWinners: %v", err)
	}
	return ctx, database
}

// seedRouteVExtras adds key q1: its winner (priority 90, in scope B) is stamped
// comprehensive and fails the precise filter, and its only precise support
// (priority 20, in scope A) ranks far below it. The precise route must return q1 at
// its precise support (p=20), and a minimum priority above 20 must NOT return it:
// the minimum may prune on the winner row, but it has to apply to the pick too.
func seedRouteVExtras(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	precise := groupedLiveFindingPayload("q1", "q1", 20, 5.0)
	precise["priority_bucket"] = "low"
	groupedLiveFact(t, ctx, database, "fact:q1:a", multiSupportScopeA, "gen:a", FindingFactKind, precise)
	loud := groupedLiveFindingPayload("q1", "q1", 90, 5.0)
	loud["priority_bucket"] = "critical"
	loud["detection_profile"] = ProfileComprehensive
	loud["match_reason"] = "dpkg_affected_range"
	groupedLiveFact(t, ctx, database, "fact:q1:b", multiSupportScopeB, "gen:b", FindingFactKind, loud)
}

// routeVFilters are the unscoped existence-filter shapes; rank marks the filters
// that follow the ruled pick-then-filter order (a legacy difference is expected).
var routeVFilters = []struct {
	name   string
	filter FindingFilter
	rank   bool
}{
	{"precise", FindingFilter{DetectionProfile: ProfilePrecise}, false},
	{"precise with a cve", FindingFilter{DetectionProfile: ProfilePrecise, CVEID: "CVE-2026-m1"}, false},
	{"precise with suppressed", FindingFilter{DetectionProfile: ProfilePrecise, IncludeSuppressed: true}, false},
	{"precise with suppression state", FindingFilter{DetectionProfile: ProfilePrecise, SuppressionState: "accepted_risk"}, false},
	{"image a", FindingFilter{ImageRef: "registry.example/a:1"}, false},
	{"image b", FindingFilter{ImageRef: "registry.example/b:2"}, false},
	{"image absent", FindingFilter{ImageRef: "registry.example/none:0"}, false},
	{"precise and image a", FindingFilter{DetectionProfile: ProfilePrecise, ImageRef: "registry.example/a:1"}, false},
	{"precise with min priority 60", FindingFilter{DetectionProfile: ProfilePrecise, MinPriorityScore: 60}, false},
	{"precise with min priority 25", FindingFilter{DetectionProfile: ProfilePrecise, MinPriorityScore: 25}, false},
	{"precise with severity medium", FindingFilter{DetectionProfile: ProfilePrecise, Severity: "medium"}, true},
	{"precise with priority bucket low", FindingFilter{DetectionProfile: ProfilePrecise, PriorityBucket: "low"}, true},
	{"image b with priority bucket low", FindingFilter{ImageRef: "registry.example/b:2", PriorityBucket: "low"}, true},
	{"precise and image a with severity medium", FindingFilter{DetectionProfile: ProfilePrecise, ImageRef: "registry.example/a:1", Severity: "medium"}, true},
}

func routeVListFilter(filter FindingFilter) FindingFilter {
	filter.Limit = 200
	if filter.Status == "" {
		filter.Status = "affected_exact" // the list requires a narrowing filter
	}
	return filter
}

// TestUnscopedExistenceFilterReadsEqualLegacyLive is the equality gate of route V
// and the image read. Every non-rank case returns the legacy page, count and
// inventory rows (legacy is filter-then-pick, which the shortcut invariant says a
// passing winner already is, and the supplement recovers for a failing one). Every
// rank case equals the independent oracle (legacy with the rank binds cleared,
// then the rank filters applied in Go to the picked rows), and each difference
// from legacy is listed for B3.
func TestUnscopedExistenceFilterReadsEqualLegacyLive(t *testing.T) {
	ctx, database := openRouteVDatabase(t)
	gated := PostgresFindingStore{DB: database, ReadFromWinners: true}
	legacy := PostgresFindingStore{DB: database, ReadFromWinners: false}
	gatedAggregate := PostgresAggregateStore{DB: database, ReadFromWinners: true}
	legacyAggregate := PostgresAggregateStore{DB: database, ReadFromWinners: false}
	dimensions := []InventoryDimension{
		InventoryByStatus, InventoryByPriorityBucket, InventoryBySeverity, InventoryByRepository, InventoryByEcosystem,
	}

	nonEmpty := 0
	differences := map[string]string{}
	for _, tc := range routeVFilters {
		t.Run(tc.name, func(t *testing.T) {
			filter := routeVListFilter(tc.filter)
			got, err := gated.ListSupplyChainImpactFindings(ctx, filter)
			if err != nil {
				t.Fatalf("gate-on list: %v", err)
			}
			want, err := legacy.ListSupplyChainImpactFindings(ctx, filter)
			if err != nil {
				t.Fatalf("legacy list: %v", err)
			}
			if tc.rank {
				oracle, err := supportsRankOracle(ctx, legacy, filter)
				if err != nil {
					t.Fatalf("rank oracle: %v", err)
				}
				if differs := multiSupportDescribe(want) != multiSupportDescribe(oracle); differs {
					differences[tc.name] = "legacy " + multiSupportDescribe(want) + " | ruled " + multiSupportDescribe(oracle)
				}
				want = oracle
			}
			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(want)
			if string(gotJSON) != string(wantJSON) {
				t.Fatalf("gate-on page differs from its reference (rank=%v)\nreference: %s\ngate-on:   %s", tc.rank, multiSupportDescribe(want), multiSupportDescribe(got))
			}
			if len(got) > 0 {
				nonEmpty++
			}

			// Count and the five inventory dimensions equal the page they summarize.
			aggregate := supportsAggregateFilter(filter)
			wantCount, wantDims := supportsListOracle(want)
			count, err := gatedAggregate.CountSupplyChainImpactFindings(ctx, aggregate)
			if err != nil {
				t.Fatalf("gate-on count: %v", err)
			}
			count.Freshness = AggregateFreshness{}
			if count.TotalFindings != wantCount.TotalFindings || count.AffectedExact != wantCount.AffectedExact ||
				count.AffectedFindings != wantCount.AffectedFindings || count.NotAffected != wantCount.NotAffected {
				t.Fatalf("count differs from the page it summarizes\npage:  %+v\ncount: %+v", wantCount, count)
			}
			for _, dimension := range dimensions {
				gotMap, _ := supportsInventoryMap(t, ctx, gatedAggregate, aggregate, dimension)
				if len(gotMap) != len(wantDims[dimension]) {
					t.Fatalf("inventory %s = %v, want %v", dimension, gotMap, wantDims[dimension])
				}
				for value, n := range wantDims[dimension] {
					if gotMap[value] != n {
						t.Fatalf("inventory %s = %v, want %v", dimension, gotMap, wantDims[dimension])
					}
				}
			}
			if !tc.rank {
				legacyCount, err := legacyAggregate.CountSupplyChainImpactFindings(ctx, aggregate)
				if err != nil {
					t.Fatalf("legacy count: %v", err)
				}
				if count.TotalFindings != legacyCount.TotalFindings {
					t.Fatalf("count = %d, legacy count = %d", count.TotalFindings, legacyCount.TotalFindings)
				}
			}
		})
	}
	if nonEmpty < len(routeVFilters)/2 {
		t.Fatalf("only %d of %d cases returned rows: the matrix lost its signal", nonEmpty, len(routeVFilters))
	}
	for name, diff := range differences {
		t.Logf("B3 difference: %s: %s", name, diff)
	}
}

// TestPreciseRouteIncludesTheKeyWhoseWinnerFailsPrecise is the failing-first case
// of ruling V (required proof 2): key p2 has a precise support and a comprehensive
// winner, so the shipped winners read lost it. Unscoped precise list, count,
// inventory and grouped inventory must include it, and an unscoped image call must
// find a key whose winner carries another image reference.
func TestPreciseRouteIncludesTheKeyWhoseWinnerFailsPrecise(t *testing.T) {
	ctx, database := openRouteVDatabase(t)
	gated := PostgresFindingStore{DB: database, ReadFromWinners: true}
	rows, err := gated.ListSupplyChainImpactFindings(ctx, FindingFilter{Limit: 50, Status: "affected_exact", DetectionProfile: ProfilePrecise})
	if err != nil {
		t.Fatalf("precise list: %v", err)
	}
	if got := multiSupportDescribe(rows); !strings.Contains(got, "p2(p=30)") {
		t.Fatalf("unscoped precise list = %q, want it to include p2 at its precise support (p=30)", got)
	}
	aggregates := PostgresAggregateStore{DB: database, ReadFromWinners: true}
	count, err := aggregates.CountSupplyChainImpactFindings(ctx, AggregateFilter{Status: "affected_exact", DetectionProfile: ProfilePrecise})
	if err != nil || count.TotalFindings != len(rows) {
		t.Fatalf("precise count = %d (%v), want the list's %d rows including p2", count.TotalFindings, err, len(rows))
	}
	grouped := PostgresGroupedInventoryStore{DB: database, ReadFromWinners: true}
	page, err := grouped.SupplyChainImpactGroupedInventory(ctx, AggregateFilter{Status: "affected_exact", DetectionProfile: ProfilePrecise}, GroupedInventoryByService, 50, 0)
	if err != nil || page.DistinctFindings != len(rows) {
		t.Fatalf("precise grouped distinct = %d (%v), want %d", page.DistinctFindings, err, len(rows))
	}
	// i1 has two supports with different image references; the higher-priority
	// one (registry.example/b:2, p=75) is the winner, and an image_ref=a call must
	// still find the key through its other support.
	byImage, err := gated.ListSupplyChainImpactFindings(ctx, FindingFilter{Limit: 50, Status: "affected_exact", ImageRef: "registry.example/a:1"})
	if err != nil {
		t.Fatalf("image list: %v", err)
	}
	found := false
	for _, row := range byImage {
		found = found || row.FindingID == "finding:i1"
	}
	if !found {
		t.Fatalf("image_ref=a list = %q, want it to include i1 although its winner carries image b", multiSupportDescribe(byImage))
	}
}

// pageAll reads every page of one filter and sort and returns the finding ids in
// order, following the cursor the way a client does.
func pageAll(ctx context.Context, store PostgresFindingStore, filter FindingFilter) ([]string, error) {
	var ids []string
	filter.Limit = 3
	for len(ids) < 100 {
		rows, err := store.ListSupplyChainImpactFindings(ctx, filter)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			return ids, nil
		}
		for _, row := range rows {
			ids = append(ids, row.FindingID)
		}
		filter.AfterFindingID = rows[len(rows)-1].FindingID
	}
	return ids, nil
}

// TestUnscopedExistenceFilterPagesLikeLegacyLive pins all three sorts, and a
// cursor that starts on a supplement row (p2 appears only through branch B), for
// the precise route and the image route.
func TestUnscopedExistenceFilterPagesLikeLegacyLive(t *testing.T) {
	ctx, database := openRouteVDatabase(t)
	gated := PostgresFindingStore{DB: database, ReadFromWinners: true}
	legacy := PostgresFindingStore{DB: database, ReadFromWinners: false}
	for _, base := range []FindingFilter{
		{Status: "affected_exact", DetectionProfile: ProfilePrecise},
		{Status: "affected_exact", ImageRef: "registry.example/b:2"},
	} {
		for _, sort := range []string{"", "priority_score_desc", "priority_score_asc"} {
			filter := base
			filter.Sort = sort
			got, err := pageAll(ctx, gated, filter)
			if err != nil {
				t.Fatalf("gate-on pages: %v", err)
			}
			want, err := pageAll(ctx, legacy, filter)
			if err != nil {
				t.Fatalf("legacy pages: %v", err)
			}
			if strings.Join(got, ",") != strings.Join(want, ",") || len(got) == 0 {
				t.Fatalf("filter %+v sort %q pages differ\nlegacy:  %v\ngate-on: %v", base, sort, want, got)
			}
		}
	}
	// A cursor that starts on the supplement row.
	cursor := FindingFilter{Limit: 50, Status: "affected_exact", DetectionProfile: ProfilePrecise, AfterFindingID: "finding:p2"}
	got, err := gated.ListSupplyChainImpactFindings(ctx, cursor)
	if err != nil {
		t.Fatalf("gate-on cursor: %v", err)
	}
	want, err := legacy.ListSupplyChainImpactFindings(ctx, cursor)
	if err != nil {
		t.Fatalf("legacy cursor: %v", err)
	}
	if multiSupportDescribe(got) != multiSupportDescribe(want) {
		t.Fatalf("cursor after the supplement row differs\nlegacy:  %s\ngate-on: %s", multiSupportDescribe(want), multiSupportDescribe(got))
	}
}

// TestUnscopedGroupedInventoryEqualsTheCountRouteLive holds the unscoped precise
// and image grouped inventory to the legacy count route: DistinctFindings equals
// the legacy total and every named bucket equals the legacy count of that bucket.
func TestUnscopedGroupedInventoryEqualsTheCountRouteLive(t *testing.T) {
	ctx, database := openRouteVDatabase(t)
	store := PostgresGroupedInventoryStore{DB: database, ReadFromWinners: true}
	legacy := PostgresAggregateStore{DB: database}
	buckets := 0
	for _, tc := range routeVFilters {
		if tc.rank {
			continue
		}
		filter := supportsAggregateFilter(routeVListFilter(tc.filter))
		want, err := legacy.CountSupplyChainImpactFindings(ctx, filter)
		if err != nil {
			t.Fatalf("%s: legacy count: %v", tc.name, err)
		}
		for _, dimension := range []GroupedInventoryDimension{GroupedInventoryByService, GroupedInventoryByWorkload, GroupedInventoryByEnvironment} {
			page, err := store.SupplyChainImpactGroupedInventory(ctx, filter, dimension, 50, 0)
			if err != nil {
				t.Fatalf("%s: grouped %s: %v", tc.name, dimension, err)
			}
			if page.DistinctFindings != want.TotalFindings {
				t.Fatalf("%s: %s DistinctFindings = %d, legacy total = %d", tc.name, dimension, page.DistinctFindings, want.TotalFindings)
			}
			for _, row := range page.Rows {
				if row.Unattributed {
					continue
				}
				bucket := filter
				switch dimension {
				case GroupedInventoryByService:
					bucket.ServiceID = row.Value
				case GroupedInventoryByWorkload:
					bucket.WorkloadID = row.Value
				default:
					bucket.Environment = row.Value
				}
				got, err := legacy.CountSupplyChainImpactFindings(ctx, bucket)
				if err != nil || row.Count != got.TotalFindings {
					t.Fatalf("%s: %s bucket %q = %d, legacy count = %d (%v)", tc.name, dimension, row.Value, row.Count, got.TotalFindings, err)
				}
				buckets++
			}
		}
	}
	if buckets == 0 {
		t.Fatal("no named bucket was compared: the matrix lost its signal")
	}
}

// TestPrecisePageStaysOnWinnersWhenTheSupportTableLagsLive documents the two
// staleness cases of ruling V (required proof 6). The winners table is maintained
// by a resweep, so it can lag the supports table, which is current by construction:
//   - a winners row whose winning support is gone still answers branch A with its
//     stored payload until the resweep removes it;
//   - a key with a precise support but no winners row is not returned (branch B
//     starts from winners rows), until the resweep adds the row.
//
// Both are bounded by the winners watermark the route reports.
func TestPrecisePageStaysOnWinnersWhenTheSupportTableLagsLive(t *testing.T) {
	ctx, database := openRouteVDatabase(t)
	gated := PostgresFindingStore{DB: database, ReadFromWinners: true}
	filter := FindingFilter{Limit: 100, Status: "affected_exact", DetectionProfile: ProfilePrecise}
	before, err := gated.ListSupplyChainImpactFindings(ctx, filter)
	if err != nil || len(before) == 0 {
		t.Fatalf("baseline list = %d rows, %v", len(before), err)
	}

	// A winners row whose winning support is deleted from the supports table.
	var winnerFact, key string
	if err := database.QueryRowContext(ctx, `
SELECT w.winner_fact_id, w.canonical_key FROM supply_chain_impact_canonical_winners AS w
WHERE w.finding_id = 'finding:m1'`).Scan(&winnerFact, &key); err != nil {
		t.Fatalf("read the m1 winner: %v", err)
	}
	if _, err := database.ExecContext(ctx, `DELETE FROM supply_chain_impact_finding_supports WHERE fact_id = $1`, winnerFact); err != nil {
		t.Fatalf("delete the winning support: %v", err)
	}
	after, err := gated.ListSupplyChainImpactFindings(ctx, filter)
	if err != nil {
		t.Fatalf("list after the support was deleted: %v", err)
	}
	if !strings.Contains(multiSupportDescribe(after), "m1(") {
		t.Fatalf("a winners row whose support is gone must still answer from the winners table until the resweep: %q", multiSupportDescribe(after))
	}

	// A key with a support and no winners row.
	if _, err := database.ExecContext(ctx, `DELETE FROM supply_chain_impact_canonical_winners WHERE canonical_key = $1`, key); err != nil {
		t.Fatalf("delete the winners row: %v", err)
	}
	gone, err := gated.ListSupplyChainImpactFindings(ctx, filter)
	if err != nil {
		t.Fatalf("list after the winners row was deleted: %v", err)
	}
	if strings.Contains(multiSupportDescribe(gone), "m1(") {
		t.Fatalf("a key with no winners row must not be returned before the resweep adds it: %q", multiSupportDescribe(gone))
	}
}
