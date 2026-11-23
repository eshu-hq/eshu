// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10880 PR-B2a disposable PostgreSQL 18 proof of the supports-sourced grouped inventory (service_id, workload_id, environment) for granted calls: repository and scope grants, a central-catalog scope grant with a finding-level hide, filter-independent partial completeness, image_ref and the precise profile served from every copy, bucket counts and distinct findings equal to the legacy count route for each grant shape on the multi-support corpus, and a not-ready supports table refused; reads ESHU_GROUPED_INVENTORY_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_GROUPED_INVENTORY_PROOF_DISPOSABLE ESHU_GROUPED_INVENTORY_PROOF_DSN

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// runSupportsBackfillToCompletion fills the supports table with the real
// marker-gated backfill, so the readiness predicate the store probes is true.
func runSupportsBackfillToCompletion(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	runSupportsBackfillWithChunk(t, ctx, database, 3)
}

// runSupportsBackfillWithChunk is runSupportsBackfillToCompletion with a chunk
// size, so a corpus of tens of thousands of facts fills in a few steps.
func runSupportsBackfillWithChunk(t *testing.T, ctx context.Context, database *sql.DB, chunk int) {
	t.Helper()
	backfill := storagepostgres.SupplyChainImpactSupportsBackfill{DB: storagepostgres.SQLDB{DB: database}, ChunkSize: chunk}
	for step := 0; ; step++ {
		report, err := backfill.RunStep(ctx)
		if err != nil {
			t.Fatalf("backfill step %d: %v", step, err)
		}
		if report.Complete {
			return
		}
		if step > 200 {
			t.Fatal("backfill did not complete")
		}
	}
}

