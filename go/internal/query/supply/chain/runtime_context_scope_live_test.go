// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

//eshu:readiness-helper live-postgres-readiness
//eshu:readiness-helper:reason #8835 helper-only source exercised through a pinned enrolled PostgreSQL runtime or suppression root.

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// bootstrapRuntimeFilterSchema makes each runtime proof independent of test order.
func bootstrapRuntimeFilterSchema(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: database}); err != nil {
		t.Fatalf("apply runtime-filter bootstrap schema: %v", err)
	}
}

// runtimeFilterOwnTx hands the open proof transaction to a store that begins and
// commits its own, so the store runs inside the proof's rolled-back transaction,
// the only place its seeded facts are visible. The outer rollback stays in charge.
type runtimeFilterOwnTx struct{ storagepostgres.SQLTx }

func (tx runtimeFilterOwnTx) Begin(context.Context) (db.Transaction, error) { return tx, nil }
func (runtimeFilterOwnTx) Commit() error                                    { return nil }
func (runtimeFilterOwnTx) Rollback() error                                  { return nil }

// fillSupplyChainImpactSupports runs the real supports backfill over the proof's
// own transaction and fails unless it completes. A grant, an image reference or a
// supplement read is served from the per-support table (#10880 PR-B2a) and is
// refused while the table's readiness marker is incomplete, so a proof that seeds
// finding facts and then reads with the winners gate on must fill the supports
// first, as the migration's backfill does in a deployment.
func fillSupplyChainImpactSupports(t *testing.T, ctx context.Context, tx *sql.Tx) {
	t.Helper()
	resetSupplyChainImpactSupportsMarker(t, ctx, tx)
	backfill := storagepostgres.SupplyChainImpactSupportsBackfill{
		DB:        runtimeFilterOwnTx{storagepostgres.SQLTx{Tx: tx}},
		ChunkSize: 50,
	}
	for step := 0; ; step++ {
		report, err := backfill.RunStep(ctx)
		if err != nil {
			t.Fatalf("supports backfill step %d: %v", step, err)
		}
		if report.Complete {
			return
		}
		if step > 200 {
			t.Fatal("supports backfill did not complete")
		}
	}
}

// resetSupplyChainImpactSupportsMarker returns the supports backfill marker to
// "not started" inside the proof's transaction. The database is shared across
// the readiness packages and an earlier proof may have committed a completed
// marker; the rollback restores it. Without the reset the backfill would find the
// marker complete and fill none of the facts this proof just seeded.
func resetSupplyChainImpactSupportsMarker(t *testing.T, ctx context.Context, tx *sql.Tx) {
	t.Helper()
	if _, err := tx.ExecContext(ctx, `
UPDATE supply_chain_impact_finding_supports_backfill
SET completed_at = NULL, cursor_scope_id = '', cursor_generation_id = '', cursor_fact_id = ''
WHERE marker_name = $1`, storagepostgres.SupplyChainImpactSupportsBackfillMarker); err != nil {
		t.Fatalf("reset supports backfill marker: %v", err)
	}
}

// assertGrantReadRefusedWhileSupportsUnfilled pins the fail-closed contract the
// proofs here used to cover by accident: with the winners gate on, a granted call
// is served from the per-support table and is refused with
// impact.ErrFindingSupportsUnavailable while that table's readiness marker is
// incomplete, instead of falling back to the winners row or the fact table.
func assertGrantReadRefusedWhileSupportsUnfilled(
	t *testing.T,
	ctx context.Context,
	tx *sql.Tx,
	filter impact.FindingFilter,
) {
	t.Helper()
	resetSupplyChainImpactSupportsMarker(t, ctx, tx)
	store := impact.NewPostgresFindingStoreWithReadModel(tx, true)
	_, err := store.ListSupplyChainImpactFindings(ctx, filter)
	if !errors.Is(err, impact.ErrFindingSupportsUnavailable) {
		t.Fatalf("grant list on an unfilled supports table error = %v, want ErrFindingSupportsUnavailable", err)
	}
	// The sentinel also wraps a failed readiness probe. The probe wraps its error
	// with this text, so its absence plus a probe that answers "not ready" without
	// an error pins the reset marker as the reason, not a broken probe.
	if strings.Contains(err.Error(), "read supply chain impact supports readiness") {
		t.Fatalf("grant list refused because the readiness probe failed, not because the marker is incomplete: %v", err)
	}
	if ready, probeErr := storagepostgres.SupplyChainImpactSupportsReady(ctx, storagepostgres.SQLTx{Tx: tx}); probeErr != nil || ready {
		t.Fatalf("supports readiness after the marker reset = %v (err %v), want false with no error", ready, probeErr)
	}
}

