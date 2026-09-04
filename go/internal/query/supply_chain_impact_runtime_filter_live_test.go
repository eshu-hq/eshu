// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
	// Registers the "pgx" driver this test opens by name. Other files in this
	// package also blank-import it, so the driver would resolve without this
	// line today -- but only by accident of what else compiles into the test
	// binary. Without it, removing a sibling import or moving this file to an
	// external package_test turns the failure into sql: unknown driver "pgx",
	// visible only when ESHU_POSTGRES_TEST_DSN is set, so CI would skip green.
	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	runtimeFilterLiveRepository = "repository:r_5747_runtime_filter"
	runtimeFilterLiveCVE        = "CVE-2026-57470"
	runtimeFilterLivePackage    = "pkg:deb/example/runtime-filter"
	runtimeFilterLiveScopeA     = "scope:5747:tenant-a"
	runtimeFilterLiveScopeB     = "scope:5747:tenant-b"
	runtimeFilterLiveScopeC     = "scope:5747:tenant-c"
	runtimeFilterLiveDecoyRepo  = "repository:r_5747_decoy"
	runtimeFilterLiveGenA       = "generation:5747:tenant-a:active"
	runtimeFilterLiveGenAStale  = "generation:5747:tenant-a:stale"
	runtimeFilterLiveGenB       = "generation:5747:tenant-b:active"
	runtimeFilterLiveGenC       = "generation:5747:tenant-c:active"
	runtimeFilterLiveGenDecoy   = "generation:5747:decoy:active"
	runtimeFilterLiveFindingID  = "finding:5747:runtime-filter"
	runtimeFilterLiveFactID     = "fact:5747:impact"
	runtimeFilterLiveBakedID    = "finding:5747:stale-baked"
	runtimeFilterLiveBakedFact  = "fact:5747:stale-baked"
	runtimeFilterLiveBakedPkg   = "pkg:deb/example/stale-baked"
	runtimeFilterLiveBakedRepo  = "repository:r_5747_stale_baked"
)