// TestGroupedInventorySupportsServesGrantsLive drives the gate-on grouped store
// on the committed grouped seed with the supports table filled. Every granted
// call reads the supports table; the legacy count route (gate off) is the
// independent oracle for the totals and the buckets.
func TestGroupedInventorySupportsServesGrantsLive(t *testing.T) {
	ctx, database := postgresproof.OpenDisposableBootstrapDatabase(
		t,
		os.Getenv("ESHU_GROUPED_INVENTORY_PROOF_DSN"),
		os.Getenv("ESHU_GROUPED_INVENTORY_PROOF_DISPOSABLE"),
		5*time.Minute,
		func(ctx context.Context, db *sql.DB) error {
			return storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db})
		},
	)
	seedGroupedInventoryLive(t, ctx, database)
	seedGroupedInventoryCentralCatalogLive(t, ctx, database)
	runSupportsBackfillToCompletion(t, ctx, database)

	store := PostgresGroupedInventoryStore{DB: database, ReadFromWinners: true}
	legacy := PostgresAggregateStore{DB: database}
	read := func(t *testing.T, filter AggregateFilter, dimension GroupedInventoryDimension) GroupedInventoryPage {
		t.Helper()
		page, err := store.SupplyChainImpactGroupedInventory(ctx, filter, dimension, 50, 0)
		if err != nil {
			t.Fatalf("SupplyChainImpactGroupedInventory(%s): %v", dimension, err)
		}
		return page
	}
	total := func(t *testing.T, filter AggregateFilter) int {
		t.Helper()
		count, err := legacy.CountSupplyChainImpactFindings(ctx, filter)
		if err != nil {
			t.Fatalf("legacy count: %v", err)
		}
		return count.TotalFindings
	}
	bucketFilter := func(filter AggregateFilter, dimension GroupedInventoryDimension, value string) AggregateFilter {
		switch dimension {
		case GroupedInventoryByService:
			filter.ServiceID = value
		case GroupedInventoryByWorkload:
			filter.WorkloadID = value
		default:
			filter.Environment = value
		}
		return filter
	}

	t.Run("scope grant hides ungranted services and marks spanning buckets partial", func(t *testing.T) {
		scoped := AggregateFilter{AllowedScopeIDs: []string{"git-repository-scope:repository:r1", "git-repository-scope:repository:r2"}}
		page := read(t, scoped, GroupedInventoryByService)
		assertGroupedLiveCounts(t, groupedLiveCounts(page), map[string]int{"service:a": 3, "service:b": 2})
		if got := groupedLiveRow(t, page, "service:a").Completeness; got != GroupCompletenessPartial {
			t.Fatalf("service:a completeness = %q, want partial (repository:r6's scope is outside the grant)", got)
		}
		if got := groupedLiveRow(t, page, "service:b").Completeness; got != GroupCompletenessComplete {
			t.Fatalf("service:b completeness = %q, want complete", got)
		}
		if page.Freshness.State != packet.FreshnessLabelFresh || page.Freshness.LatestObservedAt != "" {
			t.Fatalf("freshness = %+v, want fresh with no winners watermark", page.Freshness)
		}
		if page.DistinctFindings != total(t, scoped) {
			t.Fatalf("DistinctFindings = %d, count route = %d", page.DistinctFindings, total(t, scoped))
		}
		for _, row := range page.Rows {
			if want := total(t, bucketFilter(scoped, GroupedInventoryByService, row.Value)); row.Count != want {
				t.Fatalf("bucket %s = %d, count route = %d", row.Value, row.Count, want)
			}
		}
	})

	t.Run("repository grant marks the same buckets partial", func(t *testing.T) {
		scoped := AggregateFilter{AllowedRepositoryIDs: []string{"repository:r1", "repository:r2"}}
		page := read(t, scoped, GroupedInventoryByService)
		assertGroupedLiveCounts(t, groupedLiveCounts(page), map[string]int{"service:a": 3, "service:b": 2})
		if got := groupedLiveRow(t, page, "service:a").Completeness; got != GroupCompletenessPartial {
			t.Fatalf("service:a completeness = %q, want partial", got)
		}
		if got := groupedLiveRow(t, page, "service:b").Completeness; got != GroupCompletenessComplete {
			t.Fatalf("service:b completeness = %q, want complete", got)
		}
	})

	t.Run("central catalog scope grant marks finding-level hides partial", func(t *testing.T) {
		scoped := AggregateFilter{AllowedScopeIDs: []string{"catalog:central", "git-repository-scope:repository:r7"}}
		page := read(t, scoped, GroupedInventoryByService)
		e := groupedLiveRow(t, page, "service:e")
		if e.Count != 1 || e.Count != total(t, bucketFilter(scoped, GroupedInventoryByService, "service:e")) {
			t.Fatalf("service:e = %d, want 1 (repository:r8's finding scope is not granted)", e.Count)
		}
		if e.Completeness != GroupCompletenessPartial {
			t.Fatalf("service:e completeness = %q, want partial: the grant hides repository:r8's finding", e.Completeness)
		}
		if e.MaxPriorityScore != 65 || e.ByImpactStatus["affected_exact"] != 1 {
			t.Fatalf("service:e = %#v, want only the granted finding f8 in its breakdowns", e)
		}
		// Completeness must not depend on the request filters (a filter-dependent
		// oracle on the hidden finding f9).
		for name, vary := range map[string]func(*AggregateFilter){
			"cve_id of the granted finding":               func(f *AggregateFilter) { f.CVEID = "CVE-2026-f8" },
			"min_priority_score above the hidden finding": func(f *AggregateFilter) { f.MinPriorityScore = 65 },
			"severity of the granted finding":             func(f *AggregateFilter) { f.Severity = "medium" },
		} {
			variant := scoped
			vary(&variant)
			row := groupedLiveRow(t, read(t, variant, GroupedInventoryByService), "service:e")
			if row.Completeness != GroupCompletenessPartial || row.Count != 1 {
				t.Fatalf("%s: service:e = %q count %d, want partial count 1 regardless of the filter", name, row.Completeness, row.Count)
			}
		}
		hidden := read(t, AggregateFilter{AllowedScopeIDs: []string{"catalog:central"}}, GroupedInventoryByService)
		if len(hidden.Rows) != 0 || hidden.DistinctFindings != 0 {
			t.Fatalf("catalog-only grant page = %v (distinct %d), want no bucket", groupedLiveCounts(hidden), hidden.DistinctFindings)
		}
	})

	t.Run("image_ref and the precise profile are served to a granted call", func(t *testing.T) {
		scoped := AggregateFilter{AllowedScopeIDs: []string{"git-repository-scope:repository:r1", "git-repository-scope:repository:r2"}}
		for name, filter := range map[string]AggregateFilter{
			"precise profile":       {DetectionProfile: ProfilePrecise},
			"comprehensive profile": {DetectionProfile: ProfileComprehensive},
			"image_ref":             {ImageRef: "registry.example/none:0"},
		} {
			filter.AllowedScopeIDs = scoped.AllowedScopeIDs
			page := read(t, filter, GroupedInventoryByService)
			if page.DistinctFindings != total(t, filter) {
				t.Fatalf("%s: DistinctFindings = %d, count route = %d", name, page.DistinctFindings, total(t, filter))
			}
		}
	})

	t.Run("unscoped image_ref and precise are served and agree with the count route", func(t *testing.T) {
		// Route V (winners plus the supplement) and the image read replace the typed
		// refusal an unscoped call with these filters used to get. The grouped seed
		// has one support per key, so the legacy count is the exact oracle. The
		// precise route reads the winners table, so the table is rebuilt first.
		if err := storagepostgres.NewSupplyChainImpactWinnersStore(storagepostgres.SQLDB{DB: database}).RebuildAllWinners(ctx); err != nil {
			t.Fatalf("RebuildAllWinners: %v", err)
		}
		for name, filter := range map[string]AggregateFilter{
			"precise profile":   {DetectionProfile: ProfilePrecise},
			"image_ref (none)":  {ImageRef: "registry.example/none:0"},
			"precise and image": {DetectionProfile: ProfilePrecise, ImageRef: "registry.example/none:0"},
		} {
			page := read(t, filter, GroupedInventoryByService)
			if want := total(t, filter); page.DistinctFindings != want {
				t.Fatalf("%s: DistinctFindings = %d, legacy count route = %d", name, page.DistinctFindings, want)
			}
		}
	})

	t.Run("a supports table that is not ready refuses a granted call", func(t *testing.T) {
		if _, err := database.ExecContext(ctx, `INSERT INTO supply_chain_impact_finding_supports_dirty (scope_id) VALUES ('scope:dirty')`); err != nil {
			t.Fatalf("fence: %v", err)
		}
		_, err := store.SupplyChainImpactGroupedInventory(ctx,
			AggregateFilter{AllowedScopeIDs: []string{"git-repository-scope:repository:r1"}}, GroupedInventoryByService, 10, 0)
		if _, restoreErr := database.ExecContext(ctx, `DELETE FROM supply_chain_impact_finding_supports_dirty`); restoreErr != nil {
			t.Fatalf("restore: %v", restoreErr)
		}
		if !errors.Is(err, ErrGroupedInventoryReadModelUnavailable) || !errors.Is(err, ErrFindingSupportsUnavailable) {
			t.Fatalf("not-ready grouped call = %v, want the typed unavailable error naming the supports model", err)
		}
	})
}

