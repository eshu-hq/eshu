// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11447 Plan C outside-closure hydration contract: every decoder arm the production UNION must cover (payload repository_id, repo_id, payload-scope alias, related-only, raw scope) plus a grant-scoped row with envelope outside the closure, all resolving through ListSupplyChainImpactRuntimeContext, with an unseeded candidate rendering honest-empty.
//eshu:readiness:env ESHU_POSTGRES_TEST_DSN

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

const (
	runtimeOutsideClosureRepository   = "repository:r_11447_outside"
	runtimeOutsideClosureRawCandidate = "github.com/org/11447raw"
	runtimeOutsideClosureGrantRepo    = "repository:r_11447_grant"
	runtimeOutsideClosureOtherRepo    = "repository:r_11447_other"
	runtimeOutsideClosureAbsentRepo   = "repository:r_11447_absent"
	runtimeOutsideClosureEnvelope     = "scope:11447:outside"
	runtimeOutsideClosureEnvelopeGen  = "generation:11447:outside"
	runtimeOutsideClosureGrantScope   = "scope:11447:grant"
	runtimeOutsideClosureGrantGen     = "generation:11447:grant"
	runtimeOutsideClosureRawScope     = "github.com/org/11447raw"
	runtimeOutsideClosureRawGen       = "generation:11447:raw"
)

