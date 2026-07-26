// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"
)

const (
	runtimeNormalizedPaddedWorkload = "workload:5747:padded-scalar"
	runtimeNormalizedArrayWorkload  = "workload:5747:padded-array"
	runtimeNormalizedScalarWorkload = "workload:5747:scalar-entity-key"
	runtimeNormalizedService        = "service:5747:padded"
	runtimeNormalizedEnvironment    = "normalized-environment-5747"
	runtimeNormalizedFalseService   = "service:5747:provenance-false"
	runtimeNormalizedBlankService   = "service:5747:provenance-blank"
	runtimeNormalizedTrueService    = "service:5747:provenance-string-true"
)

func assertSupplyChainRuntimeNormalizationLive(
	t *testing.T,
	ctx context.Context,
	findingStore PostgresSupplyChainImpactFindingStore,
	aggregateStore PostgresSupplyChainImpactAggregateStore,
) {
	t.Helper()

	for _, tc := range []struct {
		name        string
		workloadID  string
		serviceID   string
		environment string
	}{
		{name: "padded_workload_scalar", workloadID: runtimeNormalizedPaddedWorkload},
		{name: "padded_workload_entity_array", workloadID: runtimeNormalizedArrayWorkload},
		{name: "scalar_workload_entity_key", workloadID: runtimeNormalizedScalarWorkload},
		{name: "padded_service_scalar", serviceID: runtimeNormalizedService},
		{name: "padded_environment_scalar", environment: runtimeNormalizedEnvironment},
		{name: "string_false_provenance", serviceID: runtimeNormalizedFalseService},
		{name: "blank_provenance", serviceID: runtimeNormalizedBlankService},
	} {
		tc := tc
		t.Run("runtime_normalization_"+tc.name, func(t *testing.T) {
			assertSupplyChainRuntimeNormalizationFilterLive(
				t,
				ctx,
				findingStore,
				aggregateStore,
				tc.workloadID,
				tc.serviceID,
				tc.environment,
				1,
			)
		})
	}

	t.Run("runtime_normalization_string_true_provenance", func(t *testing.T) {
		assertSupplyChainRuntimeNormalizationFilterLive(
			t,
			ctx,
			findingStore,
			aggregateStore,
			"",
			runtimeNormalizedTrueService,
			"",
			0,
		)
	})

	contextStore := NewPostgresSupplyChainImpactFindingStore(findingStore.DB)
	contexts, err := contextStore.ListSupplyChainImpactRuntimeContext(
		ctx,
		[]string{runtimeFilterLiveRepository},
		[]string{runtimeFilterLiveRepository},
		[]string{runtimeFilterLiveScopeA},
	)
	if err != nil {
		t.Fatalf("load normalized runtime context: %v", err)
	}
	runtimeContext := contexts[runtimeFilterLiveRepository]
	for _, workloadID := range []string{
		runtimeNormalizedPaddedWorkload,
		runtimeNormalizedArrayWorkload,
		runtimeNormalizedScalarWorkload,
	} {
		if !slices.Contains(runtimeContext.WorkloadIDs, workloadID) {
			t.Errorf("runtime context workloads = %v, missing %q", runtimeContext.WorkloadIDs, workloadID)
		}
	}
	for _, serviceID := range []string{
		runtimeNormalizedService,
		runtimeNormalizedFalseService,
		runtimeNormalizedBlankService,
	} {
		if !slices.Contains(runtimeContext.ServiceIDs, serviceID) {
			t.Errorf("runtime context services = %v, missing %q", runtimeContext.ServiceIDs, serviceID)
		}
	}
	if slices.Contains(runtimeContext.ServiceIDs, runtimeNormalizedTrueService) {
		t.Errorf("runtime context services = %v, contains string-true provenance", runtimeContext.ServiceIDs)
	}
	if !slices.Contains(runtimeContext.Environments, runtimeNormalizedEnvironment) {
		t.Errorf(
			"runtime context environments = %v, missing %q",
			runtimeContext.Environments,
			runtimeNormalizedEnvironment,
		)
	}
}

