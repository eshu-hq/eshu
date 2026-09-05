// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
)

func assertSupplyChainRuntimeContextScopesLive(
	t *testing.T,
	ctx context.Context,
	store impact.PostgresSupplyChainImpactFindingStore,
) {
	t.Helper()
	for _, tc := range []struct {
		name                 string
		access               repositoryAccessFilter
		wantCrossScope       bool
		wantConflictingFacts bool
		wantWorkloadCount    int
	}{
		{
			name:              "scope_a_only",
			access:            repositoryAccessFilter{AllowedScopeIDs: []string{runtimeFilterLiveScopeA}},
			wantWorkloadCount: 6,
		},
		{
			name: "lower_precedence_decoy_repository_grant",
			access: repositoryAccessFilter{
				AllowedRepositoryIDs: []string{runtimeFilterLiveDecoyRepo},
				AllowedScopeIDs:      []string{runtimeFilterLiveScopeA},
			},
			wantWorkloadCount: 6,
		},
		{
			name:                 "canonical_repository_grant",
			access:               repositoryAccessFilter{AllowedRepositoryIDs: []string{runtimeFilterLiveRepository}},
			wantCrossScope:       true,
			wantConflictingFacts: true,
			wantWorkloadCount:    8,
		},
		{
			name:                 "unrestricted",
			access:               repositoryAccessFilter{AllScopes: true},
			wantCrossScope:       true,
			wantConflictingFacts: true,
			wantWorkloadCount:    8,
		},
	} {
		tc := tc
		t.Run("runtime_context_"+tc.name, func(t *testing.T) {
			handler := &SupplyChainHandler{ImpactFindings: store}
			rows := []impact.SupplyChainImpactFindingRow{{RepositoryID: runtimeFilterLiveRepository}}
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
			hasCrossScope := containsAuthString(resolved.ServiceIDs, "service:5747:tenant-b") &&
				containsAuthString(resolved.WorkloadIDs, "workload:5747:tenant-b") &&
				containsAuthString(resolved.Environments, "staging-5747")
			if hasCrossScope != tc.wantCrossScope {
				t.Fatalf("cross-scope context present = %v, want %v: %+v", hasCrossScope, tc.wantCrossScope, resolved)
			}
			hasConflictingFacts := containsAuthString(resolved.WorkloadIDs, "workload:5747:conflicting-payload-scope") &&
				containsAuthString(resolved.ServiceIDs, "service:5747:conflicting-related-scope") &&
				containsAuthString(resolved.Environments, "conflicting-envelope-5747")
			if hasConflictingFacts != tc.wantConflictingFacts {
				t.Fatalf("conflicting-anchor context present = %v, want %v: %+v", hasConflictingFacts, tc.wantConflictingFacts, resolved)
			}
		})
	}
}

func assertSupplyChainConflictingAnchorFiltersLive(
	t *testing.T,
	ctx context.Context,
	findingStore impact.PostgresSupplyChainImpactFindingStore,
	aggregateStore impact.PostgresSupplyChainImpactAggregateStore,
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
			listFilter := impact.SupplyChainImpactFindingFilter{
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

			aggregateFilter := impact.SupplyChainImpactAggregateFilter{
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
			inventory, err := aggregateStore.SupplyChainImpactInventory(
				ctx,
				aggregateFilter,
				impact.SupplyChainImpactInventoryByImpactStatus,
				10,
				0,
			)
			if err != nil {
				t.Fatalf("inventory conflicting anchor: %v", err)
			}
			if len(inventory) != 0 {
				t.Fatalf("inventory conflicting anchor = %#v, want empty", inventory)
			}
			if tc.environment != "" {
				return
			}
			_, err = findingStore.ExplainSupplyChainImpact(ctx, impact.SupplyChainImpactExplanationFilter{
				CVEID:                runtimeFilterLiveCVE,
				PackageID:            runtimeFilterLivePackage,
				WorkloadID:           tc.workloadID,
				ServiceID:            tc.serviceID,
				AllowedRepositoryIDs: []string{runtimeFilterLiveDecoyRepo},
				AllowedScopeIDs:      []string{runtimeFilterLiveScopeA},
			})
			if !errors.Is(err, impact.ErrSupplyChainImpactExplanationNotFound) {
				t.Fatalf("explain conflicting anchor error = %v, want not found", err)
			}
		})
	}
}

func assertSupplyChainStaleBakedFiltersLive(
	t *testing.T,
	ctx context.Context,
	findingStore impact.PostgresSupplyChainImpactFindingStore,
	aggregateStore impact.PostgresSupplyChainImpactAggregateStore,
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
			listFilter := impact.SupplyChainImpactFindingFilter{
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

			aggregateFilter := impact.SupplyChainImpactAggregateFilter{
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
			inventory, err := aggregateStore.SupplyChainImpactInventory(
				ctx,
				aggregateFilter,
				impact.SupplyChainImpactInventoryByImpactStatus,
				10,
				0,
			)
			if err != nil {
				t.Fatalf("inventory stale baked selector: %v", err)
			}
			if len(inventory) != 0 {
				t.Fatalf("inventory stale baked selector = %#v, want empty", inventory)
			}
			if tc.environment != "" {
				return
			}
			_, err = findingStore.ExplainSupplyChainImpact(ctx, impact.SupplyChainImpactExplanationFilter{
				CVEID:           runtimeFilterLiveCVE,
				PackageID:       runtimeFilterLiveBakedPkg,
				WorkloadID:      tc.workloadID,
				ServiceID:       tc.serviceID,
				AllowedScopeIDs: []string{runtimeFilterLiveScopeA},
			})
			if !errors.Is(err, impact.ErrSupplyChainImpactExplanationNotFound) {
				t.Fatalf("explain stale baked selector error = %v, want not found", err)
			}
		})
	}
}
