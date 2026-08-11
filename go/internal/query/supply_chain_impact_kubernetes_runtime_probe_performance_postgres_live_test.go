// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

func openKubernetesRuntimePerformancePostgres(t *testing.T) *sql.DB {
	t.Helper()
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_KUBERNETES_RUNTIME_PROBE_POSTGRES_DSN"),
		os.Getenv("ESHU_KUBERNETES_RUNTIME_PROBE_POSTGRES_DISPOSABLE"),
		5*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply production Postgres schema: %v", err)
	}
	db.SetMaxOpenConns(16)
	return db
}

func seedKubernetesRuntimePerformancePostgres(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	candidates []KubernetesRuntimeCandidate,
) {
	t.Helper()
	statements := []struct {
		query string
		args  []any
	}{
		{query: `
INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload
) VALUES ($1, 'cluster', 'kubernetes', $1, 'kubernetes', $1,
          clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb)`, args: []any{kubernetesRuntimePerformanceScope}},
		{query: `
INSERT INTO scope_generations (
  generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload
) VALUES ($2, $1, 'test', clock_timestamp(), clock_timestamp(), 'active', clock_timestamp(), '{}'::jsonb)`, args: []any{kubernetesRuntimePerformanceScope, kubernetesRuntimePerformanceGen}},
		{query: `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, args: []any{kubernetesRuntimePerformanceScope, kubernetesRuntimePerformanceGen}},
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed Postgres performance scope: %v", err)
		}
	}
	uids := make([]string, len(candidates))
	factIDs := make([]string, len(candidates))
	for i, candidate := range candidates {
		uids[i] = candidate.WorkloadUID
		factIDs[i] = "fact:" + candidate.WorkloadUID
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  collector_kind, source_confidence, source_system, source_fact_key,
  observed_at, ingested_at, is_tombstone, payload
)
SELECT fact_id, $3, $4, 'kubernetes_workload', uid,
       'kubernetes', 'observed', 'kubernetes', uid,
       clock_timestamp(), clock_timestamp(), FALSE, '{}'::jsonb
FROM UNNEST($1::text[], $2::text[]) AS input(uid, fact_id)
`, uids, factIDs, kubernetesRuntimePerformanceScope, kubernetesRuntimePerformanceGen); err != nil {
		t.Fatalf("seed Postgres performance facts: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO graph_node_owner (uid, source_order_key, winning_row, updated_at)
SELECT uid, '2026-08-10T00:00:00Z|' || fact_id,
       jsonb_build_object('source_fact_id', fact_id, 'cluster_id', 'cluster-live',
                          'namespace', 'payments', 'name', uid), clock_timestamp()
FROM UNNEST($1::text[], $2::text[]) AS input(uid, fact_id)
`, uids, factIDs); err != nil {
		t.Fatalf("seed Postgres performance owners: %v", err)
	}
}

func assertKubernetesRuntimePostgresPlans(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	candidates []KubernetesRuntimeCandidate,
) {
	t.Helper()
	shapes := []struct {
		name       string
		candidates []KubernetesRuntimeCandidate
		allScopes  bool
	}{
		{name: "scoped-200", candidates: candidates[:200], allScopes: false},
		{name: "all-scopes-400", candidates: candidates[:400], allScopes: true},
	}
	for _, shape := range shapes {
		query, args := buildKubernetesRuntimeWorkloadQuery(
			shape.candidates, shape.allScopes, nil, []string{kubernetesRuntimePerformanceScope},
		)
		var raw []byte
		if err := db.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
			t.Fatalf("explain Postgres %s shape: %v", shape.name, err)
		}
		var plan []map[string]any
		if err := json.Unmarshal(raw, &plan); err != nil || len(plan) != 1 {
			t.Fatalf("decode Postgres %s plan: %v", shape.name, err)
		}
		t.Logf("Postgres %s: planning_ms=%v execution_ms=%v", shape.name, plan[0]["Planning Time"], plan[0]["Execution Time"])
	}
}