func assertSupplyChainRuntimeNormalizationFilterLive(
	t *testing.T,
	ctx context.Context,
	findingStore PostgresSupplyChainImpactFindingStore,
	aggregateStore PostgresSupplyChainImpactAggregateStore,
	workloadID string,
	serviceID string,
	environment string,
	want int,
) {
	t.Helper()
	filter := SupplyChainImpactFindingFilter{
		CVEID:            runtimeFilterLiveCVE,
		WorkloadID:       workloadID,
		ServiceID:        serviceID,
		Environment:      environment,
		DetectionProfile: "comprehensive",
		Limit:            10,
		AllowedScopeIDs:  []string{runtimeFilterLiveScopeA},
	}
	assertSupplyChainRuntimeFilterListCount(t, ctx, findingStore, filter, false, want)
	assertSupplyChainRuntimeFilterListCount(t, ctx, findingStore, filter, true, want)

	aggregateFilter := SupplyChainImpactAggregateFilter{
		CVEID:            runtimeFilterLiveCVE,
		WorkloadID:       workloadID,
		ServiceID:        serviceID,
		Environment:      environment,
		DetectionProfile: "comprehensive",
		AllowedScopeIDs:  []string{runtimeFilterLiveScopeA},
	}
	count, err := aggregateStore.CountSupplyChainImpactFindings(ctx, aggregateFilter)
	if err != nil {
		t.Fatalf("count normalized runtime filter: %v", err)
	}
	if count.TotalFindings != want {
		t.Fatalf("normalized runtime count = %d, want %d", count.TotalFindings, want)
	}
	inventory, err := aggregateStore.SupplyChainImpactInventory(
		ctx,
		aggregateFilter,
		SupplyChainImpactInventoryByImpactStatus,
		10,
		0,
	)
	if err != nil {
		t.Fatalf("inventory normalized runtime filter: %v", err)
	}
	if want == 0 && len(inventory) != 0 {
		t.Fatalf("normalized runtime inventory = %#v, want empty", inventory)
	}
	if want == 1 && (len(inventory) != 1 || inventory[0].Count != 1) {
		t.Fatalf("normalized runtime inventory = %#v, want one finding", inventory)
	}
	if environment != "" {
		return
	}
	_, err = findingStore.ExplainSupplyChainImpact(ctx, SupplyChainImpactExplanationFilter{
		CVEID:           runtimeFilterLiveCVE,
		PackageID:       runtimeFilterLivePackage,
		WorkloadID:      workloadID,
		ServiceID:       serviceID,
		AllowedScopeIDs: []string{runtimeFilterLiveScopeA},
	})
	if want == 0 && !errors.Is(err, ErrSupplyChainImpactExplanationNotFound) {
		t.Fatalf("explain normalized runtime filter error = %v, want not found", err)
	}
	if want == 1 && err != nil {
		t.Fatalf("explain normalized runtime filter: %v", err)
	}
}

func seedSupplyChainRuntimeNormalizationLiveFacts(
	t *testing.T,
	ctx context.Context,
	tx *sql.Tx,
) {
	t.Helper()
	for _, fact := range []struct {
		factID  string
		kind    string
		payload map[string]any
	}{
		{
			factID: "fact:5747:normalized:padded-workload",
			kind:   workloadIdentityFactKindQuery,
			payload: map[string]any{
				"repository_id": runtimeFilterLiveRepository,
				"workload_id":   "  " + runtimeNormalizedPaddedWorkload + "  ",
			},
		},
		{
			factID: "fact:5747:normalized:padded-array",
			kind:   workloadIdentityFactKindQuery,
			payload: map[string]any{
				"repository_id": runtimeFilterLiveRepository,
				"entity_keys":   []string{"  " + runtimeNormalizedArrayWorkload + "  "},
			},
		},
		{
			factID: "fact:5747:normalized:scalar-entity",
			kind:   workloadIdentityFactKindQuery,
			payload: map[string]any{
				"repository_id": runtimeFilterLiveRepository,
				"entity_keys":   "  " + runtimeNormalizedScalarWorkload + "  ",
			},
		},
		{
			factID: "fact:5747:normalized:padded-service",
			kind:   serviceCatalogCorrelationFactKind,
			payload: map[string]any{
				"repository_id": runtimeFilterLiveRepository,
				"service_id":    "  " + runtimeNormalizedService + "  ",
				"outcome":       "exact",
			},
		},
		{
			factID: "fact:5747:normalized:padded-environment",
			kind:   cicdRunCorrelationFactKind,
			payload: map[string]any{
				"repository_id": runtimeFilterLiveRepository,
				"environment":   "  " + runtimeNormalizedEnvironment + "  ",
				"outcome":       "exact",
			},
		},
		{
			factID: "fact:5747:normalized:false-provenance",
			kind:   serviceCatalogCorrelationFactKind,
			payload: map[string]any{
				"repository_id":   runtimeFilterLiveRepository,
				"service_id":      runtimeNormalizedFalseService,
				"outcome":         "exact",
				"provenance_only": " false ",
			},
		},
		{
			factID: "fact:5747:normalized:blank-provenance",
			kind:   serviceCatalogCorrelationFactKind,
			payload: map[string]any{
				"repository_id":   runtimeFilterLiveRepository,
				"service_id":      runtimeNormalizedBlankService,
				"outcome":         "exact",
				"provenance_only": " ",
			},
		},
		{
			factID: "fact:5747:normalized:true-provenance",
			kind:   serviceCatalogCorrelationFactKind,
			payload: map[string]any{
				"repository_id":   runtimeFilterLiveRepository,
				"service_id":      runtimeNormalizedTrueService,
				"outcome":         "exact",
				"provenance_only": " TRUE ",
			},
		},
	} {
		insertSupplyChainRuntimeFilterFact(
			t,
			ctx,
			tx,
			fact.factID,
			runtimeFilterLiveScopeA,
			runtimeFilterLiveGenA,
			fact.kind,
			false,
			fact.payload,
		)
	}
}