func TestSupplyChainImpactRuntimeFiltersEnforceScopedTruthLive(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live #5747 scoped runtime-filter proof")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	seedSupplyChainRuntimeFilterLiveFacts(t, ctx, tx)

	findingStore := impact.NewPostgresSupplyChainImpactFindingStore(tx)
	aggregateStore := impact.NewPostgresSupplyChainImpactAggregateStore(tx)
	allowedScopes := []string{runtimeFilterLiveScopeA}

	for _, tc := range []struct {
		name        string
		workloadID  string
		serviceID   string
		environment string
	}{
		{name: "workload_scalar", workloadID: "workload:5747:scalar"},
		{name: "workload_entity_key", workloadID: "workload:5747:entity-key"},
		{name: "service", serviceID: "service:5747:allowed"},
		{name: "environment", environment: "production-5747"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			filter := impact.SupplyChainImpactFindingFilter{
				CVEID:             runtimeFilterLiveCVE,
				WorkloadID:        tc.workloadID,
				ServiceID:         tc.serviceID,
				Environment:       tc.environment,
				DetectionProfile:  "comprehensive",
				Limit:             10,
				AllowedScopeIDs:   allowedScopes,
				IncludeSuppressed: false,
			}
			assertSupplyChainRuntimeFilterListCount(t, ctx, findingStore, filter, false, 1)
			assertSupplyChainRuntimeFilterListCount(t, ctx, findingStore, filter, true, 1)

			aggregateFilter := impact.SupplyChainImpactAggregateFilter{
				CVEID:             runtimeFilterLiveCVE,
				WorkloadID:        tc.workloadID,
				ServiceID:         tc.serviceID,
				Environment:       tc.environment,
				DetectionProfile:  "comprehensive",
				AllowedScopeIDs:   allowedScopes,
				IncludeSuppressed: false,
			}
			count, err := aggregateStore.CountSupplyChainImpactFindings(ctx, aggregateFilter)
			if err != nil {
				t.Fatalf("count %s: %v", tc.name, err)
			}
			if count.TotalFindings != 1 {
				t.Fatalf("count %s = %d, want 1", tc.name, count.TotalFindings)
			}
			inventory, err := aggregateStore.SupplyChainImpactInventory(
				ctx,
				aggregateFilter,
				impact.SupplyChainImpactInventoryByImpactStatus,
				10,
				0,
			)
			if err != nil {
				t.Fatalf("inventory %s: %v", tc.name, err)
			}
			if len(inventory) != 1 || inventory[0].Count != 1 {
				t.Fatalf("inventory %s = %#v, want one finding", tc.name, inventory)
			}
		})
	}

	for _, tc := range []struct {
		name        string
		workloadID  string
		serviceID   string
		environment string
	}{
		{name: "other_scope", serviceID: "service:5747:tenant-b"},
		{name: "other_scope_workload", workloadID: "workload:5747:tenant-b"},
		{name: "other_scope_environment", environment: "staging-5747"},
		{name: "stale_generation", serviceID: "service:5747:stale"},
		{name: "tombstone", serviceID: "service:5747:tombstone"},
		{name: "ambiguous", serviceID: "service:5747:ambiguous"},
		{name: "provenance_only", serviceID: "service:5747:provenance"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			filter := impact.SupplyChainImpactFindingFilter{
				CVEID:            runtimeFilterLiveCVE,
				WorkloadID:       tc.workloadID,
				ServiceID:        tc.serviceID,
				Environment:      tc.environment,
				DetectionProfile: "comprehensive",
				Limit:            10,
				AllowedScopeIDs:  allowedScopes,
			}
			assertSupplyChainRuntimeFilterListCount(t, ctx, findingStore, filter, false, 0)
			assertSupplyChainRuntimeFilterListCount(t, ctx, findingStore, filter, true, 0)

			count, err := aggregateStore.CountSupplyChainImpactFindings(ctx, impact.SupplyChainImpactAggregateFilter{
				CVEID:            runtimeFilterLiveCVE,
				WorkloadID:       tc.workloadID,
				ServiceID:        tc.serviceID,
				Environment:      tc.environment,
				DetectionProfile: "comprehensive",
				AllowedScopeIDs:  allowedScopes,
			})
			if err != nil {
				t.Fatalf("count %s: %v", tc.name, err)
			}
			if count.TotalFindings != 0 {
				t.Fatalf("count %s = %d, want 0", tc.name, count.TotalFindings)
			}
		})
	}

	explanation, err := findingStore.ExplainSupplyChainImpact(ctx, impact.SupplyChainImpactExplanationFilter{
		CVEID:           runtimeFilterLiveCVE,
		PackageID:       runtimeFilterLivePackage,
		ServiceID:       "service:5747:allowed",
		AllowedScopeIDs: allowedScopes,
	})
	if err != nil {
		t.Fatalf("explain granted service: %v", err)
	}
	if explanation.Finding.FindingID != runtimeFilterLiveFindingID {
		t.Fatalf("explain finding = %q, want %q", explanation.Finding.FindingID, runtimeFilterLiveFindingID)
	}

	assertSupplyChainRuntimeContextScopesLive(t, ctx, findingStore)
	assertSupplyChainConflictingAnchorFiltersLive(t, ctx, findingStore, aggregateStore)
	assertSupplyChainStaleBakedFiltersLive(t, ctx, findingStore, aggregateStore)
	assertSupplyChainRuntimeRepositoryPrecedenceLive(t, ctx, findingStore, aggregateStore)
	assertSupplyChainRuntimeNormalizationLive(t, ctx, findingStore, aggregateStore)
}

func assertSupplyChainRuntimeFilterListCount(
	t *testing.T,
	ctx context.Context,
	store impact.PostgresSupplyChainImpactFindingStore,
	filter impact.SupplyChainImpactFindingFilter,
	readFromWinners bool,
	want int,
) {
	t.Helper()
	store.ReadFromWinners = readFromWinners
	rows, err := store.ListSupplyChainImpactFindings(ctx, filter)
	if err != nil {
		t.Fatalf("list winners=%v: %v", readFromWinners, err)
	}
	if len(rows) != want {
		t.Fatalf("list winners=%v count = %d, want %d", readFromWinners, len(rows), want)
	}
}