func seedRuntimeOutsideClosureScope(
	t *testing.T,
	ctx context.Context,
	tx *sql.Tx,
	scopeID string,
	generationID string,
) {
	t.Helper()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload
) VALUES ($1, 'synthetic', 'synthetic', $1, 'synthetic', $1, NOW(), NOW(), 'active', '{}'::jsonb)`,
		scopeID,
	); err != nil {
		t.Fatalf("insert scope %s: %v", scopeID, err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO scope_generations (
  generation_id, scope_id, trigger_kind, observed_at, ingested_at,
  status, activated_at, payload
) VALUES ($1, $2, 'synthetic', NOW(), NOW(), 'active', NOW(), '{}'::jsonb)`,
		generationID,
		scopeID,
	); err != nil {
		t.Fatalf("insert generation %s: %v", generationID, err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`,
		generationID,
		scopeID,
	); err != nil {
		t.Fatalf("activate generation %s: %v", generationID, err)
	}
}

// TestSupplyChainImpactRuntimeContextOutsideClosureLive seeds one fact per
// outside-closure decoder arm (D1 payload repository_id on svc and ci, D2
// repo_id, D3 payload-scope alias, D4 related-only, D5 raw scope) plus a
// grant-scoped row whose envelope sits outside the closure, and asserts each
// resolves through the store. It locks the behavior the Plan C UNION exists
// to serve; the UNION shape itself is pinned hermetically by
// TestSelectRuntimeContextQueryIsPerKindUnion in the impact package.
func TestSupplyChainImpactRuntimeContextOutsideClosureLive(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live #11447 outside-closure hydration proof")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	bootstrapRuntimeFilterSchema(t, ctx, db)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	seedRuntimeOutsideClosureScope(t, ctx, tx, runtimeOutsideClosureEnvelope, runtimeOutsideClosureEnvelopeGen)
	seedRuntimeOutsideClosureScope(t, ctx, tx, runtimeOutsideClosureGrantScope, runtimeOutsideClosureGrantGen)
	seedRuntimeOutsideClosureScope(t, ctx, tx, runtimeOutsideClosureRawScope, runtimeOutsideClosureRawGen)

	insertSupplyChainRuntimeFilterFact(t, ctx, tx, "fact:11447:svc:direct",
		runtimeOutsideClosureEnvelope, runtimeOutsideClosureEnvelopeGen,
		runtimeFilterLiveServiceCatalogFactKind, false, map[string]any{
			"repository_id": runtimeOutsideClosureRepository,
			"service_id":    "service:11447:direct",
			"outcome":       "exact",
		})
	insertSupplyChainRuntimeFilterFact(t, ctx, tx, "fact:11447:ci:direct",
		runtimeOutsideClosureEnvelope, runtimeOutsideClosureEnvelopeGen,
		cloudRuntimeProbeTestCICDFactKind, false, map[string]any{
			"repository_id": runtimeOutsideClosureRepository,
			"environment":   "production-11447",
			"outcome":       "exact",
		})
	insertSupplyChainRuntimeFilterFact(t, ctx, tx, "fact:11447:ci:repo-id",
		runtimeOutsideClosureEnvelope, runtimeOutsideClosureEnvelopeGen,
		cloudRuntimeProbeTestCICDFactKind, false, map[string]any{
			"repo_id":     runtimeOutsideClosureRepository,
			"environment": "staging-11447",
			"outcome":     "exact",
		})
	insertSupplyChainRuntimeFilterFact(t, ctx, tx, "fact:11447:pm:payload-scope",
		runtimeOutsideClosureEnvelope, runtimeOutsideClosureEnvelopeGen,
		impact.PlatformMaterializationFactKindQuery, false, map[string]any{
			"scope_id":      runtimeOutsideClosureRepository,
			"deployment_id": "deployment:11447:payload-scope",
		})
	insertSupplyChainRuntimeFilterFact(t, ctx, tx, "fact:11447:wi:related-only",
		runtimeOutsideClosureEnvelope, runtimeOutsideClosureEnvelopeGen,
		impact.WorkloadIdentityFactKindQuery, false, map[string]any{
			"related_scope_ids": []string{runtimeOutsideClosureRepository},
			"workload_id":       "workload:11447:related-only",
		})
	insertSupplyChainRuntimeFilterFact(t, ctx, tx, "fact:11447:wi:raw-scope",
		runtimeOutsideClosureRawScope, runtimeOutsideClosureRawGen,
		impact.WorkloadIdentityFactKindQuery, false, map[string]any{
			"workload_id": "workload:11447:raw-scope",
		})
	insertSupplyChainRuntimeFilterFact(t, ctx, tx, "fact:11447:wi:grant-scoped",
		runtimeOutsideClosureGrantScope, runtimeOutsideClosureGrantGen,
		impact.WorkloadIdentityFactKindQuery, false, map[string]any{
			"repository_id": runtimeOutsideClosureGrantRepo,
			"workload_id":   "workload:11447:grant-scoped",
		})

	store := impact.NewPostgresFindingStore(tx)
	got, err := store.ListSupplyChainImpactRuntimeContext(ctx,
		[]string{
			runtimeOutsideClosureRepository,
			runtimeOutsideClosureRawCandidate,
			runtimeOutsideClosureGrantRepo,
			runtimeOutsideClosureAbsentRepo,
		},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("unrestricted outside-closure read: %v", err)
	}
	outside, ok := got[runtimeOutsideClosureRepository]
	if !ok {
		t.Fatalf("unrestricted read missing %s: %v", runtimeOutsideClosureRepository, got)
	}
	if len(outside.ServiceIDs) != 1 || outside.ServiceIDs[0] != "service:11447:direct" {
		t.Errorf("ServiceIDs = %v, want [service:11447:direct]", outside.ServiceIDs)
	}
	if len(outside.Environments) != 2 {
		t.Errorf("Environments = %v, want production-11447 + staging-11447 (D1 + D2 arms)", outside.Environments)
	}
	if len(outside.DeploymentIDs) != 1 || outside.DeploymentIDs[0] != "deployment:11447:payload-scope" {
		t.Errorf("DeploymentIDs = %v, want [deployment:11447:payload-scope]", outside.DeploymentIDs)
	}
	if len(outside.WorkloadIDs) != 1 || outside.WorkloadIDs[0] != "workload:11447:related-only" {
		t.Errorf("WorkloadIDs = %v, want [workload:11447:related-only]", outside.WorkloadIDs)
	}
	raw, ok := got[runtimeOutsideClosureRawCandidate]
	if !ok || len(raw.WorkloadIDs) != 1 || raw.WorkloadIDs[0] != "workload:11447:raw-scope" {
		t.Errorf("raw-scope context = %+v, want [workload:11447:raw-scope]", raw)
	}
	if _, ok := got[runtimeOutsideClosureGrantRepo]; !ok {
		t.Errorf("unrestricted read missing grant repo %s", runtimeOutsideClosureGrantRepo)
	}
	if _, ok := got[runtimeOutsideClosureAbsentRepo]; ok {
		t.Errorf("unseeded candidate %s resolved context, want honest-empty", runtimeOutsideClosureAbsentRepo)
	}

	granted, err := store.ListSupplyChainImpactRuntimeContext(ctx,
		[]string{runtimeOutsideClosureGrantRepo},
		[]string{runtimeOutsideClosureGrantRepo},
		nil,
	)
	if err != nil {
		t.Fatalf("repository-granted read: %v", err)
	}
	if ctx, ok := granted[runtimeOutsideClosureGrantRepo]; !ok || len(ctx.WorkloadIDs) != 1 {
		t.Errorf("repository-granted context = %+v, want the grant-scoped workload", granted)
	}

	denied, err := store.ListSupplyChainImpactRuntimeContext(ctx,
		[]string{runtimeOutsideClosureGrantRepo},
		[]string{runtimeOutsideClosureOtherRepo},
		nil,
	)
	if err != nil {
		t.Fatalf("non-granted read: %v", err)
	}
	if _, ok := denied[runtimeOutsideClosureGrantRepo]; ok {
		t.Errorf("non-granted read resolved %s, want excluded", runtimeOutsideClosureGrantRepo)
	}

	scopeGranted, err := store.ListSupplyChainImpactRuntimeContext(ctx,
		[]string{runtimeOutsideClosureGrantRepo},
		[]string{runtimeOutsideClosureOtherRepo},
		[]string{runtimeOutsideClosureGrantScope},
	)
	if err != nil {
		t.Fatalf("envelope-granted read: %v", err)
	}
	if ctx, ok := scopeGranted[runtimeOutsideClosureGrantRepo]; !ok || len(ctx.WorkloadIDs) != 1 {
		t.Errorf("envelope-granted context = %+v, want the grant-scoped workload", scopeGranted)
	}
}
