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
	readAt := time.Date(2026, 7, 27, 12, 0, 10, 0, time.UTC)
	now := func() time.Time { return readAt }
	direct := NewPostgresSupplyChainImpactFindingStore(tx)
	direct.Now = now
	materialized := NewPostgresSupplyChainImpactFindingStoreWithReadModel(tx, true)
	materialized.Now = now
	aggregates := NewPostgresSupplyChainImpactAggregateStore(tx)
	aggregates.Now = now

	assertSuppressionAuthorityState(t, ctx, direct, aggregates, false, 0, "")
	assertSuppressionAuthorityState(t, ctx, direct, aggregates, true, 1, "ignored")
	assertSuppressionAuthorityFilter(t, ctx, direct, aggregates, "ignored", true, 1)
	assertSuppressionAuthorityFilter(t, ctx, direct, aggregates, "expired", true, 0)
	explanation, err := direct.ExplainSupplyChainImpact(ctx, SupplyChainImpactExplanationFilter{
		FindingID: suppressionAuthorityLiveFinding,
	})
	if err != nil {
		t.Fatalf("explain ignored finding: %v", err)
	}
	if got := explanation.Finding.Suppression.State; got != "ignored" {
		t.Fatalf("explain suppression state = %q, want ignored", got)
	}

	rebuildSuppressionAuthorityWinners(t, ctx, tx)
	assertSuppressionAuthorityState(t, ctx, materialized, aggregates, false, 0, "")
	assertSuppressionAuthorityState(t, ctx, materialized, aggregates, true, 1, "ignored")
	assertSuppressionAuthorityFilter(t, ctx, materialized, aggregates, "ignored", true, 1)

	// Advance only the read clock to expires_at. No suppression mutation,
	// reducer replay, winners rebuild, or fact update is allowed: query-time
	// expiry is the production mechanism under test, and equality must expire.
	readAt = time.Date(2026, 7, 27, 12, 0, 30, 0, time.UTC)
	assertSuppressionAuthorityState(t, ctx, direct, aggregates, false, 1, "expired")
	assertSuppressionAuthorityState(t, ctx, materialized, aggregates, false, 1, "expired")
	assertSuppressionAuthorityFilter(t, ctx, direct, aggregates, "expired", false, 1)
	assertSuppressionAuthorityFilter(t, ctx, direct, aggregates, "ignored", true, 0)
	assertSuppressionAuthorityFilter(t, ctx, materialized, aggregates, "expired", false, 1)
	assertSuppressionAuthorityFilter(t, ctx, materialized, aggregates, "ignored", true, 0)
	explanation, err = direct.ExplainSupplyChainImpact(ctx, SupplyChainImpactExplanationFilter{
		FindingID: suppressionAuthorityLiveFinding,
	})
	if err != nil {
		t.Fatalf("explain expired finding: %v", err)
	}
	if got := explanation.Finding.Suppression.State; got != "expired" {
		t.Fatalf("explain suppression state = %q, want expired", got)
	}

	var persistedState string
	if err := tx.QueryRowContext(
		ctx,
		`SELECT payload #>> '{suppression,state}' FROM fact_records WHERE fact_id = $1`,
		suppressionAuthorityLiveOperatorFact,
	).Scan(&persistedState); err != nil {
		t.Fatalf("read immutable operator fact: %v", err)
	}
	if persistedState != "ignored" {
		t.Fatalf("persisted suppression state = %q, want ignored (read path must not mutate facts)", persistedState)
	}
}

func assertSuppressionAuthorityFilter(
	t *testing.T,
	ctx context.Context,
	store PostgresSupplyChainImpactFindingStore,
	aggregates PostgresSupplyChainImpactAggregateStore,
	suppressionState string,
	includeSuppressed bool,
	wantCount int,
) {
	t.Helper()
	rows, err := store.ListSupplyChainImpactFindings(ctx, SupplyChainImpactFindingFilter{
		CVEID:             suppressionAuthorityLiveCVE,
		DetectionProfile:  "comprehensive",
		SuppressionState:  suppressionState,
		IncludeSuppressed: includeSuppressed,
		Limit:             10,
	})
	if err != nil {
		t.Fatalf("list suppression_state=%q: %v", suppressionState, err)
	}
	if len(rows) != wantCount {
		t.Fatalf("list suppression_state=%q count = %d, want %d", suppressionState, len(rows), wantCount)
	}

	count, err := aggregates.CountSupplyChainImpactFindings(ctx, SupplyChainImpactAggregateFilter{
		CVEID:             suppressionAuthorityLiveCVE,
		DetectionProfile:  "comprehensive",
		SuppressionState:  suppressionState,
		IncludeSuppressed: includeSuppressed,
	})
	if err != nil {
		t.Fatalf("count suppression_state=%q: %v", suppressionState, err)
	}
	if count.TotalFindings != wantCount {
		t.Fatalf(
			"aggregate suppression_state=%q count = %d, want %d",
			suppressionState,
			count.TotalFindings,
			wantCount,
		)
	}
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
	operatorPayload["suppression_state"] = "ignored"
	operatorPayload["suppression"] = map[string]any{
		"state":          "ignored",
		"suppression_id": "suppression-5465-live",
		"source":         "eshu_policy",
		"justification":  "ignored",
		"author":         "shared_token",
		"authored_at":    "2026-07-27T12:00:00Z",
		"expires_at":     "2026-07-27T12:00:30Z",
		"reason":         "synthetic temporary exception",
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