func assertSupplyChainRuntimeContextScopesLive(
	t *testing.T,
	ctx context.Context,
	store impact.PostgresFindingStore,
) {
	t.Helper()
	for _, tc := range []struct {
		name                 string
		access               querycontract.RepositoryAccessFilter
		wantCrossScope       bool
		wantConflictingFacts bool
		wantWorkloadCount    int
	}{
		{
			name:              "scope_a_only",
			access:            querycontract.RepositoryAccessFilter{AllowedScopeIDs: []string{runtimeFilterLiveScopeA}},
			wantWorkloadCount: 6,
		},
		{
			name: "lower_precedence_decoy_repository_grant",
			access: querycontract.RepositoryAccessFilter{
				AllowedRepositoryIDs: []string{runtimeFilterLiveDecoyRepo},
				AllowedScopeIDs:      []string{runtimeFilterLiveScopeA},
			},
			wantWorkloadCount: 6,
		},
		{
			name:                 "canonical_repository_grant",
			access:               querycontract.RepositoryAccessFilter{AllowedRepositoryIDs: []string{runtimeFilterLiveRepository}},
			wantCrossScope:       true,
			wantConflictingFacts: true,
			wantWorkloadCount:    8,
		},
		{
			name:                 "unrestricted",
			access:               querycontract.RepositoryAccessFilter{AllScopes: true},
			wantCrossScope:       true,
			wantConflictingFacts: true,
			wantWorkloadCount:    8,
		},
	} {
		tc := tc
		t.Run("runtime_context_"+tc.name, func(t *testing.T) {
			handler := &Handler{ImpactFindings: store}
			rows := []impact.FindingRow{{RepositoryID: runtimeFilterLiveRepository}}
			if err := handler.applySupplyChainRuntimeContext(ctx, rows, tc.access); err != nil {
				t.Fatalf("apply runtime context: %v", err)
			}
			resolved := rows[0].RuntimeContext
			if resolved == nil {
				t.Fatal("runtime context = nil, want labeled context")
			}
			if got := len(resolved.WorkloadIDs); got != tc.wantWorkloadCount {
				t.Fatalf("workload count = %d, want %d: %v", got, tc.wantWorkloadCount, resolved.WorkloadIDs)
			}
			hasCrossScope := querycontract.ContainsAuthString(resolved.ServiceIDs, "service:5747:tenant-b") &&
				querycontract.ContainsAuthString(resolved.WorkloadIDs, "workload:5747:tenant-b") &&
				querycontract.ContainsAuthString(resolved.Environments, "staging-5747")
			if hasCrossScope != tc.wantCrossScope {
				t.Fatalf("cross-scope context present = %v, want %v: %+v", hasCrossScope, tc.wantCrossScope, resolved)
			}
			hasConflictingFacts := querycontract.ContainsAuthString(resolved.WorkloadIDs, "workload:5747:conflicting-payload-scope") &&
				querycontract.ContainsAuthString(resolved.ServiceIDs, "service:5747:conflicting-related-scope") &&
				querycontract.ContainsAuthString(resolved.Environments, "conflicting-envelope-5747")
			if hasConflictingFacts != tc.wantConflictingFacts {
				t.Fatalf("conflicting-anchor context present = %v, want %v: %+v", hasConflictingFacts, tc.wantConflictingFacts, resolved)
			}
		})
	}
}