func seedSupplyChainRuntimeFilterLiveFacts(
	t *testing.T,
	ctx context.Context,
	tx *sql.Tx,
) {
	t.Helper()
	for _, scope := range []struct {
		scopeID      string
		generationID string
	}{
		{scopeID: runtimeFilterLiveScopeA, generationID: runtimeFilterLiveGenA},
		{scopeID: runtimeFilterLiveScopeB, generationID: runtimeFilterLiveGenB},
		{scopeID: runtimeFilterLiveScopeC, generationID: runtimeFilterLiveGenC},
		{scopeID: runtimeFilterLiveDecoyRepo, generationID: runtimeFilterLiveGenDecoy},
		{scopeID: runtimePrecedenceEnvelopeRepository, generationID: runtimePrecedenceEnvelopeGeneration},
	} {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload
) VALUES ($1, 'synthetic', 'synthetic', $1, 'synthetic', $1, NOW(), NOW(), 'active', '{}'::jsonb)`,
			scope.scopeID,
		); err != nil {
			t.Fatalf("insert scope %s: %v", scope.scopeID, err)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO scope_generations (
  generation_id, scope_id, trigger_kind, observed_at, ingested_at,
  status, activated_at, payload
) VALUES ($1, $2, 'synthetic', NOW(), NOW(), 'active', NOW(), '{}'::jsonb)`,
			scope.generationID,
			scope.scopeID,
		); err != nil {
			t.Fatalf("insert generation %s: %v", scope.generationID, err)
		}
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`,
			scope.generationID,
			scope.scopeID,
		); err != nil {
			t.Fatalf("activate generation %s: %v", scope.generationID, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO scope_generations (
  generation_id, scope_id, trigger_kind, observed_at, ingested_at,
  status, superseded_at, payload
) VALUES ($1, $2, 'synthetic', NOW(), NOW(), 'superseded', NOW(), '{}'::jsonb)`,
		runtimeFilterLiveGenAStale,
		runtimeFilterLiveScopeA,
	); err != nil {
		t.Fatalf("insert stale generation: %v", err)
	}

	insertSupplyChainRuntimeFilterFact(t, ctx, tx, runtimeFilterLiveFactID, runtimeFilterLiveScopeA, runtimeFilterLiveGenA,
		impact.SupplyChainImpactFindingFactKind, false, map[string]any{
			"finding_id":        runtimeFilterLiveFindingID,
			"cve_id":            runtimeFilterLiveCVE,
			"package_id":        runtimeFilterLivePackage,
			"repository_id":     runtimeFilterLiveRepository,
			"impact_status":     "affected_exact",
			"detection_profile": "comprehensive",
			"priority_score":    "50",
			"priority_bucket":   "high",
			"suppression_state": "active",
			"service_ids":       []string{},
			"workload_ids":      []string{},
			"environments":      []string{},
			"evidence_fact_ids": []string{},
		})
	insertSupplyChainRuntimeFilterFact(t, ctx, tx, runtimeFilterLiveBakedFact, runtimeFilterLiveScopeA, runtimeFilterLiveGenA,
		impact.SupplyChainImpactFindingFactKind, false, map[string]any{
			"finding_id":        runtimeFilterLiveBakedID,
			"cve_id":            runtimeFilterLiveCVE,
			"package_id":        runtimeFilterLiveBakedPkg,
			"repository_id":     runtimeFilterLiveBakedRepo,
			"impact_status":     "affected_exact",
			"detection_profile": "comprehensive",
			"priority_score":    "40",
			"priority_bucket":   "medium",
			"suppression_state": "active",
			"service_ids":       []string{"service:5747:stale-baked"},
			"workload_ids":      []string{"workload:5747:stale-baked"},
			"environments":      []string{"stale-baked-5747"},
			"evidence_fact_ids": []string{},
		})
	insertSupplyChainRuntimeFilterFact(t, ctx, tx, "fact:5747:workload:scalar", runtimeFilterLiveScopeA, runtimeFilterLiveGenA,
		impact.WorkloadIdentityFactKindQuery, false, map[string]any{
			"repository_id": runtimeFilterLiveRepository,
			"workload_id":   "workload:5747:scalar",
		})
	insertSupplyChainRuntimeFilterFact(t, ctx, tx, "fact:5747:workload:entity", runtimeFilterLiveScopeA, runtimeFilterLiveGenA,
		impact.WorkloadIdentityFactKindQuery, false, map[string]any{
			"repository_id": runtimeFilterLiveRepository,
			"entity_keys":   []string{"workload:5747:entity-key", "repository:5747:not-a-workload"},
		})
	insertSupplyChainRuntimeFilterFact(t, ctx, tx, "fact:5747:service:allowed", runtimeFilterLiveScopeA, runtimeFilterLiveGenA,
		serviceCatalogCorrelationFactKind, false, map[string]any{
			"repository_id": runtimeFilterLiveRepository,
			"service_id":    "service:5747:allowed",
			"outcome":       "exact",
		})
	insertSupplyChainRuntimeFilterFact(t, ctx, tx, "fact:5747:environment:allowed", runtimeFilterLiveScopeA, runtimeFilterLiveGenA,
		cicdRunCorrelationFactKind, false, map[string]any{
			"repository_id": runtimeFilterLiveRepository,
			"environment":   "production-5747",
			"outcome":       "derived",
		})
	insertSupplyChainRuntimeFilterFact(t, ctx, tx, "fact:5747:service:tenant-b", runtimeFilterLiveScopeB, runtimeFilterLiveGenB,
		serviceCatalogCorrelationFactKind, false, map[string]any{
			"repository_id": runtimeFilterLiveRepository,
			"service_id":    "service:5747:tenant-b",
			"outcome":       "exact",
		})
	insertSupplyChainRuntimeFilterFact(t, ctx, tx, "fact:5747:workload:tenant-b", runtimeFilterLiveScopeB, runtimeFilterLiveGenB,
		impact.WorkloadIdentityFactKindQuery, false, map[string]any{
			"repository_id": runtimeFilterLiveRepository,
			"workload_id":   "workload:5747:tenant-b",
		})
	insertSupplyChainRuntimeFilterFact(t, ctx, tx, "fact:5747:environment:tenant-b", runtimeFilterLiveScopeB, runtimeFilterLiveGenB,
		cicdRunCorrelationFactKind, false, map[string]any{
			"repository_id": runtimeFilterLiveRepository,
			"environment":   "staging-5747",
			"outcome":       "exact",
		})
	insertSupplyChainRuntimeFilterFact(t, ctx, tx, "fact:5747:workload:conflicting-payload-scope", runtimeFilterLiveScopeC, runtimeFilterLiveGenC,
		impact.WorkloadIdentityFactKindQuery, false, map[string]any{
			"repository_id": runtimeFilterLiveRepository,
			"scope_id":      runtimeFilterLiveDecoyRepo,
			"workload_id":   "workload:5747:conflicting-payload-scope",
		})
	insertSupplyChainRuntimeFilterFact(t, ctx, tx, "fact:5747:service:conflicting-related-scope", runtimeFilterLiveScopeC, runtimeFilterLiveGenC,
		serviceCatalogCorrelationFactKind, false, map[string]any{
			"repository_id":     runtimeFilterLiveRepository,
			"related_scope_ids": []string{runtimeFilterLiveDecoyRepo},
			"service_id":        "service:5747:conflicting-related-scope",
			"outcome":           "exact",
		})
	insertSupplyChainRuntimeFilterFact(t, ctx, tx, "fact:5747:environment:conflicting-envelope-scope", runtimeFilterLiveDecoyRepo, runtimeFilterLiveGenDecoy,
		cicdRunCorrelationFactKind, false, map[string]any{
			"repository_id": runtimeFilterLiveRepository,
			"environment":   "conflicting-envelope-5747",
			"outcome":       "exact",
		})
	insertSupplyChainRuntimeFilterFact(t, ctx, tx, "fact:5747:service:stale", runtimeFilterLiveScopeA, runtimeFilterLiveGenAStale,
		serviceCatalogCorrelationFactKind, false, map[string]any{
			"repository_id": runtimeFilterLiveRepository,
			"service_id":    "service:5747:stale",
			"outcome":       "exact",
		})
	insertSupplyChainRuntimeFilterFact(t, ctx, tx, "fact:5747:service:tombstone", runtimeFilterLiveScopeA, runtimeFilterLiveGenA,
		serviceCatalogCorrelationFactKind, true, map[string]any{
			"repository_id": runtimeFilterLiveRepository,
			"service_id":    "service:5747:tombstone",
			"outcome":       "exact",
		})
	insertSupplyChainRuntimeFilterFact(t, ctx, tx, "fact:5747:service:ambiguous", runtimeFilterLiveScopeA, runtimeFilterLiveGenA,
		serviceCatalogCorrelationFactKind, false, map[string]any{
			"repository_id": runtimeFilterLiveRepository,
			"service_id":    "service:5747:ambiguous",
			"outcome":       "ambiguous",
		})
	insertSupplyChainRuntimeFilterFact(t, ctx, tx, "fact:5747:service:provenance", runtimeFilterLiveScopeA, runtimeFilterLiveGenA,
		serviceCatalogCorrelationFactKind, false, map[string]any{
			"repository_id":   runtimeFilterLiveRepository,
			"service_id":      "service:5747:provenance",
			"outcome":         "exact",
			"provenance_only": true,
		})

	if _, err := tx.ExecContext(ctx, `
INSERT INTO supply_chain_impact_canonical_winners (
  canonical_key, winner_fact_id, winner_scope_id, finding_id,
  priority_score, source_count, impact_status, ecosystem, severity_bucket,
  repository_id, cve_id, package_id, priority_bucket, detection_profile,
  suppression_state, materialized_at
) VALUES (
  'canonical:5747:runtime-filter', $1, $2, $3,
  50, 1, 'affected_exact', 'deb', 'high',
  $4, $5, $6, 'high', 'comprehensive',
  'active', NOW()
)`,
		runtimeFilterLiveFactID,
		runtimeFilterLiveScopeA,
		runtimeFilterLiveFindingID,
		runtimeFilterLiveRepository,
		runtimeFilterLiveCVE,
		runtimeFilterLivePackage,
	); err != nil {
		t.Fatalf("insert canonical winner: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO supply_chain_impact_canonical_winners (
  canonical_key, winner_fact_id, winner_scope_id, finding_id,
  priority_score, source_count, impact_status, ecosystem, severity_bucket,
  repository_id, cve_id, package_id, priority_bucket, detection_profile,
  suppression_state, materialized_at
) VALUES (
  'canonical:5747:stale-baked', $1, $2, $3,
  40, 1, 'affected_exact', 'deb', 'medium',
  $4, $5, $6, 'medium', 'comprehensive',
  'active', NOW()
)`,
		runtimeFilterLiveBakedFact,
		runtimeFilterLiveScopeA,
		runtimeFilterLiveBakedID,
		runtimeFilterLiveBakedRepo,
		runtimeFilterLiveCVE,
		runtimeFilterLiveBakedPkg,
	); err != nil {
		t.Fatalf("insert stale-baked canonical winner: %v", err)
	}
	seedSupplyChainRuntimeRepositoryPrecedenceLiveFacts(t, ctx, tx)
	seedSupplyChainRuntimeNormalizationLiveFacts(t, ctx, tx)
}

func insertSupplyChainRuntimeFilterFact(
	t *testing.T,
	ctx context.Context,
	tx *sql.Tx,
	factID string,
	scopeID string,
	generationID string,
	factKind string,
	tombstone bool,
	payload map[string]any,
) {
	t.Helper()
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal fact %s: %v", factID, err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  source_system, source_fact_key, observed_at, ingested_at,
  is_tombstone, payload
) VALUES ($1, $2, $3, $4, $1, 'synthetic', $1, NOW(), NOW(), $5, $6::jsonb)`,
		factID,
		scopeID,
		generationID,
		factKind,
		tombstone,
		string(payloadJSON),
	); err != nil {
		t.Fatalf("insert fact %s: %v", factID, err)
	}
}
