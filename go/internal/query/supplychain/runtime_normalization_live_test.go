// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
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
	runtimeNormalizedObjectWorkload = "workload:5747:object-key"
	runtimeMalformedObjectWorkload  = `{"workload:5747:object-direct": true}`
	runtimeMalformedArrayService    = `["service:5747:array-direct"]`
	runtimeMalformedObjectEnv       = `{"environment:5747:object-direct": true}`
	runtimeMalformedRepoWorkload    = "workload:5747:repository-fallback"
	runtimeMalformedRepoService     = "service:5747:repo-fallback"
	runtimeMalformedScopeEnv        = "environment-5747-scope-fallback"
)

func assertSupplyChainRuntimeNormalizationLive(
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
	t.Run("runtime_normalization_object_entity_keys", func(t *testing.T) {
		assertSupplyChainRuntimeNormalizationFilterLive(
			t,
			ctx,
			findingStore,
			aggregateStore,
			runtimeNormalizedObjectWorkload,
			"",
			"",
			0,
		)
	})
	for _, tc := range []struct {
		name        string
		workloadID  string
		serviceID   string
		environment string
	}{
		{name: "object_workload_id", workloadID: runtimeMalformedObjectWorkload},
		{name: "array_service_id", serviceID: runtimeMalformedArrayService},
		{name: "object_environment", environment: runtimeMalformedObjectEnv},
		{name: "numeric_workload_id", workloadID: "5747"},
		{name: "boolean_service_id", serviceID: "true"},
		{name: "boolean_environment", environment: "false"},
	} {
		tc := tc
		t.Run("runtime_normalization_rejects_"+tc.name, func(t *testing.T) {
			assertSupplyChainRuntimeNormalizationFilterLive(
				t,
				ctx,
				findingStore,
				aggregateStore,
				tc.workloadID,
				tc.serviceID,
				tc.environment,
				0,
			)
		})
	}
	for _, tc := range []struct {
		name        string
		workloadID  string
		serviceID   string
		environment string
	}{
		{name: "object_repository_id", workloadID: runtimeMalformedRepoWorkload},
		{name: "array_repo_id", serviceID: runtimeMalformedRepoService},
		{name: "object_scope_id", environment: runtimeMalformedScopeEnv},
	} {
		tc := tc
		t.Run("runtime_normalization_falls_back_from_"+tc.name, func(t *testing.T) {
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

	contextStore := impact.NewPostgresSupplyChainImpactFindingStore(findingStore.DB)
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
	if slices.Contains(runtimeContext.WorkloadIDs, runtimeNormalizedObjectWorkload) {
		t.Errorf("runtime context workloads = %v, contains object entity key", runtimeContext.WorkloadIDs)
	}
	for _, malformed := range []string{
		"map[workload:5747:object-direct:true]",
		"5747",
	} {
		if slices.Contains(runtimeContext.WorkloadIDs, malformed) {
			t.Errorf("runtime context workloads = %v, contains malformed %q", runtimeContext.WorkloadIDs, malformed)
		}
	}
	if !slices.Contains(runtimeContext.WorkloadIDs, runtimeMalformedRepoWorkload) {
		t.Errorf("runtime context workloads = %v, missing repository fallback", runtimeContext.WorkloadIDs)
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
	for _, malformed := range []string{"[service:5747:array-direct]", "true"} {
		if slices.Contains(runtimeContext.ServiceIDs, malformed) {
			t.Errorf("runtime context services = %v, contains malformed %q", runtimeContext.ServiceIDs, malformed)
		}
	}
	if !slices.Contains(runtimeContext.ServiceIDs, runtimeMalformedRepoService) {
		t.Errorf("runtime context services = %v, missing repo fallback", runtimeContext.ServiceIDs)
	}
	if !slices.Contains(runtimeContext.Environments, runtimeNormalizedEnvironment) {
		t.Errorf(
			"runtime context environments = %v, missing %q",
			runtimeContext.Environments,
			runtimeNormalizedEnvironment,
		)
	}
	for _, malformed := range []string{"map[environment:5747:object-direct:true]", "false"} {
		if slices.Contains(runtimeContext.Environments, malformed) {
			t.Errorf("runtime context environments = %v, contains malformed %q", runtimeContext.Environments, malformed)
		}
	}
	if !slices.Contains(runtimeContext.Environments, runtimeMalformedScopeEnv) {
		t.Errorf("runtime context environments = %v, missing scope fallback", runtimeContext.Environments)
	}
}

func assertSupplyChainRuntimeNormalizationFilterLive(
	t *testing.T,
	ctx context.Context,
	findingStore impact.PostgresSupplyChainImpactFindingStore,
	aggregateStore impact.PostgresSupplyChainImpactAggregateStore,
	workloadID string,
	serviceID string,
	environment string,
	want int,
) {
	t.Helper()
	filter := impact.SupplyChainImpactFindingFilter{
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

	aggregateFilter := impact.SupplyChainImpactAggregateFilter{
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
		impact.SupplyChainImpactInventoryByImpactStatus,
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
	_, err = findingStore.ExplainSupplyChainImpact(ctx, impact.SupplyChainImpactExplanationFilter{
		CVEID:           runtimeFilterLiveCVE,
		PackageID:       runtimeFilterLivePackage,
		WorkloadID:      workloadID,
		ServiceID:       serviceID,
		AllowedScopeIDs: []string{runtimeFilterLiveScopeA},
	})
	if want == 0 && !errors.Is(err, impact.ErrSupplyChainImpactExplanationNotFound) {
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
			kind:   impact.WorkloadIdentityFactKindQuery,
			payload: map[string]any{
				"repository_id": runtimeFilterLiveRepository,
				"workload_id":   "  " + runtimeNormalizedPaddedWorkload + "  ",
			},
		},
		{
			factID: "fact:5747:normalized:padded-array",
			kind:   impact.WorkloadIdentityFactKindQuery,
			payload: map[string]any{
				"repository_id": runtimeFilterLiveRepository,
				"entity_keys":   []string{"  " + runtimeNormalizedArrayWorkload + "  "},
			},
		},
		{
			factID: "fact:5747:normalized:scalar-entity",
			kind:   impact.WorkloadIdentityFactKindQuery,
			payload: map[string]any{
				"repository_id": runtimeFilterLiveRepository,
				"entity_keys":   "  " + runtimeNormalizedScalarWorkload + "  ",
			},
		},
		{
			factID: "fact:5747:normalized:object-entity",
			kind:   impact.WorkloadIdentityFactKindQuery,
			payload: map[string]any{
				"repository_id": runtimeFilterLiveRepository,
				"entity_keys": map[string]any{
					runtimeNormalizedObjectWorkload: true,
				},
			},
		},
		{
			factID: "fact:5747:normalized:padded-service",
			kind:   runtimeFilterLiveServiceCatalogFactKind,
			payload: map[string]any{
				"repository_id": runtimeFilterLiveRepository,
				"service_id":    "  " + runtimeNormalizedService + "  ",
				"outcome":       "exact",
			},
		},
		{
			factID: "fact:5747:normalized:padded-environment",
			kind:   cloudRuntimeProbeTestCICDFactKind,
			payload: map[string]any{
				"repository_id": runtimeFilterLiveRepository,
				"environment":   "  " + runtimeNormalizedEnvironment + "  ",
				"outcome":       "exact",
			},
		},
		{
			factID: "fact:5747:normalized:false-provenance",
			kind:   runtimeFilterLiveServiceCatalogFactKind,
			payload: map[string]any{
				"repository_id":   runtimeFilterLiveRepository,
				"service_id":      runtimeNormalizedFalseService,
				"outcome":         "exact",
				"provenance_only": " false ",
			},
		},
		{
			factID: "fact:5747:normalized:blank-provenance",
			kind:   runtimeFilterLiveServiceCatalogFactKind,
			payload: map[string]any{
				"repository_id":   runtimeFilterLiveRepository,
				"service_id":      runtimeNormalizedBlankService,
				"outcome":         "exact",
				"provenance_only": " ",
			},
		},
		{
			factID: "fact:5747:normalized:true-provenance",
			kind:   runtimeFilterLiveServiceCatalogFactKind,
			payload: map[string]any{
				"repository_id":   runtimeFilterLiveRepository,
				"service_id":      runtimeNormalizedTrueService,
				"outcome":         "exact",
				"provenance_only": " TRUE ",
			},
		},
		{
			factID: "fact:5747:malformed:object-workload",
			kind:   impact.WorkloadIdentityFactKindQuery,
			payload: map[string]any{
				"repository_id": runtimeFilterLiveRepository,
				"workload_id": map[string]any{
					"workload:5747:object-direct": true,
				},
			},
		},
		{
			factID: "fact:5747:malformed:array-service",
			kind:   runtimeFilterLiveServiceCatalogFactKind,
			payload: map[string]any{
				"repository_id": runtimeFilterLiveRepository,
				"service_id":    []any{"service:5747:array-direct"},
				"outcome":       "exact",
			},
		},
		{
			factID: "fact:5747:malformed:object-environment",
			kind:   cloudRuntimeProbeTestCICDFactKind,
			payload: map[string]any{
				"repository_id": runtimeFilterLiveRepository,
				"environment": map[string]any{
					"environment:5747:object-direct": true,
				},
				"outcome": "exact",
			},
		},
		{
			factID: "fact:5747:malformed:numeric-workload",
			kind:   impact.WorkloadIdentityFactKindQuery,
			payload: map[string]any{
				"repository_id": runtimeFilterLiveRepository,
				"workload_id":   5747,
			},
		},
		{
			factID: "fact:5747:malformed:boolean-service",
			kind:   runtimeFilterLiveServiceCatalogFactKind,
			payload: map[string]any{
				"repository_id": runtimeFilterLiveRepository,
				"service_id":    true,
				"outcome":       "exact",
			},
		},
		{
			factID: "fact:5747:malformed:boolean-environment",
			kind:   cloudRuntimeProbeTestCICDFactKind,
			payload: map[string]any{
				"repository_id": runtimeFilterLiveRepository,
				"environment":   false,
				"outcome":       "exact",
			},
		},
		{
			factID: "fact:5747:malformed:repository-object",
			kind:   impact.WorkloadIdentityFactKindQuery,
			payload: map[string]any{
				"repository_id":     map[string]any{"repository:decoy": true},
				"related_scope_ids": []string{runtimeFilterLiveRepository},
				"workload_id":       runtimeMalformedRepoWorkload,
			},
		},
		{
			factID: "fact:5747:malformed:repo-array",
			kind:   runtimeFilterLiveServiceCatalogFactKind,
			payload: map[string]any{
				"repo_id":           []any{"repository:decoy"},
				"related_scope_ids": []string{runtimeFilterLiveRepository},
				"service_id":        runtimeMalformedRepoService,
				"outcome":           "exact",
			},
		},
		{
			factID: "fact:5747:malformed:scope-object",
			kind:   cloudRuntimeProbeTestCICDFactKind,
			payload: map[string]any{
				"scope_id":          map[string]any{"scope:decoy": true},
				"related_scope_ids": []string{runtimeFilterLiveRepository},
				"environment":       runtimeMalformedScopeEnv,
				"outcome":           "exact",
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