func assertSupplyChainConflictingAnchorFiltersLive(
	t *testing.T,
	ctx context.Context,
	findingStore impact.PostgresFindingStore,
	aggregateStore impact.PostgresAggregateStore,
) {
	t.Helper()
	for _, tc := range []struct {
		name        string
		workloadID  string
		serviceID   string
		environment string
	}{
		{name: "payload_scope", workloadID: "workload:5747:conflicting-payload-scope"},
		{name: "related_scope", serviceID: "service:5747:conflicting-related-scope"},
		{name: "envelope_scope", environment: "conflicting-envelope-5747"},
	} {
		tc := tc
		t.Run("conflicting_anchor_filter_"+tc.name, func(t *testing.T) {
			listFilter := impact.FindingFilter{
				CVEID:                runtimeFilterLiveCVE,
				WorkloadID:           tc.workloadID,
				ServiceID:            tc.serviceID,
				Environment:          tc.environment,
				DetectionProfile:     "comprehensive",
				Limit:                10,
				AllowedRepositoryIDs: []string{runtimeFilterLiveDecoyRepo},
				AllowedScopeIDs:      []string{runtimeFilterLiveScopeA},
			}
			assertSupplyChainRuntimeFilterListCount(t, ctx, findingStore, listFilter, false, 0)
			assertSupplyChainRuntimeFilterListCount(t, ctx, findingStore, listFilter, true, 0)

			aggregateFilter := impact.AggregateFilter{
				CVEID:                runtimeFilterLiveCVE,
				WorkloadID:           tc.workloadID,
				ServiceID:            tc.serviceID,
				Environment:          tc.environment,
				DetectionProfile:     "comprehensive",
				AllowedRepositoryIDs: []string{runtimeFilterLiveDecoyRepo},
				AllowedScopeIDs:      []string{runtimeFilterLiveScopeA},
			}
			count, err := aggregateStore.CountSupplyChainImpactFindings(ctx, aggregateFilter)
			if err != nil {
				t.Fatalf("count conflicting anchor: %v", err)
			}
			if count.TotalFindings != 0 {
				t.Fatalf("count conflicting anchor = %d, want 0", count.TotalFindings)
			}
			inventoryPage, err := aggregateStore.SupplyChainImpactInventory(
				ctx,
				aggregateFilter,
				impact.InventoryByStatus,
				10,
				0,
			)
			inventory := inventoryPage.Rows
			if err != nil {
				t.Fatalf("inventory conflicting anchor: %v", err)
			}
			if len(inventory) != 0 {
				t.Fatalf("inventory conflicting anchor = %#v, want empty", inventory)
			}
			if tc.environment != "" {
				return
			}
			_, err = findingStore.ExplainSupplyChainImpact(ctx, packet.ExplanationFilter{
				CVEID:                runtimeFilterLiveCVE,
				PackageID:            runtimeFilterLivePackage,
				WorkloadID:           tc.workloadID,
				ServiceID:            tc.serviceID,
				AllowedRepositoryIDs: []string{runtimeFilterLiveDecoyRepo},
				AllowedScopeIDs:      []string{runtimeFilterLiveScopeA},
			})
			if !errors.Is(err, impact.ErrExplanationNotFound) {
				t.Fatalf("explain conflicting anchor error = %v, want not found", err)
			}
		})
	}
}

func assertSupplyChainStaleBakedFiltersLive(
	t *testing.T,
	ctx context.Context,
	findingStore impact.PostgresFindingStore,
	aggregateStore impact.PostgresAggregateStore,
) {
	t.Helper()
	for _, tc := range []struct {
		name        string
		workloadID  string
		serviceID   string
		environment string
	}{
		{name: "workload", workloadID: "workload:5747:stale-baked"},
		{name: "service", serviceID: "service:5747:stale-baked"},
		{name: "environment", environment: "stale-baked-5747"},
	} {
		tc := tc
		t.Run("stale_baked_filter_"+tc.name, func(t *testing.T) {
			listFilter := impact.FindingFilter{
				CVEID:            runtimeFilterLiveCVE,
				PackageID:        runtimeFilterLiveBakedPkg,
				WorkloadID:       tc.workloadID,
				ServiceID:        tc.serviceID,
				Environment:      tc.environment,
				DetectionProfile: "comprehensive",
				Limit:            10,
				AllowedScopeIDs:  []string{runtimeFilterLiveScopeA},
			}
			assertSupplyChainRuntimeFilterListCount(t, ctx, findingStore, listFilter, false, 0)
			assertSupplyChainRuntimeFilterListCount(t, ctx, findingStore, listFilter, true, 0)

			aggregateFilter := impact.AggregateFilter{
				CVEID:            runtimeFilterLiveCVE,
				PackageID:        runtimeFilterLiveBakedPkg,
				WorkloadID:       tc.workloadID,
				ServiceID:        tc.serviceID,
				Environment:      tc.environment,
				DetectionProfile: "comprehensive",
				AllowedScopeIDs:  []string{runtimeFilterLiveScopeA},
			}
			count, err := aggregateStore.CountSupplyChainImpactFindings(ctx, aggregateFilter)
			if err != nil {
				t.Fatalf("count stale baked selector: %v", err)
			}
			if count.TotalFindings != 0 {
				t.Fatalf("count stale baked selector = %d, want 0", count.TotalFindings)
			}
			inventoryPage, err := aggregateStore.SupplyChainImpactInventory(
				ctx,
				aggregateFilter,
				impact.InventoryByStatus,
				10,
				0,
			)
			inventory := inventoryPage.Rows
			if err != nil {
				t.Fatalf("inventory stale baked selector: %v", err)
			}
			if len(inventory) != 0 {
				t.Fatalf("inventory stale baked selector = %#v, want empty", inventory)
			}
			if tc.environment != "" {
				return
			}
			_, err = findingStore.ExplainSupplyChainImpact(ctx, packet.ExplanationFilter{
				CVEID:           runtimeFilterLiveCVE,
				PackageID:       runtimeFilterLiveBakedPkg,
				WorkloadID:      tc.workloadID,
				ServiceID:       tc.serviceID,
				AllowedScopeIDs: []string{runtimeFilterLiveScopeA},
			})
			if !errors.Is(err, impact.ErrExplanationNotFound) {
				t.Fatalf("explain stale baked selector error = %v, want not found", err)
			}
		})
	}
}
