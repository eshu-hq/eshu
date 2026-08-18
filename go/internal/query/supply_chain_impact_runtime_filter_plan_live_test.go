// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

const runtimeFilterHighCardinalityEnvironment = "shared-production-5747"

func TestSupplyChainImpactRuntimeFilterPlansLive(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live #5747 query-plan proof")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	seedSupplyChainRuntimeFilterLiveFacts(t, ctx, tx)
	writeStarted := time.Now()
	for _, tc := range []struct {
		kind       string
		factPrefix string
		payloadSQL string
	}{
		{
			kind:       workloadIdentityFactKindQuery,
			factPrefix: "workload",
			payloadSQL: `jsonb_build_object(
			  'repository_id', 'repository:5747:perf:' || sample,
			  'workload_id', 'workload:5747:perf:' || sample,
			  'entity_keys', jsonb_build_array('workload:5747:perf-entity:' || sample)
			)`,
		},
		{
			kind:       serviceCatalogCorrelationFactKind,
			factPrefix: "service",
			payloadSQL: `jsonb_build_object(
			  'repository_id', 'repository:5747:perf:' || sample,
			  'service_id', 'service:5747:perf:' || sample,
			  'outcome', 'exact'
			)`,
		},
		{
			kind:       cicdRunCorrelationFactKind,
			factPrefix: "environment",
			payloadSQL: `jsonb_build_object(
			  'repository_id', CASE
			    WHEN sample = 1 THEN '` + runtimeFilterLiveRepository + `'
			    ELSE 'repository:5747:perf:' || sample
			  END,
			  'environment', '` + runtimeFilterHighCardinalityEnvironment + `',
			  'outcome', 'exact'
			)`,
		},
	} {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  source_system, source_fact_key, observed_at, ingested_at, payload
)
SELECT
  'fact:5747:perf:`+tc.factPrefix+`:' || sample,
  $1,
  $2,
  $3,
  'stable:5747:perf:`+tc.factPrefix+`:' || sample,
  'synthetic',
  'source:5747:perf:`+tc.factPrefix+`:' || sample,
  NOW(),
  NOW(),
  `+tc.payloadSQL+`
FROM generate_series(1, 100000) AS sample`,
			runtimeFilterLiveScopeA,
			runtimeFilterLiveGenA,
			tc.kind,
		); err != nil {
			t.Fatalf("insert 100k %s facts: %v", tc.factPrefix, err)
		}
	}
	writeElapsed := time.Since(writeStarted)
	t.Logf("inserted 300000 indexed runtime facts in %s", writeElapsed)
	if writeElapsed > 2*time.Minute {
		t.Fatalf("indexed runtime-fact write took %s, want <= 2m", writeElapsed)
	}
	if _, err := tx.ExecContext(ctx, "ANALYZE fact_records"); err != nil {
		t.Fatalf("analyze fact_records: %v", err)
	}

	contextCandidates := make([]string, 0, 200)
	for sample := 1; sample <= 200; sample++ {
		contextCandidates = append(contextCandidates, fmt.Sprintf("repository:5747:perf:%d", sample))
	}
	contextStore := NewPostgresSupplyChainImpactFindingStore(tx)
	contexts, err := contextStore.ListSupplyChainImpactRuntimeContext(
		ctx,
		contextCandidates,
		contextCandidates,
		nil,
	)
	if err != nil {
		t.Fatalf("list 200-candidate runtime context: %v", err)
	}
	if len(contexts) != len(contextCandidates) {
		t.Fatalf("runtime context repository count = %d, want %d", len(contexts), len(contextCandidates))
	}
	explainSupplyChainRuntimeFilterPlan(
		t,
		ctx,
		tx,
		"runtime_context_200_candidates",
		selectSupplyChainImpactRuntimeContextQuery,
		pgarray.Array(supplyChainImpactRuntimeContextFactKinds),
		pgarray.Array(contextCandidates),
		pgarray.Array(contextCandidates),
		pgarray.Array([]string{}),
	)

	baseFilter := SupplyChainImpactFindingFilter{
		CVEID:                runtimeFilterLiveCVE,
		WorkloadID:           "workload:5747:scalar",
		ServiceID:            "service:5747:allowed",
		Environment:          "production-5747",
		DetectionProfile:     "comprehensive",
		Limit:                10,
		AllowedRepositoryIDs: []string{runtimeFilterLiveRepository},
	}
	for _, tc := range []struct {
		name        string
		query       string
		filter      SupplyChainImpactFindingFilter
		wantIndexes []string
	}{
		{
			name:  "legacy_workload_scalar",
			query: listSupplyChainImpactFindingsQuery,
			filter: withSupplyChainRuntimeFilterDimensions(
				baseFilter,
				"workload:5747:scalar",
				"",
				"",
			),
			wantIndexes: []string{"fact_records_workload_identity_workload_idx"},
		},
		{
			name:  "legacy_workload_entity_key",
			query: listSupplyChainImpactFindingsQuery,
			filter: withSupplyChainRuntimeFilterDimensions(
				baseFilter,
				"workload:5747:entity-key",
				"",
				"",
			),
			wantIndexes: []string{"fact_records_workload_identity_entity_keys_idx"},
		},
		{
			name:  "legacy_service",
			query: listSupplyChainImpactFindingsQuery,
			filter: withSupplyChainRuntimeFilterDimensions(
				baseFilter,
				"",
				"service:5747:allowed",
				"",
			),
			wantIndexes: []string{"fact_records_service_catalog_correlations_service_idx"},
		},
		{
			name:  "legacy_environment",
			query: listSupplyChainImpactFindingsQuery,
			filter: withSupplyChainRuntimeFilterDimensions(
				baseFilter,
				"",
				"",
				"production-5747",
			),
			wantIndexes: []string{"fact_records_ci_cd_run_correlations_environment_lookup_idx"},
		},
		{
			name:  "legacy_environment_high_cardinality",
			query: listSupplyChainImpactFindingsQuery,
			filter: withSupplyChainRuntimeFilterDimensions(
				baseFilter,
				"",
				"",
				runtimeFilterHighCardinalityEnvironment,
			),
			wantIndexes: []string{"fact_records_ci_cd_run_correlations_environment_lookup_idx"},
		},
		{
			name:   "legacy_combined",
			query:  listSupplyChainImpactFindingsQuery,
			filter: baseFilter,
			wantIndexes: []string{
				"fact_records_workload_identity_workload_idx",
				"fact_records_service_catalog_correlations_service_idx",
				"fact_records_ci_cd_run_correlations_environment_lookup_idx",
			},
		},
		{
			name:   "winners_combined",
			query:  listSupplyChainImpactFindingsFromWinnersQuery,
			filter: baseFilter,
			wantIndexes: []string{
				"fact_records_workload_identity_workload_idx",
				"fact_records_service_catalog_correlations_service_idx",
				"fact_records_ci_cd_run_correlations_environment_lookup_idx",
			},
		},
		{
			name:  "winners_environment_high_cardinality",
			query: listSupplyChainImpactFindingsFromWinnersQuery,
			filter: withSupplyChainRuntimeFilterDimensions(
				baseFilter,
				"",
				"",
				runtimeFilterHighCardinalityEnvironment,
			),
			wantIndexes: []string{"fact_records_ci_cd_run_correlations_environment_lookup_idx"},
		},
	} {
		plan := explainSupplyChainRuntimeFilterPlan(
			t,
			ctx,
			tx,
			tc.name,
			tc.query,
			supplyChainRuntimeFilterListArgs(tc.filter)...,
		)
		for _, wantIndex := range tc.wantIndexes {
			if !strings.Contains(plan, wantIndex) {
				t.Fatalf("%s plan missing index %q:\n%s", tc.name, wantIndex, plan)
			}
		}
		if strings.Contains(tc.name, "environment_high_cardinality") {
			assertSupplyChainRuntimePlanIndexRows(
				t,
				plan,
				"fact_records_ci_cd_run_correlations_environment_lookup_idx",
				100000,
			)
			assertSupplyChainRuntimePlanOutputRows(t, plan, 1)
		}
	}

	combinedAggregate := SupplyChainImpactAggregateFilter{
		CVEID:                runtimeFilterLiveCVE,
		WorkloadID:           "workload:5747:scalar",
		ServiceID:            "service:5747:allowed",
		Environment:          "production-5747",
		DetectionProfile:     "comprehensive",
		AllowedRepositoryIDs: []string{runtimeFilterLiveRepository},
	}
	aggregatePlan := explainSupplyChainRuntimeFilterPlan(
		t,
		ctx,
		tx,
		"aggregate_combined",
		supplyChainImpactAggregateCountQuery,
		supplyChainRuntimeFilterAggregateArgs(combinedAggregate)...,
	)
	for _, wantIndex := range []string{
		"fact_records_workload_identity_workload_idx",
		"fact_records_service_catalog_correlations_service_idx",
		"fact_records_ci_cd_run_correlations_environment_lookup_idx",
	} {
		if !strings.Contains(aggregatePlan, wantIndex) {
			t.Fatalf("aggregate plan missing index %q:\n%s", wantIndex, aggregatePlan)
		}
	}

	highCardinalityAggregate := SupplyChainImpactAggregateFilter{
		CVEID:                runtimeFilterLiveCVE,
		Environment:          runtimeFilterHighCardinalityEnvironment,
		DetectionProfile:     "comprehensive",
		AllowedRepositoryIDs: []string{runtimeFilterLiveRepository},
	}
	highCardinalityCount, err := NewPostgresSupplyChainImpactAggregateStore(tx).
		CountSupplyChainImpactFindings(ctx, highCardinalityAggregate)
	if err != nil {
		t.Fatalf("count high-cardinality environment findings: %v", err)
	}
	if highCardinalityCount.TotalFindings != 1 {
		t.Fatalf("high-cardinality environment findings = %d, want 1", highCardinalityCount.TotalFindings)
	}
	highCardinalityAggregatePlan := explainSupplyChainRuntimeFilterPlan(
		t,
		ctx,
		tx,
		"aggregate_environment_high_cardinality",
		supplyChainImpactAggregateCountQuery,
		supplyChainRuntimeFilterAggregateArgs(highCardinalityAggregate)...,
	)
	if !strings.Contains(
		highCardinalityAggregatePlan,
		"fact_records_ci_cd_run_correlations_environment_lookup_idx",
	) {
		t.Fatalf("high-cardinality aggregate plan missing environment index:\n%s", highCardinalityAggregatePlan)
	}
	assertSupplyChainRuntimePlanIndexRows(
		t,
		highCardinalityAggregatePlan,
		"fact_records_ci_cd_run_correlations_environment_lookup_idx",
		100000,
	)

	highCardinalityInventoryArgs := append(
		supplyChainRuntimeFilterAggregateArgs(highCardinalityAggregate),
		10,
		0,
	)
	highCardinalityInventoryPlan := explainSupplyChainRuntimeFilterPlan(
		t,
		ctx,
		tx,
		"inventory_environment_high_cardinality",
		supplyChainImpactInventoryQuery("COALESCE(fact.payload->>'impact_status', 'unknown')"),
		highCardinalityInventoryArgs...,
	)
	if !strings.Contains(
		highCardinalityInventoryPlan,
		"fact_records_ci_cd_run_correlations_environment_lookup_idx",
	) {
		t.Fatalf("high-cardinality inventory plan missing environment index:\n%s", highCardinalityInventoryPlan)
	}
	assertSupplyChainRuntimePlanIndexRows(
		t,
		highCardinalityInventoryPlan,
		"fact_records_ci_cd_run_correlations_environment_lookup_idx",
		100000,
	)
	assertSupplyChainRuntimePlanOutputRows(t, highCardinalityInventoryPlan, 1)

	noFilterPlan := explainSupplyChainRuntimeFilterPlan(
		t,
		ctx,
		tx,
		"aggregate_no_runtime_filter",
		supplyChainImpactAggregateCountQuery,
		supplyChainRuntimeFilterAggregateArgs(SupplyChainImpactAggregateFilter{})...,
	)
	for _, unexpectedIndex := range []string{
		"fact_records_workload_identity_workload_idx",
		"fact_records_workload_identity_entity_keys_idx",
		"fact_records_service_catalog_correlations_service_idx",
		"fact_records_ci_cd_run_correlations_environment_lookup_idx",
	} {
		if strings.Contains(noFilterPlan, unexpectedIndex) {
			t.Fatalf("no-filter aggregate plan unexpectedly probes %q:\n%s", unexpectedIndex, noFilterPlan)
		}
	}

	explainPlan := explainSupplyChainRuntimeFilterPlan(
		t,
		ctx,
		tx,
		"explain_workload_service",
		explainSupplyChainImpactFindingQuery,
		supplyChainRuntimeFilterExplainArgs(SupplyChainImpactExplanationFilter{
			CVEID:                runtimeFilterLiveCVE,
			PackageID:            runtimeFilterLivePackage,
			WorkloadID:           "workload:5747:scalar",
			ServiceID:            "service:5747:allowed",
			AllowedRepositoryIDs: []string{runtimeFilterLiveRepository},
		})...,
	)
	for _, wantIndex := range []string{
		"fact_records_workload_identity_workload_idx",
		"fact_records_service_catalog_correlations_service_idx",
	} {
		if !strings.Contains(explainPlan, wantIndex) {
			t.Fatalf("explain plan missing index %q:\n%s", wantIndex, explainPlan)
		}
	}
}

func withSupplyChainRuntimeFilterDimensions(
	filter SupplyChainImpactFindingFilter,
	workloadID string,
	serviceID string,
	environment string,
) SupplyChainImpactFindingFilter {
	filter.WorkloadID = workloadID
	filter.ServiceID = serviceID
	filter.Environment = environment
	return filter
}

func supplyChainRuntimeFilterListArgs(filter SupplyChainImpactFindingFilter) []any {
	return []any{
		supplyChainImpactFindingFactKind,
		filter.CVEID,
		filter.PackageID,
		filter.RepositoryID,
		filter.SubjectDigest,
		filter.ImpactStatus,
		filter.AdvisoryID,
		filter.Ecosystem,
		filter.ServiceID,
		filter.WorkloadID,
		filter.Environment,
		filter.Severity,
		filter.DetectionProfile,
		filter.PriorityBucket,
		filter.MinPriorityScore,
		filter.ImageRef,
		filter.AfterFindingID,
		normalizeSupplyChainImpactSort(filter.Sort),
		filter.Limit,
		filter.SuppressionState,
		filter.IncludeSuppressed,
		pgarray.Array(filter.AllowedRepositoryIDs),
		pgarray.Array(filter.AllowedScopeIDs),
	}
}

func supplyChainRuntimeFilterAggregateArgs(filter SupplyChainImpactAggregateFilter) []any {
	return []any{
		filter.CVEID,
		filter.PackageID,
		filter.RepositoryID,
		filter.SubjectDigest,
		filter.ImpactStatus,
		filter.AdvisoryID,
		filter.Ecosystem,
		filter.ServiceID,
		filter.WorkloadID,
		filter.Environment,
		filter.Severity,
		filter.DetectionProfile,
		filter.PriorityBucket,
		filter.MinPriorityScore,
		filter.SuppressionState,
		filter.IncludeSuppressed,
		filter.ImageRef,
		pgarray.Array(filter.AllowedRepositoryIDs),
		pgarray.Array(filter.AllowedScopeIDs),
	}
}

func supplyChainRuntimeFilterExplainArgs(filter SupplyChainImpactExplanationFilter) []any {
	return []any{
		supplyChainImpactFindingFactKind,
		filter.FindingID,
		filter.AdvisoryID,
		filter.CVEID,
		filter.PackageID,
		filter.RepositoryID,
		filter.SubjectDigest,
		filter.WorkloadID,
		filter.ServiceID,
		filter.ImageRef,
		pgarray.Array(filter.AllowedRepositoryIDs),
		pgarray.Array(filter.AllowedScopeIDs),
	}
}