// TestGroupedInventorySupportsEqualsTheCountRouteLive holds the granted grouped
// inventory to the legacy count route on the multi-support corpus, where one
// canonical key has copies in several scopes: for every grant shape and every
// non-rank filter, DistinctFindings equals the legacy total and each named
// bucket equals the legacy count for that bucket's own filter. The winners-based
// grouped read could not match here for a scope grant (the pick ran across
// scopes before the grant applied).
func TestGroupedInventorySupportsEqualsTheCountRouteLive(t *testing.T) {
	ctx, database := openSupportsEqualityDatabase(t)
	store := PostgresGroupedInventoryStore{DB: database, ReadFromWinners: true}
	legacy := PostgresAggregateStore{DB: database}
	dimensions := []GroupedInventoryDimension{GroupedInventoryByService, GroupedInventoryByWorkload, GroupedInventoryByEnvironment}

	compared, buckets := 0, 0
	for _, grant := range supportsEqualityGrants() {
		for _, tc := range supportsEqualityFilters() {
			if tc.rank || tc.filter.Limit != 0 {
				continue
			}
			t.Run(grant.name+"/"+tc.name, func(t *testing.T) {
				filter := supportsAggregateFilter(tc.filter)
				filter.AllowedRepositoryIDs, filter.AllowedScopeIDs = grant.repositories, grant.scopes
				want, err := legacy.CountSupplyChainImpactFindings(ctx, filter)
				if err != nil {
					t.Fatalf("legacy count: %v", err)
				}
				for _, dimension := range dimensions {
					page, err := store.SupplyChainImpactGroupedInventory(ctx, filter, dimension, 50, 0)
					if err != nil {
						t.Fatalf("grouped %s: %v", dimension, err)
					}
					if page.DistinctFindings != want.TotalFindings {
						t.Fatalf("%s DistinctFindings = %d, legacy total = %d", dimension, page.DistinctFindings, want.TotalFindings)
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
						if err != nil {
							t.Fatalf("legacy bucket count: %v", err)
						}
						if row.Count != got.TotalFindings {
							t.Fatalf("%s bucket %q = %d, legacy count = %d", dimension, row.Value, row.Count, got.TotalFindings)
						}
						buckets++
					}
				}
				compared++
			})
		}
	}
	if compared == 0 || buckets == 0 {
		t.Fatalf("compared %d cases and %d named buckets: the matrix lost its signal", compared, buckets)
	}
}
