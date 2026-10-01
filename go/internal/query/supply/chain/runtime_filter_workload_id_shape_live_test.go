// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	workloadIDShapeLiveScope      = "scope:7384:wid-only"
	workloadIDShapeLiveGeneration = "generation:7384:wid-only:active"
	workloadIDShapeLiveRepository = "repository:r_73840001"
	workloadIDShapeLiveIDForm     = "workload:repository:r_73840001"
	workloadIDShapeLiveNameForm   = "workload:wid-only"
	workloadIDShapeLiveCVE        = "CVE-2026-73840"
	workloadIDShapeLivePackage    = "pkg:deb/example/7384-wid-only"
	workloadIDShapeLiveFindingID  = "finding:7384:wid-only"
	workloadIDShapeLiveFactID     = "fact:7384:wid-only:impact"
	workloadIDShapeLiveWidFactID  = "fact:7384:wid-only:identity"
)

// TestSupplyChainImpactWorkloadIDFilterRepositoryIDShapeLive pins the #7384
// read-contract shape: workload-identity facts store repository-ID keys
// (workload:repository:r_<hex>), so a repository-ID workload_id filter
// matches on the wid branch while a name-form filter matches nothing there.
// The seeded repository carries wid evidence only (no service-catalog facts),
// so the name-form miss is total: the findings read is empty. The
// runtime-context read surfaces the stored ID-form key verbatim.
func TestSupplyChainImpactWorkloadIDFilterRepositoryIDShapeLive(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live #7384 workload-id shape proof")
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

	if _, err := tx.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload
) VALUES ($1, 'synthetic', 'synthetic', $1, 'synthetic', $1, NOW(), NOW(), 'active', '{}'::jsonb)`,
		workloadIDShapeLiveScope,
	); err != nil {
		t.Fatalf("insert scope: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO scope_generations (
  generation_id, scope_id, trigger_kind, observed_at, ingested_at,
  status, activated_at, payload
) VALUES ($1, $2, 'synthetic', NOW(), NOW(), 'active', NOW(), '{}'::jsonb)`,
		workloadIDShapeLiveGeneration,
		workloadIDShapeLiveScope,
	); err != nil {
		t.Fatalf("insert generation: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`,
		workloadIDShapeLiveGeneration,
		workloadIDShapeLiveScope,
	); err != nil {
		t.Fatalf("activate generation: %v", err)
	}

	insertSupplyChainRuntimeFilterFact(t, ctx, tx, workloadIDShapeLiveFactID, workloadIDShapeLiveScope, workloadIDShapeLiveGeneration,
		impact.FindingFactKind, false, map[string]any{
			"finding_id":        workloadIDShapeLiveFindingID,
			"cve_id":            workloadIDShapeLiveCVE,
			"package_id":        workloadIDShapeLivePackage,
			"repository_id":     workloadIDShapeLiveRepository,
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
	// Production workloadIdentityPayload shape: entity_keys only, no
	// workload_id field; the keys are repository-ID form post-#7384.
	insertSupplyChainRuntimeFilterFact(t, ctx, tx, workloadIDShapeLiveWidFactID, workloadIDShapeLiveScope, workloadIDShapeLiveGeneration,
		impact.WorkloadIdentityFactKindQuery, false, map[string]any{
			"reducer_domain":    "workload_identity",
			"intent_id":         "intent:7384:wid-only",
			"scope_id":          workloadIDShapeLiveScope,
			"generation_id":     workloadIDShapeLiveGeneration,
			"source_system":     "synthetic",
			"cause":             "repository snapshot",
			"entity_keys":       []string{workloadIDShapeLiveIDForm},
			"related_scope_ids": []string{},
			"canonical_id":      "canonical:7384:wid-only",
			"repository_id":     workloadIDShapeLiveRepository,
		})

	findingStore := impact.NewPostgresFindingStore(tx)
	aggregateStore := impact.NewPostgresAggregateStore(tx)
	allowedScopes := []string{workloadIDShapeLiveScope}

	idFilter := impact.FindingFilter{
		CVEID:             workloadIDShapeLiveCVE,
		WorkloadID:        workloadIDShapeLiveIDForm,
		DetectionProfile:  "comprehensive",
		Limit:             10,
		AllowedScopeIDs:   allowedScopes,
		IncludeSuppressed: false,
	}
	assertSupplyChainRuntimeFilterListCount(t, ctx, findingStore, idFilter, false, 1)
	assertSupplyChainRuntimeFilterListCount(t, ctx, findingStore, idFilter, true, 1)
	idCount, err := aggregateStore.CountSupplyChainImpactFindings(ctx, impact.AggregateFilter{
		CVEID:             workloadIDShapeLiveCVE,
		WorkloadID:        workloadIDShapeLiveIDForm,
		DetectionProfile:  "comprehensive",
		AllowedScopeIDs:   allowedScopes,
		IncludeSuppressed: false,
	})
	if err != nil {
		t.Fatalf("count id-form: %v", err)
	}
	if idCount.TotalFindings != 1 {
		t.Fatalf("count id-form = %d, want 1", idCount.TotalFindings)
	}

	// Name-form callers have no selector resolution (unlike repository_id):
	// the wid branch compares the raw filter value against stored ID keys,
	// and with no service-catalog evidence the miss is total.
	nameFilter := impact.FindingFilter{
		CVEID:             workloadIDShapeLiveCVE,
		WorkloadID:        workloadIDShapeLiveNameForm,
		DetectionProfile:  "comprehensive",
		Limit:             10,
		AllowedScopeIDs:   allowedScopes,
		IncludeSuppressed: false,
	}
	assertSupplyChainRuntimeFilterListCount(t, ctx, findingStore, nameFilter, false, 0)
	assertSupplyChainRuntimeFilterListCount(t, ctx, findingStore, nameFilter, true, 0)
	nameCount, err := aggregateStore.CountSupplyChainImpactFindings(ctx, impact.AggregateFilter{
		CVEID:             workloadIDShapeLiveCVE,
		WorkloadID:        workloadIDShapeLiveNameForm,
		DetectionProfile:  "comprehensive",
		AllowedScopeIDs:   allowedScopes,
		IncludeSuppressed: false,
	})
	if err != nil {
		t.Fatalf("count name-form: %v", err)
	}
	if nameCount.TotalFindings != 0 {
		t.Fatalf("count name-form = %d, want 0", nameCount.TotalFindings)
	}

	runtimeCtx, err := findingStore.ListSupplyChainImpactRuntimeContext(
		ctx,
		[]string{workloadIDShapeLiveRepository},
		nil,
		allowedScopes,
	)
	if err != nil {
		t.Fatalf("runtime context: %v", err)
	}
	got := runtimeCtx[workloadIDShapeLiveRepository].WorkloadIDs
	if len(got) != 1 || got[0] != workloadIDShapeLiveIDForm {
		t.Fatalf("runtime context workload_ids = %#v, want [%q]", got, workloadIDShapeLiveIDForm)
	}
}
