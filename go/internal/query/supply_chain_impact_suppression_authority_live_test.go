// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	_ "github.com/lib/pq"
)

const (
	suppressionAuthorityLiveCVE          = "CVE-2026-54650"
	suppressionAuthorityLivePackage      = "pkg:deb/example/suppression-authority"
	suppressionAuthorityLiveRepository   = "repository:r_5465_suppression_authority"
	suppressionAuthorityLiveSource       = "scope:5465:source"
	suppressionAuthorityLiveOperator     = "operator:vulnerability_suppressions"
	suppressionAuthorityLiveSourceGen    = "generation:5465:source:active"
	suppressionAuthorityLiveOperatorGen  = "generation:5465:operator:active"
	suppressionAuthorityLiveFinding      = "finding:5465:suppression-authority"
	suppressionAuthorityLiveSourceFact   = "fact:5465:source"
	suppressionAuthorityLiveOperatorFact = "fact:5465:operator"
)

func TestSupplyChainSuppressionAuthorityDirectAndMaterializedParityLive(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live #5465 suppression-authority proof")
	}

	db, err := sql.Open("postgres", dsn)
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

	seedSupplyChainSuppressionAuthorityLiveFacts(t, ctx, tx)
	direct := NewPostgresSupplyChainImpactFindingStore(tx)
	materialized := NewPostgresSupplyChainImpactFindingStoreWithReadModel(tx, true)
	aggregates := NewPostgresSupplyChainImpactAggregateStore(tx)

	assertSuppressionAuthorityState(t, ctx, direct, aggregates, false, 0, "")
	assertSuppressionAuthorityState(t, ctx, direct, aggregates, true, 1, "accepted_risk")
	explanation, err := direct.ExplainSupplyChainImpact(ctx, SupplyChainImpactExplanationFilter{
		FindingID: suppressionAuthorityLiveFinding,
	})
	if err != nil {
		t.Fatalf("explain accepted-risk finding: %v", err)
	}
	if got := explanation.Finding.Suppression.State; got != "accepted_risk" {
		t.Fatalf("explain suppression state = %q, want accepted_risk", got)
	}

	rebuildSuppressionAuthorityWinners(t, ctx, tx)
	assertSuppressionAuthorityState(t, ctx, materialized, aggregates, false, 0, "")
	assertSuppressionAuthorityState(t, ctx, materialized, aggregates, true, 1, "accepted_risk")

	if _, err := tx.ExecContext(ctx, `
UPDATE fact_records
SET payload = jsonb_set(
      jsonb_set(payload, '{suppression_state}', '"expired"'::jsonb),
      '{suppression,state}', '"expired"'::jsonb
    )
WHERE fact_id = $1`, suppressionAuthorityLiveOperatorFact); err != nil {
		t.Fatalf("expire operator finding: %v", err)
	}

	assertSuppressionAuthorityState(t, ctx, direct, aggregates, false, 1, "expired")
	rebuildSuppressionAuthorityWinners(t, ctx, tx)
	assertSuppressionAuthorityState(t, ctx, materialized, aggregates, false, 1, "expired")
}

func assertSuppressionAuthorityState(
	t *testing.T,
	ctx context.Context,
	store PostgresSupplyChainImpactFindingStore,
	aggregates PostgresSupplyChainImpactAggregateStore,
	includeSuppressed bool,
	wantCount int,
	wantState string,
) {
	t.Helper()
	filter := SupplyChainImpactFindingFilter{
		CVEID:             suppressionAuthorityLiveCVE,
		DetectionProfile:  "comprehensive",
		IncludeSuppressed: includeSuppressed,
		Limit:             10,
	}
	rows, err := store.ListSupplyChainImpactFindings(ctx, filter)
	if err != nil {
		t.Fatalf("list include_suppressed=%t: %v", includeSuppressed, err)
	}
	if len(rows) != wantCount {
		t.Fatalf("list include_suppressed=%t count = %d, want %d", includeSuppressed, len(rows), wantCount)
	}
	if wantCount == 1 {
		if rows[0].Suppression == nil {
			t.Fatal("list suppression = nil")
		}
		if got := rows[0].Suppression.State; got != wantState {
			t.Fatalf("list suppression state = %q, want %q", got, wantState)
		}
	}

	count, err := aggregates.CountSupplyChainImpactFindings(ctx, SupplyChainImpactAggregateFilter{
		CVEID:             suppressionAuthorityLiveCVE,
		DetectionProfile:  "comprehensive",
		IncludeSuppressed: includeSuppressed,
	})
	if err != nil {
		t.Fatalf("count include_suppressed=%t: %v", includeSuppressed, err)
	}
	if count.TotalFindings != wantCount {
		t.Fatalf("aggregate include_suppressed=%t count = %d, want %d", includeSuppressed, count.TotalFindings, wantCount)
	}
}

func rebuildSuppressionAuthorityWinners(
	t *testing.T,
	ctx context.Context,
	tx *sql.Tx,
) {
	t.Helper()
	store := storagepostgres.NewSupplyChainImpactWinnersStore(tx)
	if err := store.RebuildAllWinners(ctx, time.Now().UTC()); err != nil {
		t.Fatalf("rebuild canonical winners: %v", err)
	}
}

func seedSupplyChainSuppressionAuthorityLiveFacts(
	t *testing.T,
	ctx context.Context,
	tx *sql.Tx,
) {
	t.Helper()
	for _, scope := range []struct {
		scopeID      string
		generationID string
	}{
		{suppressionAuthorityLiveSource, suppressionAuthorityLiveSourceGen},
		{suppressionAuthorityLiveOperator, suppressionAuthorityLiveOperatorGen},
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

	basePayload := map[string]any{
		"finding_id":        suppressionAuthorityLiveFinding,
		"cve_id":            suppressionAuthorityLiveCVE,
		"package_id":        suppressionAuthorityLivePackage,
		"repository_id":     suppressionAuthorityLiveRepository,
		"impact_status":     "affected_exact",
		"detection_profile": "comprehensive",
		"priority_score":    "50",
		"priority_bucket":   "high",
		"suppression_state": "active",
		"suppression":       map[string]any{"state": "active"},
		"service_ids":       []string{},
		"workload_ids":      []string{},
		"environments":      []string{},
		"evidence_fact_ids": []string{},
	}
	insertSupplyChainRuntimeFilterFact(
		t,
		ctx,
		tx,
		suppressionAuthorityLiveSourceFact,
		suppressionAuthorityLiveSource,
		suppressionAuthorityLiveSourceGen,
		supplyChainImpactFindingFactKind,
		false,
		basePayload,
	)

	operatorPayload := make(map[string]any, len(basePayload))
	for key, value := range basePayload {
		operatorPayload[key] = value
	}
	operatorPayload["suppression_state"] = "accepted_risk"
	operatorPayload["suppression"] = map[string]any{
		"state":          "accepted_risk",
		"suppression_id": "suppression-5465-live",
		"source":         "eshu_policy",
		"justification":  "accepted_risk",
		"author":         "shared_token",
		"authored_at":    "2026-07-27T12:00:00Z",
		"reason":         "synthetic compensating control",
	}
	insertSupplyChainRuntimeFilterFact(
		t,
		ctx,
		tx,
		suppressionAuthorityLiveOperatorFact,
		suppressionAuthorityLiveOperator,
		suppressionAuthorityLiveOperatorGen,
		supplyChainImpactFindingFactKind,
		false,
		operatorPayload,
	)
}
